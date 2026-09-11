# Requirements pool (kander req)

The requirements pool is a standalone, minimal store outside the kanban board,
at `kanban/requirements/`. Requirement cards never enter the
`backlog→todo→working→review→done` lifecycle, are never started by
`kander start`, and are fully isolated from the task-card transaction and
review systems: syncing upstream never conflicts with this directory or with
code touchpoints.

## Requirement card

`requirements/<YYYYMMDD>-<slug>-req.md`, a minimal structure:

```markdown
# <title>

- SOURCE: <source identity, e.g. pool://login-doc-42 or a document path>
- STATUS: draft
- CREATED_AT: 2026-09-08 03:40
- DOCS: <linked document identities, comma-separated, optional>
- TASKS: <linked task card IDs, comma-separated>
- TASK_GROUPS: <linked task group IDs, comma-separated>
- ATTACHMENTS: <user-supplied attachment paths, comma-separated, stored verbatim>
- SESSION: <decompose agent identity>
- WINDOW: <decompose session launcher window>
- LAST_DECOMPOSE: <last decompose run timestamp>
- MODE: <discuss | collaborative | autonomous, empty defaults to collaborative>

## SUMMARY

<one-sentence requirement summary>

## NOTES

N/A
```

### Status and derivation

| Status | Meaning |
| --- | --- |
| `draft` | Not decomposed (initial state) |
| `decomposed` | Decomposed; TASKS/TASK_GROUPS record the linked work |
| `completed` | Written by an explicit `kander req complete`, gated by `--all-done` |
| `archived` | Terminal; excluded from derivation |

What a command **displays** is the *derived* status, recomputed live from the
linked task cards and never written back to the card file: all linked tasks
done → `completed`; any linked tasks still open → `decomposed`; no linked
tasks, or a terminal `archived` card, keeps the stored status. The card file
only ever changes through explicit commands, so `kander req complete` with its
`--all-done` gate stays the only writer of `completed` and derivation can
never race the linked task cards.

Progress is computed live as `done / total` over the linked task cards (task
groups expand through their current membership). A linked task card that no
longer exists is counted as missing and never as done, so a broken link cannot
turn a requirement into a completed one.

`kander req list` and `kander req list --json` carry the same derived status
and live progress as `kander req show`; the listing recomputes them with one
board scan for the whole batch. The `--status` filter matches the derived
status, so `--status decomposed` finds requirements with linked open tasks and
`--status draft` only finds cards with no linked tasks at all.

## Idempotence by slug

The requirement ID is `<YYYYMMDD>-<slug>-req`: the slug is the stable name the
caller chooses and the date is only a prefix. Creating a requirement with a
slug that already exists on the same day is rejected with
`req_already_exists` instead of producing a duplicate card. Importers that
pull the same requirement repeatedly should therefore reuse one stable slug
and treat the rejection as "already imported"; updating a description is done
with `kander req draft`, not by re-creating the card. There is deliberately no
separate external-id field: the slug plus the rejection is the idempotence
contract, and a second identifier would duplicate SOURCE and the slug
uniqueness without adding anything.

## Commands

```sh
kander req new --source <source-or-path> [--summary-file <file>] [--attach <path,path...>] <slug> <title...>
kander req list [--status <status>] [--json]
kander req show [--json] <requirement-id>
kander req convert <requirement-id> [task...] [--groups <group,group...>]   # record links and set decomposed
kander req convert <requirement-id> --from-drafts [--only <slug,slug...>] [--type <kind>] [--large]  # materialize drafts
kander req link <requirement-id> [task...] [--groups <group,group...>]      # append links only
kander req unlink <requirement-id>                                          # clear links
kander req complete <requirement-id> [--all-done]                           # mark completed
kander req remove <requirement-id>                                          # delete (completed cards are protected)
kander req decompose (--message <text> | --message-file <path>) [--discuss | --autonomous] [--agent <name>] [--launcher <name>] <req-id>
kander req draft <requirement-id> <task-slug> [--body-file <path>]          # upsert one PROPOSED_TASKS draft
```

`--attach` stores the given paths verbatim on the new card, the same way the
requirements TUI persists them before a decompose; the pool never copies or
uploads the targets, and the decompose prompt hands the path list to the agent.

### Decompose modes

`kander req decompose` launches the requirement's orchestrator agent; the
card's `MODE` field decides its behavior. An explicit `--discuss` or
`--autonomous` flag sets the field for that run; without a flag the recorded
value is kept, and a card that never recorded one runs collaborative.

| MODE | Behavior |
| --- | --- |
| `discuss` | Discusses the requirement and writes `## PROPOSED_TASKS` drafts only. It must not run `kander new`, so no task card exists until someone materializes the drafts |
| `collaborative` (default) | Discusses with the user and confirms each proposed task card before `kander new` |
| `autonomous` | Proposes and drives `kander new` + `req convert` directly |

