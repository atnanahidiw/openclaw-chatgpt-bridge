---
title: OpenClaw setup and testing
---

# OpenClaw setup and testing

This page covers the **OpenClaw side**. Every other guide in these docs deploys the
bridge; none of them work until OpenClaw is configured to accept the bridge's requests.

Do this page **first**. You cannot finish the bridge setup without the two values it
produces: the webhook URL and the shared secret.

## One-sentence explanation

**OpenClaw's bundled `webhooks` plugin adds an authenticated HTTP route that turns
incoming JSON into TaskFlows, and the bridge is what posts to it.**

## What you are building

```text
ChatGPT -> bridge -> POST /plugins/webhooks/gpt -> OpenClaw TaskFlow
                     └── the route you create on this page
```

## Before you start

You need:

- OpenClaw installed and its Gateway running
- access to edit `~/.openclaw/openclaw.json`
- the ability to restart the Gateway

The plugin runs **inside the Gateway process**. If your Gateway runs on another machine,
do all of this on that machine.

---

## Step 1 — Find your config and confirm the Gateway is alive

The config lives at:

```bash
~/.openclaw/openclaw.json
```

Check the Gateway answers. The default port is `18789`:

```bash
curl http://127.0.0.1:18789/health
```

You should see:

```json
{"ok":true,"status":"live"}
```

If that fails, start the Gateway before going further.

### Back up the config first

You are about to hand-edit JSON that controls a running service:

```bash
cp ~/.openclaw/openclaw.json ~/.openclaw/openclaw.json.bak.$(date +%Y%m%d-%H%M%S)
```

---

## Step 2 — Enable the webhooks plugin

Open `~/.openclaw/openclaw.json` and find the `plugins` section.

Two edits are needed. **Both**, not one.

### 2a. Allowlist the plugin

Add `"webhooks"` to `plugins.allow`:

```json
"plugins": {
  "enabled": true,
  "allow": [
    "browser",
    "workboard",
    "webhooks"
  ]
}
```

### 2b. Add the route

Add a `webhooks` entry under `plugins.entries`:

```json
"webhooks": {
  "enabled": true,
  "config": {
    "routes": {
      "gpt": {
        "path": "/plugins/webhooks/gpt",
        "sessionKey": "agent:main:chatgpt",
        "secret": {
          "source": "env",
          "provider": "default",
          "id": "OPENCLAW_WEBHOOK_SECRET"
        },
        "controllerId": "webhooks/gpt",
        "description": "ChatGPT bridge TaskFlow ingress"
      }
    }
  }
}
```

### What each field means

| Field | Required | Meaning |
|---|---|---|
| `path` | no | The URL to POST to. Defaults to `/plugins/webhooks/<routeId>`, so `gpt` gives you `/plugins/webhooks/gpt` — which is the bridge's default |
| `sessionKey` | **yes** | Which OpenClaw session owns the TaskFlows this route creates. See Step 3 |
| `secret` | **yes** | The shared secret callers must present. See Step 4 |
| `controllerId` | no | Label on the created flows. Defaults to `webhooks/<routeId>` |
| `description` | no | Operator note, shown nowhere important |

The route id (`gpt` above) is the key in the `routes` object. Name it whatever you like,
but if you change it, either set `path` explicitly or update the bridge's
`OPENCLAW_WEBHOOK_URL` to match.

---

## Step 3 — Choose the session key

This is the decision most worth thinking about, because **the caller cannot override it**.
The route is permanently bound to whatever session you configure here. The bridge sends
no session information at all — a `sessionKey` in the request body is rejected outright.

| Choice | Effect |
|---|---|
| `agent:main:main` | ChatGPT-created flows land in your main agent session, so they appear in your normal conversation on WhatsApp, Telegram, and so on |
| `agent:main:chatgpt` | ChatGPT-created flows live in their own session, separate from your day-to-day conversation |

