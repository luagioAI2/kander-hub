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

// decomposeWindow extracts the tmux server/session address recorded on the
// focused requirement by the last decompose launch. The stored value is
// "<launcher>:<session>:<window>:<pane>" (or "herdr:<tab>:<pane>"); only tmux
// and tmux-session addresses are switchable from here. In tmux-session mode
// the session name doubles as the tmux server name (kander uses one project
// session per server), so the same value selects the server with `-L`.
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

// switchToDecompose jumps the terminal into the decompose tmux window of the
// focused requirement. The window lives on a kander project tmux server whose
// name equals the project session, so the reliable command is
// `tmux -L <session> attach-session -t <session>:<window>` — it works from
// inside or outside tmux and across servers. Inside an existing tmux session
// this nests, which is the standard kander flow for following an agent.
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
	// The -L server name equals the session in tmux-session mode; when the
	// launcher is plain "tmux" (user's current server) omit -L so we stay on
	// the same server and switch-client applies.
	args := []string{}
	if strings.HasPrefix(req.Window, "tmux-session:") {
		args = []string{"-L", session}
	}
	args = append(args, "attach-session", "-t", target)
	return tea.Exec(&execCommand{run: func() error {
		cmd := exec.Command("tmux", args...)
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
