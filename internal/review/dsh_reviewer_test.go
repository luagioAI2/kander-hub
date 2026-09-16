package review

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

// The headless DSH app takes only a positional task, prints the final assistant
// message on stdout and exits, and offers no --model/--effort flags. Its review
// contract is therefore: one positional instruction, no stdin payload, the
// report read from stdout, and a --patch overlay that pins the boot back to a
// read-only sandbox because the profile's own patch layer opens it up.
func TestDshReviewerContract(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("DSH_HOME", t.TempDir())
	t.Setenv("DSH_REVIEW_MODEL", "")
	t.Setenv("DSH_REVIEW_REASONING_EFFORT", "")
	settings, err := agentSettingsFor("dsh", "PMQA", "large")
	if err != nil {
		t.Fatal(err)
	}
	if settings.stdin != config.ReviewStdinNone {
		t.Fatalf("stdin=%q", settings.stdin)
	}
	if settings.cwd != config.ReviewCWDRoot {
		t.Fatalf("cwd=%q", settings.cwd)
	}
	if settings.homePolicy != config.ReviewHomeRequired {
		t.Fatalf("home policy=%q", settings.homePolicy)
	}
	if settings.reviewHome == "" {
		t.Fatal("review home must resolve from DSH_HOME")
	}
	if settings.output.Source != process.SourceStdout || settings.output.Parse != process.ParseRaw {
		t.Fatalf("output=%+v", settings.output)
	}
	if len(settings.promptFiles) != 1 || settings.promptFiles[0].Name != "overlay" {
		t.Fatalf("prompt files=%+v", settings.promptFiles)
	}
	if settings.promptFiles[0].Path != "review-overlay.yml" {
		t.Fatalf("overlay path=%q", settings.promptFiles[0].Path)
	}
	overlay := settings.promptFiles[0].Template
	for _, want := range []string{
		"id: sandbox-policy",
		"mode: read-only",
		"id: approval",
		"policy: never",
		"id: permission",
		"defaultPreset: read-only",
		// A --patch layer replaces the whole config object of a node, so the
		// permission presets must repeat every preset they used to carry.
		"workspace-write:",
		"danger-full-access:",
	} {
		if !strings.Contains(overlay, want) {
			t.Fatalf("overlay is missing %q:\n%s", want, overlay)
		}
	}
	if _, err = process.ExpandTemplate(overlay, map[string]string{
		"inspection":      settings.inspectionRules,
		"prompt":          "PROMPT",
		"role":            "PM",
		"report_language": "en",
		"root":            "/work",
		"runtime":         "/rt",
		"output":          "/rt/output.txt",
	}, config.ReviewPromptFilePlaceholders()); err != nil {
		t.Fatal(err)
	}

	runtime := t.TempDir()
	overlayPath := filepath.Join(runtime, "review-overlay.yml")
	root := t.TempDir()
	ctx := reviewContext{
		agent: "dsh", root: root, settings: settings,
		program:         process.AgentProgram{Path: "dsh"},
		instruction:     process.TaskFileInstruction("Perform the PM review.", "/rt/prompt.txt"),
		promptFilePaths: map[string]string{"overlay": overlayPath},
	}
	inv, cwd, err := reviewerArguments(ctx, runtime, filepath.Join(runtime, "output.txt"), "/rt/prompt.txt")
	if err != nil {
		t.Fatal(err)
	}
	if cwd != root {
		t.Fatalf("cwd=%q", cwd)
	}
	want := []string{"--profile", "headless", "--patch", overlayPath, ctx.instruction}
	if !reflect.DeepEqual(inv.Argv[1:], want) {
		t.Fatalf("argv\n got %q\nwant %q", inv.Argv[1:], want)
	}
	for _, banned := range []string{"--model", "--effort"} {
		if slices.Contains(inv.Argv, banned) {
			t.Fatalf("headless rejects %s: %q", banned, inv.Argv)
		}
	}
	if inv.Env["DSH_HOME"] == "" {
		t.Fatalf("DSH_HOME missing: %v", inv.Env)
	}
}

// The report has to be the last assistant message, because that is the only
// thing the headless entry point writes to stdout.
func TestDshReviewerPromptRequiresLastMessageReport(t *testing.T) {
	ctx := reviewContext{
		agent: "dsh", role: "PMQA", root: "/work",
		base: strings.Repeat("b", 40), commit: strings.Repeat("a", 40),
		settings: agentSettings{inspectionRules: "NO-WRITE"},
	}
	// The bootstrap points at the review contract instead of inlining it, so
	// the last-message report rules live in the rendered contract. dsh must be
	// among the agents that get them, or report parsing never sees the
	// kander-findings fence.
	if contract := buildReviewContract(ctx); !strings.Contains(contract, lastMessageOutputContract) {
		t.Fatal("dsh review contract does not require the final message to be the report")
	}
	if bootstrap := buildPrompt(ctx, "/rt/evidence.txt", "Task context."); strings.Contains(bootstrap, lastMessageOutputContract) {
		t.Fatal("dsh bootstrap must point at the contract rather than inlining it")
	}
}
