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
