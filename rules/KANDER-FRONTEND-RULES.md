# Frontend Verification Rules

This module applies to tasks that change a frontend, web UI, browser interaction, page layout, styles, client-side behavior, screenshots, or visual fidelity. It also applies when the user explicitly requests frontend verification, browser end-to-end testing, visual regression, or visual acceptance.

It is not loaded for backend-only, CLI-only, library-only, documentation-only, or configuration-only work unless the user explicitly asks for frontend verification. This is a task-triggered rule module, not a global requirement that every task run all six levels.

The repository currently does not provide `kander e2e` or `kander visual` commands. Do not invent or claim those commands were run. Use the available project tools and record the actual command, environment, result, and evidence path. A missing browser, test server, screenshot tool, or design reference is a verification limitation and must be reported as such.

## Verification levels

Use the lowest levels that directly prove the acceptance criteria. Higher levels are required when the task contract, risk, or user request calls for them.

| Level | Name | Evidence | Typical owner |
| --- | --- | --- | --- |
| L0 | Static verification | Source, rendered markup, accessibility structure, required text, routes, and configuration inspected with concrete assertions | Executing agent |
| L1 | Client logic verification | Unit or integration tests for state, event, validation, and rendering logic; use a DOM test or real browser when needed | Executing agent |
| L2 | API and integration verification | Real or controlled requests with assertions on status, payload, error handling, and UI-visible behavior | Executing agent |
| L3 | Browser end-to-end verification | A real browser follows the acceptance flow; record URL, steps, environment, result, and screenshots when useful | Executing agent or verifier |
| L4 | Visual regression verification | Current screenshots are compared with an approved baseline; record the comparison tool, inputs, diff, and disposition | Executing agent plus human or visual reviewer for meaningful diffs |
| L5 | Visual fidelity acceptance | Screenshots are compared with the approved design reference for layout, typography, spacing, color, responsive behavior, and interaction states | User or explicitly designated visual reviewer |

## Level requirements

- L0-L2 belong in the task's implementation and verification records when they apply.
- L3 and L4 are separate evidence-producing activities when the task requires browser or visual confidence; they must not be replaced by a build-only result.
- L5 is an acceptance decision, not an implementation claim. The executing agent may provide screenshots and differences but must not declare visual fidelity accepted on the user's behalf.
- A task may intentionally skip a level when it does not apply. Record `N/A` with the reason; do not record an unexecuted level as passed.
- If a required level cannot run because of the environment, record the exact limitation and its impact. Do not silently downgrade the acceptance criterion.
- Screenshots and visual diffs are evidence only when their path, capture context, and relationship to the acceptance criterion are recorded.

## Evidence and handoff

Record concise facts in the card's implementation or summary document:

- verification level;
- command or manual procedure;
- environment and URL, when relevant;
- pass, fail, skipped, or blocked result;
- screenshot, baseline, diff, log, or report path;
- unresolved limitation and its effect on acceptance.

Keep full logs and review artifacts in their controlled locations. Do not paste a session transcript into the card. A later agent must be able to resume from the card, repository state, verification artifacts, and Git history without relying on the previous conversation.

## Anti-fake-green checks

For each important frontend assertion, ask whether the defect could still exist while the assertion passes. If yes, strengthen the assertion or add a direct behavior check.

- Do not skip, weaken, delete, or exclude a failing test to obtain a green result.
- Do not use a successful build, route response, or element-presence check as a substitute for the user-visible behavior it is meant to prove.
- Verify the actual event/call/data path, not merely a function name in a comment or an unused fixture.
- For a critical acceptance criterion, temporarily break the protected behavior when practical and confirm the check fails, then restore the change before delivery.
- Treat known environment failures as verification facts unless the task introduced or aggravated them.
- Review permissions, untrusted input, replay, race, and cross-module assumptions when the frontend change crosses those boundaries.

## Acceptance handoff

Before requesting user acceptance, the executing agent must provide the applicable level-by-level results and identify anything not verified. The user decides L5 visual fidelity and any explicitly reserved acceptance decision. Integration and completion follow the normal Kander review, Git, dispatch, and done gates; this module does not create a second state machine or review record.
