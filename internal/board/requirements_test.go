package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reqTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, state := range States {
		if err := os.MkdirAll(filepath.Join(root, state), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestAddAndLoadRequirement(t *testing.T) {
	root := reqTestRoot(t)
	path, err := AddRequirement(root, "login-fix", "Fix login bug", "pool://login-doc-42", "summary body")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != filepath.Join(root, RequirementsDir) {
		t.Fatalf("unexpected path %s", path)
	}
	reqs, problems, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if len(reqs) != 1 {
		t.Fatalf("want 1 requirement, got %d", len(reqs))
	}
	req := reqs[0]
	if req.ID != todayPrefix()+"-login-fix-req" {
		t.Fatalf("unexpected id %s", req.ID)
	}
	if req.Status != ReqStatusDraft {
		t.Fatalf("status = %s, want draft", req.Status)
	}
	if req.Source != "pool://login-doc-42" {
		t.Fatalf("source = %s", req.Source)
	}
	if req.Summary != "summary body" {
		t.Fatalf("summary = %q", req.Summary)
	}
}

// Empty source is normalised to "N/A" by AddRequirement so callers no longer
// need to invent a value. The legacy rejection was retired when the Web UI
// stopped asking for a SOURCE field.
var _ = AddRequirement

func TestAddRequirementRejectsDuplicateSlugPerDay(t *testing.T) {
	root := reqTestRoot(t)
	if _, err := AddRequirement(root, "login-fix", "One", "src-1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := AddRequirement(root, "login-fix", "Two", "src-2", ""); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestAddRequirementNormalizesAndValidatesSlug(t *testing.T) {
	root := reqTestRoot(t)
	// A camelCase slug is lowercased so the generated id matches reqIDRe.
	path, err := AddRequirement(root, "sayHello", "Say hello", "src", "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != todayPrefix()+"-sayhello-req.md" {
		t.Fatalf("unexpected card file %s", filepath.Base(path))
	}
	reqs, problems, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if len(reqs) != 1 || reqs[0].ID != todayPrefix()+"-sayhello-req" {
		t.Fatalf("want one normalized requirement, got %v", reqs)
	}
	// A slug that cannot be normalised to the allowed charset is rejected up
	// front instead of producing a card that LoadRequirements flags.
	for _, bad := range []string{"with space", "with/ slash", "UPPER!"} {
		if _, err := AddRequirement(root, bad, "Bad", "src", ""); err == nil {
			t.Fatalf("expected slug %q to be rejected", bad)
		}
	}
}

func TestConvertRequirementLinksAndDecomposes(t *testing.T) {
	root := reqTestRoot(t)
	if _, err := AddRequirement(root, "login-fix", "Fix login bug", "src", ""); err != nil {
		t.Fatal(err)
	}
	// convert with no targets is rejected
	if _, err := ConvertRequirement(root, todayPrefix()+"-login-fix-req", nil, nil); err == nil {
		t.Fatal("expected error for missing targets")
	}
	// nonexistent task is rejected
	if _, err := ConvertRequirement(root, todayPrefix()+"-login-fix-req", []string{"20260101-nope-task"}, nil); err == nil {
		t.Fatal("expected error for missing task")
	}
	// create a real task card, then convert
	if _, err := NewTask(root, "bug", "login-ui", "login ui", "en", false); err != nil {
		t.Fatal(err)
	}
	id := todayPrefix() + "-login-fix-req"
	if _, err := ConvertRequirement(root, id, []string{todayPrefix() + "-login-ui-task"}, nil); err != nil {
		t.Fatal(err)
	}
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	if reqs[0].Status != ReqStatusDecomposed {
		t.Fatalf("status = %s, want decomposed", reqs[0].Status)
	}
	if len(reqs[0].Tasks) != 1 || reqs[0].Tasks[0] != todayPrefix()+"-login-ui-task" {
		t.Fatalf("tasks = %v", reqs[0].Tasks)
	}
}

func TestRequirementProgressAndCompletion(t *testing.T) {
	root := reqTestRoot(t)
	if _, err := AddRequirement(root, "login-fix", "Fix login bug", "src", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := NewTask(root, "bug", "login-ui", "login ui", "en", false); err != nil {
		t.Fatal(err)
	}
	id := todayPrefix() + "-login-fix-req"
	taskID := todayPrefix() + "-login-ui-task"
	if _, err := ConvertRequirement(root, id, []string{taskID}, nil); err != nil {
		t.Fatal(err)
	}
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	req, err := RequirementStatus(reqs[0], root)
	if err != nil {
		t.Fatal(err)
	}
	if req.Total != 1 || req.Done != 0 {
		t.Fatalf("progress = %d/%d, want 0/1", req.Done, req.Total)
	}
	if RequirementProgressLine(req) != "0/1" {
		t.Fatalf("progress line = %s", RequirementProgressLine(req))
	}
	// --all-done completion must be refused while the task is open
	if err := checkAllDone(root, req); err == nil {
		t.Fatal("expected not-all-done error")
	}
	// Complete the linked task card. The full lifecycle crosses the review
	// evidence gate, which belongs to the review package, so the test plants a
	// finished card directly in done; RequirementStatus only reads the state.
	specPath := filepath.Join(root, "backlog", taskID, "spec.md")
	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	filled := appendRecordSection(string(spec), SectionDiscussion, "SELF_REVIEW: ok\nCARD_REVIEW: ok")
	filled = strings.ReplaceAll(filled, Placeholder, "done goal")
	filled = strings.Replace(filled, "- [ ] done goal", "- [x] done goal", 1)
	if err := os.MkdirAll(filepath.Join(root, "done", taskID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "done", taskID, "spec.md"), []byte(filled), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "backlog", taskID)); err != nil {
		t.Fatal(err)
	}
	reqs, _, err = LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	req, err = RequirementStatus(reqs[0], root)
	if err != nil {
		t.Fatal(err)
	}
	if req.Done != 1 || req.Total != 1 {
		t.Fatalf("progress = %d/%d, want 1/1", req.Done, req.Total)
	}
	if _, err := SetRequirementStatus(root, id, ReqStatusCompleted); err != nil {
		t.Fatal(err)
	}
	// completed cards cannot be removed and cannot go back to decomposed
	if _, err := RemoveRequirement(root, id); err == nil {
		t.Fatal("expected removal protection")
	}
	if _, err := SetRequirementStatus(root, id, ReqStatusDecomposed); err == nil {
		t.Fatal("expected completed guard")
	}
}

// checkAllDone mirrors the CLI gate in runReqComplete so the unit test covers
// the same condition without shelling out.
func checkAllDone(root string, req Requirement) error {
	req, err := RequirementStatus(req, root)
	if err != nil {
		return err
	}
	if req.Total == 0 || req.Done != req.Total || len(req.Missing) > 0 {
		return kanbanError("board.req_not_all_done", req.ID, itoa(req.Done), itoa(req.Total))
	}
	return nil
}

func TestLoadRequirementsReportsProblems(t *testing.T) {
	root := reqTestRoot(t)
	if err := EnsureRequirements(root); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(root, RequirementsDir, "not-a-req.md")
	if err := os.WriteFile(bad, []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reqs, problems, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 0 {
		t.Fatalf("want no requirements, got %d", len(reqs))
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "not-a-req") {
		t.Fatalf("problems = %v", problems)
	}
}

func TestUnlinkRequirementTargets(t *testing.T) {
	root := reqTestRoot(t)
	if _, err := AddRequirement(root, "login-fix", "Fix login bug", "src", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := NewTask(root, "bug", "login-ui", "login ui", "en", false); err != nil {
		t.Fatal(err)
	}
	id := todayPrefix() + "-login-fix-req"
	if _, err := ConvertRequirement(root, id, []string{todayPrefix() + "-login-ui-task"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := LinkRequirementTargets(root, id, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs[0].Tasks) != 0 {
		t.Fatalf("tasks = %v, want empty", reqs[0].Tasks)
	}
	if reqs[0].Status != ReqStatusDecomposed {
		t.Fatalf("status = %s, want decomposed kept", reqs[0].Status)
	}
}

func TestRequirementAttachmentsAndWindow(t *testing.T) {
	root := reqTestRoot(t)
	if _, err := AddRequirement(root, "login-fix", "Fix login bug", "src", ""); err != nil {
		t.Fatal(err)
	}
	id := todayPrefix() + "-login-fix-req"
	if _, err := SetRequirementAttachments(root, id, []string{"./shots/before.png", "  ./docs/spec.md  ", ""}); err != nil {
		t.Fatal(err)
	}
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("want 1 requirement, got %d", len(reqs))
	}
	got := reqs[0].Attach
	if len(got) != 2 || got[0] != "./shots/before.png" || got[1] != "./docs/spec.md" {
		t.Fatalf("attach = %v", got)
	}
	if raw := ParseRequirementAttachments(readReqCard(t, root, id)); len(raw) != 2 {
		t.Fatalf("ParseRequirementAttachments = %v", raw)
	}
	if _, err := SetRequirementAttachments(root, id, nil); err != nil {
		t.Fatal(err)
	}
	reqs, _, _ = LoadRequirements(root)
	if len(reqs[0].Attach) != 0 {
		t.Fatalf("expected cleared attach, got %v", reqs[0].Attach)
	}
}

func TestRequirementModeRoundTrip(t *testing.T) {
	root := reqTestRoot(t)
	path, err := AddRequirement(root, "login-fix", "Fix login", "src", "")
	if err != nil {
		t.Fatal(err)
	}
	id := todayPrefix() + "-login-fix-req"
	// Default (empty) parses as collaborative, never written to disk.
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	if reqs[0].Mode != "" {
		t.Fatalf("default mode = %q, want empty", reqs[0].Mode)
	}
	if _, err := SetRequirementMode(root, id, ReqModeAutonomous); err != nil {
		t.Fatal(err)
	}
	reqs, _, _ = LoadRequirements(root)
	if reqs[0].Mode != ReqModeAutonomous {
		t.Fatalf("mode = %q, want %q", reqs[0].Mode, ReqModeAutonomous)
	}
	raw := readReqCard(t, root, id)
	if ParseRequirementMode(raw) != ReqModeAutonomous {
		t.Fatalf("ParseRequirementMode = %q", ParseRequirementMode(raw))
	}
	// Invalid mode is rejected.
	if _, err := SetRequirementMode(root, id, "wild"); err == nil {
		t.Fatal("expected invalid mode rejection")
	}
	_ = path
}

func TestProposedTasksRoundTrip(t *testing.T) {
	root := reqTestRoot(t)
	if _, err := AddRequirement(root, "login-fix", "Fix login", "src", ""); err != nil {
		t.Fatal(err)
	}
	id := todayPrefix() + "-login-fix-req"
	drafts := map[string]string{
		"login-ui":  "# Login UI\n\n## GOAL\n\nship a usable login form\n",
		"login-api": "# Login API\n\n## GOAL\n\nship the auth endpoint\n",
	}
	if _, err := SetProposedTasks(root, id, drafts); err != nil {
		t.Fatal(err)
	}
	t.Logf("card:\n%s", readReqCard(t, root, id))
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("want 1 requirement, got %d", len(reqs))
	}
	got := reqs[0].Proposed
	if len(got) != 2 {
		t.Fatalf("Proposed = %v", got)
	}
	for slug, body := range drafts {
		if got[slug] != body {
			t.Fatalf("Proposed[%s] = %q, want %q", slug, got[slug], body)
		}
	}
	// Upsert one, remove the other, persist again.
	drafts["login-ui"] = "# Login UI v2\n\n## GOAL\n\nrevised\n"
	delete(drafts, "login-api")
	if _, err := SetProposedTasks(root, id, drafts); err != nil {
		t.Fatal(err)
	}
	t.Logf("after upsert:\n%s", readReqCard(t, root, id))
	reqs, _, _ = LoadRequirements(root)
	if len(reqs[0].Proposed) != 1 || reqs[0].Proposed["login-ui"] != drafts["login-ui"] {
		t.Fatalf("upsert removed unexpected entries: %v", reqs[0].Proposed)
	}
	// Clearing all drafts removes the section entirely.
	if _, err := SetProposedTasks(root, id, nil); err != nil {
		t.Fatal(err)
	}
	raw := readReqCard(t, root, id)
	if strings.Contains(raw, ReqSectionProposed) {
		t.Fatalf("section not removed:\n%s", raw)
	}
}

func readReqCard(t *testing.T, root, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, RequirementsDir, id+".md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// reqWithLinkedTask builds a board with one requirement and one linked task,
// the smallest fixture that exercises live progress derivation. It also points
// the CLI root resolver at the board so the RunRequirement tests can run.
func reqWithLinkedTask(t *testing.T) (string, string, string) {
	t.Helper()
	root := reqTestRoot(t)
	t.Setenv(EnvBoardDir, root)
	if _, err := AddRequirement(root, "login-fix", "Fix login bug", "src", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := NewTask(root, "bug", "login-ui", "login ui", "en", false); err != nil {
		t.Fatal(err)
	}
	return root, todayPrefix() + "-login-fix-req", todayPrefix() + "-login-ui-task"
}

// plantDoneTask moves a task card into done with a filled contract. The full
// lifecycle crosses the review evidence gate, which belongs to the review
// package, so tests plant the finished card directly; live status only reads
// the state.
func plantDoneTask(t *testing.T, root, taskID string) {
	t.Helper()
	specPath := filepath.Join(root, "backlog", taskID, "spec.md")
	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	filled := appendRecordSection(string(spec), SectionDiscussion, "SELF_REVIEW: ok\nCARD_REVIEW: ok")
	filled = strings.ReplaceAll(filled, Placeholder, "done goal")
	filled = strings.Replace(filled, "- [ ] done goal", "- [x] done goal", 1)
	if err := os.MkdirAll(filepath.Join(root, "done", taskID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "done", taskID, "spec.md"), []byte(filled), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "backlog", taskID)); err != nil {
		t.Fatal(err)
	}
}

// The CLI persists --attach paths verbatim on the new card, the same way the
// requirements TUI stores them; the pool never copies the targets.
func TestRunReqNewPersistsAttachments(t *testing.T) {
	root := reqTestRoot(t)
	t.Setenv(EnvBoardDir, root)
	code := RunRequirement([]string{"new", "--source", "src", "--attach", " ./shots/before.png , ./docs/spec.md ,, ",
		"login-fix", "Fix login bug"})
	if code != 0 {
		t.Fatalf("req new exit = %d", code)
	}
	id := todayPrefix() + "-login-fix-req"
	raw := ParseRequirementAttachments(readReqCard(t, root, id))
	if len(raw) != 2 || raw[0] != "./shots/before.png" || raw[1] != "./docs/spec.md" {
		t.Fatalf("ATTACHMENTS = %v", raw)
	}
}

// The CLI status filter matches the derived status, not the stored one, so
// `--status decomposed` finds requirements with linked open tasks and
// `--status draft` only finds cards with no linked tasks at all.
func TestRunReqListStatusFilterUsesDerivedStatus(t *testing.T) {
	root, id, taskID := reqWithLinkedTask(t)
	if _, err := ConvertRequirement(root, id, []string{taskID}, nil); err != nil {
		t.Fatal(err)
	}
	code, listed, _ := capture(t, func() int { return RunRequirement([]string{"list", "--status", "decomposed"}) })
	if code != 0 || !strings.Contains(listed, id) {
		t.Fatalf("decomposed filter lost the requirement (code=%d): %s", code, listed)
	}
	code, listed, _ = capture(t, func() int { return RunRequirement([]string{"list", "--status", "draft"}) })
	if code != 0 || strings.Contains(listed, id) {
		t.Fatalf("draft filter must not match a requirement with linked tasks (code=%d): %s", code, listed)
	}
	plantDoneTask(t, root, taskID)
	code, listed, _ = capture(t, func() int { return RunRequirement([]string{"list", "--status", "completed"}) })
	if code != 0 || !strings.Contains(listed, id) {
		t.Fatalf("completed filter lost the requirement after its task completed (code=%d): %s", code, listed)
	}
	if !strings.Contains(listed, "1/1") {
		t.Fatalf("listing must report live progress 1/1, got: %s", listed)
	}
}

// The listing reports live progress in both the table and the JSON form.
func TestRunReqListReportsLiveProgress(t *testing.T) {
	root, id, taskID := reqWithLinkedTask(t)
	if _, err := ConvertRequirement(root, id, []string{taskID}, nil); err != nil {
		t.Fatal(err)
	}
	code, listed, _ := capture(t, func() int { return RunRequirement([]string{"list"}) })
	if code != 0 {
		t.Fatalf("req list exit = %d", code)
	}
	if !strings.Contains(listed, "0/1") {
		t.Fatalf("listing must report live progress 0/1, got: %s", listed)
	}
	if !strings.Contains(listed, ReqStatusDecomposed) {
		t.Fatalf("listing must report the derived status, got: %s", listed)
	}
	code, listed, _ = capture(t, func() int { return RunRequirement([]string{"list", "--json"}) })
	if code != 0 {
		t.Fatalf("req list --json exit = %d", code)
	}
	if !strings.Contains(listed, `"Total":1`) || !strings.Contains(listed, `"Done":0`) {
		t.Fatalf("JSON listing must carry live done/total, got: %s", listed)
	}
	if !strings.Contains(listed, `"Status":"`+ReqStatusDecomposed+`"`) {
		t.Fatalf("JSON listing must carry the derived status, got: %s", listed)
	}
}

// The listing must carry the same live progress and derived status as the
// single-card view: LoadRequirements alone leaves Total at 0, so a listing
// that skips live recomputation renders "-" forever and its JSON reports
// 0/0 done totals. Regression for the req list / req show asymmetry.
func TestRequirementsLiveStatusMatchesSingleCardView(t *testing.T) {
	root, id, taskID := reqWithLinkedTask(t)
	if _, err := ConvertRequirement(root, id, []string{taskID}, nil); err != nil {
		t.Fatal(err)
	}
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	live, err := RequirementsLiveStatus(root, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 {
		t.Fatalf("want 1 requirement, got %d", len(live))
	}
	single, err := RequirementStatus(reqs[0], root)
	if err != nil {
		t.Fatal(err)
	}
	if live[0].Done != single.Done || live[0].Total != single.Total {
		t.Fatalf("batch progress %d/%d, single-card progress %d/%d", live[0].Done, live[0].Total, single.Done, single.Total)
	}
	if live[0].Total != 1 || live[0].Done != 0 {
		t.Fatalf("live progress = %d/%d, want 0/1", live[0].Done, live[0].Total)
	}
	// A requirement with linked open tasks derives "decomposed", which agrees
	// with the stored status because ConvertRequirement wrote it explicitly.
	if live[0].Status != ReqStatusDecomposed {
		t.Fatalf("derived status = %s, want decomposed", live[0].Status)
	}
	if stored := MetadataFrom(readReqCard(t, root, id), FieldReqStatus); stored != ReqStatusDecomposed {
		t.Fatalf("stored status = %s, want the value convert wrote", stored)
	}
	// Derivation never writes: the stored status is still what convert wrote,
	// not a derived value, after the live recomputation above.
	if live[0].Status != MetadataFrom(readReqCard(t, root, id), FieldReqStatus) {
		t.Fatalf("derivation must not rewrite the card file")
	}
	// The single-card form derives the same status so req show and req list
	// cannot disagree.
	if single.Status != ReqStatusDecomposed {
		t.Fatalf("single-card derived status = %s, want decomposed", single.Status)
	}
	// After the linked task completes, both forms derive "completed".
	plantDoneTask(t, root, taskID)
	reqs, _, err = LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	live, err = RequirementsLiveStatus(root, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if live[0].Done != 1 || live[0].Total != 1 {
		t.Fatalf("live progress = %d/%d, want 1/1", live[0].Done, live[0].Total)
	}
	if live[0].Status != ReqStatusCompleted {
		t.Fatalf("derived status = %s, want completed", live[0].Status)
	}
}

// Derivation is a display concern: the card file keeps the status the user set
// explicitly, and `kander req complete` with its --all-done gate stays the only
// writer of "completed".
func TestDeriveRequirementStatusKeepsStoredStatusOnFile(t *testing.T) {
	root, id, taskID := reqWithLinkedTask(t)
	if _, err := ConvertRequirement(root, id, []string{taskID}, nil); err != nil {
		t.Fatal(err)
	}
	plantDoneTask(t, root, taskID)
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	live, err := RequirementsLiveStatus(root, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if live[0].Status != ReqStatusCompleted {
		t.Fatalf("derived status = %s, want completed", live[0].Status)
	}
	if stored := MetadataFrom(readReqCard(t, root, id), FieldReqStatus); stored != ReqStatusDecomposed {
		t.Fatalf("stored status = %s, want the explicit decomposed value", stored)
	}
	// An explicit req complete writes the stored status; live derivation for a
	// card with no linked tasks keeps that stored value.
	if _, err := SetRequirementStatus(root, id, ReqStatusCompleted); err != nil {
		t.Fatal(err)
	}
	if stored := MetadataFrom(readReqCard(t, root, id), FieldReqStatus); stored != ReqStatusCompleted {
		t.Fatalf("stored status = %s, want completed", stored)
	}
}

// A linked task card that no longer exists is counted as missing, never as
// done, so derivation cannot turn a broken link into a completed requirement.
func TestRequirementsLiveStatusCountsMissingCards(t *testing.T) {
	root, id, taskID := reqWithLinkedTask(t)
	if _, err := ConvertRequirement(root, id, []string{taskID}, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "backlog", taskID)); err != nil {
		t.Fatal(err)
	}
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	live, err := RequirementsLiveStatus(root, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if live[0].Total != 1 || live[0].Done != 0 {
		t.Fatalf("live progress = %d/%d, want 0/1", live[0].Done, live[0].Total)
	}
	if len(live[0].Missing) != 1 || live[0].Missing[0] != taskID {
		t.Fatalf("missing = %v, want [%s]", live[0].Missing, taskID)
	}
	if live[0].Status != ReqStatusDecomposed {
		t.Fatalf("derived status = %s, want decomposed", live[0].Status)
	}
}

// A terminal archived card keeps its stored status; derivation never revives it.
func TestDeriveRequirementStatusKeepsArchivedTerminal(t *testing.T) {
	root, id, taskID := reqWithLinkedTask(t)
	if _, err := ConvertRequirement(root, id, []string{taskID}, nil); err != nil {
		t.Fatal(err)
	}
	plantDoneTask(t, root, taskID)
	if _, err := SetRequirementStatus(root, id, ReqStatusArchived); err != nil {
		t.Fatal(err)
	}
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		t.Fatal(err)
	}
	live, err := RequirementsLiveStatus(root, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if live[0].Status != ReqStatusArchived {
		t.Fatalf("derived status = %s, want archived", live[0].Status)
	}
}
