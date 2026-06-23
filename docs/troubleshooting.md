# Troubleshooting

## `go test` fails

Check Node first:

```bash
go version
```

Need Go 1.22 or newer.

## Bridge returns `OPENCLAW_WEBHOOK_URL is not set`

Set the env var in `.env` or Kubernetes.

## Validation errors

Common causes:

- missing `action`
- `create_flow` without `goal`
- `run_task` without `flowId`
- `run_task` without `task`

## `create_flow` returns `goal is required`

Use `goal` for `create_flow`, not `task`.

```json
{
  "action": "create_flow",
  "goal": "Build a UMKM finance app MVP"
}
```

## OpenClaw non-2xx errors

The bridge returns the upstream status and body:

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

## Bridge returns timeout

Likely causes:

- wrong tailnet address
- Tailscale sidecar not ready
- OpenClaw is down

## Tailscale connectivity

If the bridge cannot reach OpenClaw through Tailscale:

- verify `TS_AUTHKEY`
- verify pod shows up in tailnet
- verify `OPENCLAW_WEBHOOK_URL` points to the tailnet name or IP

## ADDR binding issues

- Use `ADDR=:8080` when the bridge must be reachable through the pod network or Tailscale IP.
- Use `ADDR=127.0.0.1:8080` only when another proxy or sidecar explicitly forwards traffic to localhost.

## Helm/schema issues

- Validate `chart/values.schema.json` if Helm values stop rendering.
- Run `helm lint ./chart` and `helm template` for the default and provider-specific values files.

## Request ID tracing

- The bridge forwards `X-Request-ID` upstream.
- If a request looks missing in logs, set `X-Request-ID` on the inbound request and trace the same value in OpenClaw logs.

## 401 or 403 from OpenClaw

Likely causes:

- wrong `OPENCLAW_WEBHOOK_SECRET`
- OpenClaw expects a different auth header

The bridge sends both:

- `Authorization: Bearer ...`
- `x-openclaw-webhook-secret: ...`

## Cloud portability

This template only depends on:

- Docker
- HTTP
- Kubernetes Deployment
- Kubernetes Service
- Kubernetes Ingress

That is what keeps it portable across Azure and Alibaba.
