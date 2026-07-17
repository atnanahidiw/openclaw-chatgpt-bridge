---
title: Documentation
description: Connect ChatGPT Actions to OpenClaw with a small, secure Go bridge.
---

<section class="hero">
  <span class="eyebrow">Open source bridge</span>
  <h1>Delegate work from ChatGPT to OpenClaw.</h1>
  <p class="lead">A small Go service that securely connects Custom GPT Actions to an OpenClaw webhook—without exposing OpenClaw directly.</p>
  <div class="hero-actions">
    <a class="button primary" href="{{ '/step-by-step.html' | relative_url }}">Start building&nbsp; →</a>
    <a class="button" href="https://github.com/atnanahidiw/openclaw-chatgpt-bridge">View on GitHub</a>
  </div>
</section>

<h2 class="section-heading">Get started</h2>
<div class="card-grid">
  <a class="doc-card" href="{{ '/step-by-step.html' | relative_url }}">
    <span class="card-icon">01</span>
    <strong>Step-by-step setup</strong>
    <span>Go from clone to a working bridge with the simplest supported path.</span>
  </a>
  <a class="doc-card" href="{{ '/usecases.html' | relative_url }}">
    <span class="card-icon">02</span>
    <strong>Explore use cases</strong>
    <span>See how the bridge supports engineering, operations, research, and more.</span>
  </a>
  <a class="doc-card" href="{{ '/commands.html' | relative_url }}">
    <span class="card-icon">⌘</span>
    <strong>Commands</strong>
    <span>Copy the commands for local development, Docker, and Kubernetes.</span>
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
    <span class="card-icon">AWS</span>
    <strong>Amazon Web Services</strong>
    <span>Deploy with EC2, Route 53, Caddy, and Tailscale.</span>
  </a>
  <a class="doc-card" href="{{ '/deployment/azure.html' | relative_url }}">
    <span class="card-icon">AZ</span>
    <strong>Microsoft Azure</strong>
    <span>Deploy with Azure VM, Azure DNS, Caddy, and Tailscale.</span>
  </a>
  <a class="doc-card" href="{{ '/deployment/digital-ocean.html' | relative_url }}">
    <span class="card-icon">DO</span>
    <strong>DigitalOcean</strong>
    <span>Run the bridge on a Droplet with HTTPS and private connectivity.</span>
  </a>
  <a class="doc-card" href="{{ '/deployment/gcp.html' | relative_url }}">
    <span class="card-icon">GCP</span>
    <strong>Google Cloud Platform</strong>
    <span>Deploy with Compute Engine, Cloud DNS, Caddy, and Tailscale.</span>
  </a>
</div>
