// Command openclaw-chatgpt-bridge is an HTTP service that sits between a
// ChatGPT Custom GPT Action and an OpenClaw webhook.
//
// The request flow, and the order of this file, is:
//
//	main       -> start the server, shut down cleanly on SIGINT/SIGTERM
//	config     -> read every environment variable once, at startup
//	routing    -> /healthz, /readyz, /version, /v1/openclaw
//	handling   -> authenticate, decode, normalize, validate, forward, respond
//	auth       -> the shared api_key callers must present
//	payload    -> the request contract: allowed actions and required fields
//	forwarding -> the single hop to OpenClaw
//	helpers    -> JSON encode/decode plumbing
//
// The service is stateless: no database, no sessions. Inbound callers are
// authenticated with a shared key when BRIDGE_API_KEY is set; the separate
// OPENCLAW_WEBHOOK_SECRET is what the bridge presents to OpenClaw.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Build metadata, injected at link time with -ldflags "-X main.version=...".
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func main() {
	cfg := loadConfig()

	// The bridge attaches the OpenClaw webhook secret itself, so without an
	// inbound key anyone who learns the URL can drive TaskFlows. Say so loudly
	// rather than failing silently open.
	if cfg.bridgeAPIKey == "" {
		log.Printf("WARNING: BRIDGE_API_KEY is not set; POST /v1/openclaw accepts unauthenticated requests")
	}

	mux := newMux(cfg)
	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.requestTimeout,
		WriteTimeout:      cfg.requestTimeout,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server exited with error: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Configuration
//
// Every environment variable is read once here and passed to handlers by
// value. Handlers must not call os.Getenv directly.
// ---------------------------------------------------------------------------

type config struct {
	serviceName    string
	port           int
	addr           string
	requestTimeout time.Duration
	maxBodyBytes   int64

	openclawWebhookURL string
	openclawSecret     string
	sessionKey         string

	// Shared secret callers must present on /v1/openclaw. Empty disables the
	// check, which leaves the endpoint open to anyone who knows the URL.
	bridgeAPIKey string

	tailscaleEnabled   bool
	tailscaleProxyAddr string

	httpClient *http.Client
}

func loadConfig() config {
	timeout := time.Duration(envInt("REQUEST_TIMEOUT_MS", 30000)) * time.Millisecond

	// The listener binds addr, not port. Use ":8080" to be reachable across the
	// pod network or a tailnet; bind 127.0.0.1 only behind a local proxy.
	return config{
		serviceName:    "openclaw-chatgpt-bridge",
		port:           envInt("PORT", 8080),
		addr:           envString("ADDR", ":8080"),
		requestTimeout: timeout,
		maxBodyBytes:   int64(envInt("MAX_BODY_BYTES", 1024*1024)),

		openclawWebhookURL: strings.TrimSpace(os.Getenv("OPENCLAW_WEBHOOK_URL")),
		openclawSecret:     strings.TrimSpace(os.Getenv("OPENCLAW_WEBHOOK_SECRET")),
		sessionKey:         strings.TrimSpace(os.Getenv("OPENCLAW_SESSION_KEY")),
		bridgeAPIKey:       strings.TrimSpace(os.Getenv("BRIDGE_API_KEY")),

		tailscaleEnabled:   strings.EqualFold(strings.TrimSpace(os.Getenv("TAILSCALE_ENABLED")), "true"),
		tailscaleProxyAddr: envString("TAILSCALE_PROXY_ADDR", "127.0.0.1:1055"),

		httpClient: &http.Client{Timeout: timeout},
	}
}

// envString returns the trimmed value of name, or fallback when unset or blank.
func envString(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

// envInt returns name parsed as an int. Unset, blank, and unparseable values
// all fall back silently, so a typo degrades to the default rather than
// failing startup.
func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// ---------------------------------------------------------------------------
// Routing
// ---------------------------------------------------------------------------

func newMux(cfg config) *http.ServeMux {
	mux := http.NewServeMux()

	// Liveness: the process is up.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"service": cfg.serviceName,
		})
	})

	// Readiness: the process is up *and* the upstream path is dialable.
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if cfg.tailscaleEnabled {
			conn, err := net.DialTimeout("tcp", cfg.tailscaleProxyAddr, 500*time.Millisecond)
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{
					"ok":    false,
					"error": "tailscale proxy not ready",
				})
				return
			}
			_ = conn.Close()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"service": cfg.serviceName,
		})
	})

	mux.HandleFunc("/version", versionHandler)

	mux.HandleFunc("/v1/openclaw", func(w http.ResponseWriter, r *http.Request) {
		handleOpenClaw(w, r, cfg)
	})

	return mux
}

func versionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok":    false,
			"error": "method not allowed",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":      "openclaw-chatgpt-bridge",
		"version":   version,
		"commit":    commit,
		"buildTime": buildTime,
	})
}

// ---------------------------------------------------------------------------
// Request handling
// ---------------------------------------------------------------------------

// handleOpenClaw is the only functional route. It decodes the GPT Action body,
// normalizes and validates it, forwards it to OpenClaw, and mirrors the
// upstream status and body back to the caller.
func handleOpenClaw(w http.ResponseWriter, r *http.Request, cfg config) {
	start := time.Now()

	// Propagate the caller's trace ID upstream, or mint one if absent.
	requestID := strings.TrimSpace(r.Header.Get("x-request-id"))
	if requestID == "" {
		requestID = strconv.FormatInt(time.Now().UnixNano(), 10)
	}

	// Every request emits exactly one structured log line, from this defer.
	// Add fields here rather than logging ad hoc mid-handler, and never log
	// the webhook secret or full request bodies.
	statusCode := http.StatusOK
	action := ""
	flowID := ""
	sessionKey := ""
	upstreamStatus := 0
	errMsg := ""
	defer func() {
		log.Printf(
			"request_id=%s action=%s flow_id=%s session_key=%s upstream_status=%d duration_ms=%d error=%q",
			requestID,
			action,
			flowID,
			sessionKey,
			upstreamStatus,
			time.Since(start).Milliseconds(),
			errMsg,
		)
	}()

	if r.Method != http.MethodPost {
		statusCode = http.StatusMethodNotAllowed
		errMsg = "method not allowed"
		writeJSON(w, statusCode, map[string]any{
			"ok":    false,
			"error": errMsg,
		})
		return
	}

	// Authenticate before anything else, so an unauthenticated caller learns
	// nothing about how the bridge is configured.
	if !authorizeRequest(r, cfg.bridgeAPIKey) {
		statusCode = http.StatusUnauthorized
		errMsg = "unauthorized"
		w.Header().Set("www-authenticate", `Bearer realm="openclaw-bridge"`)
		writeJSON(w, statusCode, map[string]any{
			"ok":    false,
			"error": errMsg,
		})
		return
	}

	if cfg.openclawWebhookURL == "" {
		statusCode = http.StatusInternalServerError
		errMsg = "OPENCLAW_WEBHOOK_URL is not set"
		writeJSON(w, statusCode, map[string]any{
			"ok":    false,
			"error": errMsg,
		})
		return
	}

	// Cap the body, and decode numbers as json.Number so large IDs survive
	// the round trip without float64 precision loss.
	r.Body = http.MaxBytesReader(w, r.Body, cfg.maxBodyBytes)
	defer r.Body.Close()

	var raw map[string]any
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		if isRequestBodyTooLarge(err) {
			statusCode = http.StatusRequestEntityTooLarge
			errMsg = "request body too large"
		} else {
			statusCode = http.StatusBadRequest
			errMsg = "request body must be JSON object"
		}
		writeJSON(w, statusCode, map[string]any{
			"ok":    false,
			"error": errMsg,
		})
		return
	}

	if err := ensureEOF(dec); err != nil {
		statusCode = http.StatusBadRequest
		errMsg = "request body must be a single JSON object"
		writeJSON(w, statusCode, map[string]any{
			"ok":    false,
			"error": errMsg,
		})
		return
	}

	normalized := normalizeInboundPayload(raw, cfg.sessionKey)
	action = normalized.Action
	flowID = normalized.FlowID
	sessionKey = normalized.SessionKey

	validation := validateInboundPayload(normalized)
	if !validation.OK {
		statusCode = http.StatusBadRequest
		errMsg = "invalid request"
		writeJSON(w, statusCode, map[string]any{
			"ok":      false,
			"error":   errMsg,
			"details": validation.Errors,
		})
		return
	}

	upstream := forwardToOpenClaw(r.Context(), cfg, requestID, buildOpenClawPayload(normalized))
	upstreamStatus = upstream.Status
	if !upstream.OK {
		statusCode = upstream.Status
		if statusCode == 0 {
			statusCode = http.StatusBadGateway
		}
		if upstream.Error == "" {
			errMsg = "openclaw request failed"
		} else {
			errMsg = upstream.Error
		}
		writeJSON(w, statusCode, map[string]any{
			"ok":             false,
			"error":          errMsg,
			"upstreamStatus": upstream.Status,
			"upstreamBody":   upstream.Body,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"bridge":         cfg.serviceName,
		"upstreamStatus": upstream.Status,
		"upstreamBody":   upstream.Body,
	})
}

