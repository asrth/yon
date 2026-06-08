package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ultramcu/yon/internal/model"
)

// tokenResponse is a standard OAuth 2.0 token endpoint success response
// (RFC 6749 §5.1). Unknown fields are ignored.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"` // seconds; 0 = unspecified
	RefreshToken string `json:"refresh_token"`
}

// errorResponse is an OAuth 2.0 token endpoint error response (RFC 6749 §5.2).
type errorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// ClientCredentialsToken performs the client_credentials grant against
// cfg.TokenURL and returns the resulting token.
//
// LANE: client-credentials engine (Dev A overwrites this file).
func ClientCredentialsToken(ctx context.Context, cfg model.OAuth2Config) (TokenSet, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	if cfg.Scopes != "" {
		form.Set("scope", cfg.Scopes)
	}
	if cfg.Audience != "" {
		form.Set("audience", cfg.Audience)
	}
	return tokenRequest(ctx, cfg, form, "")
}

// Refresh exchanges a refresh_token for a fresh TokenSet at cfg.TokenURL. When
// the response omits a new refresh_token, the supplied refreshToken is retained
// in the returned TokenSet (a common server behaviour, RFC 6749 §6).
func Refresh(ctx context.Context, cfg model.OAuth2Config, refreshToken string) (TokenSet, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	if cfg.Scopes != "" {
		form.Set("scope", cfg.Scopes)
	}
	return tokenRequest(ctx, cfg, form, refreshToken)
}

// tokenRequest posts form to cfg.TokenURL with client authentication applied per
// cfg.ClientAuth, parses the token response, and returns the TokenSet.
// prevRefresh is the refresh token to retain when the response omits one (empty
// for grants where retention does not apply).
func tokenRequest(ctx context.Context, cfg model.OAuth2Config, form url.Values, prevRefresh string) (TokenSet, error) {
	// Apply client authentication.
	basicAuth := false
	switch cfg.ClientAuth {
	case model.OAuth2ClientAuthBody:
		if cfg.ClientID != "" {
			form.Set("client_id", cfg.ClientID)
		}
		if cfg.ClientSecret != "" {
			form.Set("client_secret", cfg.ClientSecret)
		}
	default: // OAuth2ClientAuthBasic or "" → HTTP Basic header.
		basicAuth = true
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenSet{}, fmt.Errorf("oauth: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if basicAuth {
		req.Header.Set("Authorization", "Basic "+basicCredentials(cfg.ClientID, cfg.ClientSecret))
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return TokenSet{}, fmt.Errorf("oauth: token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return TokenSet{}, fmt.Errorf("oauth: read token response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TokenSet{}, tokenError(resp.StatusCode, body)
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return TokenSet{}, fmt.Errorf("oauth: parse token response: %w", err)
	}
	if tr.AccessToken == "" {
		return TokenSet{}, fmt.Errorf("oauth: token response missing access_token")
	}

	ts := TokenSet{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
	}
	if tr.ExpiresIn > 0 {
		ts.Expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	// Retain the previous refresh token when the server didn't return a new one.
	if ts.RefreshToken == "" && prevRefresh != "" {
		ts.RefreshToken = prevRefresh
	}
	return ts, nil
}

// basicCredentials returns the base64-encoded "clientID:clientSecret" used in
// the HTTP Basic Authorization header.
func basicCredentials(clientID, clientSecret string) string {
	return base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
}

// tokenError builds an error from a non-2xx token-endpoint response, including
// the RFC 6749 §5.2 error/error_description fields when the body is JSON.
func tokenError(status int, body []byte) error {
	var er errorResponse
	if json.Unmarshal(body, &er) == nil && er.Error != "" {
		if er.ErrorDescription != "" {
			return fmt.Errorf("oauth: token endpoint %d: %s: %s", status, er.Error, er.ErrorDescription)
		}
		return fmt.Errorf("oauth: token endpoint %d: %s", status, er.Error)
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return fmt.Errorf("oauth: token endpoint returned status %d", status)
	}
	return fmt.Errorf("oauth: token endpoint %d: %s", status, msg)
}

// Manager caches obtained tokens per OAuth2 config for the session and serves a
// valid access token, refreshing or re-fetching as needed. Safe for concurrent
// use.
type Manager struct {
	mu     sync.Mutex
	tokens map[string]TokenSet
}

// NewManager returns an empty token Manager.
func NewManager() *Manager { return &Manager{tokens: make(map[string]TokenSet)} }

// cacheKey derives a stable cache key from the fields of cfg that determine the
// token's identity, so distinct configurations never share a cached token.
func cacheKey(cfg model.OAuth2Config) string {
	// Newlines separate fields; the field values cannot contain a raw newline.
	return strings.Join([]string{
		string(cfg.Grant),
		cfg.TokenURL,
		cfg.AuthURL,
		cfg.ClientID,
		cfg.Scopes,
		cfg.Audience,
		string(cfg.ClientAuth),
	}, "\n")
}

// Token returns a valid access token for cfg: the cached token when still valid,
// a refresh via a stored refresh token when possible, else the configured grant
// (client_credentials directly, or authorization_code via open). open may be nil
// for grants that don't need a browser.
func (m *Manager) Token(ctx context.Context, cfg model.OAuth2Config, open BrowserOpener) (string, error) {
	key := cacheKey(cfg)
	now := time.Now()

	m.mu.Lock()
	cached, ok := m.tokens[key]
	m.mu.Unlock()

	// 1. A cached, still-valid token serves directly.
	if ok && cached.Valid(now) {
		return cached.AccessToken, nil
	}

	// 2. A cached token with a refresh token: try to refresh.
	if ok && cached.RefreshToken != "" {
		if ts, err := Refresh(ctx, cfg, cached.RefreshToken); err == nil && ts.AccessToken != "" {
			m.store(key, ts)
			return ts.AccessToken, nil
		}
		// Refresh failed (revoked/expired); fall through to a fresh grant.
	}

	// 3. Run the configured grant.
	var (
		ts  TokenSet
		err error
	)
	switch cfg.Grant {
	case model.GrantClientCredentials:
		ts, err = ClientCredentialsToken(ctx, cfg)
	case model.GrantAuthorizationCode:
		ts, err = AuthorizationCodeToken(ctx, cfg, open)
	default:
		return "", fmt.Errorf("oauth: unsupported grant %q", cfg.Grant)
	}
	if err != nil {
		return "", err
	}

	m.store(key, ts)
	return ts.AccessToken, nil
}

// store caches ts under key.
func (m *Manager) store(key string, ts TokenSet) {
	m.mu.Lock()
	if m.tokens == nil {
		m.tokens = make(map[string]TokenSet)
	}
	m.tokens[key] = ts
	m.mu.Unlock()
}

// Set stores a token set for cfg (e.g. the result of an explicit "Get Token").
func (m *Manager) Set(cfg model.OAuth2Config, ts TokenSet) {
	m.store(cacheKey(cfg), ts)
}

// Status returns the cached token's state for cfg for display; ok is false when
// nothing is cached.
func (m *Manager) Status(cfg model.OAuth2Config) (ts TokenSet, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ts, ok = m.tokens[cacheKey(cfg)]
	return ts, ok
}
