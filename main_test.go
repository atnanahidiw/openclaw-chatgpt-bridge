package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// testConfig returns a config pointed at a stub gateway.
func testConfig(gatewayURL string) config {
	return config{
		serviceName:    "openclaw-chatgpt-bridge",
		gatewayURL:     strings.TrimRight(gatewayURL, "/"),
		gatewayToken:   "gateway-token",
		agentTarget:    "openclaw/default",
		sessionPrefix:  "agent:main:chatgpt",
		bridgeAPIKey:   "bridge-key",
		requestTimeout: 5 * time.Second,
		asyncTimeout:   5 * time.Second,
		jobTTL:         time.Hour,
		maxBodyBytes:   1024 * 1024,
		httpClient:     &http.Client{},
	}
}

// stubGateway answers like OpenClaw's chat completions endpoint.
func stubGateway(t *testing.T, reply string, seen func(*http.Request, map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if seen != nil {
			seen(r, body)
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": reply}, "finish_reason": "stop"},
			},
		})
	}))
}

func post(t *testing.T, mux *http.ServeMux, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/openclaw", strings.NewReader(body))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("api_key", "bridge-key")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	mux.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response was not JSON: %v (%s)", err, rec.Body.String())
	}
	return out
}

// ---------------------------------------------------------------------------
// Contract
// ---------------------------------------------------------------------------

func TestValidateActions(t *testing.T) {
	cases := []struct {
		name  string
		raw   map[string]any
		valid bool
	}{
		{"ask needs a message", map[string]any{"action": "ask"}, false},
		{"ask with message", map[string]any{"action": "ask", "message": "hi"}, true},
		{"ask_async needs a message", map[string]any{"action": "ask_async"}, false},
		{"ask_async with message", map[string]any{"action": "ask_async", "message": "hi"}, true},
		{"get_result needs a jobId", map[string]any{"action": "get_result"}, false},
		{"get_result with jobId", map[string]any{"action": "get_result", "jobId": "abc"}, true},
		{"missing action", map[string]any{"message": "hi"}, false},
		{"unknown action", map[string]any{"action": "run_task", "message": "hi"}, false},
		{"old flow action is gone", map[string]any{"action": "create_flow", "goal": "x"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := validateInboundPayload(normalizeInboundPayload(tc.raw, "")).OK
			if got != tc.valid {
				t.Fatalf("valid = %v, want %v", got, tc.valid)
			}
		})
	}
}

// A caller names a session; the bridge decides where it lands.
func TestSessionNameIsNamespaced(t *testing.T) {
	cases := []struct {
		name    string
		session any
		want    string
	}{
		{"blank uses the prefix alone", nil, "agent:main:chatgpt"},
		{"empty string uses the prefix alone", "", "agent:main:chatgpt"},
		{"a name is appended", "research", "agent:main:chatgpt:research"},
		{"dots dashes underscores allowed", "my_gpt-2.0", "agent:main:chatgpt:my_gpt-2.0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]any{"action": "ask", "message": "hi"}
			if tc.session != nil {
				raw["customSession"] = tc.session
			}
			p := normalizeInboundPayload(raw, "agent:main:chatgpt")
			if p.SessionKey != tc.want {
				t.Fatalf("sessionKey = %q, want %q", p.SessionKey, tc.want)
			}
			if !validateInboundPayload(p).OK {
				t.Fatalf("expected %v to be valid", tc.session)
			}
		})
	}
}

// The prefix is the security boundary: a caller must not be able to climb out
// of it and reach another agent's session.
func TestSessionNameCannotEscapeTheNamespace(t *testing.T) {
	for _, bad := range []string{
		"agent:main:main", // a full key
		"../main",         // traversal-ish
		"a:b",             // any colon at all
		"subagent:worker", // reserved upstream namespace
		"cron:nightly",
		"acp:thing",
		"has space",
		"", // handled separately, but must never produce a bare colon
	} {
		t.Run(bad, func(t *testing.T) {
			p := normalizeInboundPayload(map[string]any{
				"action": "ask", "message": "hi", "customSession": bad,
			}, "agent:main:chatgpt")

			if bad == "" {
				if p.SessionKey != "agent:main:chatgpt" {
					t.Fatalf("empty name should fall back to the prefix, got %q", p.SessionKey)
				}
				return
			}
			if validateInboundPayload(p).OK {
				t.Fatalf("expected %q to be rejected, built %q", bad, p.SessionKey)
			}
		})
	}
}

func TestPrefixFallsBackWhenUnset(t *testing.T) {
	p := normalizeInboundPayload(map[string]any{"action": "ask", "message": "hi"}, "")
	if p.SessionKey != defaultSessionPrefix {
		t.Fatalf("sessionKey = %q, want %q", p.SessionKey, defaultSessionPrefix)
	}
	// A trailing colon in the configured prefix must not double up.
	p = normalizeInboundPayload(map[string]any{"action": "ask", "message": "hi", "customSession": "x"},
		"agent:main:chatgpt:")
	if p.SessionKey != "agent:main:chatgpt:x" {
		t.Fatalf("sessionKey = %q, want no doubled colon", p.SessionKey)
	}
}

