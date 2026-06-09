package yonner

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/ultramcu/yon/internal/model"
)

// FromCurl parses a `curl` command line into a model.Request. It is the inverse
// of ToCurl: parsing the output of ToCurl(req, …) reproduces req's meaningful
// fields (method, URL, headers, body, auth), so a user can paste a curl command
// and edit it as a normal request.
//
// The parser is deliberately forgiving — it recognises the flags curl users
// most commonly paste (method, headers, data, basic/bearer auth, user-agent /
// referer / cookie shorthands), tolerates and skips connection/output flags it
// does not model, and never panics on malformed input. It does NOT touch the
// filesystem: a `@file` data reference is kept as literal text rather than read.
//
// It returns a descriptive error for empty/whitespace-only input, input with no
// URL, or an unterminated quote.
func FromCurl(text string) (model.Request, error) {
	tokens, err := tokenizeShell(text)
	if err != nil {
		return model.Request{}, err
	}
	// Strip a leading "curl" token if present (callers commonly paste the whole
	// command); a bare argument list without it is accepted too.
	if len(tokens) > 0 && tokens[0] == "curl" {
		tokens = tokens[1:]
	}
	if len(tokens) == 0 {
		return model.Request{}, fmt.Errorf("from curl: empty command")
	}

	var (
		req         model.Request
		urlSet      bool // first positional/--url wins
		methodSet   bool // an explicit -X/-I pins the method
		dataParts   []string
		formParts   []model.FormField // -F/--form: multipart parts (wins over -d)
		dataAsQuery bool              // -G: send accumulated data as query params, not a body
		userAuth    bool              // -u seen (Basic wins over an Authorization header)
	)

	// next consumes the value for a flag that takes a separate argument. inline
	// holds the value already attached via --flag=value (ok true) so we don't
	// pull a following token in that case.
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]

		// A flag is a token that starts with '-' and is not a lone '-'.
		if !strings.HasPrefix(tok, "-") || tok == "-" {
			// Positional argument → the URL (first one wins).
			if !urlSet {
				req.URL = tok
				urlSet = true
			}
			continue
		}

		// Split a flag's inline value. For a long flag it is --flag=value; for a
		// short value-taking flag curl also accepts the value glued directly to
		// the letter (e.g. -HX-Y:z, -ualice:pw, -XPOST), so when a known
		// single-letter short flag has trailing characters, treat them as the
		// inline value.
		name, inlineVal, hasInline := tok, "", false
		if strings.HasPrefix(tok, "--") {
			if eq := strings.IndexByte(tok, '='); eq >= 0 {
				name, inlineVal, hasInline = tok[:eq], tok[eq+1:], true
			}
		} else if len(tok) > 2 && shortFlagTakesValue(tok[1]) {
			name, inlineVal, hasInline = tok[:2], tok[2:], true
		}

		// value returns the flag's argument: the inline part for --flag=value,
		// otherwise the next token (consumed). ok is false when neither exists.
		value := func() (string, bool) {
			if hasInline {
				return inlineVal, true
			}
			if i+1 < len(tokens) {
				i++
				return tokens[i], true
			}
			return "", false
		}

		switch name {
		case "-X", "--request":
			if v, ok := value(); ok {
				req.Method = model.Method(strings.ToUpper(strings.TrimSpace(v)))
				methodSet = true
			}
		case "-H", "--header":
			if v, ok := value(); ok {
				if k, val, ok := splitHeader(v); ok {
					req.Headers = append(req.Headers, model.Param{Key: k, Value: val, Enabled: true})
				}
			}
		case "-d", "--data", "--data-raw", "--data-ascii", "--data-binary":
			if v, ok := value(); ok {
				dataParts = append(dataParts, v)
			}
		case "--data-urlencode":
			if v, ok := value(); ok {
				dataParts = append(dataParts, urlEncodeData(v))
			}
		case "-F", "--form":
			if v, ok := value(); ok {
				formParts = append(formParts, parseFormField(v))
			}
		case "-G", "--get":
			dataAsQuery = true
		case "-u", "--user":
			if v, ok := value(); ok {
				user, pass, _ := strings.Cut(v, ":")
				req.Auth = model.Auth{Kind: model.AuthBasic, Username: user, Password: pass}
				userAuth = true
			}
		case "--url":
			if v, ok := value(); ok && !urlSet {
				req.URL = v
				urlSet = true
			}
		case "-A", "--user-agent":
			if v, ok := value(); ok {
				req.Headers = append(req.Headers, model.Param{Key: "User-Agent", Value: v, Enabled: true})
			}
		case "-e", "--referer":
			if v, ok := value(); ok {
				req.Headers = append(req.Headers, model.Param{Key: "Referer", Value: v, Enabled: true})
			}
		case "-b", "--cookie":
			if v, ok := value(); ok {
				req.Headers = append(req.Headers, model.Param{Key: "Cookie", Value: v, Enabled: true})
			}
		case "-I", "--head":
			req.Method = model.MethodHead
			methodSet = true

		// Tolerated value-less flags: recognised so they're not mistaken for a
		// URL, but otherwise ignored.
		case "-L", "--location", "-k", "--insecure", "--compressed",
			"-s", "--silent", "-i", "--include", "-v", "--verbose",
			"-#", "--progress-bar", "-O", "--remote-name":
			// no-op

		// Tolerated flags that take (and here discard) a value.
		case "-o", "--output", "--max-time", "--connect-timeout", "--retry":
			value() // consume the argument if present

		default:
			// Unknown flag. For an unknown short flag (or bundle like -sL),
			// treat each letter so a bundled known no-op flag (e.g. -I, -G) is
			// still honoured; for an unknown long flag, skip it (a separate
			// following value is left to be (mis)read as a URL only if the user
			// really passed one — we don't guess).
			if strings.HasPrefix(name, "--") {
				// --flag=value already absorbed its value; --flag with a
				// separate value: we leave the next token alone (it may be the
				// URL). Nothing to do.
				continue
			}
			// Short bundle: handle each letter we know.
			for _, c := range name[1:] {
				switch c {
				case 'I':
					req.Method = model.MethodHead
					methodSet = true
				case 'G':
					dataAsQuery = true
				}
				// All other letters are tolerated/ignored.
			}
		}
	}

	if strings.TrimSpace(req.URL) == "" {
		return model.Request{}, fmt.Errorf("from curl: no URL found")
	}

	data := strings.Join(dataParts, "&")

	// -G: fold the accumulated data into the URL's query as enabled Params and
	// send no body.
	if dataAsQuery {
		if data != "" {
			req.Params = append(req.Params, dataToParams(data)...)
		}
		data = ""
	}

	// -F/--form takes precedence: a multipart body is built from the collected
	// parts and any -d text is ignored (mixing -F and -d is unusual; curl itself
	// rejects it). hasBody tracks whether *any* body is present for the
	// Content-Type/method reconciliation below.
	hasBody := data != ""
	if len(formParts) > 0 {
		req.Body = model.Body{Type: model.BodyMultipart, Fields: formParts}
		hasBody = true
	} else {
		applyBody(&req, data, methodSet)
	}
	applyContentTypeAndAuth(&req, hasBody, userAuth)

	// Default method. curl uses GET, unless a body is present without an explicit
	// method, in which case it POSTs.
	if req.Method == "" {
		if hasBody {
			req.Method = model.MethodPost
		} else {
			req.Method = model.MethodGet
		}
	}

	// Auth fallback: nothing matched → explicit AuthNone.
	if req.Auth.Kind == "" {
		req.Auth = model.Auth{Kind: model.AuthNone}
	}

	return req, nil
}

