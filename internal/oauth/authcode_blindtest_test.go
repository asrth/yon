package oauth

// Contract tests for the authorization-code/PKCE engine lane (issue #30),
// exercised through the public AuthorizationCodeToken API with a fake browser
// opener and an httptest token endpoint. Fixtures use the prefix acBT.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ultramcu/yon/internal/model"
)

// acBTTokenServer returns an httptest server that records the last token-request
// form values and replies with the given JSON body. *hits counts requests.
func acBTTokenServer(t *testing.T, hits *int32, lastForm *url.Values, respond func() string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		_ = r.ParseForm()
		if lastForm != nil {
			f := r.PostForm
			*lastForm = f
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respond()))
	}))
}

// acBTRedirectingOpener returns a BrowserOpener that parses the authorization
// URL, runs assertOn(query) for assertions, then (async) GETs the redirect_uri
// with the supplied code and the state taken from the auth URL (or overridden).
func acBTRedirectingOpener(t *testing.T, code string, overrideState string, assertOn func(url.Values)) BrowserOpener {
	t.Helper()
	return func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			t.Errorf("opener: bad auth URL %q: %v", authURL, err)
			return err
		}
		q := u.Query()
		if assertOn != nil {
			assertOn(q)
		}
		redirect := q.Get("redirect_uri")
		state := q.Get("state")
		if overrideState != "" {
			state = overrideState
		}
		go func() {
			cb := redirect + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
			resp, err := http.Get(cb)
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
}

func acBTjson(m map[string]any) string { b, _ := json.Marshal(m); return string(b) }

// 1. PKCE happy path: the auth URL carries PKCE params + state; the token
// exchange sends code + code_verifier + the bound redirect_uri.
func TestAcBTPKCEHappyPath(t *testing.T) {
	var hits int32
	var form url.Values
	srv := acBTTokenServer(t, &hits, &form, func() string {
		return acBTjson(map[string]any{"access_token": "AC_AT", "token_type": "Bearer", "expires_in": 3600, "refresh_token": "AC_RT"})
	})
	defer srv.Close()

	cfg := model.OAuth2Config{Grant: model.GrantAuthorizationCode, AuthURL: "https://auth.example/authorize", TokenURL: srv.URL, ClientID: "cid", Scopes: "read", UsePKCE: true}

	open := acBTRedirectingOpener(t, "THECODE", "", func(q url.Values) {
		if q.Get("response_type") != "code" {
			t.Errorf("response_type = %q", q.Get("response_type"))
		}
		if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
			t.Errorf("missing PKCE challenge: %v", q)
		}
		if q.Get("state") == "" {
			t.Error("missing state")
		}
		if q.Get("redirect_uri") == "" {
			t.Error("missing redirect_uri")
		}
	})

	ts, err := AuthorizationCodeToken(context.Background(), cfg, open)
	if err != nil {
		t.Fatalf("AuthorizationCodeToken: %v", err)
	}
	if ts.AccessToken != "AC_AT" || ts.RefreshToken != "AC_RT" {
		t.Fatalf("token = %+v", ts)
	}
	if form.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q", form.Get("grant_type"))
	}
	if form.Get("code") != "THECODE" {
		t.Errorf("code = %q", form.Get("code"))
	}
	if form.Get("code_verifier") == "" {
		t.Error("exchange missing code_verifier")
	}
	if form.Get("redirect_uri") == "" {
		t.Error("exchange missing redirect_uri")
	}
}

