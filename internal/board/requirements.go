// Requirements pool: a standalone, minimal store at <board>/requirements/.
// Requirement cards are deliberately separate from kanban task cards: they never
// enter the backlog/todo/working/review/done lifecycle, are never started by an
// agent, and only record the source of a requirement, its decomposition status
// and the linked tasks or task groups. This file owns storage, parsing and the
// completion/progress derivation so callers stay thin.
package board

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

const (
	RequirementsDir = "requirements"

	// Requirement status values. A card starts as "draft" (not yet decomposed),
	// becomes "decomposed" when the user runs `kander req convert`, and turns
	// "completed" once every linked task is done. "archived" is terminal.
	ReqStatusDraft      = "draft"
	ReqStatusDecomposed = "decomposed"
	ReqStatusCompleted  = "completed"
	ReqStatusArchived   = "archived"

	FieldReqTitle     = "TITLE"
	FieldReqSource    = "SOURCE"
	FieldReqStatus    = "STATUS"
	FieldReqCreatedAt = "CREATED_AT"
	FieldReqDocs      = "DOCS"
	FieldReqTasks     = "TASKS"
	FieldReqGroups    = "TASK_GROUPS"

	ReqSectionSummary = "SUMMARY"
	ReqSectionNotes   = "NOTES"
)

var (
	reqIDRe = regexp.MustCompile(`^\d{8}-[a-z0-9]+(?:-[a-z0-9]+)*-req$`)
)

// Requirement is one parsed requirement card.
type Requirement struct {
	ID      string
	Path    string
	Title   string
	Source  string
	Status  string
	Created string
	Docs    []string
	Tasks   []string
	Groups  []string
	// Done and Total describe the linked task expansion used for progress;
	// Done counts linked task cards currently in the "done" state.
	Done  int
	Total int
	// Missing lists linked task IDs that no longer exist on the board.
	Missing []string
	// Summary and Notes hold the two free-text sections of the card.
	Summary string
	Notes   string
}

func requirementsRoot(root string) string {
	return filepath.Join(root, RequirementsDir)
}

// EnsureRequirements creates the requirements directory; it is a plain board
// subdirectory, so no state layout or migration is involved.
func EnsureRequirements(root string) error {
	path := filepath.Join(root, RequirementsDir)
	if isDirNoFollow(path) {
		return nil
	}
	if existsNoFollow(path) {
		return kanbanError("board.req_path_is_not_a_directory", path)
	}
	return createStateDirectory(path)
}

func splitIDList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseRequirement(id, path, text string) Requirement {
	title := untitled()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "# ") {
			title = strings.TrimSpace(line[2:])
			break
		}
	}
	req := Requirement{
		ID:      id,
		Path:    path,
		Title:   title,
		Source:  MetadataFrom(text, FieldReqSource),
		Status:  MetadataFrom(text, FieldReqStatus),
		Created: MetadataFrom(text, FieldReqCreatedAt),
		Docs:    splitIDList(MetadataFrom(text, FieldReqDocs)),
		Tasks:   splitIDList(MetadataFrom(text, FieldReqTasks)),
		Groups:  splitIDList(MetadataFrom(text, FieldReqGroups)),
	}
	if body, ok := SectionBody(text, ReqSectionSummary); ok {
		req.Summary = body
	}
	if body, ok := SectionBody(text, ReqSectionNotes); ok {
		req.Notes = body
	}
	if req.Status == "" {
		req.Status = ReqStatusDraft
	}
	return req
}

// LoadRequirements scans kanban/requirements/*.md. A card that fails the ID or
// status validation is returned through RequirementsProblems instead of being
// silently dropped; the store stays fully outside the task card scan.
func LoadRequirements(root string) ([]Requirement, []Problem, error) {
	dir := filepath.Join(root, RequirementsDir)
	items, err := os.ReadDir(dir)
	if err != nil {
		if isNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, wrapFS(err, "board.state_directory_does_not_exist", dir)
	}
	var reqs []Requirement
	var problems []Problem
	for _, item := range items {
		name := item.Name()
		if !item.Type().IsRegular() || !strings.HasSuffix(name, ".md") {
			continue
		}
		path := filepath.Join(dir, name)
		if fs.IsReparsePoint(path) {
			problems = append(problems, Problem{Path: path, Message: t("board.task_entry_must_not_be_a_symlink_reparse_point", path)})
			continue
		}
		data, err := fs.ReadRegularFile(root, path)
		if err != nil {
			problems = append(problems, Problem{Path: path, Message: t("board.invalid_task_entry", path)})
			continue
		}
		id := strings.TrimSuffix(name, ".md")
		if !reqIDRe.MatchString(id) {
			problems = append(problems, Problem{Path: path, Message: t("board.req_invalid_requirement_id", id)})
			continue
		}
		req := parseRequirement(id, path, string(data))
		switch req.Status {
		case ReqStatusDraft, ReqStatusDecomposed, ReqStatusCompleted, ReqStatusArchived:
		default:
			problems = append(problems, Problem{Path: path, Message: t("board.req_invalid_status", req.Status)})
			continue
		}
		reqs = append(reqs, req)
	}
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].ID < reqs[j].ID })
	return reqs, problems, nil
}

