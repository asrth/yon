package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/ultramcu/yon/internal/model"
)

// OAuth2 grant Select labels and their model.OAuth2Grant mapping (issue #30).
const (
	oauthGrantLabelClientCreds = "Client Credentials"
	oauthGrantLabelAuthCode    = "Authorization Code (PKCE)"
)

// OAuth2 "Client auth" Select labels.
const (
	oauthClientAuthLabelBasic = "Basic header"
	oauthClientAuthLabelBody  = "Body"
)

// defaultRedirectURI is the placeholder loopback redirect for the
// authorization_code grant; the engine binds an ephemeral port (:0).
const defaultRedirectURI = "http://127.0.0.1:0/callback"

// grantFromLabel / grantLabel map between the grant Select labels and the model.
func grantFromLabel(label string) model.OAuth2Grant {
	if label == oauthGrantLabelAuthCode {
		return model.GrantAuthorizationCode
	}
	return model.GrantClientCredentials
}

func grantLabel(g model.OAuth2Grant) string {
	if g == model.GrantAuthorizationCode {
		return oauthGrantLabelAuthCode
	}
	return oauthGrantLabelClientCreds
}

func clientAuthFromLabel(label string) model.OAuth2ClientAuthStyle {
	if label == oauthClientAuthLabelBody {
		return model.OAuth2ClientAuthBody
	}
	return model.OAuth2ClientAuthBasic
}

func clientAuthLabel(s model.OAuth2ClientAuthStyle) string {
	if s == model.OAuth2ClientAuthBody {
		return oauthClientAuthLabelBody
	}
	return oauthClientAuthLabelBasic
}

// buildOAuth2Fields constructs the OAuth 2.0 form widgets once and seeds them
// from cfg (nil = empty defaults). It is called from newAuthEditor before the
// kindSelect is wired, so editing handlers fire onChange but constructing does
// not. The Authorization-Code-only rows are shown/hidden by
// refreshOAuth2Visibility (called from rebuildFields and on grant change).
func (ae *authEditor) buildOAuth2Fields(cfg *model.OAuth2Config) {
	if cfg == nil {
		cfg = &model.OAuth2Config{}
	}

	ae.oauthGrant = widget.NewSelect(
		[]string{oauthGrantLabelClientCreds, oauthGrantLabelAuthCode}, nil)
	ae.oauthGrant.SetSelected(grantLabel(cfg.Grant))
	ae.oauthGrant.OnChanged = func(string) {
		ae.refreshOAuth2Visibility()
		ae.fire()
	}

	ae.oauthTokenURL = widget.NewEntry()
	ae.oauthTokenURL.SetPlaceHolder("https://issuer.example.com/oauth/token")
	ae.oauthTokenURL.SetText(cfg.TokenURL)
	ae.oauthTokenURL.OnChanged = func(string) { ae.fire() }

	ae.oauthAuthURL = widget.NewEntry()
	ae.oauthAuthURL.SetPlaceHolder("https://issuer.example.com/oauth/authorize")
	ae.oauthAuthURL.SetText(cfg.AuthURL)
	ae.oauthAuthURL.OnChanged = func(string) { ae.fire() }

	ae.oauthClientID = widget.NewEntry()
	ae.oauthClientID.SetText(cfg.ClientID)
	ae.oauthClientID.OnChanged = func(string) { ae.fire() }

	ae.oauthClientSecret = widget.NewPasswordEntry()
	ae.oauthClientSecret.SetText(cfg.ClientSecret)
	ae.oauthClientSecret.OnChanged = func(string) { ae.fire() }

	ae.oauthScopes = widget.NewEntry()
	ae.oauthScopes.SetPlaceHolder("space separated, e.g. openid profile")
	ae.oauthScopes.SetText(cfg.Scopes)
	ae.oauthScopes.OnChanged = func(string) { ae.fire() }

	ae.oauthAudience = widget.NewEntry()
	ae.oauthAudience.SetPlaceHolder("optional")
	ae.oauthAudience.SetText(cfg.Audience)
	ae.oauthAudience.OnChanged = func(string) { ae.fire() }

	ae.oauthRedirectURI = widget.NewEntry()
	ae.oauthRedirectURI.SetPlaceHolder(defaultRedirectURI)
	ae.oauthRedirectURI.SetText(cfg.RedirectURI)
	ae.oauthRedirectURI.OnChanged = func(string) { ae.fire() }

	ae.oauthUsePKCE = widget.NewCheck("Use PKCE (S256)", func(bool) { ae.fire() })
	ae.oauthUsePKCE.SetChecked(cfg.UsePKCE)

	ae.oauthClientAuth = widget.NewSelect(
		[]string{oauthClientAuthLabelBasic, oauthClientAuthLabelBody}, nil)
	ae.oauthClientAuth.SetSelected(clientAuthLabel(cfg.ClientAuth))
	ae.oauthClientAuth.OnChanged = func(string) { ae.fire() }

	ae.oauthStatus = widget.NewLabel("")

	getTokenBtn := widget.NewButton("Get Token", func() {
		if ae.onGetToken != nil {
			ae.onGetToken(ae.oauth2Value())
		}
	})

	// The form is assembled once; refreshOAuth2Visibility hides the
	// Authorization-Code-only rows for the client_credentials grant.
	ae.oauth2Box = container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("Grant", ae.oauthGrant),
			widget.NewFormItem("Token URL", ae.oauthTokenURL),
			widget.NewFormItem("Auth URL", ae.oauthAuthURL),
			widget.NewFormItem("Client ID", ae.oauthClientID),
			widget.NewFormItem("Client Secret", ae.oauthClientSecret),
			widget.NewFormItem("Scopes", ae.oauthScopes),
			widget.NewFormItem("Audience", ae.oauthAudience),
			widget.NewFormItem("Redirect URI", ae.oauthRedirectURI),
			widget.NewFormItem("Client auth", ae.oauthClientAuth),
		),
		ae.oauthUsePKCE,
		container.NewHBox(getTokenBtn, ae.oauthStatus),
	)
}