**Prefer a dedicated session.** The plugin's own security guidance is to bind routes to the
narrowest session that fits, because the route can inspect and mutate every TaskFlow owned
by that session. A separate session limits what a leaked secret can reach.

The session does not need to exist beforehand. OpenClaw binds it on demand.

Note that flows are owned per session, so **changing this later hides earlier flows** from
the route. A `get_flow` for a flow created under the old session returns `flow: null`.

---

## Step 4 — Set the shared secret

The plugin accepts either a plain string or a SecretRef. Prefer the reference: a secret
written directly into `openclaw.json` is easy to leak when sharing config or backups.

Generate a strong secret and store it in OpenClaw's environment file:

```bash
printf 'OPENCLAW_WEBHOOK_SECRET=%s\n' "$(openssl rand -hex 32)" >> ~/.openclaw/.env
```

The route config above already points at it:

```json
"secret": { "source": "env", "provider": "default", "id": "OPENCLAW_WEBHOOK_SECRET" }
```

Read the value back — you need the identical string on the bridge side:

```bash
grep '^OPENCLAW_WEBHOOK_SECRET=' ~/.openclaw/.env
```

### The failure mode to know about

If a secret-backed route **cannot resolve its secret at startup, the plugin skips that
route entirely** and logs a warning. It does not expose a broken endpoint.

That means a missing environment variable does not look like an auth error. It looks like
the route does not exist — a `404`. Step 7 shows how to tell those apart.

---

## Step 5 — Make OpenClaw reachable by the bridge

The bridge runs somewhere else, so it has to be able to reach the Gateway.

Check what the Gateway currently binds:

```bash
lsof -nP -iTCP:18789 -sTCP:LISTEN
```

If you see `127.0.0.1:18789`, the Gateway is loopback-only and **nothing outside that
machine can reach it**, including a bridge on your tailnet.

### Recommended: put Tailscale in front

`tailscale serve` publishes a local port across your tailnet over HTTPS, without changing
how OpenClaw binds and without exposing anything to the public internet.

Check whether it is already configured:

```bash
tailscale serve status
```

A working setup looks like this:

```text
https://your-host.your-tailnet.ts.net (tailnet only)
|-- / proxy http://127.0.0.1:18789
```

Your webhook URL is then, with **no port number** and **https**:

```text
https://your-host.your-tailnet.ts.net/plugins/webhooks/gpt
```

Three consequences worth knowing:

- Plain HTTP on port 80 does **not** answer. Only HTTPS.
- The bridge's request travels through `HTTPS_PROXY` as a `CONNECT` tunnel, not
  `HTTP_PROXY`. Set both when running Tailscale in userspace mode.
- The bridge container needs a CA trust store to verify the `*.ts.net` certificate. The
  repository `Dockerfile` installs `ca-certificates` for this reason.

### Alternative: bind the tailnet interface directly

You can instead have the Gateway listen beyond loopback via `gateway.bind` in
`openclaw.json`. Then the URL keeps the port and uses plain HTTP:

```text
http://your-host.your-tailnet.ts.net:18789/plugins/webhooks/gpt
```

Only do this on a private interface. Never bind the Gateway to a public address.

---

## Step 6 — Restart the Gateway

Config changes to routes are picked up, but **environment variables are not**. A secret you
just added to `~/.openclaw/.env` is invisible to the already-running process, so the route
will fail authentication until you restart.

Restart the Gateway now, however you normally do.

You can confirm the process actually has the variable:

```bash
ps eww $(pgrep -f openclaw | head -1) | tr ' ' '\n' | grep -c '^OPENCLAW_WEBHOOK_SECRET='
```

`1` means it is loaded. `0` means the restart did not pick up the file.

---

## Step 7 — Test it

Work down this ladder. Each rung isolates one failure, so when something breaks you know
exactly which layer to fix.

Set up two shell variables first:

```bash
SECRET=$(grep '^OPENCLAW_WEBHOOK_SECRET=' ~/.openclaw/.env | cut -d= -f2)
BASE=https://your-host.your-tailnet.ts.net
```

