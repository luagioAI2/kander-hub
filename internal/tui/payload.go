package tui

import (
	"github.com/dualface/kander/internal/board"
)

func loadBoardPayload(root string) (BoardPayload, error) {
	return board.BoardPayload(root)
}

func loadTaskPayload(root, taskID string) (Task, error) {
	return board.TaskPayload(root, taskID)
}

// loadRequirementPayload loads a single requirement card plus its live linked
// tasks so the TUI detail panel can render both the markdown body and the
// decomposition status.
func loadRequirementPayload(root, requirementID string) (Requirement, error) {
	return board.RequirementPayload(root, requirementID)
}
