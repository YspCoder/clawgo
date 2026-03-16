# HEARTBEAT.md

Heartbeat is for low-noise world maintenance.

## Required Checks
- inspect recent world events
- inspect pending quest or task pressure
- inspect whether any NPC or background process is obviously stalled
- if the user was active recently, pause non-essential background work

## Priority Rule
- user activity always outranks heartbeat work
- heartbeat should not compete with live user requests

## Output Rule
- if nothing meaningful changed, reply exactly: `HEARTBEAT_OK`
- if there is a real blocker or high-value world change, send a short alert with:
  - what changed
  - why it matters
  - what should happen next

## Do Not Do
- do not emit noisy routine summaries
- do not narrate trivial world drift
- do not mutate major world state without a clear reason