### 7a. Is the Gateway alive?

```bash
curl -s "$BASE/health"
```

Expect `{"ok":true,"status":"live"}`. If this fails, nothing below will work.

### 7b. Did your route register?

This is the test people skip, and it is the one that matters most. Compare your path
against a path you know is fake:

```bash
curl -s -o /dev/null -w "yours: %{http_code}\n" -X POST "$BASE/plugins/webhooks/gpt" \
  -H 'content-type: application/json' -d '{}'
curl -s -o /dev/null -w "fake:  %{http_code}\n" -X POST "$BASE/plugins/webhooks/not-real" \
  -H 'content-type: application/json' -d '{}'
```

| Result | Meaning |
|---|---|
| yours `401`, fake `404` | **Correct.** Your route exists and is demanding authentication |
| yours `404`, fake `404` | The route did not register. Either you did not restart, or the secret failed to resolve and the plugin skipped it |

Without the fake-path comparison a `401` is ambiguous, because you cannot tell a real
route from a generic guard.

### 7c. Does the secret work?

```bash
curl -s -X POST "$BASE/plugins/webhooks/gpt" -H 'content-type: application/json' \
  -H "Authorization: Bearer $SECRET" -d '{"action":"get_flow","flowId":"probe"}'
```

Expect:

```json
{"ok":true,"routeId":"gpt","result":{"flow":null}}
```

`flow: null` is success — you asked for a flow that does not exist, and the route answered.
A `401` here means the running process has a different secret than your `.env` file.

The plugin accepts either header, and the bridge sends both:

```bash
-H "Authorization: Bearer $SECRET"
-H "x-openclaw-webhook-secret: $SECRET"
```

### 7d. Walk a real flow end to end

This exercises every action the bridge uses. Note how `expectedRevision` is read from one
call and passed to the next.

```bash
post() { curl -s -X POST "$BASE/plugins/webhooks/gpt" \
  -H 'content-type: application/json' -H "Authorization: Bearer $SECRET" -d "$1"; }

# 1. create — note the flowId and revision in the response
post '{"action":"create_flow","goal":"webhook smoke test","notifyPolicy":"done_only"}'

# 2. read it back
post '{"action":"get_flow","flowId":"PASTE_FLOW_ID"}'

# 3. move it, quoting the revision you just read
post '{"action":"resume_flow","flowId":"PASTE_FLOW_ID","expectedRevision":0,"status":"running"}'

# 4. close it, quoting the revision again (it changed in step 3)
post '{"action":"finish_flow","flowId":"PASTE_FLOW_ID","expectedRevision":1}'
```

Clean up after yourself: finish any flow you create, or it stays open in that session.

### 7e. Check `run_task` without actually running anything

`run_task` spawns real work, which you may not want during a smoke test. Send it with a
deliberately fake `flowId` — that validates the payload shape without executing:

```bash
post '{"action":"run_task","flowId":"no-such-flow","task":"probe","runtime":"subagent"}'
```

| Response `code` | Meaning |
|---|---|
| `not_found` | **Good.** The payload passed validation and only the flow lookup failed |
| `invalid_request` | Your payload shape is wrong — read the `error` text |

---

## The action contract

Every action schema is **strict**: a key the action does not declare fails the whole
request with `400 Unrecognized keys`. This catches people out, because sending a *helpful
extra field* breaks the call rather than being ignored.

| Action | Required | Also accepted |
|---|---|---|
| `create_flow` | `goal` | `status` (`queued`/`running`/`waiting`/`blocked`), `notifyPolicy`, `controllerId`, `currentStep`, `stateJson`, `waitJson` |
| `run_task` | `flowId`, `runtime` (`subagent`/`acp`), `task` | `childSessionKey`, `label`, `status` (`queued`/`running`), `notifyPolicy`, and several ids |
| `get_flow` | `flowId` | nothing at all |
| `resume_flow` | `flowId`, `expectedRevision` | `status` (`queued`/`running`), `currentStep`, `stateJson` |
| `finish_flow` | `flowId`, `expectedRevision` | `stateJson` |

