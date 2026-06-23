# Commands

Run from:

```bash
cd /Users/atnanahidiw/.openclaw/workspace/workdir/openclaw-chatgpt-bridge
```

## Local

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

```bash
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/secret.example.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/ingress.yaml
```

## Helm

```bash
helm upgrade --install openclaw-bridge ./chart \
  --namespace openclaw-bridge \
  --create-namespace
```

Tailscale is on by default. Disable it with:

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

## Tailscale mode

```bash
kubectl apply -f k8s/deployment-with-tailscale.yaml
```

## OpenAPI

```bash
sed -n '1,220p' openapi/openclaw-bridge.openapi.yaml
```
