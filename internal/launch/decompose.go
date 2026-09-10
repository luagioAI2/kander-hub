package launch

import (
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
}

// decomposeAgentPrompt is the body that the Agent receives as its task file.
// It deliberately reuses startAgentPrompt's rule-loading contract; the
// differences are the head and the body template. mode carries the
// requirement's MODE field and switches the orchestrator between
// collaborative (asks the user to confirm each draft) and autonomous
// (proposes then drives kander new directly).
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

// commandDecompose launches an Agent session to decompose a requirement card.
// The launched session is the requirement's orchestrator: it follows
// KANDER-TASK-INTAKE-RULES.md and, once a plan is agreed, KANDER-TASK-GROUP-RULES.md.
// The card's MODE field (collaborative | autonomous) drives whether the
// orchestrator asks for confirmation between drafts.
func commandDecompose(args DecomposeArgs) error {
	root := args.Root
	reqID := args.ReqID

	// Every board read/write below (ReadRequirementDocument, SetRequirementMode,
	// WriteRequirementDocument, recordRequirementWindowLocation) acquires and
	// releases the requirements-pool lock itself, so this command must NOT hold
	// that lock for the whole call: holding it and then re-entering any of those
	// helpers would deadlock on the same exclusive lock file.

	original, err := readRequirementFn(root, reqID)
	if err != nil {
		return err
	}
	mode := board.ParseRequirementMode(original)
	if args.Autonomous {
		mode = board.ReqModeAutonomous
	} else if mode == "" {
		mode = board.ReqModeCollaborative
	}
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
	if !contains(config.ExecutionAgents, agentName) {
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
	applyAgentLaunchEnv(&plan, agentName)
	program, err := requireAgentProgram(agentName)
	if err != nil {
		return err
	}
	session, err := newAgentSession(agentName, program)
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
	isDSH := agentName == "dsh"
	// Decomposition uses the large scale model by default because reading
	// the requirement, asking clarifying questions, and producing N task
	// specs is naturally a large-scope job.
	model := cfg.Models.Kanban[agentName]
	args2, err := agentArguments(agentName, model, "large", session, false)
	if err != nil {
		return err
	}
	if agentName == "dsh" {
		// The dsh tui profile ignores a positional prompt; the decompose
		// instruction is injected into the live TUI after launch (see below).
		// The task file stays on disk so the orchestrator can read it too.
		isDSH = true
		prompt = ""
		taskFileHandedOff = true
	} else {
		args2 = append(args2, prompt)
	}
	inv, err := launchInvocation(plan, *program, args2)
	if err != nil {
		return err
	}
	updated, err := RenderDecomposeMetadata(original, session.Render())
	if err != nil {
		return err
	}
	if err := writeRequirementFn(root, reqID, updated); err != nil {
		return err
	}
	loc := (func(LaunchOutcome) error)(nil)
	if plan.Launcher == "herdr" || plan.Launcher == "tmux" || plan.Launcher == "tmux-session" {
		loc = recordRequirementWindowLocation(root, plan, reqID)
	}
	// Re-running decompose on the same requirement should reuse its existing
	// deploy window rather than create a brand-new one each time; otherwise
	// orphan dsh windows pile up on the tmux server. If the previously
	// recorded address is still reachable, hand it to launchAgent as a reuse
	// target so it restarts the agent inside that pane.
	if plan.Launcher == "tmux-session" || plan.Launcher == "tmux" {
		if oldSession, oldWindow, oldPane, ok := tmuxAddress(board.ParseRequirementWindow(original)); ok &&
			oldSession == plan.Session && staleWindowAlive(plan.Tmux, oldSession, oldWindow) {
			plan.ReuseWindow = oldWindow
			plan.ReusePane = oldPane
		}
	}
	outcome, err := launchAgent(plan, root, "req-"+reqID, inv, loc, nil, &session)
	if err != nil {
		return err
	}
	// Feed the decompose instruction into the live dsh TUI so the orchestrator
	// actually works the requirement instead of sitting at a blank prompt.
	// injectDSHTask polls for the dsh> prompt, then pastes the full body.
	if isDSH {
		if injectErr := injectDSHTask(plan, outcome, body); injectErr != nil {
			return injectErr
		}
	}
	taskFileHandedOff = true
	return reportLaunch(t("launch.started"), board.Entry{TaskID: "req-" + reqID}, agentName, plan, outcome)
}

// tmuxAddress parses a stored "<launcher>:<session>:<window>:<pane>" WINDOW
// value into its session, window and pane address. False unless it is a plain
// tmux/tmux-session address on the default server.
func tmuxAddress(stored string) (session, window, pane string, ok bool) {
	if stored == "" || !strings.HasPrefix(stored, "tmux") {
		return "", "", "", false
	}
	parts := strings.Split(stored, ":")
	if len(parts) >= 4 {
		return parts[1], parts[2], parts[3], true
	}
	return "", "", "", false
}

// staleWindowAlive reports whether a recorded tmux window (by session:window
// id) still exists, so decompose can reuse it instead of opening a new one. A
// window whose server or session is gone returns false and forces a fresh
// launch. list-windows -t <session> fails with a non-zero exit when the
// session is missing, and lists the live window ids otherwise (display-message
// is not reliable here: it returns exit 0 with empty output for an absent
// target).
func staleWindowAlive(tmux, session, window string) bool {
	if session == "" || window == "" {
		return false
	}
	res := tmuxCapture(tmux, "list-windows", "-t", session, "-F", "#{window_id}")
	if res.Code != 0 {
		return false
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if strings.TrimSpace(line) == window {
			return true
		}
	}
	return false
}
