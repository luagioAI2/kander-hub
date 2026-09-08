package reqtui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/ansi"

	"github.com/dualface/kander/internal/board"
)

// The reqtui palette deliberately mirrors the board TUI's visual language
// (internal/tui/theme.go) without import it, so this standalone package keeps
// its low-dependency surface. Both themes fill the whole screen background so
// a light theme is not reduced to black text inside a dark terminal.

type palette struct {
	Base      lipgloss.Color
	Bg        lipgloss.Color
	Dim       lipgloss.Color
	Separator lipgloss.Color
	Accent    lipgloss.Color
	Bar       lipgloss.Color
	ChromeFg  lipgloss.Color
	ChromeBg  lipgloss.Color
	PopupEdge lipgloss.Color
	Warn      lipgloss.Color
	OK        lipgloss.Color
	Headings  map[string]lipgloss.Color
}

var reqHeadingColors = map[string]lipgloss.Color{
	board.ReqStatusDraft:      lipgloss.Color("3"), // yellow
	board.ReqStatusDecomposed: lipgloss.Color("4"), // blue
	board.ReqStatusCompleted:  lipgloss.Color("2"), // green
	board.ReqStatusArchived:   lipgloss.Color("5"), // magenta
}

func themeResolve(name string) string {
	if name == "light" || name == "dark" {
		return name
	}
	if lipgloss.HasDarkBackground() {
		return "dark"
	}
	return "light"
}

func paletteFor(themeName string) palette {
	name := themeResolve(themeName)
	base := lipgloss.Color("15")
	bg := lipgloss.Color("0")
	dim := lipgloss.Color("8")
	separator := lipgloss.Color("240")
	if name == "light" {
		base = lipgloss.Color("0")
		bg = lipgloss.Color("15")
		separator = lipgloss.Color("250")
	}
	return palette{
		Base:      base,
		Bg:        bg,
		Dim:       dim,
		Separator: separator,
		Accent:    lipgloss.Color("13"),
		Bar:       lipgloss.Color("4"),
		ChromeFg:  lipgloss.Color("15"),
		ChromeBg:  lipgloss.Color("5"),
		PopupEdge: lipgloss.Color("13"),
		Warn:      lipgloss.Color("1"),
		OK:        lipgloss.Color("2"),
		Headings:  reqHeadingColors,
	}
}

// ink returns a style that colors foreground text while leaving the terminal
// background alone. Hard-coding a background per character (as earlier
// iterations did) serialized a fresh ANSI background sequence on every styled
// span, and on Windows terminals that leaked raw escape codes like "97;40m"
// into the output. Keeping the background transparent avoids that entirely.
func (p palette) ink(color lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(color)
}

// reqStyle maps a render cell tag to a lipgloss style, mirroring the tag set of
// the board TUI so both share the same color semantics.
func (p palette) style(tag string) lipgloss.Style {
	style := p.ink(p.Base)
	switch {
	case tag == "title" || tag == "footer":
		return lipgloss.NewStyle().Foreground(p.ChromeFg).Bold(true)
	case tag == "dim":
		return p.ink(p.Dim)
	case tag == "separator":
		return p.ink(p.Separator)
	case tag == "bold":
		return style.Bold(true)
	case tag == "popup-edge":
		return p.ink(p.PopupEdge)
	case tag == "popup-dim":
		return p.ink(p.Dim)
	case tag == "warn":
		return p.ink(p.Warn)
	case tag == "ok":
		return p.ink(p.OK)
	case strings.HasPrefix(tag, "heading-"):
		state := strings.TrimPrefix(tag, "heading-")
		if color, ok := p.Headings[state]; ok {
			return p.ink(color).Bold(true)
		}
		return style.Bold(true)
	case tag == "bar":
		return p.ink(p.Bar).Bold(true)
	case tag == "selected":
		return p.ink(p.Accent).Reverse(true).Bold(true)
	case tag == "select":
		return p.ink(p.Base).Reverse(true).Bold(true)
	}
	return style
}

func (p palette) stateColor(state string) lipgloss.Color {
	if c, ok := p.Headings[state]; ok {
		return c
	}
	return p.Base
}

// displayWidth returns the terminal width of text, counting wide CJK runes.
// ANSI escape sequences are skipped so a styled span measures as the visible
// glyphs it represents, not its raw bytes.
func displayWidth(text string) int {
	return ansi.PrintableRuneWidth(text)
}

func combining(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Mc, r)
}

func runeDisplayWidth(r rune) int {
	if combining(r) {
		return 0
	}
	w := runewidth.RuneWidth(r)
	if w < 0 {
		return 1
	}
	return w
}

func printableText(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r == '\n' {
			b.WriteRune(r)
			continue
		}
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func clipText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	normalized := printableText(strings.ReplaceAll(strings.ReplaceAll(text, "\t", "    "), "\n", " "))
	if displayWidth(normalized) <= width {
		return normalized
	}
	suffix := "..."
	if width < 4 {
		suffix = strings.Repeat(".", width)
	}
	available := width - displayWidth(suffix)
	var b strings.Builder
	used := 0
	for _, r := range normalized {
		w := runeDisplayWidth(r)
		if used+w > available {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + suffix
}

func padText(text string, width int) string {
	clipped := clipText(text, width)
	pad := width - displayWidth(clipped)
	if pad < 0 {
		pad = 0
	}
	return clipped + strings.Repeat(" ", pad)
}
