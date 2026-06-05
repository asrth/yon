package yonner

// Blind tests for FromCurl (issue #22): the inverse of ToCurl. Written purely
// from the contract, with no knowledge of the implementation. The tests pin the
// documented parsing behaviour: leading "curl" stripping, line continuations and
// quoting, method inference, header/body/auth handling, the Content-Type ->
// Body.Type mapping (with the JSON/XML Content-Type header dropped), -G query
// params, template literals, tolerated noise flags, errors, and a ToCurl round
// trip. They are deterministic and -race clean.

import (
	"reflect"
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// findHeader returns the value of the first header whose key matches (case
// insensitively) name, and whether it was present.
func findHeader(headers []model.Param, name string) (string, bool) {
	for _, h := range headers {
		if equalFoldBT(h.Key, name) {
			return h.Value, true
		}
	}
	return "", false
}

// equalFoldBT is a tiny local case-insensitive compare so the test file uses no
// production helpers beyond the contract symbols (FromCurl/ToCurl/model.*).
func equalFoldBT(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// A. Basics: method inference + URL.
// ---------------------------------------------------------------------------

func TestBT_FromCurl_BasicsMethodAndURL(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantMethod model.Method
		wantURL    string
		wantBody   model.BodyType
	}{
		{"plain GET", `curl https://api/x`, model.MethodGet, "https://api/x", model.BodyNone},
		{"explicit POST", `curl -X POST https://api/x`, model.MethodPost, "https://api/x", model.BodyNone},
		{"long request flag", `curl --request PUT https://api/x`, model.MethodPut, "https://api/x", model.BodyNone},
		{"head flag", `curl -I https://api/x`, model.MethodHead, "https://api/x", model.BodyNone},
		{"url flag", `curl --url https://api/x`, model.MethodGet, "https://api/x", model.BodyNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := FromCurl(c.in)
			if err != nil {
				t.Fatalf("FromCurl(%q) error: %v", c.in, err)
			}
			if got.Method != c.wantMethod {
				t.Errorf("Method = %q, want %q", got.Method, c.wantMethod)
			}
			if got.URL != c.wantURL {
				t.Errorf("URL = %q, want %q", got.URL, c.wantURL)
			}
			if got.Body.Type != c.wantBody {
				t.Errorf("Body.Type = %q, want %q", got.Body.Type, c.wantBody)
			}
			if c.wantBody == model.BodyNone && got.Body.Content != "" {
				t.Errorf("Body.Content = %q, want empty", got.Body.Content)
			}
			if got.Auth.Kind != model.AuthNone {
				t.Errorf("Auth.Kind = %q, want %q", got.Auth.Kind, model.AuthNone)
			}
			if got.FolderID != "" {
				t.Errorf("FolderID = %q, want empty", got.FolderID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// B. Headers: order preserved, Enabled true, plain headers stay headers.
// ---------------------------------------------------------------------------

func TestBT_FromCurl_HeadersPreservedInOrder(t *testing.T) {
	in := `curl https://api/x -H 'X-One: a' -H 'X-Two: b' -H 'Accept: application/json'`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	want := []model.Param{
		{Key: "X-One", Value: "a", Enabled: true},
		{Key: "X-Two", Value: "b", Enabled: true},
		{Key: "Accept", Value: "application/json", Enabled: true},
	}
	if !reflect.DeepEqual(got.Headers, want) {
		t.Fatalf("Headers = %#v, want %#v", got.Headers, want)
	}
}

// ---------------------------------------------------------------------------
// C. Body + Content-Type mapping.
// ---------------------------------------------------------------------------

func TestBT_FromCurl_BodyContentTypeMapping(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantType    model.BodyType
		wantContent string
		// ctHeaderWant: nil means Content-Type must be ABSENT; non-nil means it
		// must be present with this value.
		ctHeaderWant *string
	}{
		{
			name:         "json CT dropped",
			in:           `curl https://api/x -H 'Content-Type: application/json' -d '{"a":1}'`,
			wantType:     model.BodyJSON,
			wantContent:  `{"a":1}`,
			ctHeaderWant: nil,
		},
		{
			name:         "application/xml dropped -> xml",
			in:           `curl https://api/x -H 'Content-Type: application/xml' -d '<a/>'`,
			wantType:     model.BodyXML,
			wantContent:  `<a/>`,
			ctHeaderWant: nil,
		},
		{
			name:         "text/xml dropped -> xml",
			in:           `curl https://api/x -H 'Content-Type: text/xml' -d '<a/>'`,
			wantType:     model.BodyXML,
			wantContent:  `<a/>`,
			ctHeaderWant: nil,
		},
		{
			name:         "text/plain kept -> text",
			in:           `curl https://api/x -H 'Content-Type: text/plain' -d 'hello'`,
			wantType:     model.BodyText,
			wantContent:  `hello`,
			ctHeaderWant: strptr("text/plain"),
		},
		{
			name:         "no CT -> text",
			in:           `curl https://api/x -d x`,
			wantType:     model.BodyText,
			wantContent:  `x`,
			ctHeaderWant: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := FromCurl(c.in)
			if err != nil {
				t.Fatalf("FromCurl error: %v", err)
			}
			// Body present with no -X must be POST.
			if got.Method != model.MethodPost {
				t.Errorf("Method = %q, want POST", got.Method)
			}
			if got.Body.Type != c.wantType {
				t.Errorf("Body.Type = %q, want %q", got.Body.Type, c.wantType)
			}
			if got.Body.Content != c.wantContent {
				t.Errorf("Body.Content = %q, want %q", got.Body.Content, c.wantContent)
			}
			v, present := findHeader(got.Headers, "Content-Type")
			if c.ctHeaderWant == nil {
				if present {
					t.Errorf("Content-Type header should be dropped, got %q", v)
				}
			} else {
				if !present {
					t.Errorf("Content-Type header missing, want %q", *c.ctHeaderWant)
				} else if v != *c.ctHeaderWant {
					t.Errorf("Content-Type = %q, want %q", v, *c.ctHeaderWant)
				}
			}
		})
	}
}

func strptr(s string) *string { return &s }

func TestBT_FromCurl_MultipleDataJoinedWithAmp(t *testing.T) {
	in := `curl https://api/x -d a=1 -d b=2`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	if got.Method != model.MethodPost {
		t.Errorf("Method = %q, want POST", got.Method)
	}
	if got.Body.Content != "a=1&b=2" {
		t.Errorf("Body.Content = %q, want %q", got.Body.Content, "a=1&b=2")
	}
	if got.Body.Type != model.BodyText {
		t.Errorf("Body.Type = %q, want text", got.Body.Type)
	}
}

// ---------------------------------------------------------------------------
// D. Auth.
// ---------------------------------------------------------------------------

func TestBT_FromCurl_BasicAuth(t *testing.T) {
	in := `curl https://api/x -u alice:secret`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	want := model.Auth{Kind: model.AuthBasic, Username: "alice", Password: "secret"}
	if !reflect.DeepEqual(got.Auth, want) {
		t.Fatalf("Auth = %#v, want %#v", got.Auth, want)
	}
}

func TestBT_FromCurl_BearerAuthHeaderDropped(t *testing.T) {
	in := `curl https://api/x -H 'Authorization: Bearer tok'`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	want := model.Auth{Kind: model.AuthBearer, Token: "tok"}
	if !reflect.DeepEqual(got.Auth, want) {
		t.Errorf("Auth = %#v, want %#v", got.Auth, want)
	}
	if _, present := findHeader(got.Headers, "Authorization"); present {
		t.Errorf("Authorization header should be dropped for Bearer auth, headers=%#v", got.Headers)
	}
}

func TestBT_FromCurl_NonBearerAuthorizationStaysHeader(t *testing.T) {
	in := `curl https://api/x -H 'Authorization: Token abc'`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	if got.Auth.Kind != model.AuthNone {
		t.Errorf("Auth.Kind = %q, want none", got.Auth.Kind)
	}
	v, present := findHeader(got.Headers, "Authorization")
	if !present || v != "Token abc" {
		t.Errorf("Authorization header = %q present=%v, want %q kept", v, present, "Token abc")
	}
}

// ---------------------------------------------------------------------------
// E. -G turns data into query params, not a body.
// ---------------------------------------------------------------------------

func TestBT_FromCurl_GetUploadsDataAsParams(t *testing.T) {
	in := `curl -G https://api/x -d q=1 -d r=2`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	if got.Method != model.MethodGet {
		t.Errorf("Method = %q, want GET", got.Method)
	}
	if got.Body.Type != model.BodyNone || got.Body.Content != "" {
		t.Errorf("Body = %#v, want none/empty", got.Body)
	}
	want := []model.Param{
		{Key: "q", Value: "1", Enabled: true},
		{Key: "r", Value: "2", Enabled: true},
	}
	if !reflect.DeepEqual(got.Params, want) {
		t.Fatalf("Params = %#v, want %#v", got.Params, want)
	}
}

// ---------------------------------------------------------------------------
// F. Quoting + line continuation.
// ---------------------------------------------------------------------------

func TestBT_FromCurl_ContinuationMatchesOneLine(t *testing.T) {
	oneLine := `curl -X POST https://api/x -H 'Content-Type: application/json' -d '{"a":1}'`
	multiLine := "curl -X POST https://api/x \\\n  -H 'Content-Type: application/json' \\\n  -d '{\"a\":1}'"

	gotOne, err := FromCurl(oneLine)
	if err != nil {
		t.Fatalf("FromCurl(oneLine) error: %v", err)
	}
	gotMulti, err := FromCurl(multiLine)
	if err != nil {
		t.Fatalf("FromCurl(multiLine) error: %v", err)
	}
	if !reflect.DeepEqual(gotOne, gotMulti) {
		t.Fatalf("continuation parse differs:\n one-line: %#v\n multi:    %#v", gotOne, gotMulti)
	}
}

func TestBT_FromCurl_DoubleQuotedSpaceStaysOneValue(t *testing.T) {
	in := `curl https://api/x -H "X-Note: hello world"`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	v, present := findHeader(got.Headers, "X-Note")
	if !present || v != "hello world" {
		t.Fatalf("X-Note = %q present=%v, want %q", v, present, "hello world")
	}
	if len(got.Headers) != 1 {
		t.Fatalf("Headers = %#v, want exactly one", got.Headers)
	}
}

// ---------------------------------------------------------------------------
// G. Tolerated noise flags don't error and don't leak into the request.
// ---------------------------------------------------------------------------

func TestBT_FromCurl_ToleratesNoiseFlags(t *testing.T) {
	in := `curl -L -k --compressed -s -v -i https://api/x`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	if got.Method != model.MethodGet {
		t.Errorf("Method = %q, want GET", got.Method)
	}
	if got.URL != "https://api/x" {
		t.Errorf("URL = %q, want https://api/x", got.URL)
	}
	if len(got.Headers) != 0 {
		t.Errorf("Headers = %#v, want none", got.Headers)
	}
	if len(got.Params) != 0 {
		t.Errorf("Params = %#v, want none", got.Params)
	}
	if got.Body.Type != model.BodyNone {
		t.Errorf("Body.Type = %q, want none", got.Body.Type)
	}
}

func TestBT_FromCurl_ToleratesNoiseFlagsWithArgs(t *testing.T) {
	in := `curl -o out.json --max-time 30 https://api/x`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	if got.URL != "https://api/x" {
		t.Errorf("URL = %q, want https://api/x (flag args must not be swallowed as URL)", got.URL)
	}
	if got.Method != model.MethodGet {
		t.Errorf("Method = %q, want GET", got.Method)
	}
}

// ---------------------------------------------------------------------------
// H. Templates kept literal.
// ---------------------------------------------------------------------------

func TestBT_FromCurl_TemplatesKeptLiteral(t *testing.T) {
	in := `curl 'https://{{base}}/users?id={{id}}'`
	got, err := FromCurl(in)
	if err != nil {
		t.Fatalf("FromCurl error: %v", err)
	}
	if got.URL != "https://{{base}}/users?id={{id}}" {
		t.Fatalf("URL = %q, want templates kept literal", got.URL)
	}
}

// ---------------------------------------------------------------------------
// I. Errors (not panics).
// ---------------------------------------------------------------------------

func TestBT_FromCurl_Errors(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ``},
		{"whitespace only", `   `},
		{"no url", `curl -X POST`},
		{"unterminated single quote", `curl 'https://api/x`},
		{"unterminated double quote", `curl "https://api/x`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := FromCurl(c.in)
			if err == nil {
				t.Fatalf("FromCurl(%q) = nil error, want error", c.in)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// J. Round trip: FromCurl(ToCurl(req)) reproduces the salient request shape.
// ---------------------------------------------------------------------------

func TestBT_FromCurl_RoundTripToCurl(t *testing.T) {
	coll := model.Collection{}
	opts := Options{} // nil resolver => templates stay literal

	cases := []struct {
		name string
		req  model.Request
	}{
		{
			name: "plain GET",
			req: model.Request{
				Method: model.MethodGet,
				URL:    "https://api.example.com/users",
				Auth:   model.Auth{Kind: model.AuthNone},
				Body:   model.Body{Type: model.BodyNone},
			},
		},
		{
			name: "POST + JSON body",
			req: model.Request{
				Method:  model.MethodPost,
				URL:     "https://api.example.com/users",
				Headers: []model.Param{{Key: "X-Demo", Value: "yon", Enabled: true}},
				Auth:    model.Auth{Kind: model.AuthNone},
				Body:    model.Body{Type: model.BodyJSON, Content: `{"a":1}`},
			},
		},
		{
			name: "Bearer-auth GET",
			req: model.Request{
				Method: model.MethodGet,
				URL:    "https://api.example.com/me",
				Auth:   model.Auth{Kind: model.AuthBearer, Token: "tok"},
				Body:   model.Body{Type: model.BodyNone},
			},
		},
		{
			name: "Basic-auth POST + text body",
			req: model.Request{
				Method: model.MethodPost,
				URL:    "https://api.example.com/login",
				Auth:   model.Auth{Kind: model.AuthBasic, Username: "alice", Password: "secret"},
				Body:   model.Body{Type: model.BodyText, Content: "raw payload"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			curl := ToCurl(c.req, coll, opts)
			got, err := FromCurl(curl)
			if err != nil {
				t.Fatalf("FromCurl(ToCurl(...)) error: %v\ncurl: %s", err, curl)
			}

			if got.Method != c.req.Method {
				t.Errorf("Method = %q, want %q\ncurl: %s", got.Method, c.req.Method, curl)
			}
			if got.URL != c.req.URL {
				t.Errorf("URL = %q, want %q\ncurl: %s", got.URL, c.req.URL, curl)
			}
			if got.Body.Type != c.req.Body.Type {
				t.Errorf("Body.Type = %q, want %q\ncurl: %s", got.Body.Type, c.req.Body.Type, curl)
			}
			if got.Body.Content != c.req.Body.Content {
				t.Errorf("Body.Content = %q, want %q\ncurl: %s", got.Body.Content, c.req.Body.Content, curl)
			}
			if !reflect.DeepEqual(got.Auth, c.req.Auth) {
				t.Errorf("Auth = %#v, want %#v\ncurl: %s", got.Auth, c.req.Auth, curl)
			}
			// User-set headers (those not consumed by auth/CT mapping) must survive.
			for _, h := range c.req.Headers {
				v, present := findHeader(got.Headers, h.Key)
				if !present || v != h.Value {
					t.Errorf("header %q = %q present=%v, want %q\ncurl: %s", h.Key, v, present, h.Value, curl)
				}
			}
		})
	}
}
