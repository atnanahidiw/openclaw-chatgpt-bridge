---
title: Step-by-step setup
---

# Step-by-Step Setup

This page gives the simplest way to get the bridge running.

## The three parts of a working setup

The bridge is the middle piece. It does nothing on its own, so a complete setup is three
guides, in this order:

| | Guide | What it gives you |
|---|---|---|
| 1 | [OpenClaw setup and testing]({{ '/openclaw-setup.html' | relative_url }}) | The Gateway endpoint the bridge calls, plus the two values it needs: `OPENCLAW_GATEWAY_URL` and `OPENCLAW_GATEWAY_TOKEN` |
| 2 | **This page** | The bridge itself, running and reachable |
| 3 | [Custom GPT setup]({{ '/custom-gpt.html' | relative_url }}) | The action imported into ChatGPT, with instructions the model can actually follow |

> **Start with part 1.** Nothing below works until OpenClaw has a webhook route, and you
> cannot fill in your `.env` without the values it produces.

For a more detailed VPS setup, or a free hosted one, use a
[cloud deployment guide]({{ '/deployment/free-tier.html' | relative_url }}) instead of steps 7 to 9.

## What this setup does

The bridge sits between ChatGPT and OpenClaw:

```text
ChatGPT -> bridge -> OpenClaw
```

If OpenClaw is private, the bridge can reach it through Tailscale.

---

## 1. Get the code

Open a terminal and go to the project folder.

```bash
cd /path/to/openclaw-chatgpt-bridge
```

If you cloned the repo, you are ready.
If not, clone it first, then come back to this folder.

---

## 2. Configure `.env` {#env-setup}

**`.env` is the single source of truth for this project.** Every deployment guide reads
from it, so you fill these in once and the same values end up on your laptop, in your
container, and in your cloud provider's secret store.

Copy the example file:

```bash
cp .env.example .env
```

Then open it and set the values below. `.env` is gitignored — never commit it.

### The values

| Setting | Required | What to put there |
|---|---|---|
| `ADDR` | no | Address the bridge binds. Defaults to `:8080`. **`PORT` is read but not used for binding** |
| `REQUEST_TIMEOUT_MS` | no | How long to wait for OpenClaw. Default `30000` |
| `MAX_BODY_BYTES` | no | Maximum request size. Default `1048576` |
| `OPENCLAW_GATEWAY_URL` | **yes** | Base URL of the OpenClaw Gateway. The bridge appends `/v1/chat/completions` |
| `OPENCLAW_GATEWAY_TOKEN` | **yes** | Gateway token, from `gateway.auth.token`. **Full operator access** — it never leaves the bridge |
| `OPENCLAW_AGENT` | no | Which agent to target. Defaults to `openclaw/default` |
| `OPENCLAW_SESSION_KEY` | no | Recorded in bridge logs only. OpenClaw's webhook route decides the real session |
| `BRIDGE_API_KEY` | strongly | Shared secret callers must present **to** the bridge. See below |
| `TS_AUTHKEY` | cloud only | Tailscale key for the cloud sidecar. Not read by the bridge itself |

### The two secrets are different things

This trips people up, because both are "the secret":

```text
Custom GPT --[ BRIDGE_API_KEY ]--> bridge --[ OPENCLAW_GATEWAY_TOKEN ]--> OpenClaw
```

- `BRIDGE_API_KEY` protects **the bridge** from strangers who find its URL.
- `OPENCLAW_GATEWAY_TOKEN` is how the bridge authenticates **to OpenClaw**, and it is
  full operator access. It must never be handed to a caller.

They should be different values. Reusing one for both means a leak of either compromises
both hops.

### Setting `BRIDGE_API_KEY`

Generate one rather than inventing it — a memorable string is guessable in a way its
length does not suggest:

```bash
openssl rand -hex 32
```

Without it the bridge still starts, logs a warning, and **accepts unauthenticated
requests**: anyone who knows the URL can drive your OpenClaw TaskFlows. That is a choice
you can make deliberately, not one to drift into.

The same value goes in the Custom GPT Action as an `api_key` header — see
[Custom GPT setup]({{ '/custom-gpt.html' | relative_url }}).

### If OpenClaw is private

Point `OPENCLAW_GATEWAY_URL` at the **Tailscale hostname** or IP, never `localhost` — Go
never sends loopback addresses through a proxy, so the bridge would silently skip Tailscale:

```text
http://openclaw-gateway.tailnet:18789
```

Behind `tailscale serve` it is **https with no port**. Check with `tailscale serve status`:

```text
https://your-host.your-tailnet.ts.net
```

### Loading it {#loading-env}

Every deployment guide starts by sourcing `.env`, so its values are available to the
commands that follow:

```bash
set -a; . ./.env; set +a
```

Then check nothing important is empty. This fails loudly now rather than producing a
container that starts and misbehaves later:

```bash
: "${OPENCLAW_GATEWAY_URL:?set it in .env}"
: "${OPENCLAW_GATEWAY_TOKEN:?set it in .env}"
: "${BRIDGE_API_KEY:?set it in .env}"
echo "core values present"
```

Deploying to a cloud platform? Add `: "${TS_AUTHKEY:?set it in .env}"` — an unset
Tailscale key produces a container that starts but never joins your tailnet, which only
surfaces later as `/readyz` failing.

