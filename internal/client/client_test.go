package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func testSessionToken(t *testing.T, expiresAt time.Time, sessionID string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"exp": expiresAt.Unix(), "session_id": sessionID, "principal_type": "sts_session",
	})
	if err != nil {
		t.Fatal(err)
	}
	return "eyJhbGciOiJFUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := New(
		baseURL,
		"VAPPASIATEST",
		"fixture-temporary-secret",
		testSessionToken(t, time.Now().Add(time.Hour), "session-test"),
		"test",
	)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func testConfig(t *testing.T, baseURL string) Config {
	t.Helper()
	return Config{
		BaseURL: baseURL, AccessKeyID: "VAPPASIATEST", SecretAccessKey: "fixture-temporary-secret",
		SessionToken: testSessionToken(t, time.Now().Add(time.Hour), "session-test"), ProviderVersion: "test",
	}
}

func TestCredentialProcessHelper(t *testing.T) {
	if os.Getenv("VAPPCLOUD_CREDENTIAL_PROCESS_HELPER") != "1" {
		return
	}
	_, _ = os.Stdout.WriteString(os.Getenv("VAPPCLOUD_CREDENTIAL_PROCESS_RESPONSE"))
	os.Exit(0)
}

func TestCredentialProcessUsesDirectTemporaryCredentials(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	response, err := json.Marshal(map[string]any{
		"Version": 1, "AccessKeyId": "VAPPASIAPROCESS", "SecretAccessKey": "process-secret",
		"SessionToken": testSessionToken(t, expires, "process-session"), "Expiration": expires.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VAPPCLOUD_CREDENTIAL_PROCESS_HELPER", "1")
	t.Setenv("VAPPCLOUD_CREDENTIAL_PROCESS_RESPONSE", string(response))
	provider := credentialProcessProvider{
		command: fmt.Sprintf("%q -test.run=^TestCredentialProcessHelper$", os.Args[0]),
	}
	credentials, err := temporaryCredentialsProvider{provider: provider}.Retrieve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessKeyID != "VAPPASIAPROCESS" || credentials.SecretAccessKey != "process-secret" ||
		credentials.SessionToken == "" || !credentials.CanExpire || !credentials.Expires.Equal(expires) {
		t.Fatalf("unexpected credential_process result: %+v", credentials)
	}
}

func TestCredentialProcessParserDoesNotInvokeShellSyntax(t *testing.T) {
	t.Parallel()
	arguments, err := splitCredentialProcess(`vappctl access credential-process --account-id acc_example --role-arn "arn:vapp:iam::1:role/Project Editor";echo`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"vappctl", "access", "credential-process", "--account-id", "acc_example", "--role-arn", "arn:vapp:iam::1:role/Project Editor;echo"}
	if len(arguments) != len(want) {
		t.Fatalf("unexpected arguments: %#v", arguments)
	}
	for index := range want {
		if arguments[index] != want[index] {
			t.Fatalf("argument %d = %q, want %q", index, arguments[index], want[index])
		}
	}
	if _, err := splitCredentialProcess("vappctl\nmalicious"); err == nil {
		t.Fatal("credential_process accepted a newline")
	}
	windows, err := splitCredentialProcess(`"C:\Program Files\VAppCloud\vappctl.exe" access credential-process --account-id acc_example --role-arn role`)
	if err != nil {
		t.Fatal(err)
	}
	if windows[0] != `C:\Program Files\VAppCloud\vappctl.exe` {
		t.Fatalf("Windows credential_process executable was corrupted: %q", windows[0])
	}
	unc, err := splitCredentialProcess(`"\\server\share\vappctl.exe" access credential-process --account-id acc_example --role-arn role`)
	if err != nil {
		t.Fatal(err)
	}
	if unc[0] != `\\server\share\vappctl.exe` {
		t.Fatalf("Windows UNC credential_process executable was corrupted: %q", unc[0])
	}
	trailing, err := splitCredentialProcess(`"C:\Program Files\VAppCloud\\" access credential-process --account-id acc_example --role-arn role`)
	if err != nil {
		t.Fatal(err)
	}
	if trailing[0] != `C:\Program Files\VAppCloud\` {
		t.Fatalf("Windows trailing backslash was corrupted: %q", trailing[0])
	}
}

func TestMutationRequiresIdempotencyKey(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, "https://example.test")
	err := c.Do(context.Background(), http.MethodPost, "/v1/projects", map[string]string{"name": "x"}, nil, "")
	if err == nil || !strings.Contains(err.Error(), "idempotency") {
		t.Fatalf("expected idempotency error, got %v", err)
	}
}

func TestProtobufJSONInt64Scalars(t *testing.T) {
	t.Parallel()
	var vmm VMM
	fixture := `{"id":"vmm-1","projectId":"prj-1","deviceId":"dev-1","cpuCores":4,"memoryMb":2048,"diskMb":10240,` +
		`"desiredRevision":"3","observedRevision":"2","resourceVersion":"7",` +
		`"instanceProfileArn":"arn:vapp:iam::3:instance-profile/qa","instanceRoleArn":"arn:vapp:iam::3:role/qa"}`
	if err := json.Unmarshal([]byte(fixture), &vmm); err != nil {
		t.Fatal(err)
	}
	if vmm.ResourceVersion != 7 || vmm.DesiredRevision != 3 || vmm.CPUCores != 4 ||
		vmm.ProjectID != "prj-1" || vmm.DeviceID != "dev-1" ||
		vmm.InstanceProfileARN != "arn:vapp:iam::3:instance-profile/qa" ||
		vmm.InstanceRoleARN != "arn:vapp:iam::3:role/qa" {
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

func TestOperationPath(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"op-cmp-123": "/v1/compute-operations/op-cmp-123",
		"op-app-456": "/v1/application-operations/op-app-456",
		"uuid-vmm":   "/v1/operations/uuid-vmm",
	}
	for operationID, want := range cases {
		if got := operationPath(operationID); got != want {
			t.Fatalf("operationPath(%q) = %q, want %q", operationID, got, want)
		}
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

func TestSigV4SignsEveryRequestWithTemporaryCredentials(t *testing.T) {
	t.Parallel()
	var firstAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Errorf("request was not SigV4 signed: %q", r.Header.Get("Authorization"))
		}
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Error("STS session token was sent as a Bearer token")
		}
		if r.Header.Get("X-Amz-Security-Token") == "" || r.Header.Get("X-Amz-Date") == "" ||
			r.Header.Get("X-Amz-Content-Sha256") == "" {
			t.Error("signed request omitted required temporary-credential headers")
		}
		if r.Header.Get("X-Amz-Request-Id") == "" || r.Header.Get("Idempotency-Key") != "stable-key" {
			t.Error("mutation omitted replay-protection headers")
		}
		firstAuthorization = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	if err := c.Do(context.Background(), http.MethodPost, "/v1/projects?b=2&a=1", map[string]string{"name": "signed"}, nil, "stable-key"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(firstAuthorization, "/global/vappcloud/aws4_request") {
		t.Fatalf("unexpected SigV4 credential scope: %q", firstAuthorization)
	}
	for _, signedHeader := range []string{"host", "x-amz-content-sha256", "x-amz-date", "x-amz-request-id", "x-amz-security-token"} {
		if !strings.Contains(firstAuthorization, signedHeader) {
			t.Errorf("authorization omitted signed header %q: %s", signedHeader, firstAuthorization)
		}
	}
}

func TestSigV4PreservesAlreadyEscapedPathOctets(t *testing.T) {
	t.Parallel()
	client := newTestClient(t, "https://api.example.test")
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.EscapedPath() != "/v1/projects/a%2Fb" {
			t.Fatalf("escaped path changed before signing: %q", request.URL.EscapedPath())
		}
		actual := request.Header.Get("Authorization")
		signingTime, err := time.Parse("20060102T150405Z", request.Header.Get("X-Amz-Date"))
		if err != nil {
			t.Fatal(err)
		}
		credentials, err := client.credentials.Retrieve(request.Context())
		if err != nil {
			t.Fatal(err)
		}
		expected := request.Clone(request.Context())
		expected.Header = request.Header.Clone()
		expected.Header.Del("Authorization")
		if err := v4.NewSigner().SignHTTP(
			request.Context(), credentials, expected,
			request.Header.Get("X-Amz-Content-Sha256"), "vappcloud", "global", signingTime,
			func(options *v4.SignerOptions) { options.DisableURIPathEscaping = true },
		); err != nil {
			t.Fatal(err)
		}
		if actual != expected.Header.Get("Authorization") {
			t.Fatal("provider signature did not preserve the canonical escaped path")
		}
		wrong := request.Clone(request.Context())
		wrong.Header = request.Header.Clone()
		wrong.Header.Del("Authorization")
		if err := v4.NewSigner().SignHTTP(
			request.Context(), credentials, wrong,
			request.Header.Get("X-Amz-Content-Sha256"), "vappcloud", "global", signingTime,
		); err != nil {
			t.Fatal(err)
		}
		if actual == wrong.Header.Get("Authorization") {
			t.Fatal("test path did not distinguish preserved from double-escaped signing")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    request,
		}, nil
	})
	var response map[string]bool
	if err := client.Do(context.Background(), http.MethodGet, "/v1/projects/a%2Fb?z=last&a=first", nil, &response, ""); err != nil {
		t.Fatal(err)
	}
}

func TestWebIdentityReReadsTokenAndRefreshesAfterUnauthorized(t *testing.T) {
	t.Parallel()
	tokenFile := t.TempDir() + "/oidc-token"
	if err := os.WriteFile(tokenFile, []byte("oidc-token-one"), 0o600); err != nil {
		t.Fatal(err)
	}
	var exchanges atomic.Int32
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sts/assume-role-with-web-identity":
			n := exchanges.Add(1)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["web_identity_token"] != "oidc-token-one" || body["role_arn"] != "arn:vapp:iam::42:role/deploy" {
				t.Errorf("unexpected web identity request: %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"Credentials": map[string]any{
				"AccessKeyId":     "VAPPASIAWEB" + strconv.FormatInt(int64(n), 10),
				"SecretAccessKey": "temporary-secret", "SessionToken": testSessionToken(t, time.Now().Add(time.Hour), "web-session"),
				"Expiration": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			}})
		case "/v1/projects":
			if requests.Add(1) == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c, err := NewWithConfig(Config{
		BaseURL: server.URL, WebIdentityTokenFile: tokenFile,
		RoleARN: "arn:vapp:iam::42:role/deploy", ProviderVersion: "test", MaxRetries: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Do(context.Background(), http.MethodGet, "/v1/projects", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if exchanges.Load() != 2 || requests.Load() != 2 {
		t.Fatalf("expected refresh after unauthorized, got exchanges=%d requests=%d", exchanges.Load(), requests.Load())
	}
}

func TestTemporaryCredentialsAreNeverForwardedAcrossRedirects(t *testing.T) {
	t.Parallel()
	var redirectedRequests atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectedRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer destination.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", destination.URL+"/capture")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	client := newTestClient(t, origin.URL)
	var response map[string]any
	if err := client.Do(context.Background(), http.MethodGet, "/v1/projects", nil, &response, ""); err == nil {
		t.Fatal("expected redirect response to be rejected")
	}
	if redirectedRequests.Load() != 0 {
		t.Fatal("SigV4 temporary credentials were forwarded to a redirect target")
	}
}

func TestWebIdentityAssertionIsNeverReplayedAcrossRedirects(t *testing.T) {
	t.Parallel()
	tokenFile := t.TempDir() + "/oidc-token"
	if err := os.WriteFile(tokenFile, []byte("sensitive-oidc-assertion"), 0o600); err != nil {
		t.Fatal(err)
	}
	var redirectedRequests atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectedRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer destination.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", destination.URL+"/capture")
		w.WriteHeader(http.StatusPermanentRedirect)
	}))
	defer origin.Close()

	client, err := NewWithConfig(Config{
		BaseURL: origin.URL, WebIdentityTokenFile: tokenFile,
		RoleARN: "arn:vapp:iam::42:role/deploy", ProviderVersion: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Do(context.Background(), http.MethodGet, "/v1/projects", nil, nil, ""); err == nil {
		t.Fatal("expected web identity redirect response to be rejected")
	}
	if redirectedRequests.Load() != 0 {
		t.Fatal("web identity assertion was replayed to a redirect target")
	}
}

func TestCredentialConfigurationRejectsPartialOrAmbiguousValues(t *testing.T) {
	t.Parallel()
	token := testSessionToken(t, time.Now().Add(time.Hour), "session-validation")
	for name, config := range map[string]Config{
		"missing":           {BaseURL: "https://example.test"},
		"partial-static":    {BaseURL: "https://example.test", AccessKeyID: "VAPPASIAFIXTURE"},
		"missing-role":      {BaseURL: "https://example.test", WebIdentityTokenFile: "/tmp/oidc"},
		"ambiguous":         {BaseURL: "https://example.test", AccessKeyID: "id", SecretAccessKey: "secret", SessionToken: token, CredentialProcess: "vappctl credential-process"},
		"non-session-token": {BaseURL: "https://example.test", AccessKeyID: "id", SecretAccessKey: "secret", SessionToken: "opaque-token"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewWithConfig(config); err == nil {
				t.Fatal("expected credential validation error")
			}
		})
	}
}

func TestTranscodedGRPCErrorPreservesStatusAndMessage(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/grpc+proto")
		w.Header().Set("Grpc-Status", "16")
		w.Header().Set("Grpc-Message", "human%20authentication%20required")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	err := c.Do(context.Background(), http.MethodGet, "/v1/vmms/vmm-1/sessions", nil, nil, "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized ||
		apiErr.Code != "UNAUTHENTICATED" ||
		apiErr.Message != "human authentication required" {
		t.Fatalf("unexpected decoded gRPC error: %+v", apiErr)
	}
}

func TestRejectedTemporaryCredentialsAreRedacted(t *testing.T) {
	t.Parallel()
	const reflectedSecret = "fixture-temporary-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(APIError{
			Code: "UNAUTHENTICATED", Message: "rejected " + reflectedSecret,
		})
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	err := c.Do(context.Background(), http.MethodGet, "/v1/projects", nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), "Access Portal or vappctl") {
		t.Fatalf("expected renewal diagnostic, got %v", err)
	}
	if strings.Contains(err.Error(), reflectedSecret) {
		t.Fatalf("temporary secret leaked in diagnostic: %v", err)
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

	c := newTestClient(t, server.URL)
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

	c := newTestClient(t, server.URL)
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

	c := newTestClient(t, server.URL)
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
	key, err := NewIdempotencyKey("vappcloud_vmm.create")
	if err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	var commits atomic.Int32
	c := newTestClient(t, "http://127.0.0.1")
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

func TestCreateIdempotencyKeysAreUniqueForIdenticalPayloads(t *testing.T) {
	t.Parallel()
	first, err := NewIdempotencyKey("vappcloud_vmm.create")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewIdempotencyKey("vappcloud_vmm.create")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("distinct create invocations generated the same key: %s", first)
	}
}

func TestConfiguredUserAgentAndEndpointOverride(t *testing.T) {
	t.Parallel()
	var gotUserAgent string
	override := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer override.Close()
	config := testConfig(t, "http://127.0.0.1")
	config.ProviderVersion = "1.2.3"
	config.TerraformVersion = "1.15.8"
	config.AppendUserAgent = "company-module/4.0"
	config.EndpointOverrides = map[string]string{"vmms": override.URL}
	c, err := NewWithConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	if err := c.Do(context.Background(), http.MethodGet, "/v1/vmms", nil, &out, ""); err != nil {
		t.Fatal(err)
	}
	if gotUserAgent != "terraform-provider-vappcloud/1.2.3 terraform/1.15.8 company-module/4.0" {
		t.Fatalf("unexpected user agent %q", gotUserAgent)
	}
}

func TestConfiguredZeroRetriesMakesOneAttempt(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(APIError{Code: "UNAVAILABLE", Message: "retry"})
	}))
	defer server.Close()
	config := testConfig(t, server.URL)
	config.MaxRetries = 0
	c, err := NewWithConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	err = c.Do(context.Background(), http.MethodPost, "/v1/projects", map[string]string{"name": "one"}, nil, "key")
	if err == nil {
		t.Fatal("expected service unavailable error")
	}
	if attempts.Load() != 1 {
		t.Fatalf("expected one attempt with max_retries=0, got %d", attempts.Load())
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

	c := newTestClient(t, server.URL)
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

	c := newTestClient(t, server.URL)
	items, err := ListAll[NamedItem](context.Background(), c, "/v1/items?project_id=prj-1")
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || len(items) != 2 || items[0].ID != "first" || items[1].ID != "second" {
		t.Fatalf("unexpected paginated result: requests=%d items=%+v", requests.Load(), items)
	}
}
