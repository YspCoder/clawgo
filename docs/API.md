# Gateway API

This document describes the HTTP and WebSocket API exposed by `clawgo gateway run`.
It is based on the routes registered in `pkg/api/server.go` and the gateway wiring in
`cmd/cmd_gateway.go`.

## Authentication

All `/api/*` routes and protected extra routes require the gateway token when
`gateway.token` is non-empty. `/health` does not require authentication.

Accepted token transports:

- `Authorization: Bearer <gateway.token>`
- Query parameter: `?token=<gateway.token>`
- Cookie: `clawgo_webui_token=<gateway.token>`
- Browser asset fallback: a request whose `Referer` URL contains the same `token`

If `gateway.token` is empty, API authentication is disabled.

Unauthenticated requests return HTTP `401` with plain text:

```text
unauthorized
```

## Base URL

The gateway listens on `gateway.host` and `gateway.port` from `config.json`.
The sample config uses:

```text
http://localhost:18790
```

Examples below use:

```bash
BASE=http://localhost:18790
TOKEN=<gateway.token>
```

JSON endpoints use `Content-Type: application/json` unless noted otherwise.

## Live Connections

The current live endpoints are WebSocket endpoints, not SSE endpoints:

- `GET /api/chat/live`
- `GET /api/events/live`
- `GET /api/logs/live`

Connect with the same token rules as HTTP requests, for example:

```text
ws://localhost:18790/api/events/live?token=<gateway.token>
```

## Health Check

### `GET /health`

Purpose: liveness probe for the gateway HTTP process.

Authentication: none.

Response:

```text
ok
```

## Config

### `GET /api/config`

Purpose: read the merged gateway config. Defaults are merged with the configured
`config.json` content.

Authentication: required when `gateway.token` is set.

Query parameters:

| Name | Description |
| --- | --- |
| `mode=normalized` | Return normalized config view plus `raw_config`. |
| `mode=hot` | Return merged config plus hot reload field metadata. |
| `include_hot_reload_fields=1` | Include hot reload field names and details. |

Example:

```bash
curl -H "Authorization: Bearer $TOKEN" "$BASE/api/config?mode=normalized"
```

Response:

```json
{
  "ok": true,
  "config": {},
  "raw_config": {}
}
```

### `POST /api/config`

Purpose: save config and trigger gateway reload behavior.

Authentication: required.

Query parameters:

| Name | Description |
| --- | --- |
| `mode=raw` or omitted | Body is the raw config shape. |
| `mode=normalized` | Body is the normalized config view. |

Example body:

```json
{
  "gateway": {
    "host": "0.0.0.0",
    "port": 18790,
    "token": ""
  }
}
```

Success response:

```json
{
  "ok": true
}
```

Validation errors return HTTP `400`:

```json
{
  "ok": false,
  "error": "invalid config: ...",
  "errors": ["..."]
}
```

## Chat

### `POST /api/chat`

Purpose: send a direct message to the Agent runtime and receive one complete reply.

Authentication: required.

Request body:

| Field | Required | Description |
| --- | --- | --- |
| `session` | No | Session key. Defaults to query `session`, then `main`. |
| `message` | No | Prompt text. |
| `media` | No | Uploaded file path. Appended to the prompt as `[file: <path>]`. |

Example:

```bash
curl -X POST "$BASE/api/chat" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"session":"main","message":"Hello"}'
```

Response:

```json
{
  "ok": true,
  "reply": "Hello! How can I help?",
  "session": "main"
}
```

## Chat History

### `GET /api/chat/history`

Purpose: read stored messages for a session through the gateway history callback.

Authentication: required.

Query parameters:

| Name | Description |
| --- | --- |
| `session` | Session key. Defaults to `main`. |

Response:

```json
{
  "ok": true,
  "session": "main",
  "messages": [
    {
      "role": "user",
      "content": "Hello"
    }
  ]
}
```

