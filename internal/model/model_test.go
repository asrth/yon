package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// ---------------------------------------------------------------------------
// ResolveAuth
//
// Spec (the documented design): ResolveAuth returns the Auth actually applied.
//   - req Kind "inherit"            -> the Collection's Auth
//   - req Kind "none"/"basic"/"bearer" -> the Request's own Auth
//   - an explicit Request "none" overrides a Collection that has auth
// ---------------------------------------------------------------------------

func TestResolveAuth(t *testing.T) {
	collBasic := model.Auth{Kind: model.AuthBasic, Username: "cu", Password: "cp"}
	collBearer := model.Auth{Kind: model.AuthBearer, Token: "ctok"}
	collNone := model.Auth{Kind: model.AuthNone}

	reqBasic := model.Auth{Kind: model.AuthBasic, Username: "ru", Password: "rp"}
	reqBearer := model.Auth{Kind: model.AuthBearer, Token: "rtok"}
	reqNone := model.Auth{Kind: model.AuthNone}
	reqInherit := model.Auth{Kind: model.AuthInherit}

	tests := []struct {
		name     string
		reqAuth  model.Auth
		collAuth model.Auth
		want     model.Auth
	}{
		{
			name:     "inherit takes collection basic",
			reqAuth:  reqInherit,
			collAuth: collBasic,
			want:     collBasic,
		},
		{
			name:     "inherit takes collection bearer",
			reqAuth:  reqInherit,
			collAuth: collBearer,
			want:     collBearer,
		},
		{
			name:     "inherit takes collection none",
			reqAuth:  reqInherit,
			collAuth: collNone,
			want:     collNone,
		},
		{
			name:     "request none overrides collection basic (deliberate no-auth)",
			reqAuth:  reqNone,
			collAuth: collBasic,
			want:     reqNone,
		},
		{
			name:     "request basic overrides collection bearer",
			reqAuth:  reqBasic,
			collAuth: collBearer,
			want:     reqBasic,
		},
		{
			name:     "request bearer overrides collection basic",
			reqAuth:  reqBearer,
			collAuth: collBasic,
			want:     reqBearer,
		},
		{
			name:     "request basic used when collection is none",
			reqAuth:  reqBasic,
			collAuth: collNone,
			want:     reqBasic,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := model.Request{Method: model.MethodGet, URL: "https://example.com/x", Auth: tc.reqAuth}
			coll := model.Collection{Version: 1, Name: "C", Auth: tc.collAuth}

			got := model.ResolveAuth(req, coll)
			if got != tc.want {
				t.Fatalf("ResolveAuth = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Guard: the resolved auth for "inherit" must be the *collection* credentials,
// not the request's (the request carries no creds when it inherits). This makes
// the inherit case meaningful beyond just Kind equality.
func TestResolveAuthInheritUsesCollectionCredentials(t *testing.T) {
	coll := model.Collection{
		Version: 1,
		Auth:    model.Auth{Kind: model.AuthBasic, Username: "collUser", Password: "collPass"},
	}
	req := model.Request{
		Method: model.MethodGet,
		URL:    "https://example.com",
		// inherit + stray creds that must be ignored in favour of the collection
		Auth: model.Auth{Kind: model.AuthInherit, Username: "ignored", Password: "ignored"},
	}

	got := model.ResolveAuth(req, coll)
	if got.Username != "collUser" || got.Password != "collPass" || got.Kind != model.AuthBasic {
		t.Fatalf("inherit must resolve to collection creds, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// Request.DisplayName
//
// Spec: returns Name when set; when empty, a derived "METHOD /path" from the
// URL; a sensible fallback when the URL has no path / is unparseable.
// ---------------------------------------------------------------------------

func TestDisplayName_NameSet(t *testing.T) {
	r := model.Request{Name: "Login", Method: model.MethodPost, URL: "https://api.test/users/1"}
	if got := r.DisplayName(); got != "Login" {
		t.Fatalf("DisplayName with Name set = %q, want %q", got, "Login")
	}
}

func TestDisplayName_NameSetWinsOverURL(t *testing.T) {
	// Even if a derivable URL is present, an explicit Name must win verbatim.
	r := model.Request{Name: "  My Request  ", Method: model.MethodGet, URL: "https://api.test/x"}
	if got := r.DisplayName(); got != "  My Request  " {
		t.Fatalf("DisplayName must return Name verbatim, got %q", got)
	}
}

func TestDisplayName_DerivedFromURL(t *testing.T) {
	r := model.Request{Name: "", Method: model.MethodGet, URL: "https://api.example.com/users/42"}
	got := r.DisplayName()

	// Spec form is "METHOD /path": must include the method verb and the path.
	if !strings.Contains(got, string(model.MethodGet)) {
		t.Fatalf("derived DisplayName %q should contain method %q", got, model.MethodGet)
	}
	if !strings.Contains(got, "/users/42") {
		t.Fatalf("derived DisplayName %q should contain path %q", got, "/users/42")
	}
	// The host should not appear in a "METHOD /path" form.
	if strings.Contains(got, "api.example.com") {
		t.Fatalf("derived DisplayName %q should not include the host", got)
	}
}

func TestDisplayName_DerivedUsesRequestMethod(t *testing.T) {
	// The derived label must reflect *this* request's method, not a fixed verb.
	r := model.Request{Name: "", Method: model.MethodDelete, URL: "https://api.example.com/things/7"}
	got := r.DisplayName()
	if !strings.Contains(got, string(model.MethodDelete)) {
		t.Fatalf("derived DisplayName %q should contain method %q", got, model.MethodDelete)
	}
}

func TestDisplayName_EmptyURLFallback(t *testing.T) {
	// Name empty and URL empty: spec only promises a "sensible fallback".
	// It must be non-empty and stable, and should reflect the method.
	r := model.Request{Name: "", Method: model.MethodPut, URL: ""}
	got := r.DisplayName()
	if got == "" {
		t.Fatalf("DisplayName with empty Name and empty URL must not be empty")
	}
	if !strings.Contains(got, string(model.MethodPut)) {
		t.Fatalf("fallback DisplayName %q should still mention method %q", got, model.MethodPut)
	}
	// It must not echo the empty Name as the whole label.
	if got == "" {
		t.Fatalf("fallback DisplayName must be a derived label, not the empty Name")
	}
}

// ---------------------------------------------------------------------------
// JSON round-trip + on-disk key shape (the .yon schema)
// ---------------------------------------------------------------------------

func sampleCollection() model.Collection {
	return model.Collection{
		Version: 1,
		Name:    "My API",
		Auth:    model.Auth{Kind: model.AuthBasic, Username: "admin", Password: "s3cr3t"},
		Requests: []model.Request{
			{
				Name:   "List users",
				Method: model.MethodGet,
				URL:    "https://api.example.com/users",
				Params: []model.Param{
					{Key: "page", Value: "1", Enabled: true},
					{Key: "limit", Value: "20", Enabled: false},
				},
				Headers: []model.Param{
					{Key: "Accept", Value: "application/json", Enabled: true},
				},
				Auth: model.Auth{Kind: model.AuthInherit},
				Body: model.Body{Type: model.BodyNone},
			},
			{
				Name:    "Create user",
				Method:  model.MethodPost,
				URL:     "https://api.example.com/users",
				Headers: []model.Param{{Key: "X-Trace", Value: "abc", Enabled: true}},
				Auth:    model.Auth{Kind: model.AuthBearer, Token: "tok-123"},
				Body:    model.Body{Type: model.BodyJSON, Content: `{"name":"neo"}`},
			},
			{
				Name:   "Public ping",
				Method: model.MethodGet,
				URL:    "https://api.example.com/ping",
				Auth:   model.Auth{Kind: model.AuthNone}, // explicit no-auth override
				Body:   model.Body{Type: model.BodyText, Content: "ping"},
			},
		},
	}
}

func TestJSONRoundTrip(t *testing.T) {
	orig := sampleCollection()

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got model.Collection
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Top-level meaningful fields.
	if got.Version != orig.Version {
		t.Errorf("Version = %d, want %d", got.Version, orig.Version)
	}
	if got.Name != orig.Name {
		t.Errorf("Name = %q, want %q", got.Name, orig.Name)
	}
	if got.Auth != orig.Auth {
		t.Errorf("Collection Auth = %+v, want %+v", got.Auth, orig.Auth)
	}
	if len(got.Requests) != len(orig.Requests) {
		t.Fatalf("Requests len = %d, want %d", len(got.Requests), len(orig.Requests))
	}

	for i := range orig.Requests {
		ow, gw := orig.Requests[i], got.Requests[i]
		if gw.Name != ow.Name {
			t.Errorf("req[%d].Name = %q, want %q", i, gw.Name, ow.Name)
		}
		if gw.Method != ow.Method {
			t.Errorf("req[%d].Method = %q, want %q", i, gw.Method, ow.Method)
		}
		if gw.URL != ow.URL {
			t.Errorf("req[%d].URL = %q, want %q", i, gw.URL, ow.URL)
		}
		if gw.Auth != ow.Auth {
			t.Errorf("req[%d].Auth = %+v, want %+v", i, gw.Auth, ow.Auth)
		}
		if gw.Body.Type != ow.Body.Type || gw.Body.Content != ow.Body.Content {
			t.Errorf("req[%d].Body = %+v, want %+v", i, gw.Body, ow.Body)
		}
		if len(gw.Body.Fields) != len(ow.Body.Fields) {
			t.Errorf("req[%d].Body.Fields len = %d, want %d", i, len(gw.Body.Fields), len(ow.Body.Fields))
		} else {
			for j := range ow.Body.Fields {
				if gw.Body.Fields[j] != ow.Body.Fields[j] {
					t.Errorf("req[%d].Body.Fields[%d] = %+v, want %+v", i, j, gw.Body.Fields[j], ow.Body.Fields[j])
				}
			}
		}
		if len(gw.Params) != len(ow.Params) {
			t.Errorf("req[%d].Params len = %d, want %d", i, len(gw.Params), len(ow.Params))
		} else {
			for j := range ow.Params {
				if gw.Params[j] != ow.Params[j] {
					t.Errorf("req[%d].Params[%d] = %+v, want %+v", i, j, gw.Params[j], ow.Params[j])
				}
			}
		}
		if len(gw.Headers) != len(ow.Headers) {
			t.Errorf("req[%d].Headers len = %d, want %d", i, len(gw.Headers), len(ow.Headers))
		} else {
			for j := range ow.Headers {
				if gw.Headers[j] != ow.Headers[j] {
					t.Errorf("req[%d].Headers[%d] = %+v, want %+v", i, j, gw.Headers[j], ow.Headers[j])
				}
			}
		}
	}
}

// The on-disk schema must use the lowercase keys the spec implies.
func TestJSONKeysAreLowercaseSchema(t *testing.T) {
	data, err := json.Marshal(sampleCollection())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(data)

	for _, key := range []string{
		`"version":`, `"name":`, `"auth":`, `"requests":`,
		`"method":`, `"url":`, `"params":`, `"headers":`, `"body":`,
		`"kind":`, `"type":`, `"key":`, `"value":`, `"enabled":`,
		`"username":`, `"password":`, `"token":`, `"content":`,
	} {
		if !strings.Contains(s, key) {
			t.Errorf("marshaled JSON missing expected key %s\nJSON: %s", key, s)
		}
	}

	// Capitalised Go field names must NOT appear as keys.
	for _, bad := range []string{
		`"Version"`, `"Name"`, `"Method"`, `"URL"`, `"Auth"`,
		`"Body"`, `"Kind"`, `"Type"`, `"Requests"`,
	} {
		if strings.Contains(s, bad) {
			t.Errorf("marshaled JSON must not contain capitalised key %s", bad)
		}
	}

	// The JSON-body request must persist its raw content.
	if !strings.Contains(s, `{\"name\":\"neo\"}`) {
		t.Errorf("JSON body content not found in marshaled output: %s", s)
	}
}

// A none / inherit auth must not leak empty username/password/token noise:
// the omitempty tags should drop them. Marshal a request whose own auth is
// "none" and whose creds are all empty, and assert the JSON has no such keys
// inside that request's auth object.
func TestJSONAuthOmitsEmptyCredentials(t *testing.T) {
	c := model.Collection{
		Version: 1,
		Name:    "C",
		Auth:    model.Auth{Kind: model.AuthNone}, // collection none, no creds
		Requests: []model.Request{
			{
				Name:   "ping",
				Method: model.MethodGet,
				URL:    "https://x/ping",
				Auth:   model.Auth{Kind: model.AuthInherit}, // inherit, no creds
				Body:   model.Body{Type: model.BodyNone},
			},
		},
	}

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(data)

	// With omitempty and all-empty creds, none of these should appear anywhere.
	for _, leak := range []string{`"username":`, `"password":`, `"token":`} {
		if strings.Contains(s, leak) {
			t.Errorf("empty credential leaked into JSON: %s\nJSON: %s", leak, s)
		}
	}

	// The auth Kind itself must still be present (kind has no omitempty).
	if !strings.Contains(s, `"kind":"none"`) {
		t.Errorf("collection auth kind 'none' missing: %s", s)
	}
	if !strings.Contains(s, `"kind":"inherit"`) {
		t.Errorf("request auth kind 'inherit' missing: %s", s)
	}

	// A none Body should likewise omit empty content.
	if strings.Contains(s, `"content":`) {
		t.Errorf("empty body content leaked into JSON: %s", s)
	}
}
