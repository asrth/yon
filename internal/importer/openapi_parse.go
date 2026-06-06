package importer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// decodeSpec parses raw OpenAPI/Swagger bytes (JSON or YAML; OpenAPI 3.x or
// Swagger 2.0) into the normalized *spec, resolving local $ref. It records
// non-fatal downgrades via report.add and returns an error only when the input
// is not a recognizable spec at all.
//
// Format detection: the bytes are first tried as JSON; on failure they are
// parsed as YAML into an interface{} and re-marshalled to JSON so a single set
// of json-tagged structs serves both formats (yaml.v3 yields
// map[string]interface{}, which round-trips cleanly through encoding/json).
//
// PropOrder: object property order is captured from the source declaration
// order via a json.Decoder token stream over the raw "properties" object (after
// the YAML→JSON normalization, so YAML maps keep their file order too). This is
// deterministic. If the raw bytes for a properties object cannot be located it
// falls back to sorted keys.
func decodeSpec(data []byte, report *Report) (*spec, error) {
	jsonData, err := toJSON(data)
	if err != nil {
		return nil, err
	}

	var root rawRoot
	if err := json.Unmarshal(jsonData, &root); err != nil {
		return nil, fmt.Errorf("parse OpenAPI/Swagger document: %w", err)
	}

	switch {
	case strings.HasPrefix(root.OpenAPI, "3."):
		return decodeV3(jsonData, &root, report)
	case root.Swagger == "2.0":
		return decodeV2(jsonData, &root, report)
	default:
		return nil, fmt.Errorf(`not a recognised OpenAPI/Swagger document (no "openapi: 3.x" or "swagger: 2.0")`)
	}
}

// toJSON returns the input unchanged when it already parses as JSON; otherwise
// it parses the input as YAML and re-encodes it as JSON.
func toJSON(data []byte) ([]byte, error) {
	if json.Valid(data) {
		return data, nil
	}
	var v interface{}
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("parse document as JSON or YAML: %w", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("normalise YAML to JSON: %w", err)
	}
	return out, nil
}

// ---- raw source structs (shared shape across 3.x and 2.0 where possible) ----

type rawRoot struct {
	OpenAPI     string                       `json:"openapi"` // 3.x
	Swagger     string                       `json:"swagger"` // 2.0
	Info        rawInfo                      `json:"info"`
	Servers     []rawServer                  `json:"servers"`  // 3.x
	Schemes     []string                     `json:"schemes"`  // 2.0
	Host        string                       `json:"host"`     // 2.0
	BasePath    string                       `json:"basePath"` // 2.0
	Consumes    []string                     `json:"consumes"` // 2.0
	Security    []map[string]json.RawMessage `json:"security"`
	Components  rawComponents                `json:"components"`          // 3.x
	SecDefs     map[string]rawSecScheme      `json:"securityDefinitions"` // 2.0
	Definitions map[string]json.RawMessage   `json:"definitions"`         // 2.0 schemas
}

type rawInfo struct {
	Title string `json:"title"`
}

type rawServer struct {
	URL string `json:"url"`
}

type rawComponents struct {
	Schemas         map[string]json.RawMessage `json:"schemas"`
	SecuritySchemes map[string]rawSecScheme    `json:"securitySchemes"`
}

type rawSecScheme struct {
	Type   string `json:"type"`   // "http"/"apiKey"/"oauth2" (3.x); "basic"/"apiKey"/"oauth2" (2.0)
	Scheme string `json:"scheme"` // 3.x http: "bearer"/"basic"
	In     string `json:"in"`     // apiKey: "header"/"query"
	Name   string `json:"name"`   // apiKey: parameter name
}

// rawPathOps captures the per-method operations of a path item plus the
// path-level parameters, keeping raw bytes so we can decode them with full
// context.
type rawPathOps struct {
	Parameters json.RawMessage `json:"parameters"`
	Get        *rawOperation   `json:"get"`
	Put        *rawOperation   `json:"put"`
	Post       *rawOperation   `json:"post"`
	Delete     *rawOperation   `json:"delete"`
	Patch      *rawOperation   `json:"patch"`
	Head       *rawOperation   `json:"head"`
	Options    *rawOperation   `json:"options"`
	Trace      *rawOperation   `json:"trace"`
}

