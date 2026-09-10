# Frontend Verification

Frontend verification is a task-triggered protocol, not a global requirement. Load `rules/KANDER-FRONTEND-RULES.md` when a task changes a frontend, web UI, browser interaction, page layout, styles, screenshots, or visual fidelity, or when the user explicitly requests frontend verification, browser E2E, visual regression, or visual acceptance.

Backend-only, CLI-only, library-only, documentation-only, and configuration-only tasks do not load this module unless the user asks for frontend verification.

## Levels

- **L0 static:** inspect source or rendered markup and assert required structure, text, routes, and configuration.
- **L1 client logic:** test state, events, validation, and rendering logic.
- **L2 API integration:** make requests and assert status, payload, errors, and user-visible handling.
- **L3 browser E2E:** exercise the acceptance flow in a real browser and record the environment, steps, result, and useful screenshots.
- **L4 visual regression:** compare current screenshots with an approved baseline and preserve the comparison result and diff.
- **L5 visual fidelity:** compare the result with the approved design reference; the user or an explicitly designated visual reviewer decides acceptance.

Use only the levels that apply to the acceptance criteria. Record `N/A` and the reason for an inapplicable level. Record an environment limitation when a required level cannot run. Never record an unexecuted level as passed.

## Current command boundary

Kander currently does not provide `kander e2e` or `kander visual` commands. Do not invent those commands or claim they ran. Use the project's available browser, test, and image tools, and record the exact command or manual procedure. This document defines evidence and acceptance expectations; it does not add a CLI command or configuration switch.

## Card and report evidence

Record concise level-by-level results in the card's implementation or summary document and in the completion report when applicable:

- level;
- command or manual procedure;
- environment and URL when relevant;
- pass, fail, skipped, or blocked result;
- screenshot, baseline, diff, log, or report path;
- unresolved limitation and its effect on acceptance.

Keep complete logs and review artifacts in their controlled locations. Do not paste a session transcript into the card. A later session should be able to resume from the card, Git history, verification artifacts, and controlled review/dispatch records.

## Anti-fake-green rule

For every important assertion, ask whether the defect could still exist while the assertion passes. If yes, strengthen the assertion or add a direct behavior check. Do not skip, weaken, delete, or exclude a failing test to obtain a green result. A successful build, route response, or element-presence check is not a substitute for the behavior it is meant to prove.

When practical, temporarily break the protected behavior and confirm that the critical check fails, then restore the change before delivery. Record the result as evidence. The executing agent may provide L5 screenshots and differences, but must not declare visual fidelity accepted on the user's behalf.