// ---------------------------------------------------------------------------
// Inbound authentication
//
// The OpenAPI schema declares an apiKey scheme carrying the secret in an
// `api_key` header, which is what the Custom GPT sends:
//
//	Authentication = "API Key", Custom header name "api_key" -> api_key: <key>
//
// Authorization: Bearer <key> is also accepted, purely so the endpoint is easy
// to call from curl.
//
// Only /v1/openclaw is protected. The health endpoints stay open because
// container platforms probe them without credentials.
// ---------------------------------------------------------------------------

// authorizeRequest reports whether the caller presented the expected key.
// An empty expected key disables the check entirely.
func authorizeRequest(r *http.Request, expected string) bool {
	if expected == "" {
		return true
	}
	return secretsEqual(presentedKey(r), expected)
}

// presentedKey pulls the caller's key out of whichever header carries it,
// preferring the one the OpenAPI schema declares.
func presentedKey(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("api_key")); v != "" {
		return v
	}

	raw := strings.TrimSpace(r.Header.Get("authorization"))
	if raw == "" {
		return ""
	}
	// Split on the first space so a missing or odd scheme cannot panic.
	if scheme, token, ok := strings.Cut(raw, " "); ok && strings.EqualFold(scheme, "bearer") {
		return strings.TrimSpace(token)
	}
	return raw
}

// secretsEqual compares in constant time. Both sides are hashed first so the
// comparison is over fixed-length input and cannot leak the expected length.
func secretsEqual(got, want string) bool {
	g := sha256.Sum256([]byte(got))
	w := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(g[:], w[:]) == 1
}

// ---------------------------------------------------------------------------
// Payload contract
//
// This mirrors the OpenClaw `webhooks` plugin, whose per-action schemas are
// strict(): an unrecognized key fails the whole request with 400
// "Unrecognized keys". So buildOpenClawPayload must emit exactly the keys the
// target action accepts and nothing else. The accepted sets are, per action:
//
//	create_flow  goal(req) controllerId status notifyPolicy currentStep
//	             stateJson waitJson
//	run_task     flowId(req) runtime(req) task(req) childSessionKey label
//	             sourceId parentTaskId agentId runId preferMetadata
//	             notifyPolicy status startedAt lastEventAt progressSummary
//	get_flow     flowId(req)
//	resume_flow  flowId(req) expectedRevision(req) status currentStep stateJson
//	finish_flow  flowId(req) expectedRevision(req) stateJson
//
// Two inbound fields are deliberately NOT forwarded, because no action accepts
// them: `metadata` (surfaced as `stateJson` where that is accepted) and
// `sessionKey` (the webhook route is bound to a session by OpenClaw config, so
// the caller cannot choose one; it is kept for log context only).
//
// Changing anything here means changing three things in the same commit: the
// checks below, openapi/openclaw-bridge.openapi.yaml, and the error strings
// that enumerate valid values. Those strings are hand-written literals, not
// generated from these maps, and tests assert on them.
// ---------------------------------------------------------------------------

var allowedActions = map[string]struct{}{
	"create_flow": {},
	"run_task":    {},
	"get_flow":    {},
	"resume_flow": {},
	"finish_flow": {},
}

var allowedNotifyPolicies = map[string]struct{}{
	"done_only":     {},
	"state_changes": {},
	"silent":        {},
}

// Upstream accepts a wider status set when creating a flow than when moving
// one, so the two are validated separately.
var allowedFlowStatuses = map[string]struct{}{
	"queued":  {},
	"running": {},
	"waiting": {},
	"blocked": {},
}

var allowedTaskStatuses = map[string]struct{}{
	"queued":  {},
	"running": {},
}

var allowedRuntimes = map[string]struct{}{
	"subagent": {},
	"acp":      {},
}

// normalizedPayload is the inbound GPT Action body after type coercion and
// defaulting.
type normalizedPayload struct {
	Action       string
	FlowID       string
	SessionKey   string
	Goal         string
	Task         string
	Status       string
	NotifyPolicy string
	Runtime      string
	ChildSession string
	Metadata     map[string]any

	// nil means the caller omitted it, which is distinct from zero.
	ExpectedRevision *int64
}

type validationResult struct {
	OK     bool     `json:"ok"`
	Errors []string `json:"errors,omitempty"`
}

