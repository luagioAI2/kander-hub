package launch

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
)

// RunDecompose implements the `kander req decompose` action. It expects to be
// hooked into cli.Commands via wire_board.go in the same way the other
// commands are; the CLI command name remains `req` and dispatches to
// RunDecompose through board.RunRequirement.
func RunDecompose(args []string) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			usageDecompose(os.Stdout)
			return 0
		}
	}
	rest, agent, launcher, _, parseErr := parseAgentLauncher(args)
	if parseErr != "" {
		usageDecompose(os.Stderr)
		fmt.Fprintln(os.Stderr, parseErr)
		return 2
	}
	var message, messageFile string
	var messageSet bool
	var autonomous bool
	var positional []string
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		switch {
		case arg == "--message":
			val, next, ok := takeValue(rest, i)
			if !ok {
				return usageDecomposeFail("launch.missing_message_value")
			}
			message, messageSet, i = val, true, next
		case strings.HasPrefix(arg, "--message="):
			message, messageSet = strings.TrimPrefix(arg, "--message="), true
		case arg == "--message-file":
			val, next, ok := takeValue(rest, i)
			if !ok {
				return usageDecomposeFail("launch.missing_message_file_value")
			}
			messageFile, i = val, next
		case strings.HasPrefix(arg, "--message-file="):
			messageFile = strings.TrimPrefix(arg, "--message-file=")
		case arg == "--autonomous":
			autonomous = true
		case strings.HasPrefix(arg, "--autonomous="):
			autonomous = strings.TrimPrefix(arg, "--autonomous=") == "true"
		default:
			if strings.HasPrefix(arg, "-") {
				return usageDecomposeFail("board.unknown_option", arg)
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		return usageDecomposeFail("launch.task_id_is_required")
	}
	if !messageSet && messageFile == "" {
		return usageDecomposeFail("launch.requires_exactly_one_of_message_or_message_file", "decompose")
	}
	if messageSet && messageFile != "" {
		return usageDecomposeFail("launch.requires_exactly_one_of_message_or_message_file", "decompose")
	}
	root, err := boardRootFn()
	if err != nil {
		return fail(err)
	}
	decompArgs := DecomposeArgs{
		Root:        root,
		Agent:       agent,
		AgentSet:    agent != "",
		Launcher:    launcher,
		ReqID:       positional[0],
		Message:     message,
		MessageSet:  messageSet,
		MessageFile: messageFile,
		Autonomous:  autonomous,
	}
	if err := commandDecompose(decompArgs); err != nil {
		return fail(err)
	}
	return 0
}

func usageDecompose(w io.Writer) {
	fmt.Fprintln(w, t(
		"launch.usage_kander_req_decompose_agent_launcher_message_text_message",
	))
	fmt.Fprintln(w, t("launch.usage_decompose_autonomous_hint"))
}

func usageDecomposeFail(id string, args ...any) int {
	usageDecompose(os.Stderr)
	if id != "" {
		fmt.Fprintln(os.Stderr, t(id, args...))
	}
	return 2
}

func init() {
	// Compose the existing decompose handler with the rest of the req CLI
	// pipeline by routing the `decompose` action through board.RunRequirement.
	// board.RunRequirement already switches on the action name and calls the
	// RunDecompose hook that this package installs.
	prev := board.RunRequirementDecompose
	board.RunRequirementDecompose = func(args []string) int {
		if prev != nil {
			return prev(args)
		}
		return RunDecompose(args)
	}
	_ = config.ExecutionAgents
	_ = cli.Commands
}
