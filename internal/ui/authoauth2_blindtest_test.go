package ui

// Blind contract tests for the OAuth 2.0 auth-editor (issue #30).
//
// These are written against the documented contract of the reusable authEditor
// (auth.go + authoauth2.go) without assuming any implementation detail beyond
// the public/struct surface the other auth-editor tests rely on:
//
//   - authKindOptions(includeInherit) offers the "OAuth 2.0" label.
//   - newAuthEditor seeds an OAuth2 form from a.OAuth2 and ae.value() round-trips
//     it back into a model.Auth{Kind: AuthOAuth2, OAuth2: ...}.
//   - ae.onGetToken is invoked by the "Get Token" button with the current config.
//   - ae.setOAuthStatus updates a reachable status label.
//
// Unique prefix: oauiBT.

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/ultramcu/yon/internal/model"
)

// oauiBTContains reports whether opts contains want.
func oauiBTContains(opts []string, want string) bool {
	for _, o := range opts {
		if o == want {
			return true
		}
	}
	return false
}

// oauiBTFindButton walks the laid-out tree under root and returns the first
// *widget.Button whose Text equals text, or nil. The caller must have placed
// root inside a window (test.NewWindow) so widget renderers are cached and
// walkObjects can recurse into them.
func oauiBTFindButton(app fyne.App, root fyne.CanvasObject, text string) *widget.Button {
	var found *widget.Button
	walkObjects(app, root, func(o fyne.CanvasObject) {
		if found != nil {
			return
		}
		if b, ok := o.(*widget.Button); ok && b.Text == text {
			found = b
		}
	})
	return found
}

// oauiBTFindLabelWith walks the laid-out tree under root and returns the first
// *widget.Label whose Text equals text, or nil.
func oauiBTFindLabelWith(app fyne.App, root fyne.CanvasObject, text string) *widget.Label {
	var found *widget.Label
	walkObjects(app, root, func(o fyne.CanvasObject) {
		if found != nil {
			return
		}
		if l, ok := o.(*widget.Label); ok && l.Text == text {
			found = l
		}
	})
	return found
}

// TestOAuth2_Offered_oauiBT asserts "OAuth 2.0" is an offered auth kind in both
// the inherit and non-inherit option sets.
func TestOAuth2_Offered_oauiBT(t *testing.T) {
	test.NewApp()

	for _, includeInherit := range []bool{false, true} {
		opts := authKindOptions(includeInherit)
		if !oauiBTContains(opts, "OAuth 2.0") {
			t.Fatalf("authKindOptions(%v)=%v; want it to contain %q",
				includeInherit, opts, "OAuth 2.0")
		}
	}
}

// TestOAuth2_ValueRoundTrip_ClientCredentials_oauiBT seeds a client_credentials
// config and asserts value() returns Kind==AuthOAuth2 with the fields preserved.
func TestOAuth2_ValueRoundTrip_ClientCredentials_oauiBT(t *testing.T) {
	test.NewApp()

	seed := model.Auth{
		Kind: model.AuthOAuth2,
		OAuth2: &model.OAuth2Config{
			Grant:        model.GrantClientCredentials,
			TokenURL:     "https://t/token",
			ClientID:     "cid",
			ClientSecret: "sec",
			Scopes:       "a b",
		},
	}
	ae := newAuthEditor(seed, false, func() {})

	got := ae.value()
	if got.Kind != model.AuthOAuth2 {
		t.Fatalf("value().Kind = %q; want %q", got.Kind, model.AuthOAuth2)
	}
	if got.OAuth2 == nil {
		t.Fatal("value().OAuth2 is nil; want a populated config")
	}
	cfg := *got.OAuth2
	if cfg.Grant != model.GrantClientCredentials {
		t.Errorf("Grant = %q; want %q", cfg.Grant, model.GrantClientCredentials)
	}
	if cfg.TokenURL != "https://t/token" {
		t.Errorf("TokenURL = %q; want %q", cfg.TokenURL, "https://t/token")
	}
	if cfg.ClientID != "cid" {
		t.Errorf("ClientID = %q; want %q", cfg.ClientID, "cid")
	}
	if cfg.ClientSecret != "sec" {
		t.Errorf("ClientSecret = %q; want %q", cfg.ClientSecret, "sec")
	}
	if cfg.Scopes != "a b" {
		t.Errorf("Scopes = %q; want %q", cfg.Scopes, "a b")
	}
}

