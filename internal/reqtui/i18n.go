package reqtui

import "github.com/dualface/kander/internal/i18n"

// text is a thin wrapper around i18n.Text so callers do not need to import i18n.
func text(lang, id string, args ...any) string {
	return i18n.Text(lang, id, args...)
}