## Live Chat

### `GET /api/chat/live`

Purpose: WebSocket chat request that streams the completed reply in small JSON
chunks.

Authentication: required.

Protocol:

1. Client opens the WebSocket.
2. Client sends one JSON message with `session`, `message`, and optional `media`.
3. Server replies with zero or more `chat_chunk` messages and one `chat_done`
   message, or a `chat_error` message.

Client message:

```json
{
  "session": "main",
  "message": "Summarize today",
  "media": ""
}
```

Server chunk:

```json
{
  "ok": true,
  "type": "chat_chunk",
  "session": "main",
  "delta": "Partial reply text"
}
```

Done message:

```json
{
  "ok": true,
  "type": "chat_done",
  "session": "main"
}
```

Error message:

```json
{
  "ok": false,
  "type": "chat_error",
  "error": "invalid json",
  "session": "main"
}
```

## Events

### `GET /api/events/live`

Purpose: WebSocket event stream for gateway-side events such as config changes.

Authentication: required.

Initial message:

```json
{
  "type": "ready"
}
```

Known event payload example:

```json
{
  "type": "config_changed",
  "source": "webui"
}
```

The connection stays open until the client disconnects.

## Version

### `GET /api/version`

Purpose: read gateway build version and compiled channel keys.

Authentication: required.

Response:

```json
{
  "ok": true,
  "gateway_version": "devel",
  "compiled_channels": ["weixin", "telegram", "feishu"]
}
```

## Provider OAuth

Provider endpoints use the configured provider when `provider` is empty. Provider
updates save `config.json` and trigger a runtime reload hook when available.

### `GET|POST /api/provider/oauth/start`

Purpose: start a manual OAuth login flow.

Authentication: required.

GET query parameters or POST JSON fields:

| Field | Description |
| --- | --- |
| `provider` | Provider name. Defaults to primary provider. |
| `account_label` | Optional label for the imported account. |
| `network_proxy` | Optional proxy override. |
| `provider_config` | POST only. Inline provider config override. |

Response:

```json
{
  "ok": true,
  "flow_id": "1710000000000000000",
  "mode": "manual",
  "auth_url": "https://example.com/oauth",
  "user_code": "",
  "instructions": "Open the URL and paste the callback.",
  "account_label": "work",
  "network_proxy": ""
}
```

### `POST /api/provider/oauth/complete`

Purpose: complete a previously started manual OAuth flow and persist credentials.

Authentication: required.

Request body:

```json
{
  "provider": "codex",
  "flow_id": "1710000000000000000",
  "callback_url": "http://localhost:1455/callback?code=...",
  "account_label": "work",
  "network_proxy": ""
}
```

Response:

```json
{
  "ok": true,
  "account": "user@example.com",
  "credential_file": "/home/user/.clawgo/auth/codex-work.json",
  "network_proxy": "",
  "models": ["gpt-5.4"]
}
```

### `POST /api/provider/oauth/import`

Purpose: import an OAuth credential JSON file with multipart form data.

Authentication: required.

Multipart fields:

| Field | Required | Description |
| --- | --- | --- |
| `file` | Yes | Auth JSON file. |
| `provider` | No | Provider name. |
| `account_label` | No | Account label. |
| `network_proxy` | No | Proxy override. |
| `provider_config` | No | JSON encoded inline provider config. |

Response:

```json
{
  "ok": true,
  "account": "user@example.com",
  "credential_file": "/home/user/.clawgo/auth/codex-work.json",
  "network_proxy": "",
  "models": ["gpt-5.4"]
}
```

### `GET /api/provider/oauth/accounts`

Purpose: list OAuth accounts for a provider.

Authentication: required.

Query parameters:

| Name | Description |
| --- | --- |
| `provider` | Provider name. Defaults to primary provider. |

Response:

```json
{
  "ok": true,
  "accounts": []
}
```

