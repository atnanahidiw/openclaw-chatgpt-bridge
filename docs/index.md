---
title: Documentation
description: Connect ChatGPT Actions to OpenClaw with a small, secure Go bridge.
---

<section class="hero">
  <span class="eyebrow">Open source bridge</span>
  <h1>Delegate work from ChatGPT to OpenClaw.</h1>
  <p class="lead">A small Go service that securely connects Custom GPT Actions to OpenClaw, without exposing OpenClaw directly.</p>
  <div class="hero-actions">
    <a class="button primary" href="{{ '/step-by-step.html' | relative_url }}">Start building&nbsp; →</a>
    <a class="button" href="https://github.com/atnanahidiw/openclaw-chatgpt-bridge">View on GitHub</a>
  </div>
</section>

<div class="danger" markdown="1">
<div markdown="1">
<span class="danger-title">Security warning — read before deploying</span>

This bridge deliberately exposes a path from the public internet into your OpenClaw agent.

**The bridge endpoint is public.** A Custom GPT Action can only call a publicly reachable
HTTPS URL. The only thing between the internet and your OpenClaw is `BRIDGE_API_KEY`. Leave
it unset and the endpoint accepts anyone who finds the URL.

**Whoever holds that key can act as you**, with whatever permissions the target agent has.
Bind the OpenClaw webhook route to the narrowest session that fits.

**The execution ingress is full operator access.** Enabling the Gateway's
`/v1/chat/completions` endpoint grants callers the complete operator scope set with owner
semantics. OpenClaw's own docs say to keep it on loopback or a private tailnet and never
expose it publicly. The bridge is what holds that boundary.

Full detail in [Configure `.env`]({{ '/step-by-step.html' | relative_url }}#env-setup) and
[Custom GPT setup]({{ '/custom-gpt.html' | relative_url }}).
</div>
</div>

<h2 class="section-heading">Get started</h2>
<div class="card-grid">
  <a class="doc-card" href="{{ '/usecases.html' | relative_url }}">
    <span class="card-icon">00</span>
    <strong>Explore use cases</strong>
    <span>See how the bridge supports engineering, operations, research, and more.</span>
  </a>
  <a class="doc-card" href="{{ '/openclaw-setup.html' | relative_url }}">
    <span class="card-icon">01</span>
    <strong>OpenClaw setup</strong>
    <span>Enable the webhook route on OpenClaw and test it. Do this before the bridge.</span>
  </a>
  <a class="doc-card" href="{{ '/step-by-step.html' | relative_url }}">
    <span class="card-icon">02</span>
    <strong>Step-by-step setup</strong>
    <span>Go from clone to a working bridge with the simplest supported path.</span>
  </a>
  <a class="doc-card" href="{{ '/custom-gpt.html' | relative_url }}">
    <span class="card-icon">03</span>
    <strong>Custom GPT setup</strong>
    <span>Import the action, set the server URL, and give the model instructions that work.</span>
  </a>
  <a class="doc-card" href="{{ '/troubleshooting.html' | relative_url }}">
    <span class="card-icon">?</span>
    <strong>Troubleshooting</strong>
    <span>Diagnose common configuration, connection, and deployment problems.</span>
  </a>
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
