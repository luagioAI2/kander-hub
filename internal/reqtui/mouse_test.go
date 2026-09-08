package reqtui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dualface/kander/internal/board"
)

func pressAt(x, y int) tea.MouseMsg {
	return tea.MouseMsg{
		X:      x,
		Y:      y,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}
}

func TestMouseClickSwitchesColumn(t *testing.T) {
	m := New(t.TempDir())
	m.width = 110
	m.height = 25
	// pre-render so layout is populated
	m.boardView()
	// The second column starts after the first column's width plus separator.
	m.layout = layoutColumns(m.width, len(statusColumns))
	col2 := m.layout[1]
	// Click on the second column's header row (above any cards).
	m.handleMouse(pressAt(col2.X+5, reqBodyTop))
	if m.focus != 1 {
		t.Fatalf("expected focus on decomposed column, got %d", m.focus)
	}
}

func TestMouseClickSelectsCard(t *testing.T) {
	m := New(t.TempDir())
	_, err := board.AddRequirement(m.root, "x-one", "first", "N/A", "body")
	if err != nil {
		t.Fatal(err)
	}
	m.reload()
	m.width = 110
	m.height = 25
	m.layout = layoutColumns(m.width, len(statusColumns))
	col := m.layout[0]
	// Row of the first card's title line inside column 0.
	y := reqBodyTop + 1
	m.handleMouse(pressAt(col.X+5, y))
	if m.focus != 0 {
		t.Fatalf("expected focus draft, got %d", m.focus)
	}
	if m.cursor[board.ReqStatusDraft] != 0 {
		t.Fatalf("expected cursor on first card, got %d", m.cursor[board.ReqStatusDraft])
	}
}

func TestMouseDoubleClickOpensDetail(t *testing.T) {
	m := New(t.TempDir())
	_, err := board.AddRequirement(m.root, "x-one", "first", "N/A", "body")
	if err != nil {
		t.Fatal(err)
	}
	m.reload()
	m.width = 110
	m.height = 25
	m.layout = layoutColumns(m.width, len(statusColumns))
	col := m.layout[0]
	p := pressAt(col.X+5, reqBodyTop+1)
	m.handleMouse(p) // first click selects
	// force the double-click timestamp to be recent
	m.lastClickX, m.lastClickY, m.lastClickAt = p.X, p.Y, time.Now()
	m.handleMouse(p) // second click within window
	if m.current != viewDetail {
		t.Fatalf("expected detail view after double click, got view %d", m.current)
	}
}

func TestMouseWheelMovesCursor(t *testing.T) {
	m := New(t.TempDir())
	_, err := board.AddRequirement(m.root, "x-a", "a", "N/A", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = board.AddRequirement(m.root, "x-b", "b", "N/A", "")
	if err != nil {
		t.Fatal(err)
	}
	m.reload()
	m.focus = 0
	up := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp}
	down := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
	m.handleMouse(down)
	if m.cursor[board.ReqStatusDraft] != 1 {
		t.Fatalf("expected cursor 1 after wheel down, got %d", m.cursor[board.ReqStatusDraft])
	}
	m.handleMouse(up)
	if m.cursor[board.ReqStatusDraft] != 0 {
		t.Fatalf("expected cursor 0 after wheel up, got %d", m.cursor[board.ReqStatusDraft])
	}
}