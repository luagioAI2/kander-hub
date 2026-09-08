package reqtui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// Run implements the `kander req tui` entry point. It resolves the board root,
// starts the requirements-pool TUI, and returns a process exit code.
func Run(args []string) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprintln(os.Stdout, config.Text("reqtui.usage"))
			return 0
		}
	}
	root, err := board.RequireBoardRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := board.EnsureRequirements(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	m := New(root)
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, config.Text("reqtui.run_failed"))
		return 1
	}
	return 0
}

func init() {
	// board exposes the hook so this package can be linked without an import
	// cycle (reqtui imports board for the requirement store and launch hook).
	board.RunReqTUI = Run
}