// refreshOAuth2Visibility shows the Authorization-Code-only rows (Auth URL,
// Redirect URI, PKCE) only for the authorization_code grant. The form items
// are kept in the layout; their widgets are hidden/shown so the form keeps a
// stable structure (Fyne forms don't support removing rows cleanly).
func (ae *authEditor) refreshOAuth2Visibility() {
	authCode := grantFromLabel(ae.oauthGrant.Selected) == model.GrantAuthorizationCode
	for _, w := range []fyne.CanvasObject{ae.oauthAuthURL, ae.oauthRedirectURI, ae.oauthUsePKCE} {
		if authCode {
			w.Show()
		} else {
			w.Hide()
		}
	}
	if ae.oauth2Box != nil {
		ae.oauth2Box.Refresh()
	}
}

// oauth2Value reads the current OAuth2Config from the form. RedirectURI falls
// back to the default loopback placeholder when left blank for the
// authorization_code grant.
func (ae *authEditor) oauth2Value() model.OAuth2Config {
	cfg := model.OAuth2Config{
		Grant:        grantFromLabel(ae.oauthGrant.Selected),
		TokenURL:     ae.oauthTokenURL.Text,
		AuthURL:      ae.oauthAuthURL.Text,
		ClientID:     ae.oauthClientID.Text,
		ClientSecret: ae.oauthClientSecret.Text,
		Scopes:       ae.oauthScopes.Text,
		Audience:     ae.oauthAudience.Text,
		RedirectURI:  ae.oauthRedirectURI.Text,
		UsePKCE:      ae.oauthUsePKCE.Checked,
		ClientAuth:   clientAuthFromLabel(ae.oauthClientAuth.Selected),
	}
	if cfg.Grant == model.GrantAuthorizationCode && cfg.RedirectURI == "" {
		cfg.RedirectURI = defaultRedirectURI
	}
	return cfg
}

// setOAuthStatus updates the OAuth 2.0 status label. The owner (request editor /
// Window wiring) calls this to seed or refresh token state, e.g. from
// oauthStatusText or after a Get Token completes. Nil-safe before the form is
// built.
func (ae *authEditor) setOAuthStatus(s string) {
	if ae.oauthStatus == nil {
		return
	}
	ae.oauthStatus.SetText(s)
}
