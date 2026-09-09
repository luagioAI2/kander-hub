// Package reqtui is a standalone terminal UI for the requirements pool
// (kanban/requirements/*.md). It is deliberately separate from the kanban
// board TUI: kander keeps its own TUI untouched for low-cost upstream sync,
// while this package renders the requirements screen and delegates work
// (decompose, new, complete, archive) to the board.Requirement APIs and the
// launch hook.
package reqtui

import (
	"strings"
	"time"

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

	theme  string
	layout []columnGeometry

	// last click position/at is used to recognize a double-click on a card.
	lastClickX, lastClickY int
	lastClickAt            time.Time

	// header search box: searching means a filter query is being typed, query
	// filters which requirements are shown (empty shows all).
	searching bool
	query     string
	// helpOpen renders the ? key-binding overlay on top of the board.
	helpOpen bool

	current  view
	detail   *board.Requirement
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
		if ev.op == "decompose" {
			// reload() refreshed the pool, so the WINDOW field the launch
			// pipeline just wrote is visible here.
			if req := m.currentRequirement(); req != nil && req.Window != "" {
				m.statusMessage = m.t("reqtui.decompose_window_hint", req.Window)
			}
		}
		if ev.op == "new" && m.autoDecomposeSlug != "" {
			return m, m.launchDecomposeOn(findBySlug(m.requirements, m.autoDecomposeSlug))
		}
		return m, nil
	case failureMsg:
		m.err = string(ev)
		return m, nil
	case switchMsg:
		m.statusMessage = ev.text
		m.err = ""
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
	// The help overlay covers the board: any key closes it before it is
	// reinterpreted as a board or detail command.
	if m.helpOpen {
		m.helpOpen = false
		return m, nil
	}
	// While a search query is being typed, printable characters build the
	// query and only a few keys leave the search box.
	if m.searching {
		return m.handleSearchKey(event)
	}
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
	case "/":
		m.searching = true
	case "?":
		m.helpOpen = true
	case "d":
		return m, m.openDecomposeForm()
	case "w":
		return m, m.switchToDecompose()
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

// handleSearchKey edits the header filter query. Random printable characters
// narrow the shown requirements; enter accepts the query and esc clears it.
func (m *Model) handleSearchKey(event tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch event.String() {
	case "enter":
		m.searching = false
	case "esc":
		m.query = ""
		m.searching = false
		m.reload()
	case "backspace":
		if m.query != "" {
			runes := []rune(m.query)
			m.query = string(runes[:len(runes)-1])
			m.reload()
		}
	default:
		if k := event.String(); len(k) == 1 && k[0] >= 0x20 && k[0] < 0x7f {
			m.query += k
			m.reload()
		}
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
	q := strings.ToLower(m.query)
	for i, req := range reqs {
		if q != "" && !strings.Contains(strings.ToLower(req.Title), q) &&
			!strings.Contains(strings.ToLower(req.ID), q) {
			continue
		}
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
	board := m.boardView()
	if m.helpOpen {
		return m.overlayHelp(board)
	}
	return board
}

// helpEntry is one line of the key-binding overlay: keys on the left,
// description on the right.
type helpEntry struct {
	Keys string
	Desc string
}

// renderHelpBody lays out the reqtui key table in two columns when the
// terminal is wide enough, mirroring the board TUI's help overlay.
func (m *Model) renderHelpBody(p palette) string {
	groups := [][]helpEntry{
		{
			{"j k / ↑ ↓", m.t("reqtui.h_move")},
			{"h l / ← →", m.t("reqtui.h_column")},
			{"Enter", m.t("reqtui.h_detail")},
			{"/", m.t("reqtui.h_search")},
			{"n", m.t("reqtui.h_new")},
			{"d", m.t("reqtui.h_decompose")},
			{"w", m.t("reqtui.h_switch")},
		},
		{
			{"c", m.t("reqtui.h_complete")},
			{"a", m.t("reqtui.h_archive")},
			{"r", m.t("reqtui.h_reload")},
			{"?", m.t("reqtui.h_help")},
			{"q", m.t("reqtui.h_quit")},
			{m.t("reqtui.mouse"), m.t("reqtui.h_mouse")},
		},
	}
	render := func(entries []helpEntry) string {
		keyWidth := 0
		for _, e := range entries {
			if w := displayWidth(e.Keys); w > keyWidth {
				keyWidth = w
			}
		}
		lines := make([]string, 0, len(entries))
		for _, e := range entries {
			lines = append(lines,
				p.style("title").Render(padText(e.Keys, keyWidth))+"  "+p.style("dim").Render(e.Desc))
		}
		return strings.Join(lines, "\n")
	}
	left, right := render(groups[0]), render(groups[1])
	wide := lipgloss.JoinHorizontal(lipgloss.Top, left, "    ", right)
	if displayWidth(strings.Split(wide, "\n")[0]) <= m.width-8 {
		return wide
	}
	return left + "\n\n" + right
}

// overlayHelp composes the key-binding popup centered over the board screen.
func (m *Model) overlayHelp(board string) string {
	p := paletteFor(m.theme)
	body := m.renderHelpBody(p)
	lines := strings.Split(body, "\n")
	inner := 0
	for _, line := range lines {
		if w := displayWidth(line); w > inner {
			inner = w
		}
	}
	hint := m.t("reqtui.close_hint")
	if w := displayWidth(hint); w > inner {
		inner = w
	}
	title := m.t("reqtui.help_title")
	if w := displayWidth(title); w > inner {
		inner = w
	}

	// center the popup box on the board area
	boxW, boxH := inner+4, len(lines)+5
	if boxW > m.width-2 {
		boxW = m.width - 2
	}
	if boxH > m.height-2 {
		boxH = m.height - 2
	}
	offX := (m.width - boxW) / 2
	if offX < 0 {
		offX = 0
	}
	offY := (m.height - boxH) / 2
	if offY < 0 {
		offY = 0
	}

	// build the popup content: title, separator, body, hint
	sep := p.style("separator").Render(strings.Repeat("─", inner))
	content := strings.Join([]string{
		p.style("title").Render(padText(title, inner)),
		sep,
		padBlock(body, inner, len(lines), p),
		p.style("dim").Render(padText(hint, inner)),
	}, "\n")

	// frame the popup with a rounded border
	frame := []string{
		p.style("popup-edge").Render(borderTopLeft + strings.Repeat(borderHorizontal, boxW-2) + borderTopRight),
	}
	for _, line := range strings.Split(content, "\n") {
		frame = append(frame, p.style("popup-edge").Render(borderVertical)+padAnsi(line, boxW-2)+p.style("popup-edge").Render(borderVertical))
	}
	frame = append(frame, p.style("popup-edge").Render(borderBottomLeft+strings.Repeat(borderHorizontal, boxW-2)+borderBottomRight))

	// composite the frame onto the board at (offX, offY)
	boardLines := strings.Split(board, "\n")
	for row, popupLine := range frame {
		y := offY + row
		if y >= len(boardLines) {
			break
		}
		boardLines[y] = overlayLine(boardLines[y], popupLine, offX, boxW)
	}
	return strings.Join(boardLines, "\n")
}

// overlayLine replaces width columns of base starting at x with the popup
// line, preserving the rest of the underlying row.
func overlayLine(base, popup string, x, width int) string {
	runes := []rune(base)
	if len(runes) < x {
		// pad the underlying row up to the popup's left edge
		pad := strings.Repeat(" ", x-len(runes))
		runes = append(runes, []rune(pad)...)
	}
	end := x + width
	if end > len(runes) {
		end = len(runes)
	}
	return string(runes[:x]) + popup + string(runes[end:])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

// Double-click window, in sync with the board TUI.
const doubleClickWindow = 500 * time.Millisecond

// bodyTop is the screen row where the column panels start: one title row,
// then one blank row.
const reqBodyTop = 2

// cardLineHeight is the number of lines one collapsed requirement card takes
// in a column: title, metadata, and one blank spacer.
const reqCardLineHeight = 3

// handleMouse responds to terminal mouse events. A left click focuses the
// clicked column; clicking on a card also selects that card, and a second
// click on the same card within the double-click window opens its detail.
// Scrolling moves the cursor within the focused column.
func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.current != viewBoard {
		return m, nil
	}
	if msg.Action == tea.MouseActionPress {
		if msg.Button == tea.MouseButtonWheelUp {
			m.moveCursor(-1)
			return m, nil
		}
		if msg.Button == tea.MouseButtonWheelDown {
			m.moveCursor(1)
			return m, nil
		}
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	x, y := msg.X, msg.Y
	state, ok := columnAt(m.layout, x)
	if !ok {
		return m, nil
	}
	list := m.columns[state]
	focusIndex := indexOf(statusColumns, state)
	if focusIndex >= 0 {
		m.focus = focusIndex
	}
	// Determine whether the click landed on a card body.
	ci := m.cardIndexAt(y)
	if ci >= 0 && ci < len(list) {
		if cur := m.cursor[state]; cur >= 0 && cur < len(list) {
			// A same-position second click is the board TUI's double-click.
			if ci == cur && m.lastClickAt != (time.Time{}) &&
				time.Since(m.lastClickAt) <= doubleClickWindow &&
				x == m.lastClickX && y == m.lastClickY {
				m.lastClickAt = time.Time{}
				m.openDetail()
				return m, nil
			}
		}
		m.cursor[state] = ci
		m.lastClickX, m.lastClickY, m.lastClickAt = x, y, time.Now()
	}
	return m, nil
}

// cardIndexAt maps a screen row to the collapsed-card index in the clicked
// column, or -1 when the row is outside any card body (column header, blank
// spacer after the last card, or bottom border).
func (m *Model) cardIndexAt(y int) int {
	if y < reqBodyTop {
		return -1
	}
	colRow := y - reqBodyTop // 0 = panel top border, 1 = first content row
	if colRow < 1 {
		return -1
	}
	return (colRow - 1) / reqCardLineHeight
}

func (m *Model) boardView() string {
	p := paletteFor(m.theme)
	// header row: title + search box on the left, generation hint removed (the
	// pool has no generated-at stamp). Width math runs on plain text first so
	// ANSI codes do not skew the visible widths, then the line is colorized.
	left := " " + m.t("reqtui.title") + "  " + m.t("reqtui.search") + ": " + m.query
	if m.searching {
		left += "▏"
	}
	leftWidth := displayWidth(left)
	right := m.t("reqtui.status_help")
	rightWidth := displayWidth(right)
	if leftWidth+rightWidth > m.width {
		right = clipText(right, max(0, m.width-leftWidth))
		rightWidth = displayWidth(right)
	}
	gap := m.width - leftWidth - rightWidth
	if gap < 0 {
		gap = 0
	}
	titleLine := p.style("title").Render(clipText(left+strings.Repeat(" ", gap)+right, m.width))

	// column panels occupy everything between the title row, the blank row and
	// the bottom status bar.
	m.layout = layoutColumns(m.width, len(statusColumns))
	columnHeight := max(1, m.height-3)
	blocks := make([]string, 0, len(m.layout)*2)
	for i, col := range m.layout {
		focused := col.State == statusColumns[m.focus]
		blocks = append(blocks, m.renderColumnPanel(p, col, columnHeight, focused))
		if i < len(m.layout)-1 {
			blocks = append(blocks, " ")
		}
	}
	board := lipgloss.JoinHorizontal(lipgloss.Top, blocks...)

	// bottom status bar carries a transient notice when set, otherwise the
	// segmented summary on the left and the quick help on the right.
	return lipgloss.JoinVertical(lipgloss.Left,
		titleLine,
		p.fillLine(m.width),
		padBlock(board, m.width, columnHeight, p),
		m.renderStatusBar(p, m.width),
	)
}

// padBlock pads a rendered line block to exactly width x height using blank
// fill lines so every column panel reaches the same bottom edge.
func padBlock(block string, width, height int, p palette) string {
	lines := strings.Split(block, "\n")
	for len(lines) < height {
		lines = append(lines, p.fillLine(width))
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, padAnsi(line, width))
	}
	return strings.Join(out, "\n")
}

// renderStatusBar draws the single bottom line: the transient notice
// (problems / errors / status messages) or, when none is set, the column and
// card counts on the left and the ?-help / quit hint on the right.
func (m *Model) renderStatusBar(p palette, w int) string {
	if notice := m.transientNotice(); notice != "" {
		tag := "footer"
		if m.err != "" || len(m.problems) > 0 {
			tag = "warn"
		}
		return p.style(tag).Render(padAnsi(" "+clipText(notice, max(0, w-1)), w))
	}
	segments := []string{
		m.t("reqtui.pool_unit"),
		itoa(m.visibleCount()) + " " + m.t("reqtui.card_unit"),
		itoa(m.countStatus(board.ReqStatusDraft)) + "/" +
			itoa(m.countStatus(board.ReqStatusDecomposed)) + "/" +
			itoa(m.countStatus(board.ReqStatusCompleted)) + " " + m.t("reqtui.progress_unit"),
	}
	left := " " + strings.Join(segments, "  "+p.style("separator").Render(m.t("reqtui.vbar"))+"  ")
	if m.searching {
		left += "  " + m.t("reqtui.searching")
	}
	right := m.t("reqtui.status_help") + " "
	innerWidth := displayWidth(left) + displayWidth(right)
	if w-1 <= innerWidth {
		return p.style("footer").Render(padAnsi(clipText(left, max(0, w-1)), w))
	}
	gap := w - innerWidth
	return p.style("footer").Render(padAnsi(left+strings.Repeat(" ", gap)+right, w))
}

// transientNotice returns the current bottom-bar message, or "" when the
// summary segments should be shown instead.
func (m *Model) transientNotice() string {
	if len(m.problems) > 0 {
		return strings.Join(m.problems, " · ")
	}
	if m.err != "" {
		return m.err
	}
	if m.statusMessage != "" {
		return m.statusMessage
	}
	return ""
}

func (m *Model) visibleCount() int {
	n := 0
	for _, list := range m.columns {
		n += len(list)
	}
	return n
}

func (m *Model) countStatus(status string) int {
	return len(m.columns[status])
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
		"STATUS: " + p.style("heading-"+req.Status).Render(m.columnLabel(req.Status)),
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
	if len(req.Attach) > 0 {
		meta = append(meta, m.t("reqtui.attach_label")+":\n  "+strings.Join(req.Attach, "\n  "))
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
