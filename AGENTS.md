# AGENTS.md

Guidance for coding agents working in this repository.

## What this is

A single-binary Go HTTP service between a ChatGPT Custom GPT Action and an OpenClaw
Gateway. It authenticates the caller, forwards the instruction to OpenClaw's
OpenAI-compatible endpoint, and returns what the agent said.

```text
+----------------+      +-------------------+      +-----------------------+
| ChatGPT Action | ---> | POST /v1/openclaw | ---> | OpenClaw Gateway      |
+----------------+      | (this bridge)     |      | /v1/chat/completions  |
                        +-------------------+      +-----------------------+
```

The upstream call runs a **real agent turn**: shell commands, file access, installed
skills. It is not an LLM passthrough. That endpoint is full operator access on the OpenClaw
side, which is why the gateway token stays in the bridge and callers authenticate with a
separate `BRIDGE_API_KEY`.

State: none, except an in-memory job store for async turns.

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
| Inbound authentication | `authorizeRequest`, `presentedKey`, `secretsEqual` |
| Request handling | `handleOpenClaw` — decode, validate, dispatch |
| Payload contract | `allowedActions`, normalization, validation |
| Async job store | `jobStore` and its janitor |
| OpenClaw gateway hop | `askGateway`, error mapping |
| JSON helpers | `writeJSON`, `getString`, `ensureEOF` |

| Path | What lives there |
| --- | --- |
| [main.go](main.go) | The whole service |
| [main_test.go](main_test.go) | All tests (unit + `httptest` handler tests) |
| [openapi/openclaw-bridge.openapi.yaml](openapi/openclaw-bridge.openapi.yaml) | Schema imported into the Custom GPT Action |
| [payloads/](payloads/) | Sample request bodies used by the smoke test |
| [k8s/](k8s/) | Plain manifests |
| [chart/](chart/) | Helm chart (same deployment, templated) |
| [docs/](docs/) | Jekyll site published to GitHub Pages |
| [scripts/smoke-test.sh](scripts/smoke-test.sh) | Local smoke test: a sync `ask`, then an `ask_async` round trip |

## Commands

```bash
go test ./...          # tests
go test -race ./...    # CI runs this too
go vet ./...
go run .               # needs .env exported, or it 500s on /v1/openclaw
```

Copy `.env.example` to `.env` first. `go run .` does not read `.env` itself — the Docker
Compose and script paths do the loading.

Smoke test a running instance:

```bash
curl http://localhost:8080/healthz
./scripts/smoke-test.sh
```

## Endpoints

- `POST /v1/openclaw` — the only functional route, and the only one requiring auth
- `GET /healthz` — always OK
- `GET /readyz` — additionally dials `TAILSCALE_PROXY_ADDR` when `TAILSCALE_ENABLED=true`
- `GET /version` — build metadata injected via `-ldflags -X main.version=…`

Health endpoints stay unauthenticated on purpose: container platforms probe them without
credentials.

## Contract rules

Three actions, all thin wrappers over one chat-completions call:

| Action | Required | Optional | Returns |
| --- | --- | --- | --- |
| `ask` | `message` | `customSession` | `reply`, synchronously (`200`) |
| `ask_async` | `message` | `customSession` | `jobId`, immediately (`200`) |
| `get_result` | `jobId` | — | `status`, plus `reply` or `error` |

Every success is `200` — including `ask_async`. `202` was correct but ChatGPT Actions
report it as a `ClientResponseError`, so the code carries `status` in the body instead.

`customSession` is a **name**, not a full key. The bridge prepends its own namespace
(`sessionPrefix`, default `agent:main:chatgpt`) and sends the result as
`x-openclaw-session-key`. So `customSession: "research"` becomes
`agent:main:chatgpt:research`; omitting it uses the prefix alone. This is a security
boundary: the name is validated against `sessionNamePattern` (one path-safe segment, no
colons), so a caller cannot climb out of the namespace to reach `agent:main:main` or a
reserved `subagent:`/`cron:`/`acp:` session. `TestSessionNameCannotEscapeTheNamespace`
pins this.

**Changing the contract means changing three places in the same commit:** the Go
validation, the OpenAPI schema, and the error-message strings that enumerate valid values
(they are hand-written literals, not generated from the maps). Tests assert on those
strings.

Verify against a live OpenClaw rather than trusting the tests alone. Stub gateways cannot
tell you that a turn takes 30 seconds, or that the endpoint is disabled by default.

## Conventions

