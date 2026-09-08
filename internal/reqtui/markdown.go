package reqtui

import (
	"strings"
)

// renderMarkdown is a deliberately small Markdown-to-plain-text renderer for
// requirement SUMMARY bodies. It strips the common structural markers so the
// terminal detail view reads cleanly without pulling a full glamour rendering
// into this package. Full markdown (glamour) is available in the board TUI;
// the requirements view only needs an approximation.
func renderMarkdown(md string) string {
	var b strings.Builder
	for _, raw := range strings.Split(md, "\n") {
		line := strings.TrimRight(raw, " \t")
		if len(strings.TrimSpace(line)) == 0 {
			b.WriteString("\n")
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "### "):
			b.WriteString(columnHeader.Render(strings.TrimSpace(trimmed[4:])) + "\n")
		case strings.HasPrefix(trimmed, "## "):
			b.WriteString(columnHeader.Render(strings.TrimSpace(trimmed[3:])) + "\n")
		case strings.HasPrefix(trimmed, "# "):
			b.WriteString(columnHeader.Render(strings.TrimSpace(trimmed[2:])) + "\n")
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			b.WriteString("  • " + strings.TrimSpace(trimmed[2:]) + "\n")
		case strings.HasPrefix(trimmed, "> "):
			b.WriteString(strings.TrimSpace(trimmed[2:]) + "\n")
		default:
			// Inline emphasis markers are stripped; the text is kept verbatim.
			b.WriteString(stripInline(trimmed) + "\n")
		}
	}
	// Collapse runs of blank lines to a single blank line.
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	var out []string
	blank := false
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// stripInline removes the common inline Markdown emphasis markers (** and *)
// without attempting full syntax handling.
func stripInline(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "`", "")
	return s
}
