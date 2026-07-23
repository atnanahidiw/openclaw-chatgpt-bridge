// Command openclaw-chatgpt-bridge is an HTTP service that sits between a
// ChatGPT Custom GPT Action and an OpenClaw Gateway.
//
// The request flow, and the order of this file, is:
//
//	main       -> start the server, shut down cleanly on SIGINT/SIGTERM
//	config     -> read every environment variable once, at startup
//	routing    -> /healthz, /readyz, /version, /v1/openclaw
//	auth       -> the shared api_key callers must present
//	handling   -> authenticate, decode, validate, dispatch, respond
//	payload    -> the request contract: ask, ask_async, get_result
//	jobs       -> in-memory store backing the async actions
//	gateway    -> the single hop to OpenClaw's chat completions endpoint
//	helpers    -> JSON encode/decode plumbing
//
// The bridge calls OpenClaw's OpenAI-compatible endpoint, which runs a real
// agent turn — tools, skills and all — and returns what the agent said. That
// endpoint is full operator access, so the gateway token stays private here
// and callers are re-authenticated with BRIDGE_API_KEY.
//
// Async jobs live in memory. A restart drops them, and a second replica cannot
// see the first replica's jobs, so run a single replica.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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
	"sync"
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

	// Without an inbound key anyone who learns the URL can drive the agent,
	// because the bridge supplies the gateway token itself.
	if cfg.bridgeAPIKey == "" {
		log.Printf("WARNING: BRIDGE_API_KEY is not set; POST /v1/openclaw accepts unauthenticated requests")
	}
	if cfg.gatewayToken == "" {
		log.Printf("WARNING: OPENCLAW_GATEWAY_TOKEN is not set; every OpenClaw call will fail authentication")
	}

	jobs := newJobStore(cfg.jobTTL)
	stopJanitor := jobs.startJanitor(cfg.jobTTL / 4)
	defer stopJanitor()

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           newMux(cfg, jobs),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		// A sync turn can legitimately take minutes, so the write budget has
		// to exceed the sync timeout or the response is cut off mid-flight.
		WriteTimeout: cfg.requestTimeout + 30*time.Second,
		IdleTimeout:  120 * time.Second,
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
	addr           string
	requestTimeout time.Duration
	asyncTimeout   time.Duration
	jobTTL         time.Duration
	maxBodyBytes   int64

	// OpenClaw Gateway base URL, e.g. https://host.tailnet.ts.net. The bridge
	// appends /v1/chat/completions itself.
	gatewayURL   string
	gatewayToken string
	agentTarget  string
	sessionKey   string

	// Shared secret callers must present on /v1/openclaw. Empty disables the
	// check, which leaves the endpoint open to anyone who knows the URL.
	bridgeAPIKey string

	tailscaleEnabled   bool
	tailscaleProxyAddr string

	httpClient *http.Client
}

func loadConfig() config {
	return config{
		serviceName: "openclaw-chatgpt-bridge",
		// The listener binds addr, not port. Use ":8080" to be reachable
		// across the pod network or a tailnet; bind 127.0.0.1 only behind a
		// local proxy.
		addr: envString("ADDR", ":8080"),
		// A trivial turn takes seconds; one that runs commands or edits files
		// takes far longer, so the sync budget is generous by default.
		requestTimeout: time.Duration(envInt("REQUEST_TIMEOUT_MS", 120000)) * time.Millisecond,
		asyncTimeout:   time.Duration(envInt("ASYNC_TIMEOUT_MS", 900000)) * time.Millisecond,
		jobTTL:         time.Duration(envInt("JOB_TTL_MS", 3600000)) * time.Millisecond,
		maxBodyBytes:   int64(envInt("MAX_BODY_BYTES", 1024*1024)),

		gatewayURL:   strings.TrimRight(strings.TrimSpace(os.Getenv("OPENCLAW_GATEWAY_URL")), "/"),
		gatewayToken: strings.TrimSpace(os.Getenv("OPENCLAW_GATEWAY_TOKEN")),
		agentTarget:  envString("OPENCLAW_AGENT", "openclaw/default"),
		sessionKey:   strings.TrimSpace(os.Getenv("OPENCLAW_SESSION_KEY")),

		bridgeAPIKey: strings.TrimSpace(os.Getenv("BRIDGE_API_KEY")),

		tailscaleEnabled:   strings.EqualFold(strings.TrimSpace(os.Getenv("TAILSCALE_ENABLED")), "true"),
		tailscaleProxyAddr: envString("TAILSCALE_PROXY_ADDR", "127.0.0.1:1055"),

		// Timeout is applied per call, because async turns need a longer
		// budget than sync ones.
		httpClient: &http.Client{},
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

func newMux(cfg config, jobs *jobStore) *http.ServeMux {
	mux := http.NewServeMux()

	// Liveness: the process is up.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"service": cfg.serviceName,
		})
	})

	// Readiness: the process is up *and* the private path out is dialable.
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
		handleOpenClaw(w, r, cfg, jobs)
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
// Inbound authentication
//
// The OpenAPI schema declares an apiKey scheme carrying the secret in an
// `api_key` header, which is what the Custom GPT sends:
//
//	Authentication = "API Key", Auth Type "Custom", header name "api_key"
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
// Request handling
// ---------------------------------------------------------------------------

