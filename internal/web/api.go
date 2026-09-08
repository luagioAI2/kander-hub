package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/dualface/kander/internal/board"
)

// requirementRow is the list-page digest of one requirement card.
type requirementRow struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Done   int    `json:"done"`
	Total  int    `json:"total"`
}

// linkedTask describes one linked task on the requirement detail page.
type linkedTask struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Missing bool   `json:"missing"`
}

// requirementDetail is the detail payload for one requirement.
type requirementDetail struct {
	ID      string       `json:"id"`
	Title   string       `json:"title"`
	Status  string       `json:"status"`
	Created string       `json:"created"`
	Summary string       `json:"summary"`
	Notes   string       `json:"notes"`
	Done    int          `json:"done"`
	Total   int          `json:"total"`
	Docs    []string     `json:"docs"`
	Linked  []linkedTask `json:"linked"`
}

// taskRow is one kanban card shown on the board tab.
type taskRow struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
	Type  string `json:"type"`
	Group string `json:"group"`
}

// taskDetail is the payload shown when the user clicks a task card: the
// raw document plus the requirement(s) that link back to it.
type taskDetail struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	State        string         `json:"state"`
	Type         string         `json:"type"`
	Group        string         `json:"group"`
	Document     string         `json:"document"`
	Requirements []reqBackref   `json:"requirements"`
}

// reqBackref names one requirement that links to this task.
type reqBackref struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// boardView groups tasks by state for the kanban tab.
type boardView struct {
	States     []string            `json:"states"`
	Tasks      map[string][]taskRow `json:"tasks"`
}

// liveRequirement re-derives progress/status for a parsed card.
func liveRequirement(root string, req board.Requirement) (board.Requirement, error) {
	return board.RequirementStatus(req, root)
}

// listRequirements handles GET /api/requirements.
func (s *Server) listRequirements(w http.ResponseWriter, r *http.Request) {
	reqs, problems, err := board.LoadRequirements(s.Root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]requirementRow, 0, len(reqs))
	for _, req := range reqs {
		live, lerr := liveRequirement(s.Root, req)
		if lerr != nil {
			live = req
		}
		rows = append(rows, requirementRow{
			ID:     live.ID,
			Title:  live.Title,
			Status: live.Status,
			Done:   live.Done,
			Total:  live.Total,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"requirements": rows,
		"problems":     problems,
	})
}

// getRequirement handles GET /api/requirements/{id}.
func (s *Server) getRequirement(w http.ResponseWriter, r *http.Request) {
	id := idFromPath(r.URL.Path, "/api/requirements/")
	if id == "" {
		http.Error(w, "requirement id required", http.StatusBadRequest)
		return
	}
	detail, err := s.requirementDetail(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// requirementDetail assembles the detail payload, expanding linked tasks and
// task groups into per-task refs.
func (s *Server) requirementDetail(id string) (requirementDetail, error) {
	reqs, _, err := board.LoadRequirements(s.Root)
	if err != nil {
		return requirementDetail{}, err
	}
	var req board.Requirement
	found := false
	for _, candidate := range reqs {
		if candidate.ID == id {
			req, found = candidate, true
			break
		}
	}
	if !found {
		return requirementDetail{}, errors.New("requirement not found: " + id)
	}
	live, err := liveRequirement(s.Root, req)
	if err != nil {
		live = req
	}
	linked, lerr := s.linkedTasks(live)
	if lerr != nil {
		return requirementDetail{}, lerr
	}
	return requirementDetail{
		ID:      live.ID,
		Title:   live.Title,
		Status:  live.Status,
		Created: live.Created,
		Summary: live.Summary,
		Notes:   live.Notes,
		Done:    live.Done,
		Total:   live.Total,
		Docs:    live.Docs,
		Linked:  linked,
	}, nil
}

// linkedTasks expands the requirement's TASKS and TASK_GROUPS fields into
// per-task refs, marking entries that no longer exist.
func (s *Server) linkedTasks(req board.Requirement) ([]linkedTask, error) {
	linked := append([]string{}, req.Tasks...)
	scan, err := board.Scan(s.Root)
	if err != nil {
		return nil, err
	}
	texts := map[string]string{}
	for taskID := range scan.Entries {
		text, derr := scan.Document(taskID)
		if derr != nil {
			return nil, derr
		}
		texts[taskID] = text
	}
	groupMembers := map[string][]string{}
	for taskID, text := range texts {
		if group := board.TaskGroupFrom(text); group != "" {
			groupMembers[group] = append(groupMembers[group], taskID)
		}
	}
	for _, group := range req.Groups {
		linked = append(linked, groupMembers[group]...)
	}
	seen := map[string]bool{}
	out := make([]linkedTask, 0, len(linked))
	for _, taskID := range linked {
		if seen[taskID] {
			continue
		}
		seen[taskID] = true
		entry, ok := scan.Entries[taskID]
		if !ok {
			out = append(out, linkedTask{ID: taskID, Missing: true})
			continue
		}
		out = append(out, linkedTask{
			ID:    taskID,
			Title: board.TitleFrom(texts[taskID]),
			State: entry.State,
		})
	}
	return out, nil
}

// createRequest is the JSON body for new-requirement and new-task calls.
type createRequest struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Kind    string `json:"kind"`
	Large   bool   `json:"large"`
}

// createRequirement handles POST /api/requirements.
func (s *Server) createRequirement(w http.ResponseWriter, r *http.Request) {
	var body createRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// The SOURCE field has been retired from the UI. The storage layer now
	// normalises an empty source to "N/A", so callers no longer need to
	// invent a value.
	path, err := board.AddRequirement(s.Root, body.Slug, body.Title, "", body.Summary)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"path": path})
}

