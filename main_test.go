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
		"action":  "run_task",
		"flowId":  "flow_123",
		"task":    "Write database schema",
		"runtime": "subagent",
		"status":  "queued",
	}, "")

	result := validateInboundPayload(normalized)
	if !result.OK {
		t.Fatalf("expected payload valid, got errors: %#v", result.Errors)
	}
}

func TestRunTaskRequiresRuntime(t *testing.T) {
	base := map[string]any{
		"action": "run_task",
		"flowId": "flow_123",
		"task":   "Write database schema",
	}

	if result := validateInboundPayload(normalizeInboundPayload(base, "")); result.OK {
		t.Fatalf("expected run_task without runtime to be rejected")
	}

	base["runtime"] = "not-a-runtime"
	if result := validateInboundPayload(normalizeInboundPayload(base, "")); result.OK {
		t.Fatalf("expected unknown runtime to be rejected")
	}
}

// resume_flow and finish_flow carry optimistic concurrency upstream, so the
// revision is required and 0 must be distinguishable from "omitted".
func TestResumeAndFinishRequireExpectedRevision(t *testing.T) {
	for _, action := range []string{"resume_flow", "finish_flow"} {
		t.Run(action, func(t *testing.T) {
			without := normalizeInboundPayload(map[string]any{
				"action": action,
				"flowId": "flow_123",
			}, "")
			if validateInboundPayload(without).OK {
				t.Fatalf("expected %s without expectedRevision to be rejected", action)
			}

			raw := map[string]any{"action": action, "flowId": "flow_123", "expectedRevision": json.Number("0")}
			zero := normalizeInboundPayload(raw, "")
			if zero.ExpectedRevision == nil || *zero.ExpectedRevision != 0 {
				t.Fatalf("expected revision 0 to be preserved, got %v", zero.ExpectedRevision)
			}
			if result := validateInboundPayload(zero); !result.OK {
				t.Fatalf("expected revision 0 to be valid, got %#v", result.Errors)
			}
			if got := buildOpenClawPayload(zero)["expectedRevision"]; got != int64(0) {
				t.Fatalf("expected expectedRevision 0 in outbound body, got %#v", got)
			}
		})
	}
}

// The upstream schemas are strict(): any key the action does not declare fails
// the whole request. This pins the exact key set the bridge emits.
func TestOutboundKeysMatchUpstreamSchema(t *testing.T) {
	accepted := map[string]map[string]bool{
		"create_flow": {"action": true, "goal": true, "controllerId": true, "status": true, "notifyPolicy": true, "currentStep": true, "stateJson": true, "waitJson": true},
		"run_task":    {"action": true, "flowId": true, "runtime": true, "task": true, "childSessionKey": true, "label": true, "sourceId": true, "parentTaskId": true, "agentId": true, "runId": true, "preferMetadata": true, "notifyPolicy": true, "status": true, "startedAt": true, "lastEventAt": true, "progressSummary": true},
		"get_flow":    {"action": true, "flowId": true},
		"resume_flow": {"action": true, "flowId": true, "expectedRevision": true, "status": true, "currentStep": true, "stateJson": true},
		"finish_flow": {"action": true, "flowId": true, "expectedRevision": true, "stateJson": true},
	}

	// Deliberately over-supplies every inbound field, including the two the
	// bridge must never forward.
	inbound := map[string]any{
		"flowId":           "flow_123",
		"goal":             "Ship it",
		"task":             "Do the thing",
		"runtime":          "subagent",
		"childSessionKey":  "agent:main:worker",
		"sessionKey":       "agent:main:main",
		"notifyPolicy":     "done_only",
		"expectedRevision": json.Number("7"),
		"metadata":         map[string]any{"source": "chatgpt-action"},
	}

	for action, allowed := range accepted {
		t.Run(action, func(t *testing.T) {
			raw := map[string]any{}
			for k, v := range inbound {
				raw[k] = v
			}
			raw["action"] = action
			if action == "create_flow" || action == "run_task" {
				raw["status"] = "queued"
			}

			normalized := normalizeInboundPayload(raw, "agent:main:main")
			if result := validateInboundPayload(normalized); !result.OK {
				t.Fatalf("fixture should be valid, got %#v", result.Errors)
			}

			for key := range buildOpenClawPayload(normalized) {
				if !allowed[key] {
					t.Errorf("%s: emits %q, which upstream rejects with 400 Unrecognized keys", action, key)
				}
			}
		})
	}
}

