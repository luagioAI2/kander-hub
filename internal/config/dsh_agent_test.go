package config

import (
	"strings"
	"testing"
)

// This fork ships DSH as a built-in execution and review agent. Upstream ships
// a different extra agent in the same slot; the assertions below describe the
// DSH definition this fork actually embeds, so the test still pins the
// fork-specific agent contract instead of upstream's agent list.
func TestDshIsAnExecutionAndReviewAgent(t *testing.T) {
	setupHome(t)
	if !contains(ExecutionAgents, "dsh") || !HasReviewTemplate(nil, "dsh") {
		t.Fatal("dsh missing")
	}
	if AgentExecutableName("dsh") != "dsh" {
		t.Fatal(AgentExecutableName("dsh"))
	}
	spec, ok := RulesSpec("dsh")
	if !ok || spec.Global != ".dsh/AGENTS.md" || spec.Project != "AGENTS.md" || spec.Integration != "markdown-reference" {
		t.Fatalf("%+v %v", spec, ok)
	}
	d := AgentFor(nil, "dsh")
	// DSH's tui profile ignores a positional prompt, so the decompose session
	// delivers the instruction into the ready TUI: prompt delivery is "pane"
	// with the "dsh >" readiness match, not argv.
	if d.ProcessName != "dsh" || d.Session.Mode != "generated" || d.PromptDelivery.Mode != "pane" {
		t.Fatalf("%+v", d)
	}
	if d.PromptDelivery.Ready == nil || d.PromptDelivery.Ready.Match != "dsh >" {
		t.Fatalf("ready=%+v", d.PromptDelivery.Ready)
	}
	if d.ExitCommand == nil || *d.ExitCommand != "/exit" {
		t.Fatalf("exit_command=%v", d.ExitCommand)
	}
	if d.Args == nil || len(d.Args.Review) == 0 || d.Review == nil {
		t.Fatalf("review definition=%+v", d)
	}
	if d.Review.HomePolicy != "required" {
		t.Fatalf("review home policy=%q", d.Review.HomePolicy)
	}
	joined := strings.Join(d.Args.Review, " ")
	for _, want := range []string{"--profile", "headless", "--patch", "{prompt_file:overlay}", "{instruction}"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("review args missing %q: %v", want, d.Args.Review)
		}
	}
}