// applyBody sets req.Body from the raw data string. Empty data → BodyNone.
// Content-Type is reconciled separately (applyContentTypeAndAuth); here the body
// is provisionally BodyText, which that step may promote to JSON/XML. methodSet
// is unused for the body itself but kept for symmetry with curl's POST default,
// which is applied by the caller.
func applyBody(req *model.Request, data string, _ bool) {
	if data == "" {
		req.Body = model.Body{Type: model.BodyNone}
		return
	}
	req.Body = model.Body{Type: model.BodyText, Content: data}
}

// applyContentTypeAndAuth reconciles the parsed headers with the body and auth,
// mirroring how the app re-adds headers when sending (so ToCurl round-trips):
//
//   - Authorization: Bearer X → Auth{bearer, X}, header dropped (unless -u set a
//     Basic auth, which wins; then the header is kept as-is).
//   - With a body present, a Content-Type of application/json → Body.Type json
//     and the header is dropped (the sender re-adds it); application/xml or
//     text/xml → xml, header dropped; any other Content-Type → Body stays text
//     and the header is KEPT.
func applyContentTypeAndAuth(req *model.Request, hasBody, userAuth bool) {
	kept := req.Headers[:0]
	for _, h := range req.Headers {
		switch {
		case strings.EqualFold(h.Key, headerAuthorization):
			// Promote a Bearer token to structured Auth and drop the header,
			// unless -u already set Basic (which wins) — then keep the header.
			if !userAuth {
				if tok, ok := cutBearer(h.Value); ok {
					req.Auth = model.Auth{Kind: model.AuthBearer, Token: tok}
					continue // drop header
				}
			}
			kept = append(kept, h)
		case hasBody && strings.EqualFold(h.Key, headerContentType):
			switch ct := mediaType(h.Value); {
			case strings.EqualFold(ct, "application/json"):
				req.Body.Type = model.BodyJSON
				continue // drop header; app re-adds it
			case strings.EqualFold(ct, "application/xml"), strings.EqualFold(ct, "text/xml"):
				req.Body.Type = model.BodyXML
				continue // drop header; app re-adds it
			default:
				// Some other Content-Type: keep the header and leave the body as
				// text.
				kept = append(kept, h)
			}
		default:
			kept = append(kept, h)
		}
	}
	if len(kept) == 0 {
		req.Headers = nil
	} else {
		req.Headers = kept
	}
}

