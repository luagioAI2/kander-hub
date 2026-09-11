package launch

// The exported roots below keep agent session-store locations in one place. They
// exist for read-only consumers such as `kander usage`, which inspect the logs an
// agent already wrote instead of instrumenting the agent itself.
//
// Nothing here writes to a store, and no consumer may infer agent behaviour from
// an empty result: a store that records nothing is not the same as an agent that
// consumed nothing.

// CodexSessionsRoot returns the directory holding Codex rollout JSONL files.
func CodexSessionsRoot() string {
	return codexSessionsRoot()
}

// DshSessionsRoot returns the directory holding DSH session stores.
func DshSessionsRoot() string {
	return dshSessionsRoot()
}
