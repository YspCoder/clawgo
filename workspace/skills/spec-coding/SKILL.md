---
name: spec-coding
description: Drive non-trivial coding work through spec.md, tasks.md, and checklist.md in the current project root. Use for feature delivery, multi-file refactors, architectural changes, or any implementation that needs scope control and explicit verification.
---

# Spec Coding

Use this skill for non-trivial coding work. The goal is to keep implementation aligned with a living spec, an explicit task breakdown, and a final verification gate.

Important:

- The files inside `workspace/skills/spec-coding/` are templates and workflow instructions only.
- Real project state must live in the coding target project itself.
- Do not treat the skill directory as shared runtime state across projects.
- Do not turn this on for small tweaks or lightweight edits unless the user explicitly asks for spec-driven workflow.

## Files

Maintain these files in the current coding project root by default:

- `spec.md`
- `tasks.md`
- `checklist.md`

If the user explicitly asks for a different location, follow that instead.
Do not create these files for non-coding tasks unless the user explicitly wants the workflow.

## Intent

- `spec.md` is the north-star document.
  It explains what is being built, why it matters, what is in scope, what is out of scope, and which decisions / tradeoffs were made.
- `tasks.md` is the live implementation plan.
  It breaks the spec into modules, tasks, and ordered execution steps. Update it as work progresses.
- `checklist.md` is the completion gate.
  Only mark the project done after walking this file and confirming implementation, behavior, and validation are complete.

## Workflow

### 1) Bootstrap

If any of the three files are missing, scaffold them first.

Use:

```bash
skill_exec(spec="spec-coding", script="scripts/init.sh")
```

Optional target directory:

```bash
skill_exec(spec="spec-coding", script="scripts/init.sh", args=["/path/to/project"])
```

### 2) Write / refine the spec

Before substantial code changes:

- summarize the problem
- define desired outcome
- define in-scope / out-of-scope
- capture decisions and tradeoffs
- note risks / open questions

Keep it concrete and engineering-oriented.

### 3) Expand into tasks

Break the spec into:

- modules or workstreams
- sub-tasks
- ordered implementation steps

Update statuses as you progress. The task plan is expected to evolve.

### 4) Implement against the plan

As work changes:

- update `spec.md` when scope or key decisions shift
- update `tasks.md` as tasks are completed / deferred / added
- do not wait until the end to synchronize the docs

### 5) Verify before close-out

When implementation is done:

- walk `checklist.md`
- verify code changes
- verify tests / validation
- verify no obvious scope gaps remain

Do not declare completion until the checklist has been reviewed.

## Quality bar

- Prefer concise, structured markdown over long prose.
- Keep docs useful for resuming work in a later session.
- Avoid fake precision; if something is undecided, mark it explicitly.
- For small one-file edits, skip this workflow by default.
- For anything multi-step or multi-file, this workflow should be visible in the repo.