// shortFlagTakesValue reports whether the single-letter curl flag c expects an
// argument, so that characters glued directly after it (e.g. -HX-Y:z) are read
// as that argument rather than as a bundle of further short flags.
func shortFlagTakesValue(c byte) bool {
	switch c {
	case 'X', 'H', 'd', 'F', 'u', 'A', 'e', 'b', 'o':
		return true
	}
	return false
}

// splitHeader splits a curl -H value "Key: Value" into key and value. A
// "Key;" form (curl's syntax for an explicitly empty-valued header) yields an
// empty value. ok is false only when there is no usable key.
func splitHeader(s string) (key, value string, ok bool) {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		key = strings.TrimSpace(s[:i])
		value = strings.TrimSpace(s[i+1:])
		return key, value, key != ""
	}
	// "Key;" — header with an explicitly empty value.
	if i := strings.IndexByte(s, ';'); i >= 0 {
		key = strings.TrimSpace(s[:i])
		return key, "", key != ""
	}
	key = strings.TrimSpace(s)
	return key, "", key != ""
}

// cutBearer returns the token of an "Authorization: Bearer X" value (the part
// after a case-insensitive "Bearer " prefix), ok=false for any other scheme.
func cutBearer(v string) (string, bool) {
	v = strings.TrimSpace(v)
	const scheme = "bearer "
	if len(v) >= len(scheme) && strings.EqualFold(v[:len(scheme)], scheme) {
		return strings.TrimSpace(v[len(scheme):]), true
	}
	return "", false
}

