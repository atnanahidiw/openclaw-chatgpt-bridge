---
title: AWS deployment
---

# AWS deployment for the OpenClaw ChatGPT Bridge

This guide is written for a **non-technical person** or an **intern**.
It gives one simple path:

- use **AWS EC2** for the server
- use **Route 53** for the domain name
- use **Caddy** for HTTPS
- use **Tailscale** so the bridge can reach OpenClaw privately

## One-sentence explanation

**ChatGPT talks to the bridge on a public HTTPS URL, and the bridge talks to OpenClaw through Tailscale.**

## What goes where

| Part | What it does |
|---|---|
| EC2 instance | runs the bridge |
| Elastic IP | gives the server a fixed public IP |
| Route 53 | gives the bridge a domain name |
| Caddy | gives the bridge HTTPS |
| Tailscale | lets the bridge reach private OpenClaw |
| ChatGPT Action | calls the public bridge URL |

## Simple connection map

```text
ChatGPT -> https://bridge.yourdomain.com -> bridge on EC2 -> Tailscale -> OpenClaw
```

## Before you start

You need:

- an AWS account
- 1 EC2 instance with Ubuntu
- 1 domain name, for example `bridge.yourdomain.com`
- access to the OpenClaw tailnet address or Tailscale IP

You do **not** need Kubernetes.

---

## Step 1 — Create the EC2 instance

