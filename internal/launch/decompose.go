package launch

import (
	"slices"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// DecomposeArgs is the input to commandDecompose. It mirrors commandStart but
// addresses a requirement card rather than a task card; the requirement does
// not transition states (its decomposition state is set later by the agent
// via ConvertRequirement). The Agent is responsible for creating one or more
// task cards and then calling kander req convert to link them.
type DecomposeArgs struct {
	Root        string
	Agent       string
	AgentSet    bool
	Launcher    string
	ReqID       string
	Message     string
	MessageSet  bool
	MessageFile string
	// Autonomous switches the agent prompt from "discuss with the user,
	// confirm each card" to "decompose silently and write the cards". The
	// launcher window still appears; the agent simply does not wait for
	// user confirmation before writing cards.
	Autonomous bool
	// Discuss switches the agent prompt to drafts-only: the orchestrator
	// discusses and writes ## PROPOSED_TASKS drafts but never runs kander new,
	// so the requirement stays undecided until someone materializes the drafts.
	Discuss bool
}

// decomposeAgentPrompt is the body that the Agent receives as its task file.
// It deliberately reuses startAgentPrompt's rule-loading contract; the
// differences are the head and the body template. mode carries the
// requirement's MODE field and switches the orchestrator between discuss
// (drafts only), collaborative (asks the user to confirm each draft) and
// autonomous (proposes then drives kander new directly).
func decomposeAgentPrompt(reqID string, paths config.InstallPaths, message, cardText, mode string) (string, error) {
	if paths.Mode == config.ModeProject && paths.ProjectRoot == "" {
		return "", launchError("config.project_install_paths_are_missing_the_main_worktree")
	}
	rules, err := ruleLoadingWithLanguage(paths, cardText)
	if err != nil {
		return "", err
	}
	command := commandName(paths)
	body := t("launch.prompt.decompose_body",
		reqID,
		cardText,
		attachmentList(board.ParseRequirementAttachments(cardText)),
	)
	head := t("launch.prompt.decompose_head", reqID)
	if mode == board.ReqModeAutonomous {
		return t("launch.prompt.decompose_autonomous",
			head,
			message,
			command,
		) + "\n\n" + body + "\n\n" + rules + "\n", nil
	}
	if mode == board.ReqModeDiscuss {
		return t("launch.prompt.decompose_discuss",
			head,
			message,
			command,
			reqID,
		) + "\n\n" + body + "\n\n" + rules + "\n", nil
	}
	return t("launch.prompt.decompose",
		head,
		message,
	) + "\n\n" + body + "\n\n" + rules + "\n", nil
}

// ParseRequirementMode returns the orchestrator mode recorded on a card, or
// empty when absent.
func parseRequirementMode(text string) string {
	return board.ParseRequirementMode(text)
}

// attachmentList renders an attachment path list for the prompt; an empty
// list renders as a plain "none".
func attachmentList(paths []string) string {
	if len(paths) == 0 {
		return "N/A"
	}
	return strings.Join(paths, "; ")
}

// resolveDecomposeMode picks the mode for this run. An explicit flag wins over
// the value recorded on the card, and a card that never recorded a mode runs
// collaborative (the safe default: the orchestrator asks before creating cards).
func resolveDecomposeMode(recorded string, args DecomposeArgs) string {
	switch {
	case args.Autonomous:
		return board.ReqModeAutonomous
	case args.Discuss:
		return board.ReqModeDiscuss
	case recorded == "":
		return board.ReqModeCollaborative
	}
	return recorded
}

// commandDecompose launches an Agent session to decompose a requirement card.
// The launched session is the requirement's orchestrator: it follows
// KANDER-TASK-INTAKE-RULES.md and, once a plan is agreed, KANDER-TASK-GROUP-RULES.md.
// The card's MODE field (discuss | collaborative | autonomous) drives whether the
// orchestrator may create cards at all and whether it asks for confirmation
// between drafts.
func commandDecompose(args DecomposeArgs) error {
	root := args.Root
	reqID := args.ReqID

	// Every board read/write below (ReadRequirementDocument, SetRequirementMode,
	// WriteRequirementDocument, recordRequirementWindowLocation) acquires and
	// releases the requirements-pool lock itself, so this command must NOT hold
	// that lock for the whole call: holding it and then re-entering any of those
	// helpers would deadlock on the same exclusive lock file.

	original, err := board.ReadRequirementDocument(root, reqID)
	if err != nil {
		return err
	}
	mode := resolveDecomposeMode(board.ParseRequirementMode(original), args)
	if _, err := board.SetRequirementMode(root, reqID, mode); err != nil {
		return err
	}
	cfg, err := loadEffective()
	if err != nil {
		return err
	}
	agentName := args.Agent
	if !args.AgentSet {
		agentName, err = config.KanbanAgentFor(cfg, "large")
		if err != nil {
			return err
		}
	}
	if !slices.Contains(config.ExecutionAgents, agentName) {
		return launchError("launch.unsupported_agent", agentName)
	}
	launcher := args.Launcher
	if launcher == "" {
		launcher = cfg.Launcher
	}
	plan, err := prepareLaunch(launcher, parentDir(root), "decompose")
	if err != nil {
		return err
	}
	// applyAgentDelivery fills the plan's prompt delivery from the agent
	// definition (DSH declares pane delivery with a "dsh >" readiness match)
	// and its extra environment (DSH_PERMISSION_MODE).
	if err := applyAgentDelivery(&plan, cfg, agentName); err != nil {
		return err
	}
	program, err := requireAgentProgram(agentName, cfg)
	if err != nil {
		return err
	}
	session, err := newAgentSession(agentName, program, cfg)
	if err != nil {
		return err
	}
	if agentName == "dsh" {
		reference, preErr := dshPrecreateSession("req-"+reqID, root)
		if preErr != nil {
			return preErr
		}
		session = AgentSession{Agent: agentName, Reference: reference}
	}
	paths, err := currentInstallPaths()
	if err != nil {
		return err
	}
	body, err := decomposeAgentPrompt(reqID, paths, args.Message, original, mode)
	if err != nil {
		return err
	}
	taskFile, err := createTaskFile(body, "kander-req-"+reqID+"-decompose-")
	if err != nil {
		return err
	}
	taskFileHandedOff := false
	defer func() {
		if !taskFileHandedOff {
			_ = removeTaskFile(taskFile)
		}
	}()
	prompt := taskInstruction(t("launch.prompt.decompose_head", reqID), taskFile)
	// Decomposition uses the large scale model by default because reading
	// the requirement, asking clarifying questions, and producing N task
	// specs is naturally a large-scope job.
	model := cfg.Models.Kanban[agentName]
	args2, err := agentArguments(agentName, model, "large", session, false, cfg)
	if err != nil {
		return err
	}
	// attachPrompt hands the instruction to whatever delivery mode the agent
	// definition declares: an argv agent gets it appended to argv, while a pane
	// agent (DSH) has it typed into the ready TUI by launchAgent's pane
	// delivery. The readiness match and its budget come from the definition's
	// prompt_delivery block, so this command carries no agent-specific
	// injection logic of its own.
	inv, err := launchInvocation(plan, *program, attachPrompt(&plan, args2, prompt))
	if err != nil {
		return err
	}
	updated, err := RenderDecomposeMetadata(original, session.Render())
	if err != nil {
		return err
	}
	if err := board.WriteRequirementDocument(root, reqID, updated); err != nil {
		return err
	}
	// A container backend produces a pane address worth recording on the
	// requirement card; a direct launcher (foreground/console) has none.
	loc := (func(LaunchOutcome) error)(nil)
	if plan.capabilities().Container {
		loc = recordRequirementWindowLocation(root, plan, reqID)
	}
	// Re-running decompose on the same requirement no longer reuses a recorded
	// window explicitly: the terminal layer owns that concern now, because the
	// tmux-session launcher resolves a stable per-project session and reuses it
	// (Target.SessionExists) instead of piling up orphan sessions.
	outcome, err := launchAgent(plan, root, "req-"+reqID, inv, loc, nil, &session)
	if err != nil {
		return err
	}
	taskFileHandedOff = true
	return reportLaunch(t("launch.started"), board.Entry{TaskID: "req-" + reqID}, agentName, plan, outcome)
}
