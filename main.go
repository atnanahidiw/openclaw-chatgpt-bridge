package main

import (
	"bytes"
	"context"
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

var allowedActions = map[string]struct{}{
	"create_flow": {},
	"run_task":    {},
	"get_flow":    {},
	"resume_flow": {},
	"finish_flow": {},
}

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

type config struct {
	serviceName        string
	port               int
	addr               string
	requestTimeout     time.Duration
	maxBodyBytes       int64
	openclawWebhookURL string
	openclawSecret     string
	sessionKey         string
	tailscaleEnabled   bool
	tailscaleProxyAddr string
	httpClient         *http.Client
}

type normalizedPayload struct {
	Action       string
	FlowID       string
	SessionKey   string
	Goal         string
	Task         string
	Status       string
	NotifyPolicy string
	Metadata     map[string]any
}

type validationResult struct {
	OK     bool     `json:"ok"`
	Errors []string `json:"errors,omitempty"`
}

type upstreamResult struct {
	OK     bool   `json:"ok"`
	Status int    `json:"status"`
	Body   any    `json:"body,omitempty"`
	Error  string `json:"error,omitempty"`
}

func main() {
	cfg := loadConfig()
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

func newMux(cfg config) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"service": cfg.serviceName,
		})
	})

	mux.HandleFunc("/version", versionHandler)

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

	mux.HandleFunc("/v1/openclaw", func(w http.ResponseWriter, r *http.Request) {
		handleOpenClaw(w, r, cfg)
	})

	return mux
}

func loadConfig() config {
	timeout := time.Duration(envInt("REQUEST_TIMEOUT_MS", 30000)) * time.Millisecond
	return config{
		serviceName:        "openclaw-chatgpt-bridge",
		port:               envInt("PORT", 8080),
		addr:               envString("ADDR", ":8080"),
		requestTimeout:     timeout,
		maxBodyBytes:       int64(envInt("MAX_BODY_BYTES", 1024*1024)),
		openclawWebhookURL: strings.TrimSpace(os.Getenv("OPENCLAW_WEBHOOK_URL")),
		openclawSecret:     strings.TrimSpace(os.Getenv("OPENCLAW_WEBHOOK_SECRET")),
		sessionKey:         strings.TrimSpace(os.Getenv("OPENCLAW_SESSION_KEY")),
		tailscaleEnabled:   strings.EqualFold(strings.TrimSpace(os.Getenv("TAILSCALE_ENABLED")), "true"),
		tailscaleProxyAddr: func() string {
			addr := strings.TrimSpace(os.Getenv("TAILSCALE_PROXY_ADDR"))
			if addr == "" {
				return "127.0.0.1:1055"
			}
			return addr
		}(),
		httpClient: &http.Client{Timeout: timeout},
	}
}

func envString(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

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

func normalizeInboundPayload(raw map[string]any, defaultSessionKey string) normalizedPayload {
	payload := normalizedPayload{
		Action:       getString(raw, "action"),
		FlowID:       getString(raw, "flowId"),
		SessionKey:   getString(raw, "sessionKey"),
		Goal:         getString(raw, "goal"),
		Task:         getString(raw, "task"),
		Status:       getString(raw, "status"),
		NotifyPolicy: getString(raw, "notifyPolicy"),
		Metadata:     getMap(raw, "metadata"),
	}

	if payload.SessionKey == "" {
		payload.SessionKey = defaultSessionKey
	}
	if payload.NotifyPolicy == "" {
		payload.NotifyPolicy = "done_only"
	}

	return payload
}

func validateInboundPayload(payload normalizedPayload) validationResult {
	var errs []string

	if payload.Action == "" {
		errs = append(errs, "action is required")
	} else if _, ok := allowedActions[payload.Action]; !ok {
		errs = append(errs, "action must be one of: create_flow, run_task, get_flow, resume_flow, finish_flow")
	}

	switch payload.Action {
	case "create_flow":
		if payload.Goal == "" {
			errs = append(errs, "goal is required for create_flow")
		}
	case "run_task":
		if payload.FlowID == "" {
			errs = append(errs, "flowId is required for run_task")
		}
		if payload.Task == "" {
			errs = append(errs, "task is required for run_task")
		}
	case "get_flow", "resume_flow", "finish_flow":
		if payload.FlowID == "" {
			errs = append(errs, "flowId is required for "+payload.Action)
		}
	}

	if payload.NotifyPolicy != "" {
		if _, ok := allowedNotifyPolicies[payload.NotifyPolicy]; !ok {
			errs = append(errs, "notifyPolicy must be one of: done_only, all_events, silent")
		}
	}

	return validationResult{
		OK:     len(errs) == 0,
		Errors: errs,
	}
}

func buildOpenClawPayload(payload normalizedPayload) map[string]any {
	outbound := map[string]any{
		"action":       payload.Action,
		"notifyPolicy": payload.NotifyPolicy,
		"metadata":     payload.Metadata,
	}

	if payload.FlowID != "" {
		outbound["flowId"] = payload.FlowID
	}
	if payload.SessionKey != "" {
		outbound["sessionKey"] = payload.SessionKey
	}
	if payload.Goal != "" {
		outbound["goal"] = payload.Goal
	}
	if payload.Task != "" {
		outbound["task"] = payload.Task
	}
	if payload.Status != "" {
		outbound["status"] = payload.Status
	}

	return outbound
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

	var parsed any
	if strings.Contains(resp.Header.Get("content-type"), "application/json") {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			parsed = string(raw)
		}
	} else {
		parsed = string(raw)
	}

	return upstreamResult{
		OK:     resp.StatusCode >= 200 && resp.StatusCode < 300,
		Status: resp.StatusCode,
		Body:   parsed,
		Error: func() string {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return ""
			}
			return "OpenClaw returned non-2xx status"
		}(),
	}
}

func handleOpenClaw(w http.ResponseWriter, r *http.Request, cfg config) {
	start := time.Now()
	requestID := strings.TrimSpace(r.Header.Get("x-request-id"))
	if requestID == "" {
		requestID = strconv.FormatInt(time.Now().UnixNano(), 10)
	}

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

	if cfg.openclawWebhookURL == "" {
		statusCode = http.StatusInternalServerError
		errMsg = "OPENCLAW_WEBHOOK_URL is not set"
		writeJSON(w, statusCode, map[string]any{
			"ok":    false,
			"error": errMsg,
		})
		return
	}

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

var allowedNotifyPolicies = map[string]struct{}{
	"done_only":  {},
	"all_events": {},
	"silent":     {},
}

func isRequestBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return true
	}
	return strings.Contains(err.Error(), "http: request body too large")
}

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

func writeJSON(w http.ResponseWriter, statusCode int, body any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(body)
}
