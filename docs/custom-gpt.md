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

## Step 3 — Understand what you just exposed

Read this before going further.

**The bridge does not authenticate incoming requests.** It attaches the OpenClaw webhook
secret itself, so anyone who knows your bridge URL can create, modify, and cancel TaskFlows
in your OpenClaw. The webhook secret protects OpenClaw from arbitrary callers; it does not
protect the bridge.

You can confirm this against your own deployment:

```bash
curl -s -o /dev/null -w "%{http_code}\n" -X POST https://YOUR-BRIDGE-URL/v1/openclaw \
  -H 'content-type: application/json' \
  -d '{"action":"get_flow","flowId":"probe"}'
```

A `200` with no credentials means the endpoint is open to anyone who finds the URL.

### How exposed is that, really?

| Factor | Reality |
|---|---|
| Can a stranger find the URL? | Container Apps and Cloud Run hostnames are long and random, and not indexed. Obscurity, not security |
| What can a caller do? | Everything the webhook route's `sessionKey` owns — create, mutate, cancel flows |
| What can they *not* do? | Reach OpenClaw directly, read your tailnet, or use the webhook secret elsewhere |

### Options, weakest to strongest

1. **Accept it.** Reasonable for a personal, unpublished GPT on an obscure URL, and it is
   where you land by default. Know that you are choosing it.
2. **Set an API key in the Action.** Under **Authentication**, choose **API Key**, pick
   *Bearer* or a custom header, and set a long random value. ChatGPT will send it on every
   call. **This only helps if the bridge checks it** — today it does not, so you must add an
   inbound token check to the bridge for this to be worth anything.
3. **Put a gateway in front.** An API gateway or reverse proxy that rejects requests without
   the right header before they reach the bridge. Most work, no bridge changes.

Bind the webhook route to a narrow session either way, so a leaked URL cannot touch your
main agent's flows. See [OpenClaw setup]({{ '/openclaw-setup.html' | relative_url }}).

---

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
- A 401 means the bridge and OpenClaw secrets disagree. Tell the user plainly and do
  not retry, because retrying cannot fix it.
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
| `401` from the bridge | Bridge and OpenClaw secrets disagree | See [Troubleshooting]({{ '/troubleshooting.html' | relative_url }}) — usually a missed restart |
| First call each morning fails, later ones work | Cold start | Retry, or run with `minReplicas: 1` and leave the free tier |

---

## Checklist

- [ ] Bridge answers on `/healthz` and `/readyz`
- [ ] GPT created
- [ ] Schema imported, `sendToOpenClawBridge` visible
- [ ] `servers:` URL points at **your** bridge
- [ ] You have decided, deliberately, how exposed the bridge endpoint is
- [ ] Instructions pasted, including the expectedRevision rule
- [ ] `get_flow` works from a prompt
- [ ] `create_flow` returns a flowId
- [ ] `finish_flow` works, with the GPT reading the revision first
