---
title: Troubleshooting
---

# Troubleshooting

This page explains the most common problems in simple language.

## `go test` fails

### What to check first

Run:

```bash
go version
```

You need **Go 1.22 or newer**.

If Go is missing, install it before trying again.

---

## `OPENCLAW_GATEWAY_URL is not set`

The bridge does not know which OpenClaw Gateway to call.

### Fix

Set it in `.env` or your deployment config. It is the Gateway base URL, without a path —
the bridge appends `/v1/chat/completions` itself:

```bash
OPENCLAW_GATEWAY_URL=https://your-host.your-tailnet.ts.net
```

Behind `tailscale serve` this is **https with no port**. Check with `tailscale serve status`.

---

## `invalid request` with a details list

The body was missing a required field. The `details` array names every problem at once:

```json
{
  "ok": false,
  "error": "invalid request",
  "details": ["message is required for ask"]
}
```

Required fields:

- `ask` and `ask_async` need `message`
- `get_result` needs `jobId`

`sessionKey` must not use the reserved namespaces `subagent:`, `cron:`, or `acp:`.

---

## The turn times out

A `504` means the turn exceeded `REQUEST_TIMEOUT_MS`.

Even a trivial question takes roughly 30 seconds, because a real agent turn is starting.
Anything that reads files, searches, or runs commands takes far longer.

### Fix

Use `ask_async` and poll `get_result`. Raising `REQUEST_TIMEOUT_MS` helps a little, but a
Custom GPT Action gives up before a long turn finishes regardless, so async is the real
answer.

---

## `404` on `get_result`

The `jobId` is unknown. Either it expired past `JOB_TTL_MS`, or the bridge restarted.

**Async jobs live in memory.** They do not survive a restart, and a second replica cannot
see the first replica's jobs. Run a single replica — on Azure Container Apps that means
`--max-replicas 1`. With more than one, a poll can land on the wrong replica and 404 a job
that is running perfectly well.

---

## OpenClaw returns a non-2xx status

This means the bridge reached OpenClaw, but OpenClaw said “no”.

### Example response

```json
{
  "ok": false,
  "error": "OpenClaw returned non-2xx status",
  "upstreamStatus": 401,
  "upstreamBody": {
    "message": "unauthorized"
  }
}
```

### Common reasons

- wrong `OPENCLAW_GATEWAY_TOKEN`
- the Gateway's chat completions endpoint is disabled
- OpenClaw returned an error for another reason

---

## Deployment failures

Getting the bridge *deployed* has its own characteristic failures — an `arm64` image the
platform refuses to start, a private registry it cannot pull from, a `401` caused by missing
one of the two restarts a secret change needs.

Those are documented where you hit them, with the commands to fix each one:

- **[Things that went wrong for us]({{ '/deployment/azure.html' | relative_url }}#things-that-went-wrong-for-us)** — in the Azure guide, but the causes apply to any container platform
- [OpenClaw setup and testing]({{ '/openclaw-setup.html' | relative_url }}) — for webhook route and secret problems on the OpenClaw side

The rest of this page covers problems with a bridge that is already running.

---

## Telling the two credentials apart

There are two secrets and they fail differently.

| Symptom | Who rejected it | Fix |
|---|---|---|
| `401` with `{"ok":false,"error":"unauthorized"}` | The **bridge** rejected your caller | Send `api_key: <BRIDGE_API_KEY>`, or `Authorization: Bearer <key>`. In a Custom GPT this is the Action's API Key credential |
| `500` with `"OpenClaw rejected the bridge's gateway token"` | **OpenClaw** rejected the bridge | `OPENCLAW_GATEWAY_TOKEN` does not match `gateway.auth.token` in `~/.openclaw/openclaw.json` |

The distinction matters: a `401` is the caller's problem, a `500` is the operator's. A GPT
should retry neither.

### The key is set but still rejected

Both are read once at startup. Setting either without restarting leaves the old value in
the running process. Redeploy or restart the revision.

```bash
curl -s -o /dev/null -w "%{http_code}\n" -X POST "$BRIDGE/v1/openclaw" \
  -H 'content-type: application/json' -d '{"action":"ask","message":"ping"}'
```

`401` means a key is configured and enforced. `200` means no key is set and the endpoint is
open to anyone who knows the URL.

---

## `x509: certificate signed by unknown authority`

This means `OPENCLAW_GATEWAY_URL` is an `https` address and the bridge container has no CA trust store.

It shows up most often when OpenClaw sits behind `tailscale serve`, because that fronts OpenClaw on HTTPS instead of its plain HTTP port.

### Fix

Make sure the image installs certificates. The repository `Dockerfile` does:

```dockerfile
RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 appuser
```

If you build your own image from a minimal base such as `scratch` or bare `alpine`, add the same package, or copy `/etc/ssl/certs/ca-certificates.crt` in from the build stage.

---

## `tailscale serve` addresses

If `tailscale serve` publishes OpenClaw, the address has **no port** and uses `https`:

```bash
tailscale serve status
```

```text
https://your-host.your-tailnet.ts.net (tailnet only)
|-- / proxy http://127.0.0.1:18789
```

So the value to use is:

```bash
OPENCLAW_GATEWAY_URL=https://your-host.your-tailnet.ts.net
```

Not `http://your-host:18789`. Plain HTTP on port 80 does not answer, and the raw port is not exposed to the tailnet.

Requests to an `https` upstream travel through `HTTPS_PROXY`, so set both `HTTP_PROXY` and `HTTPS_PROXY` when using userspace Tailscale.

---

## Bridge times out

This means the bridge waited too long for OpenClaw.

### Common reasons

- wrong tailnet address
- Tailscale is not connected
- OpenClaw is down
- OpenClaw is slow

### What to check

- confirm the tailnet hostname or IP is correct
- confirm the Droplet is connected to Tailscale
- confirm OpenClaw is reachable from the Droplet

---

## Tailscale connectivity problems

If the bridge cannot reach OpenClaw through Tailscale:

- check `TS_AUTHKEY`
- check whether the Droplet appears in the tailnet
- check whether `OPENCLAW_GATEWAY_URL` uses the right tailnet hostname or IP

---

## Address binding problems

- Use `ADDR=:8080` when the bridge must be reachable from the network.
- Use `ADDR=127.0.0.1:8080` when another proxy like Caddy sits in front.

---

## Helm / YAML problems

If Helm output looks wrong:

- check `chart/values.schema.json`
- run `helm lint ./chart`
- run `helm template ./chart`

---

## Request ID tracing

The bridge forwards `X-Request-ID` upstream.

If you cannot find a request in logs, set `X-Request-ID` on the inbound request and search for the same value in OpenClaw logs.

---

## 401 or 403 from OpenClaw

Likely cause: `OPENCLAW_GATEWAY_TOKEN` does not match `gateway.auth.token` in
`~/.openclaw/openclaw.json`.

The bridge sends `Authorization: Bearer <token>` to the Gateway. If OpenClaw rejects it the
bridge reports a `500`, because the caller did nothing wrong.

---

## Cloud portability

The bridge only depends on:

- Docker or Go
- HTTP
- a public HTTPS URL for ChatGPT
- private network access to OpenClaw if you use Tailscale

That is what makes it portable.
