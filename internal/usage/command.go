package usage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
)

func init() {
	cli.Commands["usage"] = RunUsage
}

// RunUsage implements `kander usage`.
//
// The command reads agent session logs and prints what was consumed. It is
// read-only by construction: it opens no agent process, calls no provider API and
// writes nothing to the board or to any agent store, so it is always safe to run
// while agents are working.
func RunUsage(args []string) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprintln(os.Stdout, config.Text("usage.messages.main"))
			return 0
		}
	}
	opts, jsonOut, err := parseUsageArgs(args)
	if err != nil {
		return usageError(err)
	}
	report, err := collect(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kander: %s\n", err)
		return 1
	}
	if jsonOut {
		out, err := FormatJSON(report)
		if err != nil {
			fmt.Fprintf(os.Stderr, "kander: %s\n", err)
			return 1
		}
		os.Stdout.WriteString(out)
		return 0
	}
	os.Stdout.WriteString(Format(report))
	return 0
}

// collect resolves the selection and reads the sources. Resolving the card here
// keeps the collector independent of the board.
func collect(opts Options) (Report, error) {
	if opts.Task == "" {
		return Collect(opts)
	}
	root, err := board.BoardRoot()
	if err != nil {
		return Report{}, err
	}
	snapshot, err := board.ReadSnapshot(root, opts.Task)
	if err != nil {
		return Report{}, errors.New(config.Text("usage.task_not_found", opts.Task))
	}
	// A card records the session of the agent that executed it as "<agent> <reference>",
	// the same shape internal/launch parses. The reference is the exact key and must be
	// matched on its own: for Codex it is a UUID that does not contain the task id, so
	// the task id is only a fallback for stores that name a session after the card.
	session := board.MetadataFrom(snapshot.Text, board.FieldSession)
	opts.SessionIDs = append(sessionReferences(session), opts.Task)
	if opts.Root == "" {
		opts.Root = filepath.Dir(root)
	}
	return Collect(opts)
}

// sessionReferences returns the session identity recorded on a card. The value is
// "<agent> <reference>"; a bare value is accepted as a reference on its own. Only the
// reference is returned: the agent name is a label, and matching on it would let a
// session id that merely contains the agent name match by accident.
func sessionReferences(value string) []string {
	parts := strings.Fields(value)
	switch len(parts) {
	case 0:
		return nil
	case 1:
		return []string{parts[0]}
	default:
		return []string{parts[1]}
	}
}

// parseUsageArgs reads the command line. Flags are accepted in any order.
func parseUsageArgs(args []string) (Options, bool, error) {
	opts := Options{Days: 7}
	jsonOut := false
	all := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json":
			jsonOut = true
		case arg == "--all":
			all = true
		case arg == "--task" || arg == "--agent" || arg == "--days":
			if i+1 >= len(args) {
				return opts, false, errors.New(config.Text("usage.missing_value", arg))
			}
			i++
			value := args[i]
			switch arg {
			case "--task":
				opts.Task = value
			case "--agent":
				opts.Agent = value
			case "--days":
				days, err := strconv.Atoi(value)
				if err != nil || days <= 0 {
					return opts, false, errors.New(config.Text("usage.days_must_be_a_positive_integer"))
				}
				opts.Days = days
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return opts, false, errors.New(config.Text("usage.unknown_option", arg))
			}
			return opts, false, errors.New(config.Text("usage.unexpected_argument", arg))
		}
	}
	// Without an explicit selection the report covers the project the board belongs
	// to; --all widens it to every session the stores hold.
	//
	// Outside a project there is no scope to default to, so the report widens to all
	// sessions instead of failing. The scope line always states which scope was used,
	// so the widening is visible rather than silent.
	if !all && opts.Root == "" && opts.Task == "" {
		if root, err := board.BoardRoot(); err == nil {
			opts.Root = filepath.Dir(root)
		}
	}
	return opts, jsonOut, nil
}

// usageError prints how to call the command and the specific complaint.
func usageError(err error) int {
	fmt.Fprintln(os.Stderr, config.Text("usage.messages.main"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return 2
}
