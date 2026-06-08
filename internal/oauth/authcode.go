package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ultramcu/yon/internal/model"
)

// authCodeTimeout bounds how long AuthorizationCodeToken waits for the user to
// complete the browser authorization and the loopback redirect to arrive. A
// never-completed flow fails with a timeout instead of hanging forever.
const authCodeTimeout = 3 * time.Minute

// defaultCallbackPath is used for the loopback redirect when cfg.RedirectURI is
// empty (or carries no path).
const defaultCallbackPath = "/callback"

// AuthorizationCodeToken performs the authorization_code grant (PKCE when
// cfg.UsePKCE): it opens cfg.AuthURL in the browser via open, runs a loopback
// HTTP listener (cfg.RedirectURI, e.g. http://127.0.0.1:0/callback) to catch the
// authorization code, and exchanges it for tokens at cfg.TokenURL. It validates
// the state parameter and times out / honours ctx cancellation.
//
// LANE: authorization-code/PKCE/loopback engine (Dev B overwrites this file).
func AuthorizationCodeToken(ctx context.Context, cfg model.OAuth2Config, open BrowserOpener) (TokenSet, error) {
	if open == nil {
		return TokenSet{}, fmt.Errorf("oauth: authorization_code requires a browser opener")
	}
	if cfg.AuthURL == "" {
		return TokenSet{}, fmt.Errorf("oauth: authorization_code requires an authorization URL")
	}
	if cfg.TokenURL == "" {
		return TokenSet{}, fmt.Errorf("oauth: authorization_code requires a token URL")
	}

	// 1. Decide the loopback bind address + callback path from cfg.RedirectURI.
	host, path, err := loopbackTarget(cfg.RedirectURI)
	if err != nil {
		return TokenSet{}, err
	}

	ln, err := net.Listen("tcp", host)
	if err != nil {
		return TokenSet{}, fmt.Errorf("oauth: cannot start loopback listener: %w", err)
	}
	// The actual redirect URI reflects the OS-assigned port (when host's port
	// was 0) so the auth server and the token exchange agree on it exactly.
	redirectURI := (&url.URL{Scheme: "http", Host: ln.Addr().String(), Path: path}).String()

	// 2. PKCE (when requested).
	var verifier, challenge string
	if cfg.UsePKCE {
		verifier, err = randomURLSafe(64)
		if err != nil {
			ln.Close()
			return TokenSet{}, fmt.Errorf("oauth: generating PKCE verifier: %w", err)
		}
		sum := sha256.Sum256([]byte(verifier))
		challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	}

	// 3. State + authorization URL.
	state, err := randomURLSafe(24)
	if err != nil {
		ln.Close()
		return TokenSet{}, fmt.Errorf("oauth: generating state: %w", err)
	}

	authURL, err := buildAuthURL(cfg, redirectURI, state, challenge)
	if err != nil {
		ln.Close()
		return TokenSet{}, err
	}

	// 4. Wire up the loopback handler before opening the browser so the redirect
	// is guaranteed to find the server listening.
	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		if e := q.Get("error"); e != "" {
			msg := e
			if d := q.Get("error_description"); d != "" {
				msg = d
			}
			writeCallbackPage(w, false, msg)
			done <- result{err: fmt.Errorf("oauth: authorization failed: %s", msg)}
			return
		}
		if got := q.Get("state"); got != state {
			writeCallbackPage(w, false, "state mismatch")
			done <- result{err: fmt.Errorf("oauth: state mismatch (possible CSRF); ignoring callback")}
			return
		}
		code := q.Get("code")
		if code == "" {
			writeCallbackPage(w, false, "missing authorization code")
			done <- result{err: fmt.Errorf("oauth: callback missing authorization code")}
			return
		}
		writeCallbackPage(w, true, "")
		done <- result{code: code}
	})

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln) // returns when srv.Shutdown/Close is called

	// Ensure the server is always torn down on the way out.
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	// 3 (cont). Hand the user off to the browser.
	if err := open(authURL); err != nil {
		return TokenSet{}, fmt.Errorf("oauth: opening browser: %w", err)
	}

	// 4 (cont). Wait for the redirect, ctx cancellation, or the timeout.
	timer := time.NewTimer(authCodeTimeout)
	defer timer.Stop()

	var code string
	select {
	case res := <-done:
		if res.err != nil {
			return TokenSet{}, res.err
		}
		code = res.code
	case <-ctx.Done():
		return TokenSet{}, fmt.Errorf("oauth: authorization cancelled: %w", ctx.Err())
	case <-timer.C:
		return TokenSet{}, fmt.Errorf("oauth: timed out after %s waiting for the authorization redirect", authCodeTimeout)
	}

	// 5. Exchange the code for tokens.
	return exchangeAuthCode(ctx, cfg, code, redirectURI, verifier)
}

