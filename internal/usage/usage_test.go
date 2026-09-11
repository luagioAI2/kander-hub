package usage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// writeDshSession writes a DSH session log under the store layout, returning the
// store root. The log is zstd-compressed exactly as DSH writes it.
func writeDshSession(t *testing.T, root, project, id string, lines []string) {
	t.Helper()
	dir := filepath.Join(root, project, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	encoder, err := zstd.NewWriter(&compressed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encoder.Write([]byte(strings.Join(lines, "\n") + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl.zstd"), compressed.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// dshFixture is one session whose log carries usage on BOTH paths DSH writes it, plus
// an aborted call that never produced a finalized message. The chunk path is the
// superset, so the expected total counts the aborted call and ignores the duplicate.
func dshFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	lines := []string{
		`{"type":"session","version":0,"id":"task-1","createdAt":1767225600000,"delegationDepth":0,"cwd":"/work/proj"}`,
		`{"type":"request/header","seq":1,"data":{"header":{"config":{"model":"model-a","provider":"prov-a"}}}}`,
		// A normal call: the chunk and the finalized message carry identical numbers.
		`{"type":"assistant/chunk","seq":2,"time":1767225601000,"data":{"chunk":{"type":"usage","usage":{"cacheReadTokens":1000,"inputTokens":100,"outputTokens":50,"totalTokens":1150}},"step":1,"turn":1}}`,
		`{"type":"assistant/message","seq":3,"time":1767225601500,"data":{"usage":{"cacheReadTokens":1000,"inputTokens":100,"outputTokens":50,"totalTokens":1150}}}`,
		// An aborted call: usage was billed but no message was finalized.
		`{"type":"llm/retry","seq":4,"time":1767225602000,"data":{}}`,
		`{"type":"assistant/chunk","seq":5,"time":1767225603000,"data":{"chunk":{"type":"usage","usage":{"cacheReadTokens":500,"inputTokens":20,"outputTokens":10,"totalTokens":530}},"step":2,"turn":1}}`,
		// A non-usage chunk must not be counted.
		`{"type":"assistant/chunk","seq":6,"time":1767225604000,"data":{"chunk":{"type":"text-delta","text":"hello"},"step":2,"turn":1}}`,
		`{"type":"turn/end","seq":7,"time":1767225605000,"data":{"turn":1,"reason":{"kind":"completed"}}}`,
	}
	writeDshSession(t, root, "--work-proj--", "task-1", lines)
	return root
}

// TestDshCountsChunkUsageOnce pins the counting rule. DSH records the same usage on
// the streaming chunk and on the finalized message; summing both would report
// exactly double, so only the chunk form may be counted.
func TestDshCountsChunkUsageOnce(t *testing.T) {
	root := dshFixture(t)
	sessions, _, err := scanDsh(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions=%d want 1", len(sessions))
	}
	got := sessions[0]
	want := Counts{CacheRead: 1500, Input: 120, Output: 60}
	if got.Counts != want {
		t.Fatalf("counts=%+v want %+v (a doubled value means both usage paths were summed)", got.Counts, want)
	}
	if got.Steps != 2 {
		t.Fatalf("steps=%d want 2", got.Steps)
	}
	if got.Model != "model-a" {
		t.Fatalf("model=%q want model-a", got.Model)
	}
	if got.ID != "task-1" {
		t.Fatalf("id=%q want task-1", got.ID)
	}
	if got.Cwd != "/work/proj" {
		t.Fatalf("cwd=%q want /work/proj (the session header wins over the project key)", got.Cwd)
	}
}

func TestCountsBreakdownAndCacheShare(t *testing.T) {
	counts := Counts{CacheRead: 945, Input: 55, Output: 10}
	if got, want := counts.Total(), int64(1010); got != want {
		t.Fatalf("total=%d want %d", got, want)
	}
	if got, want := counts.CachedPercent(), 94.5; got != want {
		t.Fatalf("cached%%=%v want %v", got, want)
	}
	empty := Counts{}
	if got := empty.CachedPercent(); got != 0 {
		t.Fatalf("cached%% of empty=%v want 0", got)
	}
}

// TestCollectScopeAndWindow shows that a project scope excludes sessions elsewhere
// and that the look-back window drops stale ones.
func TestCollectScopeAndWindow(t *testing.T) {
	// The scanner roots are the real ones, so this test drives the pure helpers
	// instead of depending on the machine's agent stores.
	now := time.Now()
	recent := Session{ID: "a", Counts: Counts{Input: 10}, First: now.Add(-time.Hour), Last: now.Add(-time.Hour), Cwd: "/work/proj"}
	old := Session{ID: "b", Counts: Counts{Input: 10}, First: now.Add(-30 * 24 * time.Hour), Last: now.Add(-30 * 24 * time.Hour), Cwd: "/work/proj"}
	other := Session{ID: "c", Counts: Counts{Input: 10}, First: now.Add(-time.Hour), Last: now.Add(-time.Hour), Cwd: "/work/elsewhere"}

	cutoff := now.Add(-7 * 24 * time.Hour)
	if !withinWindow(recent, cutoff) {
		t.Fatal("recent session must be inside the window")
	}
	if withinWindow(old, cutoff) {
		t.Fatal("stale session must be outside the window")
	}
	if !inScope(recent, Options{Root: "/work/proj"}) {
		t.Fatal("session in the project must be in scope")
	}
	if inScope(other, Options{Root: "/work/proj"}) {
		t.Fatal("session in another project must be out of scope")
	}
	if !inScope(other, Options{}) {
		t.Fatal("without a scope every session is in scope")
	}
}

// TestWithinDirRejectsSiblingPrefix guards the separator-boundary comparison: a
// sibling directory that merely shares a name prefix must never match.
func TestWithinDirRejectsSiblingPrefix(t *testing.T) {
	root := filepath.Join(string(filepath.Separator)+"work", "proj")
	if !withinDir(root, root) {
		t.Fatal("a directory must be within itself")
	}
	if !withinDir(filepath.Join(root, "sub"), root) {
		t.Fatal("a child directory must be within the root")
	}
	if withinDir(root+"-other", root) {
		t.Fatal("a sibling sharing a name prefix must not match")
	}
	if withinDir("", root) || withinDir(root, "") {
		t.Fatal("empty paths must not match")
	}
}

func TestParseDshSessionToleratesTruncatedTail(t *testing.T) {
	root := t.TempDir()
	// A session being appended to can end mid-line; that must not discard the usage
	// already recorded, and must not fail the scan.
	lines := []string{
		`{"type":"session","id":"task-2","cwd":"/work/proj"}`,
		`{"type":"assistant/chunk","time":1767225601000,"data":{"chunk":{"type":"usage","usage":{"cacheReadTokens":10,"inputTokens":5,"outputTokens":1}}}}`,
		`{"type":"assistant/chu`,
	}
	writeDshSession(t, root, "--work-proj--", "task-2", lines)
	sessions, _, err := scanDsh(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions=%d want 1", len(sessions))
	}
	if got, want := sessions[0].Counts, (Counts{CacheRead: 10, Input: 5, Output: 1}); got != want {
		t.Fatalf("counts=%+v want %+v", got, want)
	}
}

func TestScanDshMissingRootIsNotAnError(t *testing.T) {
	sessions, note, err := scanDsh(filepath.Join(t.TempDir(), "absent"), Options{})
	if err != nil {
		t.Fatalf("a missing store must not fail the report: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions=%d want 0", len(sessions))
	}
	if note == "" {
		t.Fatal("a missing store must be explained, not silently empty")
	}
}

// writeCodexRollout writes a rollout file in the dated layout.
func writeCodexRollout(t *testing.T, root, name string, lines []string) {
	t.Helper()
	dir := filepath.Join(root, "2026", "01", "01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCodexPrefersTotalOverDeltas pins the precedence rule: a rollout that reports
// both a running total and per-call deltas must be read from the total, never from
// the sum of deltas.
func TestCodexPrefersTotalOverDeltas(t *testing.T) {
	root := t.TempDir()
	writeCodexRollout(t, root, "rollout-a.jsonl", []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"codex-1","cwd":"/work/proj"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":900,"output_tokens":50,"reasoning_output_tokens":5},"last_token_usage":{"input_tokens":1000,"cached_input_tokens":900,"output_tokens":50,"reasoning_output_tokens":5}}}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":2400,"cached_input_tokens":2200,"output_tokens":120,"reasoning_output_tokens":9},"last_token_usage":{"input_tokens":1400,"cached_input_tokens":1300,"output_tokens":70,"reasoning_output_tokens":4}}}}`,
	})
	sessions, _, err := scanCodex(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions=%d want 1", len(sessions))
	}
	got := sessions[0]
	// The final total snapshot: cached 2200, uncached 2400-2200=200, output 120.
	want := Counts{CacheRead: 2200, Input: 200, Output: 120, Reasoning: 9}
	if got.Counts != want {
		t.Fatalf("counts=%+v want %+v", got.Counts, want)
	}
	if !got.Cumulative {
		t.Fatal("a session read from a running total must be marked cumulative")
	}
	if got.Steps != 0 {
		t.Fatalf("steps=%d want 0: a cumulative total carries no per-call detail", got.Steps)
	}
}

// TestCodexSumsDeltasWhenNoTotal proves the fallback path, and that uncached input
// is the difference between input and cached input rather than their sum.
func TestCodexSumsDeltasWhenNoTotal(t *testing.T) {
	root := t.TempDir()
	writeCodexRollout(t, root, "rollout-b.jsonl", []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"codex-2","cwd":"/work/proj"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":1000,"cached_input_tokens":900,"output_tokens":50}}}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":500,"cached_input_tokens":400,"output_tokens":20}}}}`,
	})
	sessions, _, err := scanCodex(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions=%d want 1", len(sessions))
	}
	got := sessions[0]
	want := Counts{CacheRead: 1300, Input: 200, Output: 70}
	if got.Counts != want {
		t.Fatalf("counts=%+v want %+v", got.Counts, want)
	}
	if got.Cumulative {
		t.Fatal("summed deltas must not be marked cumulative")
	}
	if got.Steps != 2 {
		t.Fatalf("steps=%d want 2", got.Steps)
	}
}

// TestCodexWithoutUsageRecordsIsReportedNotZero is the behaviour that matters in
// environments whose provider returns no token counts: the session must be dropped
// and the reason stated, so an unmeasured run is never shown as a free one.
func TestCodexWithoutUsageRecordsIsReportedNotZero(t *testing.T) {
	root := t.TempDir()
	writeCodexRollout(t, root, "rollout-c.jsonl", []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"codex-3","cwd":"/work/proj"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`,
	})
	sessions, note, err := scanCodex(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions=%d want 0: a rollout without usage must not produce a zero row", len(sessions))
	}
	if note == "" {
		t.Fatal("the absence of usage records must be explained")
	}
}

// TestSessionReferencesParsesCardMetadata pins the card metadata shape. A Codex
// reference is a UUID that never contains the task id, so the reference must be
// extracted rather than used verbatim as one string.
func TestSessionReferencesParsesCardMetadata(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  []string
	}{
		{name: "agent and reference", value: "dsh 20260910-task", want: []string{"20260910-task"}},
		{name: "codex uuid", value: "codex 019d2a59-d94c-7292-8f8b-e91ea76e59f9", want: []string{"019d2a59-d94c-7292-8f8b-e91ea76e59f9"}},
		{name: "bare reference", value: "task-only", want: []string{"task-only"}},
		{name: "empty", value: "   ", want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionReferences(tc.value)
			if len(got) != len(tc.want) {
				t.Fatalf("sessionReferences(%q)=%v want %v", tc.value, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("sessionReferences(%q)=%v want %v", tc.value, got, tc.want)
				}
			}
		})
	}
}

// TestInScopeMatchesExactSessionReference proves a task scope matches the card's
// recorded session reference even when the session id shares nothing with the task id.
func TestInScopeMatchesExactSessionReference(t *testing.T) {
	uuid := "019d2a59-d94c-7292-8f8b-e91ea76e59f9"
	opts := Options{Task: "20260910-print-hello-task", SessionIDs: []string{uuid, "20260910-print-hello-task"}}
	if !inScope(Session{ID: uuid}, opts) {
		t.Fatal("the recorded session reference must match")
	}
	if !inScope(Session{ID: "20260910-print-hello-task"}, opts) {
		t.Fatal("the task id fallback must match")
	}
	if inScope(Session{ID: "unrelated-session"}, opts) {
		t.Fatal("an unrelated session must not match")
	}
}

func TestFormatRendersEverySessionAndTotals(t *testing.T) {
	report := Report{
		Scope: "all",
		Days:  7,
		Sessions: []Session{{
			Agent: "dsh", ID: "s1", Model: "m",
			Counts: Counts{CacheRead: 1500, Input: 120, Output: 60},
			First:  time.Now().Add(-time.Minute), Last: time.Now(),
		}},
		Sources: []SourceStatus{{Agent: "dsh", Root: "/store", Found: 1, WithData: 1}},
	}
	report.Totals = report.Sessions[0].Counts
	out := Format(report)
	for _, want := range []string{"dsh", "s1", "1,500", "120", "60", "1,680", "/store"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatEmptyReportExplainsItself(t *testing.T) {
	out := Format(Report{Scope: "project", ScopeArg: "/work/proj", Days: 7,
		Sources: []SourceStatus{{Agent: "codex", Root: "/store", Note: "rollouts present, no usage records"}},
	})
	if !strings.Contains(out, "no usage records") {
		t.Fatalf("an empty report must explain the source:\n%s", out)
	}
}

func TestFormatJSONRoundTrips(t *testing.T) {
	report := Report{Scope: "all", Days: 7, Totals: Counts{CacheRead: 1, Input: 2, Output: 3}}
	text, err := FormatJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Totals.Input != 2 || decoded.Days != 7 {
		t.Fatalf("round trip changed the report: %+v", decoded)
	}
}

func TestGroupAndPercentAndDuration(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0"}, {7, "7"}, {999, "999"}, {1000, "1,000"},
		{1234567, "1,234,567"}, {-4321, "-4,321"},
	} {
		if got := group(tc.in); got != tc.want {
			t.Errorf("group(%d)=%q want %q", tc.in, got, tc.want)
		}
	}
	if got := percent(94.56); got != "94.6%" {
		t.Errorf("percent=%q", got)
	}
	for _, tc := range []struct {
		span time.Duration
		want string
	}{
		{0, "-"},
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m30s"},
		{3*time.Hour + 12*time.Minute, "3h12m"},
		{50 * time.Hour, "2d2h"},
	} {
		if got := duration(tc.span); got != tc.want {
			t.Errorf("duration(%v)=%q want %q", tc.span, got, tc.want)
		}
	}
}

// TestParseUsageArgs covers the flag surface, including the failure modes that must
// exit with a usage error rather than silently widening the report.
func TestParseUsageArgs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		want    Options
		json    bool
		wantErr bool
	}{
		{name: "empty", args: nil, want: Options{Days: 7}},
		{name: "json", args: []string{"--json"}, want: Options{Days: 7}, json: true},
		{name: "all", args: []string{"--all"}, want: Options{Days: 7}},
		{name: "task", args: []string{"--task", "t1"}, want: Options{Days: 7, Task: "t1"}},
		{name: "agent", args: []string{"--agent", "dsh"}, want: Options{Days: 7, Agent: "dsh"}},
		{name: "days", args: []string{"--days", "30"}, want: Options{Days: 30}},
		{name: "days not a number", args: []string{"--days", "x"}, wantErr: true},
		{name: "days zero", args: []string{"--days", "0"}, wantErr: true},
		{name: "missing value", args: []string{"--task"}, wantErr: true},
		{name: "unknown option", args: []string{"--nope"}, wantErr: true},
		{name: "stray argument", args: []string{"extra"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, jsonOut, err := parseUsageArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if jsonOut != tc.json {
				t.Fatalf("json=%v want %v", jsonOut, tc.json)
			}
			if got.Days != tc.want.Days || got.Task != tc.want.Task || got.Agent != tc.want.Agent {
				t.Fatalf("opts=%+v want days=%d task=%q agent=%q", got, tc.want.Days, tc.want.Task, tc.want.Agent)
			}
		})
	}
}

// TestCollectOrdersHeaviestFirst keeps the report useful: the dominant cost must be
// the first row.
func TestCollectOrdersHeaviestFirst(t *testing.T) {
	sessions := []Session{
		{ID: "small", Counts: Counts{Input: 1}},
		{ID: "big", Counts: Counts{CacheRead: 1000}},
	}
	now := time.Now()
	for i := range sessions {
		sessions[i].First, sessions[i].Last = now, now
	}
	report := Report{Sessions: sessions}
	sortSessions(report.Sessions)
	if report.Sessions[0].ID != "big" {
		t.Fatalf("first=%q want big", report.Sessions[0].ID)
	}
}
