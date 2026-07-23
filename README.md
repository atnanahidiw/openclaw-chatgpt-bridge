<h1 align="center">OpenClaw ChatGPT Bridge 🐾</h1>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white" />
  <img alt="Docker" src="https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white" />
  <img alt="Tailscale" src="https://img.shields.io/badge/Tailscale-supported-222222?logo=tailscale&logoColor=white" />
  <img alt="ChatGPT Actions" src="https://img.shields.io/badge/ChatGPT-Actions-10A37F?logo=openai&logoColor=white" />
</p>

#### Bridge service for connecting a Custom GPT to OpenClaw.

<br>

<img alt="SECURITY WARNING — READ BEFORE DEPLOYING" src="https://img.shields.io/badge/⚠_SECURITY_WARNING-READ_BEFORE_DEPLOYING-b91c1c?style=for-the-badge&labelColor=7f1d1d" />  

> [!CAUTION]
> **This bridge deliberately exposes a path from the public internet into your OpenClaw agent.**
> Understand these before you deploy it.
>
> **1. The bridge endpoint is public.** A Custom GPT Action can only call a publicly
> reachable HTTPS URL. The only thing standing between the internet and your OpenClaw is
> `BRIDGE_API_KEY`. Leave it unset and the endpoint accepts **anyone** who finds the URL.
> Set it to something long and random — not a memorable phrase.
>
> **2. Whoever holds the key can act as you.** OpenClaw runs a real agent turn: shell
> commands, file edits, its installed skills. Point the bridge at a dedicated session via
> `OPENCLAW_SESSION_KEY`, so a leaked key cannot reach your main agent's work.
>
> **3. The execution ingress is full operator access.** Enabling the Gateway's
> `/v1/chat/completions` endpoint — required if you want OpenClaw to actually *do* work and
> reply — grants callers the complete operator scope set with owner semantics. OpenClaw's
> own docs say to keep it on loopback or a private tailnet and **never** expose it to the
> public internet. The bridge is what keeps that boundary: it holds the gateway token
> privately and re-authenticates callers with its own key.
>
> **4. Two secrets, two different jobs.** `BRIDGE_API_KEY` protects the bridge from
> strangers. `OPENCLAW_GATEWAY_TOKEN` is how the bridge authenticates to OpenClaw, and it
> is full operator access. It must never be given to a caller.
>
> Run OpenClaw behind Tailscale, keep the gateway off public interfaces, and treat every
> secret here as a production credential.

## Purpose

Bridge GPT Action requests to OpenClaw without exposing OpenClaw directly.

The bridge lets users describe work in ChatGPT, then hand that work to OpenClaw for execution in a controlled environment.

## What can this help with?

See [Use cases](https://atnanahidiw.github.io/openclaw-chatgpt-bridge/usecases.html) for examples across software, project management, operations, customer support, and research or grant work.

## Architecture

```text
+----------------+        +--------+        +---------------------------+
| ChatGPT Action | -----> | bridge | -----> | OpenClaw Gateway          |
+----------------+        +--------+        | POST /v1/chat/completions |
   public HTTPS            api_key           +---------------------------+
                           checked here        runs a real agent turn
```

The bridge authenticates the caller, forwards the instruction to OpenClaw's Gateway, and
returns what the agent said. OpenClaw runs a genuine agent turn with its tools and skills,
so it can read files, run commands, and act — not just answer.

Three actions:

| Action | Use it for |
|---|---|
| `ask` | quick questions. Waits for the reply; expect ~30s even for something trivial |
| `ask_async` | real work. Returns a `jobId` immediately |
| `get_result` | collecting an async reply by `jobId` |

This pattern suits ChatGPT deciding *what* to delegate and reviewing the outcome, while
OpenClaw does the work on your own machine.

## Quick Start

1. Deploy the bridge and expose `POST /v1/openclaw` through the network path your GPT Action can reach.
1. For a normal Custom GPT Action, this usually means a public HTTPS URL.
1. For a private setup, keep OpenClaw private and let the bridge reach it through Tailscale.
1. Open the Custom GPT builder in ChatGPT.
1. Go to **Actions** and add a new action from OpenAPI.
1. Import [`openapi/openclaw-bridge.openapi.yaml`](./openapi/openclaw-bridge.openapi.yaml).
1. Set Authentication to API Key, type Custom, header name `api_key`, value `BRIDGE_API_KEY`.
1. Save the GPT.
1. Test with prompts like:
   - `Ask OpenClaw to confirm it is reachable and name its working directory`
   - `Ask OpenClaw to review this repo and list release blockers`
   - `Ask OpenClaw to update the deployment docs based on the current manifests`

## API Examples

- [`openapi/openclaw-bridge.openapi.yaml`](./openapi/openclaw-bridge.openapi.yaml)
- [Use cases](https://atnanahidiw.github.io/openclaw-chatgpt-bridge/usecases.html)

## Deployment

- [Deployment overview](https://atnanahidiw.github.io/openclaw-chatgpt-bridge/deployment/) — compare the targets and pick one
  - [Free deployment options](https://atnanahidiw.github.io/openclaw-chatgpt-bridge/deployment/free-tier.html) — no server or domain needed
  - [Step-by-step setup](https://atnanahidiw.github.io/openclaw-chatgpt-bridge/step-by-step.html)
- [`CONTRIBUTING.md`](./CONTRIBUTING.md)

## GPT Action Setup

- Import [`openapi/openclaw-bridge.openapi.yaml`](./openapi/openclaw-bridge.openapi.yaml)
- Point the action at the bridge URL that ChatGPT can reach
- Use [Use cases](https://atnanahidiw.github.io/openclaw-chatgpt-bridge/usecases.html) to choose a realistic first workflow

## Operations Endpoints

- `GET /healthz`
- `GET /readyz`
- `GET /version`

## Security Notes

- `OPENCLAW_GATEWAY_TOKEN` is full operator access to OpenClaw. Keep it on the bridge only.
- Set `BRIDGE_API_KEY`, or the endpoint accepts anyone who finds the URL.
- Use `ADDR=:8080` when the bridge must be reachable through the pod network or Tailscale IP.
- Use `ADDR=127.0.0.1:8080` only when another proxy or sidecar explicitly forwards traffic to localhost.

## Troubleshooting

- [Troubleshooting guide](https://atnanahidiw.github.io/openclaw-chatgpt-bridge/troubleshooting.html)

## Version Endpoint

The bridge exposes build metadata at:

```http
GET /version
```

Example response:

```json
{
  "name": "openclaw-chatgpt-bridge",
  "version": "1.0.0",
  "commit": "abc1234",
  "buildTime": "2026-06-23T10:30:00Z"
}
```
