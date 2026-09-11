package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// completeDraft fills every contract section a card needs to pass its gates.
const completeDraft = "# Login page UI\n\n" +
	"## GOAL\n\nBuild the login page.\n\n" +
	"## EXPECTED_OUTCOME\n\nA rendered login form.\n\n" +
	"## ACCEPTANCE_CRITERIA\n\n- [ ] The form renders\n\n" +
	"## OUT_OF_SCOPE\n\n- Password reset\n"

// partialDraft supplies only GOAL: every other section must keep its skeleton
// placeholder so the contract gate still rejects the card before todo.
const partialDraft = "# Login API\n\n## GOAL\n\nServe the login endpoints.\n"

// draftBoard builds a board with one requirement carrying two drafts.
func draftBoard(t *testing.T) (string, string) {
	t.Helper()
	root := reqTestRoot(t)
	t.Setenv(EnvBoardDir, root)
	if _, err := AddRequirement(root, "mobile-login", "Add mobile login", "teambition:BUG-1", ""); err != nil {
		t.Fatal(err)
	}
	id := todayPrefix() + "-mobile-login-req"
	drafts := map[string]string{"login-ui": completeDraft, "login-api": partialDraft}
	if _, err := SetProposedTasks(root, id, drafts); err != nil {
		t.Fatal(err)
	}
	return root, id
}

func readTaskCard(t *testing.T, root, taskID string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "backlog", taskID, "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// SetSection is the only in-place section editor the draft merge relies on, so
// its replace path must drop the old section rather than duplicate it.
func TestSetSectionReplacesInsteadOfDuplicating(t *testing.T) {
	const text = "# Title\n\n## GOAL\n\n<FILL_IN>\n\n## OUT_OF_SCOPE\n\n- <FILL_IN>\n"
	got, err := SetSection(text, SectionGoal, "Build it.")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(got, "## "+SectionGoal); n != 1 {
		t.Fatalf("GOAL appears %d times, want 1:\n%s", n, got)
	}
	if body, _ := SectionBody(got, SectionGoal); body != "Build it." {
		t.Fatalf("GOAL = %q", body)
	}
	if body, _ := SectionBody(got, SectionOutOfScope); body != "- <FILL_IN>" {
		t.Fatalf("the following section must survive: %q", body)
	}
	// Replacing again is idempotent, and removing the section drops it cleanly.
	again, err := SetSection(got, SectionGoal, "Build it.")
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("SetSection is not idempotent:\n%q\n%q", got, again)
	}
	removed, err := SetSection(again, SectionGoal, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(removed, "## "+SectionGoal) {
		t.Fatalf("removal left the section behind:\n%s", removed)
	}
	// A missing section is appended, not treated as an error.
	appended, err := SetSection(text, SectionThreatModel, "Trust no input.")
	if err != nil {
		t.Fatal(err)
	}
	if body, ok := SectionBody(appended, SectionThreatModel); !ok || body != "Trust no input." {
		t.Fatalf("THREAT_MODEL = %q ok=%v", body, ok)
	}
}

// A CRLF card must keep CRLF framing: a lone LF introduced by the section
// markers would break byte-for-byte comparisons elsewhere in the pipeline. A
// body's own newlines are caller-supplied text and stay verbatim.
func TestSetSectionPreservesCRLF(t *testing.T) {
	crlf := strings.ReplaceAll("# Title\n\n## GOAL\n\n<FILL_IN>\n", "\n", "\r\n")
	got, err := SetSection(crlf, SectionGoal, "Line one.\nLine two.")
	if err != nil {
		t.Fatal(err)
	}
	// Every line the function wrote is CRLF-terminated except the last, whose
	// terminator is the body's own LF before the "\r\n" the function appends.
	for _, line := range strings.Split(got, "\r\n") {
		// The only embedded LF allowed is the one inside the caller's body.
		if strings.Count(line, "\n") > 1 {
			t.Fatalf("section framing lost CRLF: %q in\n%q", line, got)
		}
	}
	if !strings.Contains(got, "## GOAL\r\n") {
		t.Fatalf("heading not CRLF-terminated:\n%q", got)
	}
	if !strings.Contains(got, "Line two.\r\n") {
		t.Fatalf("body not CRLF-terminated:\n%q", got)
	}
	// A single-line body leaves the whole file uniformly CRLF.
	plain, err := SetSection(crlf, SectionGoal, "One line.")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ReplaceAll(plain, "\r\n", ""), "\n") {
		t.Fatalf("a single-line body must keep the file uniformly CRLF:\n%q", plain)
	}
	if body, _ := SectionBody(plain, SectionGoal); body != "One line." {
		t.Fatalf("GOAL = %q", body)
	}
}