// requirementLockScope locks the whole requirements pool with one dedicated
// lock file. Requirement cards are single-file documents; the full task-card
// transaction journal is unnecessary for them, but concurrent CLI writes still
// need mutual exclusion. The returned release function also closes the file.
func requirementLock(root string) (func(), error) {
	if err := ensureControl(root); err != nil {
		return nil, err
	}
	lockPath := control(root, "locks", "requirements.lock")
	f, err := fs.OpenLockFile(root, lockPath)
	if err != nil {
		return nil, err
	}
	lock, err := fs.LockExclusive(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = lock.Unlock(); _ = f.Close() }, nil
}

func validateRequirementID(value string) (string, error) {
	value = strings.TrimSuffix(value, ".md")
	if !reqIDRe.MatchString(value) {
		return "", kanbanError("board.req_invalid_requirement_id", value)
	}
	return value, nil
}

func requirementPath(root, id string) string {
	return filepath.Join(root, RequirementsDir, id+".md")
}

func renderRequirementCard(req Requirement) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", req.Title)
	fmt.Fprintf(&b, "- %s: %s\n", FieldReqSource, req.Source)
	fmt.Fprintf(&b, "- %s: %s\n", FieldReqStatus, req.Status)
	fmt.Fprintf(&b, "- %s: %s\n", FieldReqCreatedAt, req.Created)
	fmt.Fprintf(&b, "- %s: %s\n", FieldReqDocs, strings.Join(req.Docs, ", "))
	fmt.Fprintf(&b, "- %s: %s\n", FieldReqTasks, strings.Join(req.Tasks, ", "))
	fmt.Fprintf(&b, "- %s: %s\n", FieldReqGroups, strings.Join(req.Groups, ", "))
	b.WriteString("\n## " + ReqSectionSummary + "\n\n")
	if req.Summary == "" {
		b.WriteString(Placeholder + "\n")
	} else {
		b.WriteString(req.Summary + "\n")
	}
	b.WriteString("\n## " + ReqSectionNotes + "\n\n")
	if req.Notes == "" {
		b.WriteString("N/A\n")
	} else {
		b.WriteString(req.Notes + "\n")
	}
	return b.String()
}

// AddRequirement creates a new requirement card in the draft state and returns its path.
func AddRequirement(root, slug, title, source, summary string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || strings.ContainsAny(title, "\n\r") {
		return "", kanbanError("board.title_must_not_be_empty_or_contain_newlines")
	}
	if source == "" {
		// An empty source is accepted as the new "no source" sentinel; the
		// storage layer normalises it to "N/A" so the front-matter stays
		// well-formed without forcing the caller to invent a value.
		source = "N/A"
	}
	if source != "N/A" && strings.ContainsAny(source, "\n\r") {
		return "", kanbanError("board.transaction_invalid", "SOURCE")
	}
	if strings.ContainsAny(summary, "\r") {
		return "", kanbanError("board.transaction_invalid", "SUMMARY")
	}
	id := todayPrefix() + "-" + slug + "-req"
	release, err := requirementLock(root)
	if err != nil {
		return "", err
	}
	defer release()
	if err = EnsureRequirements(root); err != nil {
		return "", err
	}
	reqs, _, err := LoadRequirements(root)
	if err != nil {
		return "", err
	}
	for _, req := range reqs {
		if req.ID == id {
			return "", kanbanError("board.req_already_exists", id)
		}
	}
	card := renderRequirementCard(Requirement{
		Title:   title,
		Source:  source,
		Status:  ReqStatusDraft,
		Created: nowStamp(),
		Summary: summary,
	})
	path := requirementPath(root, id)
	if err = fs.WriteTextAtomic(root, path, card, true); err != nil {
		return "", wrapFS(err, "board.req_failed_to_write_requirement_card", path)
	}
	return path, nil
}

// requirementProgress derives Done/Total/Missing for one requirement by scanning
// the linked task cards on the board. Task groups expand through the existing
// TASK_GROUP membership so progress follows the live group composition.
func requirementProgress(req *Requirement, board Board, texts map[string]string) error {
	linked := append([]string{}, req.Tasks...)
	groupMembers := taskGroupMembers(texts)
	for _, group := range req.Groups {
		linked = append(linked, groupMembers[group]...)
	}
	linked = uniqueKeepOrder(linked)
	req.Total = len(linked)
	for _, id := range linked {
		entry, ok := board.Entries[id]
		if !ok {
			req.Missing = append(req.Missing, id)
			continue
		}
		if entry.State == "done" {
			req.Done++
		}
	}
	return nil
}

// RequirementStatus recomputes the live status of one requirement card from the
// board: all linked tasks done => completed; any linked tasks and at least one
// done => the decomposition stays "decomposed" with progress; no linked tasks
// keeps the stored status (draft). It reports the derived status without
// rewriting the file.
func RequirementStatus(req Requirement, root string) (Requirement, error) {
	board, err := Scan(root)
	if err != nil {
		return req, err
	}
	texts := map[string]string{}
	for id := range board.Entries {
		text, err := board.Document(id)
		if err != nil {
			return req, err
		}
		texts[id] = text
	}
	if err = requirementProgress(&req, board, texts); err != nil {
		return req, err
	}
	return req, nil
}