type rawOperation struct {
	Summary     string                        `json:"summary"`
	OperationID string                        `json:"operationId"`
	Tags        []string                      `json:"tags"`
	Parameters  []rawParam                    `json:"parameters"`
	RequestBody *rawRequestBody               `json:"requestBody"` // 3.x
	Security    *[]map[string]json.RawMessage `json:"security"`    // pointer: absent vs empty
	Consumes    []string                      `json:"consumes"`    // 2.0 op-level
}

type rawParam struct {
	Name     string          `json:"name"`
	In       string          `json:"in"` // query/header/path/cookie/body/formData
	Required bool            `json:"required"`
	Example  json.RawMessage `json:"example"` // 3.x
	Default  json.RawMessage `json:"default"` // 2.0
	Schema   json.RawMessage `json:"schema"`  // 3.x param schema; 2.0 body schema
}

type rawRequestBody struct {
	Required bool                    `json:"required"`
	Content  map[string]rawMediaType `json:"content"`
}

type rawMediaType struct {
	Schema json.RawMessage `json:"schema"`
}

// methods returns the operations of a path item in canonical verb order, each
// paired with its upper-case method name.
func (p *rawPathOps) methods() []struct {
	name string
	op   *rawOperation
} {
	return []struct {
		name string
		op   *rawOperation
	}{
		{"GET", p.Get},
		{"PUT", p.Put},
		{"POST", p.Post},
		{"DELETE", p.Delete},
		{"PATCH", p.Patch},
		{"HEAD", p.Head},
		{"OPTIONS", p.Options},
		{"TRACE", p.Trace},
	}
}

// ---- version-specific decoding ----

func decodeV3(jsonData []byte, root *rawRoot, report *Report) (*spec, error) {
	s := &spec{
		Title:   root.Info.Title,
		Version: root.OpenAPI,
		Schemes: map[string]secScheme{},
	}

	// BaseURL: servers[0].url.
	if len(root.Servers) > 0 {
		s.BaseURL = root.Servers[0].URL
		if len(root.Servers) > 1 {
			report.add("Multiple servers were declared; using the first one.")
		}
	}

	for name, raw := range root.Components.SecuritySchemes {
		s.Schemes[name] = normalizeScheme(raw, report)
	}
	s.Security = normalizeSecurity(root.Security)

	res := newResolver(root.Components.Schemas, "#/components/schemas/", report)
	s.Ops = decodeOperations(jsonData, root, res, true, report)
	return s, nil
}

func decodeV2(jsonData []byte, root *rawRoot, report *Report) (*spec, error) {
	s := &spec{
		Title:   root.Info.Title,
		Version: root.Swagger,
		Schemes: map[string]secScheme{},
	}

	// BaseURL: scheme://host + basePath. Default scheme https.
	scheme := "https"
	if len(root.Schemes) > 0 {
		scheme = root.Schemes[0]
		if len(root.Schemes) > 1 {
			report.add("Multiple schemes were declared; using the first one.")
		}
	}
	s.BaseURL = scheme + "://" + root.Host + root.BasePath

	for name, raw := range root.SecDefs {
		s.Schemes[name] = normalizeScheme(raw, report)
	}
	s.Security = normalizeSecurity(root.Security)

	res := newResolver(root.Definitions, "#/definitions/", report)
	s.Ops = decodeOperations(jsonData, root, res, false, report)
	return s, nil
}

// decodeOperations walks paths (in document order) and methods, building
// operations. isV3 selects requestBody vs in:body handling. Path order is taken
// from the raw "paths" object token stream so it is deterministic.
func decodeOperations(jsonData []byte, root *rawRoot, res *resolver, isV3 bool, report *Report) []operation {
	pathOrder := objectKeyOrder(jsonData, "paths")

	var rawPaths struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	_ = json.Unmarshal(jsonData, &rawPaths)

	var ops []operation
	for _, path := range pathOrder {
		rawItem, ok := rawPaths.Paths[path]
		if !ok {
			continue
		}
		var item rawPathOps
		if err := json.Unmarshal(rawItem, &item); err != nil {
			continue
		}

		var pathParams []rawParam
		if len(item.Parameters) > 0 {
			_ = json.Unmarshal(item.Parameters, &pathParams)
		}

		for _, m := range item.methods() {
			if m.op == nil {
				continue
			}
			ops = append(ops, buildOperation(m.name, path, m.op, pathParams, root, res, isV3, report))
		}
	}
	return ops
}

