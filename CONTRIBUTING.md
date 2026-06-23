# Contributing

This file holds setup, build, deployment, and maintenance details for the bridge.

## Local setup

```bash
cp .env.example .env
go test ./...
go run .
```

## Smoke test

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
curl -X POST http://localhost:8080/v1/openclaw \
  -H 'content-type: application/json' \
  --data @payloads/create_flow.json
```

## Docker

```bash
docker build -t openclaw-chatgpt-bridge:local .
docker run --rm -p 8080:8080 --env-file .env openclaw-chatgpt-bridge:local
```

## Kubernetes

Use the same YAML on Azure AKS and Alibaba ACK:

```bash
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/secret.example.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/ingress.yaml
```

If OpenClaw sits behind Tailscale:

```bash
kubectl apply -f k8s/deployment-with-tailscale.yaml
kubectl apply -f k8s/tailscale-secret.example.yaml
```

## Helm

```bash
helm upgrade --install openclaw-bridge ./chart \
  --namespace openclaw-bridge \
  --create-namespace
```

Tailscale is enabled by default. Disable it with:

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

## Tailscale notes

- Tailscale is enabled by default in the Helm chart.
- The chart uses a localhost proxy path for outbound traffic when tailnet mode is enabled.
- The bridge checks `TAILSCALE_ENABLED` and `TAILSCALE_PROXY_ADDR` before reporting readiness.

## OpenAPI

The ChatGPT Action schema lives at:

```bash
sed -n '1,220p' openapi/openclaw-bridge.openapi.yaml
```

