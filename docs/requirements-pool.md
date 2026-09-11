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
- MODE: <collaborative | autonomous, empty defaults to collaborative>

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
kander req link <requirement-id> [task...] [--groups <group,group...>]      # append links only
kander req unlink <requirement-id>                                          # clear links
kander req complete <requirement-id> [--all-done]                           # mark completed
kander req remove <requirement-id>                                          # delete (completed cards are protected)
kander req decompose (--message <text> | --message-file <path>) [--agent <name>] [--launcher <name>] <req-id>
kander req draft <requirement-id> <task-slug> [--body-file <path>]          # upsert one PROPOSED_TASKS draft
```

`--attach` stores the given paths verbatim on the new card, the same way the
requirements TUI persists them before a decompose; the pool never copies or
uploads the targets, and the decompose prompt hands the path list to the agent.

### Decompose modes

`kander req decompose` launches the requirement's orchestrator agent; the
card's `MODE` field decides its behavior:

| MODE | Behavior |
| --- | --- |
| `collaborative` (default) | Discusses with the user and confirms each proposed task card before `kander new` |
| `autonomous` | Proposes and drives `kander new` + `req convert` directly |

There is deliberately no `kander req resume`: resume is a task-card command
bound to a board entry in `review/` or `working/`, and a requirement card has
no board entry. Re-engaging a requirement's orchestrator means re-running
`kander req decompose`, which reuses the recorded window when it is still
alive.

## Typical flow

1. Take a requirement from an external pool → `kander req new --source pool://login-doc-42 login-fix "Fix login bug"`.
2. Decompose into task cards (`kander new`, with `--large`/task groups as
   needed) → `kander req convert <req-id> <task-id...>`.
3. Work progresses; `kander req list` shows live progress at any time (e.g. `2/5`).
4. All linked tasks done → `kander req complete <req-id> --all-done` (a failed
   check reports the gate; dropping `--all-done` forces it).
5. Write back to the external pool as needed (manual/scripted today; kander
   never writes to an external pool itself).

## Implementation boundaries

- Storage depends only on `internal/fs` (reparse-point rejection, atomic
  writes, exclusive lock `.kander/locks/requirements.lock`).
- `link`/`convert` verify that task IDs exist; `unlink` clears
  TASKS/TASK_GROUPS.
- Live status derivation is read-only and batched: `board.RequirementsLiveStatus`
  scans the board once per listing, while `board.RequirementStatus` addresses a
  single card.
- The Web API reuses the same batched derivation for its requirements listing.
