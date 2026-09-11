// Package usage reports what agents actually consumed, read offline from the
// session logs the agents themselves wrote.
//
// The package is deliberately read-only and passive. It never instruments an
// agent, never calls a provider API, and never writes to the board, so inspecting
// cost cannot change agent behaviour. Every number it reports comes from a log
// that already existed on disk.
//
// Two conventions shape the report:
//
//   - Cached input is separated from uncached input. A cache read bills at a
//     fraction of a fresh input token, so a single "total tokens" figure
//     overstates cost by roughly an order of magnitude on long sessions. The
//     cached share is reported alongside, because it is the number that explains
//     the difference.
//   - An absent number is never reported as zero. When an agent's store holds no
//     usage records, the source says so and names the store it read, so a missing
//     measurement cannot be mistaken for a cheap run.
package usage

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Counts is a token breakdown. Input counts fresh (uncached) input tokens only;
// cached input is reported separately as CacheRead.
type Counts struct {
	CacheRead int64 `json:"cache_read"`
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning,omitempty"`
}

// Add returns the element-wise sum.
func (c Counts) Add(other Counts) Counts {
	return Counts{
		CacheRead: c.CacheRead + other.CacheRead,
		Input:     c.Input + other.Input,
		Output:    c.Output + other.Output,
		Reasoning: c.Reasoning + other.Reasoning,
	}
}

// Total is the sum of every component; it is dominated by CacheRead on long
// sessions and is therefore reported next to, never instead of, the breakdown.
func (c Counts) Total() int64 {
	return c.CacheRead + c.Input + c.Output + c.Reasoning
}

// CachedPercent is the cache-read share of all input tokens. It returns 0 when no
// input was recorded.
func (c Counts) CachedPercent() float64 {
	seen := c.CacheRead + c.Input
	if seen == 0 {
		return 0
	}
	return float64(c.CacheRead) * 100 / float64(seen)
}

// Session is one agent session's usage.
type Session struct {
	Agent  string    `json:"agent"`
	ID     string    `json:"session_id"`
	Model  string    `json:"model,omitempty"`
	Steps  int       `json:"steps"`
	Counts Counts    `json:"counts"`
	First  time.Time `json:"first"`
	Last   time.Time `json:"last"`
	// Cwd is the working directory the agent recorded, used to scope a report to
	// one project. It is not printed.
	Cwd string `json:"cwd,omitempty"`
	// Path is the log the numbers came from, so any figure can be re-checked.
	Path string `json:"path"`
	// Cumulative marks a source that only exposes a running total for the whole
	// session. Steps is then 0 and no per-call breakdown exists.
	Cumulative bool `json:"cumulative,omitempty"`
}

// Duration is the wall-clock span the session covers.
func (s Session) Duration() time.Duration {
	if s.First.IsZero() || s.Last.Before(s.First) {
		return 0
	}
	return s.Last.Sub(s.First)
}

// SourceStatus records what one agent's log store yielded. It is reported even
// when the store produced nothing.
type SourceStatus struct {
	Agent    string `json:"agent"`
	Root     string `json:"root"`
	Found    int    `json:"sessions_found"`
	WithData int    `json:"sessions_with_usage"`
	Note     string `json:"note,omitempty"`
}

// Report is one collection run.
type Report struct {
	Task     string         `json:"task,omitempty"`
	Scope    string         `json:"scope"`
	ScopeArg string         `json:"scope_argument,omitempty"`
	Days     int            `json:"days"`
	Sources  []SourceStatus `json:"sources"`
	Sessions []Session      `json:"sessions"`
	Totals   Counts         `json:"totals"`
	// Unsupported names agents that keep no readable usage log. They are listed so
	// the absence of a row for them is explained rather than silent.
	Unsupported []string `json:"unsupported_agents,omitempty"`
}

// Options selects what a collection run reads.
type Options struct {
	// Root scopes by the working directory agents recorded. Empty means every
	// session found, regardless of project.
	Root string
	// Task restricts the report to one card's session chain.
	Task string
	// Agent restricts the report to one agent's store.
	Agent string
	// Days is the look-back window applied to log file modification time.
	Days int
	// Now is the reference time; zero means time.Now.
	Now time.Time
	// SessionIDs are the session identities that belong to Task, resolved by the
	// caller from the card. Scanning matches these exactly and falls back to an
	// identifier substring match.
	SessionIDs []string
}

