package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

const credentialRefreshWindow = 30 * time.Second

type temporaryCredentialsProvider struct {
	provider aws.CredentialsProvider
}

type credentialProcessProvider struct {
	command string
}

type credentialProcessResponse struct {
	Version         int        `json:"Version"`
	AccessKeyID     string     `json:"AccessKeyId"`
	SecretAccessKey string     `json:"SecretAccessKey"`
	SessionToken    string     `json:"SessionToken"`
	Expiration      *time.Time `json:"Expiration"`
	AccountID       string     `json:"AccountId"`
}

func (p credentialProcessProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	arguments, err := splitCredentialProcess(p.command)
	if err != nil {
		return aws.Credentials{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, arguments[0], arguments[1:]...) // #nosec G204 -- explicitly configured credential helper, executed without a shell.
	command.Env = os.Environ()
	var stdout boundedBuffer
	stdout.limit = 1 << 20
	command.Stdout = &stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return aws.Credentials{}, errors.New("VAppCloud credential_process timed out")
		}
		return aws.Credentials{}, errors.New("VAppCloud credential_process failed")
	}
	var response credentialProcessResponse
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return aws.Credentials{}, errors.New("VAppCloud credential_process returned invalid JSON")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return aws.Credentials{}, errors.New("VAppCloud credential_process returned more than one JSON value")
	}
	if response.Version != 1 {
		return aws.Credentials{}, errors.New("VAppCloud credential_process response Version must be 1")
	}
	credentials := aws.Credentials{
		AccessKeyID: response.AccessKeyID, SecretAccessKey: response.SecretAccessKey,
		SessionToken: response.SessionToken, AccountID: response.AccountID, Source: "VAppCloudCredentialProcess",
	}
	if response.Expiration != nil {
		credentials.CanExpire = true
		credentials.Expires = *response.Expiration
	}
	return credentials, nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		return 0, errors.New("credential_process output exceeds 1 MiB")
	}
	if len(value) > remaining {
		_, _ = b.Buffer.Write(value[:remaining])
		return remaining, errors.New("credential_process output exceeds 1 MiB")
	}
	return b.Buffer.Write(value)
}

func splitCredentialProcess(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("credential_process cannot be empty")
	}
	var arguments []string
	var current strings.Builder
	var quote rune
	escaped := false
	hadContent := false
	flush := func() {
		if hadContent {
			arguments = append(arguments, current.String())
			current.Reset()
			hadContent = false
		}
	}
	for _, character := range value {
		if character == '\n' || character == '\r' || character == 0 {
			return nil, errors.New("credential_process contains a forbidden control character")
		}
		if escaped {
			if character == '\\' {
				// Preserve the leading pair of a Windows UNC path. Elsewhere a
				// doubled backslash is the portable way to encode one literal
				// trailing or embedded backslash.
				if current.Len() == 0 {
					current.WriteString(`\\`)
				} else {
					current.WriteRune('\\')
				}
				hadContent = true
				escaped = false
				continue
			}
			if character != '\\' && character != '"' && character != '\'' && character != ' ' && character != '\t' {
				current.WriteRune('\\')
			}
			current.WriteRune(character)
			hadContent = true
			escaped = false
			continue
		}
		if character == '\\' && quote != '\'' {
			escaped = true
			hadContent = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			} else {
				current.WriteRune(character)
				hadContent = true
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			hadContent = true
			continue
		}
		if character == ' ' || character == '\t' {
			flush()
			continue
		}
		current.WriteRune(character)
		hadContent = true
	}
	if escaped || quote != 0 {
		return nil, errors.New("credential_process contains an incomplete escape or quote")
	}
	flush()
	if len(arguments) == 0 || strings.TrimSpace(arguments[0]) == "" {
		return nil, errors.New("credential_process executable is required")
	}
	return arguments, nil
}

func (p temporaryCredentialsProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	credentials, err := p.provider.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{}, err
	}
	if err := validateTemporaryCredentials(credentials); err != nil {
		return aws.Credentials{}, err
	}
	return credentials, nil
}

type webIdentityProvider struct {
	client      *Client
	tokenFile   string
	roleARN     string
	sessionName string
}

type stsCredentialResponse struct {
	Credentials struct {
		AccessKeyID     string    `json:"AccessKeyId"`
		SecretAccessKey string    `json:"SecretAccessKey"`
		SessionToken    string    `json:"SessionToken"`
		Expiration      time.Time `json:"Expiration"`
	} `json:"Credentials"`
}