// Neither is accepted by any upstream action.
func TestSessionKeyAndMetadataAreNeverForwarded(t *testing.T) {
	for _, action := range []string{"create_flow", "run_task", "get_flow", "resume_flow", "finish_flow"} {
		t.Run(action, func(t *testing.T) {
			normalized := normalizeInboundPayload(map[string]any{
				"action":           action,
				"flowId":           "flow_123",
				"goal":             "Ship it",
				"task":             "Do the thing",
				"runtime":          "acp",
				"expectedRevision": json.Number("3"),
				"sessionKey":       "agent:main:main",
				"metadata":         map[string]any{"source": "chatgpt-action"},
			}, "agent:main:main")

			outbound := buildOpenClawPayload(normalized)
			if _, ok := outbound["sessionKey"]; ok {
				t.Errorf("%s: sessionKey must not be forwarded", action)
			}
			if _, ok := outbound["metadata"]; ok {
				t.Errorf("%s: metadata must not be forwarded", action)
			}
		})
	}
}

// metadata has no upstream home, but three actions accept a free-form
// stateJson, so it should survive there rather than being silently dropped.
func TestMetadataBecomesStateJSON(t *testing.T) {
	meta := map[string]any{"source": "chatgpt-action"}

	for _, tc := range []struct {
		action string
		want   bool
	}{
		{"create_flow", true},
		{"resume_flow", true},
		{"finish_flow", true},
		{"get_flow", false},
		{"run_task", false},
	} {
		t.Run(tc.action, func(t *testing.T) {
			normalized := normalizeInboundPayload(map[string]any{
				"action":           tc.action,
				"flowId":           "flow_123",
				"goal":             "Ship it",
				"task":             "Do the thing",
				"runtime":          "subagent",
				"expectedRevision": json.Number("1"),
				"metadata":         meta,
			}, "")

			_, got := buildOpenClawPayload(normalized)["stateJson"]
			if got != tc.want {
				t.Fatalf("%s: stateJson present = %v, want %v", tc.action, got, tc.want)
			}
		})
	}
}

func TestNotifyPolicyMatchesUpstreamEnum(t *testing.T) {
	// "all_events" was never a real upstream value.
	for _, policy := range []string{"all_events", "unsupported"} {
		normalized := normalizeInboundPayload(map[string]any{
			"action":       "create_flow",
			"goal":         "Build app",
			"notifyPolicy": policy,
		}, "")
		if validateInboundPayload(normalized).OK {
			t.Fatalf("expected notifyPolicy %q to be rejected", policy)
		}
	}

	for _, policy := range []string{"done_only", "state_changes", "silent"} {
		normalized := normalizeInboundPayload(map[string]any{
			"action":       "create_flow",
			"goal":         "Build app",
			"notifyPolicy": policy,
		}, "")
		if result := validateInboundPayload(normalized); !result.OK {
			t.Fatalf("expected notifyPolicy %q to be valid, got %#v", policy, result.Errors)
		}
	}
}

// notifyPolicy is only declared by create_flow and run_task upstream.
func TestNotifyPolicyOnlySentWhereAccepted(t *testing.T) {
	for _, tc := range []struct {
		action string
		want   bool
	}{
		{"create_flow", true},
		{"run_task", true},
		{"get_flow", false},
		{"resume_flow", false},
		{"finish_flow", false},
	} {
		t.Run(tc.action, func(t *testing.T) {
			normalized := normalizeInboundPayload(map[string]any{
				"action":           tc.action,
				"flowId":           "flow_123",
				"goal":             "Ship it",
				"task":             "Do the thing",
				"runtime":          "subagent",
				"expectedRevision": json.Number("1"),
			}, "")

			_, got := buildOpenClawPayload(normalized)["notifyPolicy"]
			if got != tc.want {
				t.Fatalf("%s: notifyPolicy present = %v, want %v", tc.action, got, tc.want)
			}
		})
	}
}