// source reads one agent's log store.
type source struct {
	agent string
	root  func() string
	scan  func(root string, opts Options) ([]Session, string, error)
}

// sources lists the agents whose logs can be read offline, in stable order.
func sources() []source {
	return []source{
		{agent: "dsh", root: dshRoot, scan: scanDsh},
		{agent: "codex", root: codexRoot, scan: scanCodex},
	}
}

// unsupportedAgents keep their usage in formats this package cannot read, or not
// at all. They are named in the report so an absent row is explained.
var unsupportedAgents = []string{"claude", "grok", "cursor"}

// Collect reads every configured source and returns one report.
func Collect(opts Options) (Report, error) {
	if opts.Days <= 0 {
		opts.Days = 7
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	report := Report{
		Task:        opts.Task,
		Days:        opts.Days,
		Unsupported: append([]string{}, unsupportedAgents...),
	}
	switch {
	case opts.Task != "":
		report.Scope = "task"
		report.ScopeArg = opts.Task
	case opts.Root != "":
		report.Scope = "project"
		report.ScopeArg = opts.Root
	default:
		report.Scope = "all"
	}

	cutoff := opts.Now.Add(-time.Duration(opts.Days) * 24 * time.Hour)
	for _, src := range sources() {
		if opts.Agent != "" && opts.Agent != src.agent {
			continue
		}
		root := src.root()
		status := SourceStatus{Agent: src.agent, Root: root}
		sessions, note, err := src.scan(root, opts)
		if err != nil {
			status.Note = err.Error()
			report.Sources = append(report.Sources, status)
			continue
		}
		status.Note = note
		for _, session := range sessions {
			status.Found++
			if session.Counts.Total() == 0 {
				continue
			}
			if !withinWindow(session, cutoff) {
				continue
			}
			if !inScope(session, opts) {
				continue
			}
			status.WithData++
			report.Totals = report.Totals.Add(session.Counts)
			report.Sessions = append(report.Sessions, session)
		}
		report.Sources = append(report.Sources, status)
	}
	sortSessions(report.Sessions)
	return report, nil
}

// sortSessions puts the heaviest sessions first: the report exists to make the
// dominant cost visible, so ordering by consumption is the useful default.
func sortSessions(sessions []Session) {
	sort.SliceStable(sessions, func(i, j int) bool {
		left, right := sessions[i], sessions[j]
		if left.Counts.Total() != right.Counts.Total() {
			return left.Counts.Total() > right.Counts.Total()
		}
		return left.Last.After(right.Last)
	})
}

// withinWindow keeps sessions touched inside the look-back window. It uses the last
// recorded activity so a long session that started earlier still counts.
func withinWindow(session Session, cutoff time.Time) bool {
	if session.Last.IsZero() {
		return true
	}
	return !session.Last.Before(cutoff)
}

// inScope applies the report's selection: an explicit task chain, or a project.
func inScope(session Session, opts Options) bool {
	if opts.Task != "" {
		if len(opts.SessionIDs) == 0 {
			return strings.Contains(session.ID, opts.Task)
		}
		for _, id := range opts.SessionIDs {
			if id == "" {
				continue
			}
			if session.ID == id || strings.Contains(session.ID, id) {
				return true
			}
		}
		return false
	}
	if opts.Root == "" {
		return true
	}
	return withinDir(session.Cwd, opts.Root)
}

// withinDir reports whether cwd is root itself or a path under it. It compares on a
// separator boundary so a sibling directory sharing a name prefix never matches.
func withinDir(cwd, root string) bool {
	if cwd == "" || root == "" {
		return false
	}
	c := normalizePath(cwd)
	r := normalizePath(root)
	if c == r {
		return true
	}
	return strings.HasPrefix(c, strings.TrimSuffix(r, string(filepath.Separator))+string(filepath.Separator))
}

// normalizePath drops the Windows extended-length prefix and case so the two sides
// compare consistently. One agent records its cwd in extended-length form.
func normalizePath(p string) string {
	p = strings.TrimPrefix(p, `\\?\`)
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}
