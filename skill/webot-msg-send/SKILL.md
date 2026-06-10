---
name: webot-msg-send
description: Send text messages through a locally running webot-msg service and discover its HTTP address, bot ID, and API token from local webot-msg runtime files. Use when an agent needs to call webot-msg, reply through the bot message API, construct /bots/{botID}/messages HTTP requests, find the configured port or auth store, or troubleshoot local webot-msg send failures.
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

Never show `bot_token`, `api_token`, or `context_token` in user-visible output.

## Linux CLI Requirements

- Required: `curl`, `python3`.
- Useful when available: `pgrep`, `ps`, `systemctl`, `ss`.
- Do not use GUI browser automation, desktop notifications, clipboard tools, or macOS-only commands.

## Workflow

1. Locate the running service config.
   - If a running process uses `-port N`, use `N`; it overrides TOML.
   - If a running process uses `-c path`, read that TOML.
   - Otherwise read `~/.webot-msg/config/webot-msg.toml` when it exists.
   - If no TOML exists, use defaults.
   - On Linux, inspect the process with `pgrep -af 'webot-msg'` or `ps -ef | grep '[w]ebot-msg'`.
   - For systemd deployments, inspect `systemctl cat webot-msg` and `systemctl show webot-msg -p User -p FragmentPath` when permitted.

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

The server also accepts `token` as a query, form, or JSON field, but prefer the `Authorization` header because tokens in URLs are easier to leak.

### Typing Status

Use only when the user asks for typing status behavior.

```http
POST /bots/{botID}/typing
Authorization: Bearer {api_token}
Content-Type: application/json

{"status":1}
```

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

- `200 OK`: message was accepted by `webot-msg`.
- `400 Missing text`: request body did not include non-empty `text`.
- `400 Context not ready`: the bot has no current reply context; wait for an incoming message or fix login/context state.
- `401 Unauthorized`: wrong or missing `api_token`, or token does not belong to that `bot_id`.
- `404 Bot not found`: the selected `bot_id` is not in the auth store used by the running service.
- `404 Unknown action`: endpoint path is wrong.
- `500`: upstream iLink send failed; report the error string without exposing secrets.

## Safety Rules

- Treat sending as a side effect. Only send when the user clearly requested it and the message text is known.
- Keep calls on `127.0.0.1` unless the user explicitly gives a remote deployment.
- Do not commit, paste, or log auth store secrets.
- Do not edit `auth.json` while sending messages.
- If service user differs from the agent user, inspect the process or service configuration before assuming `~`.