// ---------------------------------------------------------------------------
// ask
// ---------------------------------------------------------------------------

func TestAskReturnsTheReply(t *testing.T) {
	var gotHeader, gotModel, gotContent string
	gw := stubGateway(t, "pong", func(r *http.Request, body map[string]any) {
		gotHeader = r.Header.Get("x-openclaw-session-key")
		gotModel, _ = body["model"].(string)
		if msgs, ok := body["messages"].([]any); ok && len(msgs) > 0 {
			if m, ok := msgs[0].(map[string]any); ok {
				gotContent, _ = m["content"].(string)
			}
		}
		if got := r.Header.Get("authorization"); got != "Bearer gateway-token" {
			t.Errorf("gateway auth = %q, want the bridge's token", got)
		}
	})
	defer gw.Close()

	mux := newMux(testConfig(gw.URL), newJobStore(time.Hour))
	rec := post(t, mux, `{"action":"ask","message":"ping"}`, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	if body["reply"] != "pong" {
		t.Fatalf("reply = %v, want pong", body["reply"])
	}
	if gotHeader != "agent:main:chatgpt" {
		t.Errorf("session header = %q, want the configured session", gotHeader)
	}
	if gotModel != "openclaw/default" {
		t.Errorf("model = %q", gotModel)
	}
	if gotContent != "ping" {
		t.Errorf("message content = %q", gotContent)
	}
}

func TestAskMapsGatewayFailures(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		wantBridge int
	}{
		{"gateway rejects our token", http.StatusUnauthorized, http.StatusInternalServerError},
		{"gateway forbids", http.StatusForbidden, http.StatusInternalServerError},
		{"gateway errors", http.StatusInternalServerError, http.StatusBadGateway},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("nope"))
			}))
			defer gw.Close()

			mux := newMux(testConfig(gw.URL), newJobStore(time.Hour))
			rec := post(t, mux, `{"action":"ask","message":"hi"}`, nil)
			if rec.Code != tc.wantBridge {
				t.Fatalf("bridge status = %d, want %d", rec.Code, tc.wantBridge)
			}
			if decode(t, rec)["ok"] != false {
				t.Errorf("expected ok=false")
			}
		})
	}
}

// A 200 with no usable content is a failure, not an empty success.
func TestAskRejectsEmptyReply(t *testing.T) {
	for _, payload := range []string{`{"choices":[]}`, `{"choices":[{"message":{"content":"   "}}]}`} {
		gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(payload))
		}))

		mux := newMux(testConfig(gw.URL), newJobStore(time.Hour))
		rec := post(t, mux, `{"action":"ask","message":"hi"}`, nil)
		gw.Close()

		if rec.Code == http.StatusOK {
			t.Fatalf("empty reply should not be a 200, got %s", rec.Body.String())
		}
	}
}

// ---------------------------------------------------------------------------
// ask_async + get_result
// ---------------------------------------------------------------------------

func TestAsyncRoundTrip(t *testing.T) {
	release := make(chan struct{})
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // hold the turn open so we can observe "running"
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "done thinking"}}},
		})
	}))
	defer gw.Close()

	jobs := newJobStore(time.Hour)
	mux := newMux(testConfig(gw.URL), jobs)

	rec := post(t, mux, `{"action":"ask_async","message":"long job"}`, nil)
	// Deliberately 200: ChatGPT Actions mishandle 202 and report it as an error.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decode(t, rec)
	id, _ := body["jobId"].(string)
	if id == "" {
		t.Fatal("expected a jobId")
	}
	if body["status"] != jobStatusRunning {
		t.Fatalf("status = %v, want running", body["status"])
	}

	// While the gateway is still held, the job must report running.
	rec = post(t, mux, `{"action":"get_result","jobId":"`+id+`"}`, nil)
	if got := decode(t, rec)["status"]; got != jobStatusRunning {
		t.Fatalf("status = %v, want running", got)
	}

	close(release)

	deadline := time.Now().Add(3 * time.Second)
	var final map[string]any
	for time.Now().Before(deadline) {
		final = decode(t, post(t, mux, `{"action":"get_result","jobId":"`+id+`"}`, nil))
		if final["status"] != jobStatusRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if final["status"] != jobStatusDone {
		t.Fatalf("status = %v, want done (%v)", final["status"], final)
	}
	if final["reply"] != "done thinking" {
		t.Fatalf("reply = %v", final["reply"])
	}
}