// RequirementProgressLine renders the "done/total" progress token for list output.
func RequirementProgressLine(req Requirement) string {
	if req.Total == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", req.Done, req.Total)
}

// SetRequirementStatus writes a new status onto an existing requirement card.
func SetRequirementStatus(root, id, status string) (string, error) {
	id, err := validateRequirementID(id)
	if err != nil {
		return "", err
	}
	release, err := requirementLock(root)
	if err != nil {
		return "", err
	}
	defer release()
	path := requirementPath(root, id)
	data, err := fs.ReadRegularFile(root, path)
	if err != nil {
		return "", wrapFS(err, "board.req_requirement_not_found", id)
	}
	text := string(data)
	current := MetadataFrom(text, FieldReqStatus)
	if current == ReqStatusCompleted && status == ReqStatusDecomposed {
		return "", kanbanError("board.req_already_completed", id)
	}
	next, err := setMetadata(text, FieldReqStatus, status)
	if err != nil {
		return "", err
	}
	if err = fs.WriteTextAtomic(root, path, next, true); err != nil {
		return "", wrapFS(err, "board.req_failed_to_write_requirement_card", path)
	}
	return path, nil
}

// LinkRequirementTargets appends task IDs and/or a task group ID to a
// requirement card. Duplicates are ignored; every task ID must exist on the
// board, and the group ID must be a syntactically valid group reference.
func LinkRequirementTargets(root, id string, tasks []string, groups []string, unset bool) (string, error) {
	id, err := validateRequirementID(id)
	if err != nil {
		return "", err
	}
	release, err := requirementLock(root)
	if err != nil {
		return "", err
	}
	defer release()
	path := requirementPath(root, id)
	data, err := fs.ReadRegularFile(root, path)
	if err != nil {
		return "", wrapFS(err, "board.req_requirement_not_found", id)
	}
	text := string(data)
	if unset {
		text, err = setMetadata(text, FieldReqTasks, "")
		if err != nil {
			return "", err
		}
		text, err = setMetadata(text, FieldReqGroups, "")
		if err != nil {
			return "", err
		}
		if err = fs.WriteTextAtomic(root, path, text, true); err != nil {
			return "", wrapFS(err, "board.req_failed_to_write_requirement_card", path)
		}
		return path, nil
	}
	req := parseRequirement(id, path, text)
	board, err := Scan(root)
	if err != nil {
		return "", err
	}
	for _, taskID := range tasks {
		taskID, err = NormalizeTaskID(taskID)
		if err != nil {
			return "", err
		}
		if _, ok := board.Entries[taskID]; !ok {
			return "", kanbanError("board.task_does_not_exist", taskID)
		}
		req.Tasks = uniqueKeepOrder(append(req.Tasks, taskID))
	}
	for _, group := range groups {
		if !taskGroupRe.MatchString(group) {
			return "", kanbanError("board.transaction_invalid", group)
		}
		req.Groups = uniqueKeepOrder(append(req.Groups, group))
	}
	next, err := setMetadata(text, FieldReqTasks, strings.Join(req.Tasks, ", "))
	if err != nil {
		return "", err
	}
	next, err = setMetadata(next, FieldReqGroups, strings.Join(req.Groups, ", "))
	if err != nil {
		return "", err
	}
	if err = fs.WriteTextAtomic(root, path, next, true); err != nil {
		return "", wrapFS(err, "board.req_failed_to_write_requirement_card", path)
	}
	return path, nil
}

// ConvertRequirement decomposes a requirement: the user supplies the task IDs
// and/or task groups that implement it, the card records them and moves to
// "decomposed". Use LinkRequirementTargets first when tasks are created later.
func ConvertRequirement(root, id string, tasks, groups []string) (string, error) {
	if len(tasks) == 0 && len(groups) == 0 {
		return "", kanbanError("board.req_convert_requires_targets")
	}
	if _, err := LinkRequirementTargets(root, id, tasks, groups, false); err != nil {
		return "", err
	}
	return SetRequirementStatus(root, id, ReqStatusDecomposed)
}

// RemoveRequirement deletes a requirement card. Completed cards are protected.
func RemoveRequirement(root, id string) (string, error) {
	id, err := validateRequirementID(id)
	if err != nil {
		return "", err
	}
	release, err := requirementLock(root)
	if err != nil {
		return "", err
	}
	defer release()
	path := requirementPath(root, id)
	data, err := fs.ReadRegularFile(root, path)
	if err != nil {
		return "", wrapFS(err, "board.req_requirement_not_found", id)
	}
	if MetadataFrom(string(data), FieldReqStatus) == ReqStatusCompleted {
		return "", kanbanError("board.req_completed_cards_cannot_be_removed", id)
	}
	if _, err := fs.RemoveRegularFileIfExists(root, path); err != nil {
		return "", wrapFS(err, "board.req_failed_to_remove_requirement_card", path)
	}
	return path, nil
}
