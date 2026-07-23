---
title: OpenClaw setup and testing
---

# OpenClaw setup and testing

This page covers the **OpenClaw side**. Every other guide deploys the bridge; none of them
work until OpenClaw exposes an endpoint the bridge can call.

Do this page **first**. You cannot finish the bridge setup without the two values it
produces: the Gateway URL and its auth token.

## One-sentence explanation

**OpenClaw's Gateway can serve an OpenAI-compatible `/v1/chat/completions` endpoint that
runs a real agent turn, and the bridge is what calls it.**

## What you are building

```text
+---------+      +--------+      +---------------------+      +---------------+
| ChatGPT | ---> | bridge | ---> | POST                |      | a real agent  |
+---------+      +--------+      | /v1/chat/completions| ---> | turn: shell,  |
                                 +---------------------+      | files, skills |
                                    ^
                                    +-- the endpoint you enable on this page
```

The agent that answers has its normal capabilities: shell commands, file access, and your
installed skills. It executes work rather than only describing it.

<div class="danger" markdown="1">
<div markdown="1">
<span class="danger-title">This endpoint is full operator access</span>

OpenClaw's own documentation is explicit: a valid token here restores the complete operator
scope set and runs turns with owner semantics. Anyone who can call it can do anything the
target agent can do, including running commands on this machine.

**Keep it on loopback or a private tailnet. Never expose it to the public internet.** The
bridge is what makes it safe to use from ChatGPT: it holds the Gateway token privately and
re-authenticates callers with its own separate key.
</div>
</div>

## Before you start

You need:

- OpenClaw installed, with its Gateway running
- access to edit `~/.openclaw/openclaw.json`
- the ability to restart the Gateway

---

## Step 1 — Confirm the Gateway is alive

The config lives at `~/.openclaw/openclaw.json`. The Gateway's default port is `18789`:

```bash
curl http://127.0.0.1:18789/health
```

Expect `{"ok":true,"status":"live"}`. If that fails, start the Gateway before going further.

### Back up the config first

You are about to hand-edit JSON that controls a running service:

```bash
cp ~/.openclaw/openclaw.json ~/.openclaw/openclaw.json.bak.$(date +%Y%m%d-%H%M%S)
```

---

## Step 2 — Enable the chat completions endpoint

**It is disabled by default.** Add the `http` block inside `gateway`:

```json
"gateway": {
  "port": 18789,
  "bind": "loopback",
  "http": {
    "endpoints": {
      "chatCompletions": {
        "enabled": true
      }
    }
  }
}
```

Leave `bind` as `loopback`. Step 5 publishes it to your tailnet without opening it to the
internet.

---

## Step 3 — Find the Gateway token

The bridge authenticates with the token already in your config:

```bash
python3 -c "import json;print(json.load(open('$HOME/.openclaw/openclaw.json'))['gateway']['auth']['token'])"
```

That value becomes `OPENCLAW_GATEWAY_TOKEN` in the bridge's `.env`.

If `gateway.auth.mode` is `password` rather than `token`, use the password instead — the
endpoint accepts either as `Authorization: Bearer <value>`.

**This token is the sensitive one.** It is not a webhook secret scoped to one route; it is
operator access to the whole Gateway. It belongs on the bridge and nowhere else.

---

## Step 4 — Choose the session namespace

Every turn runs in an OpenClaw session. You do not hand the caller a session key; you give
the bridge a **prefix**, and the caller can only add a name onto the end of it.

Set that prefix with `OPENCLAW_SESSION_PREFIX`. The default, `agent:main:chatgpt`, is the
right one — it keeps ChatGPT's work in its own corner, away from your day-to-day chat. Point
it at `agent:main:main` and everything ChatGPT does spills into your normal conversation, so
don't.

Here is how a request turns into a session:

- caller sends `research` → the work lands in `agent:main:chatgpt:research`
- caller sends nothing → it lands in `agent:main:chatgpt`

The reason this is a prefix rather than a fixed key is safety. The caller supplies a name,
never a whole key, and the bridge throws out anything with a colon in it. So even someone
who steals the bridge key cannot craft a value that reaches `agent:main:main` or a reserved
`subagent:` / `cron:` / `acp:` session — the namespace simply isn't theirs to pick.

None of these sessions need to exist first. OpenClaw makes one the moment it is used.

## Step 5 — Make the Gateway reachable by the bridge

The bridge runs elsewhere, so it needs a path in. Check what the Gateway binds:

```bash
lsof -nP -iTCP:18789 -sTCP:LISTEN
```

`127.0.0.1:18789` means loopback only — correct, and not reachable from anywhere else yet.

### Recommended: put Tailscale in front

`tailscale serve` publishes a local port across your tailnet over HTTPS, without changing
how the Gateway binds and without exposing anything publicly:

```bash
tailscale serve status
```

A working setup looks like:

```text
https://your-host.your-tailnet.ts.net (tailnet only)
|-- / proxy http://127.0.0.1:18789
```

Your `OPENCLAW_GATEWAY_URL` is then, with **no port** and **https**:

```text
https://your-host.your-tailnet.ts.net
```

The bridge appends `/v1/chat/completions` itself, so do not include a path.

Three consequences worth knowing:

- Plain HTTP on port 80 does **not** answer. Only HTTPS.
- The bridge's request travels through `HTTPS_PROXY` as a `CONNECT` tunnel when running
  Tailscale in userspace mode, not `HTTP_PROXY`. Set both.
- The bridge container needs a CA trust store to verify the `*.ts.net` certificate. The
  repository `Dockerfile` installs `ca-certificates` for this reason.

---

## Step 6 — Restart the Gateway

Config changes to endpoints need a restart. Environment variables are not reloaded either,
so if you changed anything in `~/.openclaw/.env` this is when it takes effect.