// importItem is one external requirement to merge into the pool.
type importItem struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// importResponse reports per-item outcomes so the UI can show partial failure.
type importResponse struct {
	Created []string          `json:"created"`
	Failed  map[string]string `json:"failed"`
}

// importRequirements handles POST /api/requirements/import. The body is an
// array of importItem values; items are processed independently so one bad
// row does not block the rest.
func (s *Server) importRequirements(w http.ResponseWriter, r *http.Request) {
	var items []importItem
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	resp := importResponse{Created: []string{}, Failed: map[string]string{}}
	for _, item := range items {
		path, err := board.AddRequirement(s.Root, item.Slug, item.Title, "", item.Summary)
		if err != nil {
			resp.Failed[item.Title] = err.Error()
			continue
		}
		resp.Created = append(resp.Created, path)
	}
	writeJSON(w, http.StatusOK, resp)
}

// convertRequest identifies the requirement to convert and the task targets.
type convertRequest struct {
	ID     string   `json:"id"`
	Tasks  []string `json:"tasks"`
	Groups []string `json:"groups"`
}

// convertRequirement handles POST /api/requirements/convert.
func (s *Server) convertRequirement(w http.ResponseWriter, r *http.Request) {
	var body convertRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	path, err := board.ConvertRequirement(s.Root, body.ID, body.Tasks, body.Groups)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

// completeRequest identifies the requirement to complete.
type completeRequest struct {
	ID string `json:"id"`
}

// completeRequirement handles POST /api/requirements/complete.
func (s *Server) completeRequirement(w http.ResponseWriter, r *http.Request) {
	var body completeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	path, err := board.SetRequirementStatus(s.Root, body.ID, board.ReqStatusCompleted)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

// decomposeRequest identifies the requirement the user wants the agent to
// decompose. Message is the additional guidance passed to the agent; if empty
// the user must supply a file path. The agent prompt is built by the launch
// package; web only dispatches into the registered hook.
type decomposeRequest struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// decomposeRequirement handles POST /api/requirements/decompose: spawns a
// long-running Agent session that decomposes the requirement into N task
// cards. The endpoint returns immediately after the agent is launched; the
// user follows progress through their existing launcher (tmux pane, herdr
// tab, console window).
func (s *Server) decomposeRequirement(w http.ResponseWriter, r *http.Request) {
	if board.RunRequirementDecompose == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("requirement decompose backend not available"))
		return
	}
	var body decomposeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.ID == "" || body.Message == "" {
		writeError(w, http.StatusBadRequest, errors.New("id and message are required"))
		return
	}
	args := []string{"--message", body.Message, body.ID}
	if rc := board.RunRequirementDecompose(args); rc != 0 {
		writeError(w, http.StatusBadRequest, errors.New("decompose failed; check the launcher pane for details"))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "launched", "id": body.ID})
}

// getBoard handles GET /api/board: the full kanban snapshot grouped by state.
func (s *Server) getBoard(w http.ResponseWriter, r *http.Request) {
	scan, err := board.Scan(s.Root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	view := boardView{
		States: board.States,
		Tasks:  map[string][]taskRow{},
	}
	for _, state := range board.States {
		view.Tasks[state] = []taskRow{}
	}
	for _, entry := range scan.Entries {
		text, derr := scan.Document(entry.TaskID)
		if derr != nil {
			continue
		}
		row := taskRow{
			ID:    entry.TaskID,
			Title: board.TitleFrom(text),
			State: entry.State,
			Group: board.TaskGroupFrom(text),
			Type:  board.MetadataFrom(text, board.FieldType),
		}
		view.Tasks[entry.State] = append(view.Tasks[entry.State], row)
	}
	writeJSON(w, http.StatusOK, view)
}

// createTask handles POST /api/tasks: create a kanban task card directly
// (this is the "requirement converts to a task" fast path).
func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var body createRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.Kind == "" {
		body.Kind = "feature"
	}
	path, err := board.NewTask(s.Root, body.Kind, body.Slug, body.Title, "cn", body.Large)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"path": path})
}

// getTask handles GET /api/tasks/{id}: returns the full task document plus
// any requirement that links back to it. Used by the board click handler.
func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	id := idFromPath(r.URL.Path, "/api/tasks/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("task id required"))
		return
	}
	detail, err := s.taskDetail(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// taskDetail reads one task card and scans requirements for back-refs.
func (s *Server) taskDetail(id string) (taskDetail, error) {
	scan, err := board.Scan(s.Root)
	if err != nil {
		return taskDetail{}, err
	}
	entry, ok := scan.Entries[id]
	if !ok {
		return taskDetail{}, errors.New("task not found: " + id)
	}
	text, err := scan.Document(id)
	if err != nil {
		return taskDetail{}, err
	}
	reqs, _, rerr := board.LoadRequirements(s.Root)
	if rerr != nil {
		return taskDetail{}, rerr
	}
	backrefs := []reqBackref{}
	for _, req := range reqs {
		live, _ := liveRequirement(s.Root, req)
		linked, lerr := s.linkedTasks(live)
		if lerr != nil {
			continue
		}
		for _, link := range linked {
			if link.ID == id && !link.Missing {
				backrefs = append(backrefs, reqBackref{
					ID:     live.ID,
					Title:  live.Title,
					Status: live.Status,
				})
				break
			}
		}
	}
	return taskDetail{
		ID:           entry.TaskID,
		Title:        board.TitleFrom(text),
		State:        entry.State,
		Group:        board.TaskGroupFrom(text),
		Type:         board.MetadataFrom(text, board.FieldType),
		Document:     text,
		Requirements: backrefs,
	}, nil
}