// TestOAuth2_ValueRoundTrip_AuthorizationCode_oauiBT seeds an authorization_code
// config and asserts AuthURL, UsePKCE and RedirectURI round-trip. A blank
// RedirectURI is also accepted to round-trip as the default loopback redirect.
func TestOAuth2_ValueRoundTrip_AuthorizationCode_oauiBT(t *testing.T) {
	test.NewApp()

	seed := model.Auth{
		Kind: model.AuthOAuth2,
		OAuth2: &model.OAuth2Config{
			Grant:       model.GrantAuthorizationCode,
			TokenURL:    "https://t/token",
			AuthURL:     "https://a/authorize",
			UsePKCE:     true,
			RedirectURI: "http://127.0.0.1:0/callback",
		},
	}
	ae := newAuthEditor(seed, false, func() {})

	got := ae.value()
	if got.Kind != model.AuthOAuth2 || got.OAuth2 == nil {
		t.Fatalf("value() = %+v; want Kind=%q with non-nil OAuth2",
			got, model.AuthOAuth2)
	}
	cfg := *got.OAuth2
	if cfg.Grant != model.GrantAuthorizationCode {
		t.Errorf("Grant = %q; want %q", cfg.Grant, model.GrantAuthorizationCode)
	}
	if cfg.AuthURL != "https://a/authorize" {
		t.Errorf("AuthURL = %q; want %q", cfg.AuthURL, "https://a/authorize")
	}
	if !cfg.UsePKCE {
		t.Errorf("UsePKCE = false; want true")
	}
	if cfg.RedirectURI != "http://127.0.0.1:0/callback" {
		t.Errorf("RedirectURI = %q; want %q",
			cfg.RedirectURI, "http://127.0.0.1:0/callback")
	}

	// A blank RedirectURI for the authorization_code grant should round-trip as
	// a sensible non-empty default loopback redirect rather than "".
	seed.OAuth2.RedirectURI = ""
	ae2 := newAuthEditor(seed, false, func() {})
	cfg2 := ae2.value().OAuth2
	if cfg2 == nil {
		t.Fatal("value().OAuth2 is nil for blank-redirect authorization_code config")
	}
	if cfg2.RedirectURI == "" {
		t.Errorf("RedirectURI = %q; want a non-empty default loopback redirect", cfg2.RedirectURI)
	}
}

// TestOAuth2_GetTokenButton_FiresOnGetToken_oauiBT taps the "Get Token" button
// and asserts ae.onGetToken is invoked with the current config.
func TestOAuth2_GetTokenButton_FiresOnGetToken_oauiBT(t *testing.T) {
	app := test.NewApp()

	seed := model.Auth{
		Kind: model.AuthOAuth2,
		OAuth2: &model.OAuth2Config{
			Grant:    model.GrantClientCredentials,
			TokenURL: "https://t/token",
			ClientID: "cid",
		},
	}
	ae := newAuthEditor(seed, false, func() {})

	var captured model.OAuth2Config
	var called bool
	ae.onGetToken = func(cfg model.OAuth2Config) {
		captured = cfg
		called = true
	}

	// Place the editor in a window so widget renderers are cached and the tree
	// walk can reach the "Get Token" button (a non-field widget inside oauth2Box).
	w := test.NewWindow(ae.container)
	defer w.Close()
	w.Resize(fyne.NewSize(600, 600))

	btn := oauiBTFindButton(app, ae.container, "Get Token")
	if btn == nil {
		t.Fatal(`could not find a *widget.Button with text "Get Token" in the editor`)
	}
	test.Tap(btn)

	if !called {
		t.Fatal("onGetToken was not invoked by the Get Token button")
	}
	if captured.TokenURL != "https://t/token" {
		t.Errorf("onGetToken received TokenURL = %q; want %q",
			captured.TokenURL, "https://t/token")
	}
	if captured.ClientID != "cid" {
		t.Errorf("onGetToken received ClientID = %q; want %q",
			captured.ClientID, "cid")
	}
	if captured.Grant != model.GrantClientCredentials {
		t.Errorf("onGetToken received Grant = %q; want %q",
			captured.Grant, model.GrantClientCredentials)
	}
}

// TestOAuth2_SetOAuthStatus_oauiBT asserts setOAuthStatus is non-panicking and,
// when a status label is reachable in the laid-out tree, reflects the text.
func TestOAuth2_SetOAuthStatus_oauiBT(t *testing.T) {
	app := test.NewApp()

	ae := newAuthEditor(model.Auth{
		Kind:   model.AuthOAuth2,
		OAuth2: &model.OAuth2Config{Grant: model.GrantClientCredentials},
	}, false, func() {})

	w := test.NewWindow(ae.container)
	defer w.Close()
	w.Resize(fyne.NewSize(600, 600))

	const status = "oauiBT token acquired"
	// Must not panic.
	ae.setOAuthStatus(status)

	if got := oauiBTFindLabelWith(app, ae.container, status); got == nil {
		t.Errorf("after setOAuthStatus(%q) no reachable label showed that text", status)
	}
}
