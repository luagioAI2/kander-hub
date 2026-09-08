package reqtui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/term"
)

// readKey reads a single key from r. If r is a real terminal, raw mode is used
// so single keystrokes are returned; for non-terminal input it falls back to
// reading whole lines. On cfg.Refresh expiry the sentinel errRefresh is
// returned so the caller can redraw.
var errRefresh = errors.New("reqtui: refresh")

func readKey(r io.Reader, refresh time.Duration) (string, error) {
	if refresh <= 0 {
		refresh = 2 * time.Second
	}
	type result struct {
		key string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		key, err := readKeyOnce(r)
		ch <- result{key: key, err: err}
	}()
	select {
	case res := <-ch:
		return res.key, res.err
	case <-time.After(refresh):
		return "", errRefresh
	}
}

func readKeyOnce(r io.Reader) (string, error) {
	type fd interface {
		Fd() uintptr
	}
	if f, ok := r.(fd); ok && term.IsTerminal(int(f.Fd())) {
		oldState, err := term.MakeRaw(int(f.Fd()))
		if err == nil {
			defer term.Restore(int(f.Fd()), oldState)
			return readFromTerminal(r, f)
		}
	}
	reader := bufio.NewReader(r)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// fdReader combines an io.Reader and the fd type so we can read raw bytes
// from the same handle the terminal raw mode set up.
type fdReader interface {
	io.Reader
	Fd() uintptr
}

// fd is the minimal interface used to detect a terminal handle.
type fd interface {
	Fd() uintptr
}

func readFromTerminal(r io.Reader, f fd) (string, error) {
	if fr, ok := r.(fdReader); ok && fr.Fd() == f.Fd() {
		buf := make([]byte, 4)
		n, err := fr.Read(buf)
		if err != nil && n == 0 {
			return "", err
		}
		return decodeKey(buf[:n]), nil
	}
	reader := bufio.NewReader(r)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// readAtLeast was an earlier raw-mode helper. The current implementation reads
// directly from the underlying terminal file, so this function is no longer
// referenced; it is kept to keep the test surface stable.
func readAtLeast(r io.Reader, buf []byte, min int) (int, error) {
	if min < 1 {
		min = 1
	}
	total := 0
	for total < min {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			if total > 0 {
				return total, nil
			}
			return 0, err
		}
	}
	return total, nil
}

// decodeKey maps the raw terminal escape sequences produced in raw mode to
// the action keys the rest of the TUI understands.
func decodeKey(buf []byte) string {
	if len(buf) == 0 {
		return ""
	}
	if buf[0] == 0x1b {
		if len(buf) == 1 {
			return "esc"
		}
		if buf[1] == '[' || buf[1] == 'O' {
			tail := string(buf[2:])
			switch tail {
			case "A":
				return "up"
			case "B":
				return "down"
			case "C":
				return "right"
			case "D":
				return "left"
			case "H":
				return "home"
			case "F":
				return "end"
			}
			if strings.HasPrefix(tail, "5~") {
				return "pgup"
			}
			if strings.HasPrefix(tail, "6~") {
				return "pgdn"
			}
		}
		return "esc"
	}
	if buf[0] == 0x03 {
		return "ctrl-c"
	}
	if buf[0] == 0x0d || buf[0] == 0x0a {
		return "enter"
	}
	if buf[0] == 0x20 {
		return "space"
	}
	switch buf[0] {
	case 'q', 'Q', 'j', 'k', 'g', 'G', 'h', 'l', 'n':
		return string(buf[0:1])
	}
	if buf[0] == 0x7f || buf[0] == 0x08 {
		return "backspace"
	}
	return fmt.Sprintf("byte-%x", buf[0])
}

// terminalSize returns the terminal size for w when w points at a TTY. Tests
// can substitute a non-tty writer to keep cfg deterministic.
func terminalSize(w io.Writer) (int, int, error) {
	type fd interface {
		Fd() uintptr
	}
	if f, ok := w.(fd); ok {
		if term.IsTerminal(int(f.Fd())) {
			width, height, err := term.GetSize(int(f.Fd()))
			if err == nil {
				return width, height, nil
			}
			return 0, 0, err
		}
	}
	return 0, 0, errors.New("not a terminal")
}
