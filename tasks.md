# Task Breakdown (tasks.md)

## Workstreams

### 1. Workspace workflow
- [x] Define how spec-driven coding should work in ClawGo.
- [x] Update workspace policy to require `spec.md`, `tasks.md`, and `checklist.md` for non-trivial coding tasks.
- [x] Add a built-in `spec-coding` skill.

### 2. Scaffolding
- [x] Add a script that initializes the three markdown documents in the current project root.
- [x] Keep scaffolding idempotent so existing docs are preserved.
- [x] Reuse shared template files so shell and runtime initialization stay consistent.

### 3. Agent context
- [x] Extend the context builder to surface current-project `spec.md`, `tasks.md`, and `checklist.md` when present.
- [x] Add truncation to keep prompt growth bounded.
- [x] Add tests for loading and truncation behavior.
- [x] Restrict project planning docs to coding-oriented tasks only.
- [x] Auto-initialize spec docs in the coding target project root when a coding task begins and files are missing.
- [x] Auto-register the current coding request into `tasks.md`.
- [x] Auto-mark the request complete and append a progress note when the turn succeeds.
- [x] Reopen previously completed tasks when later repair/regression/debug work indicates they are not actually done.

### 4. Documentation
- [x] Document the workflow in `README.md`.
- [x] Apply the workflow to the current ClawGo enhancement itself by creating the three docs in the repo root.
- [x] Clarify that skill files are templates and real project docs live in the coding target project.

## Progress Notes
- The implementation stays lightweight on purpose: markdown files + built-in skill + policy + context loading.
- No new planner service or database state was introduced in this iteration.
