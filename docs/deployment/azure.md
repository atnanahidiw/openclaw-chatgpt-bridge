---
title: Azure deployment
---

# Azure deployment for the OpenClaw ChatGPT Bridge

This guide is written for a **non-technical person** or an **intern**.

<div class="tested" markdown="1">
**Tested: the free tier Container Apps path was deployed end to end, successfully.**

Jump to it: [Free tier deployment with Azure Container Apps](#free-tier-deployment-with-azure-container-apps).
</div>

There are **two ways** to run the bridge on Azure. Pick one:

| | Virtual machine | [Free tier: Container Apps](#free-tier-deployment-with-azure-container-apps) |
|---|---|---|
| Cost | a few dollars a month | nothing, within the free limits |
| Domain name | you buy one | not needed |
| HTTPS | Caddy sets it up | included, automatic |
| Runs when idle | always on | sleeps, wakes on demand |

**→ Want the free option? [Skip to Free tier deployment with Azure Container Apps](#free-tier-deployment-with-azure-container-apps).**

The virtual machine path below uses:

- **Azure Virtual Machines** for the server
- **Azure DNS** for the domain name
- **Caddy** for HTTPS
- **Tailscale** so the bridge can reach OpenClaw privately

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
+---------+   HTTPS    +-----------------+   Tailscale   +----------+
| ChatGPT | ---------> | bridge on VM    | ------------> | OpenClaw |
+---------+            | + Caddy for TLS |               +----------+
                       +-----------------+

   public                 reachable from                 never leaves
  internet                 the internet                  your tailnet
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

Now write your `.env` into that folder, at `/opt/openclaw-bridge/.env`.

**[Configure `.env`]({{ '/step-by-step.html' | relative_url }}#env-setup)** lists every
setting, says which ones you need, and shows how to generate the secrets. Follow it there.

### What is different on a server

Three things change once the bridge lives on a VM rather than in a container:

| Setting | Why it differs here |
|---|---|
| `ADDR=127.0.0.1:8080` | Caddy sits in front, so the bridge only needs to listen on localhost. A cloud container uses `:8080` instead |
| `TS_AUTHKEY` | You do not need it. Tailscale runs on the VM itself and you logged it in back at Step 4 |
| Where the file lives | This `.env` belongs on the **server**, at `/opt/openclaw-bridge/.env`. It is not the one in your local checkout |

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

## Step 10 — Connect it to ChatGPT

Your bridge is running. The last piece is telling ChatGPT about it.

There are four things to do: import the schema, point it at your bridge URL, add the API
key, and write instructions the model will follow. They are all on one page.

**[Custom GPT setup]({{ '/custom-gpt.html' | relative_url }})**

One thing to carry across from this guide: only the bridge URL has to be reachable from the
internet. OpenClaw stays private, and the Gateway token never leaves the bridge.

---

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

---

## Free tier deployment with Azure Container Apps <span class="tested-tag">Tested</span> {#free-tier-deployment-with-azure-container-apps}

This is the **free** path on Azure. It does not use a virtual machine, a domain name, or Caddy.
Instead it uses **Azure Container Apps**, which runs Tailscale as a second container beside the bridge and hands you an HTTPS URL for nothing.

If you want the always-on virtual machine instead, that is the [first half of this page](#one-sentence-explanation).
For how Azure compares to other free platforms, see [Free deployment options]({{ '/deployment/free-tier.html' | relative_url }}).

### What "free" means here

Azure gives every subscription these amounts free, every calendar month, with no expiry:

- 2 million HTTP requests
- 180,000 vCPU-seconds
- 360,000 GiB-seconds

Two things make this a good fit for the bridge:

- **Health probe requests are not billable**, so `/healthz` and `/readyz` checks never eat the request allowance.
- **Scaling to zero costs nothing.** When nobody is using the bridge, you are charged for no compute at all.

Two honest caveats:

- **Stay on the Consumption plan.** A Dedicated workload profile adds a management fee, and bringing your own virtual network can add charges.
- **Free allowances can change.** Check the [Container Apps pricing page](https://azure.microsoft.com/en-us/pricing/details/container-apps/) before you rely on it.

### Why this is simpler than Cloud Run

Cloud Run runs one container, so the [Cloud Run section]({{ '/deployment/gcp.html' | relative_url }}#free-tier-deployment-with-cloud-run) has to build a special image with Tailscale copied inside it.

Container Apps runs **several containers side by side**, sharing one network. So Tailscale is just a second container, exactly like the project's Helm chart already does on Kubernetes. You keep the repository's normal `Dockerfile` and write no startup script.

### What goes where

| Part | What it does |
|---|---|
| Container Apps environment | the space your app runs in |
| `bridge` container | runs the bridge |
| `tailscale` container | joins the tailnet and offers a local proxy |
| Container Apps URL | gives the bridge a free public HTTPS address |
| Azure Container Registry | stores the image you build |
| ChatGPT Action | calls the public bridge URL |

### Simple connection map

```text
+---------+  HTTPS   +-------------------------+  Tailscale   +----------+
| ChatGPT | -------> | Container App           | -----------> | OpenClaw |
+---------+          |                         |              +----------+
                     | bridge  <-->  tailscale |
                     |      localhost:1055     |
                     +-------------------------+

 public URL           one replica, two containers        stays private
```

Both containers share `localhost`, which is what makes the proxy work.

### Before you start

You need:

- an Azure subscription
- the `az` command installed and signed in
- access to the OpenClaw tailnet address or Tailscale IP
- an account on [Tailscale](https://tailscale.com)

You do **not** need Kubernetes, a domain name, or a server.

---

### Step 1 — Get a Tailscale auth key

Your container joins the tailnet by itself, so it needs a key of its own. It takes a minute
to make, and two of the switches on it matter.

**[Getting a Tailscale auth key]({{ '/step-by-step.html' | relative_url }}#tailscale-auth-key)**

Save it in `.env` as `TS_AUTHKEY`, then come back here.

---

### Step 2 — Prepare Azure

Install the Container Apps extension and register the provider:

```bash
az extension add --name containerapp --upgrade
az provider register --namespace Microsoft.App
```

Create a resource group and an environment:

```bash
RESOURCE_GROUP=openclaw-bridge
LOCATION=eastus

az group create --name "$RESOURCE_GROUP" --location "$LOCATION"

az containerapp env create \
  --name openclaw-env \
  --resource-group "$RESOURCE_GROUP" \
  --location "$LOCATION"
```

The environment takes a couple of minutes to create.

---

### Step 3 — Build and publish the image

Container Apps pulls your image from a registry, so you need one. There are two ways to do
this, and they are both fine. Pick based on whether you want a private image or a free one.

| | **Option A — GitHub Container Registry** | **Option B — Azure Container Registry** |
|---|---|---|
| Cost | free | **~$5/month** (Basic SKU, no free tier) |
| Image visibility | public | private |
| Container Apps credentials | none needed | registry secret in the YAML |
| Who builds it | your machine | Azure, in the cloud |
| Needs a local container CLI | yes | no |
| Handles `amd64` for you | no, you pass `--platform` | yes, automatic |

> **Note.** Option B is the simpler path and the one to choose if you want the image kept
> private or you have no container tooling locally. It just is not free, so if you came to
> this page for a zero-cost deployment, use Option A. The rest of the guide works with
> either. Step 4 shows the one line that differs.

Do **one** of the two sections below, then continue to Step 4.

---

#### Option A — GitHub Container Registry (free)

#### Two important constraints

**Container Apps only runs `linux/amd64` images.** If you are on an Apple Silicon Mac or
any other ARM machine, a plain local build produces an `arm64` image that Container Apps
refuses to start. The repository `Dockerfile` handles this: it pins the build stage to your
own machine and cross-compiles, because Go cross-compiles natively and emulating the whole
toolchain would be many times slower.

```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} go build ...
```

**Any Docker-compatible CLI works.** The commands below use `docker`; substitute `nerdctl`
or `podman` unchanged if that is what you have.

#### Build

```bash
docker build --platform linux/amd64 \
  -t ghcr.io/YOUR_GITHUB_USER/openclaw-chatgpt-bridge:v1 \
  --build-arg VERSION=0.2.0 \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  --build-arg BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)" .
```

Confirm the architecture before pushing. This is the single most common cause of a
revision that deploys but never starts:

```bash
docker image inspect ghcr.io/YOUR_GITHUB_USER/openclaw-chatgpt-bridge:v1 \
  --format '{{.Architecture}} {{.Os}}'
```

It must print `amd64 linux`.

Avoid the tag `latest`. A fixed tag like `v1` makes it obvious which build is running.

#### Push

Log in with a GitHub token that has the `write:packages` scope:

```bash
docker login ghcr.io -u YOUR_GITHUB_USER
docker push ghcr.io/YOUR_GITHUB_USER/openclaw-chatgpt-bridge:v1
```

#### Make the package public

**New GitHub packages are private by default**, and Container Apps has no credentials, so
the deployment will fail with an image-pull error until you change this.

1. Open `https://github.com/users/YOUR_GITHUB_USER/packages/container/openclaw-chatgpt-bridge/settings`
2. Scroll to **Danger Zone**
3. Click **Change visibility** → **Public**

Verify it worked by doing exactly what Azure will do, and fetch the manifest anonymously:

```bash
REPO=YOUR_GITHUB_USER/openclaw-chatgpt-bridge
TOKEN=$(curl -s "https://ghcr.io/token?scope=repository:${REPO}:pull&service=ghcr.io" \
  | sed -E 's/.*"token":"([^"]+)".*/\1/')
curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $TOKEN" \
  -H "Accept: application/vnd.oci.image.manifest.v1+json" \
  "https://ghcr.io/v2/${REPO}/manifests/v1"
```

`200` means Azure can pull it. `403` means it is still private.

---

#### Option B — Azure Container Registry (~$5/month, private)

This is the original path. It costs money, but it needs no container tooling on your
machine and produces an `amd64` image without you thinking about architecture.

Create a registry. The name must be globally unique and lowercase:

```bash
ACR_NAME=openclawbridge$RANDOM

az acr create \
  --resource-group "$RESOURCE_GROUP" \
  --name "$ACR_NAME" \
  --sku Basic \
  --admin-enabled true
```

Build the image in the cloud, so you do not need Docker on your own computer.
This uses the repository's **normal** `Dockerfile`. Nothing special is needed:

```bash
az acr build \
  --registry "$ACR_NAME" \
  --image bridge:v1 \
  --file Dockerfile .
```

Avoid the tag `latest`. A fixed tag like `v1` makes it obvious which build is running.

Because the image stays private, Container Apps needs credentials. Collect them for
Step 4:

```bash
ACR_SERVER="$(az acr show --name "$ACR_NAME" --query loginServer -o tsv)"
ACR_PASSWORD="$(az acr credential show --name "$ACR_NAME" --query 'passwords[0].value' -o tsv)"
IMAGE="${ACR_SERVER}/bridge:v1"
```

---

### Step 4 — Write the app definition

Sidecars cannot be described with command line flags alone, so the app is defined in a YAML file.

**Fill in `.env` first.** It is the single source of truth for every value below.
See [Configure `.env`]({{ '/step-by-step.html' | relative_url }}#env-setup) for what each
setting means and how to generate `BRIDGE_API_KEY`.

```bash
set -a; . ./.env; set +a

: "${OPENCLAW_GATEWAY_URL:?set it in .env}"
: "${OPENCLAW_GATEWAY_TOKEN:?set it in .env}"
: "${BRIDGE_API_KEY:?set it in .env}"
: "${TS_AUTHKEY:?set it in .env}"
```

Then collect the two values that are specific to this deployment:

```bash
ENV_ID="$(az containerapp env show --name openclaw-env --resource-group "$RESOURCE_GROUP" --query id -o tsv)"

# Option A (GHCR) — set IMAGE yourself.
IMAGE="ghcr.io/YOUR_GITHUB_USER/openclaw-chatgpt-bridge:v1"
# Option B (ACR) — IMAGE and ACR_* were already set at the end of Step 3.
```

Now write the file. Both versions below are **complete**. Copy the one matching the option
you chose in Step 3, rather than assembling pieces. They differ only in the `registries`
block and one extra secret.

<details class="use-case" markdown="1">
<summary><strong>Full <code>app.yaml</code> — Option A: public image (GHCR, free)</strong></summary>

No registry credentials, because Container Apps pulls a public image anonymously.

```bash
cat > app.yaml <<EOF
location: ${LOCATION}
type: Microsoft.App/containerApps
name: openclaw-bridge
properties:
  managedEnvironmentId: ${ENV_ID}
  configuration:
    activeRevisionsMode: Single
    ingress:
      external: true
      targetPort: 8080
      transport: auto
      allowInsecure: false
    secrets:
      - name: tailscale-authkey
        value: ${TS_AUTHKEY}
      - name: gateway-token
        value: ${OPENCLAW_GATEWAY_TOKEN}
      - name: bridge-api-key
        value: ${BRIDGE_API_KEY}
  template:
    containers:
      - name: bridge
        image: ${IMAGE}
        resources:
          cpu: 0.25
          memory: 0.5Gi
        env:
          - name: ADDR
            value: ":8080"
          - name: OPENCLAW_GATEWAY_URL
            value: "${OPENCLAW_GATEWAY_URL}"
          - name: OPENCLAW_SESSION_PREFIX
            value: "${OPENCLAW_SESSION_PREFIX}"
          - name: REQUEST_TIMEOUT_MS
            value: "30000"
          - name: TAILSCALE_ENABLED
            value: "true"
          - name: TAILSCALE_PROXY_ADDR
            value: "127.0.0.1:1055"
          - name: HTTP_PROXY
            value: "http://localhost:1055"
          - name: HTTPS_PROXY
            value: "http://localhost:1055"
          - name: NO_PROXY
            value: "127.0.0.1,localhost"
          - name: OPENCLAW_GATEWAY_TOKEN
            secretRef: gateway-token
          - name: BRIDGE_API_KEY
            secretRef: bridge-api-key
      - name: tailscale
        image: docker.io/tailscale/tailscale:stable
        resources:
          cpu: 0.25
          memory: 0.5Gi
        env:
          - name: TS_AUTHKEY
            secretRef: tailscale-authkey
          - name: TS_HOSTNAME
            value: "openclaw-bridge"
          - name: TS_USERSPACE
            value: "true"
          - name: TS_KUBE_SECRET
            value: ""
          - name: TS_STATE_DIR
            value: "/tmp/tailscale"
          - name: TS_SOCKS5_SERVER
            value: "localhost:1055"
          - name: TS_OUTBOUND_HTTP_PROXY_LISTEN
            value: "localhost:1055"
          - name: TS_ACCEPT_DNS
            value: "false"
    scale:
      minReplicas: 0
      maxReplicas: 3
EOF
```

</details>

<details class="use-case" markdown="1">
<summary><strong>Full <code>app.yaml</code> — Option B: private registry (ACR)</strong></summary>

Identical to Option A apart from the third secret and the `registries` block, which let
Container Apps authenticate to the registry. The same shape works for a private GHCR image:
use `ghcr.io` as the server, your GitHub username, and a token with `read:packages`.

```bash
cat > app.yaml <<EOF
location: ${LOCATION}
type: Microsoft.App/containerApps
name: openclaw-bridge
properties:
  managedEnvironmentId: ${ENV_ID}
  configuration:
    activeRevisionsMode: Single
    ingress:
      external: true
      targetPort: 8080
      transport: auto
      allowInsecure: false
    secrets:
      - name: tailscale-authkey
        value: ${TS_AUTHKEY}
      - name: gateway-token
        value: ${OPENCLAW_GATEWAY_TOKEN}
      - name: bridge-api-key
        value: ${BRIDGE_API_KEY}
      - name: registry-password
        value: ${ACR_PASSWORD}
    registries:
      - server: ${ACR_SERVER}
        username: ${ACR_NAME}
        passwordSecretRef: registry-password
  template:
    containers:
      - name: bridge
        image: ${IMAGE}
        resources:
          cpu: 0.25
          memory: 0.5Gi
        env:
          - name: ADDR
            value: ":8080"
          - name: OPENCLAW_GATEWAY_URL
            value: "${OPENCLAW_GATEWAY_URL}"
          - name: OPENCLAW_SESSION_PREFIX
            value: "${OPENCLAW_SESSION_PREFIX}"
          - name: REQUEST_TIMEOUT_MS
            value: "30000"
          - name: TAILSCALE_ENABLED
            value: "true"
          - name: TAILSCALE_PROXY_ADDR
            value: "127.0.0.1:1055"
          - name: HTTP_PROXY
            value: "http://localhost:1055"
          - name: HTTPS_PROXY
            value: "http://localhost:1055"
          - name: NO_PROXY
            value: "127.0.0.1,localhost"
          - name: OPENCLAW_GATEWAY_TOKEN
            secretRef: gateway-token
          - name: BRIDGE_API_KEY
            secretRef: bridge-api-key
      - name: tailscale
        image: docker.io/tailscale/tailscale:stable
        resources:
          cpu: 0.25
          memory: 0.5Gi
        env:
          - name: TS_AUTHKEY
            secretRef: tailscale-authkey
          - name: TS_HOSTNAME
            value: "openclaw-bridge"
          - name: TS_USERSPACE
            value: "true"
          - name: TS_KUBE_SECRET
            value: ""
          - name: TS_STATE_DIR
            value: "/tmp/tailscale"
          - name: TS_SOCKS5_SERVER
            value: "localhost:1055"
          - name: TS_OUTBOUND_HTTP_PROXY_LISTEN
            value: "localhost:1055"
          - name: TS_ACCEPT_DNS
            value: "false"
    scale:
      minReplicas: 0
      maxReplicas: 3
EOF
```

</details>

### The settings that matter

| Setting | Why |
|---|---|
| `TS_USERSPACE: "true"` | Container Apps does not allow privileged containers, so Tailscale must run in userspace mode |
| `TS_SOCKS5_SERVER` and `TS_OUTBOUND_HTTP_PROXY_LISTEN` | Open the local proxy on port 1055. Use these variables rather than passing the flags yourself. See the warning below |
| `TS_KUBE_SECRET: ""` | The Tailscale image stores state in a Kubernetes secret by default. This is not Kubernetes, so turn that off |
| `HTTP_PROXY` on the bridge | Tells the bridge to send its OpenClaw calls through the Tailscale container |
| `cpu` and `memory` | On the Consumption plan the totals across **all** containers must be an allowed pair. Two containers at `0.25` / `0.5Gi` add up to `0.5` / `1.0Gi`, which is allowed |
| `minReplicas: 0` | Scales to zero when idle, which is what keeps it free |

#### A warning about Tailscale flags

The Tailscale image has two different "extra flags" variables, and mixing them up is a common mistake:

- `TS_EXTRA_ARGS` passes flags to **`tailscale up`**
- `TS_TAILSCALED_EXTRA_ARGS` passes flags to **`tailscaled`**

`--socks5-server` and `--outbound-http-proxy-listen` are `tailscaled` flags. Putting them in `TS_EXTRA_ARGS` sends them to the wrong program. This guide avoids the problem entirely by using the dedicated `TS_SOCKS5_SERVER` and `TS_OUTBOUND_HTTP_PROXY_LISTEN` variables.

---

### Step 5 — Deploy

```bash
az containerapp create \
  --name openclaw-bridge \
  --resource-group "$RESOURCE_GROUP" \
  --yaml app.yaml
```

Get your public URL:

```bash
az containerapp show \
  --name openclaw-bridge \
  --resource-group "$RESOURCE_GROUP" \
  --query 'properties.configuration.ingress.fqdn' -o tsv
```

It looks like:

```text
openclaw-bridge.politesky-1234abcd.eastus.azurecontainerapps.io
```

Once the app is running, delete `app.yaml`. It still contains your secrets in plain text:

```bash
rm app.yaml
```

#### Two settings that trip people up

**`external: true` is required.**
ChatGPT is on the public internet. With `external: false` the app is only reachable from inside the environment, and every ChatGPT call fails.
The bridge is still protected, because OpenClaw only accepts calls carrying the webhook secret.

**`OPENCLAW_GATEWAY_URL` must not be `localhost`.**
Go never sends requests for `localhost` or `127.0.0.1` through `HTTP_PROXY`, no matter how the proxy is configured.
If you point the bridge at a loopback address it will quietly skip Tailscale and fail to reach OpenClaw.
Always use the tailnet hostname or the `100.x.x.x` address.

---

### Step 6 — Test the bridge

Save your URL first:

```bash
BRIDGE_URL="https://$(az containerapp show --name openclaw-bridge --resource-group "$RESOURCE_GROUP" --query 'properties.configuration.ingress.fqdn' -o tsv)"
```

#### Check the health page

```bash
curl "$BRIDGE_URL/healthz"
```

You should see a small JSON response.

#### Check that Tailscale is connected

```bash
curl "$BRIDGE_URL/readyz"
```

This one is the real test. It returns `ok` only when the Tailscale container is answering on port 1055.
If it says `tailscale proxy not ready`, your auth key is wrong or expired.

#### Test the main API endpoint

```bash
curl -X POST "$BRIDGE_URL/v1/openclaw" \
  -H 'content-type: application/json' \
  -H "api_key: $BRIDGE_API_KEY" \
  --data '{"action":"ask","message":"Confirm you are reachable."}'
```

If it works, the bridge is ready.

#### If something fails

Read the logs from each container separately:

```bash
az containerapp logs show --name openclaw-bridge --resource-group "$RESOURCE_GROUP" --container bridge --tail 50
az containerapp logs show --name openclaw-bridge --resource-group "$RESOURCE_GROUP" --container tailscale --tail 50
```

The `bridge` container logs one line per request with `request_id`, `action`, `upstream_status`, and `duration_ms`.
The `tailscale` container tells you whether it joined the tailnet.

---

### Step 7 — Connect it to ChatGPT

Your bridge is running. The last piece is telling ChatGPT about it.

There are four things to do: import the schema, point it at your bridge URL, add the API
key, and write instructions the model will follow. They are all on one page.

**[Custom GPT setup]({{ '/custom-gpt.html' | relative_url }})**

One thing to carry across from this guide: only the bridge URL has to be reachable from the
internet. OpenClaw stays private, and the Gateway token never leaves the bridge.

---

---

### About the cold start

With `minReplicas: 0` the app stops when nobody is using it. That is what keeps it free.

The next request has to start both containers and rejoin the tailnet before the bridge answers. Expect the **first request after a quiet period to take several seconds**. Requests after that are fast.

If that first slow call ever causes a ChatGPT Action to time out, you have two options:

- ask the same thing again, since the second call is fast, or
- set `minReplicas: 1`, which keeps one copy running. Idle replicas are billed at a reduced rate, but this **will use up your free grant** and eventually cost money.

---

### Changing a secret later

Secrets live in two places and **both** must be updated, or the bridge and its callers stop
agreeing. Edit `.env` first, then push it:

```bash
set -a; . ./.env; set +a

az containerapp secret set \
  --name openclaw-bridge --resource-group openclaw-bridge \
  --secrets "bridge-api-key=$BRIDGE_API_KEY" \
            "gateway-token=$OPENCLAW_GATEWAY_TOKEN" \
            "tailscale-authkey=$TS_AUTHKEY"
```

**Updating the secret is not enough.** The container reads secrets at start, so it keeps
serving the old value until the revision restarts:

```bash
az containerapp revision restart \
  --name openclaw-bridge --resource-group openclaw-bridge \
  --revision "$(az containerapp show --name openclaw-bridge \
      --resource-group openclaw-bridge --query properties.latestRevisionName -o tsv)"
```

Then confirm the new value is live:

```bash
BRIDGE="https://$(az containerapp show --name openclaw-bridge --resource-group openclaw-bridge \
  --query 'properties.configuration.ingress.fqdn' -o tsv)"

curl -s -o /dev/null -w "no key:  %{http_code}\n" -X POST "$BRIDGE/v1/openclaw" \
  -H 'content-type: application/json' -d '{"action":"get_flow","flowId":"probe"}'
curl -s -o /dev/null -w "new key: %{http_code}\n" -X POST "$BRIDGE/v1/openclaw" \
  -H 'content-type: application/json' -H "api_key: $BRIDGE_API_KEY" \
  -d '{"action":"get_flow","flowId":"probe"}'
```

`401` then `200` is correct. If the new key returns `401`, the restart did not take effect.

Changing `BRIDGE_API_KEY` also means updating the credential saved in the ChatGPT Action. See
[Custom GPT setup]({{ '/custom-gpt.html' | relative_url }}).

### Things that went wrong for us

These are real failures from an actual deployment, in roughly the order they bite. If
something is broken and you are not sure where to start, read this table first.

| Symptom | Cause | Fix |
|---|---|---|
| Revision deploys but the container never starts | The image is `arm64`; Azure Container Apps only runs `linux/amd64` | Build with `--platform linux/amd64` and confirm with `docker image inspect … --format '{{.Architecture}}'` before pushing |
| Image pull fails on a brand new registry | New GitHub packages are **private** by default and Container Apps has no credentials | Set the package public, then verify an anonymous manifest fetch returns `200` |
| Bridge returns `401` from OpenClaw | The secret changed but a process is still running with the old one | Two restarts are needed and they are independent: restart OpenClaw so it re-reads `.env`, **and** `az containerapp revision restart` so the container re-reads the Azure secret |
| Secret looks identical but still fails | `cut -d= -f2` truncated the value at an `=` | Use `cut -d= -f2-`, and compare SHA-256 prefixes instead of reading values by eye |
| Route `404`s even though the config looks right | The secret failed to resolve, so the plugin skipped the route | See [OpenClaw setup]({{ '/openclaw-setup.html' | relative_url }}). Compare your path against a deliberately fake one |
| `/readyz` says `tailscale proxy not ready` | The Tailscale sidecar has not joined the tailnet | Check the auth key is ephemeral **and** reusable, and that it has not expired |
| Everything works, then breaks after an idle period | Cold start | The first request after the app scales to zero has to restart Tailscale too. Retry once; if it matters, set `minReplicas: 1` and leave the free tier |

#### The restart rule worth memorising

Configuration hot-reloads. **Environment variables do not.** Any change to a secret means
restarting whichever processes read it at startup, on both sides of the bridge.

---

### Final checklist

- [ ] Azure subscription and `az` signed in
- [ ] Tailscale ephemeral, reusable auth key created
- [ ] Resource group and Container Apps environment created
- [ ] Image built for `linux/amd64` and pushed
- [ ] GHCR package set to **public** (anonymous manifest fetch returns `200`)
- [ ] `app.yaml` written, deployed, and then deleted
- [ ] Ingress is `external: true`
- [ ] `/healthz` returns JSON
- [ ] `/readyz` reports ready, proving Tailscale connected
- [ ] `OPENCLAW_GATEWAY_URL` uses a tailnet address, not localhost
- [ ] Custom GPT action imported and pointed at the Container Apps URL

### Short summary

If you only remember one thing, remember this:

- **ChatGPT talks to the free Container Apps URL**
- **the bridge talks to OpenClaw through the Tailscale container on `localhost:1055`**
- **OpenClaw does not need to be public, and you do not need a server or a domain**