func (p *webIdentityProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	info, err := os.Stat(p.tokenFile)
	if err != nil {
		return aws.Credentials{}, fmt.Errorf("stat VAppCloud web identity token file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return aws.Credentials{}, errors.New("VAppCloud web identity token file must be a regular file")
	}
	if info.Size() <= 0 || info.Size() > 1<<20 {
		return aws.Credentials{}, errors.New("VAppCloud web identity token file must contain at most 1 MiB")
	}
	rawToken, err := os.ReadFile(p.tokenFile)
	if err != nil {
		return aws.Credentials{}, fmt.Errorf("read VAppCloud web identity token file: %w", err)
	}
	token := strings.TrimSpace(string(rawToken))
	if token == "" {
		return aws.Credentials{}, errors.New("VAppCloud web identity token file is empty")
	}
	sessionName := p.sessionName
	if sessionName == "" {
		sessionName = "terraform-provider"
	}
	body, err := json.Marshal(map[string]any{
		"web_identity_token": token,
		"role_arn":           p.roleARN,
		"duration_seconds":   3600,
		"session_name":       sessionName,
	})
	if err != nil {
		return aws.Credentials{}, fmt.Errorf("encode VAppCloud web identity request: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.client.requestURL("/v1/sts/assume-role-with-web-identity"),
		bytes.NewReader(body),
	)
	if err != nil {
		return aws.Credentials{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", p.client.userAgent)
	res, err := p.client.http.Do(req)
	if err != nil {
		return aws.Credentials{}, fmt.Errorf("exchange VAppCloud web identity token: %w", err)
	}
	if res.StatusCode/100 != 2 {
		apiErr := decodeAPIError(res)
		apiErr.Message = redact(apiErr.Message, token)
		for key, value := range apiErr.Details {
			apiErr.Details[key] = redact(value, token)
		}
		return aws.Credentials{}, apiErr
	}
	defer func() { _ = res.Body.Close() }()
	var out stsCredentialResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil {
		return aws.Credentials{}, fmt.Errorf("decode VAppCloud web identity credentials: %w", err)
	}
	credentials := aws.Credentials{
		AccessKeyID:     out.Credentials.AccessKeyID,
		SecretAccessKey: out.Credentials.SecretAccessKey,
		SessionToken:    out.Credentials.SessionToken,
		CanExpire:       true,
		Expires:         out.Credentials.Expiration,
		Source:          "VAppCloudWebIdentity",
	}
	if err := validateTemporaryCredentials(credentials); err != nil {
		return aws.Credentials{}, fmt.Errorf("invalid VAppCloud web identity response: %w", err)
	}
	return credentials, nil
}

func newCredentialProvider(config Config, client *Client) (aws.CredentialsProvider, bool, error) {
	staticConfigured := config.AccessKeyID != "" || config.SecretAccessKey != "" || config.SessionToken != ""
	processConfigured := config.CredentialProcess != ""
	webIdentityConfigured := config.WebIdentityTokenFile != "" || config.RoleARN != ""
	configured := 0
	for _, present := range []bool{staticConfigured, processConfigured, webIdentityConfigured} {
		if present {
			configured++
		}
	}
	if configured == 0 {
		return nil, false, errors.New("VAppCloud temporary credentials, credential_process, or web identity credentials are required")
	}
	if configured > 1 {
		return nil, false, errors.New("configure exactly one VAppCloud credential source")
	}
	if staticConfigured {
		if config.AccessKeyID == "" || config.SecretAccessKey == "" || config.SessionToken == "" {
			return nil, false, errors.New("access_key_id, secret_access_key, and session_token must be configured together")
		}
		expires, err := sessionTokenExpiration(config.SessionToken)
		if err != nil {
			return nil, false, err
		}
		credentials := aws.Credentials{
			AccessKeyID:     config.AccessKeyID,
			SecretAccessKey: config.SecretAccessKey,
			SessionToken:    config.SessionToken,
			CanExpire:       true,
			Expires:         expires,
			Source:          "VAppCloudAccessPortal",
		}
		if err := validateTemporaryCredentials(credentials); err != nil {
			return nil, false, err
		}
		return aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			if credentials.Expires.Before(time.Now().Add(credentialRefreshWindow)) {
				return aws.Credentials{}, errors.New("VAppCloud temporary credentials expired; renew them through the Access Portal or vappctl")
			}
			return credentials, nil
		}), false, nil
	}
	if processConfigured {
		provider := credentialProcessProvider{command: config.CredentialProcess}
		return temporaryCredentialsProvider{provider: provider}, true, nil
	}
	if config.WebIdentityTokenFile == "" || config.RoleARN == "" {
		return nil, false, errors.New("web_identity_token_file and role_arn must be configured together")
	}
	return &webIdentityProvider{
		client: client, tokenFile: config.WebIdentityTokenFile,
		roleARN: config.RoleARN, sessionName: config.SessionName,
	}, true, nil
}

func validateTemporaryCredentials(credentials aws.Credentials) error {
	if strings.TrimSpace(credentials.AccessKeyID) == "" || strings.TrimSpace(credentials.SecretAccessKey) == "" ||
		strings.TrimSpace(credentials.SessionToken) == "" {
		return errors.New("temporary credentials require AccessKeyId, SecretAccessKey, and SessionToken")
	}
	if !credentials.CanExpire || credentials.Expires.IsZero() {
		return errors.New("VAppCloud credentials must be temporary and include Expiration")
	}
	if !credentials.Expires.After(time.Now().Add(credentialRefreshWindow)) {
		return errors.New("VAppCloud temporary credentials are expired or too close to expiration")
	}
	tokenExpiration, err := sessionTokenExpiration(credentials.SessionToken)
	if err != nil {
		return err
	}
	delta := tokenExpiration.Sub(credentials.Expires)
	// aws.CredentialsCache deliberately shortens Expires by its refresh window
	// (plus bounded jitter). The token itself remains the upper bound.
	if delta < -5*time.Second || delta > 2*credentialRefreshWindow {
		return errors.New("VAppCloud credential Expiration does not match the bound STS session")
	}
	return nil
}

func sessionTokenExpiration(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("VAppCloud session_token must be a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, errors.New("VAppCloud session_token has an invalid JWT payload")
	}
	var claims struct {
		Expiration    int64  `json:"exp"`
		SessionID     string `json:"session_id"`
		PrincipalType string `json:"principal_type"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Expiration <= 0 || claims.SessionID == "" || claims.PrincipalType != "sts_session" {
		return time.Time{}, errors.New("VAppCloud session_token must contain exp, session_id, and principal_type=sts_session claims")
	}
	return time.Unix(claims.Expiration, 0), nil
}
