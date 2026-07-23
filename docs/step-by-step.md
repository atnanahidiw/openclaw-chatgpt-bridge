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
| 1 | [OpenClaw setup and testing]({{ '/openclaw-setup.html' | relative_url }}) | The webhook route the bridge posts to, plus the two values it needs: `OPENCLAW_WEBHOOK_URL` and `OPENCLAW_WEBHOOK_SECRET` |
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

## 2. Create your `.env` file

Copy the example file:

```bash
cp .env.example .env
```

Open `.env` and set these values:

- `OPENCLAW_WEBHOOK_URL`
- `OPENCLAW_WEBHOOK_SECRET`
- `OPENCLAW_SESSION_KEY`

### What each one means

| Setting | What to put there |
|---|---|
| `OPENCLAW_WEBHOOK_URL` | The OpenClaw webhook address |
| `OPENCLAW_WEBHOOK_SECRET` | A long secret shared between the bridge and OpenClaw |
| `OPENCLAW_SESSION_KEY` | Recorded in bridge logs only. The OpenClaw webhook route is bound to a session by OpenClaw config, so the bridge cannot choose one |

### Important note about private OpenClaw

If OpenClaw is private, point `OPENCLAW_WEBHOOK_URL` to the **Tailscale hostname** or **Tailscale IP address**.
For example:

```text
http://openclaw-gateway.tailnet:18789/plugins/webhooks/gpt
```

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
  --data '{"action":"create_flow","goal":"test"}'
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
> instructions the model needs to handle `expectedRevision` — see
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
- `OPENCLAW_WEBHOOK_SECRET` matches on both sides
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
- [ ] `OPENCLAW_WEBHOOK_URL` is set correctly
- [ ] `go test ./...` passes, or Docker works
- [ ] bridge starts with `go run .` or Docker
- [ ] `healthz` and `readyz` respond
- [ ] `/v1/openclaw` works
- [ ] GPT Action is imported
- [ ] Test prompt works in ChatGPT
