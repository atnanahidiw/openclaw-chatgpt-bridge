---
title: Custom GPT setup
---

# Custom GPT setup

This page turns a deployed bridge into a working Custom GPT.

Do it **after** [OpenClaw setup]({{ '/openclaw-setup.html' | relative_url }}) and after the
bridge is deployed and answering on a public HTTPS URL.

## One-sentence explanation

**The Custom GPT calls one action, the bridge forwards it to OpenClaw, and OpenClaw does
the work.**

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

After importing you should see one available action: `sendToOpenClawBridge`.

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
  -H 'content-type: application/json' -d '{"action":"get_flow","flowId":"probe"}'

# with the key -> 200
curl -s -o /dev/null -w "%{http_code}\n" -X POST "$BRIDGE/v1/openclaw" \
  -H 'content-type: application/json' -H "api_key: $KEY" \
  -d '{"action":"get_flow","flowId":"probe"}'
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

Without instructions the model will guess at the contract and get rejected — most often by
inventing an `expectedRevision` instead of reading one.

Paste this into the **Instructions** box and edit to taste:

```text
You drive an OpenClaw automation bridge through the sendToOpenClawBridge action.

WORKFLOW
- Start work with create_flow and a clear, specific goal. Remember the returned
  flowId for the rest of the conversation.
- Send work with run_task: it needs flowId, task, and runtime. Use runtime
  "subagent" unless the user asks otherwise.
- Check progress with get_flow.
- Close work with finish_flow when the user says it is done.

EXPECTED REVISION - THIS IS THE PART THAT BREAKS
resume_flow and finish_flow both require expectedRevision, and you cannot guess it.
Always call get_flow first, read the "revision" number from the response, and pass
that exact value. The revision changes after every write, so re-read it before each
one. Never invent, reuse, or increment it yourself.

FIELD RULES
- notifyPolicy accepts only: done_only, state_changes, silent.
  It is valid on create_flow and run_task only. Never send it to get_flow,
  resume_flow, or finish_flow.
- status on create_flow: queued, running, waiting, or blocked.
- status on run_task and resume_flow: queued or running.
- finish_flow and get_flow accept no status at all.
- Never send sessionKey. OpenClaw decides which session owns the work.
- Send only the fields the action accepts. Extra fields fail the whole request.

ERRORS
- "Unrecognized keys" means you sent a field that action does not accept. Remove it
  and retry with only the allowed fields.
- "expectedRevision is required" means you skipped get_flow. Call it, then retry.
- A 401 with body {"error":"unauthorized"} means the Action's api_key is missing or
  wrong. A 401 carrying an upstreamStatus means OpenClaw rejected the bridge's own
  secret. Either way, tell the user plainly and do not retry: retrying cannot fix a
  credential mismatch.
- Report failures exactly as returned. Never claim work succeeded when it did not.

STYLE
- Confirm the goal before creating a flow.
- After each call, tell the user what happened in one short sentence.
- Show the flowId when it changes, so the user can follow along.
```

### Why the revision rule needs saying twice

`expectedRevision` is optimistic concurrency: OpenClaw rejects a write whose revision does
not match current state. A model that guesses `0`, or increments its last value, fails
intermittently — which is harder to debug than failing every time.

---

## Step 5 — Test it

Save the GPT, then work up from the simplest call.

### 1. A read that changes nothing

```text
Check the status of flow probe
```

Expect the GPT to call `get_flow` and report that nothing was found. This proves the
action, the URL, and the network path.

### 2. Create something

```text
Create a flow to review the deployment docs and list anything out of date
```

Expect a `flowId` back. Note it.

### 3. The revision round-trip

```text
Finish that flow
```

This is the real test. The GPT should call `get_flow` first, read the revision, then call
`finish_flow` with it. If it goes straight to `finish_flow`, your instructions are not
being followed — tighten the wording in Step 4.

---

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
| "I could not reach the service" | `servers:` still has the placeholder URL, or the bridge is asleep | Fix the URL. A cold-starting bridge can take several seconds — ask again |
| Talks about calling the action but nothing happens | The action was saved without being imported cleanly | Re-import the schema and confirm `sendToOpenClawBridge` is listed |
| `Unrecognized keys` | The model added a field the action does not accept | Strengthen the FIELD RULES section of the instructions |
| `expectedRevision is required` | The model skipped `get_flow` | Strengthen the EXPECTED REVISION section |
| `401`, body is `{"error":"unauthorized"}` | The Action's `api_key` is missing or wrong | Re-check Step 3: the Action credential must equal `BRIDGE_API_KEY` on the bridge |
| `401` with an `upstreamStatus` field | OpenClaw rejected the bridge's own webhook secret | See [Troubleshooting]({{ '/troubleshooting.html' | relative_url }}) — usually a missed restart |
| First call each morning fails, later ones work | Cold start | Retry, or run with `minReplicas: 1` and leave the free tier |

---

## Checklist

- [ ] Bridge answers on `/healthz` and `/readyz`
- [ ] GPT created
- [ ] Schema imported, `sendToOpenClawBridge` visible
- [ ] `servers:` URL points at **your** bridge
- [ ] `BRIDGE_API_KEY` set on the bridge, same value saved in the Action
- [ ] Unauthenticated request returns `401`, authenticated returns `200`
- [ ] Instructions pasted, including the expectedRevision rule
- [ ] `get_flow` works from a prompt
- [ ] `create_flow` returns a flowId
- [ ] `finish_flow` works, with the GPT reading the revision first