### Confirming the gateway token matches

A mismatch here is the most common cause of a healthy-looking deployment that returns
`401`. Compare hashes rather than reading the values:

```bash
printf '%s' "$OPENCLAW_GATEWAY_TOKEN" | shasum -a 256 | cut -c1-16
python3 -c "import json;print(json.load(open('$HOME/.openclaw/openclaw.json'))['gateway']['auth']['token'])" \
  | tr -d '\n' | shasum -a 256 | cut -c1-16
```

The two hashes must match. Use `cut -d= -f2-`, not `-f2`, anywhere you read a secret out of
a `.env` file, or a value containing `=` is silently truncated.

---

## 3. Check the code can run

If you have Go 1.22 or newer installed, run:

```bash
go test ./...
```

This checks that the project still builds and the tests pass.

If Go is not installed, skip this step and use Docker instead.

---

## 4. Start the bridge locally

Run the server:

```bash
go run .
```

The bridge listens on `ADDR`.

### Common values for `ADDR`

| Value | When to use it |
|---|---|
| `:8080` | when the bridge should listen on all network interfaces |
| `127.0.0.1:8080` | when another proxy, like Caddy, will sit in front of it |

If you are only testing on your own laptop, `:8080` is usually the easiest.

---

## 5. Check that the server is alive

Open these URLs in a browser or use `curl`:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

### What they mean

- `healthz` = the process is up
- `readyz` = the bridge is ready to accept traffic

If both return a small JSON response, the server is working.

---

## 6. Test a bridge request

Send a sample request to the bridge:

```bash
curl -X POST http://localhost:8080/v1/openclaw \
  -H 'content-type: application/json' \
  -H "api_key: $BRIDGE_API_KEY" \
  --data '{"action":"ask","message":"Confirm you are reachable."}'
```

If you want to use a file instead, replace the inline JSON with your own payload file.

This step checks the full flow:

1. request reaches the bridge
2. bridge forwards it to OpenClaw
3. response comes back

---

## 7. Run with Docker

If you do not want to install Go locally, use Docker.

### Build the image

```bash
docker build -t openclaw-chatgpt-bridge:local .
```

### Run the container

```bash
docker run --rm -p 8080:8080 --env-file .env openclaw-chatgpt-bridge:local
```

If you want the bridge to be reachable from another machine or a reverse proxy, keep `ADDR=:8080` in your `.env` file.

---

## 8. Optional: put a proxy in front

If you want HTTPS, put **Caddy**, **Nginx**, or another reverse proxy in front of the bridge.

For example:

- proxy listens on `:443`
- bridge listens on `127.0.0.1:8080`
- proxy forwards traffic to the bridge

This is the normal setup for a public Custom GPT Action.

---

## 9. Optional: deploy with Kubernetes

If you are using Kubernetes, apply the manifests in `k8s/`.

If OpenClaw is private, use the Tailscale-backed manifest.

---

## 10. Add the GPT Action

> For the full version — schema import, the `servers:` URL, endpoint exposure, and the
> instructions the model needs for the async loop — see
> [Custom GPT setup]({{ '/custom-gpt.html' | relative_url }}).

1. Open the Custom GPT builder in ChatGPT.
2. Go to **Actions**.
3. Add a new action from OpenAPI.
4. Import `openapi/openclaw-bridge.openapi.yaml`.
5. Point it at the bridge URL that ChatGPT can reach.
6. Save the GPT.

### What must be public?

Only the **bridge URL** must be reachable by ChatGPT.
OpenClaw itself can stay private behind Tailscale.

---

## 11. Test the GPT Action

After you save the GPT, do one small test.

### What to type in ChatGPT

Use a simple prompt like this:

```text
Create a flow to test the bridge
```

### What should happen

1. ChatGPT sends the request to the bridge.
2. The bridge forwards it to OpenClaw.
3. OpenClaw does the work.
4. ChatGPT shows the result.

### What to check if it fails

If the test does not work, check these things first:

- the bridge URL is correct
- HTTPS works
- the OpenAPI file was imported correctly
- `OPENCLAW_GATEWAY_TOKEN` matches `gateway.auth.token` in OpenClaw
- the bridge can reach OpenClaw through Tailscale

---

## 12. How to use it day to day

Once it is set up, the flow is simple:

1. Open your Custom GPT.
2. Ask it to do a task.
3. ChatGPT sends the task to the bridge.
4. The bridge sends it to OpenClaw.
5. OpenClaw does the work.
6. ChatGPT shows the result back to you.

### Good example prompts

- `Create a flow to review this repo and identify release blockers`
- `Run a task to update the deployment docs based on the current manifests`
- `Run a task to summarize repeated support issues and draft FAQ updates`
- `Finish the flow`

### Simple rule to remember

- Use ChatGPT for the request and review.
- Use OpenClaw for the actual execution.

---

## Quick checklist

- [ ] Code is in the right folder
- [ ] `.env` is created
- [ ] `OPENCLAW_GATEWAY_URL` is set correctly
- [ ] `go test ./...` passes, or Docker works
- [ ] bridge starts with `go run .` or Docker
- [ ] `healthz` and `readyz` respond
- [ ] `/v1/openclaw` works
- [ ] GPT Action is imported
- [ ] Test prompt works in ChatGPT
