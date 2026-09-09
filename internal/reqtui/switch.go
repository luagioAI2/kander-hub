package reqtui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbletea"
)

// switchMsg reports the outcome of a window switch attempt for the status bar.
type switchMsg struct{ text string }

// decomposeWindow extracts the tmux window address recorded on the focused
// requirement by the last decompose launch. The stored value is
// "<launcher>:<session>:<window>:<pane>" (or "herdr:<tab>:<pane>"); only tmux
// addresses are switchable from here.
func decomposeWindow(stored string) (session, window string, ok bool) {
	if stored == "" || !strings.HasPrefix(stored, "tmux") {
		return "", "", false
	}
	parts := strings.Split(stored, ":")
	// tmux:session:window:pane
	if len(parts) >= 4 {
		return parts[1], parts[2], true
	}
	if len(parts) == 3 {
		return parts[1], "", true
	}
	return "", "", false
}

// switchToDecompose switches the terminal to the decompose tmux window of the
// focused requirement. Inside tmux it runs switch-client, which suspends this
// TUI via tea.Exec; outside tmux it reports the manual command instead.
func (m *Model) switchToDecompose() tea.Cmd {
	req := m.currentRequirement()
	if req == nil {
		return nil
	}
	session, window, ok := decomposeWindow(req.Window)
	if !ok {
		return func() tea.Msg {
			return switchMsg{m.t("reqtui.switch_unavailable")}
		}
	}
	target := session
	if window != "" {
		target = session + ":" + window
	}
	if os.Getenv("TMUX") == "" {
		return func() tea.Msg {
			return switchMsg{m.t("reqtui.switch_outside_tmux", "tmux switch-client -t "+target)}
		}
	}
	return tea.Exec(&execCommand{run: func() error {
		cmd := exec.Command("tmux", "switch-client", "-t", target)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}}, func(err error) tea.Msg {
		if err != nil {
			return switchMsg{fmt.Sprintf("%s: %v", m.t("reqtui.switch_failed"), err)}
		}
		return switchMsg{m.t("reqtui.switch_done", target)}
	})
}