// handleOpenClaw is the only functional route.
func handleOpenClaw(w http.ResponseWriter, r *http.Request, cfg config, jobs *jobStore) {
	start := time.Now()

	// Propagate the caller's trace ID, or mint one if absent.
	requestID := strings.TrimSpace(r.Header.Get("x-request-id"))
	if requestID == "" {
		requestID = newID()
	}

	// Every request emits exactly one structured log line, from this defer.
	// Add fields here rather than logging ad hoc mid-handler, and never log
	// the gateway token, the api key, or message bodies.
	statusCode := http.StatusOK
	action := ""
	sessionKey := ""
	jobID := ""
	errMsg := ""
	defer func() {
		log.Printf(
			"request_id=%s action=%s session_key=%s job_id=%s status=%d duration_ms=%d error=%q",
			requestID, action, sessionKey, jobID, statusCode,
			time.Since(start).Milliseconds(), errMsg,
		)
	}()

	if r.Method != http.MethodPost {
		statusCode = http.StatusMethodNotAllowed
		errMsg = "method not allowed"
		writeJSON(w, statusCode, map[string]any{"ok": false, "error": errMsg})
		return
	}

	// Authenticate before anything else, so an unauthenticated caller learns
	// nothing about how the bridge is configured.
	if !authorizeRequest(r, cfg.bridgeAPIKey) {
		statusCode = http.StatusUnauthorized
		errMsg = "unauthorized"
		w.Header().Set("www-authenticate", `Bearer realm="openclaw-bridge"`)
		writeJSON(w, statusCode, map[string]any{"ok": false, "error": errMsg})
		return
	}

	if cfg.gatewayURL == "" {
		statusCode = http.StatusInternalServerError
		errMsg = "OPENCLAW_GATEWAY_URL is not set"
		writeJSON(w, statusCode, map[string]any{"ok": false, "error": errMsg})
		return
	}

	// Cap the body, and decode numbers as json.Number so large ids survive
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
		writeJSON(w, statusCode, map[string]any{"ok": false, "error": errMsg})
		return
	}

	if err := ensureEOF(dec); err != nil {
		statusCode = http.StatusBadRequest
		errMsg = "request body must be a single JSON object"
		writeJSON(w, statusCode, map[string]any{"ok": false, "error": errMsg})
		return
	}

	payload := normalizeInboundPayload(raw, cfg.sessionKey)
	action = payload.Action
	sessionKey = payload.SessionKey
	jobID = payload.JobID

	if result := validateInboundPayload(payload); !result.OK {
		statusCode = http.StatusBadRequest
		errMsg = "invalid request"
		writeJSON(w, statusCode, map[string]any{
			"ok": false, "error": errMsg, "details": result.Errors,
		})
		return
	}

	switch payload.Action {
	case actionAsk:
		ctx, cancel := context.WithTimeout(r.Context(), cfg.requestTimeout)
		defer cancel()

		reply, err := askGateway(ctx, cfg, payload)
		if err != nil {
			statusCode = statusForGatewayError(err)
			errMsg = err.Error()
			writeJSON(w, statusCode, map[string]any{"ok": false, "error": errMsg})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"bridge":     cfg.serviceName,
			"reply":      reply,
			"sessionKey": payload.SessionKey,
		})

	case actionAskAsync:
		created := jobs.start(payload.SessionKey)
		jobID = created.ID
		statusCode = http.StatusAccepted

		// Deliberately detached from the request context: the caller gets an
		// id immediately and the turn keeps running after they disconnect.
		go func(p normalizedPayload, id string) {
			ctx, cancel := context.WithTimeout(context.Background(), cfg.asyncTimeout)
			defer cancel()

			reply, err := askGateway(ctx, cfg, p)
			if err != nil {
				jobs.fail(id, err.Error())
				log.Printf("request_id=%s job_id=%s session_key=%s status=failed error=%q",
					requestID, id, p.SessionKey, err.Error())
				return
			}
			jobs.finish(id, reply)
			log.Printf("request_id=%s job_id=%s session_key=%s status=done reply_bytes=%d",
				requestID, id, p.SessionKey, len(reply))
		}(payload, created.ID)

		writeJSON(w, statusCode, map[string]any{
			"ok":         true,
			"bridge":     cfg.serviceName,
			"jobId":      created.ID,
			"status":     jobStatusRunning,
			"sessionKey": payload.SessionKey,
			"hint":       "poll get_result with this jobId until status is done or failed",
		})

	case actionGetResult:
		found, ok := jobs.get(payload.JobID)
		if !ok {
			statusCode = http.StatusNotFound
			errMsg = "unknown jobId; it may have expired, or the bridge restarted"
			writeJSON(w, statusCode, map[string]any{"ok": false, "error": errMsg})
			return
		}

		body := map[string]any{
			"ok":         true,
			"bridge":     cfg.serviceName,
			"jobId":      found.ID,
			"status":     found.Status,
			"startedAt":  found.CreatedAt.UTC().Format(time.RFC3339),
			"elapsedMs":  found.elapsed().Milliseconds(),
			"sessionKey": found.SessionKey,
		}
		switch found.Status {
		case jobStatusDone:
			body["reply"] = found.Reply
		case jobStatusFailed:
			// Surface the failure rather than reporting a bare status, so the
			// caller can tell a timeout from a bad credential without digging
			// through bridge logs it cannot see.
			body["ok"] = false
			body["error"] = found.Err
			body["failedAt"] = found.EndedAt.UTC().Format(time.RFC3339)
			body["hint"] = "this turn will not complete; read error and retry only if it is transient"
			errMsg = found.Err
		default:
			body["hint"] = "still running; poll again in a few seconds"
		}
		writeJSON(w, http.StatusOK, body)
	}
}

