# Project Scope (spec.md)

## Overview
- What is being built: spec-driven coding support for ClawGo.
- Why this change is needed: coding tasks need a durable project scope, task plan, and verification gate instead of ad hoc reasoning scattered across chat turns.
- Desired outcome: ClawGo should guide non-trivial coding work through `spec.md`, `tasks.md`, and `checklist.md`, expose that workflow in workspace policy, make active project docs visible to the agent during execution, and keep `tasks.md` progress moving automatically during coding turns.
  Completed tasks should also be reopenable when later debugging or regression work shows the task is not actually done.

## In Scope
- Add a built-in `spec-coding` skill.
- Provide a scaffold script for `spec.md`, `tasks.md`, and `checklist.md`.
- Update workspace agent policy to prefer this workflow for non-trivial coding work.
- Load current-project spec docs into agent context when present.
- Document the workflow in the repo README.

## Out of Scope
- A dedicated planner UI for spec documents.
- Automatic task execution directly from `tasks.md`.
- Complex state synchronization beyond markdown documents.

## Decisions
- Use plain markdown files named exactly `spec.md`, `tasks.md`, and `checklist.md`.
- Default location is the current project root, not the long-term workspace directory.
- Expose the workflow primarily through workspace policy + built-in skill, not a new core planner service.
- Load project docs into the system prompt with truncation to keep context bounded.
- Only activate this workflow automatically for coding-oriented tasks, not for general chat or non-coding work.
- Treat `workspace/skills/spec-coding/` as template storage only; project-specific docs must live in the target coding project.

## Tradeoffs
- Loading project docs into prompt improves continuity but increases token usage when the files exist.
- Root-level markdown files are simple and transparent, but they do add repo-visible artifacts.
- A skill + prompt workflow is faster to adopt than introducing a new runtime subsystem, but it depends on agent discipline.

## Risks / Open Questions
- `tasks.md` can grow large; prompt truncation mitigates this but may eventually need smarter summarization.
- Some users may prefer a nested path such as `.clawgo/specs/`; current default stays simple unless explicitly requested.
