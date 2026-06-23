package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeCreateFlow(t *testing.T) {
	normalized := normalizeInboundPayload(map[string]any{
		"action": "create_flow",
		"goal":   "Build UMKM finance app MVP",
		"metadata": map[string]any{
			"source": "chatgpt",
		},
	}, "agent:main:main")

	if normalized.Action != "create_flow" {
		t.Fatalf("expected action create_flow, got %q", normalized.Action)
	}
	if normalized.SessionKey != "agent:main:main" {
		t.Fatalf("expected default session key, got %q", normalized.SessionKey)
	}
	if normalized.NotifyPolicy != "done_only" {
		t.Fatalf("expected default notify policy, got %q", normalized.NotifyPolicy)
	}
}

func TestValidateRunTask(t *testing.T) {
	normalized := normalizeInboundPayload(map[string]any{
		"action": "run_task",
		"flowId": "flow_123",
		"task":   "Write database schema",
		"status": "queued",
	}, "")

	result := validateInboundPayload(normalized)
	if !result.OK {
		t.Fatalf("expected payload valid, got errors: %#v", result.Errors)
	}
}

func TestRejectInvalidAction(t *testing.T) {
	normalized := normalizeInboundPayload(map[string]any{
		"action": "bogus",
	}, "")

	result := validateInboundPayload(normalized)
	if result.OK {
		t.Fatalf("expected invalid payload")
	}
	if len(result.Errors) == 0 {
		t.Fatalf("expected validation errors")
	}
}

func TestValidateFlowActionsRequireFlowID(t *testing.T) {
	for _, action := range []string{"get_flow", "resume_flow", "finish_flow"} {
		t.Run(action, func(t *testing.T) {
			normalized := normalizeInboundPayload(map[string]any{
				"action": action,
			}, "")

			result := validateInboundPayload(normalized)
			if result.OK {
				t.Fatalf("expected %s to require flowId", action)
			}
		})
	}
}

func TestValidateNotifyPolicy(t *testing.T) {
	normalized := normalizeInboundPayload(map[string]any{
		"action":       "create_flow",
		"goal":         "Build app",
		"notifyPolicy": "unsupported",
	}, "")

	result := validateInboundPayload(normalized)
	if result.OK {
		t.Fatalf("expected invalid notify policy")
	}
}

func TestForwardToOpenClaw(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Request-ID"); got != "test-request-123" {
			t.Fatalf("unexpected request id header: %q", got)
		}
		if got := r.Header.Get("authorization"); got != "Bearer secret-123" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		if got := r.Header.Get("x-openclaw-webhook-secret"); got != "secret-123" {
			t.Fatalf("unexpected secret header: %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream payload: %v", err)
		}
		if payload["action"] != "create_flow" {
			t.Fatalf("unexpected action: %#v", payload["action"])
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer upstream.Close()

	cfg := config{
		serviceName:        "openclaw-chatgpt-bridge",
		port:               8080,
		requestTimeout:     2 * time.Second,
		maxBodyBytes:       1 << 20,
		openclawWebhookURL: upstream.URL,
		openclawSecret:     "secret-123",
		sessionKey:         "agent:main:main",
	}

	result := forwardToOpenClaw(context.Background(), cfg, "test-request-123", buildOpenClawPayload(normalizeInboundPayload(map[string]any{
		"action": "create_flow",
		"goal":   "Build app",
	}, cfg.sessionKey)))

	if !result.OK {
		t.Fatalf("expected upstream success, got: %#v", result)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", result.Status)
	}
}

