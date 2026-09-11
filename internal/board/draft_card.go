// Draft-to-card materialization for the requirements pool. The requirements
// orchestrator writes complete spec.md drafts under the card's
// ## PROPOSED_TASKS section; `kander req convert --from-drafts` turns the
// confirmed ones into real task cards.
//
// A draft supplies contract *content* only. The card envelope — TYPE, SIZE,
// LANGUAGE, CREATED_AT and every required section heading — still comes from
// renderContract, so a draft that omits a section yields a card still holding
// that section's placeholder (which the existing gates reject before todo)
// rather than a card whose structure is silently wrong.
package board

import "strings"

// draftMergeSections lists the contract sections a draft may supply.
//
// The execution sections are deliberately excluded: DISCUSSION carries the
// post-creation SELF_REVIEW/CARD_REVIEW records, and IMPLEMENTATION/SUMMARY are
// the executing agent's delivery records. Letting a draft pre-fill them would
// forge the self-review and completion evidence that the gates exist to check.
var draftMergeSections = []string{
	SectionGoal,
	SectionUserDecisions,
	SectionExpectedOutcome,
	SectionAcceptanceCriteria,
	SectionThreatModel,
	SectionOutOfScope,
}

// utf8BOM is what a Windows editor or a piped file may prefix to a draft body.
// U+FEFF is not Unicode whitespace, so trimming space alone would leave it
// attached to the first line and silently hide both the draft's "# " heading and
// a section heading that starts the body.
const utf8BOM = "\ufeff"

// trimBOM removes a leading byte-order mark.
func trimBOM(text string) string {
	return strings.TrimPrefix(text, utf8BOM)
}

// draftTitle returns the draft's own "# <title>" heading, or "" when it has none
// or still holds the placeholder. Only the heading before the first "## section"
// counts, so a "# comment" line inside the body cannot rename the card.
func draftTitle(draft string) string {
	for _, raw := range strings.Split(draft, "\n") {
		line := strings.TrimSpace(trimBOM(raw))
		if strings.HasPrefix(line, "## ") {
			return ""
		}
		if strings.HasPrefix(line, "# ") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "# "))
			if title == "" || strings.Contains(title, Placeholder) {
				return ""
			}
			return title
		}
	}
	return ""
}

// withDraftTitle replaces the skeleton's leading title line, keeping that line's
// original ending so a CRLF card stays CRLF.
func withDraftTitle(text, title string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		content := strings.TrimRight(line, "\r")
		if strings.HasPrefix(strings.TrimSpace(content), "# ") {
			lines[i] = "# " + title + line[len(content):]
			return strings.Join(lines, "\n")
		}
	}
	return text
}

// draftNewline reports the newline the skeleton uses, so draft text written with
// LF does not leave a mixed-ending card behind.
func draftNewline(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// normalizeNewlines rewrites body to the card's newline style. A card is a new
// file, so normalizing keeps it uniform instead of mixing the skeleton's CRLF
// with the draft author's LF.
func normalizeNewlines(body, newline string) string {
	if newline != "\r\n" || !strings.Contains(body, "\n") {
		return body
	}
	return strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
}

// mergeDraftContract folds a draft's contract sections into a card skeleton,
// reusing SetSection so ordering and newline handling match every other in-place
// section edit. Sections the draft omits keep their skeleton placeholder.
//
// title is the name already validated by the caller, so the card's heading and
// its validated title can never disagree.
func mergeDraftContract(skeleton, draft, title string) (string, error) {
	text := skeleton
	newline := draftNewline(text)
	if title != "" {
		text = withDraftTitle(text, title)
	}
	for _, heading := range draftMergeSections {
		body, ok := SectionBody(draft, heading)
		if !ok || strings.TrimSpace(body) == "" {
			continue
		}
		next, err := SetSection(text, heading, normalizeNewlines(body, newline))
		if err != nil {
			return "", err
		}
		text = next
	}
	return text, nil
}
