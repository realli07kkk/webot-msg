---
name: webot-msg-send
description: Send text messages through a locally running webot-msg service and discover its HTTP address, bot ID, API token, and protection-mode send outcome from local webot-msg runtime files and HTTP responses. Use when an agent needs to call webot-msg, reply through the bot message API, construct /bots/{botID}/messages HTTP requests, find the configured port or auth store, handle 202 queued protection responses, or troubleshoot local webot-msg send failures.
---

# webot-msg Send

Use the local `webot-msg` HTTP API to send text through a logged-in bot. Assume the agent and `webot-msg` run on the same machine unless the user says otherwise.

This skill is designed for CLI-only Linux hosts. Use shell commands, local files, and HTTP requests; do not require a browser or desktop UI.

## Core Facts

- Base URL is local: `http://127.0.0.1:{port}`.
- Default port is `26322`.
- Runtime config defaults to `~/.webot-msg/config/webot-msg.toml`.
- Auth store defaults to `~/.webot-msg/config/auth.json`.
- Legacy auth store may exist at `./config/auth.json`.
- Send endpoint is `POST /bots/{botID}/messages`.
- Auth is the bot's `api_token`; prefer `Authorization: Bearer {api_token}`.
- The API sends to the bot's current stored conversation context, not an arbitrary recipient. If `ilink_user_id` or `context_token` is empty, sending fails with `Context not ready`.
- Ordinary text sent through the API may be transformed by the service before reaching WeChat: successful direct sends append a uuid v7 message ID, and protection mode may also append a `[限流阈值] ...` status line.
- When protection mode freezes API sends, the service queues the text in Redis and returns `202` with `status: "queued"` instead of sending immediately. Treat that as accepted for later replay, not as a failure to retry immediately.

Never show `bot_token`, `api_token`, or `context_token` in user-visible output.

## Linux CLI Requirements

- Required: `curl`, `python3`.
- Useful when available: `pgrep`, `ps`, `systemctl`, `ss`.
- Do not use GUI browser automation, desktop notifications, clipboard tools, or macOS-only commands.

## Workflow

1. Locate the running service config.
   - Read `~/.webot-msg/config/webot-msg.toml` when it exists.
   - If no TOML exists, use defaults.
   - On Linux, inspect the process with `pgrep -af 'webot-msg'` or `ps -ef | grep '[w]ebot-msg'` only to confirm the service user when needed.
   - For systemd deployments, inspect `systemctl show webot-msg -p User -p FragmentPath` when permitted if the service user may differ from the agent user.

2. Resolve the API address.
   - Read `[api].port` from TOML.
   - Fall back to `26322`.
   - Use `http://127.0.0.1:{port}`, not `0.0.0.0`.
   - If uncertain whether the service is listening, check `ss -ltnp | grep ":${PORT}"` when `ss` is available.

3. Resolve the auth store.
   - Read `[storage].auth_path` from the same TOML.
   - Fall back to `~/.webot-msg/config/auth.json`.
   - If the default auth file is absent and the repo-local `./config/auth.json` exists, check the legacy file.
   - Expand `~` using the service user's home directory. In normal same-user deployments, this is the agent's `$HOME`.

4. Select a bot.
   - Auth JSON shape is:

```json
{
  "bots": {
    "BOT_ID": {
      "bot_id": "BOT_ID",
      "ilink_user_id": "...",
      "context_token": "...",
      "api_token": "..."
    }
  }
}
```

   - If exactly one bot exists, use it.
   - If multiple bots exist and the user did not specify one, ask which `bot_id` to use.
   - Do not invent or regenerate `api_token`. If it is missing, ask the user to start/restart `webot-msg` so it can create one.

5. Send the message with JSON.

```bash
curl -sS -X POST "http://127.0.0.1:${PORT}/bots/${BOT_ID}/messages" \
  -H "Authorization: Bearer ${API_TOKEN}" \
  -H "Content-Type: application/json" \
  --data "$(python3 -c 'import json,sys; print(json.dumps({"text": sys.argv[1]}))' "$TEXT")"
```

Use the exact user-requested message text. Do not send test messages unless the user explicitly asked for a send.
If the response is `202` with `status: "queued"`, tell the user the message was queued by protection mode and include the queue length; do not send the same text again unless the user explicitly asks.

## Request Reference

### Send Message

```http
POST /bots/{botID}/messages
Authorization: Bearer {api_token}
Content-Type: application/json

{"text":"message text"}
```

Successful response:

```json
{"code":200,"message":"OK"}
```

Protection queued response:

```json
{"code":202,"status":"queued","queued":3}
```