func TestOpenClawEndpointMethodNotAllowed(t *testing.T) {
	server := httptest.NewServer(newMux(config{
		serviceName:    "openclaw-chatgpt-bridge",
		requestTimeout: 2 * time.Second,
		maxBodyBytes:   1 << 20,
		sessionKey:     "agent:main:main",
	}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/openclaw")
	if err != nil {
		t.Fatalf("GET /v1/openclaw: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestVersionEndpoint(t *testing.T) {
	server := httptest.NewServer(newMux(config{
		serviceName:    "openclaw-chatgpt-bridge",
		requestTimeout: 2 * time.Second,
		maxBodyBytes:   1 << 20,
		sessionKey:     "agent:main:main",
	}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/version")
	if err != nil {
		t.Fatalf("GET /version: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode version response: %v", err)
	}
	if body["name"] != "openclaw-chatgpt-bridge" {
		t.Fatalf("expected name to match, got %#v", body["name"])
	}
	if body["version"] == "" {
		t.Fatalf("expected non-empty version, got %#v", body["version"])
	}
	if body["commit"] == "" {
		t.Fatalf("expected non-empty commit, got %#v", body["commit"])
	}
	if body["buildTime"] == "" {
		t.Fatalf("expected non-empty buildTime, got %#v", body["buildTime"])
	}
}

func TestVersionEndpointMethodNotAllowed(t *testing.T) {
	server := httptest.NewServer(newMux(config{
		serviceName:    "openclaw-chatgpt-bridge",
		requestTimeout: 2 * time.Second,
		maxBodyBytes:   1 << 20,
		sessionKey:     "agent:main:main",
	}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/version", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /version: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestOpenClawEndpointForwardsRequestID(t *testing.T) {
	observedRequestID := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedRequestID = r.Header.Get("X-Request-ID")
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer upstream.Close()

	server := httptest.NewServer(newMux(config{
		serviceName:        "openclaw-chatgpt-bridge",
		requestTimeout:     2 * time.Second,
		maxBodyBytes:       1 << 20,
		openclawWebhookURL: upstream.URL,
		sessionKey:         "agent:main:main",
		httpClient:         upstream.Client(),
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/openclaw", strings.NewReader(`{"action":"create_flow","goal":"Build app"}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "test-request-123")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/openclaw: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if observedRequestID != "test-request-123" {
		t.Fatalf("expected request id to be forwarded, got %q", observedRequestID)
	}
}

func TestOpenClawEndpointGeneratesRequestID(t *testing.T) {
	observedRequestID := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedRequestID = r.Header.Get("X-Request-ID")
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer upstream.Close()

	server := httptest.NewServer(newMux(config{
		serviceName:        "openclaw-chatgpt-bridge",
		requestTimeout:     2 * time.Second,
		maxBodyBytes:       1 << 20,
		openclawWebhookURL: upstream.URL,
		sessionKey:         "agent:main:main",
		httpClient:         upstream.Client(),
	}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/v1/openclaw", "application/json", strings.NewReader(`{"action":"create_flow","goal":"Build app"}`))
	if err != nil {
		t.Fatalf("POST /v1/openclaw: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if observedRequestID == "" {
		t.Fatalf("expected generated request id to be forwarded")
	}
}

func TestOpenClawEndpointInvalidJSON(t *testing.T) {
	server := httptest.NewServer(newMux(config{
		serviceName:        "openclaw-chatgpt-bridge",
		requestTimeout:     2 * time.Second,
		maxBodyBytes:       1 << 20,
		openclawWebhookURL: "http://example.invalid",
		sessionKey:         "agent:main:main",
		httpClient:         &http.Client{Timeout: 2 * time.Second},
	}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/v1/openclaw", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatalf("POST /v1/openclaw: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestOpenClawEndpointMissingAction(t *testing.T) {
	server := httptest.NewServer(newMux(config{
		serviceName:        "openclaw-chatgpt-bridge",
		requestTimeout:     2 * time.Second,
		maxBodyBytes:       1 << 20,
		openclawWebhookURL: "http://example.invalid",
		sessionKey:         "agent:main:main",
		httpClient:         &http.Client{Timeout: 2 * time.Second},
	}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/v1/openclaw", "application/json", strings.NewReader(`{"goal":"Build app"}`))
	if err != nil {
		t.Fatalf("POST /v1/openclaw: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestOpenClawEndpointMissingWebhookURL(t *testing.T) {
	server := httptest.NewServer(newMux(config{
		serviceName:    "openclaw-chatgpt-bridge",
		requestTimeout: 2 * time.Second,
		maxBodyBytes:   1 << 20,
		sessionKey:     "agent:main:main",
	}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/v1/openclaw", "application/json", strings.NewReader(`{"action":"create_flow","goal":"Build app"}`))
	if err != nil {
		t.Fatalf("POST /v1/openclaw: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestOpenClawEndpointUpstream401(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "unauthorized",
		})
	}))
	defer upstream.Close()

	server := httptest.NewServer(newMux(config{
		serviceName:        "openclaw-chatgpt-bridge",
		requestTimeout:     2 * time.Second,
		maxBodyBytes:       1 << 20,
		openclawWebhookURL: upstream.URL,
		sessionKey:         "agent:main:main",
		httpClient:         upstream.Client(),
	}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/v1/openclaw", "application/json", strings.NewReader(`{"action":"create_flow","goal":"Build app"}`))
	if err != nil {
		t.Fatalf("POST /v1/openclaw: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body["ok"] != false {
		t.Fatalf("expected ok=false, got %#v", body["ok"])
	}
	if body["error"] == "" {
		t.Fatalf("expected useful error body, got %#v", body)
	}
	if body["upstreamStatus"] != float64(http.StatusUnauthorized) {
		t.Fatalf("expected upstreamStatus 401, got %#v", body["upstreamStatus"])
	}
}

func TestOpenClawEndpointOversizedBody(t *testing.T) {
	server := httptest.NewServer(newMux(config{
		serviceName:        "openclaw-chatgpt-bridge",
		requestTimeout:     2 * time.Second,
		maxBodyBytes:       16,
		openclawWebhookURL: "http://example.invalid",
		sessionKey:         "agent:main:main",
		httpClient:         &http.Client{Timeout: 2 * time.Second},
	}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/v1/openclaw", "application/json", strings.NewReader(`{"action":"create_flow","goal":"`+strings.Repeat("x", 128)+`"}`))
	if err != nil {
		t.Fatalf("POST /v1/openclaw: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 400 or 413, got %d", resp.StatusCode)
	}
}
