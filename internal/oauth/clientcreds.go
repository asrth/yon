package oauth

import (
	"context"

	"github.com/ultramcu/yon/internal/model"
)

// ClientCredentialsToken performs the client_credentials grant against
// cfg.TokenURL and returns the resulting token.
//
// LANE: client-credentials engine (Dev A overwrites this file).
func ClientCredentialsToken(ctx context.Context, cfg model.OAuth2Config) (TokenSet, error) {
	panic("oauth.ClientCredentialsToken: not implemented")
}

// Refresh exchanges a refresh_token for a fresh TokenSet at cfg.TokenURL.
func Refresh(ctx context.Context, cfg model.OAuth2Config, refreshToken string) (TokenSet, error) {
	panic("oauth.Refresh: not implemented")
}

// Manager caches obtained tokens per OAuth2 config for the session and serves a
// valid access token, refreshing or re-fetching as needed. Safe for concurrent
// use.
type Manager struct {
	// implemented by the client-credentials lane
}

// NewManager returns an empty token Manager.
func NewManager() *Manager { return &Manager{} }

// Token returns a valid access token for cfg: the cached token when still valid,
// a refresh via a stored refresh token when possible, else the configured grant
// (client_credentials directly, or authorization_code via open). open may be nil
// for grants that don't need a browser.
func (m *Manager) Token(ctx context.Context, cfg model.OAuth2Config, open BrowserOpener) (string, error) {
	panic("oauth.Manager.Token: not implemented")
}

// Set stores a token set for cfg (e.g. the result of an explicit "Get Token").
func (m *Manager) Set(cfg model.OAuth2Config, ts TokenSet) {
	panic("oauth.Manager.Set: not implemented")
}

// Status returns the cached token's state for cfg for display; ok is false when
// nothing is cached.
func (m *Manager) Status(cfg model.OAuth2Config) (ts TokenSet, ok bool) {
	panic("oauth.Manager.Status: not implemented")
}
