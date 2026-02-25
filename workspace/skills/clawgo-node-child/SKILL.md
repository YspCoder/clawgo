---
name: clawgo-node-child
description: Deploy and register a clawgo child node to a parent clawgo gateway without ad-hoc Python agents. Use when setting up sub-nodes, syncing provider settings from parent to child, enabling reverse tunnel fallback, and validating parent-dispatched nodes actions (describe/run/invoke/agent_task).
---

# Clawgo Node Child

Use built-in clawgo services only. Do not create `node_agent.py`.

## Execute

1. Run `scripts/deploy_child.sh` on the operator host with env vars:
   - `PARENT_HOST` `PARENT_PASS`
   - `CHILD_HOST` `CHILD_PASS`
   - Optional: `PARENT_GATEWAY_PORT` (default `18790`), `RELAY_PORT` (default `17789`), `CHILD_PORT` (default `7789`), `NODE_ID` (default `node-child`)
2. Script will:
   - Copy `/usr/local/bin/clawgo` from parent to child
   - Sync `providers` + `agents.defaults` into child `/root/.clawgo/provider-sync.json`
   - Create child gateway config `/root/.clawgo/config.child.json`
   - Install/start child service `clawgo-child-gateway.service`
   - Install/start reverse tunnel service `clawgo-child-revtunnel.service`
   - Register child node to parent `/nodes/register`
   - Install child heartbeat cron (`/nodes/heartbeat` every minute)
3. Validate from parent:
   - Health: `curl http://127.0.0.1:${RELAY_PORT}/health`
   - Task dispatch: `clawgo agent` + `nodes action=agent_task node=<NODE_ID> mode=relay ...`

## Notes

- Prefer direct endpoint if reachable; keep reverse tunnel as fallback.
- If parent and CLI visibility differ, ensure node state persistence is enabled (`memory/nodes-state.json`).
- If child service fails, check:
  - `journalctl -u clawgo-child-gateway.service -n 100 --no-pager`
  - `journalctl -u clawgo-child-revtunnel.service -n 100 --no-pager`
