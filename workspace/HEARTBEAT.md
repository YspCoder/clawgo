# HEARTBEAT.md

## Required checks
- Review pending TODOs and recent task audit records.
- If user was active recently, pause background maintenance work.

## Output rule
- If nothing important changed: reply `HEARTBEAT_OK`.
- If blocked/high-value change: send concise alert with next step.