// normalizeInboundPayload coerces the decoded body into normalizedPayload and
// applies defaults: sessionKey falls back to OPENCLAW_SESSION_KEY, and
// notifyPolicy falls back to "done_only".
func normalizeInboundPayload(raw map[string]any, defaultSessionKey string) normalizedPayload {
	payload := normalizedPayload{
		Action:           getString(raw, "action"),
		FlowID:           getString(raw, "flowId"),
		SessionKey:       getString(raw, "sessionKey"),
		Goal:             getString(raw, "goal"),
		Task:             getString(raw, "task"),
		Status:           getString(raw, "status"),
		NotifyPolicy:     getString(raw, "notifyPolicy"),
		Runtime:          getString(raw, "runtime"),
		ChildSession:     getString(raw, "childSessionKey"),
		Metadata:         getMap(raw, "metadata"),
		ExpectedRevision: getInt64(raw, "expectedRevision"),
	}

	if payload.SessionKey == "" {
		payload.SessionKey = defaultSessionKey
	}
	if payload.NotifyPolicy == "" {
		payload.NotifyPolicy = "done_only"
	}

	return payload
}

// validateInboundPayload collects every problem with the request rather than
// failing on the first, so the caller gets one complete list of errors.
func validateInboundPayload(payload normalizedPayload) validationResult {
	var errs []string

	if payload.Action == "" {
		errs = append(errs, "action is required")
	} else if _, ok := allowedActions[payload.Action]; !ok {
		errs = append(errs, "action must be one of: create_flow, run_task, get_flow, resume_flow, finish_flow")
	}

	// Per-action required fields, matching the upstream strict schemas.
	switch payload.Action {
	case "create_flow":
		if payload.Goal == "" {
			errs = append(errs, "goal is required for create_flow")
		}
		if payload.Status != "" {
			if _, ok := allowedFlowStatuses[payload.Status]; !ok {
				errs = append(errs, "status must be one of: queued, running, waiting, blocked")
			}
		}
	case "run_task":
		if payload.FlowID == "" {
			errs = append(errs, "flowId is required for run_task")
		}
		if payload.Task == "" {
			errs = append(errs, "task is required for run_task")
		}
		if payload.Runtime == "" {
			errs = append(errs, "runtime is required for run_task")
		} else if _, ok := allowedRuntimes[payload.Runtime]; !ok {
			errs = append(errs, "runtime must be one of: subagent, acp")
		}
		if payload.Status != "" {
			if _, ok := allowedTaskStatuses[payload.Status]; !ok {
				errs = append(errs, "status must be one of: queued, running")
			}
		}
	case "get_flow":
		if payload.FlowID == "" {
			errs = append(errs, "flowId is required for get_flow")
		}
	case "resume_flow", "finish_flow":
		if payload.FlowID == "" {
			errs = append(errs, "flowId is required for "+payload.Action)
		}
		if payload.ExpectedRevision == nil {
			errs = append(errs, "expectedRevision is required for "+payload.Action+"; read it from get_flow")
		} else if *payload.ExpectedRevision < 0 {
			errs = append(errs, "expectedRevision must not be negative")
		}
		if payload.Action == "resume_flow" && payload.Status != "" {
			if _, ok := allowedTaskStatuses[payload.Status]; !ok {
				errs = append(errs, "status must be one of: queued, running")
			}
		}
	}

	if payload.NotifyPolicy != "" {
		if _, ok := allowedNotifyPolicies[payload.NotifyPolicy]; !ok {
			errs = append(errs, "notifyPolicy must be one of: done_only, state_changes, silent")
		}
	}

	return validationResult{
		OK:     len(errs) == 0,
		Errors: errs,
	}
}

// buildOpenClawPayload emits only the keys the target action accepts. Adding a
// key here that the action does not declare fails the request upstream.
func buildOpenClawPayload(payload normalizedPayload) map[string]any {
	outbound := map[string]any{"action": payload.Action}

	// The caller's metadata has no home upstream, but three actions take a free
	// -form stateJson, so it rides along there rather than being dropped.
	stateJSON := func() {
		if len(payload.Metadata) > 0 {
			outbound["stateJson"] = payload.Metadata
		}
	}

	switch payload.Action {
	case "create_flow":
		outbound["goal"] = payload.Goal
		outbound["notifyPolicy"] = payload.NotifyPolicy
		if payload.Status != "" {
			outbound["status"] = payload.Status
		}
		stateJSON()

	case "run_task":
		outbound["flowId"] = payload.FlowID
		outbound["task"] = payload.Task
		outbound["runtime"] = payload.Runtime
		outbound["notifyPolicy"] = payload.NotifyPolicy
		if payload.ChildSession != "" {
			outbound["childSessionKey"] = payload.ChildSession
		}
		if payload.Status != "" {
			outbound["status"] = payload.Status
		}

	case "get_flow":
		outbound["flowId"] = payload.FlowID

	case "resume_flow":
		outbound["flowId"] = payload.FlowID
		outbound["expectedRevision"] = *payload.ExpectedRevision
		if payload.Status != "" {
			outbound["status"] = payload.Status
		}
		stateJSON()

	case "finish_flow":
		outbound["flowId"] = payload.FlowID
		outbound["expectedRevision"] = *payload.ExpectedRevision
		stateJSON()
	}

	return outbound
}