func buildOperation(method, path string, op *rawOperation, pathParams []rawParam, root *rawRoot, res *resolver, isV3 bool, report *Report) operation {
	o := operation{
		Method: method,
		Path:   path,
	}

	// Name: summary, else operationId, else "METHOD path".
	switch {
	case op.Summary != "":
		o.Name = op.Summary
	case op.OperationID != "":
		o.Name = op.OperationID
	default:
		o.Name = method + " " + path
	}

	if len(op.Tags) > 0 {
		o.Tag = op.Tags[0]
	}

	// Merge path-level + operation-level parameters (op-level wins on name+in).
	merged := mergeParams(pathParams, op.Parameters)
	var bodyParam *rawParam // 2.0 in:body
	for i := range merged {
		p := &merged[i]
		switch p.In {
		case "query":
			o.Query = append(o.Query, normalizeParam(p))
		case "header":
			o.Headers = append(o.Headers, normalizeParam(p))
		case "body":
			bodyParam = p // 2.0
			// path stays in the URL template; formData/cookie are not stored.
		}
	}

	if isV3 {
		o.Body = buildV3Body(op.RequestBody, res)
	} else {
		o.Body = buildV2Body(bodyParam, op.Consumes, root.Consumes, res)
	}

	// Operation-level security overrides the global default (empty ⇒ none).
	if op.Security != nil {
		o.HasSecurity = true
		o.Security = normalizeSecurity(*op.Security)
	}

	return o
}

// mergeParams overlays op-level params onto path-level params, keyed by
// name+in; op-level entries replace matching path-level ones. Order: surviving
// path-level params first, then op-level params.
func mergeParams(pathParams, opParams []rawParam) []rawParam {
	key := func(p rawParam) string { return p.In + "\x00" + p.Name }
	overridden := map[string]bool{}
	for _, p := range opParams {
		overridden[key(p)] = true
	}
	var out []rawParam
	for _, p := range pathParams {
		if !overridden[key(p)] {
			out = append(out, p)
		}
	}
	out = append(out, opParams...)
	return out
}

func normalizeParam(p *rawParam) param {
	return param{
		Name:     p.Name,
		Value:    paramValue(p),
		Required: p.Required,
	}
}

// paramValue renders a parameter's example/default as a string. It checks the
// param's own example (3.x), then its schema's example/default, then the param
// default (2.0).
func paramValue(p *rawParam) string {
	if v := rawScalarString(p.Example); v != "" {
		return v
	}
	if len(p.Schema) > 0 {
		var sch struct {
			Example json.RawMessage `json:"example"`
			Default json.RawMessage `json:"default"`
		}
		if json.Unmarshal(p.Schema, &sch) == nil {
			if v := rawScalarString(sch.Example); v != "" {
				return v
			}
			if v := rawScalarString(sch.Default); v != "" {
				return v
			}
		}
	}
	if v := rawScalarString(p.Default); v != "" {
		return v
	}
	return ""
}

// rawScalarString renders a JSON scalar (string/number/bool) as a plain string.
// Strings are unquoted; objects/arrays/null yield "".
func rawScalarString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	return scalarToString(v)
}

func scalarToString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	default:
		// object/array — not a scalar.
		return ""
	}
}

func buildV3Body(rb *rawRequestBody, res *resolver) *reqBody {
	if rb == nil || len(rb.Content) == 0 {
		return nil
	}
	ct, mt := pickContent(rb.Content)
	b := &reqBody{ContentType: ct}
	if len(mt.Schema) > 0 {
		b.Schema = res.resolve(mt.Schema, nil)
	}
	return b
}

