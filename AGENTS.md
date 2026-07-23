# AGENTS.md

Guidance for coding agents working in this repository.

## What this is

A single-binary Go HTTP service that sits between a ChatGPT Custom GPT Action and an
OpenClaw webhook. It validates the inbound Action payload, normalizes it, forwards it to
`OPENCLAW_WEBHOOK_URL`, and returns the upstream status/body to the caller.

```text
ChatGPT Action -> POST /v1/openclaw (this bridge) -> OpenClaw webhook
```

There is no database, no auth of its own, and no persistent state. Every request is
stateless pass-through with validation.

## Layout

All Go source is `package main`, flat at the repo root — no `src/`, no `cmd/`, and tests
sit beside the code they test (they call unexported functions, so they must).

[main.go](main.go) is deliberately one file, organized into banner-commented sections that
follow the request lifecycle top to bottom. Keep new code in the section it belongs to
rather than appending to the end, and keep the map in the package doc comment current:

| Section | Contents |
| --- | --- |
| Entry point | `main()`, server lifecycle, graceful shutdown |
| Configuration | `config` struct, `loadConfig`, env helpers |
| Routing | `newMux`, `/healthz`, `/readyz`, `/version` |
| Request handling | `handleOpenClaw` — the `/v1/openclaw` flow |
| Payload contract | Action whitelists, normalization, validation, outbound shaping |
| Upstream forwarding | `forwardToOpenClaw` |
| JSON helpers | `writeJSON`, `getString`, `getMap`, `ensureEOF` |

| Path | What lives there |
| --- | --- |
| [main.go](main.go) | The whole service |
| [main_test.go](main_test.go) | All tests (unit + `httptest` handler tests) |
| [openapi/openclaw-bridge.openapi.yaml](openapi/openclaw-bridge.openapi.yaml) | Schema imported into the Custom GPT Action |
| [payloads/](payloads/) | Sample request bodies used by smoke tests |
| [k8s/](k8s/) | Plain manifests |
| [chart/](chart/) | Helm chart (same deployment, templated) |
| [docs/](docs/) | Jekyll site published to GitHub Pages |
| [scripts/test-webhook.sh](scripts/test-webhook.sh) | Local smoke test against a running bridge |

## Commands

```bash
go test ./...          # tests
go test -race ./...    # CI runs this too
go vet ./...
go run .               # needs .env values exported, or it 500s on /v1/openclaw
```

Copy `.env.example` to `.env` first. `go run .` does not read `.env` itself — the Docker
Compose and script paths do the loading.

Smoke test a running instance:

```bash
curl http://localhost:8080/healthz
./scripts/test-webhook.sh
```

## Endpoints

- `POST /v1/openclaw` — the only functional route
- `GET /healthz` — always OK
- `GET /readyz` — additionally dials `TAILSCALE_PROXY_ADDR` when `TAILSCALE_ENABLED=true`
- `GET /version` — build metadata injected via `-ldflags -X main.version=…`

## Contract rules

This mirrors the OpenClaw `webhooks` plugin. **Its per-action schemas are `strict()`** —
any key the action does not declare fails the entire request with
`400 Unrecognized keys`. So `buildOpenClawPayload` must emit exactly the accepted key set
per action and nothing more. The authoritative schema lives in the installed package at
`dist/extensions/webhooks/index.js`, with prose in `docs/plugins/webhooks.md`.

Actions are whitelisted in `allowedActions`; notify policies, statuses, and runtimes have
their own maps beside it. Per-action rules live in `validateInboundPayload`:

| Action | Required | Also accepted |
| --- | --- | --- |
| `create_flow` | `goal` | `status` (queued/running/waiting/blocked), `notifyPolicy`, `stateJson` |
| `run_task` | `flowId`, `task`, `runtime` (subagent/acp) | `childSessionKey`, `status` (queued/running), `notifyPolicy` |
| `get_flow` | `flowId` | — nothing else |
| `resume_flow` | `flowId`, `expectedRevision` | `status` (queued/running), `stateJson` |
| `finish_flow` | `flowId`, `expectedRevision` | `stateJson` |

Two inbound fields are deliberately **never forwarded**, because no action accepts them:

- `sessionKey` — the webhook route is bound to a session by OpenClaw config, so a caller
  cannot choose one. `OPENCLAW_SESSION_KEY` survives for log context only.
- `metadata` — re-emitted as `stateJson` on the three actions that accept it, dropped for
  `get_flow` and `run_task`.

`expectedRevision` is optimistic concurrency: read it from a `get_flow` response first.
`getInt64` returns a `*int64` so revision `0` is distinguishable from omitted.

**Changing the contract means changing three places in the same commit:** the Go
validation, the OpenAPI schema, and the error-message strings that enumerate valid values
(they are hand-written literals, not generated from the maps). Tests assert on those
strings, and `TestOutboundKeysMatchUpstreamSchema` pins the emitted key set per action.

Verify against a live OpenClaw rather than trusting the tests alone — the strict-schema
failures only show up over the wire.

## Conventions

- Standard library only. `go.mod` has zero dependencies — keep it that way unless the user
  asks for a dependency explicitly.
- All responses go through `writeJSON` and carry an `ok` boolean. Errors add `error`, and
  validation failures add `details` (a string array). Upstream failures add
  `upstreamStatus` and `upstreamBody`.
- One structured log line per `/v1/openclaw` request, emitted from a `defer` in
  `handleOpenClaw` in `key=value` form. Add fields there rather than logging ad hoc
  mid-handler. Never log the webhook secret or full request bodies.
- Config is read once at startup by `loadConfig` via the `envString`/`envInt` helpers,
  which fall back silently on unset or unparseable values. Do not read `os.Getenv` from
  handlers — pass `config` through.
- JSON decoding uses `dec.UseNumber()` and `ensureEOF` to reject trailing data; bodies are
  capped by `MAX_BODY_BYTES`. Preserve both when touching the decode path.
- Inbound `X-Request-ID` is echoed upstream, generated when absent. Keep that propagation.

## Gotchas

- **The Dockerfile copies `*.go` from the root only.** If Go source ever moves into
  subdirectories, [Dockerfile](Dockerfile) needs a matching `COPY` or the image build
  breaks while CI still passes — the two build paths are independent.
- **CI validates a hard-coded list of YAML paths** in
  [.github/workflows/ci.yml](.github/workflows/ci.yml). New chart values files or
  workflows must be added to that list to be checked. That step loads YAML with a
  strict loader that rejects duplicate mapping keys — plain `yaml.safe_load` keeps the
  last one silently, which is how a duplicated `goal` property once sat in the OpenAPI
  spec with CI green.
- **`k8s/` and `chart/` describe the same deployment twice.** A change to env vars,
  probes, or the Tailscale sidecar needs to land in both, plus
  [chart/values.schema.json](chart/values.schema.json) when adding a values key.
- **`ADDR` vs `PORT`:** the server binds `ADDR` (default `:8080`); `PORT` is loaded into
  config but not used for binding. Use `ADDR=:8080` for pod/tailnet reachability; only
  bind `127.0.0.1` when a sidecar proxies to localhost.
- Docs under `docs/` deploy to GitHub Pages on push to `main`. README links to the
  published site, so renaming a docs page breaks those links.

## Before finishing

Run `go test -race ./...` and `go vet ./...`. If you touched `chart/`, also run
`helm lint ./chart` and `helm template openclaw-bridge ./chart` against each values preset
(default, `values.azure.yaml`, `values.alibaba.yaml`) — CI does exactly this.
