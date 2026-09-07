package board

import (
	"os"
	"sort"
	"time"

	"github.com/dualface/kander/internal/fs"
)

// TaskSummary is the task digest used by tui; its fields match the onevoke payload.
type TaskSummary struct {
	TaskID      string `json:"task_id"`
	Title       string `json:"title"`
	State       string `json:"state"`
	Kind        string `json:"kind"`
	Type        string `json:"type"`
	TaskGroup   string `json:"task_group"`
	Assignee    string `json:"assignee"`
	CreatedAt   string `json:"created_at"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
	Time        string `json:"time"`
	Result      string `json:"result"`
	Document    string `json:"document,omitempty"`
}

// RequirementSummary is the digest the TUI renders for one requirement card.
// LinkedTasks describe each task or task-group member that the requirement
// has been decomposed into: State is its current board state, Missing means
// the linked ID has been removed from the board.
type RequirementSummary struct {
	RequirementID string                 `json:"requirement_id"`
	Title         string                 `json:"title"`
	Status        string                 `json:"status"`
	Source        string                 `json:"source"`
	CreatedAt     string                 `json:"created_at"`
	Docs          []string               `json:"docs,omitempty"`
	Done          int                    `json:"done"`
	Total         int                    `json:"total"`
	Document      string                 `json:"document,omitempty"`
	Linked        []RequirementLinkedRef `json:"linked,omitempty"`
}

// RequirementLinkedRef describes one linked task or task-group expansion.
type RequirementLinkedRef struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // "task" or "group"
	Title    string `json:"title"`
	State    string `json:"state"`
	Missing  bool   `json:"missing,omitempty"`
}

// BoardView is the read-only board JSON consumed by tui.
type BoardView struct {
	GeneratedAt  string             `json:"generated_at"`
	Root         string             `json:"root"`
	Tasks        []TaskSummary      `json:"tasks"`
	Requirements []RequirementSummary `json:"requirements,omitempty"`
}

// TaskDisplayTime matches onevoke task_display_time.
func TaskDisplayTime(entry Entry, text string) string {
	switch entry.State {
	case "working", "review":
		if value := MetadataFrom(text, FieldStartedAt); value != "" {
			return value
		}
		return "-"
	case "done":
		if value := MetadataFrom(text, FieldFinishedAt); value != "" {
			return value
		}
		info, err := os.Stat(entry.Document)
		if err == nil {
			return time.Unix(info.ModTime().Unix(), 0).Format("2006-01-02 15:04")
		}
		return "-"
	}
	if value := MetadataFrom(text, FieldCreatedAt); value != "" {
		return value
	}
	return "-"
}

// TaskSummaryOf builds a digest from an entry and its document.
func TaskSummaryOf(entry Entry, text string) TaskSummary {
	kind := entry.Kind
	if len(fieldLines(text, FieldSize)) > 0 {
		kind = attachSize(entry, text).Kind
	}
	if kind == "" {
		kind = "small"
	}
	taskType := MetadataFrom(text, FieldType)
	if taskType == "" {
		taskType = "-"
	}
	return TaskSummary{
		TaskID:      entry.TaskID,
		Title:       TitleFrom(text),
		State:       entry.State,
		Kind:        kind,
		Type:        taskType,
		TaskGroup:   taskGroupFrom(text),
		Assignee:    MetadataFrom(text, FieldOwner),
		CreatedAt:   MetadataFrom(text, FieldCreatedAt),
		StartedAt:   MetadataFrom(text, FieldStartedAt),
		CompletedAt: MetadataFrom(text, FieldFinishedAt),
		Time:        TaskDisplayTime(entry, text),
		Result:      resultFrom(text),
	}
}

func sortTaskSummaries(tasks []TaskSummary) {
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Time != tasks[j].Time {
			return tasks[i].Time > tasks[j].Time
		}
		return tasks[i].TaskID > tasks[j].TaskID
	})
	sort.SliceStable(tasks, func(i, j int) bool {
		return stateIndex(tasks[i].State) < stateIndex(tasks[j].State)
	})
}

// BoardPayload builds the payload from Scan, bypassing LoadBoard so no check warning is printed to the terminal.
func BoardPayload(root string) (BoardView, error) {
	scanned, err := Scan(root)
	if err != nil {
		return BoardView{}, err
	}
	tasks := make([]TaskSummary, 0, len(scanned.Entries))
	for _, entry := range scanned.Entries {
		text, err := scanned.Document(entry.TaskID)
		if err != nil {
			return BoardView{}, err
		}
		tasks = append(tasks, TaskSummaryOf(entry, text))
	}
	sortTaskSummaries(tasks)
	requirements, err := buildRequirementPayload(scanned, tasks, root)
	if err != nil {
		return BoardView{}, err
	}
	return BoardView{
		GeneratedAt:  time.Now().Format("2006-01-02 15:04:05"),
		Root:         root,
		Tasks:        tasks,
		Requirements: requirements,
	}, nil
}

// TaskPayload returns one card digest plus its body, also via Scan.
func TaskPayload(root, taskID string) (TaskSummary, error) {
	scanned, err := Scan(root)
	if err != nil {
		return TaskSummary{}, err
	}
	entry, err := Locate(scanned, taskID)
	if err != nil {
		return TaskSummary{}, err
	}
	text, err := scanned.Document(entry.TaskID)
	if err != nil {
		return TaskSummary{}, err
	}
	summary := TaskSummaryOf(entry, text)
	summary.Document = text
	return summary, nil
}

// RequirementPayload returns one requirement card digest plus its body and
// the live status of its linked tasks, in the same shape used by the TUI.
func RequirementPayload(root, requirementID string) (RequirementSummary, error) {
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		return RequirementSummary{}, err
	}
	for _, req := range reqs {
		if req.ID != requirementID {
			continue
		}
		board, err := Scan(root)
		if err != nil {
			return RequirementSummary{}, err
		}
		tasks := make([]TaskSummary, 0, len(board.Entries))
		for _, entry := range board.Entries {
			text, err := board.Document(entry.TaskID)
			if err != nil {
				return RequirementSummary{}, err
			}
			tasks = append(tasks, TaskSummaryOf(entry, text))
		}
		summary := buildRequirementSummary(req, board, tasks)
		data, err := fs.ReadRegularFile(root, req.Path)
		if err == nil {
			summary.Document = string(data)
		}
		return summary, nil
	}
	return RequirementSummary{}, &Error{Message: t("board.req_requirement_not_found", requirementID)}
}

// buildRequirementPayload converts requirement cards into the TUI digest and
// resolves the linked task/task-group expansion against the live board.
func buildRequirementPayload(board Board, tasks []TaskSummary, root string) ([]RequirementSummary, error) {
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		return nil, err
	}
	out := make([]RequirementSummary, 0, len(reqs))
	for _, req := range reqs {
		out = append(out, buildRequirementSummary(req, board, tasks))
	}
	return out, nil
}

func buildRequirementSummary(req Requirement, board Board, tasks []TaskSummary) RequirementSummary {
	summary := RequirementSummary{
		RequirementID: req.ID,
		Title:         req.Title,
		Status:        req.Status,
		Source:        req.Source,
		CreatedAt:     req.Created,
		Docs:          req.Docs,
	}
	if len(req.Docs) == 0 {
		summary.Docs = nil
	}
	texts := map[string]string{}
	for _, t := range tasks {
		entry, ok := board.Entries[t.TaskID]
		if !ok {
			continue
		}
		doc, err := board.Document(t.TaskID)
		if err != nil {
			continue
		}
		texts[t.TaskID] = doc
		_ = entry
	}
	groupMembers := taskGroupMembers(texts)
	index := map[string]TaskSummary{}
	for _, t := range tasks {
		index[t.TaskID] = t
	}
	seen := map[string]struct{}{}
	addLinked := func(id, kind string) {
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		if kind == "group" {
			for _, member := range groupMembers[id] {
				if _, used := seen[member]; used {
					continue
				}
				seen[member] = struct{}{}
				summary.Linked = append(summary.Linked, linkedFromTask(member, index))
			}
			return
		}
		summary.Linked = append(summary.Linked, linkedFromTask(id, index))
	}
	for _, id := range req.Tasks {
		addLinked(id, "task")
	}
	for _, id := range req.Groups {
		addLinked(id, "group")
	}
	summary.Total = len(summary.Linked)
	for _, link := range summary.Linked {
		if !link.Missing && link.State == "done" {
			summary.Done++
		}
	}
	if len(summary.Linked) == 0 {
		summary.Linked = nil
	}
	return summary
}

func linkedFromTask(id string, index map[string]TaskSummary) RequirementLinkedRef {
	link := RequirementLinkedRef{ID: id, Kind: "task"}
	if task, ok := index[id]; ok {
		link.Title = task.Title
		link.State = task.State
	} else {
		link.Title = id
		link.Missing = true
	}
	return link
}