### `POST /api/provider/oauth/accounts`

Purpose: manage a provider OAuth account.

Authentication: required.

Query parameters:

| Name | Description |
| --- | --- |
| `provider` | Provider name. Defaults to primary provider. |

Request body:

```json
{
  "action": "refresh",
  "credential_file": "/home/user/.clawgo/auth/codex-work.json"
}
```

Supported actions:

- `refresh`
- `delete`
- `clear_cooldown`

Response examples:

```json
{
  "ok": true,
  "account": {}
}
```

```json
{
  "ok": true,
  "deleted": true
}
```

```json
{
  "ok": true,
  "cleared": true
}
```

## Provider Models / Runtime

### `POST /api/provider/models`

Purpose: replace the configured model list for a provider.

Authentication: required.

Request body:

```json
{
  "provider": "openai",
  "model": "gpt-5.4",
  "models": ["gpt-5.4", "gpt-5.4-mini"]
}
```

At least one value from `model` or `models` is required.

Response:

```json
{
  "ok": true,
  "models": ["gpt-5.4", "gpt-5.4-mini"]
}
```

### `GET /api/provider/runtime`

Purpose: inspect provider runtime state and history.

Authentication: required.

Query parameters:

| Name | Description |
| --- | --- |
| `provider` | Provider name. Defaults to primary provider where applicable. |
| `kind` | Runtime event kind filter. |
| `reason` | Runtime reason filter. |
| `target` | Runtime target filter. |
| `sort` | Sort mode passed to provider runtime view. |
| `changes_only=true` | Include only change events. |
| `window_sec` | Time window in seconds. |
| `limit` | Max result count. |
| `cursor` | Cursor offset. |
| `health_below` | Filter by health threshold. |
| `cooldown_until_before_sec` | Filter cooldowns before now plus this many seconds. |

Response:

```json
{
  "ok": true,
  "view": {}
}
```

### `POST /api/provider/runtime`

Purpose: operate on provider runtime metadata.

Authentication: required.

Request body:

```json
{
  "provider": "codex",
  "action": "refresh_now",
  "only_expiring": true
}
```

Supported actions:

- `clear_api_cooldown`
- `clear_history`
- `refresh_now`
- `rerank`

Response examples:

```json
{
  "ok": true,
  "cleared": true
}
```

```json
{
  "ok": true,
  "provider": "codex",
  "refreshed": true,
  "result": {},
  "candidate_order": [],
  "summary": {}
}
```

```json
{
  "ok": true,
  "provider": "codex",
  "reranked": true,
  "candidate_order": []
}
```

## Weixin Login

### `GET /api/weixin/status`

Purpose: read Weixin channel status, pending login records, and accounts.

Authentication: required.

Response:

```json
{
  "ok": true,
  "enabled": false,
  "base_url": "https://ilinkai.weixin.qq.com",
  "pending_logins": [],
  "pending_login": {
    "login_id": "",
    "qr_available": false
  },
  "accounts": []
}
```

If the channel is unavailable, the endpoint returns HTTP `200` with `ok: false`
and an `error` field.

### `POST /api/weixin/login/start`

Purpose: start a Weixin QR login flow.

Authentication: required.

Request body: none.

Response: same shape as `GET /api/weixin/status`.

### `POST /api/weixin/login/cancel`

Purpose: cancel a pending Weixin login by ID.

Authentication: required.

Request body:

```json
{
  "login_id": "login-id"
}
```

Response: same shape as `GET /api/weixin/status`.

### `GET /api/weixin/qr.svg`

Purpose: render the current or selected pending Weixin login QR code as SVG.

Authentication: required.

Query parameters:

| Name | Description |
| --- | --- |
| `login_id` | Optional pending login ID. Defaults to the first pending login. |

Response headers:

```text
Content-Type: image/svg+xml
```

### `POST /api/weixin/accounts/remove`

Purpose: remove a Weixin account by bot ID.

