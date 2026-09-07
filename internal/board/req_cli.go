// kander req: the requirements-pool CLI. Requirement cards live outside the
// kanban lifecycle; this file only maps flags to the board.Requirement APIs and
// renders output. It never mutates task cards.
package board

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func reqUsage(w io.Writer, action string) {
	messages := map[string]string{
		"":         "board.messages.req",
		"new":      "board.messages.req-new",
		"list":     "board.messages.req-list",
		"show":     "board.messages.req-show",
		"convert":  "board.messages.req-convert",
		"link":     "board.messages.req-link",
		"unlink":   "board.messages.req-unlink",
		"remove":   "board.messages.req-remove",
		"complete": "board.messages.req-complete",
	}
	if id, ok := messages[action]; ok {
		fmt.Fprintln(w, t(id))
	}
}

func reqUsageFail(action, id string, args ...any) int {
	reqUsage(os.Stderr, action)
	if id != "" {
		fmt.Fprintln(os.Stderr, t(id, args...))
	}
	return 2
}

func splitCSV(value string) []string {
	return splitIDList(value)
}

func reqProgress(req Requirement) string {
	line := RequirementProgressLine(req)
	if line == "" {
		if req.Status == ReqStatusCompleted {
			return "100%"
		}
		return "-"
	}
	return line
}

type reqJSON struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Source  string   `json:"source"`
	Status  string   `json:"status"`
	Created string   `json:"created_at"`
	Docs    []string `json:"docs"`
	Tasks   []string `json:"tasks"`
	Groups  []string `json:"task_groups"`
	Done    int      `json:"done"`
	Total   int      `json:"total"`
	Missing []string `json:"missing_tasks,omitempty"`
}

func loadReqsForRun(root string) ([]Requirement, error) {
	reqs, problems, err := LoadRequirements(root)
	if err != nil {
		return nil, err
	}
	for _, problem := range problems {
		fmt.Fprintln(os.Stderr, t("board.invalid", problem.Message))
	}
	return reqs, nil
}

func findRequirement(reqs []Requirement, id string) (Requirement, error) {
	for _, req := range reqs {
		if req.ID == id {
			return req, nil
		}
	}
	return Requirement{}, kanbanError("board.req_requirement_not_found", id)
}

// RunRequirement implements kander req.
func RunRequirement(args []string) int {
	if len(args) == 0 {
		reqUsage(os.Stderr, "")
		return 2
	}
	action, rest := args[0], args[1:]
	switch action {
	case "new":
		return runReqNew(rest)
	case "list":
		return runReqList(rest)
	case "show":
		return runReqShow(rest)
	case "convert":
		return runReqConvert(rest)
	case "link", "unlink":
		return runReqLink(action, rest)
	case "remove":
		return runReqRemove(rest)
	case "complete":
		return runReqComplete(rest)
	case "-h", "--help":
		reqUsage(os.Stdout, "")
		return 0
	default:
		fmt.Fprintln(os.Stderr, t("board.req_unknown_action", action))
		reqUsage(os.Stderr, "")
		return 2
	}
}

func runReqNew(args []string) int {
	values := map[string]string{}
	for _, name := range []string{"--source", "--summary-file"} {
		var err error
		args, values[name], _, err = takeValueFlag(args, name)
		if err != nil {
			return reqUsageFail("new", "board.option_requires_a_value", name)
		}
	}
	if len(args) < 2 {
		return reqUsageFail("new", "board.req_slug_and_title_are_required")
	}
	slug, title := args[0], strings.Join(args[1:], " ")
	summary := values["--summary-file"]
	if summary == "" {
		summary = Placeholder
	} else {
		data, err := os.ReadFile(summary)
		if err != nil {
			return fail(err)
		}
		summary = strings.TrimSpace(string(data))
	}
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	path, err := AddRequirement(root, slug, title, values["--source"], summary)
	if err != nil {
		return fail(err)
	}
	fmt.Println(path)
	return 0
}

func runReqList(args []string) int {
	args, machine := takeFlag(args, "--json")
	args, statusFilter, _, err := takeValueFlag(args, "--status")
	if err != nil {
		return reqUsageFail("list", "board.option_requires_a_value", "--status")
	}
	if len(args) > 0 {
		return reqUsageFail("list", "board.too_many_arguments")
	}
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	reqs, err := loadReqsForRun(root)
	if err != nil {
		return fail(err)
	}
	var out []Requirement
	for _, req := range reqs {
		if statusFilter != "" && req.Status != statusFilter {
			continue
		}
		out = append(out, req)
	}
	if machine {
		if err = json.NewEncoder(os.Stdout).Encode(out); err != nil {
			return fail(err)
		}
		return 0
	}
	if len(out) == 0 {
		fmt.Println(t("board.req_no_requirements"))
		return 0
	}
	fmt.Println(t("board.req_list_header"))
	for _, req := range out {
		fmt.Printf("%s\t%s\t%s\t%s\t%s\n", req.ID, req.Status, reqProgress(req), req.Title, req.Source)
	}
	return 0
}

