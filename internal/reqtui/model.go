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
	columnHeader = lipgloss.NewStyle().Bold(true)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
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

	theme   string
	layout  []columnGeometry

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
		theme:   "auto",
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
	case tea.MouseMsg:
		return m.handleMouse(ev)
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

// handleMouse responds to terminal mouse events: clicking a column header or
// body moves focus to that column, and clicking a row selects it.
func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.current != viewBoard {
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	x, y := msg.X, msg.Y
	// resolve the column from the saved layout (body starts after header rows)
	colX := x
	if state, ok := columnAt(m.layout, colX); ok {
		m.focus = indexOf(statusColumns, state)
	}
	_ = y
	return m, nil
}

func (m *Model) boardView() string {
	p := paletteFor(m.theme)
	// header row
	header := p.style("title").Render(" " + m.t("reqtui.title") + "   ")
	right := m.t("reqtui.help")
	rightWidth := displayWidth(right)
	gap := m.width - displayWidth(header) - rightWidth
	if gap < 1 {
		gap = 0
	}
	titleLine := header + strings.Repeat(" ", max(0, gap)) + right

	// column panels
	m.layout = layoutColumns(m.width, len(statusColumns))
	columnHeight := max(1, m.height-3)
	blocks := make([]string, 0, len(m.layout)*2)
	for i, col := range m.layout {
		focused := col.State == statusColumns[m.focus]
		blocks = append(blocks, m.renderColumnPanel(p, col, columnHeight, focused))
		if i < len(m.layout)-1 {
			blocks = append(blocks, p.fillColumn(1, columnHeight))
		}
	}
	board := lipgloss.JoinHorizontal(lipgloss.Top, blocks...)

	var b strings.Builder
	b.WriteString(titleLine)
	b.WriteString("\n")
	b.WriteString(p.fillLine(m.width))
	b.WriteString("\n")
	b.WriteString(board)

	// problems / status
	if len(m.problems) > 0 {
		b.WriteString("\n")
		for _, problem := range m.problems {
			b.WriteString(p.style("warn").Render(problem))
			b.WriteString("\n")
		}
	}
	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(p.style("warn").Render(m.err))
	} else if m.statusMessage != "" {
		b.WriteString("\n")
		b.WriteString(p.style("dim").Render(m.statusMessage))
	}

	return p.fillBlock(b.String(), m.width, m.height)
}

func (m *Model) renderColumnPanel(p palette, col columnGeometry, bodyHeight int, focused bool) string {
	status := col.State
	list := m.columns[status]
	width := col.Width
	label := m.columnLabel(status)
	badge := itoa(len(list))

	lines := []string{panelTop(p, status, label, badge, width, focused)}
	contentWidth := width - panelChrome
	if contentWidth < 1 {
		contentWidth = 1
	}
	body := make([]string, 0, bodyHeight)
	if len(list) == 0 {
		body = append(body, p.style("dim").Render(centerText(m.t("reqtui.empty"), contentWidth)))
	} else {
		cur := m.cursor[status]
		for row, idx := range list {
			req := m.requirements[idx]
			selected := focused && row == cur
			card := m.cardLines(req, status, contentWidth)
			for line, text := range card {
				body = append(body, reqCardStyle(p, status, line, selected).Render(" "+padText(text, contentWidth)+" "))
			}
			body = append(body, "")
		}
	}
	for i := 0; i < bodyHeight; i++ {
		content := ""
		if i < len(body) {
			content = body[i]
		}
		lines = append(lines, panelRow(p, status, content, width, focused))
	}
	lines = append(lines, panelBottom(p, status, width, focused))
	return strings.Join(lines, "\n")
}

// cardLines renders the visible lines of one requirement card: the title then
// the status/progress metadata.
func (m *Model) cardLines(req board.Requirement, status string, width int) []string {
	title := req.Title
	if status == board.ReqStatusCompleted && req.Total > 0 {
		title += " " + itoa(req.Done) + "/" + itoa(req.Total)
	}
	meta := compactID(req.ID)
	if req.Source != "" {
		meta += "  " + compactSource(req.Source)
	}
	return []string{clipText(title, width), clipText(meta, width)}
}

func compactID(id string) string {
	if len(id) > 9 && id[:8][0] >= '0' && id[:8][0] <= '9' {
		return id[9:]
	}
	return id
}

func compactSource(source string) string {
	if strings.HasPrefix(source, "pool://") {
		return "@" + strings.TrimPrefix(source, "pool://")
	}
	return source
}

func (m *Model) rowHeight() int { return m.height }

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
	p := paletteFor(m.theme)
	var b strings.Builder
	b.WriteString(p.style("heading-" + req.Status).Render(req.Title))
	b.WriteString("\n\n")
	meta := []string{
		"ID: " + req.ID,
		"STATUS: " + p.style("heading-" + req.Status).Render(m.columnLabel(req.Status)),
	}
	if req.Created != "" {
		meta = append(meta, "CREATED: "+req.Created)
	}
	if req.Source != "" {
		meta = append(meta, "SOURCE: "+req.Source)
	}
	if req.Total > 0 {
		meta = append(meta, "PROGRESS: "+itoa(req.Done)+"/"+itoa(req.Total))
	}
	if len(req.Tasks) > 0 {
		meta = append(meta, "TASKS: "+strings.Join(req.Tasks, ", "))
	}
	b.WriteString(p.style("dim").Render(strings.Join(meta, "\n")))
	b.WriteString("\n\n")
	if req.Summary != "" && req.Summary != board.Placeholder {
		b.WriteString(renderMarkdown(req.Summary, m.theme))
		b.WriteString("\n\n")
	}
	if req.Notes != "" && req.Notes != "N/A" {
		b.WriteString(columnHeader.Render(m.t("reqtui.notes")))
		b.WriteString("\n")
		b.WriteString(req.Notes)
	}
	b.WriteString("\n\n")
	b.WriteString(p.style("dim").Render(m.t("reqtui.back_hint")))
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
