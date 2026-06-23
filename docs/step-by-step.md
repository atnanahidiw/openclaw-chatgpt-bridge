# Step-by-Step Setup

## 1. Enter project

```bash
cd /Users/atnanahidiw/.openclaw/workspace/workdir/openclaw-chatgpt-bridge
```

## 2. Set env

```bash
cp .env.example .env
```

Edit `.env`:

- `OPENCLAW_WEBHOOK_URL`
- `OPENCLAW_WEBHOOK_SECRET`
- `OPENCLAW_SESSION_KEY`

If OpenClaw is private, point `OPENCLAW_WEBHOOK_URL` to the tailnet hostname or IP.

## 3. Run tests

```bash
go test ./...
```

## 4. Start server

```bash
go run .
```

## 5. Check health

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

## 6. Test bridge request

```bash
curl -X POST http://localhost:8080/v1/openclaw \
  -H 'content-type: application/json' \
  --data @payloads/create_flow.json
```

## 7. Build container

```bash
docker build -t openclaw-chatgpt-bridge:local .
```

## 8. Run container

```bash
docker run --rm -p 8080:8080 --env-file .env openclaw-chatgpt-bridge:local
```

## 9. Deploy to Kubernetes

Use same manifests on Azure AKS and Alibaba ACK:

```bash
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/secret.example.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/ingress.yaml
```

## 10. Add Tailscale if needed

```bash
kubectl apply -f k8s/deployment-with-tailscale.yaml
```

That gives bridge pod tailnet access without changing app code.

## 11. Or install with Helm

```bash
helm upgrade --install openclaw-bridge ./chart \
  --namespace openclaw-bridge \
  --create-namespace
```

Tailscale is enabled by default in the chart. If you want to disable it:

```bash
helm upgrade --install openclaw-bridge ./chart \
  --namespace openclaw-bridge \
  --create-namespace \
  --set tailscale.enabled=false
```

Azure preset:

```bash
helm upgrade --install openclaw-bridge ./chart \
  --namespace openclaw-bridge \
  --create-namespace \
  -f chart/values.azure.yaml
```

Alibaba preset:

```bash
helm upgrade --install openclaw-bridge ./chart \
  --namespace openclaw-bridge \
  --create-namespace \
  -f chart/values.alibaba.yaml
```

## 12. Wire ChatGPT Action

Import `openapi/openclaw-bridge.openapi.yaml` into a Custom GPT Action and point it at public bridge URL.
