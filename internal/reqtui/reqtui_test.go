package reqtui

import (
	"strings"
	"testing"
)

func TestBoardViewRendersEmptyColumns(t *testing.T) {
	m := New(t.TempDir())
	view := m.boardView()
	if view == "" {
		t.Fatal("expected a non-empty board view")
	}
	for _, status := range statusColumns {
		label := m.columnLabel(status)
		if !strings.Contains(view, label+" (0)") {
			t.Fatalf("expected column header %q in view:\n%s", label, view)
		}
	}
}

func TestMoveFocusWraps(t *testing.T) {
	m := &Model{
		columns: make(map[string][]int),
		cursor:  make(map[string]int),
		current: viewBoard,
	}
	m.moveFocus(1)
	if m.focus != 1 {
		t.Fatalf("expected focus 1, got %d", m.focus)
	}
	m.moveFocus(1)
	if m.focus != 2 {
		t.Fatalf("expected focus 2, got %d", m.focus)
	}
	m.moveFocus(1)
	if m.focus != 0 {
		t.Fatalf("expected wrap to 0, got %d", m.focus)
	}
	m.moveFocus(-1)
	if m.focus != 2 {
		t.Fatalf("expected back wrap to 2, got %d", m.focus)
	}
}

func TestRenderMarkdownStripsMarkers(t *testing.T) {
	md := "# Title\n\n## Section\n\n- a\n- b\n\nDone **bold**."
	out := renderMarkdown(md)
	if strings.Contains(out, "**") {
		t.Fatalf("expected inline markers stripped, got:\n%s", out)
	}
	if !strings.Contains(out, "• a") {
		t.Fatalf("expected bullet rendering, got:\n%s", out)
	}
}