// ---------------------------------------------------------------------------
// Upstream forwarding
// ---------------------------------------------------------------------------

// upstreamResult is the outcome of the OpenClaw hop. Status carries the
// upstream HTTP status when there was one, or the status the bridge should
// return when the hop itself failed.
type upstreamResult struct {
	OK     bool   `json:"ok"`
	Status int    `json:"status"`
	Body   any    `json:"body,omitempty"`
	Error  string `json:"error,omitempty"`
}

func forwardToOpenClaw(ctx context.Context, cfg config, requestID string, payload map[string]any) upstreamResult {
	body, err := json.Marshal(payload)
	if err != nil {
		return upstreamResult{OK: false, Status: http.StatusBadRequest, Error: err.Error()}
	}

	reqCtx, cancel := context.WithTimeout(ctx, cfg.requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cfg.openclawWebhookURL, bytes.NewReader(body))
	if err != nil {
		return upstreamResult{OK: false, Status: http.StatusBadRequest, Error: err.Error()}
	}

	req.Header.Set("content-type", "application/json")
	if requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	// Sent both ways because OpenClaw webhook routes accept either form.
	if cfg.openclawSecret != "" {
		req.Header.Set("authorization", "Bearer "+cfg.openclawSecret)
		req.Header.Set("x-openclaw-webhook-secret", cfg.openclawSecret)
	}

	client := cfg.httpClient
	if client == nil {
		client = &http.Client{Timeout: cfg.requestTimeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
			return upstreamResult{OK: false, Status: http.StatusGatewayTimeout, Error: "OpenClaw request timed out"}
		}
		return upstreamResult{OK: false, Status: http.StatusBadGateway, Error: err.Error()}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return upstreamResult{OK: false, Status: http.StatusBadGateway, Error: err.Error()}
	}

	// Pass JSON through structured; anything else becomes a string, so a
	// stray HTML error page from a proxy still reaches the caller intact.
	var parsed any
	if strings.Contains(resp.Header.Get("content-type"), "application/json") {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			parsed = string(raw)
		}
	} else {
		parsed = string(raw)
	}

	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	result := upstreamResult{
		OK:     ok,
		Status: resp.StatusCode,
		Body:   parsed,
	}
	if !ok {
		result.Error = "OpenClaw returned non-2xx status"
	}

	return result
}

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

// writeJSON is the single response path. Every response carries an "ok"
// boolean; errors add "error", validation failures add "details".
func writeJSON(w http.ResponseWriter, statusCode int, body any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(body)
}

// getString reads a string field, tolerating numbers so a numeric flowId is
// not silently dropped. Any other type yields "".
func getString(raw map[string]any, key string) string {
	v, ok := raw[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	default:
		return ""
	}
}

// getInt64 reads an integer field. It returns nil when the key is absent or
// not a whole number, so callers can tell "omitted" apart from zero — which
// matters for expectedRevision, where 0 is a legitimate revision.
func getInt64(raw map[string]any, key string) *int64 {
	v, ok := raw[key]
	if !ok || v == nil {
		return nil
	}
	var s string
	switch t := v.(type) {
	case json.Number:
		s = t.String()
	case string:
		s = strings.TrimSpace(t)
	default:
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

// getMap reads an object field, returning an empty map rather than nil so
// callers never have to nil-check.
func getMap(raw map[string]any, key string) map[string]any {
	v, ok := raw[key]
	if !ok || v == nil {
		return map[string]any{}
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return obj
}

// ensureEOF rejects trailing data after the first JSON value, so a body like
// "{...}{...}" fails loudly instead of having its second half ignored.
func ensureEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("extra data")
		}
		return err
	}
	return nil
}

func isRequestBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return true
	}
	return strings.Contains(err.Error(), "http: request body too large")
}
