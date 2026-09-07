package launch

import (
	"strings"
	"time"
)

// dshTUIReady reports whether the dsh TUI input prompt is visible in the
// captured pane text. Injection is only safe once the prompt exists; keys
// sent during TUI startup are dropped. The loose "dsh>" variant matches the
// dsh-onevoke readiness probe.
func dshTUIReady(paneText string) bool {
	return strings.Contains(paneText, "dsh >") || strings.Contains(paneText, "dsh>")
}

// The readiness poll mirrors dsh-onevoke's 40x2s (~80s) budget.
const (
	dshReadyPollCount    = 40
	dshReadyPollInterval = 2 * time.Second
)

// tmuxPaneText captures the currently visible text of a tmux pane.
func tmuxPaneText(tmux, pane string) string {
	return tmuxCapture(tmux, "capture-pane", "-t", pane, "-p").Stdout
}

// tmuxSendText sends literal text into the pane (no key-name expansion).
func tmuxSendText(tmux, pane, text string) cmdResult {
	return tmuxCapture(tmux, "send-keys", "-t", pane, "-l", text)
}

// tmuxSendEnter sends Enter into the pane.
func tmuxSendEnter(tmux, pane string) cmdResult {
	return tmuxCapture(tmux, "send-keys", "-t", pane, "Enter")
}

// waitDSHReady polls until the dsh TUI prompt appears or the budget runs out.
// A false return means the prompt is not injected and the user must paste it.
func waitDSHReady(tmux, pane string) bool {
	for i := 0; i < dshReadyPollCount; i++ {
		sleepFn(dshReadyPollInterval)
		if dshTUIReady(tmuxPaneText(tmux, pane)) {
			return true
		}
	}
	return false
}

// injectDSHTask injects the task instruction into a ready dsh TUI pane.
// Only the tmux launchers are supported; herdr injection is out of scope.
func injectDSHTask(plan LaunchPlan, outcome LaunchOutcome, prompt string) error {
	if plan.Launcher != "tmux" && plan.Launcher != "tmux-session" {
		return launchError("launch.dsh_prompt_injection_requires_the_tmux_launcher", plan.Launcher)
	}
	pane := outcome.Pane
	if pane == "" {
		return launchError("launch.dsh_prompt_injection_has_no_pane_target")
	}
	if !waitDSHReady(plan.Tmux, pane) {
		return launchError("launch.dsh_tui_did_not_become_ready_in_time_please_paste")
	}
	if res := tmuxSendText(plan.Tmux, pane, prompt); res.Code != 0 {
		return launchError("launch.dsh_failed_to_send_prompt", orExit(trimNL(res.Stderr), res.Code))
	}
	if res := tmuxSendEnter(plan.Tmux, pane); res.Code != 0 {
		return launchError("launch.dsh_failed_to_send_enter", orExit(trimNL(res.Stderr), res.Code))
	}
	return nil
}
