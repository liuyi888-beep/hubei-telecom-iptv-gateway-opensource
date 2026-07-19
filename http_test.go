package main

import (
	"net/http"
	"testing"
	"time"
)

type responseRecorder struct {
	header http.Header
	status int
}

func (r *responseRecorder) Header() http.Header {
	return r.header
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return len(b), nil
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
}

func newResponseRecorder() *responseRecorder {
	return &responseRecorder{header: make(http.Header)}
}

func TestStatusJSONIsTheOnlyAuthenticationStatusEndpoint(t *testing.T) {
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	g, err := newGateway(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer g.close()

	legacy := newResponseRecorder()
	g.handler().ServeHTTP(legacy, mustRequest(t, http.MethodGet, "/api/auth/status"))
	if legacy.status != http.StatusNotFound {
		t.Fatalf("legacy auth endpoint status=%d", legacy.status)
	}

	status := newResponseRecorder()
	g.handler().ServeHTTP(status, mustRequest(t, http.MethodGet, "/status.json"))
	if status.status != http.StatusOK {
		t.Fatalf("status endpoint status=%d", status.status)
	}
}

func mustRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(method, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestStatusPayloadUsesExpiredAuthenticationSnapshot(t *testing.T) {
	cfg := defaultConfig()
	cfg.AuthSessionTTLSeconds = 60
	cfg.DataDir = t.TempDir()
	g, err := newGateway(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer g.close()
	g.authStatus = AuthStatus{OK: true, Message: "login ok"}
	g.lastLogin = nowLocal().Add(-2 * time.Minute)
	payload := g.statusPayload()
	status, ok := payload["auth_status"].(AuthStatus)
	if !ok {
		t.Fatalf("auth status payload=%T", payload["auth_status"])
	}
	if status.OK || status.Message != "session expired" {
		t.Fatalf("auth status=%+v", status)
	}
}
