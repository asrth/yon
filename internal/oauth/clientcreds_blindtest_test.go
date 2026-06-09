package oauth

// Contract ("blind") tests for the client-credentials engine lane of Yon's
// OAuth 2.0 support (issue #30). These exercise the package through its public
// API only (ClientCredentialsToken, Refresh, Manager, TokenSet) against
// httptest token servers, asserting the documented behaviour without depending
// on implementation internals. Fixtures use the unique prefix ccBT.

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ultramcu/yon/internal/model"
)

// ccBTBasicHeader returns the value the Authorization header should hold for the
// HTTP Basic client-authentication style with the given id/secret.
func ccBTBasicHeader(id, secret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(id+":"+secret))
}

// TestCcBTClientCredentialsHappyPath: a token server asserts the grant type,
// HTTP Basic client auth, and scope, and returns a token with expires_in. The
// resulting TokenSet carries the access token, token type, and a future expiry.
func TestCcBTClientCredentialsHappyPath(t *testing.T) {
	const (
		ccBTID     = "ccBTclient"
		ccBTSecret = "ccBTsecret"
		ccBTScope  = "read write"
		ccBTAccess = "ccBTaccess-token"
	)

	var gotForm bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotForm = true
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if got := r.PostForm.Get("grant_type"); got != "client_credentials" {
			t.Errorf("grant_type = %q, want client_credentials", got)
		}
		if got := r.PostForm.Get("scope"); got != ccBTScope {
			t.Errorf("scope = %q, want %q", got, ccBTScope)
		}
		if got := r.Header.Get("Authorization"); got != ccBTBasicHeader(ccBTID, ccBTSecret) {
			t.Errorf("Authorization = %q, want Basic %s:%s", got, ccBTID, ccBTSecret)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + ccBTAccess + `","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	cfg := model.OAuth2Config{
		Grant:        model.GrantClientCredentials,
		TokenURL:     srv.URL,
		ClientID:     ccBTID,
		ClientSecret: ccBTSecret,
		Scopes:       ccBTScope,
		ClientAuth:   model.OAuth2ClientAuthBasic,
	}

	ts, err := ClientCredentialsToken(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ClientCredentialsToken: %v", err)
	}
	if !gotForm {
		t.Fatal("token server was never called")
	}
	if ts.AccessToken != ccBTAccess {
		t.Errorf("AccessToken = %q, want %q", ts.AccessToken, ccBTAccess)
	}
	if ts.TokenType != "Bearer" {
		t.Errorf("TokenType = %q, want Bearer", ts.TokenType)
	}
	if !ts.Expiry.After(time.Now()) {
		t.Errorf("Expiry = %v, want a time in the future", ts.Expiry)
	}
	if !ts.Valid(time.Now()) {
		t.Error("expected the freshly-minted token to be Valid")
	}
}

// TestCcBTClientAuthBodyStyle: with OAuth2ClientAuthBody the server must see
// client_id/client_secret as form fields and must NOT receive a Basic
// Authorization header.
func TestCcBTClientAuthBodyStyle(t *testing.T) {
	const (
		ccBTID     = "ccBTbodyclient"
		ccBTSecret = "ccBTbodysecret"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if got := r.PostForm.Get("client_id"); got != ccBTID {
			t.Errorf("client_id form field = %q, want %q", got, ccBTID)
		}
		if got := r.PostForm.Get("client_secret"); got != ccBTSecret {
			t.Errorf("client_secret form field = %q, want %q", got, ccBTSecret)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization header = %q, want none for body-style auth", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ccBTbody-access","token_type":"Bearer","expires_in":60}`))
	}))
	defer srv.Close()

	cfg := model.OAuth2Config{
		Grant:        model.GrantClientCredentials,
		TokenURL:     srv.URL,
		ClientID:     ccBTID,
		ClientSecret: ccBTSecret,
		ClientAuth:   model.OAuth2ClientAuthBody,
	}

	ts, err := ClientCredentialsToken(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ClientCredentialsToken: %v", err)
	}
	if ts.AccessToken != "ccBTbody-access" {
		t.Errorf("AccessToken = %q, want ccBTbody-access", ts.AccessToken)
	}
}

// TestCcBTErrorResponse: a 401 with an RFC 6749 error/error_description body
// surfaces as an error mentioning both the error code and the description.
func TestCcBTErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"bad secret"}`))
	}))
	defer srv.Close()

	cfg := model.OAuth2Config{
		Grant:        model.GrantClientCredentials,
		TokenURL:     srv.URL,
		ClientID:     "ccBTbad",
		ClientSecret: "ccBTwrong",
	}

	_, err := ClientCredentialsToken(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected an error from a 401 token response, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "invalid_client") {
		t.Errorf("error %q should mention invalid_client", msg)
	}
	if !strings.Contains(msg, "bad secret") {
		t.Errorf("error %q should mention the error_description 'bad secret'", msg)
	}
}

// TestCcBTRefreshRetainsRefreshToken: Refresh sends grant_type=refresh_token;
// when the response omits a refresh_token the original one is retained.
func TestCcBTRefreshRetainsRefreshToken(t *testing.T) {
	const ccBTOldRefresh = "ccBToldrefresh"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if got := r.PostForm.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}
		if got := r.PostForm.Get("refresh_token"); got != ccBTOldRefresh {
			t.Errorf("refresh_token = %q, want %q", got, ccBTOldRefresh)
		}
		w.Header().Set("Content-Type", "application/json")
		// Deliberately no refresh_token in the response.
		_, _ = w.Write([]byte(`{"access_token":"ccBTrefreshed-access","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	cfg := model.OAuth2Config{
		Grant:        model.GrantClientCredentials,
		TokenURL:     srv.URL,
		ClientID:     "ccBTrefreshclient",
		ClientSecret: "ccBTrefreshsecret",
	}

	ts, err := Refresh(context.Background(), cfg, ccBTOldRefresh)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if ts.AccessToken != "ccBTrefreshed-access" {
		t.Errorf("AccessToken = %q, want ccBTrefreshed-access", ts.AccessToken)
	}
	if ts.RefreshToken != ccBTOldRefresh {
		t.Errorf("RefreshToken = %q, want the original %q to be retained", ts.RefreshToken, ccBTOldRefresh)
	}
}