// 2. State mismatch is rejected and the token endpoint is never hit.
func TestAcBTStateMismatchRejected(t *testing.T) {
	var hits int32
	srv := acBTTokenServer(t, &hits, nil, func() string { return acBTjson(map[string]any{"access_token": "x"}) })
	defer srv.Close()

	cfg := model.OAuth2Config{Grant: model.GrantAuthorizationCode, AuthURL: "https://auth.example/authorize", TokenURL: srv.URL, ClientID: "cid"}
	open := acBTRedirectingOpener(t, "CODE", "WRONG_STATE", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_, err := AuthorizationCodeToken(ctx, cfg, open)
	if err == nil {
		t.Fatal("want an error on state mismatch")
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Errorf("token endpoint was hit %d times; want 0 on state mismatch", hits)
	}
}

// 3. Authorization-server error (error=access_denied) → error, no exchange.
func TestAcBTAuthServerError(t *testing.T) {
	var hits int32
	srv := acBTTokenServer(t, &hits, nil, func() string { return acBTjson(map[string]any{"access_token": "x"}) })
	defer srv.Close()

	cfg := model.OAuth2Config{Grant: model.GrantAuthorizationCode, AuthURL: "https://auth.example/authorize", TokenURL: srv.URL, ClientID: "cid"}
	open := func(authURL string) error {
		u, _ := url.Parse(authURL)
		redirect := u.Query().Get("redirect_uri")
		go func() {
			resp, err := http.Get(redirect + "?error=access_denied&error_description=nope")
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	_, err := AuthorizationCodeToken(context.Background(), cfg, open)
	if err == nil || !strings.Contains(err.Error(), "access_denied") && !strings.Contains(err.Error(), "nope") {
		t.Fatalf("want an access_denied error, got %v", err)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Errorf("token endpoint hit %d times; want 0", hits)
	}
}

// 4. No PKCE: the auth URL has no code_challenge and the exchange has no
// code_verifier, but the flow still succeeds.
func TestAcBTNoPKCE(t *testing.T) {
	var hits int32
	var form url.Values
	srv := acBTTokenServer(t, &hits, &form, func() string {
		return acBTjson(map[string]any{"access_token": "NOPKCE_AT", "token_type": "Bearer"})
	})
	defer srv.Close()

	cfg := model.OAuth2Config{Grant: model.GrantAuthorizationCode, AuthURL: "https://auth.example/authorize", TokenURL: srv.URL, ClientID: "cid", UsePKCE: false}
	open := acBTRedirectingOpener(t, "C2", "", func(q url.Values) {
		if q.Get("code_challenge") != "" {
			t.Errorf("unexpected code_challenge with PKCE off")
		}
	})
	ts, err := AuthorizationCodeToken(context.Background(), cfg, open)
	if err != nil {
		t.Fatalf("AuthorizationCodeToken: %v", err)
	}
	if ts.AccessToken != "NOPKCE_AT" {
		t.Fatalf("token = %+v", ts)
	}
	if form.Get("code_verifier") != "" {
		t.Errorf("unexpected code_verifier with PKCE off")
	}
}

// 5a. An opener that errors propagates the error.
func TestAcBTOpenerError(t *testing.T) {
	cfg := model.OAuth2Config{Grant: model.GrantAuthorizationCode, AuthURL: "https://auth.example/authorize", TokenURL: "https://unused.example/token", ClientID: "cid"}
	open := func(string) error { return context.Canceled }
	if _, err := AuthorizationCodeToken(context.Background(), cfg, open); err == nil {
		t.Fatal("want the opener error to propagate")
	}
}

// 5b. ctx cancellation returns promptly when the redirect never arrives.
func TestAcBTCtxCancel(t *testing.T) {
	cfg := model.OAuth2Config{Grant: model.GrantAuthorizationCode, AuthURL: "https://auth.example/authorize", TokenURL: "https://unused.example/token", ClientID: "cid"}
	open := func(string) error { return nil } // never redirects
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := AuthorizationCodeToken(ctx, cfg, open); err == nil {
		t.Fatal("want a ctx error when the redirect never arrives")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("ctx cancel took %v; want prompt", time.Since(start))
	}
}

// 6. Empty RedirectURI → the opener sees a real loopback http://127.0.0.1:PORT
// it can GET.
func TestAcBTActualRedirectPort(t *testing.T) {
	var hits int32
	srv := acBTTokenServer(t, &hits, nil, func() string {
		return acBTjson(map[string]any{"access_token": "RP_AT", "token_type": "Bearer"})
	})
	defer srv.Close()

	cfg := model.OAuth2Config{Grant: model.GrantAuthorizationCode, AuthURL: "https://auth.example/authorize", TokenURL: srv.URL, ClientID: "cid"}
	open := acBTRedirectingOpener(t, "RPCODE", "", func(q url.Values) {
		ru, err := url.Parse(q.Get("redirect_uri"))
		if err != nil {
			t.Errorf("redirect_uri parse: %v", err)
			return
		}
		if ru.Scheme != "http" || ru.Hostname() != "127.0.0.1" || ru.Port() == "" || ru.Port() == "0" {
			t.Errorf("redirect_uri = %q; want http://127.0.0.1:<port>/...", q.Get("redirect_uri"))
		}
	})
	ts, err := AuthorizationCodeToken(context.Background(), cfg, open)
	if err != nil {
		t.Fatalf("AuthorizationCodeToken: %v", err)
	}
	if ts.AccessToken != "RP_AT" {
		t.Fatalf("token = %+v", ts)
	}
}
