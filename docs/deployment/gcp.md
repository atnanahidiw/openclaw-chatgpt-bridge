---
title: Google Cloud deployment
---

# Google Cloud deployment for the OpenClaw ChatGPT Bridge

This guide is written for a **non-technical person** or an **intern**.

There are **two ways** to run the bridge on Google Cloud. Pick one:

| | Virtual machine | [Free tier: Cloud Run](#free-tier-deployment-with-cloud-run) |
|---|---|---|
| Cost | a few dollars a month | nothing, within the free limits |
| Domain name | you buy one | not needed |
| HTTPS | Caddy sets it up | included, automatic |
| Runs when idle | always on | sleeps, wakes on demand |
| Where | the rest of this page | [jump to the free tier section](#free-tier-deployment-with-cloud-run) |

**→ Want the free option? [Skip to Free tier deployment with Cloud Run](#free-tier-deployment-with-cloud-run).**

The virtual machine path below uses:

- **Google Compute Engine** for the server
- **Cloud DNS** for the domain name
- **Caddy** for HTTPS
- **Tailscale** so the bridge can reach OpenClaw privately

## One-sentence explanation

**ChatGPT talks to the bridge on a public HTTPS URL, and the bridge talks to OpenClaw through Tailscale.**

## What goes where

| Part | What it does |
|---|---|
| Compute Engine VM | runs the bridge |
| Static external IP | gives the server a fixed public IP |
| Cloud DNS | gives the bridge a domain name |
| Caddy | gives the bridge HTTPS |
| Tailscale | lets the bridge reach private OpenClaw |
| ChatGPT Action | calls the public bridge URL |

## Simple connection map

```text
ChatGPT -> https://bridge.yourdomain.com -> bridge on VM -> Tailscale -> OpenClaw
```

## Before you start

You need:

- a Google Cloud project
- 1 Compute Engine VM with Ubuntu
- 1 domain name, for example `bridge.yourdomain.com`
- access to the OpenClaw tailnet address or Tailscale IP

You do **not** need Kubernetes.

---

## Step 1 — Create the VM

1. Sign in to the [Google Cloud Console](https://console.cloud.google.com/).
2. Open **Compute Engine**.
3. Click **VM instances**.
4. Click **Create instance**.
5. Give the VM a name, such as `openclaw-bridge`.
6. Choose an Ubuntu image, such as **Ubuntu 22.04 LTS** or **24.04 LTS**.
7. Pick a small machine size.
8. Make sure the VM will have an **external IP**.
9. Click **Create**.

When the VM is ready, copy its external IP for now.

---

## Step 2 — Make the external IP static

If the VM IP changes, your domain record will break.
So reserve a **static external IP**.

### Where to click in Google Cloud

1. In the Google Cloud Console, open **VPC network**.
2. Click **IP addresses**.
3. Click **Reserve external static IP address**.
4. Give it a name, such as `openclaw-bridge-ip`.
5. Keep the region the same as your VM.
6. Click **Reserve**.
7. Go back to the VM instance.
8. Edit the VM’s network interface if needed and attach the reserved static IP.

Now the VM has a fixed public IP.
Keep that IP handy.

---

## Step 3 — Create the DNS record in Cloud DNS

This step tells the internet that your domain should open your VM.

### Where to click in Cloud DNS

1. Open **Network services**.
2. Click **Cloud DNS**.
3. If your domain is not added yet, click **Create zone**.
4. Enter your domain name, for example `yourdomain.com`.
5. Choose a **public zone**.
6. Click **Create**.
7. Open the zone.
8. Click **Add record set**.

### How to create the A record

In the record form, fill in these fields:

| Field | What to enter |
|---|---|
| DNS name | `bridge` |
| Resource record type | `A` |
| IPv4 address | the static external IP |
| TTL | leave the default value |

This creates:

```text
bridge.yourdomain.com -> <static-ip>
```

### Finish the record

1. Check the values again.
2. Click **Create** or **Save**.
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
http://openclaw-gateway.tailnet:18789
```

Or, if you only have an IP address:

```text
http://100.x.x.x:18789
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
OPENCLAW_GATEWAY_URL=https://your-host.your-tailnet.ts.net
OPENCLAW_GATEWAY_TOKEN=replace-with-gateway-auth-token
OPENCLAW_SESSION_KEY=agent:main:main
BRIDGE_API_KEY=replace-with-a-long-random-secret
REQUEST_TIMEOUT_MS=30000
MAX_BODY_BYTES=1048576
EOF
```

### What each line means

Every setting is explained once in
**[Configure `.env`]({{ '/step-by-step.html' | relative_url }}#env-setup)** — what it does,
which are required, and why `BRIDGE_API_KEY` and `OPENCLAW_GATEWAY_TOKEN` are two
different secrets rather than one.

Three things are specific to this VM setup:

| Setting | Why it differs here |
|---|---|
| `ADDR=127.0.0.1:8080` | The bridge listens **only on localhost**, because Caddy sits in front and proxies to it. A cloud container would use `:8080` instead |
| `TS_AUTHKEY` | Not needed. Tailscale runs on the VM itself, authenticated in Step 4 — there is no sidecar container to authorise |
| Where the file lives | This `.env` is on the **server**, at `/opt/openclaw-bridge/.env`, not the one in your local checkout |

Generate the two secrets rather than inventing them:

```bash
openssl rand -hex 32   # BRIDGE_API_KEY
# OPENCLAW_GATEWAY_TOKEN is not generated: copy gateway.auth.token
# from ~/.openclaw/openclaw.json
```

Leave `BRIDGE_API_KEY` unset and the bridge starts anyway, logs a warning, and accepts
unauthenticated requests from anyone who finds the URL.

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
  -H "api_key: $BRIDGE_API_KEY" \
  --data '{"action":"ask","message":"Confirm you are reachable."}'
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
- [ ] Static external IP reserved
- [ ] Cloud DNS record points to the static IP
- [ ] Docker installed
- [ ] Caddy installed
- [ ] Tailscale connected
- [ ] OpenClaw reachable over the tailnet
- [ ] Bridge container running
- [ ] HTTPS works on `https://bridge.yourdomain.com`
- [ ] Custom GPT action imported

---

## Free tier deployment with Cloud Run

This is the **free** path on Google Cloud. It does not use a virtual machine, a domain name, or Caddy.
Instead it uses **Cloud Run**, which runs the bridge with Tailscale bundled into the image and hands you an HTTPS URL for nothing.

If you want the always-on virtual machine instead, that is the [first half of this page](#one-sentence-explanation).
For how Cloud Run compares to other free platforms, see [Free deployment options]({{ '/deployment/free-tier.html' | relative_url }}).
### What "free" means here

Google's free tier includes, per month:

- 2 million requests
- 180,000 vCPU-seconds
- 360,000 GiB-seconds of memory
- 1 GB of outbound data transfer from North America

A bridge that handles a few hundred ChatGPT requests a month stays far inside these limits.

Two honest caveats:

- **A billing account is still required**, even though you are not charged inside the free limits. You need a credit card on file.
- **Free allowances can change.** Check the [Cloud Run pricing page](https://cloud.google.com/run/pricing) before you rely on it.

### What goes where

| Part | What it does |
|---|---|
| Cloud Run service | runs the bridge |
| Cloud Run URL | gives the bridge a free public HTTPS address |
| Tailscale (userspace) | lets the bridge reach private OpenClaw |
| Secret Manager | stores the Tailscale key and the webhook secret |
| ChatGPT Action | calls the public bridge URL |

### Simple connection map

```text
ChatGPT -> https://<service>-<hash>.run.app -> bridge on Cloud Run -> Tailscale -> OpenClaw
```

### Before you start

You need:

- a Google Cloud project with billing enabled
- the `gcloud` command installed and signed in
- access to the OpenClaw tailnet address or Tailscale IP
- an account on [Tailscale](https://tailscale.com)

You do **not** need Kubernetes, a domain name, or a server.

---

### Step 1 — Get a Tailscale auth key

Cloud Run starts and stops your container automatically, so the bridge must be able to join the tailnet **without anyone typing a password**.
That is what an auth key is for.

1. Open the [Tailscale admin console](https://login.tailscale.com/admin/settings/keys).
2. Click **Generate auth key**.
3. Turn on **Ephemeral**.
4. Turn on **Reusable**.
5. Click **Generate key** and copy the value. It starts with `tskey-`.

#### Why ephemeral and reusable?

| Setting | Why |
|---|---|
| **Ephemeral** | When Cloud Run stops the container, the device removes itself from your tailnet. Without this, your device list fills with dead entries |
| **Reusable** | Cloud Run may start the container many times. A single-use key would work once and then fail |

Copy the key somewhere safe for the next step. You cannot view it again later.

---

### Step 2 — Store your secrets

Never put the auth key or the webhook secret directly in the deploy command, because they end up in your shell history and in the Cloud Run configuration in plain text.

Set your project first:

```bash
gcloud config set project YOUR_PROJECT_ID
```

Turn on the services this guide needs:

```bash
gcloud services enable run.googleapis.com \
  artifactregistry.googleapis.com \
  secretmanager.googleapis.com
```

**Fill in `.env` first** — it is the single source of truth for every value below.
See [Configure `.env`]({{ '/step-by-step.html' | relative_url }}#env-setup) for what each
setting means and how to generate `BRIDGE_API_KEY`.

```bash
set -a; . ./.env; set +a

: "${OPENCLAW_GATEWAY_URL:?set it in .env}"
: "${OPENCLAW_GATEWAY_TOKEN:?set it in .env}"
: "${BRIDGE_API_KEY:?set it in .env}"
: "${TS_AUTHKEY:?set it in .env}"
```

Now store the secrets, taking two of them straight from `.env`:

```bash
printf '%s' "$OPENCLAW_GATEWAY_TOKEN" \
  | gcloud secrets create gateway-token --data-file=-

printf '%s' "$BRIDGE_API_KEY" \
  | gcloud secrets create bridge-api-key --data-file=-

printf '%s' "$TS_AUTHKEY" \
  | gcloud secrets create tailscale-authkey --data-file=-
```

Let Cloud Run read them:

```bash
PROJECT_NUMBER="$(gcloud projects describe "$(gcloud config get-value project)" --format='value(projectNumber)')"
SERVICE_ACCOUNT="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"

for SECRET in tailscale-authkey gateway-token bridge-api-key; do
  gcloud secrets add-iam-policy-binding "$SECRET" \
    --member="serviceAccount:${SERVICE_ACCOUNT}" \
    --role="roles/secretmanager.secretAccessor"
done
```

---

### Step 3 — Create the startup script

Cloud Run runs **one container**. That container has to start Tailscale first, then the bridge.

In the project folder, create a file named `start.sh`:

```bash
cat > start.sh <<'EOF'
#!/bin/sh
set -e

# Cloud Run has no /dev/net/tun, so Tailscale must run in userspace mode.
# State is kept in memory because the container filesystem does not survive.
/app/tailscaled \
  --tun=userspace-networking \
  --socket=/tmp/tailscaled.sock \
  --state=mem: \
  --socks5-server=localhost:1055 \
  --outbound-http-proxy-listen=localhost:1055 &

# Join the tailnet. This must finish before the bridge starts serving.
/app/tailscale --socket=/tmp/tailscaled.sock up \
  --auth-key="${TAILSCALE_AUTHKEY}" \
  --hostname="${TS_HOSTNAME:-openclaw-bridge}"

# Send the bridge's outbound calls through the tailnet.
export HTTP_PROXY="http://localhost:1055"
export HTTPS_PROXY="http://localhost:1055"
export NO_PROXY="127.0.0.1,localhost"

exec /app/openclaw-chatgpt-bridge
EOF
chmod +x start.sh
```

#### What the important lines do

| Line | Why it matters |
|---|---|
| `--tun=userspace-networking` | Cloud Run cannot create a VPN network device. This mode works without one |
| `--state=mem:` | Nothing is written to disk, and the device removes itself when the container stops |
| `--outbound-http-proxy-listen` | Opens a local HTTP proxy on port 1055 that reaches the tailnet |
| `export HTTP_PROXY=...` | Tells the bridge to send its OpenClaw calls through that proxy |
| `exec` | The bridge becomes the main process, so Cloud Run's stop signal reaches it |

---

### Step 4 — Create the Dockerfile

The repository's normal `Dockerfile` builds only the bridge.
For Cloud Run you need one image containing **both** the bridge and Tailscale.

Create a file named `Dockerfile.cloudrun`:

```dockerfile
FROM golang:1.22-alpine AS build

WORKDIR /src

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

COPY go.mod ./
COPY *.go ./

RUN go test ./...
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /out/openclaw-chatgpt-bridge .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 appuser

COPY --from=build /out/openclaw-chatgpt-bridge /app/openclaw-chatgpt-bridge
COPY --from=docker.io/tailscale/tailscale:stable /usr/local/bin/tailscaled /app/tailscaled
COPY --from=docker.io/tailscale/tailscale:stable /usr/local/bin/tailscale /app/tailscale
COPY start.sh /app/start.sh

RUN chmod +x /app/start.sh

USER 10001
EXPOSE 8080

CMD ["/app/start.sh"]
```

This copies the Tailscale programs out of the official Tailscale image, so you do not have to install anything by hand.
It runs as a non-root user, which is why the script puts the Tailscale socket in `/tmp`.

---

### Step 5 — Build and upload the image

Create a place to store the image:

```bash
gcloud artifacts repositories create openclaw \
  --repository-format=docker \
  --location=us-central1
```

The build runs in the cloud, so you do not need Docker on your own computer.

`gcloud builds submit --tag` always builds the file named exactly `Dockerfile`, and there is no flag to point it at another name.
Since this repository already has a `Dockerfile` for normal deployments, tell Cloud Build which one to use with a small build file.

Create `cloudbuild.yaml`:

```bash
cat > cloudbuild.yaml <<'EOF'
steps:
  - name: gcr.io/cloud-builders/docker
    args: ["build", "-f", "Dockerfile.cloudrun", "-t", "$_IMAGE", "."]
images:
  - "$_IMAGE"
EOF
```

Then build:

```bash
REGION=us-central1
PROJECT_ID="$(gcloud config get-value project)"
IMAGE="${REGION}-docker.pkg.dev/${PROJECT_ID}/openclaw/bridge:latest"

gcloud builds submit --config cloudbuild.yaml --substitutions _IMAGE="$IMAGE" .
```

This takes a few minutes the first time.

If you would rather keep only one Dockerfile, you can instead rename `Dockerfile.cloudrun` to `Dockerfile` and run `gcloud builds submit --tag "$IMAGE" .` — but then the plain Docker and Kubernetes instructions in `CONTRIBUTING.md` will start Tailscale too, which is usually not what you want locally.

---

### Step 6 — Deploy the bridge

```bash
gcloud run deploy openclaw-bridge \
  --image "$IMAGE" \
  --region "$REGION" \
  --port 8080 \
  --allow-unauthenticated \
  --min-instances 0 \
  --max-instances 3 \
  --memory 256Mi \
  --set-env-vars 'ADDR=:8080' \
  --set-env-vars "OPENCLAW_GATEWAY_URL=$OPENCLAW_GATEWAY_URL" \
  --set-env-vars "OPENCLAW_SESSION_KEY=$OPENCLAW_SESSION_KEY" \
  --set-env-vars "REQUEST_TIMEOUT_MS=$REQUEST_TIMEOUT_MS" \
  --set-env-vars 'TAILSCALE_ENABLED=true' \
  --set-env-vars 'TAILSCALE_PROXY_ADDR=127.0.0.1:1055' \
  --set-secrets 'TAILSCALE_AUTHKEY=tailscale-authkey:latest' \
  --set-secrets 'OPENCLAW_GATEWAY_TOKEN=gateway-token:latest' \
  --set-secrets 'BRIDGE_API_KEY=bridge-api-key:latest'
```

Replace the `OPENCLAW_GATEWAY_URL` value with your real OpenClaw Gateway address.

If OpenClaw sits behind `tailscale serve`, the address is **https with no port**. Check it with `tailscale serve status` on the OpenClaw machine, and use that form instead:

```text
OPENCLAW_GATEWAY_URL=https://your-host.your-tailnet.ts.net
```

The `Dockerfile.cloudrun` above installs `ca-certificates` so the bridge can verify that certificate.

When it finishes, `gcloud` prints your service URL. It looks like:

```text
https://openclaw-bridge-abc123-uc.a.run.app
```

That is the public HTTPS address for your ChatGPT Action. There is nothing else to set up.

#### Three settings that trip people up

**`--allow-unauthenticated` is required.**
ChatGPT cannot sign in to Google. Without this flag every request comes back as `403`.
The bridge is still protected, because OpenClaw only accepts calls carrying the webhook secret.

**Set `ADDR`, not `PORT`.**
The bridge binds the address in `ADDR`. It reads `PORT` but does not use it for binding.
Cloud Run sets `PORT` automatically, and that alone will not move the bridge — so keep `--port 8080` and `ADDR=:8080` matched.

**`OPENCLAW_GATEWAY_URL` must not be `localhost`.**
Go never sends requests for `localhost` or `127.0.0.1` through `HTTP_PROXY`, no matter how the proxy is configured.
If you point the bridge at a loopback address it will quietly skip Tailscale and fail to reach OpenClaw.
Always use the tailnet hostname or the `100.x.x.x` address.

---

### Step 7 — Test the bridge

#### Check the health page

Open this in a browser, using your own service URL:

```text
https://openclaw-bridge-abc123-uc.a.run.app/healthz
```

You should see a small JSON response.

#### Check that Tailscale is connected

```text
https://openclaw-bridge-abc123-uc.a.run.app/readyz
```

This one is the real test. It returns `ok` only when the Tailscale proxy is answering.
If it returns `tailscale proxy not ready`, your auth key is wrong or expired.

#### Test the main API endpoint

```bash
curl -X POST https://openclaw-bridge-abc123-uc.a.run.app/v1/openclaw \
  -H 'content-type: application/json' \
  -H "api_key: $BRIDGE_API_KEY" \
  --data '{"action":"ask","message":"Confirm you are reachable."}'
```

If it works, the bridge is ready.

#### If something fails

Read the logs:

```bash
gcloud run services logs read openclaw-bridge --region "$REGION" --limit 50
```

Each bridge request logs one line containing `request_id`, `action`, `upstream_status`, and `duration_ms`.

---

### Step 8 — Add the action in Custom GPT

In ChatGPT:

1. Open **Explore GPTs**
2. Create a new GPT, or edit an existing one
3. Open **Actions**
4. Click **Create new action**
5. Import `openapi/openclaw-bridge.openapi.yaml`
6. Change the `servers:` URL at the top of the schema to your Cloud Run URL
7. Save the GPT

#### What must be public?

Only the **bridge URL**:

```text
https://openclaw-bridge-abc123-uc.a.run.app/v1/openclaw
```

OpenClaw itself stays private behind Tailscale.

---

### About the cold start

Cloud Run stops your container when nobody is using it. That is what keeps it free.

The next request has to start the container again, which means starting Tailscale and joining the tailnet before the bridge answers. Expect the **first request after a quiet period to take several seconds**. Requests after that are fast.

If that first slow call ever causes a ChatGPT Action to time out, you have two options:

- ask the same thing again, since the second call is fast, or
- set `--min-instances 1`, which keeps one copy always running. This **leaves the free tier** and costs a few dollars a month.

---

### Final checklist

- [ ] Google Cloud project with billing enabled
- [ ] Tailscale ephemeral, reusable auth key created
- [ ] Both secrets stored in Secret Manager
- [ ] `start.sh` and `Dockerfile.cloudrun` created
- [ ] Image built and pushed
- [ ] Service deployed with `--allow-unauthenticated`
- [ ] `/healthz` returns JSON
- [ ] `/readyz` reports ready, proving Tailscale connected
- [ ] `OPENCLAW_GATEWAY_URL` uses a tailnet address, not localhost
- [ ] Custom GPT action imported and pointed at the Cloud Run URL

### Short summary

If you only remember one thing, remember this:

- **ChatGPT talks to the free Cloud Run URL**
- **the bridge talks to OpenClaw through Tailscale in userspace mode**
- **OpenClaw does not need to be public, and you do not need a server or a domain**
