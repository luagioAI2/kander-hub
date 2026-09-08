// Package reqtui is a small terminal board dedicated to the kander requirements
// pool. It is intentionally separate from internal/tui so that kander-hub can
// keep its upstream kander TUI untouched and avoid lock-step merge conflicts.
//
// Rendering is direct ANSI: one requirement per row, an Enter key opens a
// detail page that shows the requirement body plus its linked tasks. The
// package depends only on the i18n package; storage access flows in through
// closures so there is no cycle with internal/board.
package reqtui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// LoadSummary returns the rows shown on the list page; LoadDetail returns the
// data shown on the detail page. Both are wired by the caller so this package
// never imports internal/board.
type (
	LoadSummary func() ([]RequirementSummary, error)
	LoadDetail  func(id string) (RequirementDetail, error)
)

// Config tunes the rendering of the requirements board.
type Config struct {
	Lang         string
	Refresh      time.Duration
	Width        int
	Height       int
	Stdin        io.Reader
	Stdout       io.Writer
	LoadSummary  LoadSummary
	LoadDetail   LoadDetail
}

// Run starts the requirement TUI. It returns nil on a normal exit (q or Esc).
func Run(cfg Config) error {
	if cfg.Stdin == nil {
		cfg.Stdin = io.Reader(nil)
	}
	if cfg.Stdout == nil {
		cfg.Stdout = io.Writer(nil)
	}
	if cfg.Refresh <= 0 {
		cfg.Refresh = 2 * time.Second
	}
	if cfg.Lang == "" {
		cfg.Lang = "cn"
	}
	if cfg.LoadSummary == nil || cfg.LoadDetail == nil {
		return errors.New("reqtui: LoadSummary and LoadDetail are required")
	}
	if w, h, err := terminalSize(cfg.Stdout); err == nil {
		if cfg.Width <= 0 {
			cfg.Width = w
		}
		if cfg.Height <= 0 {
			cfg.Height = h
		}
	}
	if cfg.Width <= 0 {
		cfg.Width = 100
	}
	if cfg.Height <= 0 {
		cfg.Height = 30
	}

	for {
		action, err := runListPage(cfg)
		if err != nil {
			return err
		}
		switch action.kind {
		case actionQuit:
			return nil
		case actionOpen:
			if err := runDetailPage(action.requirementID, cfg); err != nil {
				return err
			}
		}
	}
}

type actionKind int

const (
	actionNone actionKind = iota
	actionQuit
	actionOpen
)

type action struct {
	kind          actionKind
	requirementID string
}

func runListPage(cfg Config) (action, error) {
	if cfg.Stdin == nil {
		return action{}, errors.New("reqtui: no stdin reader")
	}
	reader := bufio.NewReader(cfg.Stdin)
	selected := 0
	scroll := 0
	for {
		reqs, err := cfg.LoadSummary()
		if err != nil {
			return action{}, err
		}
		view := renderListView(reqs, selected, scroll, cfg)
		if _, err := io.WriteString(cfg.Stdout, view); err != nil {
			return action{}, err
		}
		key, err := readKey(reader, cfg.Refresh)
		if err != nil {
			if errors.Is(err, errRefresh) {
				continue
			}
			return action{}, err
		}
		switch key {
		case "q", "Q", "ctrl-c":
			return action{kind: actionQuit}, nil
		case "esc":
			return action{kind: actionQuit}, nil
		case "j", "down":
			if len(reqs) > 0 {
				selected = clamp(selected+1, 0, len(reqs)-1)
			}
		case "k", "up":
			if len(reqs) > 0 {
				selected = clamp(selected-1, 0, len(reqs)-1)
			}
		case "g":
			selected = 0
		case "G":
			if len(reqs) > 0 {
				selected = len(reqs) - 1
			}
		case "enter", "l", "right":
			if len(reqs) > 0 {
				return action{kind: actionOpen, requirementID: reqs[selected].ID}, nil
			}
		case "n":
			fmt.Fprintln(cfg.Stdout, text(cfg.Lang, "reqtui.use_kander_req_new"))
		}
		if selected < scroll {
			scroll = selected
		}
		if selected >= scroll+visibleRows(cfg) {
			scroll = selected - visibleRows(cfg) + 1
		}
	}
}

func runDetailPage(reqID string, cfg Config) error {
	if cfg.Stdin == nil {
		return errors.New("reqtui: no stdin reader")
	}
	reader := bufio.NewReader(cfg.Stdin)
	scroll := 0
	for {
		summary, err := cfg.LoadDetail(reqID)
		if err != nil {
			return err
		}
		view := renderDetailView(summary, scroll, cfg)
		if _, err := io.WriteString(cfg.Stdout, view); err != nil {
			return err
		}
		key, err := readKey(reader, cfg.Refresh)
		if err != nil {
			if errors.Is(err, errRefresh) {
				continue
			}
			return err
		}
		lines := strings.Split(view, "\n")
		maxScroll := len(lines) - cfg.Height
		if maxScroll < 0 {
			maxScroll = 0
		}
		switch key {
		case "q", "Q", "esc", "ctrl-c", "h", "left":
			return nil
		case "j", "down":
			if scroll < maxScroll {
				scroll++
			}
		case "k", "up":
			if scroll > 0 {
				scroll--
			}
		case "pgdn", " ":
			scroll = clamp(scroll+visibleRows(cfg), 0, maxScroll)
		case "pgup":
			scroll = clamp(scroll-visibleRows(cfg), 0, maxScroll)
		}
	}
}

func clamp(value, lo, hi int) int {
	if value < lo {
		return lo
	}
	if value > hi {
		return hi
	}
	return value
}

func visibleRows(cfg Config) int {
	rows := cfg.Height - 4 // header + footer + margins
	if rows < 1 {
		rows = 1
	}
	return rows
}