// mediaType returns the media type of a Content-Type value, stripping any
// "; charset=…" parameters and surrounding whitespace.
func mediaType(v string) string {
	if i := strings.IndexByte(v, ';'); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// urlEncodeData applies curl's --data-urlencode rules to one data fragment:
//   - "name=content"  → "name=" + encode(content)   (encode after the first '=')
//   - "=content"      → encode(content)              (leading '=' dropped)
//   - "content"       → encode(content)              (whole string)
//
// A "@file" reference is not read from disk; the literal is encoded as content
// like any other text, since we have no file to load.
func urlEncodeData(s string) string {
	if i := strings.IndexByte(s, '='); i >= 0 {
		if i == 0 {
			return url.QueryEscape(s[1:])
		}
		return s[:i+1] + url.QueryEscape(s[i+1:])
	}
	return url.QueryEscape(s)
}

// parseFormField parses one curl -F/--form value into a multipart FormField.
// The value is split on the FIRST '='; the left side is the key. A right side
// beginning with '@' is a file part (IsFile=true) whose Value is the path after
// the '@', with a trailing ";filename=NAME" stripped off into Filename; any
// other right side is a text part whose Value is taken literally. A value with
// no '=' becomes a text field with an empty value. Imported fields are enabled.
func parseFormField(s string) model.FormField {
	key, rhs, _ := strings.Cut(s, "=")
	f := model.FormField{Key: key, Enabled: true}
	if strings.HasPrefix(rhs, "@") {
		f.IsFile = true
		path := rhs[1:]
		if i := strings.Index(path, ";filename="); i >= 0 {
			f.Filename = path[i+len(";filename="):]
			path = path[:i]
		}
		f.Value = path
	} else {
		f.Value = rhs
	}
	return f
}

// dataToParams turns a urlencoded "a=1&b=2" data string into enabled query
// Params. Each pair is split on the first '=' and percent-decoded; a bare key
// with no '=' becomes a Param with an empty value.
func dataToParams(data string) []model.Param {
	var out []model.Param
	for _, pair := range strings.Split(data, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		if dk, err := url.QueryUnescape(k); err == nil {
			k = dk
		}
		if dv, err := url.QueryUnescape(v); err == nil {
			v = dv
		}
		out = append(out, model.Param{Key: k, Value: v, Enabled: true})
	}
	return out
}

// tokenizeShell splits a shell command line into argument tokens, implementing a
// small POSIX-ish tokenizer (we never shell out). It supports:
//
//   - backslash-newline line continuations (the pair is removed, joining lines);
//   - single quotes '…' (everything literal, no escapes);
//   - double quotes "…" (literal, but \" \\ \$ and \` are unescaped);
//   - ANSI-C quotes $'…' (\n \t \r \\ \' \" are unescaped);
//   - unquoted runs split on unescaped whitespace, honouring a backslash before
//     any character (including a literal escaped space);
//   - adjacent quoted/unquoted runs concatenate into a single token.
//
// It returns an error on an unterminated quote.
func tokenizeShell(s string) ([]string, error) {
	var (
		tokens  []string
		cur     strings.Builder
		started bool // whether the current token has any content (even "")
	)
	flush := func() {
		if started {
			tokens = append(tokens, cur.String())
			cur.Reset()
			started = false
		}
	}

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case c == '\\':
			// Line continuation: backslash immediately before a newline joins
			// the lines (both characters vanish).
			if i+1 < len(runes) && runes[i+1] == '\n' {
				i++
				continue
			}
			// Otherwise escape the next character literally.
			if i+1 < len(runes) {
				cur.WriteRune(runes[i+1])
				started = true
				i++
			}
		case c == '\'':
			started = true
			j := i + 1
			for j < len(runes) && runes[j] != '\'' {
				cur.WriteRune(runes[j])
				j++
			}
			if j >= len(runes) {
				return nil, fmt.Errorf("from curl: unterminated single quote")
			}
			i = j
		case c == '"':
			started = true
			j := i + 1
			for j < len(runes) && runes[j] != '"' {
				if runes[j] == '\\' && j+1 < len(runes) {
					switch n := runes[j+1]; n {
					case '"', '\\', '$', '`':
						cur.WriteRune(n)
						j += 2
						continue
					}
				}
				cur.WriteRune(runes[j])
				j++
			}
			if j >= len(runes) {
				return nil, fmt.Errorf("from curl: unterminated double quote")
			}
			i = j
		case c == '$' && i+1 < len(runes) && runes[i+1] == '\'':
			// ANSI-C quoting: $'…' with C-style escapes.
			started = true
			j := i + 2
			for j < len(runes) && runes[j] != '\'' {
				if runes[j] == '\\' && j+1 < len(runes) {
					n := runes[j+1]
					decoded, ok := ansiCEscape(n)
					if ok {
						cur.WriteRune(decoded)
						j += 2
						continue
					}
				}
				cur.WriteRune(runes[j])
				j++
			}
			if j >= len(runes) {
				return nil, fmt.Errorf("from curl: unterminated $'…' quote")
			}
			i = j
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		default:
			cur.WriteRune(c)
			started = true
		}
	}
	flush()
	return tokens, nil
}

// ansiCEscape decodes the backslash escapes honoured inside an ANSI-C $'…'
// quote. ok is false for an unrecognised escape (the backslash is then kept
// literally by the caller).
func ansiCEscape(n rune) (rune, bool) {
	switch n {
	case 'n':
		return '\n', true
	case 't':
		return '\t', true
	case 'r':
		return '\r', true
	case '\\':
		return '\\', true
	case '\'':
		return '\'', true
	case '"':
		return '"', true
	}
	return 0, false
}
