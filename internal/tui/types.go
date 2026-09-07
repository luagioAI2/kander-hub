package tui

import (
	"encoding/json"
	"strings"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)
const (
	cardHeight = 4
	// Minimum column width. Cards narrower than this are already unreadable, so showing one column fewer is preferable.
	minColumnWidth     = config.DefaultTUIMinColumnWidth
	minMinColumnWidth  = config.MinTUIMinColumnWidth
	maxMinColumnWidth  = config.MaxTUIMinColumnWidth
	minColumnWidthStep = 4
	// The user configures the number of columns on screen; the width comes from splitting the terminal width evenly.
	defaultColumns = config.DefaultTUIColumns
	minColumns     = config.MinTUIColumns
	minBoardHeight = 8
	headerRow      = 0
	spacerRow      = 1
	panelTopRow    = 2
	bodyTop        = 3
	// Detail panel: the top border, the metadata line, the separator, and only then the body.
	detailPanelTopRow  = 2
	detailMetaRow      = 3
	detailRuleRow      = 4
	detailBodyTop      = 5
	mouseScrollStep    = 3
	copyNoticeSeconds  = 2.0
	defaultRefreshSecs = config.DefaultTUIRefresh
	minRefreshSecs     = config.MinTUIRefresh
	maxRefreshSecs     = config.MaxTUIRefresh
	refreshStep        = 5
)

var (
	activeStates = []string{"backlog", "todo", "working", "review", "done"}
	allStates    = append([]string(nil), board.States...)
	themes       = config.TUIThemes
	// RequirementColumn is a virtual column dedicated to the requirements pool.
	// Cards in this column are RequirementSummary entries, never TaskSummary.
	RequirementColumn = "requirements"
)

// Type aliases keep the rest of the TUI package agnostic to whether the
// underlying types come from the board or requirement packages.
type (
	Task              = board.TaskSummary
	BoardPayload      = board.BoardView
	Requirement       = board.RequirementSummary
	RequirementLinked = board.RequirementLinkedRef
)

func knownState(state string) bool {
	if state == RequirementColumn {
		return true
	}
	for _, item := range allStates {
		if item == state {
			return true
		}
	}
	return false
}

func stateIndex(state string) int {
	for i, item := range allStates {
		if item == state {
			return i
		}
	}
	if state == RequirementColumn {
		return len(allStates)
	}
	return len(allStates) + 1
}

func boardContentKey(tasks []Task, requirements []Requirement) string {
	type row map[string]string
	rows := make([]row, 0, len(tasks))
	for _, task := range tasks {
		item := row{
			"assignee":     task.Assignee,
			"completed_at": task.CompletedAt,
			"created_at":   task.CreatedAt,
			"document":     task.Document,
			"kind":         task.Kind,
			"result":       task.Result,
			"started_at":   task.StartedAt,
			"state":        task.State,
			"task_group":   task.TaskGroup,
			"task_id":      task.TaskID,
			"time":         task.Time,
			"title":        task.Title,
			"type":         task.Type,
		}
		if task.Document == "" {
			delete(item, "document")
		}
		rows = append(rows, item)
	}
	requirementRows := make([]row, 0, len(requirements))
	for _, req := range requirements {
		links := make([]string, 0, len(req.Linked))
		for _, link := range req.Linked {
			links = append(links, link.ID+"="+link.State)
		}
		item := row{
			"requirement_id": req.RequirementID,
			"title":          req.Title,
			"status":         req.Status,
			"source":         req.Source,
			"created_at":     req.CreatedAt,
			"done":           itoa(req.Done),
			"total":          itoa(req.Total),
			"links":          strings.Join(links, ","),
		}
		requirementRows = append(requirementRows, item)
	}
	data, err := json.Marshal(struct {
		Tasks        []row `json:"t"`
		Requirements []row `json:"r"`
	}{rows, requirementRows})
	if err != nil {
		return ""
	}
	return string(data)
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// maxColumns is the upper bound on the column count: showing every active column is as far as it goes.
func maxColumns() int {
	return config.MaxTUIColumns
}

func clampColumns(count int) int {
	if count < minColumns {
		return minColumns
	}
	if limit := maxColumns(); count > limit {
		return limit
	}
	return count
}

func clampMinColumnWidth(width int) int {
	if width < minMinColumnWidth {
		return minMinColumnWidth
	}
	if width > maxMinColumnWidth {
		return maxMinColumnWidth
	}
	return width
}

func clampRefresh(seconds int) int {
	if seconds < minRefreshSecs {
		return minRefreshSecs
	}
	if seconds > maxRefreshSecs {
		return maxRefreshSecs
	}
	return seconds
}

func themeIndex(name string) int {
	for i, item := range themes {
		if item == name {
			return i
		}
	}
	return 0
}

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, " | ")
}