Authentication: required.

Request body:

```json
{
  "bot_id": "bot-id"
}
```

Response: same shape as `GET /api/weixin/status`.

### `POST /api/weixin/accounts/default`

Purpose: set the default Weixin account by bot ID.

Authentication: required.

Request body:

```json
{
  "bot_id": "bot-id"
}
```

Response: same shape as `GET /api/weixin/status`.

## Upload

### `POST /api/upload`

Purpose: upload a file for later chat usage.

Authentication: required.

Content type: `multipart/form-data`.

Multipart fields:

| Field | Required | Description |
| --- | --- | --- |
| `file` | Yes | File to upload. |

The server stores files under the OS temp directory in `clawgo_webui_uploads`.

Response:

```json
{
  "ok": true,
  "path": "/tmp/clawgo_webui_uploads/1710000000000000000_input.txt",
  "name": "input.txt"
}
```

## Cron

### `GET /api/cron`

Purpose: list cron jobs or read one cron job.

Authentication: required.

Query parameters:

| Name | Description |
| --- | --- |
| `id` | Optional job ID. When present, returns one job. |

List response:

```json
{
  "ok": true,
  "jobs": []
}
```

Single job response:

```json
{
  "ok": true,
  "job": {}
}
```

### `POST /api/cron`

Purpose: create or mutate cron jobs through the gateway cron handler.

Authentication: required.

Query parameters:

| Name | Description |
| --- | --- |
| `id` | Optional job ID copied into the request args. |

Request body:

```json
{
  "action": "create",
  "name": "daily-check",
  "message": "Run daily check",
  "expr": "0 9 * * *",
  "deliver": false,
  "channel": "",
  "to": ""
}
```

Supported actions from the gateway runtime:

- `create`
- `update`
- `delete`
- `enable`
- `disable`
- `get`
- `list`

Legacy scheduling fields `kind`, `everyMs`, and `atMs` are still accepted by the
runtime for older clients.

Response:

```json
{
  "ok": true,
  "result": {}
}
```

## Skills

### `GET /api/skills`

Purpose: list skills, inspect a skill's files, or read a skill file.

Authentication: required.

Query parameters:

| Name | Description |
| --- | --- |
| `check_updates=1` | Check ClawHub for remote versions when `clawhub` is installed. |
| `id` | Skill ID for detail operations. |
| `files=1` | With `id`, list files in that skill. |
| `file` | With `id`, read one relative text file. |

List response:

```json
{
  "ok": true,
  "skills": [],
  "source": "clawhub",
  "clawhub_installed": false,
  "clawhub_path": ""
}
```

Files response:

```json
{
  "ok": true,
  "id": "example",
  "files": ["SKILL.md"]
}
```

File response:

```json
{
  "ok": true,
  "id": "example",
  "file": "SKILL.md",
  "content": "# example"
}
```

### `POST /api/skills`

Purpose: import, install, enable, disable, create, update, or write skill files.

Authentication: required.

Multipart upload:

- `Content-Type: multipart/form-data`
- `file`: `.zip`, `.tar`, `.tar.gz`, or `.tgz` archive containing one or more
  `SKILL.md` files.

Multipart response:

```json
{
  "ok": true,
  "imported": ["my-skill"]
}
```

JSON body fields:

| Field | Description |
| --- | --- |
| `action` | `install`, `enable`, `disable`, `write_file`, `create`, or `update`. |
| `id` | Existing skill ID. |
| `name` | Skill name. Defaults to `id`. |
| `description` | Used by `create` and `update`. |
| `tools` | String list used by `create` and `update`. |
| `system_prompt` | Used by `create` and `update`. |
| `file` | Relative file path for `write_file`. |
| `content` | File content for `write_file`. |
| `ignore_suspicious` | Adds `--force` to ClawHub install. |

Example:

```json
{
  "action": "disable",
  "name": "example"
}
```

