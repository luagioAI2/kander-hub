// Package reqtui is a standalone terminal UI for the requirements pool
// (kanban/requirements/*.md). It is deliberately separate from the kanban
// board TUI: kander keeps its own TUI untouched for low-cost upstream sync,
// while this package renders the requirements screen and delegates work
// (decompose, new, complete, archive) to the board.Requirement APIs and the
// launch hook.
package reqtui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

var (
	selectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("24")).
			Foreground(lipgloss.Color("255"))
	statusStyle = map[string]lipgloss.Style{
		board.ReqStatusDraft:      lipgloss.NewStyle().Foreground(lipgloss.Color("220")),
		board.ReqStatusDecomposed: lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		board.ReqStatusCompleted:  lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		board.ReqStatusArchived:   lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	}
	columnHeader = lipgloss.NewStyle().Bold(true)
	msgStyle     = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208"))
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// statusColumns is the ordered set of columns shown on the board screen.
var statusColumns = []string{
	board.ReqStatusDraft,
	board.ReqStatusDecomposed,
	board.ReqStatusCompleted,
}

// view is the top-level Bubble Tea screen: the requirements board or a
// scrolling detail view over one requirement.
type view int

const (
	viewBoard view = iota
	viewDetail
)

// Model is the Bubble Tea model of the requirements pool TUI.
type Model struct {
	root         string
	requirements []board.Requirement
	problems     []string
	width        int
	height       int

	columns map[string][]int // status -> indices into requirements
	focus   int              // index into statusColumns
	cursor  map[string]int   // status -> selected row index

	current view
	detail  *board.Requirement
	viewport viewport.Model

	statusMessage string
	err           string

	// autoDecomposeSlug is set after a "new + decompose now" form so that once
	// the card is created and the pool reloads we launch the agent on it.
	autoDecomposeSlug string

	quit bool
}

// New creates a fresh model bound to the given board root.
func New(root string) *Model {
	m := &Model{
		root:    root,
		columns: make(map[string][]int),
		cursor:  make(map[string]int),
		current: viewBoard,
		width:   80,
		height:  24,
	}
	m.reload()
	return m
}

// Init satisfies tea.Model.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update satisfies tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch ev := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = ev.Width
		m.height = ev.Height
		if m.viewport.Height > 0 {
			m.viewport.Width = ev.Width
			m.viewport.Height = m.detailHeight()
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(ev)
	case successMsg:
		m.statusMessage = m.statusText(ev)
		m.err = ""
		m.reload()
		if ev.op == "new" && m.autoDecomposeSlug != "" {
			return m, m.launchDecomposeOn(findBySlug(m.requirements, m.autoDecomposeSlug))
		}
		return m, nil
	case failureMsg:
		m.err = string(ev)
		return m, nil
	case formResult:
		switch ev.kind {
		case "new":
			m.autoDecomposeSlug = ""
			if ev.decompose {
				m.autoDecomposeSlug = ev.slug
			}
			return m, m.createRequirement(ev)
		case "decompose":
			return m, m.finishDecompose(ev)
		}
	}
	return m, nil
}

func (m *Model) detailHeight() int {
	h := m.height - 3
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) handleKey(event tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.current == viewDetail {
		return m.handleDetailKey(event)
	}
	return m.handleBoardKey(event)
}

func (m *Model) handleBoardKey(event tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch event.String() {
	case "q", "ctrl+c":
		m.quit = true
		return m, tea.Quit
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "h", "left":
		m.moveFocus(-1)
	case "l", "right", "tab":
		m.moveFocus(1)
	case "shift+tab":
		m.moveFocus(-1)
	case "d":
		return m, m.openDecomposeForm()
	case "n":
		return m, m.openNewForm()
	case "c":
		return m, m.complete()
	case "a":
		return m, m.archive()
	case "enter":
		m.openDetail()
	case "r":
		m.reload()
		m.statusMessage = m.t("reqtui.reloaded")
	}
	return m, nil
}

func (m *Model) handleDetailKey(event tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch event.String() {
	case "q", "esc":
		m.current = viewBoard
		m.viewport = viewport.Model{}
		return m, nil
	}
	if m.viewport.Height > 0 {
		m.viewport, _ = m.viewport.Update(tea.KeyMsg(event))
	}
	return m, nil
}

func (m *Model) moveFocus(step int) {
	n := len(statusColumns)
	if n == 0 {
		return
	}
	m.focus = (m.focus + step + n) % n
}

func (m *Model) moveCursor(step int) {
	status := statusColumns[m.focus]
	list := m.columns[status]
	cur := m.cursor[status]
	if cur >= len(list) {
		cur = 0
	}
	m.cursor[status] = cur + step
	if m.cursor[status] < 0 {
		m.cursor[status] = 0
	}
	if max := len(list) - 1; m.cursor[status] > max && max >= 0 {
		m.cursor[status] = max
	}
}

func (m *Model) currentRequirement() *board.Requirement {
	if m.focus >= len(statusColumns) {
		return nil
	}
	status := statusColumns[m.focus]
	list := m.columns[status]
	cur := m.cursor[status]
	if cur < 0 || cur >= len(list) {
		return nil
	}
	idx := list[cur]
	if idx < 0 || idx >= len(m.requirements) {
		return nil
	}
	return &m.requirements[idx]
}

