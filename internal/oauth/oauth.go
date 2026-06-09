// Package oauth is Yon's UI-free OAuth 2.0 engine (issue #30): it obtains and
// refreshes access tokens for the client_credentials and authorization_code
// (PKCE) grants. It imports neither Fyne nor the ui package — the browser-open
// step is injected as a BrowserOpener so the engine stays testable.
package oauth

import (
	"net/http"
	"time"
)

// expirySkew is subtracted from a token's lifetime so a token is treated as
// expired slightly early, avoiding sending a token that lapses in flight.
const expirySkew = 30 * time.Second

// TokenSet is a token obtained from an OAuth 2.0 token endpoint.
type TokenSet struct {
	AccessToken  string
	RefreshToken string
	TokenType    string    // usually "Bearer"
	Expiry       time.Time // zero = no known expiry (treated as non-expiring)
}

// Expired reports whether the token is at/after its expiry (minus a small skew).
// A zero Expiry means "no known expiry" and never reports expired.
func (t TokenSet) Expired(now time.Time) bool {
	if t.Expiry.IsZero() {
		return false
	}
	return !now.Before(t.Expiry.Add(-expirySkew))
}

// Valid reports whether the token has an access token and is not expired.
func (t TokenSet) Valid(now time.Time) bool {
	return t.AccessToken != "" && !t.Expired(now)
}

// BrowserOpener opens a URL in the user's web browser. The UI injects a real
// opener (e.g. updater.OpenFile); tests inject a fake. Returning an error aborts
// the authorization_code flow.
type BrowserOpener func(url string) error

// httpClient is the client the engine uses for token-endpoint calls; overridable
// in tests. Defaults to a sane-timeout client.
var httpClient = &http.Client{Timeout: 30 * time.Second}
