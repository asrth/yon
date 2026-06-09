package ui

// Blind contract tests for the OAuth 2.0 send/status glue in oauth.go (issue
// #30). These cover the two highest-risk behaviours that the form-level tests
// (authoauth2_blindtest_test.go) do not touch:
//
//   - resolveOAuth2Config expands {{variable}} templates in every text field,
//     against the window's active scope, without mutating the original config.
//   - oauthStatusText looks up the token cache under the SAME (resolved) key the
//     token was fetched under, so a token acquired for the resolved values is
//     reported as present when the status is requested with the still-templated
//     config (cache-key agreement — otherwise a fetched token is silently never
//     reflected/reused).
//
// Unique prefix: oaflowBT.

import (
	"testing"

	"github.com/ultramcu/yon/internal/model"
	"github.com/ultramcu/yon/internal/oauth"
)

// oaflowBTWindow builds a Window whose collection defines one variable per
// OAuth2Config text field, so a fully templated config resolves to concrete
// values.
func oaflowBTWindow(t *testing.T) *Window {
	t.Helper()
	coll := model.NewCollection("T")
	coll.Variables = []model.Variable{
		{Key: "turl", Value: "https://t.example/token", Enabled: true},
		{Key: "aurl", Value: "https://a.example/authorize", Enabled: true},
		{Key: "cid", Value: "the-client", Enabled: true},
		{Key: "csec", Value: "the-secret", Enabled: true},
		{Key: "redir", Value: "http://127.0.0.1:0/cb", Enabled: true},
		{Key: "scope", Value: "read write", Enabled: true},
		{Key: "aud", Value: "https://api.example", Enabled: true},
	}
	return newScopeWindow(t, coll)
}

// TestOAuth2_ResolveExpandsAllFields_oaflowBT pins that resolveOAuth2Config
// expands {{variables}} in every text field and leaves the original untouched.
func TestOAuth2_ResolveExpandsAllFields_oaflowBT(t *testing.T) {
	w := oaflowBTWindow(t)
	cfg := model.OAuth2Config{
		Grant:        model.GrantAuthorizationCode,
		TokenURL:     "{{turl}}",
		AuthURL:      "{{aurl}}",
		ClientID:     "{{cid}}",
		ClientSecret: "{{csec}}",
		RedirectURI:  "{{redir}}",
		Scopes:       "{{scope}}",
		Audience:     "{{aud}}",
	}

	got := w.resolveOAuth2Config(cfg)
	for _, c := range []struct{ field, have, want string }{
		{"TokenURL", got.TokenURL, "https://t.example/token"},
		{"AuthURL", got.AuthURL, "https://a.example/authorize"},
		{"ClientID", got.ClientID, "the-client"},
		{"ClientSecret", got.ClientSecret, "the-secret"},
		{"RedirectURI", got.RedirectURI, "http://127.0.0.1:0/cb"},
		{"Scopes", got.Scopes, "read write"},
		{"Audience", got.Audience, "https://api.example"},
	} {
		if c.have != c.want {
			t.Errorf("resolved %s = %q; want %q", c.field, c.have, c.want)
		}
	}

	// The original (templated) config must not be mutated by resolution.
	if cfg.TokenURL != "{{turl}}" || cfg.ClientSecret != "{{csec}}" {
		t.Errorf("original cfg was mutated: TokenURL=%q ClientSecret=%q", cfg.TokenURL, cfg.ClientSecret)
	}
}

// TestOAuth2_StatusCacheKeyAgreement_oaflowBT pins that oauthStatusText resolves
// {{variables}} before reading the cache, so it shares the manager's cache slot
// with a fetched token. A token cached under the RESOLVED config must be visible
// when the status is requested with the still-templated config. A version that
// read the cache with the unresolved config would report "No token yet".
func TestOAuth2_StatusCacheKeyAgreement_oaflowBT(t *testing.T) {
	w := oaflowBTWindow(t)
	cfg := model.OAuth2Config{
		Grant:    model.GrantClientCredentials,
		TokenURL: "{{turl}}",
		ClientID: "{{cid}}",
		Scopes:   "{{scope}}",
	}

	// Cache a token under the resolved key, exactly as a "Get Token" would.
	w.app.oauth.Set(w.resolveOAuth2Config(cfg), oauth.TokenSet{AccessToken: "AT"})

	if got := w.oauthStatusText(cfg); got == "No token yet" {
		t.Fatalf("oauthStatusText(templated cfg) = %q; want it to find the token cached under the resolved key (cache-key disagreement)", got)
	}
}