// reload rescans the requirement pool and re-partitions the columns.
func (m *Model) reload() {
	reqs, problems, err := board.LoadRequirements(m.root)
	m.requirements = reqs
	m.problems = nil
	for _, problem := range problems {
		m.problems = append(m.problems, problem.Message)
	}
	if err != nil {
		m.err = err.Error()
	}
	m.columns = make(map[string][]int)
	for i := range statusColumns {
		m.columns[statusColumns[i]] = nil
	}
	for i, req := range reqs {
		status := req.Status
		if _, ok := m.columns[status]; !ok {
			status = board.ReqStatusDraft
		}
		m.columns[status] = append(m.columns[status], i)
	}
	for _, status := range statusColumns {
		if cur := m.cursor[status]; cur >= len(m.columns[status]) && len(m.columns[status]) > 0 {
			m.cursor[status] = len(m.columns[status]) - 1
		}
	}
	if m.focus >= len(statusColumns) {
		m.focus = 0
	}
}

// View satisfies tea.Model.
func (m *Model) View() string {
	if m.current == viewDetail {
		return m.detailView()
	}
	return m.boardView()
}

func (m *Model) columnLabel(status string) string {
	labels := map[string]string{
		board.ReqStatusDraft:      m.t("reqtui.draft"),
		board.ReqStatusDecomposed: m.t("reqtui.decomposed"),
		board.ReqStatusCompleted:  m.t("reqtui.completed"),
	}
	if label, ok := labels[status]; ok {
		return label
	}
	return status
}

func (m *Model) boardView() string {
	var cols []string
	for _, status := range statusColumns {
		cols = append(cols, m.renderColumn(status))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)

	var b strings.Builder
	b.WriteString(columnHeader.Render(m.t("reqtui.title")))
	b.WriteString("\n\n")
	b.WriteString(body)
	if len(m.problems) > 0 {
		b.WriteString("\n")
		for _, problem := range m.problems {
			b.WriteString(msgStyle.Render(problem))
			b.WriteString("\n")
		}
	}
	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(msgStyle.Render(m.err))
	} else if m.statusMessage != "" {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(m.statusMessage))
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(m.t("reqtui.help")))
	return b.String()
}

func (m *Model) renderColumn(status string) string {
	list := m.columns[status]
	header := columnHeader.Render(m.columnLabel(status) + " (" + itoa(len(list)) + ")")
	rows := []string{header}
	if len(list) == 0 {
		rows = append(rows, dimStyle.Render(m.t("reqtui.empty")))
	} else {
		cur := m.cursor[status]
		columnFocused := m.focus == indexOf(statusColumns, status)
		for row, idx := range list {
			req := m.requirements[idx]
			title := req.Title
			if status == board.ReqStatusCompleted && req.Total > 0 {
				title += " " + itoa(req.Done) + "/" + itoa(req.Total)
			}
			label := m.columnLabel(status)
			if s, ok := statusStyle[status]; ok {
				label = s.Render(label)
			}
			line := label + " " + title
			if columnFocused && row == cur {
				rows = append(rows, selectedStyle.Render(line))
			} else {
				rows = append(rows, line)
			}
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func indexOf(list []string, value string) int {
	for i, v := range list {
		if v == value {
			return i
		}
	}
	return -1
}

func findBySlug(reqs []board.Requirement, slug string) *board.Requirement {
	for i := range reqs {
		if reqs[i].ID == slug+"-req" || strings.HasSuffix(reqs[i].ID, "-"+slug+"-req") {
			return &reqs[i]
		}
	}
	return nil
}

// openDetail captures the focused requirement and switches to the scrolling
// detail view.
func (m *Model) openDetail() {
	req := m.currentRequirement()
	if req == nil {
		return
	}
	copy := *req
	m.detail = &copy
	m.current = viewDetail
	m.viewport = viewport.New(m.width, m.detailHeight())
	m.viewport.SetContent(m.renderDetail(&copy))
}

func (m *Model) renderDetail(req *board.Requirement) string {
	var b strings.Builder
	b.WriteString(columnHeader.Render(req.Title))
	b.WriteString("\n\n")
	meta := []string{
		"ID: " + req.ID,
		"STATUS: " + m.columnLabel(req.Status),
	}
	if req.Created != "" {
		meta = append(meta, "CREATED: "+req.Created)
	}
	if len(req.Tasks) > 0 {
		meta = append(meta, "TASKS: "+strings.Join(req.Tasks, ", "))
	}
	b.WriteString(dimStyle.Render(strings.Join(meta, "  ")))
	b.WriteString("\n\n")
	if req.Summary != "" && req.Summary != board.Placeholder {
		b.WriteString(renderMarkdown(req.Summary))
		b.WriteString("\n\n")
	}
	if req.Notes != "" && req.Notes != "N/A" {
		b.WriteString(columnHeader.Render(m.t("reqtui.notes")))
		b.WriteString("\n")
		b.WriteString(req.Notes)
	}
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render(m.t("reqtui.back_hint")))
	return b.String()
}

func (m *Model) detailView() string {
	return m.viewport.View() + "\n" + dimStyle.Render(m.t("reqtui.back_hint"))
}

func (m *Model) t(id string, args ...any) string {
	return config.Text(id, args...)
}

// itoa avoids pulling strconv into the render path; a tiny helper for counts.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
