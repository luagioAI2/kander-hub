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
	// FieldReqAttach records user-supplied attachment paths (files or
	// directories such as screenshots or spec documents), comma-separated.
	// The paths are stored verbatim; the pool never copies the targets.
	FieldReqAttach = "ATTACHMENTS"
	// FieldReqWindow mirrors board.FieldWindow so the requirement card can
	// record the launcher window of its decompose session for quick switch.
	FieldReqWindow = "WINDOW"
	// FieldReqMode records whether the decompose session runs as a
	// collaborative orchestrator (asks the user to confirm each proposed task
	// card before kander new) or as an autonomous orchestrator (proposes,
	// then drives kander new directly). Valid values: "collaborative",
	// "autonomous". Empty defaults to "collaborative".
	FieldReqMode = "MODE"

	// FieldReqSession mirrors board.FieldSession for requirement cards so the
	// decompose agent's identity can be persisted next to the requirement and
	// later resumed by kander req resume.
	FieldReqSession = "SESSION"
	// FieldReqLastDecompose records the last decompose run timestamp on a
	// requirement card. It is updated by the launch pipeline after each
	// decompose command so the user can see when the agent last worked on it.
	FieldReqLastDecompose = "LAST_DECOMPOSE"

	ReqSectionSummary = "SUMMARY"
	ReqSectionNotes   = "NOTES"
	// ReqSectionProposed stores task card spec.md drafts produced by the
	// decompose orchestrator. Each draft lives under its own
	// "## <task-id>" heading so kander can locate them without re-parsing.
	ReqSectionProposed = "PROPOSED_TASKS"
)