// ---------------------------------------------------------------------------
// Payload contract
//
// Three actions, all thin wrappers over one OpenClaw chat completions call:
//
//	ask         run a turn and return the reply. Bounded by REQUEST_TIMEOUT_MS
//	ask_async   start a turn, return a jobId immediately
//	get_result  read an async turn's reply by jobId
//
// Changing anything here means changing three places in the same commit: the
// checks below, openapi/openclaw-bridge.openapi.yaml, and the error strings
// that enumerate valid values. Tests assert on those strings.
// ---------------------------------------------------------------------------

const (
	actionAsk       = "ask"
	actionAskAsync  = "ask_async"
	actionGetResult = "get_result"
)

var allowedActions = map[string]struct{}{
	actionAsk:       {},
	actionAskAsync:  {},
	actionGetResult: {},
}

// Reserved by the gateway; it rejects these with a 400, so catch them here
// where the error can name the actual problem.
var reservedSessionPrefixes = []string{"subagent:", "cron:", "acp:"}

// normalizedPayload is the inbound GPT Action body after coercion and defaults.
type normalizedPayload struct {
	Action     string
	Message    string
	SessionKey string
	User       string
	JobID      string
}

type validationResult struct {
	OK     bool     `json:"ok"`
	Errors []string `json:"errors,omitempty"`
}

// normalizeInboundPayload coerces the decoded body and applies defaults:
// sessionKey falls back to OPENCLAW_SESSION_KEY.
func normalizeInboundPayload(raw map[string]any, defaultSessionKey string) normalizedPayload {
	payload := normalizedPayload{
		Action:     getString(raw, "action"),
		Message:    getString(raw, "message"),
		SessionKey: getString(raw, "sessionKey"),
		User:       getString(raw, "user"),
		JobID:      getString(raw, "jobId"),
	}
	if payload.SessionKey == "" {
		payload.SessionKey = defaultSessionKey
	}
	return payload
}

// validateInboundPayload collects every problem rather than failing on the
// first, so the caller gets one complete list.
func validateInboundPayload(payload normalizedPayload) validationResult {
	var errs []string

	if payload.Action == "" {
		errs = append(errs, "action is required")
	} else if _, ok := allowedActions[payload.Action]; !ok {
		errs = append(errs, "action must be one of: ask, ask_async, get_result")
	}

	switch payload.Action {
	case actionAsk, actionAskAsync:
		if payload.Message == "" {
			errs = append(errs, "message is required for "+payload.Action)
		}
	case actionGetResult:
		if payload.JobID == "" {
			errs = append(errs, "jobId is required for get_result")
		}
	}

	for _, reserved := range reservedSessionPrefixes {
		if strings.Contains(payload.SessionKey, reserved) {
			errs = append(errs, "sessionKey must not use a reserved namespace: subagent:, cron:, acp:")
			break
		}
	}

	return validationResult{OK: len(errs) == 0, Errors: errs}
}