// A draft body written with LF must not leave a mixed-ending card behind: the
// card is a new file, so its skeleton's newline wins.
func TestMergeDraftContractNormalizesNewlines(t *testing.T) {
	skeleton := renderContract("Login page UI", "feature", "en")
	if draftNewline(skeleton) != "\r\n" {
		t.Fatalf("fixture skeleton newline = %q, want CRLF", draftNewline(skeleton))
	}
	draft := "# Login page UI\n\n## GOAL\n\nLine one.\nLine two.\n"
	merged, err := mergeDraftContract(skeleton, draft, "Login page UI")
	if err != nil {
		t.Fatal(err)
	}
	// The body is stored with the card's newline, and reads back that way.
	if body, _ := SectionBody(merged, SectionGoal); body != "Line one.\r\nLine two." {
		t.Fatalf("GOAL = %q, want the body normalized to CRLF", body)
	}
	if strings.Contains(strings.ReplaceAll(merged, "\r\n", ""), "\n") {
		t.Fatalf("merged card is not uniformly CRLF:\n%q", merged)
	}
	// The title line keeps its own ending rather than introducing an LF.
	if !strings.Contains(merged, "# Login page UI\r\n") {
		t.Fatalf("title line lost its CRLF:\n%q", merged)
	}
}

// A UTF-8 BOM is not Unicode whitespace, so it must be trimmed explicitly; left
// in place it hides both the "# " heading and a body that starts with a section.
func TestDraftTitleAndSectionsTolerateBOM(t *testing.T) {
	if got := draftTitle("\ufeff# Login page UI\n\n## GOAL\n\nBody.\n"); got != "Login page UI" {
		t.Fatalf("draftTitle with BOM = %q", got)
	}
	// A BOM in front of a leading section must still yield that section when the
	// writer stripped it, and the draft parser must not lose the whole body.
	draft := trimBOM("\ufeff## GOAL\n\nBody.\n")
	if body, ok := SectionBody(draft, SectionGoal); !ok || body != "Body." {
		t.Fatalf("GOAL after BOM strip = %q ok=%v", body, ok)
	}
	if got := draftTitle("\ufeff# <FILL_IN>\n"); got != "" {
		t.Fatalf("a placeholder heading must not become the title, got %q", got)
	}
}