- Standard library only. `go.mod` has zero dependencies — keep it that way unless the user
  asks for a dependency explicitly.
- All responses go through `writeJSON` and carry an `ok` boolean. Errors add `error`,
  validation failures add `details` (a string array), async failures add `failedAt` and a
  `hint`.
- **Failures must stay legible to a model.** The `hint` field exists so a GPT knows whether
  to retry. Do not remove it, and do not replace plain-language errors with codes.
- One structured log line per `/v1/openclaw` request, emitted from a `defer` in
  `handleOpenClaw` in `key=value` form. Async outcomes log a second line when the turn
  ends. Never log the gateway token, the api key, or message bodies.
- Config is read once at startup by `loadConfig` via the `envString`/`envInt` helpers,
  which fall back silently on unset or unparseable values. Do not read `os.Getenv` from
  handlers — pass `config` through.
- Auth runs **before** any other check, so an unauthenticated caller learns nothing about
  configuration. `TestAuthIsCheckedBeforeConfiguration` pins this.
- Secrets are compared with `secretsEqual`, which hashes both sides and uses
  `subtle.ConstantTimeCompare`. Do not replace it with `==`.
- JSON decoding uses `dec.UseNumber()` and `ensureEOF` to reject trailing data; bodies are
  capped by `MAX_BODY_BYTES`. Preserve both when touching the decode path.

## Gotchas

- **Async jobs live in memory.** A restart drops them, and a second replica cannot see the
  first replica's jobs, so a poll can `404` a healthy job. **Run one replica** — on Azure
  Container Apps that is `--max-replicas 1`. Adding horizontal scale requires moving the
  job store somewhere shared first.
- **The upstream endpoint is disabled by default.** `gateway.http.endpoints.chatCompletions`
  must be enabled in `~/.openclaw/openclaw.json`, and the Gateway restarted.
- **Judge that endpoint by content type, not status code.** OpenClaw's Control UI answers
  `200 text/html` for paths that do not exist, so a `200` from `/v1/models` proves nothing
  on its own. This has caused false "it works" conclusions more than once.
- **Two credentials fail differently.** A bad caller key is `401` (their problem); a bad
  gateway token is `500` (the operator's). Keep that mapping — a GPT should retry neither,
  but the distinction tells a human where to look.
- **Every success returns 200, never 202.** ChatGPT Actions surface non-200 success codes
  as a `ClientResponseError`. `ask_async` therefore answers `200` with `status: "running"`
  in the body. `TestAsyncRoundTrip` asserts the code.
- **Turns are slow.** Even a trivial question takes seconds; anything touching files takes
  minutes. `ask` exists for convenience, but `ask_async` is the honest default. Do not
  lower `REQUEST_TIMEOUT_MS` to "fix" a timeout — switch to async.
- **The Dockerfile copies `*.go` from the root only.** If Go source ever moves into
  subdirectories, [Dockerfile](Dockerfile) needs a matching `COPY` or the image build
  breaks while CI still passes — the two build paths are independent. It also
  cross-compiles via `--platform=$BUILDPLATFORM` and `GOARCH=$TARGETARCH`, because
  Container Apps requires `linux/amd64` and emulating the Go toolchain is far slower.
- **CI validates a hard-coded list of YAML paths** in
  [.github/workflows/ci.yml](.github/workflows/ci.yml). New chart values files or workflows
  must be added to that list. That step uses a strict loader that rejects duplicate mapping
  keys — plain `yaml.safe_load` keeps the last one silently, which is how a duplicated
  `goal` property once sat in the OpenAPI spec with CI green.
- **`k8s/` and `chart/` describe the same deployment twice.** A change to env vars, probes,
  or the Tailscale sidecar needs to land in both, plus
  [chart/values.schema.json](chart/values.schema.json) when adding a values key.
- **`ADDR` vs `PORT`:** the server binds `ADDR` (default `:8080`); `PORT` is not used for
  binding at all. Use `ADDR=:8080` for pod/tailnet reachability; bind `127.0.0.1` only when
  a sidecar proxies to localhost.
- Docs under `docs/` deploy to GitHub Pages on push to `main`. README links to the
  published site, so renaming a docs page breaks those links.

## Before finishing

Run `go test -race ./...` and `go vet ./...`. If you touched `chart/`, also run
`helm lint ./chart` and `helm template openclaw-bridge ./chart` against each values preset
(default, `values.azure.yaml`, `values.alibaba.yaml`) — CI does exactly this.