// ---------------------------------------------------------------------------
// Async job store
//
// In memory on purpose: the bridge has no database and this is the smallest
// thing that works. The consequences are real, so they are documented rather
// than hidden — a restart drops every job, and a second replica cannot see
// the first replica's jobs. Run one replica.
// ---------------------------------------------------------------------------

const (
	jobStatusRunning = "running"
	jobStatusDone    = "done"
	jobStatusFailed  = "failed"
)

type job struct {
	ID         string
	Status     string
	Reply      string
	Err        string
	SessionKey string
	CreatedAt  time.Time
	EndedAt    time.Time
}

// elapsed is how long the turn ran, or has been running so far.
func (j job) elapsed() time.Duration {
	if j.EndedAt.IsZero() {
		return time.Since(j.CreatedAt)
	}
	return j.EndedAt.Sub(j.CreatedAt)
}

type jobStore struct {
	mu   sync.Mutex
	jobs map[string]*job
	ttl  time.Duration
}

func newJobStore(ttl time.Duration) *jobStore {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &jobStore{jobs: map[string]*job{}, ttl: ttl}
}

func (s *jobStore) start(sessionKey string) job {
	s.mu.Lock()
	defer s.mu.Unlock()

	j := &job{ID: newID(), Status: jobStatusRunning, SessionKey: sessionKey, CreatedAt: time.Now()}
	s.jobs[j.ID] = j
	return *j
}

func (s *jobStore) finish(id, reply string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if j, ok := s.jobs[id]; ok {
		j.Status = jobStatusDone
		j.Reply = reply
		j.EndedAt = time.Now()
	}
}

func (s *jobStore) fail(id, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if j, ok := s.jobs[id]; ok {
		j.Status = jobStatusFailed
		j.Err = msg
		j.EndedAt = time.Now()
	}
}

func (s *jobStore) get(id string) (job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return job{}, false
	}
	return *j, true
}

// sweep drops finished jobs past their TTL. Running jobs are kept regardless,
// so a slow turn is never collected out from under a poller.
func (s *jobStore) sweep(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for id, j := range s.jobs {
		if j.Status == jobStatusRunning {
			continue
		}
		if now.Sub(j.EndedAt) > s.ttl {
			delete(s.jobs, id)
			removed++
		}
	}
	return removed
}

func (s *jobStore) startJanitor(every time.Duration) func() {
	if every <= 0 {
		every = 15 * time.Minute
	}
	ticker := time.NewTicker(every)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				s.sweep(time.Now())
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	return func() { close(done) }
}

// ---------------------------------------------------------------------------
// OpenClaw gateway hop
// ---------------------------------------------------------------------------

var (
	errGatewayUnauthorized = errors.New("OpenClaw rejected the bridge's gateway token")
	errGatewayTimeout      = errors.New("OpenClaw did not reply in time")
	errGatewayEmptyReply   = errors.New("OpenClaw returned no reply text")
)

// askGateway runs one agent turn and returns what the agent said.
func askGateway(ctx context.Context, cfg config, payload normalizedPayload) (string, error) {
	body := map[string]any{
		"model": cfg.agentTarget,
		"messages": []map[string]string{
			{"role": "user", "content": payload.Message},
		},
	}
	// The OpenAI `user` field gives the gateway a stable session per
	// conversation, so a multi-turn GPT chat keeps one OpenClaw session.
	if payload.User != "" {
		body["user"] = payload.User
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.gatewayURL+"/v1/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+cfg.gatewayToken)
	if payload.SessionKey != "" {
		req.Header.Set("x-openclaw-session-key", payload.SessionKey)
	}

	resp, err := cfg.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", errGatewayTimeout
		}
		return "", err
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", errGatewayUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("OpenClaw returned " + strconv.Itoa(resp.StatusCode) + ": " + firstLine(string(rawBody)))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rawBody, &parsed); err != nil {
		return "", errors.New("OpenClaw returned a response the bridge could not parse")
	}
	if len(parsed.Choices) == 0 {
		return "", errGatewayEmptyReply
	}

	reply := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if reply == "" {
		return "", errGatewayEmptyReply
	}
	return reply, nil
}

func statusForGatewayError(err error) int {
	switch {
	case errors.Is(err, errGatewayUnauthorized):
		// The caller authenticated fine; it is the bridge's own credential
		// that is wrong, which is a server-side misconfiguration.
		return http.StatusInternalServerError
	case errors.Is(err, errGatewayTimeout):
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
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

// getString reads a string field, tolerating numbers so a numeric id is not
// silently dropped. Any other type yields "".
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

// newID returns a random hex id. Falls back to a timestamp if the system
// entropy source fails, so an id is always produced.
func newID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(buf)
}

// firstLine keeps upstream error text to one short line, so a stray HTML page
// cannot flood the response or the log.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
