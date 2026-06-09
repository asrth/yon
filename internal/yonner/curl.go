package yonner

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ultramcu/yon/internal/model"
)

// requestURL parses req.URL and appends its enabled Query Parameters, preserving
// the existing query and order (shared by Build and ToCurl). opts.Resolve
// expands {{variable}} templates in the URL and in each enabled param's key and
// value before they are parsed/escaped; a nil resolver leaves them verbatim.
func requestURL(req model.Request, opts Options) (string, error) {
	u, err := url.Parse(opts.resolve(strings.TrimSpace(req.URL)))
	if err != nil {
		return "", err
	}
	var qb strings.Builder
	qb.WriteString(u.RawQuery)
	for _, p := range req.Params {
		if !p.Enabled {
			continue
		}
		if qb.Len() > 0 {
			qb.WriteByte('&')
		}
		qb.WriteString(url.QueryEscape(opts.resolve(p.Key)))
		qb.WriteByte('=')
		qb.WriteString(url.QueryEscape(opts.resolve(p.Value)))
	}
	u.RawQuery = qb.String()
	return restoreTemplateBraces(u.String()), nil
}

// templateBraceRestorer turns the percent-encoded forms of the {{ }} template
// delimiters back into literal braces. url.Parse/url.String (for the path) and
// url.QueryEscape (for params) percent-encode '{' and '}', so an unresolved
// {{variable}} would otherwise render as %7B%7Bvariable%7D%7D in the built URL
// and copied curl. Only the *doubled* sequence is restored, which is the
// Postman template delimiter; a lone encoded brace from a real value (e.g. JSON
// in a query param) is left encoded. Go always emits uppercase percent hex, but
// lowercase is handled too for safety.
var templateBraceRestorer = strings.NewReplacer(
	"%7B%7B", "{{", "%7b%7b", "{{",
	"%7D%7D", "}}", "%7d%7d", "}}",
)

// restoreTemplateBraces applies templateBraceRestorer to s. See its doc.
func restoreTemplateBraces(s string) string {
	return templateBraceRestorer.Replace(s)
}

// ToCurl renders req as an equivalent `curl` command line, matching how Send
// builds and sends it: query params, enabled headers, resolved auth (explicit
// Authorization header wins), JSON auto Content-Type, and the body — plus the
// connection Options (-L follow redirects, -k insecure TLS, --max-time).
//
// opts.Resolve is applied to the same fields as BuildWith (URL, enabled query
// params, enabled headers, body, and resolved Basic/Bearer auth), so the copied
// curl reflects the variable-expanded request that is actually sent.
func ToCurl(req model.Request, coll model.Collection, opts Options) string {
	var b strings.Builder
	b.WriteString("curl")
	if opts.FollowRedirects {
		b.WriteString(" -L")
	}
	if opts.InsecureTLS {
		b.WriteString(" -k")
	}
	if secs := int(opts.Timeout.Seconds()); secs > 0 {
		fmt.Fprintf(&b, " --max-time %d", secs)
	}

	method := string(req.Method)
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet {
		b.WriteString(" -X " + method)
	}

	u := opts.resolve(strings.TrimSpace(req.URL))
	if full, err := requestURL(req, opts); err == nil {
		u = full
	}
	b.WriteString(" " + shellQuote(u))

	userSetAuth, userSetCT := false, false
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
			userSetCT = true
		}
		fmt.Fprintf(&b, " -H %s", shellQuote(key+": "+value))
	}

	if !userSetAuth {
		switch auth := model.ResolveAuth(req, coll); auth.Kind {
		case model.AuthBasic:
			fmt.Fprintf(&b, " -u %s", shellQuote(opts.resolve(auth.Username)+":"+opts.resolve(auth.Password)))
		case model.AuthBearer:
			fmt.Fprintf(&b, " -H %s", shellQuote("Authorization: Bearer "+opts.resolve(auth.Token)))
		}
	}

	switch req.Body.Type {
	case model.BodyForm:
		// curl sets the application/x-www-form-urlencoded Content-Type itself for
		// --data-urlencode, so we emit no Content-Type header here.
		for _, f := range req.Body.Fields {
			if !f.Enabled || f.Key == "" {
				continue
			}
			fmt.Fprintf(&b, " --data-urlencode %s", shellQuote(opts.resolve(f.Key)+"="+opts.resolve(f.Value)))
		}
	case model.BodyMultipart:
		// curl builds the multipart/form-data Content-Type (with boundary) itself
		// for -F, so we emit no Content-Type header here.
		for _, f := range req.Body.Fields {
			if !f.Enabled || f.Key == "" {
				continue
			}
			if f.IsFile {
				arg := opts.resolve(f.Key) + "=@" + opts.resolve(f.Value)
				if f.Filename != "" {
					arg += ";filename=" + opts.resolve(f.Filename)
				}
				fmt.Fprintf(&b, " -F %s", shellQuote(arg))
			} else {
				fmt.Fprintf(&b, " -F %s", shellQuote(opts.resolve(f.Key)+"="+opts.resolve(f.Value)))
			}
		}
	default:
		if req.Body.Type != model.BodyNone && req.Body.Content != "" {
			if req.Body.Type == model.BodyJSON && !userSetCT {
				fmt.Fprintf(&b, " -H %s", shellQuote("Content-Type: application/json"))
			}
			fmt.Fprintf(&b, " --data-raw %s", shellQuote(opts.resolve(req.Body.Content)))
		}
	}

	return b.String()
}

// shellQuote wraps s in single quotes, escaping embedded single quotes, so the
// value is safe to paste into a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
