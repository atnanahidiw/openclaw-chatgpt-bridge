---
title: Free deployment options
---

# Free deployment options for the OpenClaw ChatGPT Bridge

You can run the bridge without paying for a server, a domain, or an HTTPS certificate.
This page helps you pick a platform. The step-by-step instructions live in a **Free tier** section at the bottom of each provider's guide:

- [Google Cloud Run]({{ '/deployment/gcp.html' | relative_url }}#free-tier-deployment-with-cloud-run) — in the Google Cloud guide
- [Azure Container Apps]({{ '/deployment/azure.html' | relative_url }}#free-tier-deployment-with-azure-container-apps) — in the Azure guide

## One-sentence explanation

**ChatGPT talks to the bridge on a free HTTPS URL, and the bridge talks to OpenClaw through Tailscale.**

## How these differ from the other guides

The AWS, Azure, DigitalOcean, and Google Cloud guides all rent a server, buy a domain, and install Caddy for HTTPS.
The free guides skip all three.

| | Other guides | Free guides |
|---|---|---|
| Server | a VM you pay for | a serverless container platform |
| Domain name | you buy one | not needed |
| HTTPS certificate | Caddy sets it up | included, automatic |
| Runs when idle | yes, always on | no, sleeps and wakes on demand |
| Monthly cost | a few dollars | nothing, within the free limits |

---

## Which free platforms actually work?

Free hosting changes often, and several popular options stopped working for this use case.
Here is where things stand as of **July 2026**:

| Platform | Free? | Good for this bridge? |
|---|---|---|
| **[Google Cloud Run]({{ '/deployment/gcp.html' | relative_url }}#free-tier-deployment-with-cloud-run)** | Yes, always free tier | **Recommended.** Official Tailscale support, free HTTPS URL, no sleep penalty you will notice |
| **[Azure Container Apps]({{ '/deployment/azure.html' | relative_url }}#free-tier-deployment-with-azure-container-apps)** | Yes, always free grant | **Equally good, and simpler to set up.** Same monthly free grant, and it runs Tailscale as a sidecar container so you keep the normal `Dockerfile` |
| Huawei Cloud FunctionGraph | Yes, monthly free tier | 1 million requests and 400,000 GB-seconds a month, reset monthly. Workable in theory, awkward in practice: functions are short-lived, so Tailscale has to rejoin the tailnet on every cold start |
| Alibaba Cloud | **Trial only** | No permanent free compute. New accounts get a 12-month ECS trial and 3 monthly cycles of Function Compute quota, then it becomes paid |
| Oracle Cloud Always Free | Yes, a real VM | Works well, but signup is frequently declined or stuck in review, and ARM capacity is often unavailable. If you get one, follow the [DigitalOcean guide]({{ '/deployment/digital-ocean.html' | relative_url }}) instead — the steps are the same |
| Northflank | Free plan, no forced sleep | Workable, but a credit card is required |
| Render | Free web services | Free instances spin down when idle and can take ~50 seconds to wake, which is long enough for a ChatGPT Action to give up |
| Fly.io | **No longer free** | New accounts get a short trial only. The old free allowances were removed |
| Koyeb | **Closed to new users** | The free Starter tier was closed after the Mistral AI acquisition in early 2026 |

---

## Cloud Run or Container Apps?

Both are genuinely free for this workload, and their monthly grants are identical: **2 million requests, 180,000 vCPU-seconds, 360,000 GiB-seconds**.

| | Google Cloud Run | Azure Container Apps |
|---|---|---|
| Tailscale runs as | extra files inside your image | a separate container beside the bridge |
| Dockerfile | a custom one you build | the repository's normal one |
| Startup script | you write a `start.sh` | none needed |
| Health checks count as requests | yes | no, probes are not billable |
| Setup effort | more steps | fewer steps |

**Pick Azure Container Apps if you have no strong preference.** The sidecar model matches what the project's Helm chart already does on Kubernetes, and it needs neither a custom image nor a startup script.

**Pick Cloud Run** if you are already on Google Cloud, or you want the path Tailscale documents officially.

Both require a billing account with a card on file, even though neither charges you inside the free limits.
Both allowances can change — check the [Cloud Run](https://cloud.google.com/run/pricing) and [Container Apps](https://azure.microsoft.com/en-us/pricing/details/container-apps/) pricing pages before relying on them.

---

## What every free option has in common

Whichever platform you choose, three things hold true:

**Tailscale must run in userspace mode.**
Serverless platforms do not give containers the privileges needed for a normal VPN network device. Userspace mode works without one and offers a local proxy instead.

**`OPENCLAW_WEBHOOK_URL` must be a tailnet address.**
Go never sends requests for `localhost` or `127.0.0.1` through a proxy. Point the bridge at a loopback address and it will silently skip Tailscale. Use the tailnet hostname or the `100.x.x.x` address.

**If OpenClaw sits behind `tailscale serve`, the URL is `https` with no port.**
`tailscale serve` fronts a local port on HTTPS 443 across your tailnet, so the address becomes `https://your-host.your-tailnet.ts.net/...` rather than `http://your-host:18789/...`. Three consequences:

- Plain HTTP on port 80 does **not** work. Only HTTPS.
- The request travels through `HTTPS_PROXY` as a `CONNECT` tunnel, not `HTTP_PROXY`. Set both, as the guides do.
- The container must carry a CA trust store or the call fails with `x509: certificate signed by unknown authority`. The repository `Dockerfile` installs `ca-certificates` for exactly this reason.

Check what your host publishes with:

```bash
tailscale serve status
```

**Use an ephemeral, reusable auth key.**
The platform starts and stops your containers on its own. Ephemeral keeps your device list clean; reusable lets it start more than once.

## Short summary

- **ChatGPT talks to a free HTTPS URL**
- **the bridge talks to OpenClaw through Tailscale in userspace mode**
- **OpenClaw does not need to be public, and you do not need a server or a domain**
