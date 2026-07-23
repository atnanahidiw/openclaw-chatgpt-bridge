---
title: Custom GPT setup
---

# Custom GPT setup

This page turns a deployed bridge into a working Custom GPT.

Do it **after** [OpenClaw setup]({{ '/openclaw-setup.html' | relative_url }}) and after the
bridge is deployed and answering on a public HTTPS URL.

## One-sentence explanation

**The Custom GPT delegates work to OpenClaw as an external tool, and reports back what
OpenClaw actually did.**

## Before you start

You need:

- a ChatGPT plan that can create Custom GPTs
- your bridge's public HTTPS URL, answering on `/healthz`
- the file `openapi/openclaw-bridge.openapi.yaml` from this repository

Check the bridge first. Everything below assumes this works:

```bash
curl https://YOUR-BRIDGE-URL/healthz
curl https://YOUR-BRIDGE-URL/readyz
```

`/readyz` is the important one — it returns `ok` only when the bridge can reach OpenClaw
through Tailscale.

---

## Step 1 — Create the GPT

1. Open ChatGPT.
2. Go to **Explore GPTs**.
3. Click **Create**.
4. Open the **Configure** tab.
5. Give it a name and description, for example *OpenClaw Operator*.

You can keep the GPT private. Nothing here requires publishing it.

---

## Step 2 — Add the action

1. Scroll to **Actions** and click **Create new action**.
2. Click **Import from URL**, or paste the schema directly.
3. Paste the contents of `openapi/openclaw-bridge.openapi.yaml`.

### Then fix the server URL

The schema ships with a placeholder. Find the `servers:` block near the top and replace it
with your own bridge URL:

```yaml
servers:
  - url: https://YOUR-BRIDGE-URL
```

**This is the step people forget.** If you leave the placeholder, every call fails or, worse,
goes somewhere that is not yours.

After importing you should see one available action: `sendToOpenClaw`.

---

## Step 3 — Set the API key

The bridge attaches the OpenClaw webhook secret itself, so **without an inbound key anyone
who knows your bridge URL can create, modify, and cancel TaskFlows in your OpenClaw.** The
webhook secret protects OpenClaw from arbitrary callers; it does nothing to protect the
bridge.

Set `BRIDGE_API_KEY` on the bridge and give the same value to the Action.

### On the bridge

