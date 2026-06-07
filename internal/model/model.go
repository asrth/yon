// Package model holds Yon's pure domain types and logic: the data that
// describes a Request, a Collection, and a Response, plus a small amount of
// pure logic (auth inheritance, display-name derivation).
//
// It depends only on the standard library and imports neither Fyne nor any
// networking package (see the UI-free-core rule). The store package serializes these types
// to .yon files with encoding/json, so the JSON struct tags here define the
// on-disk schema and must stay stable.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Method is the HTTP verb of a Request. Yon ships well-known verbs as consts
// but treats Method as a free-form string, so an arbitrary custom verb the user
// types is sent verbatim.
type Method string

// Well-known HTTP methods. Method is a string type, so a Request may also carry
// any other (custom) verb not listed here.
const (
	MethodGet     Method = "GET"
	MethodPost    Method = "POST"
	MethodPut     Method = "PUT"
	MethodDelete  Method = "DELETE"
	MethodPatch   Method = "PATCH"
	MethodHead    Method = "HEAD"
	MethodOptions Method = "OPTIONS"
)

// AuthKind identifies how a Request or Collection authenticates.
//
// A Collection's Auth never uses AuthInherit; a Request may use any kind,
// where AuthInherit means "use the Collection's Auth".
type AuthKind string

// Auth kinds.
const (
	// AuthInherit means a Request defers to its Collection's Auth. It is only
	// valid on a Request, never on a Collection.
	AuthInherit AuthKind = "inherit"
	// AuthNone means no Authorization header is sent. On a Request it is an
	// explicit override that suppresses any inherited Collection Auth.
	AuthNone AuthKind = "none"
	// AuthBasic is HTTP Basic auth using Username and Password.
	AuthBasic AuthKind = "basic"
	// AuthBearer is bearer-token auth using Token.
	AuthBearer AuthKind = "bearer"
)