The server also accepts `token` as a query, form, or JSON field, but prefer the `Authorization` header because tokens in URLs are easier to leak.

### Typing Status

Use only when the user asks for typing status behavior.

```http
POST /bots/{botID}/typing
Authorization: Bearer {api_token}
Content-Type: application/json

{"status":1}
```

## Protection Mode Behavior

Protection mode is optional and controlled from the running service console with `/protection enable|disable|status`. It uses Redis and stores the on/off state in `~/.webot-msg/state/protection.json`; do not edit the state file or Redis keys while sending.

When protection is enabled:

- Direct API sends still return `200` when the message is sent immediately.
- The visible WeChat text for ordinary direct sends is the requested text plus service-added lines: an optional `[限流阈值] 剩余可发 ...` status line and a final uuid v7 message ID line.
- Once a bot is frozen by the message-count or 24h active-conversation window, API sends are enqueued per bot and return HTTP `202` with `status: "queued"` and `queued: N`. They are not delivered to iLink at request time.
- A queued message is replayed FIFO after the user sends a new message to the bot from WeChat, which resets the protection window. Replay prepends a `[积压补发] ...` line with the original API call time in `Asia/Shanghai`, then sends through the normal protected-send path, so it can still gain the status line and uuid v7 ID at actual delivery time.
- Queue max length is currently 1000 and queued payload TTL follows the protection active window, currently 24h by default. These are built-in values, not TOML settings.
- Queue full returns HTTP `503` with `{"code":503,"error":"send queue full"}`; that request was not accepted and may be retried later with caller-side backoff.
- Redis/protection state failures are fail-closed for ordinary text and can return HTTP `429` protection errors. Do not bypass protection by disabling it unless the user explicitly asks.
- Typing status, console sends, protection reminder messages, and ordinary iLink network-send failures do not enter the queue.
- Use `/protection status` in the running service console to inspect remaining send count, warning window, frozen reason, and queued message count.

## Discovery Snippet

Use this local snippet when you need a quick endpoint summary. It prints token presence, not token values.

```bash
python3 - <<'PY'
import json, os, pathlib, re

home = pathlib.Path.home()
cfg_path = home / ".webot-msg/config/webot-msg.toml"
port = 26322
auth_path = home / ".webot-msg/config/auth.json"

if cfg_path.exists():
    text = cfg_path.read_text()
    m = re.search(r'(?m)^\s*port\s*=\s*(\d+)\s*$', text)
    if m:
        port = int(m.group(1))
    m = re.search(r'(?m)^\s*auth_path\s*=\s*"([^"]+)"\s*$', text)
    if m:
        value = m.group(1)
        auth_path = pathlib.Path(str(home) + value[1:]) if value.startswith("~") else pathlib.Path(value)

if not auth_path.exists() and pathlib.Path("config/auth.json").exists():
    auth_path = pathlib.Path("config/auth.json")

print(f"base_url=http://127.0.0.1:{port}")
print(f"auth_path={auth_path}")
data = json.loads(auth_path.read_text())
for bot_id, bot in sorted((data.get("bots") or {}).items()):
    print(
        f"bot_id={bot_id} "
        f"api_token_present={bool(bot.get('api_token'))} "
        f"context_ready={bool(bot.get('ilink_user_id') and bot.get('context_token'))}"
    )
PY
```

For actual sending, read `api_token` from the selected bot in memory and pass it only to the HTTP request. Do not echo it back to the user.

## Error Handling

- `200 OK`: message was sent immediately by `webot-msg`.
- `202 Accepted`: protection mode queued the API text for later FIFO replay. Do not retry the same text automatically.
- `400 Missing text`: request body did not include non-empty `text`.
- `400 Context not ready`: the bot has no current reply context; wait for an incoming message or fix login/context state.
- `401 Unauthorized`: wrong or missing `api_token`, or token does not belong to that `bot_id`.
- `404 Bot not found`: the selected `bot_id` is not in the auth store used by the running service.
- `404 Unknown action`: endpoint path is wrong.
- `429`: protection mode rejected the send, usually fail-closed because Redis/protection state is unavailable. Report the protection error and do not retry in a tight loop.
- `503 send queue full`: protection queue did not accept this request. The caller may retry later with backoff.
- `500`: upstream iLink send failed; report the error string without exposing secrets.

## Safety Rules

- Treat sending as a side effect. Only send when the user clearly requested it and the message text is known.
- Keep calls on `127.0.0.1` unless the user explicitly gives a remote deployment.
- Do not commit, paste, or log auth store secrets.
- Do not edit `auth.json` while sending messages.
- If service user differs from the agent user, inspect the process or service configuration before assuming `~`.
