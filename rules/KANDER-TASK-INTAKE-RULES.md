# Task Intake Guidance

## Workload Gate

Classify the request before touching code, state the classification and its reason in the one sentence the plan already carries, and settle the mode once. The classification is the agent's own judgment and is not an extra question put to the user.

In-session execution is eligible only when the request is genuinely small, localized, and gains nothing material from separate context for implementation, verification, or review. When any of the following holds, the work is not eligible for in-session execution, and the plan uses the board:

- It spans multiple files, modules, services, or components.
- It has two or more independent workstreams.
- Repository exploration is needed before implementation.
- Implementation and verification benefit from separate context.
- Debugging requires tracing across components.
- External or version-specific facts must be verified.
- An independent post-change review is materially useful.
- The user asks for delegation, parallel work, agents, or cards.

Delegation is a set of actions, never a description:

- When the work is delegated, produce it by running the commands. Do not implement it in this session first and create cards afterwards to record what is already done.
- Describing, simulating, or reasoning about cards, delegation, or agents is never a substitute for running the commands that create them.
- Once the user has chosen a board option, this session does not implement the work; the confirmed plan's cards are executed per `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination".
- If `kander new`, `kander pick`, or `kander start` fails, report the actual error and stop. Do not silently finish the work in this session, and do not report cards that were not created: cite only the task IDs the commands actually returned.
- Never claim that work was delegated, or that cards exist, unless the command that creates them succeeded.
- An explicit user instruction for the current request takes precedence: when the user asks this session to implement the work, or asks this agent to execute an existing card, follow that instruction.

## Creation and Confirmation

GitHub issues follow `KANDER-ISSUE-RULES.md` for investigation, consent, and binding before this guidance applies.

Give intake guidance only for a new bug or feature request that has not yet chosen an execution mode. Tasks continued via `start`, `resume`, or `notify`, and existing cards named by the user, continue on the original card without asking again. When the user has already chosen the board or direct execution, follow that choice. Pure Q&A, read-only investigation, minor documentation or configuration tweaks, releases, and merges do not trigger intake.

**Planning the split.** When `rules.task_groups=true` and `rules.git=true`, read `KANDER-TASK-GROUP-RULES.md` "Task Splitting and Task Groups" before presenting options, and state in the plan whether the work is one card, several independent single cards, or one or more task groups, with the reason. For a group, list the member card titles, their dependency order, and the step that merges each group branch back into `develop` in dependency order once its gates pass. Card bodies are written after confirmation; the split and the merge-back step are confirmed with the plan. When task groups are disabled, always plan a single card and do not load the disabled module.

**Checking work in flight.** Before presenting the plan, compare the paths the plan expects to change with the files changed by every card in `working/` or `review/` and every open group branch (`git diff --name-only $(git merge-base <branch> develop) <branch>`), and state the result in the plan. On overlap, do not plan parallel independent cards: order the new card after the in-flight one (record the dependency as `PREREQUISITES` in its `DISCUSSION` per `KANDER-TASK-GROUP-RULES.md` "Dependencies Between Task Cards"; for a non-group card the executing agent verifies it before starting), or, when an active task group touches the same module, plan the new card as a member of that group's successor group instead of a single card that would land on `develop` underneath the group. Hotspot files shared by many cards (generated code, lock files, IDE project files, shared contract or schema files, per-locale string tables) count as overlap even when the planned edits are in different regions. Several new cards from one request that touch the same hotspot are ordered serially the same way.

**Presenting the options.** After the analysis and implementation plan, present the options once, for single-card and multi-card plans alike, numbered from `1` as the only numbered question in that message:

```text
1. Confirm the plan and use the kanban board (create the cards; this session starts and advances them here)
2. Confirm the plan and use the kanban board (create the cards; a separate orchestrator session advances and reports them in its own window)
3. Confirm the plan and use the kanban board (create the cards only, leave them in backlog, start later on instruction)
4. Confirm the plan, skip the board, implement directly in this session
5. Adjust the plan (no card created or started)
```

- **Option 1** authorizes the plan, the development, and the kanban flow at once, including the merge-back steps the plan states, for every standalone card and task group it names; do not ask again before starting or integrating. For a standalone card with `rules.git=true`, this covers integration into `develop` and cleanup under the conditions in `KANDER-GIT-RULES.md` "Integration and Cleanup"; the completion flow is `KANDER-KANBAN-RULES.md` "Execution and Completion".
  - For each single card, in order: `kander new`, fill in the complete contract per the confirmed plan, complete the self-review and any applicable independent card review per `KANDER-KANBAN-RULES.md` "Post-Creation Self-Review" and fix the findings, `kander pick <task-id>`, `kander start <task-id>`. Start and tracking follow `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination"; the discussing agent no longer implements a card it has delegated.
  - For a task group, create every member card the same way, complete the group-level checks in `KANDER-TASK-GROUP-RULES.md` "Task Splitting and Task Groups", then orchestrate per that file. The confirmed plan is the orchestration plan and carries the integration authorization for each group.
- **Option 2** carries the same authorization as option 1 but hands the advancing to a separate session. Create every card as in option 1, with its self-review, applicable independent card review, and group-level checks, and leave it in `backlog/`. Write the confirmed plan into a notes file (card and group order, which cards may run in parallel, merge-back steps, user decisions) and run `kander orchestrate --message-file <notes> <task-id|task-group-id>...` in plan order per `KANDER-KANBAN-RULES.md` "Orchestrator Sessions". On success, tell the user where the orchestrator runs, that it advances and reports every card in its own window, and that this session starts no card and no longer follows the plan. On launch failure, report the error; the cards stay in `backlog/` and the user chooses to retry or to continue with option 1 here.
- **Option 3** authorizes the plan and card creation only. Create the cards with their self-review and applicable independent card review, leave them in `backlog/`, and do not move or start them. A later start instruction enters the option 1 flow from `kander pick` onward and carries the confirmed plan's integration authorization; do not ask for it again. After `pick`, `kander move <task-id> working --owner <agent>` replaces `kander start` only when the user explicitly asks this agent to execute the card itself, per `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination".
- **Option 4** implements directly per the project rules, without a card.
- **Option 5** modifies the plan; no card is created or started.

## Requirement Decomposition Modes

A requirement card in `kanban/requirements/` is decomposed by an orchestrator session, not by the intake flow above. Its `MODE` field decides how far that session may go, and the mode is part of the confirmed plan:

- `discuss`: discuss the requirement and write `## PROPOSED_TASKS` drafts only. Do not run `kander new`. The drafts are proposals, and the requirement stays undecided until the user confirms them.
- `collaborative`: discuss and write drafts, then confirm each proposed card with the user before `kander new`.
- `autonomous`: write the drafts and create the cards without waiting for confirmation.

Confirmed drafts become cards with `kander req convert <req-id> --from-drafts`, which creates the card from the draft and links it to the requirement. A draft supplies contract content only: the card envelope, the card type, and the size come from the command, and the self-review and card-review records are never taken from a draft. Cards created this way need the same post-creation self-review as cards created by `kander new`, and being materialized from an accepted draft is not a reason to skip it.

The following are contract violations in requirement mode, not shortcuts:

- Creating a task card while the mode is `discuss`, including one "just to check" the flow.
- Reporting drafts as cards. Writing `## PROPOSED_TASKS` or running `kander req draft` creates no card; cite only the task IDs `kander new` or `kander req convert --from-drafts` actually returned.
- Advancing a requirement to `completed`, or a card past `working`, on the strength of a draft rather than a real card that passed its gates.
- Materializing drafts the user has not confirmed when the mode is `discuss` or `collaborative`.