// Auth describes the authentication for a Request or Collection. Only the
// fields relevant to Kind are meaningful: Username/Password for AuthBasic,
// Token for AuthBearer.
type Auth struct {
	Kind     AuthKind `json:"kind"`
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`
	Token    string   `json:"token,omitempty"`
}

// Param is a key/value pair with an Enabled flag, used for both query
// parameters and headers. A disabled Param is kept (not deleted) but is not
// applied when the Request is sent.
type Param struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

// BodyType identifies the kind of payload a Request carries.
type BodyType string

// Body types.
const (
	// BodyNone means the Request has no body.
	BodyNone BodyType = "none"
	// BodyJSON means the body is JSON; the sender adds an
	// "application/json" Content-Type unless one is already set.
	BodyJSON BodyType = "json"
	// BodyText means the body is raw text with no automatic Content-Type.
	BodyText BodyType = "text"
	// BodyXML means the body is XML; the sender adds an "application/xml"
	// Content-Type unless one is already set.
	BodyXML BodyType = "xml"
)

// Body is the payload of a Request. It is held on every Request regardless of
// Method and is sent as-is when Content is non-empty.
type Body struct {
	Type    BodyType `json:"type"`
	Content string   `json:"content,omitempty"`
}

// Folder is a one-level-deep grouping of Requests within a Collection. Folders
// do NOT nest (no folder ever lives inside another) and they do NOT own their
// requests: a Request points at its folder via Request.FolderID, while
// Collection.Requests stays a single flat, ordered slice. A Folder is therefore
// a display/grouping layer over the flat requests, identified by a stable ID.
type Folder struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Collapsed bool   `json:"collapsed,omitempty"`
}

// NewFolderID returns a short, collision-resistant folder id: the literal "f"
// followed by 8 hex characters (4 crypto-random bytes). Mirrors the uuid helper
// style in internal/variables. On the (vanishingly unlikely) rand failure it
// still returns a well-formed, if non-random, id.
func NewFolderID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "f00000000"
	}
	return "f" + hex.EncodeToString(b[:])
}

// Request is a single HTTP request definition the user composes and sends.
//
// FolderID is the id of the Folder this Request belongs to, or "" for a
// top-level (ungrouped) request. It is grouping metadata only: it never changes
// the Request's position in the flat Collection.Requests slice.
type Request struct {
	Name     string  `json:"name,omitempty"`
	Method   Method  `json:"method"`
	URL      string  `json:"url"`
	Params   []Param `json:"params,omitempty"`  // query parameters
	Headers  []Param `json:"headers,omitempty"` // request headers
	Auth     Auth    `json:"auth"`
	Body     Body    `json:"body"`
	FolderID string  `json:"folderId,omitempty"` // owning Folder id, or "" for top-level
	// Options holds per-request connection overrides (timeout, TLS verification,
	// redirect following). A nil Options — the default — means "inherit the global
	// Settings", and thanks to the pointer + omitempty it serializes with NO
	// "options" key, so a Request created before this field existed stays
	// byte-identical on disk. See RequestOptions for the per-field tri-state.
	Options *RequestOptions `json:"options,omitempty"`
	// Captures extract values from the Response into runtime variables (see
	// variables.Scope.Runtime) for use by later requests. omitempty + nil keeps
	// a Request authored before this field existed byte-identical on disk.
	Captures []Capture `json:"captures,omitempty"`
	// Assertions are post-response checks run against the Response. omitempty +
	// nil keeps a Request authored before this field existed byte-identical on
	// disk.
	Assertions []Assertion `json:"assertions,omitempty"`
}

// CaptureSource identifies where a Capture reads its value from in a Response.
type CaptureSource string

// Capture sources.
const (
	// CaptureJSONBody reads the value at a JSON path (Capture.Expr) inside the
	// response body parsed as JSON.
	CaptureJSONBody CaptureSource = "jsonBody"
	// CaptureHeader reads the value of the response header named by Capture.Expr
	// (matched case-insensitively).
	CaptureHeader CaptureSource = "header"
)

// Capture extracts a value from a Response and binds it to a runtime variable
// (see variables.Scope.Runtime) so a later request can reference it as
// {{variable}}. The extraction is described by Source plus Expr (a JSON path for
// jsonBody, a header name for header). A disabled Capture is kept but not run.
type Capture struct {
	Variable string        `json:"variable"`
	Source   CaptureSource `json:"source"`
	Expr     string        `json:"expr,omitempty"`
	Enabled  bool          `json:"enabled"`
}

// AssertSource identifies which part of a Response an Assertion inspects.
type AssertSource string

// Assertion sources.
const (
	// AssertStatus inspects the numeric HTTP status code.
	AssertStatus AssertSource = "status"
	// AssertJSONBody inspects the value at a JSON path (Assertion.Expr) inside
	// the response body parsed as JSON.
	AssertJSONBody AssertSource = "jsonBody"
	// AssertHeader inspects the response header named by Assertion.Expr
	// (matched case-insensitively).
	AssertHeader AssertSource = "header"
	// AssertResponseTimeMs inspects the response duration in whole milliseconds.
	AssertResponseTimeMs AssertSource = "responseTimeMs"
	// AssertRawBody inspects the raw response body as a string.
	AssertRawBody AssertSource = "rawBody"
)

// AssertOp is the comparison an Assertion applies to the actual value.
type AssertOp string

// Assertion operators.
const (
	// OpEquals passes when actual equals Expected (both trimmed).
	OpEquals AssertOp = "equals"
	// OpNotEquals passes when actual does not equal Expected (both trimmed).
	OpNotEquals AssertOp = "notEquals"
	// OpContains passes when actual contains Expected as a substring.
	OpContains AssertOp = "contains"
	// OpNotContains passes when actual does not contain Expected as a substring.
	OpNotContains AssertOp = "notContains"
	// OpExists passes when the source/path is present in the Response.
	OpExists AssertOp = "exists"
	// OpNotExists passes when the source/path is absent from the Response.
	OpNotExists AssertOp = "notExists"
	// OpLessThan passes when actual < Expected, both parsed as float64.
	OpLessThan AssertOp = "lessThan"
	// OpGreaterThan passes when actual > Expected, both parsed as float64.
	OpGreaterThan AssertOp = "greaterThan"
	// OpMatches passes when actual matches the regular expression in Expected.
	OpMatches AssertOp = "matches"
)

// Assertion is a single check run against a Response. Source (plus Expr for
// jsonBody/header) selects the actual value; Op compares it to Expected. A
// disabled Assertion is kept but not run, and is omitted from results.
type Assertion struct {
	Source   AssertSource `json:"source"`
	Expr     string       `json:"expr,omitempty"`
	Op       AssertOp     `json:"op"`
	Expected string       `json:"expected,omitempty"`
	Enabled  bool         `json:"enabled"`
}

// RequestOptions are the per-request connection overrides layered on top of the
// global Settings. Each field is a pointer so it is tri-state: nil = "inherit
// the global value", non-nil = "override with this value". omitempty drops any
// unset field from the JSON, and a wholly-empty RequestOptions still serializes
// as {} — but Request only carries a non-nil *RequestOptions when something is
// actually overridden (see the UI's value()), so unset requests omit the key
// entirely. yonner.ApplyRequestOptions overlays these onto yonner.Options.
type RequestOptions struct {
	// TimeoutSeconds overrides the total request time budget, in whole seconds.
	// A value <= 0 means "no client timeout" (matches yonner.Options.Timeout).
	TimeoutSeconds *int `json:"timeoutSeconds,omitempty"`
	// InsecureTLS overrides TLS certificate verification (true = skip verify).
	InsecureTLS *bool `json:"insecureTLS,omitempty"`
	// FollowRedirects overrides redirect following (false = return the first
	// redirect response instead of following it).
	FollowRedirects *bool `json:"followRedirects,omitempty"`
}

// Collection is a flat, ordered list of Requests persisted as one .yon file.
// Version is the schema version (starting at 1) and Auth is the
// Collection-level default whose Kind is one of none/basic/bearer.
//
// Variables are collection-scoped {{template}} values (lowest precedence, below
// the active environment). ActiveEnvironment names the environment selected for
// this collection; the environments themselves live in sibling files, not in the
// .yon (see the store package), so secrets never land in the committed file.
type Collection struct {
	Version           int        `json:"version"`
	Name              string     `json:"name"`
	Auth              Auth       `json:"auth"`
	Variables         []Variable `json:"variables,omitempty"`
	ActiveEnvironment string     `json:"activeEnvironment,omitempty"`
	Folders           []Folder   `json:"folders,omitempty"`
	Requests          []Request  `json:"requests,omitempty"`
	// Environments are environments stored INLINE in the .yon file (issue #35),
	// as opposed to the default sibling-file storage (the .environments/
	// directory + .env). omitempty keeps a collection with no inline
	// environments byte-identical on disk. An environment lives in exactly one
	// place — here OR a sibling file, never both.
	Environments []Environment `json:"environments,omitempty"`
}

// Response is the result of sending a Request. It is read-only data: status,
// status text, response headers, body bytes, and timing/size metadata.
type Response struct {
	Status     int           `json:"status"`
	StatusText string        `json:"statusText"`
	Headers    []Param       `json:"headers,omitempty"`
	Body       []byte        `json:"body,omitempty"`
	Duration   time.Duration `json:"duration"`
	Size       int64         `json:"size"`
}

// NewCollection returns an empty Collection at schema version 1 with the given
// name, no Auth (Kind AuthNone) and no Requests. It is the starting point for
// the "New" action.
func NewCollection(name string) Collection {
	return Collection{
		Version:  1,
		Name:     name,
		Auth:     Auth{Kind: AuthNone},
		Requests: nil,
	}
}

// ResolveAuth returns the Auth actually applied when req is sent: the
// Collection's Auth when the Request's Auth Kind is AuthInherit, otherwise the
// Request's own Auth. An explicit Request Auth of AuthNone is returned as-is,
// suppressing any inherited Collection Auth.
func ResolveAuth(req Request, coll Collection) Auth {
	if req.Auth.Kind == AuthInherit {
		return coll.Auth
	}
	return req.Auth
}

// DisplayName returns the Request's Name, or a derived "METHOD /path" label
// when Name is empty. The path is taken from the URL; if the URL cannot be
// parsed, it falls back to the raw URL, and if that is empty too, to just the
// Method.
func (r Request) DisplayName() string {
	if r.Name != "" {
		return r.Name
	}

	method := string(r.Method)

	raw := strings.TrimSpace(r.URL)
	if raw == "" {
		return method
	}

	u, err := url.Parse(raw)
	if err != nil {
		return method + " " + raw
	}

	path := u.Path
	if u.Opaque != "" { // e.g. "mailto:foo" or schemeless "host:port" forms
		path = u.Opaque
	}
	if path == "" {
		// No path component (e.g. "https://example.com"); fall back to the
		// host, then to the raw URL, so the label is never just bare.
		if u.Host != "" {
			return method + " " + u.Host
		}
		return method + " " + raw
	}

	return fmt.Sprintf("%s %s", method, path)
}