// loopbackTarget derives the listen address (host:port) and callback path from a
// configured redirect URI. An empty redirectURI, or one whose port is 0/absent,
// binds 127.0.0.1:0 (OS-assigned free port). The path defaults to /callback.
func loopbackTarget(redirectURI string) (host, path string, err error) {
	if redirectURI == "" {
		return "127.0.0.1:0", defaultCallbackPath, nil
	}
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "", "", fmt.Errorf("oauth: invalid redirect URI %q: %w", redirectURI, err)
	}
	h := u.Hostname()
	if h == "" {
		h = "127.0.0.1"
	}
	port := u.Port() // "" when absent
	if port == "" {
		port = "0"
	}
	p := u.Path
	if p == "" || p == "/" {
		p = defaultCallbackPath
	}
	return net.JoinHostPort(h, port), p, nil
}

// buildAuthURL builds the authorization endpoint URL with the standard
// authorization_code query parameters, adding the PKCE challenge when present.
func buildAuthURL(cfg model.OAuth2Config, redirectURI, state, challenge string) (string, error) {
	u, err := url.Parse(cfg.AuthURL)
	if err != nil {
		return "", fmt.Errorf("oauth: invalid authorization URL %q: %w", cfg.AuthURL, err)
	}
	q := u.Query() // preserve any params already on the configured AuthURL
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	if cfg.Scopes != "" {
		q.Set("scope", cfg.Scopes)
	}
	if cfg.Audience != "" {
		q.Set("audience", cfg.Audience)
	}
	if challenge != "" {
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// exchangeAuthCode POSTs the token endpoint to swap the authorization code for a
// TokenSet, applying client authentication per cfg.ClientAuth.
func exchangeAuthCode(ctx context.Context, cfg model.OAuth2Config, code, redirectURI, verifier string) (TokenSet, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", cfg.ClientID)
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}

	// Client auth: Basic header by default, or client_secret in the body. With
	// no secret (a public PKCE client) the client_id already in the body is
	// enough, so we send neither a Basic header nor a client_secret field.
	useBasic := cfg.ClientAuth != model.OAuth2ClientAuthBody && cfg.ClientSecret != ""
	if cfg.ClientAuth == model.OAuth2ClientAuthBody && cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenSet{}, fmt.Errorf("oauth: building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if useBasic {
		// RFC 6749 §2.3.1: form-url-encode the id/secret before Basic-encoding.
		req.SetBasicAuth(url.QueryEscape(cfg.ClientID), url.QueryEscape(cfg.ClientSecret))
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return TokenSet{}, fmt.Errorf("oauth: token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return TokenSet{}, fmt.Errorf("oauth: reading token response: %w", err)
	}

	var tr acTokenResponse
	// Body may be empty/non-JSON on some error responses; ignore the decode
	// error and fall back to status-based reporting below.
	_ = json.Unmarshal(body, &tr)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || tr.Error != "" {
		if msg := tr.errorMessage(); msg != "" {
			return TokenSet{}, fmt.Errorf("oauth: token endpoint error: %s", msg)
		}
		return TokenSet{}, fmt.Errorf("oauth: token endpoint returned HTTP %d", resp.StatusCode)
	}
	if tr.AccessToken == "" {
		return TokenSet{}, fmt.Errorf("oauth: token response missing access_token")
	}

	return tr.toTokenSet(time.Now()), nil
}

// acTokenResponse is the standard OAuth 2.0 token-endpoint JSON response. Kept
// local to this file so the authorization_code lane does not depend on the
// client_credentials lane's unexported helpers.
type acTokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// errorMessage returns the most descriptive error string the server provided.
func (tr acTokenResponse) errorMessage() string {
	if tr.ErrorDescription != "" {
		return tr.ErrorDescription
	}
	return tr.Error
}

// toTokenSet converts the parsed response into a TokenSet, computing Expiry from
// expires_in relative to now (zero ExpiresIn → no known expiry).
func (tr acTokenResponse) toTokenSet(now time.Time) TokenSet {
	ts := TokenSet{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
	}
	if ts.TokenType == "" {
		ts.TokenType = "Bearer"
	}
	if tr.ExpiresIn > 0 {
		ts.Expiry = now.Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return ts
}

// randomURLSafe returns a cryptographically random string of n unreserved
// (base64url, no padding) characters. With n in [43,128] it is a valid PKCE
// code_verifier. It reads enough random bytes to cover n base64 chars.
func randomURLSafe(n int) (string, error) {
	if n <= 0 {
		return "", fmt.Errorf("oauth: random length must be positive")
	}
	nbytes := (n*3 + 3) / 4 // enough base64 chars to cover n
	b := make([]byte, nbytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := base64.RawURLEncoding.EncodeToString(b)
	return s[:n], nil
}

// writeCallbackPage renders the minimal "you can close this tab" page shown in
// the browser after the redirect. ok=false shows the (escaped) failure reason.
func writeCallbackPage(w http.ResponseWriter, ok bool, reason string) {
	var body string
	if ok {
		body = "<h1>Authorization complete</h1><p>You can close this tab and return to Yon.</p>"
	} else {
		body = "<h1>Authorization failed</h1><p>" + html.EscapeString(reason) +
			"</p><p>You can close this tab and return to Yon.</p>"
	}
	fmt.Fprintf(w, "<!doctype html><html><head><meta charset=\"utf-8\">"+
		"<title>Yon OAuth</title></head><body>%s</body></html>", body)
}
