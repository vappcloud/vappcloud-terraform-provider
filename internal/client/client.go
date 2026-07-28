package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	mathrand "math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout = 30 * time.Second
	maxAttempts    = 6
)

type Client struct {
	baseURL      *url.URL
	token        string
	jwt          string
	jwtExpiresAt time.Time
	userAgent    string
	http         *http.Client
	mu           sync.Mutex
	sleep        func(context.Context, time.Duration) error
	random       *mathrand.Rand
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

func New(baseURL, token, version string) (*Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("VAppCloud token is required")
	}
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid VAppCloud API URL %q", baseURL)
	}
	return &Client{
		baseURL:   u,
		token:     token,
		userAgent: "terraform-provider-vappcloud/" + version,
		http:      &http.Client{Timeout: defaultTimeout},
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
		random: mathrand.New(mathrand.NewSource(time.Now().UnixNano())),
	}, nil
}

func (c *Client) SetHTTPClient(h *http.Client) {
	c.http = h
}

func (c *Client) authToken(ctx context.Context) (string, error) {
	if strings.Count(c.token, ".") == 2 {
		return c.token, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.jwt != "" && time.Now().Add(30*time.Second).Before(c.jwtExpiresAt) {
		return c.jwt, nil
	}
	body, _ := json.Marshal(map[string]string{"service_token": c.token})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL.String()+"/token", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	res, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange service token: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return "", decodeAPIError(res)
	}
	var out tokenResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("decode token exchange: %w", err)
	}
	if out.AccessToken == "" {
		return "", errors.New("token exchange returned an empty access token")
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = 300
	}
	c.jwt = out.AccessToken
	c.jwtExpiresAt = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return c.jwt, nil
}

func (c *Client) Do(ctx context.Context, method, path string, body any, out any, idempotencyKey string) error {
	token, err := c.authToken(ctx)
	if err != nil {
		return err
	}
	var encoded []byte
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	mutation := method != http.MethodGet && method != http.MethodHead
	if mutation && idempotencyKey == "" {
		return errors.New("mutation requires an idempotency key")
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, reqErr := http.NewRequestWithContext(ctx, method, c.baseURL.String()+path, bytes.NewReader(encoded))
		if reqErr != nil {
			return reqErr
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", c.userAgent)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		res, doErr := c.http.Do(req)
		if doErr != nil {
			if attempt == maxAttempts-1 {
				return fmt.Errorf("VAppCloud API request failed: %w", doErr)
			}
			if err := c.sleep(ctx, c.backoff(attempt, 0)); err != nil {
				return err
			}
			continue
		}

		if res.StatusCode/100 == 2 {
			defer res.Body.Close()
			if out == nil || res.StatusCode == http.StatusNoContent {
				_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
				return nil
			}
			if err := json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(out); err != nil {
				return fmt.Errorf("decode VAppCloud API response: %w", err)
			}
			return nil
		}

		apiErr := decodeAPIError(res)
		if res.StatusCode == http.StatusUnauthorized && strings.Count(c.token, ".") != 2 && attempt == 0 {
			c.mu.Lock()
			c.jwt = ""
			c.jwtExpiresAt = time.Time{}
			c.mu.Unlock()
			token, err = c.authToken(ctx)
			if err != nil {
				return err
			}
			continue
		}
		if !apiErr.Retryable || attempt == maxAttempts-1 {
			apiErr.Message = redact(apiErr.Message, c.token, token)
			for key, value := range apiErr.Details {
				apiErr.Details[key] = redact(value, c.token, token)
			}
			return apiErr
		}
		if err := c.sleep(ctx, c.backoff(attempt, apiErr.RetryAfter)); err != nil {
			return err
		}
	}
	return errors.New("VAppCloud API retry budget exhausted")
}

func redact(value string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func decodeAPIError(res *http.Response) *APIError {
	defer res.Body.Close()
	e := &APIError{
		StatusCode: res.StatusCode,
		Code:       http.StatusText(res.StatusCode),
		Message:    http.StatusText(res.StatusCode),
		RequestID:  res.Header.Get("X-Request-ID"),
		Retryable:  res.StatusCode == 429 || res.StatusCode == 502 || res.StatusCode == 503 || res.StatusCode == 504,
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(e)
	if e.Code == "" {
		e.Code = http.StatusText(res.StatusCode)
	}
	if e.Message == "" {
		e.Message = e.Code
	}
	if e.RequestID == "" {
		e.RequestID = res.Header.Get("X-Correlation-ID")
	}
	if raw := res.Header.Get("Retry-After"); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil {
			e.RetryAfter = time.Duration(seconds) * time.Second
		} else if when, err := http.ParseTime(raw); err == nil {
			e.RetryAfter = time.Until(when)
		}
	}
	if e.Code == "DEPENDENCY_PENDING" || e.Code == "OPERATION_IN_PROGRESS" {
		e.Retryable = true
	}
	return e
}

func (c *Client) backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > 30*time.Second {
			return 30 * time.Second
		}
		return retryAfter
	}
	base := math.Min(30, math.Pow(2, float64(attempt)))
	jitter := 0.75 + c.random.Float64()*0.5
	return time.Duration(base * jitter * float64(time.Second))
}

func IdempotencyKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("tf-%d", time.Now().UnixNano())
	}
	return "tf-" + hex.EncodeToString(b)
}

func Escape(id string) string {
	return url.PathEscape(id)
}

func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func IsFailedPrecondition(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusPreconditionFailed || apiErr.Code == "FAILED_PRECONDITION")
}
