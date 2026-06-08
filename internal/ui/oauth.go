package ui

import "github.com/ultramcu/yon/internal/model"

// This file is the UI-side contract for OAuth 2.0 (issue #30). The OAuth2 auth
// EDITOR (Dev C) calls these Window methods; the WIRING lane (Dev D) implements
// them — an app-level oauth.Manager, the BrowserOpener, the "Get Token" flow,
// the token-status text, and the yonner Options.OAuth2Token provider used on
// send. Stubs panic; Dev D overwrites this file.

// oauthGetToken runs the OAuth 2.0 flow for cfg and stores the resulting token
// in the app's session token manager, then calls onDone with a short status
// string (or a non-nil error). It resolves {{variables}} in cfg first. For the
// authorization_code grant it opens the browser and runs the loopback listener;
// callers should treat it as asynchronous (onDone fires on the UI thread).
//
// LANE: Dev D (wiring).
func (w *Window) oauthGetToken(cfg model.OAuth2Config, onDone func(status string, err error)) {
	panic("ui.oauthGetToken: not implemented")
}

// oauthStatusText returns a short human description of the cached token for cfg,
// e.g. "No token yet" or "Token acquired · expires 12:34:56". Used by the editor
// to show token state.
//
// LANE: Dev D (wiring).
func (w *Window) oauthStatusText(cfg model.OAuth2Config) string {
	panic("ui.oauthStatusText: not implemented")
}
