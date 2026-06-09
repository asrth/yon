// Package yonner is Yon's build-and-send engine: it turns a model.Request
// (together with its Collection, for Auth inheritance) into a real *http.Request,
// sends it, and produces a model.Response.
//
// It depends only on the standard library and internal/model. It never imports
// Fyne or any UI package (the UI-free-core rule), so it is pure, testable Go that could also
// back a future CLI.
package yonner

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ultramcu/yon/internal/model"
)

// Options controls how Send dials and runs an HTTP request. These are the
// app-level connection defaults; v1 has no per-request override.
type Options struct {
	// Timeout is the total time budget for the request (dial + send + read).
	// A zero value means no client timeout (rely on ctx instead).
	Timeout time.Duration
	// InsecureTLS, when true, disables TLS certificate verification.
	InsecureTLS bool
	// FollowRedirects, when false, makes Send return the first (redirect)
	// response instead of following it.
	FollowRedirects bool
	// Resolve expands Postman-style {{variable}} templates in the outgoing
	// request's text fields (URL, enabled query params, enabled headers, body,
	// and resolved Basic/Bearer auth) just before the *http.Request is built.
	// It is applied per string value; the caller supplies whatever substitution
	// it wants (environment lookups, defaults, leaving unknown names intact).
	// A nil Resolve is treated as the identity function, so requests are sent
	// verbatim and existing behaviour is unchanged.
	Resolve func(string) string
	// OAuth2Token, when non-nil, resolves an OAuth 2.0 Auth (Kind AuthOAuth2)
	// into a Bearer access token just before the request is built. The UI
	// supplies it backed by the OAuth token manager (cached/refreshed token);
	// the core stays UI-free and network-free here. A nil provider, a nil
	// config, or an empty returned token sends no Authorization header; a
	// non-nil error from the provider fails BuildWith so the send surfaces the
	// OAuth problem.
	OAuth2Token func(*model.OAuth2Config) (string, error)
	// DialContext, when non-nil, replaces the transport's TCP dialer so every
	// Request is dialed THROUGH it instead of straight from this host. The
	// tunnel manager supplies the SSH-backed dialer for the active Environment's
	// jump host (see internal/tunnel); the target URL is never rewritten — only
	// how its connection is opened changes. nil = dial directly (today's
	// behaviour). When set, newClient also disables the HTTP proxy so the proxy
	// is not dialed through the SSH connection (ADR 0001).
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

// resolve applies o.Resolve to s, treating a nil Resolve as the identity
// function. All outgoing string values pass through here exactly once so
// substitution stays centralised and is never applied twice.
func (o Options) resolve(s string) string {
	if o.Resolve == nil {
		return s
	}
	return o.Resolve(s)
}

// DefaultOptions returns the standard connection defaults: a 30s timeout,
// certificate verification on, and redirect following enabled.
func DefaultOptions() Options {
	return Options{
		Timeout:         30 * time.Second,
		InsecureTLS:     false,
		FollowRedirects: true,
	}
}

// ApplyRequestOptions overlays a Request's per-request overrides onto base
// (the global options). A nil ro, or a nil field within it, leaves the
// corresponding base value unchanged. Resolve and DialContext are never
// touched. base is taken by value, so callers get a copy and the global
// options are not mutated.
func ApplyRequestOptions(base Options, ro *model.RequestOptions) Options {
	if ro == nil {
		return base
	}
	if ro.TimeoutSeconds != nil {
		// A value <= 0 means "no client timeout" — time.Duration(<=0)*Second is
		// <= 0, which Options.Timeout already treats as no timeout, so the
		// semantics carry through without a special case.
		base.Timeout = time.Duration(*ro.TimeoutSeconds) * time.Second
	}
	if ro.InsecureTLS != nil {
		base.InsecureTLS = *ro.InsecureTLS
	}
	if ro.FollowRedirects != nil {
		base.FollowRedirects = *ro.FollowRedirects
	}
	return base
}

// headerAuthorization is the canonical Authorization header name.
const headerAuthorization = "Authorization"

// headerContentType is the canonical Content-Type header name.
const headerContentType = "Content-Type"

// Build turns a Request (plus its Collection, for Auth inheritance) into a real
// *http.Request bound to ctx. It:
//
//   - parses req.URL and appends every enabled Param in req.Params as a query
//     parameter, preserving (concatenating with) any query already present in
//     the URL — the URL query and the Param table are never merged or
//     deduplicated, matching Yon's "no two-way sync" rule;
//   - sets every enabled Header from req.Headers;
//   - resolves Auth via model.ResolveAuth and applies it as an Authorization
//     header (basic or bearer), unless the user already supplied an enabled
//     Authorization header in the table — in which case the explicit header
//     wins and Auth is not applied;
//   - sends Body as-is for any Method (including GET) when Body.Type is not
//     "none" and Body.Content is non-empty, and for JSON bodies adds
//     Content-Type: application/json (XML bodies add application/xml) only when
//     the user has not already set a Content-Type header.
func Build(ctx context.Context, req model.Request, coll model.Collection) (*http.Request, error) {
	// No Options here, so Resolve is nil (identity): build the request verbatim.
	return BuildWith(ctx, req, coll, Options{})
}

// BuildWith is Build with explicit Options, so opts.Resolve can expand
// {{variable}} templates in the URL, enabled query params, enabled headers, the
// body, and the resolved Basic/Bearer auth just before the *http.Request is
// constructed. With a nil opts.Resolve it is identical to Build. Send uses this
// form so the variables a caller configures take effect on the wire.
//
// Substitution is applied to outgoing string values only; the stored Request is
// never mutated and disabled entries are skipped before their values are read.
// The Content-Type auto-set logic runs on the post-substitution header keys, so
// a user-supplied (possibly templated) Content-Type still suppresses the JSON or
// XML default.
func BuildWith(ctx context.Context, req model.Request, coll model.Collection, opts Options) (*http.Request, error) {
	// Build the URL with enabled query params appended in order (see requestURL,
	// shared with ToCurl). The resolver expands the URL and each enabled param.
	finalURL, err := requestURL(req, opts)
	if err != nil {
		return nil, err
	}

	// Body: WYSIWYG — send for any method when present. buildBodyReader resolves
	// {{variable}} templates and reports the Content-Type the body type implies
	// (applied below only when the user did not set one explicitly).
	body, autoContentType, err := buildBodyReader(req.Body, opts.resolve)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, string(req.Method), finalURL, body)
	if err != nil {
		return nil, err
	}

	// Track whether the user explicitly set Authorization / Content-Type
	// (case-insensitive) so derived values never clobber explicit ones. The
	// check runs on the resolved key, so a templated header name is honoured.
	userSetAuth := false
	userSetContentType := false

	// Set enabled headers from the table. http.Header.Add canonicalizes keys,
	// so case differences collapse correctly. Both key and value are resolved.
	for _, h := range req.Headers {
		if !h.Enabled {
			continue
		}
		key := opts.resolve(h.Key)
		value := opts.resolve(h.Value)
		if strings.EqualFold(key, headerAuthorization) {
			userSetAuth = true
		}
		if strings.EqualFold(key, headerContentType) {
			userSetContentType = true
		}
		httpReq.Header.Add(key, value)
	}

	// Auth: derived Authorization, only when the user didn't set one explicitly.
	// ResolveAuth returns a value, so resolving its fields never mutates the
	// stored Request or Collection.
	if !userSetAuth {
		auth := model.ResolveAuth(req, coll)
		switch auth.Kind {
		case model.AuthBasic:
			httpReq.SetBasicAuth(opts.resolve(auth.Username), opts.resolve(auth.Password))
		case model.AuthBearer:
			httpReq.Header.Set(headerAuthorization, "Bearer "+opts.resolve(auth.Token))
		case model.AuthOAuth2:
			// OAuth 2.0: the injected provider returns a (cached/refreshed)
			// access token, sent as a Bearer. A nil provider/config or empty
			// token sends nothing; a provider error fails the build.
			if opts.OAuth2Token != nil && auth.OAuth2 != nil {
				tok, err := opts.OAuth2Token(auth.OAuth2)
				if err != nil {
					return nil, fmt.Errorf("oauth2: %w", err)
				}
				if tok != "" {
					httpReq.Header.Set(headerAuthorization, "Bearer "+tok)
				}
			}
		default:
			// "none" / "inherit"-resolved-to-none: send no Authorization.
		}
	}

	// Automatic Content-Type from the body type (JSON/XML/form/multipart), only
	// when the body implies one and the user did not set Content-Type explicitly.
	if autoContentType != "" && !userSetContentType {
		httpReq.Header.Set(headerContentType, autoContentType)
	}

	return httpReq, nil
}

