package launch

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

func isWindowsGOOS() bool { return runtime.GOOS == "windows" }

func lookPathExec(name string) (string, error) {
	return exec.LookPath(name)
}

func fileIsTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

type startedProc struct {
	proc *os.Process
	mu   sync.Mutex
	code *int
	wait chan int
}

func (s *startedProc) Wait() (int, error) {
	c, ok := <-s.wait
	if !ok {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.code != nil {
			return *s.code, nil
		}
		return 0, nil
	}
	return c, nil
}

func (s *startedProc) Poll() *int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.code == nil {
		return nil
	}
	v := *s.code
	return &v
}

func startProcess(argv []string, env map[string]string, cwd string, console bool) (*startedProc, error) {
	// On Windows the console launcher wraps every .cmd/.bat entry in
	// `cmd.exe /d /s /v:off /c ...`. CREATE_NEW_CONSOLE alone leaves the
	// inner process attached to the parent's stdio, so any TTY-required CLI
	// (DSH TUI, etc.) rejects the launch with "stdin/stdout must be TTYs".
	// For a batch agent under the console launcher the wrapper is re-emitted
	// via `cmd /c start`, which detaches the batch into a real interactive
	// console of its own. The wrapper exits as soon as the detached window is
	// open, so its handle never reports an exit and the liveness window
	// simply times out as success. Other launchers and native exes keep the
	// original path.
	if isWindowsBatchConsole(argv, console) {
		if cmdExe, lookErr := exec.LookPath("cmd.exe"); lookErr == nil {
			handle, err := startDetachedBatch(cmdExe, argv[5], env, cwd)
			if err == nil {
				return handle, nil
			}
		}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = envSlice(env)
	if console {
		applyConsoleAttr(cmd)
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return waitDone(cmd)
}

// isWindowsBatchConsole reports whether argv is the standard cmd.exe batch
// wrapper (`cmd.exe /d /s /v:off /c ...`) emitted by internal/process for a
// .cmd/.bat agent on the console launcher. argv is the resolved argv the
// wrapper actually built (cmd.exe may appear as a full path).
func isWindowsBatchConsole(argv []string, console bool) bool {
	if !console || runtime.GOOS != "windows" || len(argv) < 6 {
		return false
	}
	if !strings.EqualFold(filepath.Base(argv[0]), "cmd.exe") {
		return false
	}
	return strings.EqualFold(argv[1], "/d") &&
		strings.EqualFold(argv[3], "/v:off") &&
		strings.EqualFold(argv[4], "/c")
}

// startDetachedBatch opens a fresh console window running the inner batch.
// The outer cmd.exe exits as soon as `start` returns, so the returned handle
// never reports an exit; the detached batch owns its console from here on.
func startDetachedBatch(cmdExe, inner string, env map[string]string, cwd string) (*startedProc, error) {
	batchArgs, err := buildDetachedBatchArgv(cmdExe, inner, cwd)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(batchArgs[0], batchArgs[1:]...)
	cmd.Dir = cwd
	cmd.Env = envSlice(env)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Reap the wrapper in the background so it does not linger as a zombie;
	// nobody polls its exit code.
	proc := cmd.Process
	go func() { _ = cmd.Wait() }()
	return &startedProc{proc: proc, wait: make(chan int, 1)}, nil
}

// buildDetachedBatchArgv assembles the outer cmd.exe /c start invocation that
// hands the inner %VAR% string to a fresh console. Exposed so the assembly
// can be unit-tested without spawning a process.
func buildDetachedBatchArgv(cmdExe, inner, cwd string) ([]string, error) {
	return []string{
		cmdExe, "/d", "/s", "/v:off", "/c",
		"start", "\"kander\"", "/D", cwd,
		"cmd", "/d", "/s", "/v:off", "/c", inner,
	}, nil
}

func waitDone(cmd *exec.Cmd) (*startedProc, error) {
	wait := make(chan int, 1)
	handle := &startedProc{proc: cmd.Process, wait: wait}
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				code = 1
			}
		}
		handle.mu.Lock()
		handle.code = &code
		handle.mu.Unlock()
		wait <- code
		close(wait)
	}()
	return handle, nil
}

func terminateProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Signal(syscall.SIGTERM)
}

func killProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}
