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

## `OPENCLAW_WEBHOOK_URL is not set`

This means the bridge does not know where to send requests.

### Fix

Set this in `.env` or in your deployment config:

```bash
OPENCLAW_WEBHOOK_URL=http://your-openclaw-address/plugins/webhooks/gpt
```

If OpenClaw is private, use the Tailscale hostname or IP.

---

## `create_flow` says `goal is required`

This usually means the request used the wrong field.

### Correct example

```json
{
  "action": "create_flow",
  "goal": "Build a UMKM finance app MVP"
}
```

For `create_flow`, use `goal`, not `task`.

---

## Validation errors

Common causes:

- missing `action`
- `create_flow` without `goal`
- `run_task` without `flowId`
- `run_task` without `task`

### What to do

Check the JSON you send to `/v1/openclaw` and make sure the required fields are present.

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

- wrong `OPENCLAW_WEBHOOK_SECRET`
- wrong auth header expected by OpenClaw
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

## `x509: certificate signed by unknown authority`

This means `OPENCLAW_WEBHOOK_URL` is an `https` address and the bridge container has no CA trust store.

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
OPENCLAW_WEBHOOK_URL=https://your-host.your-tailnet.ts.net/plugins/webhooks/gpt
```

Not `http://your-host:18789/...`. Plain HTTP on port 80 does not answer, and the raw port is not exposed to the tailnet.

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
- check whether `OPENCLAW_WEBHOOK_URL` uses the right tailnet hostname or IP

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

Likely causes:

- wrong `OPENCLAW_WEBHOOK_SECRET`
- OpenClaw expects a different auth header

The bridge sends both:

- `Authorization: Bearer ***`
- `x-openclaw-webhook-secret: ...`

---

## Cloud portability

The bridge only depends on:

- Docker or Go
- HTTP
- a public HTTPS URL for ChatGPT
- private network access to OpenClaw if you use Tailscale

That is what makes it portable.