const (
	// ReqModeCollaborative makes the orchestrator present each draft to the
	// user and only kander new after an explicit confirmation.
	ReqModeCollaborative = "collaborative"
	// ReqModeAutonomous makes the orchestrator run without confirmation and
	// drive kander new + req convert directly from the drafts.
	ReqModeAutonomous = "autonomous"
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
	// Session is the agent identity that last worked on this requirement; it
	// is set by the launch pipeline when kander req decompose runs.
	Session string
	// LastDecompose is the timestamp of the last decompose launch; it is set
	// by the launch pipeline after a successful decompose run.
	LastDecompose string
	// Attach lists user-supplied attachment paths stored verbatim on the card.
	Attach []string
	// Window is the launcher window address of the last decompose session,
	// written by the launch pipeline (tmux:session:window:pane or herdr:tab:pane).
	Window string
	// Mode is the orchestrator collaboration mode: "collaborative" (default)
	// or "autonomous". Empty is treated as collaborative.
	Mode string
	// Proposed holds draft task card bodies keyed by their draft slug.
	// The orchestrator fills this section in PROPOSED_TASKS; the front-end
	// renders one column per draft and the user confirms or rejects each.
	Proposed map[string]string
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
		ID:            id,
		Path:          path,
		Title:         title,
		Source:        MetadataFrom(text, FieldReqSource),
		Status:        MetadataFrom(text, FieldReqStatus),
		Created:       MetadataFrom(text, FieldReqCreatedAt),
		Docs:          splitIDList(MetadataFrom(text, FieldReqDocs)),
		Tasks:         splitIDList(MetadataFrom(text, FieldReqTasks)),
		Groups:        splitIDList(MetadataFrom(text, FieldReqGroups)),
		Session:       MetadataFrom(text, FieldReqSession),
		LastDecompose: MetadataFrom(text, FieldReqLastDecompose),
		Attach:        splitIDList(MetadataFrom(text, FieldReqAttach)),
		Window:        MetadataFrom(text, FieldReqWindow),
		Mode:          MetadataFrom(text, FieldReqMode),
	}
	if body, ok := SectionBody(text, ReqSectionSummary); ok {
		req.Summary = body
	}
	if body, ok := SectionBody(text, ReqSectionNotes); ok {
		req.Notes = body
	}
	if start, end, ok := proposedSectionSpan(text); ok {
		req.Proposed = parseProposedTasks(text[start:end])
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

// ReadRequirementDocument reads a requirement card as UTF-8 text. It is the
// requirement counterpart of ReadDocument and is exported because the launch
// pipeline needs it to update metadata before the Agent reads the card.
func ReadRequirementDocument(root, id string) (string, error) {
	id, err := validateRequirementID(id)
	if err != nil {
		return "", err
	}
	path := requirementPath(root, id)
	data, err := fs.ReadRegularFile(root, path)
	if err != nil {
		return "", wrapFS(err, "board.req_requirement_not_found", id)
	}
	return string(data), nil
}

// WriteRequirementDocument overwrites a requirement card atomically. Mirrors
// WriteDocument so the launch pipeline can persist updated SESSION and
// LAST_DECOMPOSE fields.
func WriteRequirementDocument(root, id, text string) error {
	id, err := validateRequirementID(id)
	if err != nil {
		return err
	}
	path := requirementPath(root, id)
	return fs.WriteTextAtomic(root, path, text, true)
}

// AcquireRequirementLock is the exported form of requirementLock for the
// launch pipeline, which keeps its own copy of the root but should still
// serialise concurrent writes against the requirement pool.
func AcquireRequirementLock(root string) (func(), error) {
	return requirementLock(root)
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
	if req.Session != "" {
		fmt.Fprintf(&b, "- %s: %s\n", FieldReqSession, req.Session)
	}
	if req.LastDecompose != "" {
		fmt.Fprintf(&b, "- %s: %s\n", FieldReqLastDecompose, req.LastDecompose)
	}
	if len(req.Attach) > 0 {
		fmt.Fprintf(&b, "- %s: %s\n", FieldReqAttach, strings.Join(req.Attach, ", "))
	}
	if req.Window != "" {
		fmt.Fprintf(&b, "- %s: %s\n", FieldReqWindow, req.Window)
	}
	if req.Mode != "" {
		fmt.Fprintf(&b, "- %s: %s\n", FieldReqMode, req.Mode)
	}
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
	if len(req.Proposed) > 0 {
		b.WriteString("\n## " + ReqSectionProposed + "\n\n")
		b.WriteString("```text\n")
		slugs := make([]string, 0, len(req.Proposed))
		for s := range req.Proposed {
			slugs = append(slugs, s)
		}
		sort.Strings(slugs)
		for _, slug := range slugs {
			fmt.Fprintf(&b, "### %s\n\n", slug)
			b.WriteString(req.Proposed[slug])
			if !strings.HasSuffix(req.Proposed[slug], "\n") {
				b.WriteString("\n")
			}
		}
		b.WriteString("```\n")
	}
	return b.String()
}

// proposedSectionSpan returns [start,end) byte offsets of the PROPOSED_TASKS
// section (including the heading and any trailing blank line) in raw card
// text. The body lives inside a ```text fence, so ## headings inside the
// drafts are ignored; the section ends at the closing fence or the next
// top-level ## heading outside it, whichever comes first.
func proposedSectionSpan(text string) (int, int, bool) {
	lines := strings.Split(text, "\n")
	start := -1
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inFence {
			if trimmed == "## "+ReqSectionProposed {
				start = i
				continue
			}
			if start >= 0 && strings.HasPrefix(trimmed, "## ") {
				end := byteOffset(text, i)
				return byteOffset(text, start), end, true
			}
			if trimmed == "```text" || trimmed == "```" {
				inFence = true
			}
			continue
		}
		if trimmed == "```" {
			inFence = false
		}
	}
	if start < 0 {
		return 0, 0, false
	}
	return byteOffset(text, start), len(text), true
}

// byteOffset converts a zero-based line index into a byte offset in text.
func byteOffset(text string, lineIndex int) int {
	lines := strings.Split(text, "\n")
	off := 0
	for i := 0; i < lineIndex && i < len(lines); i++ {
		off += len(lines[i]) + 1
	}
	return off
}

// replaceProposedSection writes the PROPOSED_TASKS body (a fence-wrapped
// string, or empty to remove) in place of any existing section.
func replaceProposedSection(text, body string) (string, error) {
	heading := "## " + ReqSectionProposed
	if body == "" {
		start, end, ok := proposedSectionSpan(text)
		if !ok {
			return text, nil
		}
		out := text[:start]
		// Drop the blank line that separated the section from the previous one.
		out = strings.TrimRight(out, "\n")
		// Re-add nothing: preserve everything after `end` verbatim.
		return out + text[end:], nil
	}
	if start, end, ok := proposedSectionSpan(text); ok {
		return text[:start] + heading + "\n\n" + body + text[end:], nil
	}
	return strings.TrimRight(text, "\n") + "\n\n" + heading + "\n\n" + body, nil
}

