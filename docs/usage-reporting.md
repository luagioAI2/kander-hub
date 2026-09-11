# Usage Reporting

`kander usage` reports what agents actually consumed. It exists because Kander's
review contract makes the main agent the always-resident thread that re-reads
context every turn, and nothing else in the toolchain showed what that costs.

## Boundaries

The command is read-only and passive by construction:

- It opens no agent process and sends no prompt, so inspecting cost cannot change
  agent behaviour.
- It calls no provider API and does no metering of its own. Every number comes from
  a log the agent had already written.
- It writes nothing: not to the board, not to a card, not to an agent store.

Because of this it is always safe to run while agents are working, including
against a live board.

## Sources

| Agent    | Store                                   | Format                             |
| -------- | --------------------------------------- | ---------------------------------- |
| `dsh`    | `$DSH_HOME/sessions` (default `~/.dsh/sessions`) | one zstd JSON Lines file per session |
| `codex`  | `$CODEX_HOME/sessions` (default `~/.codex/sessions`) | one rollout JSONL per session |

The store roots are owned by `internal/launch`, which already needs them for session
discovery; `internal/usage` consumes them through `launch.DshSessionsRoot` and
`launch.CodexSessionsRoot` so the layout is described in one place.

`claude`, `grok` and `cursor` keep no usage log this package can read. They are
named in every report so the absence of a row is explained rather than silent.

## Counting rules

Two rules decide whether the numbers mean anything. Both are covered by tests,
because both are easy to get wrong in a way that looks plausible.

### One record per API call

DSH writes the same usage object twice for a single call: once as a streaming chunk
(`assistant/chunk` → `data.chunk.usage`) and once on the finalized message
(`assistant/message` → `data.usage`). The values are identical, so summing both
reports exactly double.

Only the chunk form is counted, because it is the superset: it also carries usage for
calls that were billed but never finalized, such as an aborted attempt that produced
no message. Measured on a live store, the chunk form carried 676 records against 658
messages with byte-identical sums, confirming the message form is a strict subset.

### Cached input is not fresh input

A cache read bills at a fraction of a fresh input token, so a single "total tokens"
figure overstates cost by roughly an order of magnitude on a long session. The report
therefore shows `cache-read` and `input` in separate columns and prints the cached
share next to them. Measured on this repository's own working sessions, the cached
share ranged from 83% to 99% — the total alone would be badly misleading.

Codex reports `input_tokens` inclusive of `cached_input_tokens`, so uncached input is
the difference between them. Adding the two fields would double-count every cached
token.

Codex also reports usage in two shapes: a running session total and a per-call delta.
The total is preferred, because it is one authoritative snapshot; deltas are summed
only when no total is present, and such a session is marked `cumulative` with no
per-call detail.

## Absent data is not zero

When a store holds no usage records, the session is dropped rather than reported as a
zero row, and the source line states what was read and what it contained. This
matters in practice: a Codex build or provider that returns no token counts writes
none, and reporting `0` there would read as "this run was free" instead of
"this run was not measured".

## Scopes

| Invocation                | Scope                                              |
| ------------------------- | -------------------------------------------------- |
| `kander usage`            | the project the board belongs to                    |
| `kander usage --task <id>`| one card's session chain                            |
| `kander usage --all`      | every session in every store                        |
| `--agent <name>`          | restricts to one agent's store                      |
| `--days N`                | look-back window on last activity (default 7)       |
| `--json`                  | machine-readable output                             |

A card records its agent session as `<agent> <reference>` in the `SESSION` field, the
same shape `internal/launch` parses. The reference is the exact key and is matched on
its own, because a Codex reference is a UUID that shares nothing with the task id; the
task id is only a fallback for stores that name a session after the card.

Outside a project there is no scope to default to, so the report widens to all
sessions instead of failing. The scope line always states which scope was used, so
the widening is visible.

## Not yet covered

- Build and review token or wall-clock cost is not attributed per review run. The
  `reviews/<run_id>/` structure is the natural place for it.
- There is no automatic "this task could have used a smaller model" hint. That needs
  the measurement above plus a risk signal, and only the measurement exists today.
- Provider-side rate-limit windows are not read. The account-level `used_percent`
  delta is the closest thing to a true billing figure, but it is account-wide and
  cannot be attributed to one card.
