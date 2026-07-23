# Helm chart

Install:

```bash
helm upgrade --install openclaw-bridge ./chart \
  --namespace openclaw-bridge \
  --create-namespace
```

Tailscale is enabled by default. To turn it off:

```bash
helm upgrade --install openclaw-bridge ./chart \
  --namespace openclaw-bridge \
  --create-namespace \
  --set tailscale.enabled=false
```

If you want to explicitly pass the auth key secret name:

```bash
helm upgrade --install openclaw-bridge ./chart \
  --namespace openclaw-bridge \
  --create-namespace \
  --set tailscale.enabled=true \
  --set tailscale.authKeySecretName=tailscale-auth \
  --set tailscale.openclawWebhookUrl=http://openclaw-gateway.tailnet:18789/plugins/webhooks/gpt
```

Provider presets:

```bash
helm upgrade --install openclaw-bridge ./chart \
  --namespace openclaw-bridge \
  --create-namespace \
  -f ./chart/values.azure.yaml
```

```bash
helm upgrade --install openclaw-bridge ./chart \
  --namespace openclaw-bridge \
  --create-namespace \
  -f ./chart/values.alibaba.yaml
```
