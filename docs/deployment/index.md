---
title: Deployment
description: Pick a platform and deploy the OpenClaw ChatGPT Bridge behind HTTPS.
---

# Deployment

Every guide below ends with the same result: the bridge running behind a public HTTPS URL
that a Custom GPT Action can call, with a private path from the bridge to OpenClaw.

Finish [OpenClaw setup]({{ '/openclaw-setup.html' | relative_url }}) and
[Step-by-step setup]({{ '/step-by-step.html' | relative_url }}) first. The deployment guides
assume you already have a working `.env` and a webhook OpenClaw answers.

<div class="danger" markdown="1">
<div markdown="1">
<span class="danger-title">Set `BRIDGE_API_KEY` before you expose anything</span>

The bridge endpoint has to be publicly reachable for ChatGPT to call it. `BRIDGE_API_KEY` is
the only thing standing between the internet and your OpenClaw agent. Leave it unset and the
endpoint accepts anyone who finds the URL.
</div>
</div>

<h2 class="section-heading">Choose a deployment target</h2>
<div class="card-grid">
  <a class="doc-card" href="{{ '/deployment/aws.html' | relative_url }}">
    <span class="card-icon card-logo"><img src="{{ '/assets/img/amazonwebservices.svg' | relative_url }}" alt="" width="20" height="20"></span>
    <strong>Amazon Web Services</strong>
    <span>Deploy with EC2, Route 53, Caddy, and Tailscale.</span>
  </a>
  <a class="doc-card" href="{{ '/deployment/azure.html' | relative_url }}">
    <span class="card-icon card-logo"><img src="{{ '/assets/img/microsoftazure.svg' | relative_url }}" alt="" width="20" height="20"></span>
    <strong>Microsoft Azure</strong>
    <span>Deploy with Azure VM, Azure DNS, Caddy, and Tailscale.</span>
  </a>
  <a class="doc-card" href="{{ '/deployment/digital-ocean.html' | relative_url }}">
    <span class="card-icon card-logo"><img src="{{ '/assets/img/digitalocean.svg' | relative_url }}" alt="" width="20" height="20"></span>
    <strong>DigitalOcean</strong>
    <span>Run the bridge on a Droplet with HTTPS and private connectivity.</span>
  </a>
  <a class="doc-card" href="{{ '/deployment/gcp.html' | relative_url }}">
    <span class="card-icon card-logo"><img src="{{ '/assets/img/googlecloud.svg' | relative_url }}" alt="" width="20" height="20"></span>
    <strong>Google Cloud Platform</strong>
    <span>Deploy with Compute Engine, Cloud DNS, Caddy, and Tailscale.</span>
  </a>
</div>

## Which one should I pick?

| | Best for | HTTPS | Free path |
|---|---|---|---|
| [Google Cloud]({{ '/deployment/gcp.html' | relative_url }}) | Scale-to-zero container hosting | Managed | [Cloud Run]({{ '/deployment/gcp.html' | relative_url }}#free-tier-deployment-with-cloud-run) |
| [Azure]({{ '/deployment/azure.html' | relative_url }}) | Container hosting, tested end to end | Managed | [Container Apps]({{ '/deployment/azure.html' | relative_url }}#free-tier-deployment-with-azure-container-apps) |
| [AWS]({{ '/deployment/aws.html' | relative_url }}) | A plain VM you control | Caddy | none |
| [DigitalOcean]({{ '/deployment/digital-ocean.html' | relative_url }}) | The simplest VM path | Caddy | none |

If you have no preference, start with [the free options]({{ '/deployment/free-tier.html' | relative_url }}).

## After deploying

- Point your Custom GPT Action at the new URL. See [Custom GPT setup]({{ '/custom-gpt.html' | relative_url }}).
- Something not working? [Troubleshooting]({{ '/troubleshooting.html' | relative_url }}) covers the common failures.