// TestCcBTManagerCaches: the first Manager.Token call hits the server; a second
// call for the same config returns the same token with no further request. A
// config with a different ClientID does not share the cache.
func TestCcBTManagerCaches(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		// Encode the call count so distinct fetches yield distinct tokens.
		_, _ = w.Write([]byte(`{"access_token":"ccBTtoken-` + itoaCcBT(calls) + `","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	mgr := NewManager()
	cfgA := model.OAuth2Config{
		Grant:        model.GrantClientCredentials,
		TokenURL:     srv.URL,
		ClientID:     "ccBTcacheA",
		ClientSecret: "ccBTsecretA",
	}

	tok1, err := mgr.Token(context.Background(), cfgA, nil)
	if err != nil {
		t.Fatalf("first Token: %v", err)
	}
	if calls != 1 {
		t.Fatalf("after first Token, server calls = %d, want 1", calls)
	}

	tok2, err := mgr.Token(context.Background(), cfgA, nil)
	if err != nil {
		t.Fatalf("second Token: %v", err)
	}
	if calls != 1 {
		t.Errorf("after cached second Token, server calls = %d, want still 1", calls)
	}
	if tok2 != tok1 {
		t.Errorf("cached token = %q, want the same as first %q", tok2, tok1)
	}

	// A distinct config (different ClientID) must not reuse the cache.
	cfgB := cfgA
	cfgB.ClientID = "ccBTcacheB"
	tok3, err := mgr.Token(context.Background(), cfgB, nil)
	if err != nil {
		t.Fatalf("Token for distinct config: %v", err)
	}
	if calls != 2 {
		t.Errorf("after distinct-config Token, server calls = %d, want 2", calls)
	}
	if tok3 == tok1 {
		t.Errorf("distinct config returned the same token %q; caches must not be shared", tok3)
	}
}

// TestCcBTManagerRefreshesExpired: a Manager seeded (via Set) with an EXPIRED
// token that carries a refresh token uses the refresh endpoint on Token and
// returns the new access token.
func TestCcBTManagerRefreshesExpired(t *testing.T) {
	var sawRefresh bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if r.PostForm.Get("grant_type") == "refresh_token" {
			sawRefresh = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ccBTafter-refresh","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	cfg := model.OAuth2Config{
		Grant:        model.GrantClientCredentials,
		TokenURL:     srv.URL,
		ClientID:     "ccBTrefreshmgr",
		ClientSecret: "ccBTrefreshmgrsecret",
	}

	mgr := NewManager()
	mgr.Set(cfg, TokenSet{
		AccessToken:  "ccBTstale-access",
		RefreshToken: "ccBTseed-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour), // already expired
	})

	tok, err := mgr.Token(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if !sawRefresh {
		t.Error("expected the refresh_token grant to be used for an expired cached token")
	}
	if tok != "ccBTafter-refresh" {
		t.Errorf("token = %q, want the refreshed ccBTafter-refresh", tok)
	}

	// The refreshed token should now be the cached status.
	ts, ok := mgr.Status(cfg)
	if !ok {
		t.Fatal("Status reports nothing cached after a refresh")
	}
	if ts.AccessToken != "ccBTafter-refresh" {
		t.Errorf("cached AccessToken = %q, want ccBTafter-refresh", ts.AccessToken)
	}
}

// TestCcBTTokenSetValidExpired: a token lapsing within the expiry skew counts as
// expired; a zero-Expiry token is never expired and is valid.
func TestCcBTTokenSetValidExpired(t *testing.T) {
	now := time.Now()

	// Expires in 10s — inside the ~30s skew, so it is treated as expired.
	soon := TokenSet{AccessToken: "ccBTsoon", Expiry: now.Add(10 * time.Second)}
	if !soon.Expired(now) {
		t.Error("token expiring in 10s should be Expired under the skew")
	}
	if soon.Valid(now) {
		t.Error("token expiring in 10s should NOT be Valid under the skew")
	}

	// Zero Expiry — never expires.
	noExpiry := TokenSet{AccessToken: "ccBTforever"}
	if noExpiry.Expired(now) {
		t.Error("zero-Expiry token should never be Expired")
	}
	if !noExpiry.Valid(now) {
		t.Error("zero-Expiry token with an access token should be Valid")
	}

	// A token comfortably in the future is valid.
	future := TokenSet{AccessToken: "ccBTfuture", Expiry: now.Add(time.Hour)}
	if !future.Valid(now) {
		t.Error("token expiring in an hour should be Valid")
	}
}

// itoaCcBT renders a small non-negative int without importing strconv, keeping
// the helper local to this contract-test file.
func itoaCcBT(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