1. Sign in to the [AWS Management Console](https://console.aws.amazon.com/).
2. Open **EC2**.
3. Click **Launch instance**.
4. Give the instance a name, such as `openclaw-bridge`.
5. Choose an Ubuntu image, such as **Ubuntu Server 22.04 LTS** or **24.04 LTS**.
6. Pick a small instance type. Start with something like `t3.micro` or similar if you only need a light workload.
7. Select or create a key pair if you want SSH access.
8. In the networking settings, make sure the instance can have internet access.
9. Launch the instance.

When the instance is ready, copy its public IP for now.

---

## Step 2 — Give the server a fixed IP

AWS public IPs can change if you stop and start the instance.
For a domain name, you want a fixed address, so use an **Elastic IP**.

### Where to click in AWS

1. In the AWS console, stay in **EC2**.
2. In the left menu, find **Network & Security**.
3. Click **Elastic IPs**.
4. Click **Allocate Elastic IP address**.
5. Leave the default options and click **Allocate**.
6. Select the new Elastic IP.
7. Click **Actions**.
8. Click **Associate Elastic IP address**.
9. Choose your EC2 instance.
10. Confirm the association.

Now the instance has a fixed public IP.
Keep that IP handy.

---

## Step 3 — Create the domain record in Route 53

This step tells the internet that your domain should open your AWS server.

### Where to click in Route 53

1. Open **Route 53** in the AWS console.
2. In the left menu, click **Hosted zones**.
3. If your domain is not there yet, click **Create hosted zone**.
4. Enter your domain name, for example `yourdomain.com`.
5. Make it a **Public hosted zone**.
6. Click **Create hosted zone**.
7. Open the hosted zone for your domain.
8. Click **Create record**.

### How to create the A record

In the record form, fill in these fields:

| Field | What to enter |
|---|---|
| Record name | `bridge` |
| Record type | `A` |
| Value | the Elastic IP address |
| TTL | leave the default value |

This creates:

```text
bridge.yourdomain.com -> <elastic-ip>
```

### Finish the record

1. Check the values again.
2. Click **Create records**.
3. Wait a few minutes for DNS to update.

If you manage DNS somewhere else, create the same `A` record there instead.

---

## Step 4 — Install the needed tools

SSH into the EC2 instance and run these commands one by one.
These install the software we need later:

- Docker runs the bridge
- Caddy gives the bridge HTTPS
- Tailscale lets the bridge reach private OpenClaw

```bash
sudo apt update
sudo apt install -y ca-certificates curl gnupg
```

### Install Docker

```bash
curl -fsSL https://get.docker.com | sudo sh
```

### Install Caddy

```bash
sudo apt install -y caddy
```

### Install Tailscale

```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
```

If you already have a Tailscale auth key, you can use that instead of the interactive login.

When this step is done, the EC2 instance has all the tools it needs.
Next, we connect it to OpenClaw.

---

## Step 5 — Connect the server to OpenClaw through Tailscale

This is the private path between the bridge and OpenClaw.

The bridge should talk to OpenClaw using a **tailnet address**.
Ask the OpenClaw owner for one of these:

- the **Tailscale hostname** for the OpenClaw server, or
- the **Tailscale IP address** for the OpenClaw server

The address may look like this:

```text
http://openclaw-gateway.tailnet:18789/plugins/webhooks/gpt
```

Or, if you only have an IP address:

```text
http://100.x.x.x:18789/plugins/webhooks/gpt
```

Think of this as the bridge's private back door to OpenClaw.
ChatGPT never uses this address directly.
Only the bridge uses it.

### Check that the private connection works

From the EC2 instance, try opening the OpenClaw address.
If it loads or responds, the private network link is ready.

If it does not work, ask the OpenClaw owner to confirm:

- the server is online
- Tailscale is running on the OpenClaw side
- the hostname or IP is correct

That means OpenClaw does **not** need to be public.

---

## Step 6 — Create the bridge config file

Create a folder for the bridge:

```bash
sudo mkdir -p /opt/openclaw-bridge
cd /opt/openclaw-bridge
```

Now create an `.env` file:

```bash
sudo tee .env >/dev/null <<'EOF'
ADDR=127.0.0.1:8080
OPENCLAW_WEBHOOK_URL=http://openclaw-gateway.tailnet:18789/plugins/webhooks/gpt
OPENCLAW_WEBHOOK_SECRET=replace-with-a-long-random-secret
OPENCLAW_SESSION_KEY=agent:main:main
REQUEST_TIMEOUT_MS=30000
MAX_BODY_BYTES=1048576
EOF
```

### What each line means

| Setting | Meaning |
|---|---|
| `ADDR=127.0.0.1:8080` | bridge listens only locally on the server |
| `OPENCLAW_WEBHOOK_URL` | private OpenClaw address over Tailscale |
| `OPENCLAW_WEBHOOK_SECRET` | secret used when the bridge calls OpenClaw |
| `OPENCLAW_SESSION_KEY` | default session key sent to OpenClaw |
| `REQUEST_TIMEOUT_MS` | how long to wait before timing out |
| `MAX_BODY_BYTES` | maximum request size allowed |

---

## Step 7 — Run the bridge container

If you already have a published image, run:

```bash
docker run -d \
  --name openclaw-bridge \
  --restart unless-stopped \
  --env-file .env \
  -p 127.0.0.1:8080:8080 \
  ghcr.io/your-org/openclaw-chatgpt-bridge:latest
```

If you do **not** have an image yet, build it first:

```bash
docker build -t openclaw-chatgpt-bridge:local .
```

Then run:

```bash
docker run -d \
  --name openclaw-bridge \
  --restart unless-stopped \
  --env-file .env \
  -p 127.0.0.1:8080:8080 \
  openclaw-chatgpt-bridge:local
```

### Check that the container is running

```bash
docker ps
```

You should see `openclaw-bridge` in the list.

---

## Step 8 — Give the bridge HTTPS with Caddy

Open the Caddy file:

```bash
sudo nano /etc/caddy/Caddyfile
```

Replace its contents with:

```caddy
bridge.yourdomain.com {
    reverse_proxy 127.0.0.1:8080
}
```

Save the file, then reload Caddy:

```bash
sudo systemctl reload caddy
```

Caddy will automatically get and renew the HTTPS certificate.

---

## Step 9 — Test the bridge

### Check the health page

Open this in a browser:

```text
https://bridge.yourdomain.com/healthz
```

You should see a small JSON response.

### Test the main API endpoint

Run this command:

```bash
curl -X POST https://bridge.yourdomain.com/v1/openclaw \
  -H 'content-type: application/json' \
  --data '{"action":"create_flow","goal":"test"}'
```

If it works, the bridge is ready.

---

## Step 10 — Add the action in Custom GPT

In ChatGPT:

1. Open **Explore GPTs**
2. Create a new GPT, or edit an existing one
3. Open **Actions**
4. Click **Create new action**
5. Import `openapi/openclaw-bridge.openapi.yaml`
6. Save the GPT

You can keep the Custom GPT private.

### What must be public?

Only the **bridge URL** must be public and reachable over HTTPS:

```text
https://bridge.yourdomain.com/v1/openclaw
```

OpenClaw itself can stay private behind Tailscale.

---

## Final checklist

- [ ] EC2 instance created
- [ ] Elastic IP attached
- [ ] Route 53 record points to the Elastic IP
- [ ] Docker installed
- [ ] Caddy installed
- [ ] Tailscale connected
- [ ] OpenClaw reachable over the tailnet
- [ ] Bridge container running
- [ ] HTTPS works on `https://bridge.yourdomain.com`
- [ ] Custom GPT action imported
