package reqtui

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRenderListViewEmpty(t *testing.T) {
	cfg := Config{Lang: "en", Width: 80, Height: 24, Refresh: time.Second}
	view := renderListView(nil, 0, 0, cfg)
	if !strings.Contains(view, "(no requirement cards)") {
		t.Fatalf("empty state missing: %q", view)
	}
}

func TestRenderListViewRows(t *testing.T) {
	reqs := []RequirementSummary{
		{ID: "20260908-login-req", Title: "Fix login bug", Status: "draft", Source: "pool://42"},
		{ID: "20260908-docs-req", Title: "Write docs", Status: "decomposed", Source: "pool://43", Done: 2, Total: 3},
	}
	cfg := Config{Lang: "en", Width: 80, Height: 24, Refresh: time.Second}
	view := renderListView(reqs, 0, 0, cfg)
	if !strings.Contains(view, "Fix login bug") || !strings.Contains(view, "Write docs") {
		t.Fatalf("rows missing: %q", view)
	}
	if !strings.Contains(view, "2/3") {
		t.Fatalf("progress missing: %q", view)
	}
	if !strings.Contains(view, "> ") {
		t.Fatalf("selection marker missing: %q", view)
	}
}

func TestRenderDetailViewLinks(t *testing.T) {
	req := RequirementDetail{
		ID:      "20260908-login-req",
		Title:   "Fix login bug",
		Status:  "decomposed",
		Source:  "pool://42",
		Done:    1,
		Total:   2,
		Linked: []LinkedRef{
			{ID: "20260908-a-task", Title: "Alpha", State: "done"},
			{ID: "20260908-b-task", Title: "Beta", State: "working"},
			{ID: "20260908-x-task", Missing: true},
		},
	}
	cfg := Config{Lang: "en", Width: 100, Height: 24, Refresh: time.Second}
	view := renderDetailView(req, 0, cfg)
	if !strings.Contains(view, "- [x] 20260908-a-task") {
		t.Fatalf("done checkbox missing: %q", view)
	}
	if !strings.Contains(view, "- [ ] 20260908-b-task") {
		t.Fatalf("open checkbox missing: %q", view)
	}
	if !strings.Contains(view, "missing") {
		t.Fatalf("missing marker missing: %q", view)
	}
}

func TestRenderDetailViewNoLinks(t *testing.T) {
	req := RequirementDetail{ID: "20260908-x-req", Title: "X", Status: "draft"}
	cfg := Config{Lang: "en", Width: 100, Height: 24, Refresh: time.Second}
	view := renderDetailView(req, 0, cfg)
	if !strings.Contains(view, "(no linked tasks)") {
		t.Fatalf("no-links state missing: %q", view)
	}
}

func TestRunRejectsMissingLoaders(t *testing.T) {
	err := Run(Config{Lang: "en", Width: 80, Height: 24, Refresh: time.Second, Stdin: strings.NewReader("q\n"), Stdout: &strings.Builder{}})
	if err == nil || !strings.Contains(err.Error(), "LoadSummary") {
		t.Fatalf("expected loader error, got %v", err)
	}
}

func TestRunListQuitImmediately(t *testing.T) {
	var out strings.Builder
	called := 0
	cfg := Config{
		Lang: "en", Width: 80, Height: 24, Refresh: time.Second,
		Stdin:  strings.NewReader("q\n"),
		Stdout: &out,
		LoadSummary: func() ([]RequirementSummary, error) {
			called++
			return []RequirementSummary{{ID: "20260908-x-req", Title: "X", Status: "draft"}}, nil
		},
		LoadDetail: func(string) (RequirementDetail, error) { return RequirementDetail{}, os.ErrNotExist },
	}
	if err := Run(cfg); err != nil {
		t.Fatal(err)
	}
	if called == 0 {
		t.Fatal("LoadSummary never called")
	}
	if !strings.Contains(out.String(), "X") {
		t.Fatalf("output missing rows: %q", out.String())
	}
}

func TestDecodeKeyBasic(t *testing.T) {
	cases := map[string][]byte{
		"q":      {'q'},
		"esc":    {0x1b},
		"enter":  {0x0d},
		"up":     {0x1b, '[', 'A'},
		"down":   {0x1b, '[', 'B'},
		"pgup":   {0x1b, '[', '5', '~'},
		"ctrl-c": {0x03},
	}
	for want, buf := range cases {
		if got := decodeKey(buf); got != want {
			t.Fatalf("decodeKey(%v) = %q, want %q", buf, got, want)
		}
	}
}

func TestDisplayWidthAndClip(t *testing.T) {
	if got := displayWidth("ab"); got != 2 {
		t.Fatalf("ascii width = %d", got)
	}
	if got := displayWidth("中文"); got != 4 {
		t.Fatalf("cjk width = %d", got)
	}
	if got := clipText("hello", 4); got != "hel…" {
		t.Fatalf("clip = %q", got)
	}
}