Notes that are easy to get wrong:

- **`notifyPolicy` is `done_only`, `state_changes`, or `silent`.** There is no `all_events`.
- **`notifyPolicy` is only accepted by `create_flow` and `run_task`.** Sending it to the
  other three fails the request.
- **`sessionKey` and `metadata` are accepted by no action.** The bridge strips both, and
  re-sends `metadata` as `stateJson` where that is allowed.
- **`expectedRevision` is optimistic concurrency.** Read it from `get_flow` immediately
  before writing, and expect it to change after every mutation.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `404` on your route, `404` on a fake path too | Route never registered | Restart the Gateway. If still 404, the secret failed to resolve — check the log for a `[webhooks]` warning |
| `401` with the right secret | The running process has a different value | The Gateway was started before you wrote the `.env` entry. Restart it |
| `400 Unrecognized keys: "..."` | Extra fields in the body | Remove them. See the contract table above |
| `x509: certificate signed by unknown authority` | The caller has no CA trust store and your URL is `https` | Install `ca-certificates` in the calling container |
| Bridge reaches nothing, no error | `OPENCLAW_WEBHOOK_URL` points at `localhost` | Go never proxies loopback addresses. Use the tailnet hostname |
| `get_flow` returns `flow: null` for a flow you know exists | It belongs to a different session | The route's `sessionKey` changed, or another route created it |

Read the Gateway log after any restart. The plugin logs one line per route:

```text
[webhooks] registered route gpt on /plugins/webhooks/gpt for session agent:main:chatgpt
```

That line is logged at **info** level. If your config sets `logging.level` to `warn` — a
common default — you will not see it even when the route registered correctly. Either
lower the level temporarily, or use the `401`-versus-`404` check in Step 7b instead, which
does not depend on logging at all.

### Confirming which session actually owns a flow

Webhook responses deliberately scrub owner and session metadata, so you cannot read
ownership back through the API. If you need to prove a `sessionKey` change took effect,
query the state database directly:

```bash
sqlite3 "file:$HOME/.openclaw/state/openclaw.sqlite?mode=ro" \
  "SELECT owner_key, count(*) FROM flow_runs WHERE controller_id='webhooks/gpt' GROUP BY owner_key;"
```

Flows created before the change keep their old `owner_key`, which is why they stop being
visible to the route.

---

## Security notes

- Use a **unique secret per route**, and prefer a SecretRef over an inline string.
- Bind each route to the **narrowest session** that fits, because the route can inspect and
  mutate every TaskFlow owned by that session.
- Expose only the specific webhook path you need.
- Keep the Gateway off public interfaces. Reach it over a tailnet instead.
- The plugin already applies shared-secret auth, body size and timeout guards, fixed-window
  rate limiting, and in-flight request limits.

---

## Checklist

- [ ] Config backed up
- [ ] `webhooks` added to `plugins.allow`
- [ ] Route added under `plugins.entries.webhooks.config.routes`
- [ ] `sessionKey` chosen deliberately
- [ ] Secret generated and stored in `~/.openclaw/.env`
- [ ] OpenClaw reachable from where the bridge will run
- [ ] Gateway restarted **after** writing the secret
- [ ] Your path returns `401` while a fake path returns `404`
- [ ] Correct secret returns `{"ok":true,...}`
- [ ] A full create → get → resume → finish cycle succeeds
- [ ] Webhook URL and secret copied for the bridge's `.env`

## What next

With the webhook working, set up the bridge:

- [Step-by-step setup]({{ '/step-by-step.html' | relative_url }}) for the simplest path
- [Free deployment options]({{ '/deployment/free-tier.html' | relative_url }}) to host it for nothing
