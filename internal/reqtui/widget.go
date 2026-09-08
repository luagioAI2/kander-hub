package reqtui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Rounded panel border characters, matching the board TUI's column panels.
const (
	borderTopLeft     = "╭"
	borderTopRight    = "╮"
	borderBottomLeft  = "╰"
	borderBottomRight = "╯"
	borderHorizontal  = "─"
	borderVertical    = "│"
)

// panelChrome is the width each side of a column panel uses for border plus
// padding, exactly like internal/tui.
const panelChrome = 4

// columnGeometry is the x/width of one rendered column, reused for mouse
// hit-testing.
type columnGeometry struct {
	State string
	X     int
	Width int
}

// layoutColumns divides the available width into equal columns with a vertical
// separator column between them.
func layoutColumns(width, count int) []columnGeometry {
	if count < 1 {
		count = 1
	}
	seps := count - 1
	inner := width - seps
	if inner < count {
		inner = count
	}
	colWidth := inner / count
	rem := inner - colWidth*count
	out := make([]columnGeometry, 0, count)
	x := 0
	for i := 0; i < count; i++ {
		w := colWidth
		if rem > 0 {
			w++
			rem--
		}
		out = append(out, columnGeometry{State: statusColumns[i], X: x, Width: w})
		x += w + 1 // leave one column for the separator
	}
	return out
}

func columnAt(layout []columnGeometry, x int) (string, bool) {
	for _, col := range layout {
		if x >= col.X && x < col.X+col.Width {
			return col.State, true
		}
	}
	return "", false
}

// panelTop renders the top border with an embedded title and count badge:
//   ╭─ 草稿 3 ─────────╮
func panelTop(p palette, state, label, badge string, width int, focused bool) string {
	border := panelBorder(p, state, focused)
	if width < 6 {
		return border.Render(strings.Repeat(borderHorizontal, max(0, width)))
	}
	head := borderTopLeft + borderHorizontal
	tail := borderHorizontal + borderTopRight
	title := " " + label + " "
	chip := ""
	if badge != "" {
		title = " " + label
		chip = " " + badge + " "
	}
	used := displayWidth(title) + displayWidth(chip)
	fill := width - displayWidth(head) - displayWidth(tail) - used
	if fill < 0 {
		title = " " + clipText(label, max(1, width-8))
		if chip == "" {
			title += " "
		}
		used = displayWidth(title) + displayWidth(chip)
		fill = max(0, width-displayWidth(head)-displayWidth(tail)-used)
	}
	rendered := p.style("heading-" + state).Render(title)
	if chip != "" {
		rendered += p.style("badge-" + state).Render(chip)
	}
	return border.Render(head) + rendered +
		border.Render(strings.Repeat(borderHorizontal, fill)) +
		border.Render(tail)
}

func panelBottom(p palette, state string, width int, focused bool) string {
	border := panelBorder(p, state, focused)
	if width < 2 {
		return border.Render(strings.Repeat(borderHorizontal, max(0, width)))
	}
	return border.Render(borderBottomLeft + strings.Repeat(borderHorizontal, width-2) + borderBottomRight)
}

func panelRow(p palette, state, content string, width int, focused bool) string {
	border := panelBorder(p, state, focused)
	inner := width - 2
	if inner < 0 {
		inner = 0
	}
	return border.Render(borderVertical) + padText(content, inner) + border.Render(borderVertical)
}

func centerText(text string, width int) string {
	pad := (width - displayWidth(text)) / 2
	if pad < 0 {
		pad = 0
	}
	return padText(strings.Repeat(" ", pad)+text, width)
}

// panelBorder chooses the color of the panel edge: the focused column uses its
// own state color, the others a low-contrast separator.
func panelBorder(p palette, state string, focused bool) lipgloss.Style {
	if focused {
		return p.ink(p.stateColor(state))
	}
	return p.style("separator")
}

// reqCardStyle colors one line of a requirement card. The title line takes the
// column color; following lines are metadata in the base color.
func reqCardStyle(p palette, state string, line int, selected bool) lipgloss.Style {
	color := p.stateColor(state)
	if selected {
		style := p.ink(color).Reverse(true)
		if line == 0 {
			return style.Bold(true)
		}
		return style
	}
	if line == 0 {
		return p.ink(color)
	}
	return p.ink(p.Base)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
