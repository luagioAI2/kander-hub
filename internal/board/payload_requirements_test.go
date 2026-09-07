package board

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequirementPayloadIncludesLinkedTasks(t *testing.T) {
	root := reqTestRoot(t)
	if _, err := AddRequirement(root, "login-fix", "Fix login bug", "src", "summary"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewTask(root, "bug", "login-ui", "login ui", "en", false); err != nil {
		t.Fatal(err)
	}
	id := todayPrefix() + "-login-fix-req"
	if _, err := ConvertRequirement(root, id, []string{todayPrefix() + "-login-ui-task"}, nil); err != nil {
		t.Fatal(err)
	}
	summary, err := RequirementPayload(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if summary.RequirementID != id {
		t.Fatalf("requirement_id = %s", summary.RequirementID)
	}
	if summary.Status != ReqStatusDecomposed {
		t.Fatalf("status = %s", summary.Status)
	}
	if summary.Total != 1 {
		t.Fatalf("total = %d", summary.Total)
	}
	if len(summary.Linked) != 1 {
		t.Fatalf("linked = %v", summary.Linked)
	}
	link := summary.Linked[0]
	if link.ID != todayPrefix()+"-login-ui-task" || link.State != "backlog" {
		t.Fatalf("link = %+v", link)
	}
}

func TestBoardPayloadIncludesRequirements(t *testing.T) {
	root := reqTestRoot(t)
	if _, err := AddRequirement(root, "login-fix", "Fix login bug", "src", ""); err != nil {
		t.Fatal(err)
	}
	view, err := BoardPayload(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Requirements) != 1 {
		t.Fatalf("want 1 requirement, got %d", len(view.Requirements))
	}
	if view.Requirements[0].RequirementID != todayPrefix()+"-login-fix-req" {
		t.Fatalf("id = %s", view.Requirements[0].RequirementID)
	}
}

func TestBoardPayloadReturnsEmptyWhenRequirementsMissing(t *testing.T) {
	root := reqTestRoot(t)
	view, err := BoardPayload(root)
	if err != nil {
		t.Fatal(err)
	}
	if view.Requirements != nil {
		t.Fatalf("expected nil requirements, got %v", view.Requirements)
	}
}

func TestRequirementPayloadRequiresExistingID(t *testing.T) {
	root := reqTestRoot(t)
	if err := EnsureRequirements(root); err != nil {
		t.Fatal(err)
	}
	if _, err := RequirementPayload(root, "20260101-nope-req"); err == nil {
		t.Fatal("expected error for missing requirement")
	}
}

func TestRequirementPayloadMarksMissingLinkedTask(t *testing.T) {
	root := reqTestRoot(t)
	if err := os.MkdirAll(filepath.Join(root, RequirementsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := "# x\n\n- SOURCE: src\n- STATUS: decomposed\n- CREATED_AT: 2026-01-01 00:00\n- DOCS: \n- TASKS: 20260101-ghost-task\n- TASK_GROUPS: \n\n## SUMMARY\n\n<placeholder>\n"
	if err := os.WriteFile(filepath.Join(root, RequirementsDir, "20260101-ghost-req.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	summary, err := RequirementPayload(root, "20260101-ghost-req")
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Linked) != 1 || !summary.Linked[0].Missing {
		t.Fatalf("linked = %+v", summary.Linked)
	}
}
