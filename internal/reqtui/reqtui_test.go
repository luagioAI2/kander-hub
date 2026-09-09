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
		if !strings.Contains(view, label) {
			t.Fatalf("expected column label %q in view:\n%s", label, view)
		}
		if !strings.Contains(view, "0") {
			t.Fatalf("expected zero badge in column %q:\n%s", label, view)
		}
	}
}

func TestMoveFocusWraps(t *testing.T) {
	m := &Model{
		columns: make(map[string][]int),
		cursor:  make(map[string]int),
		current: viewBoard,
	}
	m.focus = 0
	m.moveFocus(-1)
	if m.focus != len(statusColumns)-1 {
		t.Fatalf("focus = %d, want wrap to last", m.focus)
	}
	m.moveFocus(1)
	if m.focus != 0 {
		t.Fatalf("focus = %d, want wrap to 0", m.focus)
	}
}

func TestSplitAttachInput(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{" a ", []string{"a"}},
		{"a,b;c", []string{"a", "b", "c"}},
		{"a\nb", []string{"a", "b"}},
		{"a,, b,", []string{"a", "b"}},
	}
	for _, c := range cases {
		got := splitAttachInput(c.in)
		if !equalStrings(got, c.want) {
			t.Fatalf("splitAttachInput(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDecomposeWindow(t *testing.T) {
	cases := []struct {
		in     string
		ok     bool
		sess   string
		window string
	}{
		{"", false, "", ""},
		{"herdr:t1:p1", false, "", ""},
		{"tmux:main:kb-foo:0", true, "main", "kb-foo"},
		{"tmux:main:kb-foo", true, "main", ""},
	}
	for _, c := range cases {
		sess, window, ok := decomposeWindow(c.in)
		if ok != c.ok || sess != c.sess || window != c.window {
			t.Fatalf("decomposeWindow(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, sess, window, ok, c.sess, c.window, c.ok)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