func TestConvertRequirementDraftsCreatesAndLinksCards(t *testing.T) {
	root, id := draftBoard(t)
	created, err := ConvertRequirementDrafts(root, id, "feature", "en", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Stable slug order, so a repeated run is reproducible.
	want := []string{todayPrefix() + "-login-api-task", todayPrefix() + "-login-ui-task"}
	if len(created) != 2 || created[0] != want[0] || created[1] != want[1] {
		t.Fatalf("created = %v, want %v", created, want)
	}
	for _, taskID := range created {
		if _, err := os.Stat(filepath.Join(root, "backlog", taskID, "spec.md")); err != nil {
			t.Fatalf("card %s missing: %v", taskID, err)
		}
	}
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	live, err := RequirementsLiveStatus(root, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if live[0].Total != 2 || live[0].Done != 0 {
		t.Fatalf("progress = %d/%d, want 0/2", live[0].Done, live[0].Total)
	}
	if live[0].Status != ReqStatusDecomposed {
		t.Fatalf("status = %s, want decomposed", live[0].Status)
	}
	// Consumed drafts mean a second run has nothing left to materialize, so it
	// cannot create duplicate cards.
	if _, err := ConvertRequirementDrafts(root, id, "feature", "en", false, nil); err == nil {
		t.Fatal("expected a no-drafts error on the second run")
	}
	if raw := readReqCard(t, root, id); strings.Contains(raw, "### login-ui") {
		t.Fatalf("converted drafts must be removed from the card:\n%s", raw)
	}
}

// A draft supplies contract content; the envelope still comes from the board, and
// the post-creation self-review record can never be pre-filled by a draft.
func TestConvertRequirementDraftsMergesContractAndKeepsEnvelope(t *testing.T) {
	root, id := draftBoard(t)
	if _, err := ConvertRequirementDrafts(root, id, "bug", "en", true, nil); err != nil {
		t.Fatal(err)
	}
	uiCard := readTaskCard(t, root, todayPrefix()+"-login-ui-task")
	if got := TitleFrom(uiCard); got != "Login page UI" {
		t.Fatalf("title = %q, want the draft heading", got)
	}
	if body, ok := SectionBody(uiCard, SectionGoal); !ok || body != "Build the login page." {
		t.Fatalf("GOAL = %q", body)
	}
	if body, _ := SectionBody(uiCard, SectionAcceptanceCriteria); !strings.Contains(body, "The form renders") {
		t.Fatalf("ACCEPTANCE_CRITERIA = %q", body)
	}
	// The envelope is generated, not supplied by the draft.
	if got := MetadataFrom(uiCard, FieldType); got != "Bug" {
		t.Fatalf("TYPE = %q", got)
	}
	if got := MetadataFrom(uiCard, FieldSize); got != "large" {
		t.Fatalf("SIZE = %q", got)
	}
	if got := MetadataFrom(uiCard, FieldLanguage); got != "en" {
		t.Fatalf("LANGUAGE = %q", got)
	}
	// DISCUSSION carries the self-review records, so a draft must never be able
	// to fill it in and forge the gate.
	if disc, _ := SectionBody(uiCard, SectionDiscussion); strings.Contains(disc, "SELF_REVIEW") {
		t.Fatalf("draft forged the self-review record: %q", disc)
	}
	// A partial draft leaves the untouched sections as placeholders, so the card
	// fails the contract gate instead of looking complete.
	apiCard := readTaskCard(t, root, todayPrefix()+"-login-api-task")
	if body, _ := SectionBody(apiCard, SectionGoal); body != "Serve the login endpoints." {
		t.Fatalf("GOAL = %q", body)
	}
	if body, _ := SectionBody(apiCard, SectionExpectedOutcome); body != Placeholder {
		t.Fatalf("EXPECTED_OUTCOME = %q, want the untouched placeholder", body)
	}
	if body, _ := SectionBody(apiCard, SectionOutOfScope); body != "- "+Placeholder {
		t.Fatalf("OUT_OF_SCOPE = %q, want the untouched placeholder", body)
	}
}

// Materializing a subset leaves the other drafts available for a later run.
func TestConvertRequirementDraftsOnlyLeavesOtherDrafts(t *testing.T) {
	root, id := draftBoard(t)
	created, err := ConvertRequirementDrafts(root, id, "feature", "en", false, []string{"login-ui"})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 || created[0] != todayPrefix()+"-login-ui-task" {
		t.Fatalf("created = %v", created)
	}
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs[0].Proposed) != 1 {
		t.Fatalf("Proposed = %v, want only login-api left", reqs[0].Proposed)
	}
	if _, ok := reqs[0].Proposed["login-api"]; !ok {
		t.Fatalf("login-api must stay proposed: %v", reqs[0].Proposed)
	}
	if _, ok := reqs[0].Proposed["login-ui"]; ok {
		t.Fatal("the converted draft must be consumed")
	}
	if len(reqs[0].Tasks) != 1 || reqs[0].Tasks[0] != created[0] {
		t.Fatalf("Tasks = %v, want [%s]", reqs[0].Tasks, created[0])
	}
}

// Progress must be derived from the cards this command actually created, so a
// materialized draft counts the moment its card reaches done.
func TestConvertRequirementDraftsFeedsLiveProgress(t *testing.T) {
	root, id := draftBoard(t)
	created, err := ConvertRequirementDrafts(root, id, "feature", "en", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	progress := func() (int, int, string) {
		t.Helper()
		reqs, _, err := LoadRequirements(root)
		if err != nil {
			t.Fatal(err)
		}
		live, err := RequirementsLiveStatus(root, reqs)
		if err != nil {
			t.Fatal(err)
		}
		return live[0].Done, live[0].Total, live[0].Status
	}
	if done, total, status := progress(); done != 0 || total != 2 || status != ReqStatusDecomposed {
		t.Fatalf("before any card finished: %d/%d %s", done, total, status)
	}
	plantDoneTask(t, root, created[0])
	if done, total, status := progress(); done != 1 || total != 2 || status != ReqStatusDecomposed {
		t.Fatalf("after one card finished: %d/%d %s", done, total, status)
	}
	plantDoneTask(t, root, created[1])
	done, total, status := progress()
	if done != 2 || total != 2 || status != ReqStatusCompleted {
		t.Fatalf("after both cards finished: %d/%d %s", done, total, status)
	}
	// Derivation reads the board; it must never rewrite the stored card.
	stored, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	if stored[0].Status != ReqStatusDraft {
		t.Fatalf("stored STATUS = %q, want the untouched draft", stored[0].Status)
	}
}

func TestConvertRequirementDraftsRejectsBadInput(t *testing.T) {
	root, id := draftBoard(t)
	if _, err := ConvertRequirementDrafts(root, id, "feature", "en", false, []string{"nope"}); err == nil {
		t.Fatal("expected an unknown-draft error")
	}
	// An unknown TYPE is rejected before anything is created.
	if _, err := ConvertRequirementDrafts(root, id, "nonsense", "en", false, nil); err == nil {
		t.Fatal("expected an unknown-type error")
	}
	if reqs, _, _ := LoadRequirements(root); len(reqs[0].Proposed) != 2 {
		t.Fatalf("a rejected run must not consume drafts: %v", reqs[0].Proposed)
	}
}

func TestConvertRequirementDraftsRequiresDrafts(t *testing.T) {
	root := reqTestRoot(t)
	t.Setenv(EnvBoardDir, root)
	if _, err := AddRequirement(root, "plain", "No drafts yet", "src", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ConvertRequirementDrafts(root, todayPrefix()+"-plain-req", "feature", "en", false, nil); err == nil {
		t.Fatal("expected a no-drafts error")
	}
}

// A slug that already has a card is a real conflict, but the cards created
// before it must still be recorded: otherwise the drafts stay and every retry
// fails on the same slug without ever making progress.
func TestConvertRequirementDraftsRecordsProgressBeforeFailure(t *testing.T) {
	root, id := draftBoard(t)
	if _, err := NewTask(root, "feature", "login-ui", "Already taken", "en", false); err != nil {
		t.Fatal(err)
	}
	created, err := ConvertRequirementDrafts(root, id, "feature", "en", false, nil)
	if err == nil {
		t.Fatal("expected a task-already-exists error")
	}
	// login-api sorts first, so it was created before the conflict was hit.
	if len(created) != 1 || created[0] != todayPrefix()+"-login-api-task" {
		t.Fatalf("created = %v, want only login-api", created)
	}
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs[0].Tasks) != 1 || reqs[0].Tasks[0] != created[0] {
		t.Fatalf("Tasks = %v, want the card created before the failure", reqs[0].Tasks)
	}
	// Only the conflicting draft is left, so a retry after resolving the slug
	// conflict has real work to do instead of repeating the same failure.
	if len(reqs[0].Proposed) != 1 {
		t.Fatalf("Proposed = %v, want only login-ui left", reqs[0].Proposed)
	}
	if _, ok := reqs[0].Proposed["login-ui"]; !ok {
		t.Fatalf("the conflicting draft must stay: %v", reqs[0].Proposed)
	}
}

func TestRunReqConvertFromDraftsRejectsMixedTargets(t *testing.T) {
	root := tempBoard(t)
	if _, err := AddRequirement(root, "mobile-login", "Add mobile login", "src", ""); err != nil {
		t.Fatal(err)
	}
	id := todayPrefix() + "-mobile-login-req"
	if _, err := SetProposedTasks(root, id, map[string]string{"login-ui": completeDraft}); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := capture(t, func() int {
		return RunRequirement([]string{"convert", id, "20260911-some-task", "--from-drafts"})
	})
	if code == 0 || !strings.Contains(stderr, "from-drafts") {
		t.Fatalf("explicit task IDs must be rejected with --from-drafts (code=%d): %s", code, stderr)
	}
	code, _, stderr = capture(t, func() int {
		return RunRequirement([]string{"convert", id, "--groups", "20260911-x-group", "--from-drafts"})
	})
	if code == 0 || !strings.Contains(stderr, "from-drafts") {
		t.Fatalf("--groups must be rejected with --from-drafts (code=%d): %s", code, stderr)
	}
	// The draft-only options must not be silently ignored without the flag.
	code, _, stderr = capture(t, func() int {
		return RunRequirement([]string{"convert", id, "20260911-some-task", "--only", "login-ui"})
	})
	if code == 0 || !strings.Contains(stderr, "from-drafts") {
		t.Fatalf("--only without --from-drafts must be a usage error (code=%d): %s", code, stderr)
	}
}

func TestRunReqConvertFromDraftsEndToEnd(t *testing.T) {
	root := tempBoard(t)
	if _, err := AddRequirement(root, "mobile-login", "Add mobile login", "src", ""); err != nil {
		t.Fatal(err)
	}
	id := todayPrefix() + "-mobile-login-req"
	if _, err := SetProposedTasks(root, id, map[string]string{"login-ui": completeDraft}); err != nil {
		t.Fatal(err)
	}
	taskID := todayPrefix() + "-login-ui-task"
	code, stdout, stderr := capture(t, func() int {
		return RunRequirement([]string{"convert", id, "--from-drafts"})
	})
	if code != 0 {
		t.Fatalf("convert --from-drafts exit = %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, taskID) {
		t.Fatalf("output must name the created card: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(root, "backlog", taskID, "spec.md")); err != nil {
		t.Fatalf("card not created: %v", err)
	}
	// The default TYPE is documented rather than inferred from SOURCE text.
	if got := MetadataFrom(readTaskCard(t, root, taskID), FieldType); got != typeNames[defaultDraftCardType] {
		t.Fatalf("TYPE = %q, want the documented default %q", got, typeNames[defaultDraftCardType])
	}
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs[0].Tasks) != 1 || reqs[0].Tasks[0] != taskID {
		t.Fatalf("Tasks = %v, want [%s]", reqs[0].Tasks, taskID)
	}
	// A second run has no drafts left, so it must fail rather than duplicate.
	code, _, stderr = capture(t, func() int {
		return RunRequirement([]string{"convert", id, "--from-drafts", "--type", "bug", "--large"})
	})
	if code == 0 {
		t.Fatalf("the second run must fail with no drafts left: %s", stderr)
	}
}
