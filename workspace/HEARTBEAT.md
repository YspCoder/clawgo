# HEARTBEAT.md

## Required checks
- Review `memory/tasks.json` and pending TODOs.
- If user was active recently, pause autonomy work.

## Output rule
- If nothing important changed: reply `HEARTBEAT_OK`.
- If blocked/high-value change: send concise alert with next step.
