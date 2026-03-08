# Node P2P E2E

这份文档用于验证 `gateway.nodes.p2p` 的两条真实数据面：

- `websocket_tunnel`
- `webrtc`

目标不是单元测试，而是两台公网机器上的真实联通性验证。

## 验证目标

验证通过需要同时满足：

1. 两台远端 node 都能成功注册到同一个 gateway
2. `websocket_tunnel` 模式下，远端 node 任务可成功完成
3. `webrtc` 模式下，远端 node 任务可成功完成
4. `webrtc` 模式下，`/webui/api/nodes` 的 `p2p.active_sessions` 大于 `0`
5. `Dashboard` / `Subagents` 能看到 node P2P 会话状态和最近调度路径

## 前置条件

- 一台 gateway 机器
- 两台远端 node 机器
- 三台机器都能运行 `clawgo`
- 远端机器有 `python3`
- gateway 机器对外开放 WebUI / node registry 端口

推荐：

- 先验证 `websocket_tunnel`
- 再切到 `webrtc`
- `webrtc` 至少配置一个可用的 `stun_servers`

## 测试思路

为了排除 HTTP relay 误判，建议让目标 node 的 `endpoint` 故意写成只对目标 node 本机有效的地址，例如：

```text
http://127.0.0.1:<port>
```

这样如果任务仍能完成，就说明请求不是靠 gateway 直接 HTTP relay 打过去的，而是走了 node P2P 通道。

## 建议配置

### 1. websocket_tunnel

```json
{
  "gateway": {
    "host": "0.0.0.0",
    "port": 18790,
    "token": "YOUR_GATEWAY_TOKEN",
    "nodes": {
      "p2p": {
        "enabled": true,
        "transport": "websocket_tunnel",
        "stun_servers": [],
        "ice_servers": []
      }
    }
  }
}
```

### 2. webrtc

```json
{
  "gateway": {
    "host": "0.0.0.0",
    "port": 18790,
    "token": "YOUR_GATEWAY_TOKEN",
    "nodes": {
      "p2p": {
        "enabled": true,
        "transport": "webrtc",
        "stun_servers": ["stun:stun.l.google.com:19302"],
        "ice_servers": []
      }
    }
  }
}
```

## 最小 node endpoint

在每台远端 node 上启动一个最小 HTTP 服务，用于返回固定结果：

```python
#!/usr/bin/env python3
import json
import os
import socket
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = int(os.environ.get("PORT", "19081"))
LABEL = os.environ.get("NODE_LABEL", socket.gethostname())

class H(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0") or 0)
        raw = self.rfile.read(length) if length else b"{}"
        try:
            req = json.loads(raw.decode("utf-8") or "{}")
        except Exception:
            req = {}
        action = req.get("action") or self.path.strip("/")
        payload = {
            "handler": LABEL,
            "hostname": socket.gethostname(),
            "path": self.path,
            "echo": req,
        }
        if action == "agent_task":
            payload["result"] = f"agent_task from {LABEL}"
        else:
            payload["result"] = f"{action} from {LABEL}"
        body = json.dumps({
            "ok": True,
            "code": "ok",
            "node": LABEL,
            "action": action,
            "payload": payload,
        }).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

HTTPServer(("0.0.0.0", PORT), H).serve_forever()
```

## 注册远端 node

在每台 node 上执行：

```bash
clawgo node register \
  --gateway http://<gateway-host>:18790 \
  --token YOUR_GATEWAY_TOKEN \
  --id <node-id> \
  --name <node-name> \
  --endpoint http://127.0.0.1:<endpoint-port> \
  --actions run,agent_task \
  --models gpt-4o-mini \
  --capabilities run,invoke,model \
  --watch \
  --heartbeat-sec 10
```

验证注册成功：

```bash
curl -s -H 'Authorization: Bearer YOUR_GATEWAY_TOKEN' \
  http://<gateway-host>:18790/webui/api/nodes
```

预期：

- 远端 node 出现在 `nodes`
- `online = true`
- 主拓扑中出现 `node.<id>.main`

## 建议的任务验证方式

不要通过普通聊天 prompt 让模型“自己决定是否调用 nodes 工具”作为主判据。  
更稳定的方式是直接调用 subagent runtime，把任务派给远端 node branch：

```bash
curl -s \
  -H 'Authorization: Bearer YOUR_GATEWAY_TOKEN' \
  -H 'Content-Type: application/json' \
  http://<gateway-host>:18790/webui/api/subagents_runtime \
  -d '{
    "action": "dispatch_and_wait",
    "agent_id": "node.<node-id>.main",
    "task": "Return exactly the string NODE_P2P_OK",
    "wait_timeout_sec": 30
  }'
```

预期：

- `ok = true`
- `result.reply.status = completed`
- `result.reply.result` 含远端 endpoint 返回内容

## websocket_tunnel 判定

在 `websocket_tunnel` 模式下，上面的任务应能成功完成。

如果目标 node 的 `endpoint` 配成了 `127.0.0.1:<port>`，且任务仍成功，则说明：

- 不是 gateway 直接 HTTP relay 到远端公网地址
- 实际请求已经通过 node websocket 隧道送达目标 node

## webrtc 判定

切到 `webrtc` 配置后，重复同样的 `dispatch_and_wait`。

随后查看：

```bash
curl -s -H 'Authorization: Bearer YOUR_GATEWAY_TOKEN' \
  http://<gateway-host>:18790/webui/api/nodes
```

预期 `p2p` 段包含：

- `transport = "webrtc"`
- `active_sessions > 0`
- `nodes[].status = "open"`
- `nodes[].last_ready_at` 非空

这表示 WebRTC DataChannel 已经真正建立，而不只是 signaling 被触发。

## WebUI 判定

验证页面：

- `Dashboard`
  - 能看到 Node P2P 会话明细
  - 能看到最近节点调度记录，包括 `used_transport` 和 `fallback_from`
- `Subagents`
  - 远端 node branch 的卡片/tooltip 能显示：
    - P2P transport
    - session status
    - retry count
    - last ready
    - last error

## 常见问题

### 1. gateway 端口上已经有旧实例

现象：

- 新配置明明改了，但 `/webui/api/version` 或 `/webui/api/nodes` 仍表现出旧行为

处理：

- 先确认端口上实际监听的是哪一个 `clawgo` 进程
- 再启动测试实例

### 2. chat 路由干扰 node 工具验证

现象：

- 普通聊天请求被 router 或 skill 行为分流
- 没有真正命中 `nodes` 数据面

处理：

- 直接用 `/webui/api/subagents_runtime` 的 `dispatch_and_wait`
- 让任务明确走 `node.<id>.main`

### 3. webrtc 一直停在 connecting

优先检查：

- `stun_servers` 是否可达
- 两端机器是否允许 UDP 出站
- 是否需要 `turn:` / `turns:` 服务器

### 4. 任务成功但 UI 没显示会话

优先检查：

- 是否真的运行在 `webrtc` 配置下
- `/webui/api/nodes` 返回的 `p2p` 是否含 `active_sessions`
- 前端是否已经更新到包含 node P2P runtime 展示的版本

## 回归建议

每次改动以下模块后，至少回归一次本流程：

- `pkg/nodes/webrtc.go`
- `pkg/nodes/transport.go`
- `pkg/agent/loop.go`
- `pkg/api/server.go`
- `cmd/clawgo/cmd_node.go`
- `cmd/clawgo/cmd_gateway.go`