// Send builds the request via Build and sends it with an *http.Client honoring
// opts (Timeout, InsecureTLS, FollowRedirects). It measures the round-trip
// Duration, reads the full body, and returns a model.Response. The supplied ctx
// governs cancellation and deadlines; a cancelled or timed-out ctx yields a
// meaningful error rather than a partial Response.
func Send(ctx context.Context, req model.Request, coll model.Collection, opts Options) (model.Response, error) {
	httpReq, err := BuildWith(ctx, req, coll, opts)
	if err != nil {
		return model.Response{}, err
	}

	client := newClient(opts)

	start := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		return model.Response{}, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	duration := time.Since(start)
	if err != nil {
		return model.Response{}, err
	}

	return model.Response{
		Status:     resp.StatusCode,
		StatusText: statusText(resp),
		Headers:    headersToParams(resp.Header),
		Body:       bodyBytes,
		Duration:   duration,
		Size:       int64(len(bodyBytes)),
	}, nil
}

// newClient builds an *http.Client configured from opts: total Timeout,
// InsecureTLS (InsecureSkipVerify on the transport's TLS config), and
// FollowRedirects (ErrUseLastResponse to stop following when false).
func newClient(opts Options) *http.Client {
	// Clone http.DefaultTransport so requests honour the standard environment
	// proxy (HTTP_PROXY/HTTPS_PROXY/NO_PROXY), HTTP/2, and the sane connection
	// timeouts — a desktop API client must work behind a corporate proxy.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if opts.InsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in app-level toggle
	}

	// SSH jump host: route every connection through the supplied dialer and turn
	// OFF the HTTP proxy. Otherwise the cloned DefaultTransport would still try to
	// reach HTTP_PROXY/HTTPS_PROXY — and it would do so THROUGH the SSH tunnel,
	// which is never what's wanted (ADR 0001). When DialContext is nil the
	// transport is left exactly as the clone above, so proxy and the default
	// dialer are honoured and existing behaviour is unchanged.
	if opts.DialContext != nil {
		transport.DialContext = opts.DialContext
		transport.Proxy = nil
	}

	client := &http.Client{
		Timeout:   opts.Timeout,
		Transport: transport,
	}
	if !opts.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client
}

// statusText returns a human-readable status: the standard text for the code,
// falling back to the raw resp.Status line when the code is unknown.
func statusText(resp *http.Response) string {
	if t := http.StatusText(resp.StatusCode); t != "" {
		return t
	}
	return resp.Status
}

// headersToParams flattens response headers into a []model.Param with each
// entry Enabled, preserving multi-value headers as separate Params.
func headersToParams(h http.Header) []model.Param {
	var params []model.Param
	for key, values := range h {
		for _, v := range values {
			params = append(params, model.Param{Key: key, Value: v, Enabled: true})
		}
	}
	return params
}
