# DigitalOcean deployment for the OpenClaw ChatGPT Bridge

This guide is written for a **non-technical person** or an **intern**.
It gives one simple path:

- use **DigitalOcean** for the server
- use **Caddy** for HTTPS
- use **Tailscale** so the bridge can reach OpenClaw privately

## One-sentence explanation

**ChatGPT talks to the bridge on a public HTTPS URL, and the bridge talks to OpenClaw through Tailscale.**

## What goes where

| Part | What it does |
|---|---|
| DigitalOcean Droplet | runs the bridge |
| Caddy | gives the bridge HTTPS |
| Tailscale | lets the bridge reach private OpenClaw |
| Domain name | gives the bridge a nice URL |
| ChatGPT Action | calls the public bridge URL |

## Simple connection map

```text
ChatGPT -> https://bridge.yourdomain.com -> bridge on Droplet -> Tailscale -> OpenClaw
```

## Before you start

You need:

- 1 DigitalOcean Droplet with Ubuntu
- 1 domain name, for example `bridge.yourdomain.com`
- access to the OpenClaw tailnet address or Tailscale IP

You do **not** need Kubernetes.

---

## Step 1 — Create a Droplet

1. Sign in to [DigitalOcean](https://cloud.digitalocean.com).
2. In the left menu, click **Create**.
3. Click **Droplets**.
4. Choose an Ubuntu image, such as **Ubuntu 22.04** or **Ubuntu 24.04**.
5. Pick a basic Droplet size. **1 GB RAM is enough to start**.
6. Make sure the Droplet will have a **public IPv4 address**.
7. Click **Create Droplet**.

When the Droplet is ready, copy its public IP address.

---

## Step 2 — Point your domain to the Droplet

This step tells the internet that your domain should open your Droplet.

### Where to click in DigitalOcean

1. Open the [DigitalOcean Control Panel](https://cloud.digitalocean.com).
2. In the left menu, click **Networking**.
3. In the Networking page, click **Domains**.
4. If your domain is not added yet, click **Add a domain**.
5. Type your domain name, for example `yourdomain.com`.
6. Click **Add domain**.
7. After the domain appears in the list, click the **domain name** itself.
8. You are now on the **Domain records** page.
9. Click **Create a record**.

### How to create the A record

In the record form, fill in these fields:

| Field | What to enter |
|---|---|
| Record type | `A` |
| Hostname | `bridge` |
| Will direct to | the Droplet public IP |
| TTL | leave the default value |

This creates:

```text
bridge.yourdomain.com -> <droplet-ip>
```

### Important note

If you want the root domain instead of a subdomain, the hostname is usually `@`.
For this project, we recommend using a subdomain like `bridge.yourdomain.com` because it is simpler and clearer.

### Finish the record

1. Check the values again.
2. Click **Create Record**.
3. Wait a few minutes for DNS to update.

---

## Step 3 — Install the needed tools

SSH into the Droplet and run these commands one by one.
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

When this step is done, the Droplet has all the tools it needs.
Next, we connect it to OpenClaw.

---

## Step 4 — Connect the Droplet to OpenClaw through Tailscale

This is the private path between the bridge and OpenClaw.

The bridge should talk to OpenClaw using a **tailnet address** such as:

```text
http://openclaw-gateway.tailnet:18789/plugins/webhooks/gpt
```

Think of this as the bridge's private back door to OpenClaw.
ChatGPT never uses this address directly.
Only the bridge uses it.

### How do I know the tailnet address?

Ask the person who manages OpenClaw for one of these:

- the **Tailscale hostname** for the OpenClaw server, or
- the **Tailscale IP address** for the OpenClaw server

If the OpenClaw machine is named `openclaw-gateway` in Tailscale, the address will often look like:

```text
http://openclaw-gateway.tailnet:18789/plugins/webhooks/gpt
```

If you only have the IP address, it may look like this instead:

```text
http://100.x.x.x:18789/plugins/webhooks/gpt
```

### Check that the private connection works

From the Droplet, try opening the OpenClaw address.
If it loads or responds, the private network link is ready.

If it does not work, ask the OpenClaw owner to confirm:

- the server is online
- Tailscale is running on the OpenClaw side
- the hostname or IP is correct

That means OpenClaw does **not** need to be public.

---

## Step 5 — Create the bridge config file

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

## Step 6 — Run the bridge container

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

## Step 7 — Give the bridge HTTPS with Caddy

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

## Step 8 — Test the bridge

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

## Step 9 — Add the action in Custom GPT

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

- [ ] Droplet created
- [ ] Domain points to the Droplet
- [ ] Docker installed
- [ ] Caddy installed
- [ ] Tailscale connected
- [ ] OpenClaw reachable over the tailnet
- [ ] Bridge container running
- [ ] HTTPS works on `https://bridge.yourdomain.com`
- [ ] Custom GPT action imported

## Short summary

If you only remember one thing, remember this:

- **ChatGPT talks to the public bridge URL**
- **the bridge talks to OpenClaw through Tailscale**
- **OpenClaw does not need to be public**
