//go:build windows

package launch

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestApplyConsoleAttr(t *testing.T) {
	cmd := exec.Command("cmd")
	applyConsoleAttr(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}
	want := uint32(windows.CREATE_NEW_CONSOLE | windows.CREATE_NEW_PROCESS_GROUP)
	if cmd.SysProcAttr.CreationFlags&want != want {
		t.Fatalf("CreationFlags=%#x want bits %#x", cmd.SysProcAttr.CreationFlags, want)
	}
}

// TestIsWindowsBatchConsole confirms the helper recognises the exact argv
// shape internal/process emits for a .cmd/.bat agent on Windows: a resolved
// cmd.exe path followed by /d, /s, /v:off, /c and the inner command. Any
// other shape (native exe, shell invocation, missing flag) must miss.
func TestIsWindowsBatchConsole(t *testing.T) {
	cmd := func(parts ...string) []string {
		out := append([]string{`C:\Windows\System32\cmd.exe`}, parts...)
		return out
	}
	cases := []struct {
		name    string
		argv    []string
		console bool
		want    bool
	}{
		{
			name:    "full path cmd.exe batch wrapper",
			argv:    cmd("/d", "/s", "/v:off", "/c", "%KANDER_CMD_AAA_0%"),
			console: true,
			want:    true,
		},
		{
			name:    "bare cmd.exe batch wrapper",
			argv:    append([]string{"cmd.exe"}, "/d", "/s", "/v:off", "/c", "x"),
			console: true,
			want:    true,
		},
		{
			name:    "native exe is never batch console",
			argv:    []string{`C:\Tools\dsh.exe`, "--profile", "tui"},
			console: true,
			want:    false,
		},
		{
			name:    "shell invocation is never batch console",
			argv:    cmd("/d", "/s", "/v:off", "/c", "%KANDER_CMD_AAA_0%"),
			console: false,
			want:    false,
		},
		{
			name:    "missing /v:off is rejected",
			argv:    cmd("/d", "/s", "/c", "x"),
			console: true,
			want:    false,
		},
		{
			name:    "argv too short is rejected",
			argv:    cmd("/d", "/s", "/v:off"),
			console: true,
			want:    false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWindowsBatchConsole(tc.argv, tc.console); got != tc.want {
				t.Fatalf("isWindowsBatchConsole(%v, %v) = %v, want %v", tc.argv, tc.console, got, tc.want)
			}
		})
	}
}

// TestBuildDetachedBatchArgv checks the rewrapped form used to detach a
// batch invocation into a real console on Windows. cmd /c start receives the
// inner %VAR% string untouched and /D pins the working directory.
func TestBuildDetachedBatchArgv(t *testing.T) {
	cmdExe := `C:\Windows\System32\cmd.exe`
	inner := `%KANDER_CMD_AAA_0% %KANDER_CMD_AAA_1%`
	cwd := `C:\plan\support\kander-hub-smoke`

	got, err := buildDetachedBatchArgv(cmdExe, inner, cwd)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		cmdExe, "/d", "/s", "/v:off", "/c",
		"start", "\"kander\"", "/D", cwd,
		"cmd", "/d", "/s", "/v:off", "/c", inner,
	}
	if !equalStrings(got, want) {
		t.Fatalf("argv mismatch\n got: %v\nwant: %v", got, want)
	}
	if !strings.Contains(strings.Join(got, " "), filepath.FromSlash(cwd)) {
		t.Fatalf("expected /D %s in argv, got %v", cwd, got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