func TestAuthorizeRequest(t *testing.T) {
	const key = "s3cr3t-bridge-key"

	newReq := func(headers map[string]string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/openclaw", nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		return r
	}

	cases := []struct {
		name     string
		expected string
		headers  map[string]string
		want     bool
	}{
		{"no key configured allows anything", "", nil, true},

		// The header the OpenAPI schema declares, so this is what the GPT sends.
		{"api_key header", key, map[string]string{"api_key": key}, true},
		{"wrong api_key header", key, map[string]string{"api_key": "nope"}, false},

		// Accepted fallback, for curl.
		{"bearer token", key, map[string]string{"Authorization": "Bearer " + key}, true},
		{"bearer is case-insensitive", key, map[string]string{"Authorization": "bearer " + key}, true},
		{"raw authorization value", key, map[string]string{"Authorization": key}, true},

		{"missing credential", key, nil, false},
		{"wrong key", key, map[string]string{"Authorization": "Bearer nope"}, false},
		{"empty bearer token", key, map[string]string{"Authorization": "Bearer "}, false},
		{"scheme only", key, map[string]string{"Authorization": "Bearer"}, false},
		{"key as prefix is not enough", key, map[string]string{"Authorization": "Bearer " + key + "extra"}, false},

		// api_key takes precedence, so a bad one is not rescued by a good Bearer.
		{"api_key wins over authorization", key, map[string]string{
			"api_key": "nope", "Authorization": "Bearer " + key}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := authorizeRequest(newReq(tc.headers), tc.expected); got != tc.want {
				t.Fatalf("authorizeRequest = %v, want %v", got, tc.want)
			}
		})
	}
}

// The endpoint must be closed when a key is configured, and the rejection must
// not depend on the rest of the request being valid.
func TestOpenClawEndpointRequiresAPIKey(t *testing.T) {
	var upstreamHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cfg := config{
		serviceName:        "openclaw-chatgpt-bridge",
		openclawWebhookURL: upstream.URL,
		bridgeAPIKey:       "expected-key",
		requestTimeout:     2 * time.Second,
		maxBodyBytes:       1024 * 1024,
		httpClient:         upstream.Client(),
	}
	mux := newMux(cfg)

	body := `{"action":"create_flow","goal":"x"}`

	t.Run("rejects without a key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/openclaw", strings.NewReader(body))
		req.Header.Set("content-type", "application/json")
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if got := rec.Header().Get("www-authenticate"); got == "" {
			t.Errorf("expected a www-authenticate challenge header")
		}
		if upstreamHits != 0 {
			t.Errorf("unauthenticated request reached OpenClaw %d times", upstreamHits)
		}
	})

	t.Run("accepts with the key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/openclaw", strings.NewReader(body))
		req.Header.Set("content-type", "application/json")
		req.Header.Set("Authorization", "Bearer expected-key")
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if upstreamHits != 1 {
			t.Errorf("upstream hits = %d, want 1", upstreamHits)
		}
	})

	// A bad key must fail before the bridge reveals configuration problems.
	t.Run("auth is checked before configuration", func(t *testing.T) {
		broken := newMux(config{serviceName: "x", bridgeAPIKey: "expected-key"}) // no webhook URL
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/openclaw", strings.NewReader(body))
		mux2 := broken
		mux2.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (not a 500 leaking config state)", rec.Code)
		}
	})
}

// Platform probes have no credentials, so these must stay open.
func TestHealthEndpointsStayOpenWithAPIKeySet(t *testing.T) {
	mux := newMux(config{serviceName: "openclaw-chatgpt-bridge", bridgeAPIKey: "expected-key"})

	for _, path := range []string{"/healthz", "/readyz", "/version"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want 200", path, rec.Code)
			}
		})
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