func TestAsyncRecordsFailure(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer gw.Close()

	mux := newMux(testConfig(gw.URL), newJobStore(time.Hour))
	id, _ := decode(t, post(t, mux, `{"action":"ask_async","message":"hi"}`, nil))["jobId"].(string)

	deadline := time.Now().Add(3 * time.Second)
	var final map[string]any
	for time.Now().Before(deadline) {
		final = decode(t, post(t, mux, `{"action":"get_result","jobId":"`+id+`"}`, nil))
		if final["status"] != jobStatusRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if final["status"] != jobStatusFailed {
		t.Fatalf("status = %v, want failed", final["status"])
	}
	if final["ok"] != false {
		t.Errorf("a failed job should report ok=false")
	}
}

func TestGetResultUnknownJob(t *testing.T) {
	gw := stubGateway(t, "x", nil)
	defer gw.Close()

	mux := newMux(testConfig(gw.URL), newJobStore(time.Hour))
	rec := post(t, mux, `{"action":"get_result","jobId":"does-not-exist"}`, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Job store
// ---------------------------------------------------------------------------

func TestJobStoreSweepKeepsRunningJobs(t *testing.T) {
	s := newJobStore(time.Millisecond)

	running := s.start("agent:main:test")
	finished := s.start("agent:main:test")
	s.finish(finished.ID, "ok")

	time.Sleep(10 * time.Millisecond)
	removed := s.sweep(time.Now())

	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := s.get(running.ID); !ok {
		t.Error("a running job must never be swept, however old")
	}
	if _, ok := s.get(finished.ID); ok {
		t.Error("the finished job should be gone")
	}
}

func TestJobStoreIsConcurrencySafe(t *testing.T) {
	s := newJobStore(time.Hour)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j := s.start("agent:main:test")
			s.finish(j.ID, "reply")
			if got, ok := s.get(j.ID); !ok || got.Reply != "reply" {
				t.Errorf("job %s did not round-trip", j.ID)
			}
			s.sweep(time.Now())
		}()
	}
	wg.Wait()
}

func TestJobIDsAreUnique(t *testing.T) {
	s := newJobStore(time.Hour)
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		id := s.start("agent:main:test").ID
		if seen[id] {
			t.Fatalf("duplicate job id %q", id)
		}
		seen[id] = true
	}
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

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
		{"api_key header", key, map[string]string{"api_key": key}, true},
		{"wrong api_key header", key, map[string]string{"api_key": "nope"}, false},
		{"bearer token", key, map[string]string{"Authorization": "Bearer " + key}, true},
		{"bearer is case-insensitive", key, map[string]string{"Authorization": "bearer " + key}, true},
		{"raw authorization value", key, map[string]string{"Authorization": key}, true},
		{"missing credential", key, nil, false},
		{"wrong key", key, map[string]string{"Authorization": "Bearer nope"}, false},
		{"empty bearer token", key, map[string]string{"Authorization": "Bearer "}, false},
		{"scheme only", key, map[string]string{"Authorization": "Bearer"}, false},
		{"key as prefix is not enough", key, map[string]string{"Authorization": "Bearer " + key + "extra"}, false},
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

func TestEndpointRequiresAPIKey(t *testing.T) {
	var reached int
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	}))
	defer gw.Close()

	mux := newMux(testConfig(gw.URL), newJobStore(time.Hour))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/openclaw", strings.NewReader(`{"action":"ask","message":"hi"}`))
	req.Header.Set("content-type", "application/json")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec.Header().Get("www-authenticate") == "" {
		t.Error("expected a www-authenticate challenge")
	}
	if reached != 0 {
		t.Errorf("unauthenticated request reached the gateway %d times", reached)
	}
}

// A bad key must fail before the bridge reveals configuration problems.
func TestAuthIsCheckedBeforeConfiguration(t *testing.T) {
	cfg := testConfig("")
	cfg.gatewayURL = "" // misconfigured on purpose
	mux := newMux(cfg, newJobStore(time.Hour))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/openclaw", strings.NewReader(`{"action":"ask","message":"hi"}`))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 rather than a 500 leaking config state", rec.Code)
	}
}

func TestHealthEndpointsStayOpen(t *testing.T) {
	mux := newMux(testConfig("http://127.0.0.1:1"), newJobStore(time.Hour))
	for _, path := range []string{"/healthz", "/readyz", "/version"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s = %d, want 200", path, rec.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Request hygiene
// ---------------------------------------------------------------------------

func TestRequestHygiene(t *testing.T) {
	gw := stubGateway(t, "ok", nil)
	defer gw.Close()
	mux := newMux(testConfig(gw.URL), newJobStore(time.Hour))

	t.Run("rejects GET", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/openclaw", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("rejects malformed JSON", func(t *testing.T) {
		if rec := post(t, mux, `not json`, nil); rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("rejects trailing data", func(t *testing.T) {
		rec := post(t, mux, `{"action":"ask","message":"a"}{"action":"ask","message":"b"}`, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("rejects oversized bodies", func(t *testing.T) {
		cfg := testConfig(gw.URL)
		cfg.maxBodyBytes = 32
		small := newMux(cfg, newJobStore(time.Hour))
		rec := post(t, small, `{"action":"ask","message":"`+strings.Repeat("x", 500)+`"}`, nil)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413", rec.Code)
		}
	})
}

func TestMissingGatewayURLIsAServerError(t *testing.T) {
	cfg := testConfig("")
	mux := newMux(cfg, newJobStore(time.Hour))
	rec := post(t, mux, `{"action":"ask","message":"hi"}`, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
