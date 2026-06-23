<h1 align="center">OpenClaw ChatGPT Bridge 🐾</h1>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white" />
  <img alt="Docker" src="https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white" />
  <img alt="Tailscale" src="https://img.shields.io/badge/Tailscale-supported-222222?logo=tailscale&logoColor=white" />
  <img alt="ChatGPT Actions" src="https://img.shields.io/badge/ChatGPT-Actions-10A37F?logo=openai&logoColor=white" />
</p>

Bridge service for connecting a Custom GPT to OpenClaw.

## Purpose

Bridge GPT Action requests to OpenClaw without exposing OpenClaw directly.

The bridge lets users describe work in ChatGPT, then hand that work to OpenClaw for execution in a controlled environment.

## What can this help with?

See [Use cases](./docs/usecases.md) for examples across software, project management, operations, customer support, and research or grant work.

## Architecture

```text
ChatGPT Action -> OpenClaw bridge -> OpenClaw webhook
```

The bridge validates GPT Action payloads, forwards them to OpenClaw, preserves `X-Request-ID` for tracing, and returns the upstream response shape.

This pattern is useful when ChatGPT should coordinate and review the work, while OpenClaw performs the task.

## Quick Start

1. Deploy the bridge and expose `POST /v1/openclaw` through the network path your GPT Action can reach.
1. For a normal Custom GPT Action, this usually means a public HTTPS URL.
1. For a private setup, keep OpenClaw private and let the bridge reach it through Tailscale.
1. Open the Custom GPT builder in ChatGPT.
1. Go to **Actions** and add a new action from OpenAPI.
1. Import [`openapi/openclaw-bridge.openapi.yaml`](./openapi/openclaw-bridge.openapi.yaml).
1. Set auth to match your bridge deployment if needed.
1. Save the GPT.
1. Try one of the examples in [Use cases](./docs/usecases.md), or test with prompts like:
   - `Create a flow to review this repo and identify release blockers`
   - `Run a task to update the deployment docs based on the current manifests`
   - `Run a task to summarize repeated support issues and draft FAQ updates`
   - `Finish the flow`

## API Examples

- [`openapi/openclaw-bridge.openapi.yaml`](./openapi/openclaw-bridge.openapi.yaml)
- [Use cases](./docs/usecases.md)

## Deployment

- [`docs/step-by-step.md`](./docs/step-by-step.md)
- [`docs/deployment/`](./docs/deployment)
- [`CONTRIBUTING.md`](./CONTRIBUTING.md)

## GPT Action Setup

- Import [`openapi/openclaw-bridge.openapi.yaml`](./openapi/openclaw-bridge.openapi.yaml)
- Point the action at the bridge URL that ChatGPT can reach
- Use [Use cases](./docs/usecases.md) to choose a realistic first workflow

## Operations Endpoints

- `GET /healthz`
- `GET /readyz`
- `GET /version`

## Security Notes

- Keep `OPENCLAW_WEBHOOK_SECRET` set for the upstream hop.
- Use `ADDR=:8080` when the bridge must be reachable through the pod network or Tailscale IP.
- Use `ADDR=127.0.0.1:8080` only when another proxy or sidecar explicitly forwards traffic to localhost.

## Troubleshooting

- [`docs/troubleshooting.md`](./docs/troubleshooting.md)

## Version Endpoint

The bridge exposes build metadata at:

```http
GET /version
```

Example response:

```json
{
  "name": "openclaw-chatgpt-bridge",
  "version": "0.1.0",
  "commit": "abc1234",
  "buildTime": "2026-06-23T10:30:00Z"
}
```
