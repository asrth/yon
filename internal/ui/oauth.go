package ui

import (
	"context"
	"time"

	"fyne.io/fyne/v2"

	"github.com/ultramcu/yon/internal/model"
	"github.com/ultramcu/yon/internal/oauth"
	"github.com/ultramcu/yon/internal/updater"
)

// oauthFlowTimeout bounds a single "Get Token" / on-send token acquisition.
// It must comfortably outlast an interactive authorization_code browser flow
// (the user has to switch to the browser, sign in, and approve) while still
// guaranteeing the goroutine — and any blocked send — eventually unwinds rather
// than hanging forever on a never-returned callback.
const oauthFlowTimeout = 3 * time.Minute

// resolveOAuth2Config expands {{variable}} templates in every text field of an
// OAuth2Config using the window's active environment + collection + runtime
// scope (the same resolver the send path uses), returning a copy with concrete
// values so the engine never has to know about Yon's templating. The original
// cfg (which still carries {{names}}) is left untouched; only the returned copy
// is handed to the manager.
//
// Both the cache KEY and the request the manager makes are derived from the
// resolved config, so a token fetched via "Get Token" for the resolved values
// is the same one a later send reuses (Status/Token agree on the key).
func (w *Window) resolveOAuth2Config(cfg model.OAuth2Config) model.OAuth2Config {
	resolve := w.varScope().Resolve
	out := cfg
	out.TokenURL = resolve(cfg.TokenURL)
	out.AuthURL = resolve(cfg.AuthURL)
	out.ClientID = resolve(cfg.ClientID)
	out.ClientSecret = resolve(cfg.ClientSecret)
	out.RedirectURI = resolve(cfg.RedirectURI)
	out.Scopes = resolve(cfg.Scopes)
	out.Audience = resolve(cfg.Audience)
	return out
}

// browserOpener returns the oauth.BrowserOpener used to launch the user's
// browser for the authorization_code grant. It is backed by the same OS "open"
// helper used for the PDF "Open" action (issue #16), keeping the OAuth engine
// Fyne-/OS-free: the engine asks for a URL to be opened and this glue decides
// how. A nil-safe identity is unnecessary — the engine only calls it when the
// configured grant actually requires user interaction.
func (w *Window) browserOpener() oauth.BrowserOpener {
	return func(url string) error { return updater.OpenFile(url) }
}

// oauthGetToken runs the "Get Token" flow for cfg off the UI goroutine and
// reports the outcome back on it via onDone. It first resolves {{variables}} in
// cfg, then asks the app-wide manager for a token — which, for the
// authorization_code grant, opens the browser via browserOpener and blocks on
// the redirect callback; running on a goroutine keeps the UI responsive
// throughout. On completion onDone is invoked on the Fyne goroutine (so the
// caller may touch widgets) with a human status string from oauthStatusText, or
// the error. A context timeout guarantees the goroutine unwinds even if the
// user abandons the browser flow.
func (w *Window) oauthGetToken(cfg model.OAuth2Config, onDone func(status string, err error)) {
	resolved := w.resolveOAuth2Config(cfg)
	open := w.browserOpener()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), oauthFlowTimeout)
		defer cancel()
		_, err := w.app.oauth.Token(ctx, resolved, open)
		// Compute the post-fetch status on this goroutine (Status is a cheap map
		// read), then hand both back to the UI goroutine. Recomputing via
		// oauthStatusText keeps the displayed string identical to a fresh open of
		// the editor.
		status := w.oauthStatusText(cfg)
		if onDone == nil {
			return
		}
		fyne.Do(func() { onDone(status, err) })
	}()
}

// oauthStatusText returns a one-line, human-readable description of the cached
// token state for cfg, suitable for the editor's token-status label. It
// resolves {{variables}} first so it reads the same cache slot the send path
// will. Formats:
//
//	"No token yet"                       — nothing cached for this config
//	"Token acquired"                     — a token is cached with no known expiry
//	"Token acquired · expires 15:04:05"  — a token is cached with an expiry
func (w *Window) oauthStatusText(cfg model.OAuth2Config) string {
	resolved := w.resolveOAuth2Config(cfg)
	ts, ok := w.app.oauth.Status(resolved)
	if !ok || ts.AccessToken == "" {
		return "No token yet"
	}
	if ts.Expiry.IsZero() {
		return "Token acquired"
	}
	return "Token acquired · expires " + ts.Expiry.Format("15:04:05")
}

// oauth2TokenProvider builds the yonner.Options.OAuth2Token callback for a send.
// yonner calls it (with the request's resolved OAuth2Config) only when the
// effective Auth is AuthOAuth2; it returns a Bearer access token. The token is
// normally already cached from an earlier "Get Token", so this returns fast;
// for client_credentials it can also fetch on demand (no browser needed). The
// returned func re-resolves {{variables}} against the window's CURRENT scope so
// a send always uses the live environment values.
func (w *Window) oauth2TokenProvider() func(*model.OAuth2Config) (string, error) {
	open := w.browserOpener()
	return func(cfg *model.OAuth2Config) (string, error) {
		resolved := w.resolveOAuth2Config(*cfg)
		ctx, cancel := context.WithTimeout(context.Background(), oauthFlowTimeout)
		defer cancel()
		return w.app.oauth.Token(ctx, resolved, open)
	}
}
