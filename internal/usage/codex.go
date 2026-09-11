package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/launch"
)

func codexRoot() string { return launch.CodexSessionsRoot() }

// scanCodex reads Codex rollout JSONL files, which are grouped by date under the
// sessions root.
//
// Whether a rollout contains usage at all depends on the environment: a build or a
// provider that returns no token counts writes none. On such a store this source
// reports zero sessions with data rather than zero tokens, and the command says so.
func scanCodex(root string, opts Options) ([]Session, string, error) {
	if root == "" {
		return nil, "", nil
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, "session directory does not exist", nil
		}
		return nil, "", err
	}

	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasPrefix(entry.Name(), "rollout-") && strings.HasSuffix(entry.Name(), ".jsonl") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}

	var sessions []Session
	for _, path := range paths {
		session, ok := parseCodexRollout(path)
		if ok {
			sessions = append(sessions, session)
		}
	}
	if len(sessions) == 0 {
		return nil, "rollouts present, no usage records", nil
	}
	return sessions, "", nil
}

// parseCodexRollout extracts one rollout's usage. It reports false when the file
// carries no usage records at all, so the caller can distinguish "no data" from
// "no tokens".
//
// Codex reports usage in two shapes and they must not be combined: a running
// session total, and a per-call delta. The final total is preferred because it is a
// single authoritative snapshot; deltas are summed only when no total is present.
func parseCodexRollout(path string) (Session, bool) {
	file, err := os.Open(path)
	if err != nil {
		return Session{}, false
	}
	defer file.Close()

	session := Session{Agent: "codex", Path: path}
	var deltas Counts
	sawDelta := false
	var total Counts
	sawTotal := false

	reader := bufio.NewScanner(file)
	reader.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for reader.Scan() {
		line := reader.Bytes()
		if len(line) == 0 {
			continue
		}
		var record codexRecord
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		if record.Type == "session_meta" {
			if record.Payload.ID != "" {
				session.ID = record.Payload.ID
			}
			if record.Payload.Cwd != "" {
				session.Cwd = record.Payload.Cwd
			}
			continue
		}
		usages := record.usage()
		if len(usages) == 0 {
			continue
		}
		for _, usage := range usages {
			switch usage.kind {
			case usageTotal:
				total = usage.counts
				sawTotal = true
				session.Cumulative = true
			case usageDelta:
				deltas = deltas.Add(usage.counts)
				sawDelta = true
				session.Steps++
			}
		}
		if at := record.timestamp(); !at.IsZero() {
			if session.First.IsZero() || at.Before(session.First) {
				session.First = at
			}
			if at.After(session.Last) {
				session.Last = at
			}
		}
	}
	if session.ID == "" {
		session.ID = strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "rollout-"), ".jsonl")
	}
	switch {
	case sawTotal:
		session.Counts = total
	case sawDelta:
		session.Counts = deltas
	default:
		return session, false
	}
	if session.Cumulative {
		// A running total carries no per-call detail.
		session.Steps = 0
	}
	return session, true
}

type usageKind int

const (
	usageNone usageKind = iota
	usageTotal
	usageDelta
)

type codexCounts struct {
	kind   usageKind
	counts Counts
}

// codexRecord is the outer envelope of a rollout line.
type codexRecord struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Payload   struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Cwd       string          `json:"cwd"`
		Info      json.RawMessage `json:"info"`
		TokenInfo json.RawMessage `json:"token_usage"`
	} `json:"payload"`
}

// codexUsage is the token shape inside a usage record. InputTokens is inclusive of
// cached input, so uncached input is the difference; treating the two as separate
// additive fields would double-count every cached token.
type codexUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

func (u codexUsage) counts() Counts {
	uncached := u.InputTokens - u.CachedInputTokens
	if uncached < 0 {
		uncached = 0
	}
	return Counts{
		CacheRead: u.CachedInputTokens,
		Input:     uncached,
		Output:    u.OutputTokens,
		Reasoning: u.ReasoningOutputTokens,
	}
}

// usage finds every usage object on a record. Both shapes can appear on one record,
// so both are returned in a fixed order rather than letting map iteration decide
// which one is seen first; the parser then applies its own precedence.
func (r codexRecord) usage() []codexCounts {
	var out []codexCounts
	for _, raw := range []json.RawMessage{r.Payload.Info, r.Payload.TokenInfo} {
		if len(raw) == 0 {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			continue
		}
		for _, candidate := range []struct {
			name string
			kind usageKind
		}{
			{"last_token_usage", usageDelta},
			{"total_token_usage", usageTotal},
		} {
			nested, ok := fields[candidate.name]
			if !ok {
				continue
			}
			var usage codexUsage
			if err := json.Unmarshal(nested, &usage); err != nil {
				continue
			}
			out = append(out, codexCounts{kind: candidate.kind, counts: usage.counts()})
		}
	}
	return out
}

func (r codexRecord) timestamp() time.Time {
	if r.Timestamp == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, r.Timestamp)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