// pickContent prefers application/json, else the first content type in sorted
// order for determinism.
func pickContent(content map[string]rawMediaType) (string, rawMediaType) {
	if mt, ok := content["application/json"]; ok {
		return "application/json", mt
	}
	keys := make([]string, 0, len(content))
	for k := range content {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys[0], content[keys[0]]
}

func buildV2Body(bodyParam *rawParam, opConsumes, rootConsumes []string, res *resolver) *reqBody {
	if bodyParam == nil {
		return nil
	}
	ct := "application/json"
	switch {
	case len(opConsumes) > 0:
		ct = opConsumes[0]
	case len(rootConsumes) > 0:
		ct = rootConsumes[0]
	}
	b := &reqBody{ContentType: ct}
	if len(bodyParam.Schema) > 0 {
		b.Schema = res.resolve(bodyParam.Schema, nil)
	}
	return b
}

// ---- security ----

// normalizeSecurity flattens a security requirements list ([{schemeA: [...]},
// {schemeB: [...]}]) into a deduped []secReq by name, preserving first-seen
// order. OAuth scopes are ignored.
func normalizeSecurity(reqs []map[string]json.RawMessage) []secReq {
	var out []secReq
	seen := map[string]bool{}
	for _, m := range reqs {
		// Within one requirement object, sort names for determinism.
		names := make([]string, 0, len(m))
		for name := range m {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, secReq{Name: name})
		}
	}
	return out
}

// normalizeScheme maps a raw security scheme (3.x or 2.0) to a secScheme.
func normalizeScheme(raw rawSecScheme, report *Report) secScheme {
	switch raw.Type {
	case "http": // 3.x
		switch strings.ToLower(raw.Scheme) {
		case "bearer":
			return secScheme{Type: "bearer"}
		case "basic":
			return secScheme{Type: "basic"}
		default:
			report.add(fmt.Sprintf("HTTP security scheme %q is not supported.", raw.Scheme))
			return secScheme{Type: ""}
		}
	case "basic": // 2.0
		return secScheme{Type: "basic"}
	case "apiKey":
		return secScheme{Type: "apiKey", In: raw.In, Name: raw.Name}
	case "oauth2":
		return secScheme{Type: "oauth2"}
	default:
		report.add(fmt.Sprintf("Security scheme type %q is not supported.", raw.Type))
		return secScheme{Type: ""}
	}
}

// ---- $ref resolution ----

// resolver resolves local schema $refs into concrete *schema values, with a
// per-resolution visited set to break cycles.
type resolver struct {
	defs   map[string]json.RawMessage // name → raw schema bytes
	prefix string                     // "#/components/schemas/" or "#/definitions/"
	report *Report
}

func newResolver(defs map[string]json.RawMessage, prefix string, report *Report) *resolver {
	if defs == nil {
		defs = map[string]json.RawMessage{}
	}
	return &resolver{defs: defs, prefix: prefix, report: report}
}

// resolve converts raw schema bytes into a normalized *schema. visited tracks
// ref names currently being expanded to guard against cycles.
func (r *resolver) resolve(raw json.RawMessage, visited map[string]bool) *schema {
	if len(raw) == 0 {
		return nil
	}

	var head struct {
		Ref string `json:"$ref"`
	}
	if json.Unmarshal(raw, &head) == nil && head.Ref != "" {
		return r.resolveRef(head.Ref, visited)
	}
	return r.buildSchema(raw, visited)
}

func (r *resolver) resolveRef(ref string, visited map[string]bool) *schema {
	if !strings.HasPrefix(ref, r.prefix) {
		// External/remote ref or a ref into an unsupported location.
		r.report.add(fmt.Sprintf("External or unsupported $ref %q was left empty.", ref))
		return &schema{}
	}
	name := strings.TrimPrefix(ref, r.prefix)

	if visited[name] {
		r.report.add(fmt.Sprintf("Cyclic $ref to %q was broken (left empty).", name))
		return &schema{}
	}
	raw, ok := r.defs[name]
	if !ok {
		r.report.add(fmt.Sprintf("Unresolved local $ref %q was left empty.", ref))
		return &schema{}
	}

	if visited == nil {
		visited = map[string]bool{}
	}
	visited[name] = true
	out := r.buildSchema(raw, visited)
	delete(visited, name)
	return out
}

