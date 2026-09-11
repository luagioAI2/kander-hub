package usage

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/mattn/go-runewidth"
)

func t(id string, args ...any) string {
	return config.Text(id, args...)
}

// Format renders the report as a table plus the notes that explain absent numbers.
func Format(report Report) string {
	var out strings.Builder
	out.WriteString(scopeLine(report))
	out.WriteByte('\n')

	if len(report.Sessions) == 0 {
		out.WriteString(t("usage.no_usage_records"))
		out.WriteByte('\n')
		out.WriteString(sourceNotes(report))
		return out.String()
	}

	headings := []string{
		t("usage.col_agent"),
		t("usage.col_session"),
		t("usage.col_model"),
		t("usage.col_cache_read"),
		t("usage.col_input"),
		t("usage.col_output"),
		t("usage.col_total"),
		t("usage.col_cached"),
		t("usage.col_duration"),
	}
	english := []string{
		"agent", "session", "model", "cache-read", "input", "output", "total", "cached", "duration",
	}

	rows := make([][]string, 0, len(report.Sessions)+1)
	for _, session := range report.Sessions {
		counts := session.Counts
		rows = append(rows, []string{
			session.Agent,
			session.ID,
			emptyDash(session.Model),
			group(counts.CacheRead),
			group(counts.Input),
			group(counts.Output),
			group(counts.Total()),
			percent(counts.CachedPercent()),
			duration(session.Duration()),
		})
	}
	totals := report.Totals
	rows = append(rows, []string{
		t("usage.totals"), "", "",
		group(totals.CacheRead), group(totals.Input), group(totals.Output),
		group(totals.Total()), percent(totals.CachedPercent()), "",
	})

	widths := make([]int, len(headings))
	for i := range headings {
		widths[i] = runewidth.StringWidth(headings[i])
		if w := runewidth.StringWidth(english[i]); w > widths[i] {
			widths[i] = w
		}
		for _, row := range rows {
			if w := runewidth.StringWidth(row[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	out.WriteString(joinRow(headings, widths))
	out.WriteByte('\n')
	separators := make([]string, len(widths))
	for i, w := range widths {
		separators[i] = strings.Repeat("-", w)
	}
	out.WriteString(joinRow(separators, widths))
	out.WriteByte('\n')
	for _, row := range rows {
		out.WriteString(joinRow(row, widths))
		out.WriteByte('\n')
	}
	out.WriteString(sourceNotes(report))
	return out.String()
}

func joinRow(cells []string, widths []int) string {
	parts := make([]string, len(cells))
	for i, cell := range cells {
		pad := widths[i] - runewidth.StringWidth(cell)
		if pad < 0 {
			pad = 0
		}
		parts[i] = cell + strings.Repeat(" ", pad)
	}
	return strings.TrimRight(strings.Join(parts, "  "), " ")
}

func scopeLine(report Report) string {
	var scope string
	switch report.Scope {
	case "task":
		scope = t("usage.scope_task", report.ScopeArg)
	case "project":
		scope = t("usage.scope_project", report.ScopeArg)
	default:
		scope = t("usage.scope_all")
	}
	return scope + "  " + t("usage.window_days", report.Days)
}

// sourceNotes explains every source, including the ones that produced nothing. It
// also states the two counting rules that make the numbers readable.
func sourceNotes(report Report) string {
	var out strings.Builder
	for _, status := range report.Sources {
		if status.Note != "" {
			// A source diagnostic is quoted verbatim: it names the store that was read
			// and why it held nothing, which must not be paraphrased away.
			out.WriteString(status.Agent + ": " + status.Note)
		} else {
			out.WriteString(t("usage.source_summary", status.Agent, status.Found, status.WithData, status.Root))
		}
		out.WriteByte('\n')
	}
	if len(report.Unsupported) > 0 {
		out.WriteString(t("usage.unsupported_agents", strings.Join(report.Unsupported, ", ")))
		out.WriteByte('\n')
	}
	out.WriteString(t("usage.note_cache_read"))
	out.WriteByte('\n')
	out.WriteString(t("usage.note_single_source"))
	out.WriteByte('\n')
	return out.String()
}

// FormatJSON renders the report as machine-readable JSON.
func FormatJSON(report Report) (string, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// group adds thousands separators so large token counts stay readable.
func group(value int64) string {
	digits := strconv.FormatInt(value, 10)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var parts []string
	for len(digits) > 3 {
		parts = append([]string{digits[len(digits)-3:]}, parts...)
		digits = digits[:len(digits)-3]
	}
	parts = append([]string{digits}, parts...)
	return sign + strings.Join(parts, ",")
}

func percent(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64) + "%"
}

// duration renders a span compactly: 45s, 12m30s, 3h12m, 2d4h.
func duration(span time.Duration) string {
	if span <= 0 {
		return "-"
	}
	seconds := int64(span.Seconds())
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	remain := seconds % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm%ds", minutes, remain)
	default:
		return fmt.Sprintf("%ds", remain)
	}
}
