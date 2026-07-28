package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

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

func TestProtobufJSONInt64Scalars(t *testing.T) {
	t.Parallel()
	var vmm VMM
	fixture := `{"id":"vmm-1","cpu_cores":4,"memory_mb":2048,"disk_mb":10240,` +
		`"desired_revision":"3","observed_revision":"2","resource_version":"7"}`
	if err := json.Unmarshal([]byte(fixture), &vmm); err != nil {
		t.Fatal(err)
	}
	if vmm.ResourceVersion != 7 || vmm.DesiredRevision != 3 || vmm.CPUCores != 4 {
		t.Fatalf("unexpected decoded VMM: %+v", vmm)
	}
	payload, err := json.Marshal(map[string]any{
		"resource_version": vmm.ResourceVersion,
		"cpu_cores":        vmm.CPUCores,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(payload); got != `{"cpu_cores":4,"resource_version":"7"}` {
		t.Fatalf("unexpected protobuf JSON payload: %s", got)
	}
}

func TestStableIdempotencyKey(t *testing.T) {
	t.Parallel()
	payload := map[string]any{"name": "worker", "resource_version": Version(7)}
	first, err := StableIdempotencyKey("vappcloud_vmm.update", "vmm-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	second, err := StableIdempotencyKey("vappcloud_vmm.update", "vmm-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("stable payload produced different keys: %q != %q", first, second)
	}
	changed, err := StableIdempotencyKey("vappcloud_vmm.update", "vmm-1", map[string]any{
		"name": "worker-2", "resource_version": Version(7),
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("changed payload reused its idempotency key")
	}
}

func TestServiceTokenExchangeAndRedaction(t *testing.T) {
	t.Parallel()
	const secret = "vappsvc_fixture_redacted"
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

func TestOnlyExplicitServiceTokensAreExchanged(t *testing.T) {
	t.Parallel()
	var exchanges atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			exchanges.Add(1)
			t.Error("ordinary bearer token was sent to the service-token exchange")
		}
		if got := r.Header.Get("Authorization"); got != "Bearer opaque-token" {
			t.Errorf("unexpected authorization header %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	c, err := New(server.URL, "opaque-token", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Do(context.Background(), http.MethodGet, "/v1/projects", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if exchanges.Load() != 0 {
		t.Fatalf("expected no exchanges, got %d", exchanges.Load())
	}
}

func TestServiceTokenReauthenticatesOnceAfterLateUnauthorized(t *testing.T) {
	t.Parallel()
	var exchanges atomic.Int32
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			n := exchanges.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "jwt-" + strconv.FormatInt(int64(n), 10),
				"expires_in":   300,
			})
			return
		}
		switch requests.Add(1) {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			w.WriteHeader(http.StatusUnauthorized)
		default:
			if got := r.Header.Get("Authorization"); got != "Bearer jwt-2" {
				t.Errorf("request did not use refreshed token: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		}
	}))
	defer server.Close()

	c, err := New(server.URL, "vappsvc_expired", "test")
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(context.Context, time.Duration) error { return nil }
	if err := c.Do(context.Background(), http.MethodGet, "/v1/projects", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if exchanges.Load() != 2 {
		t.Fatalf("expected one initial exchange and one reauthentication, got %d", exchanges.Load())
	}
}

func TestServerCannotOverrideRetryClassification(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(APIError{
			Code: "INVALID_ARGUMENT", Message: "do not retry", Retryable: true,
		})
	}))
	defer server.Close()

	c, _ := New(server.URL, "opaque-token", "test")
	c.sleep = func(context.Context, time.Duration) error { return nil }
	err := c.Do(context.Background(), http.MethodGet, "/v1/projects", nil, nil, "")
	if err == nil {
		t.Fatal("expected API error")
	}
	if attempts.Load() != 1 {
		t.Fatalf("non-retryable status was retried %d times", attempts.Load())
	}
}

func TestConcurrentRetries(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	c, _ := New(server.URL, "opaque-token", "test")
	c.sleep = func(context.Context, time.Duration) error { return nil }
	var group sync.WaitGroup
	for range 32 {
		group.Add(1)
		go func() {
			defer group.Done()
			_ = c.Do(context.Background(), http.MethodGet, "/v1/projects", nil, nil, "")
		}()
	}
	group.Wait()
}

func TestValidateBaseURL(t *testing.T) {
	t.Parallel()
	for _, valid := range []string{
		"https://api.4lock.net",
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"http://[::1]:8080",
	} {
		if _, err := ValidateBaseURL(valid); err != nil {
			t.Errorf("expected %q to be valid: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"http://api.4lock.net",
		"ftp://localhost",
		"https://user:password@api.4lock.net",
		"https://api.4lock.net?token=secret",
		"api.4lock.net",
	} {
		if _, err := ValidateBaseURL(invalid); err == nil {
			t.Errorf("expected %q to be rejected", invalid)
		}
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

func TestResponseLossReplaysSameMutation(t *testing.T) {
	t.Parallel()
	payload := map[string]string{"name": "worker"}
	key, err := StableIdempotencyKey("vappcloud_vmm.create", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	var commits atomic.Int32
	c, _ := New("http://127.0.0.1", "opaque-token", "test")
	c.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		n := attempts.Add(1)
		if req.Header.Get("Idempotency-Key") != key {
			t.Errorf("replay changed idempotency key")
		}
		if n == 1 {
			commits.Add(1)
			return nil, errors.New("response lost after commit")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"id":"vmm-replayed","resource_version":"1"}`)),
		}, nil
	})})
	c.sleep = func(context.Context, time.Duration) error { return nil }
	var out VMM
	if err := c.Do(context.Background(), http.MethodPost, "/v1/vmms", payload, &out, key); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 || commits.Load() != 1 || out.ID != "vmm-replayed" {
		t.Fatalf("unexpected replay result: attempts=%d commits=%d resource=%+v", attempts.Load(), commits.Load(), out)
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

func TestListAllFollowsPageToken(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("page_size") != "200" {
			t.Errorf("missing maximum page_size: %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("page_token") == "" {
			_ = json.NewEncoder(w).Encode(Page[NamedItem]{
				Items: []NamedItem{{ID: "first"}}, NextCursor: "cursor-2",
			})
			return
		}
		if r.URL.Query().Get("page_token") != "cursor-2" {
			t.Errorf("unexpected page token: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(Page[NamedItem]{Items: []NamedItem{{ID: "second"}}})
	}))
	defer server.Close()

	c, _ := New(server.URL, "header.payload.signature", "test")
	items, err := ListAll[NamedItem](context.Background(), c, "/v1/items?project_id=prj-1")
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || len(items) != 2 || items[0].ID != "first" || items[1].ID != "second" {
		t.Fatalf("unexpected paginated result: requests=%d items=%+v", requests.Load(), items)
	}
}