Response:

```json
{
  "ok": true
}
```

### `DELETE /api/skills`

Purpose: delete an enabled or disabled skill directory.

Authentication: required.

Query parameters:

| Name | Required | Description |
| --- | --- | --- |
| `id` | Yes | Skill ID. |

Response:

```json
{
  "ok": true,
  "deleted": true,
  "id": "example"
}
```

## Sessions

### `GET /api/sessions`

Purpose: list user-facing session keys found in the main agent session store.

Authentication: required.

Query parameters:

| Name | Description |
| --- | --- |
| `include_internal=1` | Include internal, subagent, heartbeat, cron, and hook sessions. |

Response:

```json
{
  "ok": true,
  "sessions": [
    {
      "key": "main",
      "channel": "main"
    }
  ]
}
```

## Memory

### `GET /api/memory`

Purpose: list memory files or read one memory file.

Authentication: required.

Query parameters:

| Name | Description |
| --- | --- |
| `path` | Optional relative path. `MEMORY.md` reads from workspace root. |

List response:

```json
{
  "ok": true,
  "files": ["MEMORY.md"]
}
```

File response:

```json
{
  "ok": true,
  "path": "MEMORY.md",
  "content": "..."
}
```

### `POST /api/memory`

Purpose: write a memory file under `workspace/memory`.

Authentication: required.

Request body:

```json
{
  "path": "notes.md",
  "content": "Remember this."
}
```

Response:

```json
{
  "ok": true,
  "path": "notes.md"
}
```

### `DELETE /api/memory`

Purpose: delete a memory file under `workspace/memory`.

Authentication: required.

Query parameters:

| Name | Required | Description |
| --- | --- | --- |
| `path` | Yes | Relative memory file path. |

Response:

```json
{
  "ok": true,
  "deleted": true,
  "path": "notes.md"
}
```

## Workspace File

### `GET /api/workspace_file`

Purpose: read a relative text file under the configured workspace.

Authentication: required.

Query parameters:

| Name | Required | Description |
| --- | --- | --- |
| `path` | Yes | Relative workspace path. Absolute and `..` paths are rejected. |

Response:

```json
{
  "ok": true,
  "path": "AGENTS.md",
  "found": true,
  "content": "..."
}
```

### `POST /api/workspace_file`

Purpose: write a relative text file under the configured workspace. Parent
directories are created when needed.

Authentication: required.

Request body:

```json
{
  "path": "notes/today.md",
  "content": "..."
}
```

Response:

```json
{
  "ok": true,
  "path": "notes/today.md",
  "saved": true
}
```

## Tools

### `GET /api/tool_allowlist_groups`

Purpose: read built-in tool allowlist group definitions.

Authentication: required.

Response:

```json
{
  "ok": true,
  "groups": []
}
```

### `GET /api/tools`

Purpose: read the runtime tool catalog plus MCP-specific tool and server checks.

Authentication: required.

Response:

```json
{
  "tools": [],
  "mcp_tools": [],
  "mcp_server_checks": [
    {
      "name": "context7",
      "enabled": false,
      "transport": "stdio",
      "status": "disabled",
      "message": "server is disabled",
      "command": "npx",
      "resolved": "",
      "package": "@upstash/context7-mcp",
      "installer": "",
      "installable": false,
      "missing_command": false
    }
  ]
}
```

Note: this endpoint currently does not include an `ok` field.

## MCP Install

### `POST /api/mcp/install`

Purpose: install an MCP package through a supported package manager and resolve
the installed binary path.

Authentication: required.

Request body:

```json
{
  "package": "example-mcp",
  "installer": "uv"
}
```

Supported installers:

- `uv` (default): runs `uv tool install <package>`
- `bun`: runs `bun add -g <package>`

Response:

```json
{
  "ok": true,
  "package": "example-mcp",
  "output": "installed example-mcp via uv",
  "bin_name": "example-mcp",
  "bin_path": "/home/user/.local/bin/example-mcp"
}
```

