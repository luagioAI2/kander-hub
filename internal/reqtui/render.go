package reqtui

import (
	"fmt"
	"strings"
)

// RequirementSummary is the digest rendered for one requirement card.
type RequirementSummary struct {
	ID     string
	Title  string
	Status string
	Source string
	Done   int
	Total  int
}

// RequirementDetail bundles the card body plus the live linked task refs.
type RequirementDetail struct {
	ID       string
	Title    string
	Status   string
	Source   string
	Done     int
	Total    int
	Document string
	Linked   []LinkedRef
}

// LinkedRef describes one linked task; Missing means the linked ID has been
// removed from the board.
type LinkedRef struct {
	ID      string
	Title   string
	State   string
	Missing bool
}

// renderListView draws one screen of the requirement list. Each requirement
// takes one row so a 30-row terminal already shows ~26 requirements; the
// footer reminds the user how to operate the view.
func renderListView(reqs []RequirementSummary, selected, scroll int, cfg Config) string {
	width := cfg.Width
	if width < 40 {
		width = 40
	}
	height := visibleRows(cfg)
	var out strings.Builder
	header := text(cfg.Lang, "reqtui.title")
	stamp := timeNowString()
	right := text(cfg.Lang, "reqtui.updated", stamp)
	left := header
	leftWidth := displayWidth(left)
	rightWidth := displayWidth(right)
	if leftWidth+rightWidth+2 > width {
		width = leftWidth + rightWidth + 2
	}
	gap := width - leftWidth - rightWidth
	if gap < 0 {
		gap = 0
	}
	out.WriteString("\x1b[1m")
	out.WriteString(padLine(left+strings.Repeat(" ", gap)+right, width))
	out.WriteString("\x1b[0m")
	out.WriteString("\r\n")
	if len(reqs) == 0 {
		out.WriteString(text(cfg.Lang, "reqtui.empty"))
		out.WriteString("\r\n\r\n")
	} else {
		end := scroll + height
		if end > len(reqs) {
			end = len(reqs)
		}
		for i := scroll; i < end; i++ {
			out.WriteString(renderRow(reqs[i], i == selected, width, cfg))
			out.WriteString("\r\n")
		}
	}
	out.WriteString("\r\n")
	footer := text(cfg.Lang, "reqtui.footer")
	out.WriteString("\x1b[2m")
	out.WriteString(padLine(footer, width))
	out.WriteString("\x1b[0m")
	out.WriteString("\r\n")
	return out.String()
}

func renderRow(row RequirementSummary, selected bool, width int, cfg Config) string {
	progress := "-"
	if row.Total > 0 {
		progress = fmt.Sprintf("%d/%d", row.Done, row.Total)
	}
	statusColor := statusColor(row.Status)
	title := row.Title
	if title == "" {
		title = row.ID
	}
	prefix := "  "
	if selected {
		prefix = "> "
	}
	titleClip := clipText(title, width-22)
	line := fmt.Sprintf("%s%-30s %s %-12s %-6s",
		prefix,
		clipText(row.ID, 30),
		statusColor(clipText(row.Status, 12)),
		titleClip,
		progress,
	)
	if selected {
		return "\x1b[7m" + padLine(line, width) + "\x1b[0m"
	}
	return padLine(line, width)
}

// renderDetailView assembles the requirement detail page: the requirement card
// body, then a linked-tasks table that mirrors `kander req show`. Scrolling is
// done in the caller before the view is emitted.
func renderDetailView(req RequirementDetail, scroll int, cfg Config) string {
	var out strings.Builder
	width := cfg.Width
	if width < 40 {
		width = 40
	}
	title := req.Title
	if title == "" {
		title = req.ID
	}
	header := text(cfg.Lang, "reqtui.detail_title", title)
	out.WriteString("\x1b[1m")
	out.WriteString(padLine(header, width))
	out.WriteString("\x1b[0m")
	out.WriteString("\r\n")
	status := req.Status
	if status == "" {
		status = "draft"
	}
	progress := "-"
	if req.Total > 0 {
		progress = fmt.Sprintf("%d/%d", req.Done, req.Total)
	}
	meta := fmt.Sprintf("%s  %s  %s  %s",
		req.ID, status, progress, req.Source,
	)
	out.WriteString(padLine(meta, width))
	out.WriteString("\r\n")
	out.WriteString(strings.Repeat("─", width))
	out.WriteString("\r\n")
	if req.Document != "" {
		out.WriteString(req.Document)
	}
	if !outAlreadyLinkedSection(req.Document) {
		out.WriteString("\r\n## Linked Tasks\r\n\r\n")
	}
	if len(req.Linked) == 0 {
		out.WriteString(text(cfg.Lang, "reqtui.detail_no_links"))
		out.WriteString("\r\n")
	} else {
		for _, link := range req.Linked {
			checkbox := "[ ]"
			if !link.Missing && link.State == "done" {
				checkbox = "[x]"
			}
			state := link.State
			if state == "" {
				state = "-"
			}
			if link.Missing {
				state = "missing"
			}
			line := fmt.Sprintf("- %s %s  %s  %s",
				checkbox,
				clipText(link.ID, 30),
				clipText(state, 10),
				clipText(link.Title, 40),
			)
			out.WriteString(padLine(line, width))
			out.WriteString("\r\n")
		}
	}
	out.WriteString("\r\n")
	footer := text(cfg.Lang, "reqtui.detail_footer")
	out.WriteString("\x1b[2m")
	out.WriteString(padLine(footer, width))
	out.WriteString("\x1b[0m")
	out.WriteString("\r\n")
	return out.String()
}

func outAlreadyLinkedSection(doc string) bool {
	return strings.Contains(doc, "## Linked Tasks")
}

// padLine returns s padded with spaces so the display width matches width.
func padLine(s string, width int) string {
	w := displayWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// clipText shortens s so its display width is at most width, appending "…" when
// it had to be truncated.
func clipText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return truncate(s, width-1) + "…"
}

func truncate(s string, width int) string {
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := runewidth(r)
		if w+rw > width {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String()
}

// displayWidth counts visible columns in a string; ASCII counts as one column,
// CJK and other full-width runes count as two.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runewidth(r)
	}
	return w
}

// runewidth returns the visible columns of r.
func runewidth(r rune) int {
	if r < 0x1100 {
		return 1
	}
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0x9FFF, // CJK Radicals, Unified Ideographs
		r >= 0xA000 && r <= 0xA4CF, // Yi
		r >= 0xAC00 && r <= 0xD7A3, // Hangul Syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK Compatibility Ideographs
		r >= 0xFE30 && r <= 0xFE4F, // CJK Compatibility Forms
		r >= 0xFF00 && r <= 0xFF60, // Fullwidth Forms
		r >= 0xFFE0 && r <= 0xFFE6: // Fullwidth signs
		return 2
	}
	return 1
}

// statusColor returns a small color wrapper function for the requirement
// status so the list row hints at progress without taking much room.
func statusColor(status string) func(string) string {
	switch status {
	case "completed":
		return func(s string) string { return "\x1b[32m" + s + "\x1b[0m" }
	case "decomposed":
		return func(s string) string { return "\x1b[36m" + s + "\x1b[0m" }
	case "archived":
		return func(s string) string { return "\x1b[2m" + s + "\x1b[0m" }
	}
	return func(s string) string { return "\x1b[33m" + s + "\x1b[0m" }
}
