# Azure deployment for the OpenClaw ChatGPT Bridge

This guide is written for a **non-technical person** or an **intern**.
It gives one simple path:

- use **Azure Virtual Machines** for the server
- use **Azure DNS** for the domain name
- use **Caddy** for HTTPS
- use **Tailscale** so the bridge can reach OpenClaw privately

## One-sentence explanation

**ChatGPT talks to the bridge on a public HTTPS URL, and the bridge talks to OpenClaw through Tailscale.**

## What goes where

| Part | What it does |
|---|---|
| Azure VM | runs the bridge |
| Static public IP | gives the server a fixed public IP |
| Azure DNS | gives the bridge a domain name |
| Caddy | gives the bridge HTTPS |
| Tailscale | lets the bridge reach private OpenClaw |
| ChatGPT Action | calls the public bridge URL |

## Simple connection map

```text
ChatGPT -> https://bridge.yourdomain.com -> bridge on VM -> Tailscale -> OpenClaw
```

## Before you start

You need:

- an Azure subscription
- 1 Ubuntu VM
- 1 domain name, for example `bridge.yourdomain.com`
- access to the OpenClaw tailnet address or Tailscale IP

You do **not** need Kubernetes.

---

## Step 1 — Create the VM

1. Sign in to the [Azure portal](https://portal.azure.com/).
2. In the left menu, click **Virtual machines**.
3. Click **Create**.
4. Click **Azure virtual machine**.
5. Give the VM a name, such as `openclaw-bridge`.
6. Choose an Ubuntu image, such as **Ubuntu 22.04 LTS** or **24.04 LTS**.
7. Pick a small VM size.
8. Make sure the VM will have a **public IP address**.
9. Create the VM.

When the VM is ready, copy its public IP for now.

---

## Step 2 — Make the public IP static

If the VM IP changes, your domain record will break.
So make the public IP **static**.

### Where to click in Azure

1. Open the VM you just created.
2. In the VM overview, click the **public IP address** resource.
3. Open the public IP resource.
4. Make sure the assignment is **Static**.
5. If you need to create a new public IP, choose **Create new** and pick **Static**.

Now the VM has a fixed public IP.
Keep that IP handy.

---

## Step 3 — Create the DNS zone and A record

This step tells the internet that your domain should open your VM.

### Where to click in Azure DNS

1. Open **Azure DNS** in the portal.
2. Click **Create** to make a DNS zone.
3. Enter your domain name, for example `yourdomain.com`.
4. Create the zone.
5. Open the DNS zone.
6. Click **Record sets**.
7. Click **Add**.

### How to create the A record

In the record form, fill in these fields:

| Field | What to enter |
|---|---|
| Name | `bridge` |
| Type | `A` |
| TTL | leave the default value |
| IP address | the static public IP |

This creates:

```text
bridge.yourdomain.com -> <static-ip>
```

### Finish the record

1. Check the values again.
2. Click **Save**.
3. Wait a few minutes for DNS to update.

If you manage DNS somewhere else, create the same `A` record there instead.

---

## Step 4 — Install the needed tools

SSH into the VM and run these commands one by one.
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

When this step is done, the VM has all the tools it needs.
Next, we connect it to OpenClaw.

---

## Step 5 — Connect the VM to OpenClaw through Tailscale

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

From the VM, try opening the OpenClaw address.
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
  --network host \
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

- [ ] VM created
- [ ] Static public IP attached
- [ ] Azure DNS record points to the static IP
- [ ] Docker installed
- [ ] Caddy installed
- [ ] Tailscale connected
- [ ] OpenClaw reachable over the tailnet
- [ ] Bridge container running
- [ ] HTTPS works on `https://bridge.yourdomain.com`
- [ ] Custom GPT action imported