// buildSchema constructs a normalized *schema from raw (non-$ref) bytes,
// recursing into properties and items. PropOrder is captured from the raw
// "properties" object declaration order.
func (r *resolver) buildSchema(raw json.RawMessage, visited map[string]bool) *schema {
	var src struct {
		Type       string                     `json:"type"`
		Format     string                     `json:"format"`
		Properties map[string]json.RawMessage `json:"properties"`
		Items      json.RawMessage            `json:"items"`
		Required   []string                   `json:"required"`
		Example    json.RawMessage            `json:"example"`
		Default    json.RawMessage            `json:"default"`
		Enum       []json.RawMessage          `json:"enum"`
	}
	if err := json.Unmarshal(raw, &src); err != nil {
		return &schema{}
	}

	out := &schema{
		Type:     src.Type,
		Format:   src.Format,
		Required: src.Required,
	}

	// Example, else default.
	if len(src.Example) > 0 {
		out.Example = decodeAny(src.Example)
	} else if len(src.Default) > 0 {
		out.Example = decodeAny(src.Default)
	}

	for _, e := range src.Enum {
		out.Enum = append(out.Enum, decodeAny(e))
	}

	// Properties (objects).
	if len(src.Properties) > 0 {
		out.Properties = map[string]*schema{}
		out.PropOrder = propertyOrder(raw, src.Properties)
		for _, name := range out.PropOrder {
			out.Properties[name] = r.resolve(src.Properties[name], visited)
		}
	}

	// Items (arrays).
	if len(src.Items) > 0 {
		out.Items = r.resolve(src.Items, visited)
	}

	// Infer the structural type when absent.
	if out.Type == "" && out.Properties != nil {
		out.Type = "object"
	}
	if out.Type == "" && out.Items != nil {
		out.Type = "array"
	}

	return out
}

// propertyOrder returns property names in source declaration order, captured
// from the raw "properties" object token stream. Falls back to sorted keys if
// the raw object can't be located.
func propertyOrder(rawSchema json.RawMessage, props map[string]json.RawMessage) []string {
	order := objectKeyOrder(rawSchema, "properties")
	if len(order) == 0 {
		return sortedKeys(props)
	}
	filtered := make([]string, 0, len(props))
	seen := map[string]bool{}
	for _, k := range order {
		if _, ok := props[k]; ok && !seen[k] {
			filtered = append(filtered, k)
			seen[k] = true
		}
	}
	// Append any keys the token scan missed (sorted) so none are dropped.
	for _, k := range sortedKeys(props) {
		if !seen[k] {
			filtered = append(filtered, k)
		}
	}
	return filtered
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// decodeAny unmarshals raw JSON into a generic Go value, or nil on error.
func decodeAny(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

// objectKeyOrder returns the keys of the named top-level object in declaration
// order by scanning the JSON token stream. It returns nil if the field is
// absent or not an object.
func objectKeyOrder(data []byte, field string) []string {
	dec := json.NewDecoder(bytes.NewReader(data))

	tok, err := dec.Token()
	if err != nil {
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil
	}

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil
		}
		key, _ := keyTok.(string)
		if key == field {
			return innerObjectKeyOrder(dec)
		}
		if err := skipValue(dec); err != nil {
			return nil
		}
	}
	return nil
}

// innerObjectKeyOrder reads the next value (expected to be an object) and
// returns its keys in order, consuming the whole object.
func innerObjectKeyOrder(dec *json.Decoder) []string {
	tok, err := dec.Token()
	if err != nil {
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil
	}
	var keys []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil
		}
		if key, ok := keyTok.(string); ok {
			keys = append(keys, key)
		}
		if err := skipValue(dec); err != nil {
			return nil
		}
	}
	_, _ = dec.Token() // consume closing '}'
	return keys
}

// skipValue consumes a single JSON value (scalar, object, or array) from the
// decoder, including all nested content.
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return nil // scalar already consumed
	}
	switch d {
	case '{', '[':
		depth := 1
		for depth > 0 {
			t, err := dec.Token()
			if err != nil {
				return err
			}
			if dd, ok := t.(json.Delim); ok {
				switch dd {
				case '{', '[':
					depth++
				case '}', ']':
					depth--
				}
			}
		}
	}
	return nil
}
