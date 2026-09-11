package launch

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// The flag wins over the recorded value, an unset mode defaults to the safe
// collaborative run, and a recorded mode survives a run that passes no flag.
func TestResolveDecomposeMode(t *testing.T) {
	reqID := "20260911-mobile-login-req"
	cases := []struct {
		name     string
		recorded string
		args     DecomposeArgs
		want     string
	}{
		{"unset defaults to collaborative", "", DecomposeArgs{ReqID: reqID}, board.ReqModeCollaborative},
		{"recorded discuss is kept", board.ReqModeDiscuss, DecomposeArgs{ReqID: reqID}, board.ReqModeDiscuss},
		{"recorded autonomous is kept", board.ReqModeAutonomous, DecomposeArgs{ReqID: reqID}, board.ReqModeAutonomous},
		{"flag promotes to autonomous", board.ReqModeDiscuss, DecomposeArgs{ReqID: reqID, Autonomous: true}, board.ReqModeAutonomous},
		{"flag demotes to discuss", board.ReqModeAutonomous, DecomposeArgs{ReqID: reqID, Discuss: true}, board.ReqModeDiscuss},
		{"flag overrides an unset mode", "", DecomposeArgs{ReqID: reqID, Discuss: true}, board.ReqModeDiscuss},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveDecomposeMode(tc.recorded, tc.args); got != tc.want {
				t.Fatalf("mode = %q, want %q", got, tc.want)
			}
		})
	}
}

// The discuss prompt is the only thing keeping a "still deciding" requirement
// from growing task cards, so it must both forbid kander new and name the
// materialization command.
func TestDecomposePromptPerMode(t *testing.T) {
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "")

	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(t.TempDir(), "rules")}
	const card = "- LANGUAGE: en\n- STATUS: draft\n"
	reqID := "20260911-mobile-login-req"

	build := func(mode string) string {
		prompt, err := decomposeAgentPrompt(reqID, paths, "split the login work", card, mode)
		if err != nil {
			t.Fatal(err)
		}
		return prompt
	}
	discuss := build(board.ReqModeDiscuss)
	collaborative := build(board.ReqModeCollaborative)
	autonomous := build(board.ReqModeAutonomous)

	// Each mode must produce its own prompt; silently falling through to the
	// collaborative text would let discuss mode create cards.
	if discuss == collaborative || discuss == autonomous || collaborative == autonomous {
		t.Fatal("the three modes must produce distinct prompts")
	}
	for _, mode := range []string{board.ReqModeDiscuss, board.ReqModeCollaborative, board.ReqModeAutonomous} {
		got := build(mode)
		if !strings.Contains(got, board.ReqModeDiscuss) || !strings.Contains(got, board.ReqModeCollaborative) || !strings.Contains(got, board.ReqModeAutonomous) {
			t.Fatalf("%s prompt must document all three modes: %s", mode, got)
		}
	}
	if !strings.Contains(discuss, "--from-drafts") {
		t.Fatalf("discuss prompt must name the materialization command: %s", discuss)
	}
	if !strings.Contains(discuss, reqID) {
		t.Fatalf("discuss prompt must carry the requirement id: %s", discuss)
	}
	// The shared body already forbids advancing to working without confirmation.
	for _, want := range []string{"PROPOSED_TASKS", "spec.md"} {
		if !strings.Contains(discuss, want) {
			t.Fatalf("discuss prompt lost %q: %s", want, discuss)
		}
	}
}

// --discuss and --autonomous set the same field to opposite ends of the scale, so
// passing both must fail before anything is launched.
func TestRunDecomposeRejectsConflictingModeFlags(t *testing.T) {
	code := RunDecompose([]string{"--discuss", "--autonomous", "--message", "x", "20260911-mobile-login-req"})
	if code == 0 {
		t.Fatal("passing both mode flags must fail")
	}
}
