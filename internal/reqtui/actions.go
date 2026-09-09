package reqtui

import (
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/dualface/kander/internal/board"
)

// newSlugRe mirrors board's requirement slug rule (lowercase ASCII letters,
// digits, and hyphens between segments). Used to give immediate in-form
// feedback instead of failing silently on submit.
var newSlugRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

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
	// attachment paths collected by the decompose form, written to the card
	// before the agent launches
	attach []string
	// the requirement id a decompose form targeted
	reqID string
}

// validateNewSlug is an in-form validator for the new-requirement slug field.
// It rejects an empty / ill-formed slug and one that would collide with an
// existing requirement id under today's date, so the user sees the problem
// while filling in the form instead of only discovering after submit that no
// card was created.
func (m *Model) validateNewSlug(v string) error {
	v = strings.ToLower(strings.TrimSpace(v))
	if !newSlugRe.MatchString(v) {
		return errors.New(m.t("board.slug_may_contain_only_lowercase_ascii_letters_digits_and"))
	}
	id := time.Now().Format("20060102") + "-" + v + "-req"
	for _, r := range m.requirements {
		if r.ID == id {
			return errors.New(m.t("reqtui.new_slug_taken", v))
		}
	}
	return nil
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
				Value(&slug).
				Validate(m.validateNewSlug),
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
			kind: "new", title: title, slug: strings.ToLower(strings.TrimSpace(slug)),
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
	var message, attachInput string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title(m.t("reqtui.decompose_message_title")).
				Value(&message),
			huh.NewInput().
				Title(m.t("reqtui.decompose_attach")).
				Description(m.t("reqtui.decompose_attach_hint")).
				Prompt("> ").
				Value(&attachInput),
		),
	).WithShowHelp(true)
	return tea.Exec(&execCommand{run: form.Run}, func(err error) tea.Msg {
		if err != nil {
			return failureMsg(err.Error())
		}
		return formResult{kind: "decompose", reqID: req.ID, body: message, attach: splitAttachInput(attachInput)}
	})
}

// splitAttachInput splits a comma- or semicolon-separated attachment path
// list from the decompose form into individual trimmed paths.
func splitAttachInput(raw string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// finishDecompose launches the agent via the board hook. Attachments typed in
// the form are persisted on the card first so the launch prompt picks them
// up. The launch opens its own launcher window (tmux pane, herdr tab or
// console window) and returns when the session is up; the status line then
// offers the w key to switch to the window.
func (m *Model) finishDecompose(res formResult) tea.Cmd {
	id, message, attach := res.reqID, res.body, res.attach
	return func() tea.Msg {
		if len(attach) > 0 {
			if _, err := board.SetRequirementAttachments(m.root, id, attach); err != nil {
				return failureMsg(err.Error())
			}
		}
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
