#!/usr/bin/env bash
set -euo pipefail

: "${PARENT_HOST:?need PARENT_HOST}"
: "${PARENT_PASS:?need PARENT_PASS}"
: "${CHILD_HOST:?need CHILD_HOST}"
: "${CHILD_PASS:?need CHILD_PASS}"

PARENT_GATEWAY_PORT="${PARENT_GATEWAY_PORT:-18790}"
RELAY_PORT="${RELAY_PORT:-17789}"
CHILD_PORT="${CHILD_PORT:-7789}"
NODE_ID="${NODE_ID:-node-child}"

SSHP_PARENT=(sshpass -p "$PARENT_PASS" ssh -o StrictHostKeyChecking=no "root@${PARENT_HOST}")
SSHP_CHILD=(sshpass -p "$CHILD_PASS" ssh -o StrictHostKeyChecking=no "root@${CHILD_HOST}")

# 1) Copy clawgo binary parent -> local -> child
sshpass -p "$PARENT_PASS" scp -o StrictHostKeyChecking=no "root@${PARENT_HOST}:/usr/local/bin/clawgo" /tmp/clawgo_child
sshpass -p "$CHILD_PASS" scp -o StrictHostKeyChecking=no /tmp/clawgo_child "root@${CHILD_HOST}:/usr/local/bin/clawgo"
"${SSHP_CHILD[@]}" "chmod +x /usr/local/bin/clawgo"

# 2) Sync providers + agents.defaults from parent
"${SSHP_PARENT[@]}" "python3 - <<'PY'
import json
c=json.load(open('/root/.clawgo/config.json'))
p={'providers':c.get('providers',{}),'agents_defaults':c.get('agents',{}).get('defaults',{})}
print(json.dumps(p,ensure_ascii=False))
PY" > /tmp/provider-sync.json
cat /tmp/provider-sync.json | "${SSHP_CHILD[@]}" "mkdir -p /root/.clawgo && cat > /root/.clawgo/provider-sync.json"

# 3) Build child config (no ad-hoc python server)
"${SSHP_CHILD[@]}" "python3 - <<'PY'
import json
src='/root/.clawgo/provider-sync.json'
out='/root/.clawgo/config.child.json'
d=json.load(open(src))
cfg={
  'gateway': {'host':'0.0.0.0','port': ${CHILD_PORT}},
  'providers': d.get('providers',{}),
  'agents': {'defaults': d.get('agents_defaults',{})},
  'channels': {},
  'tools': {'shell': {'enabled': True}},
  'logging': {'level':'info'}
}
json.dump(cfg, open(out,'w'), ensure_ascii=False, indent=2)
print(out)
PY"

# 4) Install child gateway service
"${SSHP_CHILD[@]}" "cat > /etc/systemd/system/clawgo-child-gateway.service <<EOF
[Unit]
Description=Clawgo Child Gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/clawgo gateway run --config /root/.clawgo/config.child.json
Restart=always
RestartSec=2
User=root
WorkingDirectory=/root

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now clawgo-child-gateway.service"

# 5) Install reverse tunnel service (child -> parent)
"${SSHP_CHILD[@]}" "yum install -y sshpass >/dev/null 2>&1 || true"
"${SSHP_CHILD[@]}" "cat > /etc/systemd/system/clawgo-child-revtunnel.service <<EOF
[Unit]
Description=Clawgo Child Reverse Tunnel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/sshpass -p ${PARENT_PASS} /usr/bin/ssh -o StrictHostKeyChecking=no -o ServerAliveInterval=20 -o ServerAliveCountMax=3 -N -R ${RELAY_PORT}:127.0.0.1:${CHILD_PORT} root@${PARENT_HOST}
Restart=always
RestartSec=3
User=root

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now clawgo-child-revtunnel.service"

# 6) Register + heartbeat
"${SSHP_CHILD[@]}" "curl -s -X POST http://${PARENT_HOST}:${PARENT_GATEWAY_PORT}/nodes/register -H 'Content-Type: application/json' -d '{\"id\":\"${NODE_ID}\",\"name\":\"${NODE_ID}\",\"os\":\"linux\",\"arch\":\"amd64\",\"version\":\"clawgo-child\",\"endpoint\":\"http://127.0.0.1:${RELAY_PORT}\",\"capabilities\":{\"run\":true,\"invoke\":true,\"model\":true},\"actions\":[\"run\",\"invoke\",\"agent_task\"],\"models\":[\"local-sim\"]}'"
"${SSHP_CHILD[@]}" "(crontab -l 2>/dev/null; echo '*/1 * * * * curl -s -X POST http://${PARENT_HOST}:${PARENT_GATEWAY_PORT}/nodes/heartbeat -H \"Content-Type: application/json\" -d \"{\\\"id\\\":\\\"${NODE_ID}\\\"}\" >/dev/null 2>&1') | crontab -"

echo "DONE: ${NODE_ID} deployed"