Set `BRIDGE_API_KEY` in your `.env` — see
[Configure `.env`]({{ '/step-by-step.html' | relative_url }}#env-setup) for how to generate
one and why it matters. Then deploy that value. How depends on where the bridge runs — see the
[deployment guide]({{ '/deployment/free-tier.html' | relative_url }}) for your platform,
which reads the value straight out of `.env`.

If the variable is unset the bridge still starts, but it logs a warning at boot and accepts
unauthenticated requests. That is a deliberate choice you can make, not an accident you can
drift into unaware.

### In the Action

1. In the Action editor, open **Authentication**.
2. Choose **API Key**.
3. Set **Auth Type** to **Custom**.
4. Set the **Custom Header Name** to `api_key`.
5. Paste the same value you set as `BRIDGE_API_KEY`.

The schema already declares this, so the builder should preselect most of it:

```yaml
security:
  - api_key: []
components:
  securitySchemes:
    api_key:
      type: apiKey
      in: header
      name: api_key
```

### Check it worked

```bash
BRIDGE=https://YOUR-BRIDGE-URL
KEY=your-key

# no credential -> 401
curl -s -o /dev/null -w "%{http_code}\n" -X POST "$BRIDGE/v1/openclaw" \
  -H 'content-type: application/json' -d '{"action":"ask","message":"ping"}'

# with the key -> 200
curl -s -o /dev/null -w "%{http_code}\n" -X POST "$BRIDGE/v1/openclaw" \
  -H 'content-type: application/json' -H "api_key: $KEY" \
  -d '{"action":"ask","message":"ping"}'
```

`401` then `200` means the endpoint is closed and your key works. Two `200`s mean the key is
not set on the bridge and it is still open.

Health endpoints stay open on purpose — container platforms probe `/healthz` and `/readyz`
without credentials, so requiring a key there would break deployment.

### What the key does and does not protect

| | |
|---|---|
| Stops a stranger who finds your bridge URL | Yes |
| Stops someone who has the key | No — treat it like a password |
| Protects OpenClaw if the bridge is compromised | No. Bind the webhook route to a narrow session so the blast radius is small |

Rotating it means updating both sides: the bridge's environment variable **and** the Action's
saved credential. The bridge reads it at startup, so redeploy or restart the revision after
changing it.

## Step 4 — Give the GPT instructions

Frame OpenClaw as **an external tool the GPT delegates to**, not a chatbot it talks with.
That framing matters: OpenClaw runs on a real machine with shell access, file access, and
your configured skills. It executes. The GPT's job is to decide *what* to delegate, then
report back what came out.

Paste this into the **Instructions** box and edit to taste:

```text
You have one external tool: OpenClaw, reached through the sendToOpenClaw action.

WHAT OPENCLAW IS
OpenClaw is an autonomous agent running on the user's own machine. It can read
and write files, run shell commands, and use its installed skills. It is not a
chat partner: it carries out instructions and reports what it did. Treat it the
way you would treat a capable engineer you are handing a ticket to.

Your job is to decide what to delegate, phrase it precisely, and relay the
result. Do not pretend to do the work yourself, and never invent an answer you
did not receive from OpenClaw.

CHOOSING THE RIGHT CALL
Default to ask_async. Use it unless you have a specific reason not to, including
for greetings and one-line questions.

Why: even a trivial reply takes about 60 seconds, because a real agent turn has
to start. If the bridge has been idle it also has to wake up first, which adds
another 20 or so. That total can exceed the time this Action is allowed to wait,
and the user sees a connection error instead of an answer. ask_async returns in
well under a second, so it never hits that limit.

- ask_async: your default. Returns a jobId immediately. Then poll get_result.
- get_result: fetch an async reply using that jobId.
- ask: only when the user explicitly asks you to wait, or you are already several
  successful turns into a conversation and know the bridge is warm. Even then it
  is the riskier choice.

"Say hi to OpenClaw" is an ask_async, not an ask. Short question does not mean
short turn.

THE ASYNC LOOP
1. Call ask_async with a specific instruction. Keep the jobId.
2. Tell the user the work has started, in one short sentence.
3. Call get_result with that jobId.
4. status "running" means keep waiting. Wait several seconds before polling
   again. Do not poll in a tight loop.
5. status "done" means read the reply field and report it.
6. status "failed" means read the error field, tell the user plainly, and stop.
   Follow the hint field: if it says the turn will not complete, do not retry.

CONVERSATION CONTINUITY
Pass the same `user` value on every call in one conversation, so OpenClaw keeps
context between turns. Invent one stable string at the start of the chat, for
example "conv:" plus a short random suffix. Change it only when the user asks to
start fresh.

WRITING GOOD INSTRUCTIONS
- Be specific about the goal and what "done" looks like.
- Name paths, files, or commands when you know them.
- Ask for exact output when you need it verbatim, and say "do not guess".
- One task per call. Do not bundle unrelated work.

REPORTING BACK
- Relay what OpenClaw actually said. Quote it when the wording matters.
- If OpenClaw reports a failure or a partial result, say so plainly. Never
  present a failure as a success.
- If a reply looks wrong or incomplete, say what you noticed rather than
  smoothing over it.

ERRORS
- 401 with error "unauthorized": the Action's api_key is missing or wrong. Tell
  the user to check it. Do not retry.
- 500: the bridge is misconfigured, often a bad gateway token. Not retryable.
- 502: OpenClaw was unreachable. The user's machine may be asleep or offline.
- 504: the turn took too long. Retry with ask_async instead.
- 400 with details: read the details list, fix the fields, retry once.

SAFETY
OpenClaw acts on a real machine. Before delegating anything destructive —
deleting files, force-pushing, changing system settings — confirm with the user
first, and repeat back exactly what will happen.
```

### Why the async loop needs spelling out

Left to itself a model tends to call `ask` for everything, hit the timeout, and report
failure for work that would have succeeded. It also tends to poll `get_result` immediately
and repeatedly rather than waiting. Both behaviours are cheap to prevent in the instructions
and annoying to debug afterwards.

---

## Step 5 — Test it

Save the GPT, then work up from the smallest call.

### 1. Prove the connection

```text
Ask OpenClaw to confirm it is reachable and name its working directory.
```

Expect an `ask`, and a real path in the answer. This proves the Action, the URL, the API
key, and the network path in one step. Takes around 60 seconds.

### 2. Prove it can act

```text
Ask OpenClaw to run `sw_vers` and paste the exact output.
```

The output should be real system information, not a plausible guess. If you get something
that looks invented, the GPT is answering for itself instead of delegating — tighten the
"never invent an answer" line in the instructions.

### 3. Prove the async loop

```text
Ask OpenClaw to summarize what is in its workspace, reading files as needed.
```

Watch that the GPT calls `ask_async`, tells you it started, then polls `get_result` with
sensible gaps rather than hammering it. Real work takes a minute or two.

## Day-to-day use

```text
Create a flow to review this repo and identify release blockers
Run a task to update the deployment docs based on the current manifests
What is the status of that flow?
Finish the flow
```

The rule of thumb: **ChatGPT decides and reviews, OpenClaw executes.**

---

## Troubleshooting

| What you see in ChatGPT | Cause | Fix |
|---|---|---|
| "I could not reach the service" | `servers:` still has the placeholder URL, or the bridge is asleep | Fix the URL. A cold-starting bridge takes a few seconds — ask again |
| Talks about calling the action but nothing happens | The action was saved without importing cleanly | Re-import the schema and confirm `sendToOpenClaw` is listed |
| `401`, body `{"error":"unauthorized"}` | The Action's `api_key` is missing or wrong | Re-check Step 3: it must equal `BRIDGE_API_KEY` on the bridge |
| "I could not reach OpenClaw", or a connection error | Almost always a cold start plus a sync `ask`. The bridge sleeps when idle (~20s to wake) and a turn takes ~30s, which together exceed what the Action waits for | Make the GPT use `ask_async`. Nothing is broken; it just took too long |
| `504` from the bridge | The turn outlived `REQUEST_TIMEOUT_MS` | Use `ask_async`. Raising the timeout does not help, because the Action gives up first |
| `500` | The bridge's own gateway token is wrong, or its URL is unset | Server-side. Check `OPENCLAW_GATEWAY_TOKEN` and `OPENCLAW_GATEWAY_URL` |
| `502` | OpenClaw was unreachable | The machine running OpenClaw may be asleep, offline, or off the tailnet |
| `404` on `get_result` | The jobId expired, or the bridge restarted and lost it | Jobs are in memory. Start the work again |
| Answers look invented rather than executed | The GPT answered instead of delegating | Tighten "never invent an answer you did not receive from OpenClaw" |
| Polls `get_result` in a tight loop | Instructions not followed | Re-state the wait-several-seconds rule |

---|---|---|
| "I could not reach the service" | `servers:` still has the placeholder URL, or the bridge is asleep | Fix the URL. A cold-starting bridge can take several seconds — ask again |
| Talks about calling the action but nothing happens | The action was saved without being imported cleanly | Re-import the schema and confirm `sendToOpenClaw` is listed |
| `Unrecognized keys` | The model added a field the action does not accept | Strengthen the FIELD RULES section of the instructions |
| `401`, body is `{"error":"unauthorized"}` | The Action's `api_key` is missing or wrong | Re-check Step 3: the Action credential must equal `BRIDGE_API_KEY` on the bridge |
| `401` with an `upstreamStatus` field | OpenClaw rejected the bridge's own webhook secret | See [Troubleshooting]({{ '/troubleshooting.html' | relative_url }}) — usually a missed restart |
| First call each morning fails, later ones work | Cold start | Retry, or run with `minReplicas: 1` and leave the free tier |

---

## Checklist

- [ ] Bridge answers on `/healthz` and `/readyz`
- [ ] GPT created
- [ ] Schema imported, `sendToOpenClaw` visible
- [ ] `servers:` URL points at **your** bridge
- [ ] `BRIDGE_API_KEY` set on the bridge, same value saved in the Action
- [ ] Unauthenticated request returns `401`, authenticated returns `200`
- [ ] Instructions pasted, including the async loop and the external-tool framing
- [ ] A simple `ask` returns a real answer
- [ ] An `ask_async` task completes and the GPT reports the reply
