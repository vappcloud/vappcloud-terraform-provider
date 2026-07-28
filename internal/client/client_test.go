package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMutationRequiresIdempotencyKey(t *testing.T) {
	t.Parallel()
	c, err := New("https://example.test", "header.payload.signature", "test")
	if err != nil {
		t.Fatal(err)
	}
	err = c.Do(context.Background(), http.MethodPost, "/v1/projects", map[string]string{"name": "x"}, nil, "")
	if err == nil || !strings.Contains(err.Error(), "idempotency") {
		t.Fatalf("expected idempotency error, got %v", err)
	}
}

func TestServiceTokenExchangeAndRedaction(t *testing.T) {
	t.Parallel()
	const secret = "vappsvc_super_secret_value"
	var exchanges atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			exchanges.Add(1)
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["service_token"] != secret {
				t.Errorf("token exchange did not receive service token")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "header.payload.signature", "expires_in": 300})
		case "/v1/projects":
			if got := r.Header.Get("Authorization"); got != "Bearer header.payload.signature" {
				t.Errorf("unexpected authorization header %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(APIError{Code: "INVALID_ARGUMENT", Message: "reflected " + secret})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c, err := New(server.URL, secret, "test")
	if err != nil {
		t.Fatal(err)
	}
	err = c.Do(context.Background(), http.MethodGet, "/v1/projects", nil, nil, "")
	if err == nil {
		t.Fatal("expected API error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("service token leaked in diagnostic: %s", err)
	}
	if exchanges.Load() != 1 {
		t.Fatalf("expected one token exchange, got %d", exchanges.Load())
	}
}

func TestBoundedRetryUsesIdempotencyKey(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if r.Header.Get("Idempotency-Key") != "stable-key" {
			t.Errorf("idempotency key changed or missing")
		}
		if n < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(APIError{Code: "UNAVAILABLE", Message: "retry", Retryable: true})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	c, _ := New(server.URL, "header.payload.signature", "test")
	c.sleep = func(context.Context, time.Duration) error { return nil }
	var out map[string]string
	if err := c.Do(context.Background(), http.MethodPost, "/v1/vmms", map[string]string{"name": "worker"}, &out, "stable-key"); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected three attempts, got %d", attempts.Load())
	}
}

func TestWaitOperationRecovery(t *testing.T) {
	t.Parallel()
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		state := "pending"
		if polls.Add(1) > 1 {
			state = "succeeded"
		}
		_ = json.NewEncoder(w).Encode(Operation{ID: "op-1", State: state})
	}))
	defer server.Close()

	c, _ := New(server.URL, "header.payload.signature", "test")
	c.sleep = func(context.Context, time.Duration) error { return nil }
	op, err := c.WaitOperation(context.Background(), "op-1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if op.State != "succeeded" || polls.Load() != 2 {
		t.Fatalf("unexpected operation result: %+v polls=%d", op, polls.Load())
	}
}