func runReqShow(args []string) int {
	args, machine := takeFlag(args, "--json")
	if len(args) != 1 {
		return reqUsageFail("show", "board.req_requirement_id_required")
	}
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	reqs, err := loadReqsForRun(root)
	if err != nil {
		return fail(err)
	}
	req, err := findRequirement(reqs, args[0])
	if err != nil {
		return fail(err)
	}
	req, err = RequirementStatus(req, root)
	if err != nil {
		return fail(err)
	}
	if machine {
		if err = json.NewEncoder(os.Stdout).Encode(req); err != nil {
			return fail(err)
		}
		return 0
	}
	fmt.Println(t("board.show_location", req.Status, req.Path))
	fmt.Println()
	fmt.Println(t("board.req_show_source", req.Source))
	fmt.Println(t("board.req_show_created", req.Created))
	if len(req.Docs) > 0 {
		fmt.Println(t("board.req_show_docs", strings.Join(req.Docs, ", ")))
	}
	fmt.Println(t("board.req_show_tasks", strings.Join(req.Tasks, ", ")))
	fmt.Println(t("board.req_show_groups", strings.Join(req.Groups, ", ")))
	fmt.Println(t("board.req_show_progress", reqProgress(req)))
	if len(req.Missing) > 0 {
		fmt.Println(t("board.req_missing_tasks", strings.Join(req.Missing, ", ")))
	}
	fmt.Println()
	fmt.Println("## " + ReqSectionSummary)
	fmt.Println(req.Summary)
	fmt.Println()
	fmt.Println("## " + ReqSectionNotes)
	fmt.Println(req.Notes)
	return 0
}

func runReqConvert(args []string) int {
	var tasks, groups []string
	args, groupsRaw, _, err := takeValueFlag(args, "--groups")
	if err != nil {
		return reqUsageFail("convert", "board.option_requires_a_value", "--groups")
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return reqUsageFail("convert", "board.unknown_option", arg)
		}
	}
	if len(args) < 1 {
		return reqUsageFail("convert", "board.req_requirement_id_required")
	}
	id := args[0]
	tasks = append(tasks, args[1:]...)
	groups = append(groups, splitCSV(groupsRaw)...)
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	path, err := ConvertRequirement(root, id, tasks, groups)
	if err != nil {
		return fail(err)
	}
	fmt.Println(t("board.req_converted", id, path))
	return 0
}

func runReqLink(action string, args []string) int {
	var tasks, groups []string
	args, groupsRaw, _, err := takeValueFlag(args, "--groups")
	if err != nil {
		return reqUsageFail(action, "board.option_requires_a_value", "--groups")
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return reqUsageFail(action, "board.unknown_option", arg)
		}
	}
	if len(args) < 1 {
		return reqUsageFail(action, "board.req_requirement_id_required")
	}
	id := args[0]
	tasks = append(tasks, args[1:]...)
	groups = append(groups, splitCSV(groupsRaw)...)
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	unlink := action == "unlink"
	if !unlink && len(tasks) == 0 && len(groups) == 0 {
		return reqUsageFail(action, "board.req_convert_requires_targets")
	}
	path, err := LinkRequirementTargets(root, id, tasks, groups, unlink)
	if err != nil {
		return fail(err)
	}
	fmt.Println(path)
	return 0
}

func runReqRemove(args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return reqUsageFail("remove", "board.req_requirement_id_required")
	}
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	if _, err = RemoveRequirement(root, args[0]); err != nil {
		return fail(err)
	}
	fmt.Println(t("board.req_removed", args[0]))
	return 0
}

func runReqComplete(args []string) int {
	args, expectDone := takeFlag(args, "--all-done")
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return reqUsageFail("complete", "board.req_requirement_id_required")
	}
	id := args[0]
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	if expectDone {
		reqs, err := loadReqsForRun(root)
		if err != nil {
			return fail(err)
		}
		req, err := findRequirement(reqs, id)
		if err != nil {
			return fail(err)
		}
		req, err = RequirementStatus(req, root)
		if err != nil {
			return fail(err)
		}
		if req.Total == 0 || req.Done != req.Total || len(req.Missing) > 0 {
			fmt.Fprintln(os.Stderr, t("board.req_not_all_done", id, strconv.Itoa(req.Done), strconv.Itoa(req.Total)))
			return 1
		}
	}
	path, err := SetRequirementStatus(root, id, ReqStatusCompleted)
	if err != nil {
		return fail(err)
	}
	fmt.Println(t("board.req_completed", id, path))
	return 0
}
