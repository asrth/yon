package oauth

import (
	"context"

	"github.com/ultramcu/yon/internal/model"
)

// AuthorizationCodeToken performs the authorization_code grant (PKCE when
// cfg.UsePKCE): it opens cfg.AuthURL in the browser via open, runs a loopback
// HTTP listener (cfg.RedirectURI, e.g. http://127.0.0.1:0/callback) to catch the
// authorization code, and exchanges it for tokens at cfg.TokenURL. It validates
// the state parameter and times out / honours ctx cancellation.
//
// LANE: authorization-code/PKCE/loopback engine (Dev B overwrites this file).
func AuthorizationCodeToken(ctx context.Context, cfg model.OAuth2Config, open BrowserOpener) (TokenSet, error) {
	panic("oauth.AuthorizationCodeToken: not implemented")
}