`discuss` is the mode for "we are still deciding": the requirement stays at
`draft` with zero linked tasks, and the discussion's output is reviewable as
drafts before anything exists on the board. `--discuss` and `--autonomous`
select opposite ends of the scale, so passing both is a usage error rather than
a silent win for one of them.

There is deliberately no `kander req resume`: resume is a task-card command
bound to a board entry in `review/` or `working/`, and a requirement card has
no board entry. Re-engaging a requirement's orchestrator means re-running
`kander req decompose`, which reuses the recorded window when it is still
alive.

### Materializing drafts

`kander req convert <req-id> --from-drafts` turns the requirement's drafts into
real task cards and links them, in stable slug order, so the same input always
produces the same result:

- The card **envelope** is generated by the board, not by the draft: `TYPE`
  (default `feature`, overridable with `--type`), `SIZE` (`--large`), and
  `LANGUAGE` (from `agent_language`) are written by kander, and `TASK_GROUP`
  stays empty. A draft cannot forge them.
- A draft supplies **contract content** only, for `GOAL`, `USER_DECISIONS`,
  `EXPECTED_OUTCOME`, `ACCEPTANCE_CRITERIA`, `THREAT_MODEL` and `OUT_OF_SCOPE`.
  A section the draft omits keeps its skeleton placeholder, so an incomplete
  draft produces a card that the existing `todo` contract gate rejects rather
  than one that looks complete.
- `DISCUSSION`, `IMPLEMENTATION` and `SUMMARY` are never taken from a draft.
  `DISCUSSION` carries the post-creation `SELF_REVIEW`/`CARD_REVIEW` records, and
  pre-filling it would forge exactly the evidence the gates check; a
  draft-created card therefore still needs its self-review recorded before it
  can leave `backlog`.
- The draft's own `# <title>` heading becomes the card title; a draft without
  one falls back to the slug with hyphens replaced by spaces. A leading UTF-8
  byte-order mark is stripped from the draft body rather than left to hide the
  heading.
- Materialized drafts are **removed** from `## PROPOSED_TASKS`, so a second run
  is a no-op reporting that nothing is left instead of a duplicate-card error,
  and `--only <slug,slug...>` materializes a subset while leaving the rest as
  proposals for a later run.
- If a later draft fails, the cards already created are still linked and their
  drafts still consumed, so a retry after fixing the cause makes progress
  instead of repeating the same failure.
- `--from-drafts` materializes the requirement's own drafts; combining it with
  explicit task IDs or `--groups` is rejected, as are `--only`/`--type`/`--large`
  without `--from-drafts`.

## Typical flow

1. Take a requirement from an external pool → `kander req new --source pool://login-doc-42 login-fix "Fix login bug"`.
2. Decompose into task cards (`kander new`, with `--large`/task groups as
   needed) → `kander req convert <req-id> <task-id...>`.
3. Work progresses; `kander req list` shows live progress at any time (e.g. `2/5`).
4. All linked tasks done → `kander req complete <req-id> --all-done` (a failed
   check reports the gate; dropping `--all-done` forces it).
5. Write back to the external pool as needed (manual/scripted today; kander
   never writes to an external pool itself).

When the tasks are not agreed yet, steps 2 and 3 are replaced by a discussion
that produces drafts, and the cards appear only once they are confirmed:

```sh
kander req decompose --discuss --message "split this up" <req-id>   # writes PROPOSED_TASKS drafts only
# review kanban/requirements/<req-id>.md; add or edit drafts with kander req draft
kander req convert <req-id> --from-drafts [--only <slug>] [--type bug] [--large]
kander req list                                                     # now shows live progress, e.g. 0/3
```

An importer that pulls from an external tracker lands here by default: create
the requirement, run one `discuss` pass, and let a human confirm the drafts
before any card exists.

## Implementation boundaries

- Storage depends only on `internal/fs` (reparse-point rejection, atomic
  writes, exclusive lock `.kander/locks/requirements.lock`).
- `link`/`convert` verify that task IDs exist; `unlink` clears
  TASKS/TASK_GROUPS.
- Draft materialization is `board.ConvertRequirementDrafts`, which reuses
  `NewTaskFromDraft` (card envelope), `LinkRequirementTargets` and
  `SetProposedTasks`; it holds no pool-level lock itself, because each of those
  helpers takes its own and re-entering the exclusive lock would deadlock.
- `board.SetSection` replaces a `## section` in place and preserves the file's
  newline style, so a draft written with LF does not leave a mixed-ending card.
- Live status derivation is read-only and batched: `board.RequirementsLiveStatus`
  scans the board once per listing, while `board.RequirementStatus` addresses a
  single card.
- The Web API reuses the same batched derivation for its requirements listing.