---

## Step 7 — Test it

Work down this ladder. Each rung isolates one failure.

```bash
TOKEN=$(python3 -c "import json;print(json.load(open('$HOME/.openclaw/openclaw.json'))['gateway']['auth']['token'])")
BASE=https://your-host.your-tailnet.ts.net
```

### 7a. Is the Gateway alive?

```bash
curl -s "$BASE/health"
```

Expect `{"ok":true,"status":"live"}`.

### 7b. Did the endpoint actually enable? Check the **content type**

This is the test people get wrong, and it is worth doing carefully.

```bash
curl -s -o /dev/null -w "%{http_code} %{content_type}\n" \
  -H "Authorization: Bearer $TOKEN" "$BASE/v1/models"
```

| Result | Meaning |
|---|---|
| `200 application/json` | **Correct.** The API is serving |
| `200 text/html` | **Not enabled.** That is the Control UI answering with its single-page app, which returns `200` for almost any path |
| `404` | Not enabled, and no SPA fallback on this path |

A bare status code tells you nothing here. The Control UI happily returns `200 text/html`
for paths that do not exist, so always check the content type.

Confirm with a path that should never exist:

```bash
curl -s -o /dev/null -w "%{http_code} %{content_type}\n" \
  -X POST -H "Authorization: Bearer $TOKEN" "$BASE/v1/definitely-not-real"
```

That should be `404 text/plain`.

### 7c. List the agent targets

```bash
curl -s -H "Authorization: Bearer $TOKEN" "$BASE/v1/models"
```

You should see ids like `openclaw`, `openclaw/default`, and `openclaw/<agentId>`. One of
these becomes `OPENCLAW_AGENT` on the bridge; `openclaw/default` is a safe choice.

### 7d. Run a real turn

```bash
curl -s "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'x-openclaw-session-key: agent:main:chatgpt' \
  -d '{"model":"openclaw/default","messages":[{"role":"user","content":"Reply with exactly one word: pong"}]}'
```

Expect a standard OpenAI response with `choices[0].message.content` set to `pong`.

**Expect this to take several seconds even for a trivial question** — a real agent turn is
starting, with the agent's full system context. Around 7 seconds locally is normal.

### 7e. Prove tools work

The point of this endpoint is that the agent *acts*. Ask for something only a tool can
answer:

```bash
curl -s "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"model":"openclaw/default","messages":[{"role":"user","content":"Run the shell command `uname -a` and paste the exact output. Do not guess."}]}'
```

If the output matches your real machine, tools are working. If it looks plausible but
generic, the agent guessed — check that its tool policy allows shell access.

---

## Step 8 — Turn off the webhooks plugin, if you enabled it

Earlier versions of this bridge used OpenClaw's `webhooks` plugin. **It cannot execute
anything** — `run_task` there only records a TaskFlow row for external automation that does
its own work — so the bridge no longer uses it.

If you enabled it for an older version, disable it now rather than leaving an authenticated
surface nobody tests:

```json
"plugins": {
  "entries": {
    "webhooks": { "enabled": false }
  }
}
```

Remove `"webhooks"` from `plugins.allow` too, and drop `OPENCLAW_WEBHOOK_SECRET` from
`~/.openclaw/.env`. Restart afterwards.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `/v1/chat/completions` returns `404` | Endpoint not enabled, or Gateway not restarted | Re-check Step 2, then restart |
| `/v1/models` returns `200 text/html` | You are hitting the Control UI, not the API | Same as above. Judge by content type, never by status |
| `401` from the Gateway | Token mismatch | Compare against `gateway.auth.token`. If `auth.mode` is `password`, use the password |
| Turn returns a plausible but wrong answer | The agent guessed instead of using tools | Check the agent's tool policy; ask again with "do not guess" |
| Bridge reports `502` | Gateway unreachable from the bridge | Confirm `tailscale serve status`, and that this machine is awake and on the tailnet |
| Bridge reports `504` | The turn outlived the bridge's timeout | Normal for real work. Use the bridge's `ask_async` action |
| Everything works locally, fails from the bridge | `OPENCLAW_GATEWAY_URL` points at `localhost` | Go never proxies loopback addresses. Use the tailnet hostname |

Read the Gateway log after a restart. Note that route registration and similar messages are
logged at **info**; if `logging.level` is `warn` you will not see them, so prefer the
content-type test above over log-reading.

---

## Security notes

- The Gateway token is **operator access**, not a scoped webhook secret. It belongs on the
  bridge only, never in a client.
- Keep `bind` on `loopback` and reach the Gateway over a tailnet.
- Bind the bridge to a **dedicated session** so a leaked bridge key cannot touch your main
  agent's work.
- The bridge must set its own `BRIDGE_API_KEY`. Without it, anyone who finds the bridge URL
  inherits everything above.

---

## Checklist

- [ ] Config backed up
- [ ] `gateway.http.endpoints.chatCompletions.enabled` set to `true`
- [ ] Gateway restarted
- [ ] `/v1/models` returns `200` **with `application/json`**
- [ ] A real turn returns `choices[0].message.content`
- [ ] A tool-requiring question returns real machine output
- [ ] Session key chosen deliberately
- [ ] Gateway reachable from where the bridge will run
- [ ] Gateway URL and token copied for the bridge's `.env`
- [ ] Old `webhooks` plugin disabled, if it was ever enabled

## What next

With the endpoint working, set up the bridge:

- [Step-by-step setup]({{ '/step-by-step.html' | relative_url }}) for the simplest path
- [Free deployment options]({{ '/deployment/free-tier.html' | relative_url }}) to host it for nothing
- [Custom GPT setup]({{ '/custom-gpt.html' | relative_url }}) to wire it into ChatGPT
