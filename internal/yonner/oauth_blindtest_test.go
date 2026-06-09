package yonner

import (
	"context"
	"errors"
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// ---------------------------------------------------------------------------
// CONTRACT (issue #30 — yonner OAuth2 integration)
//
// yonner.Options has a field:
//
//	OAuth2Token func(*model.OAuth2Config) (string, error)
//
// In BuildWith(ctx, req, coll, opts), when the request's resolved auth
// (model.ResolveAuth) is model.AuthOAuth2:
//   - if opts.OAuth2Token != nil AND req.Auth.OAuth2 != nil it is called; a
//     returned token tok != "" is applied as "Authorization: Bearer <tok>";
//   - a returned error fails BuildWith (returns a non-nil error);
//   - a nil provider, nil config, or empty token => NO Authorization header;
//   - an explicit user-set Authorization header still wins over OAuth2 (the
//     existing userSetAuth rule), and the provider is not applied.
//
// These are blind contract tests: they assert the documented behaviour only,
// constructing nothing beyond contract symbols. Unique prefix: oaBT.
// ---------------------------------------------------------------------------

// oaBTConfig builds a minimal OAuth2 config carrying a recognisable TokenURL so
// tests can assert the provider received the request's own config.
func oaBTConfig() *model.OAuth2Config {
	return &model.OAuth2Config{TokenURL: "https://oabt.example/token"}
}

// oaBTBuild is a small helper that builds the request and fails the test fatally
// on an unexpected error path, returning the Authorization header value.
func oaBTBuild(t *testing.T, req model.Request, coll model.Collection, opts Options) string {
	t.Helper()
	httpReq, err := BuildWith(context.Background(), req, coll, opts)
	if err != nil {
		t.Fatalf("oaBT: BuildWith returned error: %v", err)
	}
	return httpReq.Header.Get("Authorization")
}

// oaBT1 — Applies Bearer: a GET with AuthOAuth2 + a non-nil config and a
// provider returning ("TOK", nil) must yield Authorization: Bearer TOK.
func TestOaBT_AppliesBearer(t *testing.T) {
	req := model.Request{
		Method: model.MethodGet,
		URL:    "https://oabt.example/resource",
		Auth: model.Auth{
			Kind:   model.AuthOAuth2,
			OAuth2: oaBTConfig(),
		},
	}
	opts := Options{
		OAuth2Token: func(*model.OAuth2Config) (string, error) { return "TOK", nil },
	}

	got := oaBTBuild(t, req, model.NewCollection(""), opts)
	if want := "Bearer TOK"; got != want {
		t.Errorf("oaBT1: Authorization = %q, want %q", got, want)
	}
}

// oaBT2 — Provider error fails the build: when the provider returns an error,
// BuildWith must return a non-nil error.
func TestOaBT_ProviderErrorFailsBuild(t *testing.T) {
	req := model.Request{
		Method: model.MethodGet,
		URL:    "https://oabt.example/resource",
		Auth: model.Auth{
			Kind:   model.AuthOAuth2,
			OAuth2: oaBTConfig(),
		},
	}
	opts := Options{
		OAuth2Token: func(*model.OAuth2Config) (string, error) {
			return "", errors.New("oaBT token fetch failed")
		},
	}

	httpReq, err := BuildWith(context.Background(), req, model.NewCollection(""), opts)
	if err == nil {
		t.Fatalf("oaBT2: BuildWith returned nil error; want non-nil. req=%v", httpReq)
	}
}

// oaBT3 — Nil provider => no auth header: AuthOAuth2 but Options.OAuth2Token is
// nil must not error and must not set Authorization.
func TestOaBT_NilProviderNoHeader(t *testing.T) {
	req := model.Request{
		Method: model.MethodGet,
		URL:    "https://oabt.example/resource",
		Auth: model.Auth{
			Kind:   model.AuthOAuth2,
			OAuth2: oaBTConfig(),
		},
	}
	// OAuth2Token left nil.
	got := oaBTBuild(t, req, model.NewCollection(""), Options{})
	if got != "" {
		t.Errorf("oaBT3: Authorization = %q, want empty (nil provider)", got)
	}
}

// oaBT4 — Empty token => no header: a provider returning ("", nil) must leave
// Authorization unset and must not error.
func TestOaBT_EmptyTokenNoHeader(t *testing.T) {
	req := model.Request{
		Method: model.MethodGet,
		URL:    "https://oabt.example/resource",
		Auth: model.Auth{
			Kind:   model.AuthOAuth2,
			OAuth2: oaBTConfig(),
		},
	}
	opts := Options{
		OAuth2Token: func(*model.OAuth2Config) (string, error) { return "", nil },
	}

	got := oaBTBuild(t, req, model.NewCollection(""), opts)
	if got != "" {
		t.Errorf("oaBT4: Authorization = %q, want empty (empty token)", got)
	}
}

// oaBT5 — Inherited from collection: a request with AuthInherit whose collection
// carries AuthOAuth2 must resolve (via ResolveAuth) to the collection's oauth2,
// so the provider IS used and Bearer is applied.
func TestOaBT_InheritedFromCollection(t *testing.T) {
	req := model.Request{
		Method: model.MethodGet,
		URL:    "https://oabt.example/resource",
		Auth:   model.Auth{Kind: model.AuthInherit},
	}
	coll := model.NewCollection("")
	coll.Auth = model.Auth{
		Kind:   model.AuthOAuth2,
		OAuth2: oaBTConfig(),
	}
	opts := Options{
		OAuth2Token: func(*model.OAuth2Config) (string, error) { return "INHERITED", nil },
	}

	got := oaBTBuild(t, req, coll, opts)
	if want := "Bearer INHERITED"; got != want {
		t.Errorf("oaBT5: Authorization = %q, want %q (inherited oauth2)", got, want)
	}
}

// oaBT6 — Explicit Authorization header wins: when the user has an enabled
// Authorization header AND AuthOAuth2, the explicit header is kept and the
// provider is not applied (existing userSetAuth rule).
func TestOaBT_ExplicitHeaderWins(t *testing.T) {
	const explicit = "Custom xyz"
	consulted := false

	req := model.Request{
		Method: model.MethodGet,
		URL:    "https://oabt.example/resource",
		Headers: []model.Param{
			{Key: "Authorization", Value: explicit, Enabled: true},
		},
		Auth: model.Auth{
			Kind:   model.AuthOAuth2,
			OAuth2: oaBTConfig(),
		},
	}
	opts := Options{
		OAuth2Token: func(*model.OAuth2Config) (string, error) {
			consulted = true
			return "SHOULD-NOT-APPLY", nil
		},
	}

	got := oaBTBuild(t, req, model.NewCollection(""), opts)
	if got != explicit {
		t.Errorf("oaBT6: Authorization = %q, want %q (explicit header must win)", got, explicit)
	}
	// The provider's result must not be applied; ideally it is not even
	// consulted. Either way the header above already proves it was not applied;
	// we additionally assert it was not consulted, the stronger contract.
	if consulted {
		t.Errorf("oaBT6: OAuth2Token was consulted; want it skipped when the user set Authorization")
	}
}

// oaBT7 — Provider receives the request's OAuth2Config: the *model.OAuth2Config
// handed to the provider must be the request's own config (assert TokenURL).
func TestOaBT_ProviderReceivesConfig(t *testing.T) {
	cfg := oaBTConfig()

	var seenURL string
	seen := false
	req := model.Request{
		Method: model.MethodGet,
		URL:    "https://oabt.example/resource",
		Auth: model.Auth{
			Kind:   model.AuthOAuth2,
			OAuth2: cfg,
		},
	}
	opts := Options{
		OAuth2Token: func(c *model.OAuth2Config) (string, error) {
			seen = true
			if c != nil {
				seenURL = c.TokenURL
			}
			return "TOK", nil
		},
	}

	if got := oaBTBuild(t, req, model.NewCollection(""), opts); got != "Bearer TOK" {
		t.Errorf("oaBT7: Authorization = %q, want %q", got, "Bearer TOK")
	}
	if !seen {
		t.Fatalf("oaBT7: provider was never called")
	}
	if want := cfg.TokenURL; seenURL != want {
		t.Errorf("oaBT7: provider saw TokenURL %q, want %q", seenURL, want)
	}
}