// parseProposedTasks extracts each "### <slug>" section under PROPOSED_TASKS.
// The body is expected to live inside a ```text fence; the fence markers and
// the leading PROPOSED_TASKS heading are skipped. Returns an empty map (never
// nil) when the section is missing or empty.
func parseProposedTasks(body string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(body, "\n")
	active := false
	inFence := false
	var current string
	var buf strings.Builder
	flush := func() {
		if current == "" {
			return
		}
		out[current] = strings.TrimLeft(strings.TrimRight(buf.String(), "\n"), "\n") + "\n"
		if v := out[current]; v == "\n" {
			delete(out, current)
		}
	}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if !active {
			if line == "## "+ReqSectionProposed {
				active = true
			}
			continue
		}
		if !inFence {
			if line == "```text" || line == "```" {
				inFence = true
				continue
			}
			// Top-level section heading after PROPOSED_TASKS terminates it.
			if strings.HasPrefix(line, "## ") {
				break
			}
			continue
		}
		if line == "```" {
			inFence = false
			continue
		}
		if strings.HasPrefix(line, "### ") {
			flush()
			buf.Reset()
			current = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			continue
		}
		if current == "" {
			continue
		}
		buf.WriteString(raw)
		buf.WriteString("\n")
	}
	flush()
	return out
}

// SetProposedTasks replaces the PROPOSED_TASKS section with the supplied
// drafts. Slug keys are sorted on write so the section stays stable across
// repeated orchestrator runs.
func SetProposedTasks(root, id string, drafts map[string]string) (string, error) {
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
	var section string
	if len(drafts) > 0 {
		slugs := make([]string, 0, len(drafts))
		for s := range drafts {
			slugs = append(slugs, s)
		}
		sort.Strings(slugs)
		var b strings.Builder
		b.WriteString("```text\n")
		for _, slug := range slugs {
			fmt.Fprintf(&b, "### %s\n\n", slug)
			b.WriteString(drafts[slug])
			if !strings.HasSuffix(drafts[slug], "\n") {
				b.WriteString("\n")
			}
		}
		b.WriteString("```\n")
		section = b.String()
	}
	next, err := replaceProposedSection(string(data), section)
	if err != nil {
		return "", err
	}
	if err = fs.WriteTextAtomic(root, path, next, true); err != nil {
		return "", wrapFS(err, "board.req_failed_to_write_requirement_card", path)
	}
	return path, nil
}

// AddRequirement creates a new requirement card in the draft state and returns its path.
func AddRequirement(root, slug, title, source, summary string) (string, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !slugRe.MatchString(slug) {
		return "", kanbanError("board.slug_may_contain_only_lowercase_ascii_letters_digits_and")
	}
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

// SetRequirementAttachments replaces the ATTACHMENTS field with the given
// paths, stored verbatim (no copying or existence check; the launch prompt
// hands them to the agent as-is). An empty list clears the field.
func SetRequirementAttachments(root, id string, paths []string) (string, error) {
	id, err := validateRequirementID(id)
	if err != nil {
		return "", err
	}
	cleaned := uniqueKeepOrder(attachPaths(paths))
	for _, p := range cleaned {
		if strings.ContainsAny(p, "\n\r") {
			return "", kanbanError("board.transaction_invalid", FieldReqAttach)
		}
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
	next, err := setMetadata(string(data), FieldReqAttach, strings.Join(cleaned, ", "))
	if err != nil {
		return "", err
	}
	if err = fs.WriteTextAtomic(root, path, next, true); err != nil {
		return "", wrapFS(err, "board.req_failed_to_write_requirement_card", path)
	}
	return path, nil
}

// attachPaths trims and drops empty entries from an attachment path list.
func attachPaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SetRequirementMode writes the orchestrator collaboration mode. Valid values
// are "collaborative" and "autonomous"; an empty value clears the field.
func SetRequirementMode(root, id, mode string) (string, error) {
	id, err := validateRequirementID(id)
	if err != nil {
		return "", err
	}
	if mode != "" && mode != ReqModeCollaborative && mode != ReqModeAutonomous {
		return "", kanbanError("board.transaction_invalid", FieldReqMode)
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
	next, err := setMetadata(string(data), FieldReqMode, mode)
	if err != nil {
		return "", err
	}
	if err = fs.WriteTextAtomic(root, path, next, true); err != nil {
		return "", wrapFS(err, "board.req_failed_to_write_requirement_card", path)
	}
	return path, nil
}

// ParseRequirementAttachments reads the ATTACHMENTS metadata field from a
// raw requirement card text without a full parse; the launch prompt uses it
// to hand attachment paths to the decompose agent.
func ParseRequirementAttachments(text string) []string {
	return splitIDList(MetadataFrom(text, FieldReqAttach))
}

// ParseRequirementMode returns the orchestrator collaboration mode recorded
// on the card. Empty string means "not set"; callers should treat that as
// collaborative.
func ParseRequirementMode(text string) string {
	return MetadataFrom(text, FieldReqMode)
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
