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
