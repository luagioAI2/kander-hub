package reqtui

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/dualface/kander/internal/board"
)

// successMsg reports a completed write so Update can reload the pool.
type successMsg struct {
	op string
	id string
}

func (m *Model) statusText(ev successMsg) string {
	switch ev.op {
	case "new":
		return m.t("reqtui.created", ev.id)
	case "complete":
		return m.t("reqtui.completed_ok", ev.id)
	case "archive":
		return m.t("reqtui.archived_ok", ev.id)
	case "decompose":
		return m.t("reqtui.decompose_launched", ev.id)
	}
	return ""
}

type failureMsg string

// writeTask returns a background command that performs one requirement write
// and reports a success or failure message.
func (m *Model) writeTask(op, id string, run func() error) tea.Cmd {
	return func() tea.Msg {
		if err := run(); err != nil {
			return failureMsg(err.Error())
		}
		return successMsg{op: op, id: id}
	}
}

// complete flips the focused requirement to completed.
func (m *Model) complete() tea.Cmd {
	req := m.currentRequirement()
	if req == nil {
		return nil
	}
	return m.writeTask("complete", req.ID, func() error {
		_, err := board.SetRequirementStatus(m.root, req.ID, board.ReqStatusCompleted)
		return err
	})
}

// archive flips the focused requirement to archived.
func (m *Model) archive() tea.Cmd {
	req := m.currentRequirement()
	if req == nil {
		return nil
	}
	return m.writeTask("archive", req.ID, func() error {
		_, err := board.SetRequirementStatus(m.root, req.ID, board.ReqStatusArchived)
		return err
	})
}

// execCommand hands the terminal to a blocking program (a Huh form) and waits
// for it to finish, using Bubble Tea's alt-screen-aware Exec.
type execCommand struct {
	run func() error
}

func (c *execCommand) SetStdin(io.Reader)  {}
func (c *execCommand) SetStdout(io.Writer) {}
func (c *execCommand) SetStderr(io.Writer) {}
func (c *execCommand) Run() error          { return c.run() }

// formResult carries the values a completed form submitted back to the model.
type formResult struct {
	kind string
	// new requirement
	title string
	slug  string
	// decompose immediately after creating the card
	decompose bool
	// decompose message or summary body
	body string
	// the requirement id a decompose form targeted
	reqID string
}

// openNewForm suspends the TUI, collects a new-requirement form (title /
// slug / summary / decompose-now?), and reports the values through formResult.
func (m *Model) openNewForm() tea.Cmd {
	var title, slug, summary string
	var decompose bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(m.t("reqtui.new_title")).
				Prompt("> ").
				Value(&title),
			huh.NewInput().
				Title(m.t("reqtui.new_slug")).
				Prompt("> ").
				Value(&slug),
			huh.NewText().
				Title(m.t("reqtui.new_summary")).
				Value(&summary),
			huh.NewConfirm().
				Title(m.t("reqtui.new_decompose")).
				Value(&decompose),
		),
	).WithShowHelp(true)
	return tea.Exec(&execCommand{run: form.Run}, func(err error) tea.Msg {
		if err != nil {
			return failureMsg(err.Error())
		}
		return formResult{
			kind: "new", title: title, slug: slug,
			body: summary, decompose: decompose,
		}
	})
}

// createRequirement creates the card after a completed new-requirement form.
func (m *Model) createRequirement(res formResult) tea.Cmd {
	var createdID string
	return func() tea.Msg {
		id, err := board.AddRequirement(m.root, res.slug, res.title, "N/A", res.body)
		if err != nil {
			return failureMsg(err.Error())
		}
		createdID = id
		// The model reload reads the pool after this message; the auto-decompose
		// chain is keyed by the slug the user typed, so the created id here only
		// feeds the status line.
		return successMsg{op: "new", id: createdID}
	}
}

// launchDecomposeOn launches the decompose agent for the given requirement. It
// suspends once more for the optional message form, then runs the launch hook.
func (m *Model) launchDecomposeOn(req *board.Requirement) tea.Cmd {
	if req == nil {
		return nil
	}
	if board.RunRequirementDecompose == nil {
		m.err = m.t("reqtui.decompose_unavailable")
		return nil
	}
	var message string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title(m.t("reqtui.decompose_message_title")).
				Value(&message),
		),
	).WithShowHelp(true)
	return tea.Exec(&execCommand{run: form.Run}, func(err error) tea.Msg {
		if err != nil {
			return failureMsg(err.Error())
		}
		return formResult{kind: "decompose", reqID: req.ID, body: message}
	})
}

// finishDecompose launches the agent via the board hook. The launch opens its
// own launcher window (tmux pane, herdr tab or console window) and returns
// when the session is up; the user switches over to that window to follow or
// steer the agent.
func (m *Model) finishDecompose(res formResult) tea.Cmd {
	id, message := res.reqID, res.body
	return func() tea.Msg {
		args := []string{"--message", message, id}
		if rc := board.RunRequirementDecompose(args); rc != 0 {
			return failureMsg(m.t("reqtui.decompose_failed", id))
		}
		return successMsg{op: "decompose", id: id}
	}
}

// openDecomposeForm launches the decompose agent for the focused requirement:
// first the optional message form, then the launch hook.
func (m *Model) openDecomposeForm() tea.Cmd {
	req := m.currentRequirement()
	if req == nil {
		return nil
	}
	return m.launchDecomposeOn(req)
}
