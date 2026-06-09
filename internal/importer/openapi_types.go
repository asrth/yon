package importer

import "github.com/ultramcu/yon/internal/model"

// ImportOpenAPI parses an OpenAPI 3.x or Swagger 2.0 document — in JSON or YAML —
// into a Yon Collection, returning a Report of anything unsupported. It is the
// OpenAPI counterpart to Import (which handles Postman Collection v2.1) and
// reuses the same model.Collection and Report types.
//
// The work is split across three internal units that share ONLY the normalized
// types in this file:
//
//	decodeSpec      (openapi_parse.go)   raw bytes  -> *spec   (format + version + $ref)
//	specToCollection(openapi_map.go)     *spec      -> model.Collection
//	exampleJSON     (openapi_example.go) *schema    -> a JSON example string
func ImportOpenAPI(data []byte) (model.Collection, Report, error) {
	var report Report
	s, err := decodeSpec(data, &report)
	if err != nil {
		return model.Collection{}, report, err
	}
	return specToCollection(s, &report), report, nil
}

// spec is the version-agnostic normalized form of an OpenAPI/Swagger document.
// decodeSpec fills it from either a 3.x or a 2.0 source; specToCollection reads
// it. It deliberately captures only what Yon can represent.
type spec struct {
	Title    string               // info.title → collection name ("" allowed)
	Version  string               // "3.0.1" / "2.0" / … (informational)
	BaseURL  string               // resolved base URL; may contain {placeholders}; "" if none
	Ops      []operation          // every operation, in document order
	Security []secReq             // global/default security requirements (may be empty)
	Schemes  map[string]secScheme // securityScheme name → normalized scheme
}

// operation is a single path + method.
type operation struct {
	Method      string   // upper-case HTTP method: GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS
	Path        string   // raw template path, e.g. "/users/{id}"
	Name        string   // summary, else operationId, else "METHOD path"
	Tag         string   // first tag, or "" → top-level (no folder)
	Query       []param  // parameters in:query
	Headers     []param  // parameters in:header
	Body        *reqBody // request body, or nil
	Security    []secReq // when HasSecurity, overrides spec.Security for this op
	HasSecurity bool     // op declared its own security (empty slice ⇒ explicitly none)
}

// param is a normalized query/header/path parameter.
type param struct {
	Name     string // parameter name
	Value    string // default/example rendered as a string, else ""
	Required bool
}

// reqBody is a normalized request body.
type reqBody struct {
	ContentType string  // e.g. "application/json"; "" → treat as text
	Schema      *schema // schema for example generation, or nil
}

// secReq references a required security scheme by name (OAuth scopes ignored).
type secReq struct {
	Name string
}

// secScheme is a normalized security scheme.
type secScheme struct {
	Type string // "bearer" | "basic" | "apiKey" | "oauth2" | "" (unknown/unsupported)
	In   string // apiKey only: "header" | "query"
	Name string // apiKey only: the header/query parameter name
}

// schema is the JSON-Schema subset used for example generation. The parser
// resolves local $ref into a concrete schema before handing it on, so consumers
// never have to chase references.
type schema struct {
	Type       string             // object/array/string/integer/number/boolean
	Format     string             // e.g. "date-time"/"int64" (informational)
	Properties map[string]*schema // object properties
	PropOrder  []string           // property names in document order (stable output)
	Items      *schema            // array element schema
	Required   []string           // required property names
	Example    any                // example/default value, if any
	Enum       []any              // allowed values, if any
}
