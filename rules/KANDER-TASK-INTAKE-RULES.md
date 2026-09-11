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

Provide guidance only for new bug or feature requests that have not yet chosen an execution mode. Tasks continued via `start`, `resume`, or `notify`, and existing cards named by the user, continue on the original card; do not ask again or create another card. When the user has already explicitly chosen the kanban board or direct execution, follow the chosen flow without asking again. Pure Q&A, read-only investigation, minor documentation or configuration tweaks, releases, and merges do not trigger intake guidance.

When `rules.task_groups=true` and `rules.git=true`, read `KANDER-TASK-GROUP-RULES.md` "Task Splitting and Task Groups" before presenting the options, and state in the plan whether the work is one card, several independent single cards, or one or more task groups, with the reason. For a group, also list the member card titles, their dependency order, and the step that merges each group branch back into `develop` in dependency order once its gates pass. Card bodies are written after confirmation; the split and the merge-back step are confirmed together with the plan. When task groups are disabled, always plan a single card, whatever the number of goals, and do not load the disabled module.

After finishing the analysis and implementation plan, present the options once:

```text
- Confirm the plan and use the kanban board (create the cards and start)
- Confirm the plan and use the kanban board (create the cards, do not start yet)
- Confirm the plan, skip the board, do it in this session
- Adjust the plan
```

Number these options from `1` and make them the only numbered question in that message, so the numbers cannot collide with another question.

- Choosing `Confirm the plan and use the kanban board (create the cards and start)` authorizes the plan, the development, and the kanban flow at once, including the merge-back steps the plan states; do not ask again before starting work or before integrating. This covers every standalone card and every task group named in the confirmed plan.

  For a standalone card with `rules.git=true`, this execution authorization also covers integration into `develop` and cleanup, with the conditions stated in `KANDER-GIT-RULES.md` "Commit and Push"; do not request a separate merge-back confirmation. The completion flow is defined in `KANDER-KANBAN-RULES.md` "Execution and Completion".

  For a single card, and for each of several independent single cards, run in order: `kander new`, fill in the complete contract according to the confirmed plan, complete the self-review and any applicable independent card review per `KANDER-KANBAN-RULES.md` "Post-Creation Self-Review" and fix the findings, `kander pick <task-id>` (defined in `KANDER-KANBAN-RULES.md` "Command Contract"), `kander start <task-id>`. Start and tracking responsibilities follow `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination"; the discussing agent no longer implements a card that has been delegated.

  For a task group, create every member card the same way, complete the group-level checks in `KANDER-TASK-GROUP-RULES.md` "Task Splitting and Task Groups", then orchestrate per that file. The confirmed plan is the orchestration plan; it already carries the integration authorization for each group it names.

- Choosing `Confirm the plan and use the kanban board (create the cards, do not start yet)` authorizes the plan and card creation only. Create the cards, complete the self-review and applicable independent card review, and leave them in `backlog/`; do not move them to `todo/` or start them. Starting later requires a further user instruction; that instruction enters the same flow as the first option from `kander pick` onward (after `pick`, `kander move <task-id> working --owner <agent>` replaces `kander start` only when the user explicitly asks this agent to execute the card itself, per `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination"), and it carries the integration authorization of the confirmed plan; do not ask for it again.
- Choosing `Confirm the plan, skip the board, do it in this session` implements directly per the project rules, without creating a card.
- Choosing `Adjust the plan` modifies the plan; no card is created or started yet.