## Logs

### `GET /api/logs/recent`

Purpose: read recent log entries from the configured gateway log file.

Authentication: required.

Query parameters:

| Name | Description |
| --- | --- |
| `limit` | Number of lines to scan. Defaults to `10`, maximum `200`. |

Response:

```json
{
  "ok": true,
  "logs": [
    {
      "time": "2026-05-10T00:00:00Z",
      "level": "INFO",
      "msg": "gateway started"
    }
  ]
}
```

JSON log lines are returned as parsed objects. Plain text lines are wrapped as
`time`, `level`, and `msg`.

### `GET /api/logs/live`

Purpose: WebSocket tail of new log entries from the configured gateway log file.

Authentication: required.

Server message:

```json
{
  "ok": true,
  "type": "log_entry",
  "entry": {
    "time": "2026-05-10T00:00:00Z",
    "level": "INFO",
    "msg": "gateway started"
  }
}
```

If the log file cannot be opened after the WebSocket upgrade, the server sends:

```json
{
  "ok": false,
  "error": "open ...: no such file or directory"
}
```

## Protected Extra Routes

### `GET /v1/ws`

Purpose: AIStudio relay WebSocket route registered by the gateway with
`SetProtectedRoute`.

Authentication: required.

Provider selection:

- Query parameter: `provider`
- Header: `X-Clawgo-Provider`
- Default: `aistudio`

This route is owned by `pkg/wsrelay` and is documented here only because the
gateway registers it as a protected API-facing route.

## Error Responses

Handlers mostly use Go's `http.Error`, so many errors are plain text with an
HTTP status code instead of JSON.

Common responses:

| Status | Body | Meaning |
| --- | --- | --- |
| `400` | `invalid json` or validation text | Invalid request body, missing required field, invalid path, or unsupported action. |
| `401` | `unauthorized` | Missing or invalid gateway token. |
| `404` | `... not found` | Requested skill, login, QR, or resource was not found. |
| `405` | `method not allowed` | HTTP method is not supported by that route. |
| `412` | `clawhub is not installed...` | Skill install requires ClawHub. |
| `429` | `clawhub rate limit exceeded...` | ClawHub rejected skill lookup or install due to rate limiting. |
| `500` | error text | Gateway config, filesystem, log, cron, or runtime handler failure. |
| `502` | error text | Upstream channel/login operation failed. |
| `503` | `weixin channel unavailable` | Weixin route needs an active Weixin channel. |

JSON validation responses may look like:

```json
{
  "ok": false,
  "error": "invalid config: ...",
  "errors": ["..."]
}
```

WebSocket endpoints report post-upgrade errors as JSON messages when possible.

## Common Flows

### Send a chat message

```bash
curl -X POST "$BASE/api/chat" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"session":"main","message":"Ping"}'
```

### Upload a file and reference it in chat

```bash
UPLOAD_PATH=$(
  curl -s -X POST "$BASE/api/upload" \
    -H "Authorization: Bearer $TOKEN" \
    -F "file=@./notes.txt" |
  jq -r .path
)

curl -X POST "$BASE/api/chat" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"session\":\"main\",\"message\":\"Read this file\",\"media\":\"$UPLOAD_PATH\"}"
```

### Start and complete OAuth

```bash
curl -X POST "$BASE/api/provider/oauth/start" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"provider":"codex","account_label":"work"}'
```

Open the returned `auth_url`, then call:

```bash
curl -X POST "$BASE/api/provider/oauth/complete" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"provider":"codex","flow_id":"<flow_id>","callback_url":"<callback_url>"}'
```

### Watch config and log events

Use WebSocket clients against:

```text
ws://localhost:18790/api/events/live?token=<gateway.token>
ws://localhost:18790/api/logs/live?token=<gateway.token>
```
