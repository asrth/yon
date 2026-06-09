package importer

// Blind tests for specToCollection, written from the openapi_types.go +
// model.go CONTRACT ONLY (the mapper is a panic stub during fail-before). These
// construct *spec literals directly and assert the *structural* mapping into a
// model.Collection — never random IDs or brittle body bytes.

import (
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// mapBTFindFolder returns the folder with the given Name, failing if absent or
// duplicated.
func mapBTFindFolder(t *testing.T, coll model.Collection, name string) model.Folder {
	t.Helper()
	var found *model.Folder
	for i := range coll.Folders {
		if coll.Folders[i].Name == name {
			if found != nil {
				t.Fatalf("folder %q appears more than once in %v", name, coll.Folders)
			}
			f := coll.Folders[i]
			found = &f
		}
	}
	if found == nil {
		t.Fatalf("folder %q not found; folders = %v", name, coll.Folders)
	}
	return *found
}

// mapBTReqByName returns the (first) request with the given Name, failing if
// absent.
func mapBTReqByName(t *testing.T, coll model.Collection, name string) model.Request {
	t.Helper()
	for _, r := range coll.Requests {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("request %q not found; requests = %v", name, mapBTReqNames(coll))
	return model.Request{}
}

// mapBTReqNames lists request names in order, for failure messages.
func mapBTReqNames(coll model.Collection) []string {
	names := make([]string, 0, len(coll.Requests))
	for _, r := range coll.Requests {
		names = append(names, r.Name)
	}
	return names
}

// mapBTVar returns the variable with the given Key and whether it was present.
func mapBTVar(coll model.Collection, key string) (model.Variable, bool) {
	for _, v := range coll.Variables {
		if v.Key == key {
			return v, true
		}
	}
	return model.Variable{}, false
}

// mapBTHasParam reports whether the slice contains a Param with the given key.
func mapBTHasParam(params []model.Param, key string) (model.Param, bool) {
	for _, p := range params {
		if p.Key == key {
			return p, true
		}
	}
	return model.Param{}, false
}

// mapBTOp is a small constructor for an operation with sensible defaults.
func mapBTOp(name, method, path, tag string) operation {
	return operation{
		Method: method,
		Path:   path,
		Name:   name,
		Tag:    tag,
	}
}

// 1. Folders by tag: ops tagged "Users","Users","Pets" → 2 folders; the two
// Users requests share the Users folder's ID; the Pets request points at the
// Pets folder.
func TestSpecToCollection_FoldersByTag(t *testing.T) {
	s := &spec{
		Title: "Tagged API",
		Ops: []operation{
			mapBTOp("List Users", "GET", "/users", "Users"),
			mapBTOp("Create User", "POST", "/users", "Users"),
			mapBTOp("List Pets", "GET", "/pets", "Pets"),
		},
	}

	var report Report
	coll := specToCollection(s, &report)

	if len(coll.Folders) != 2 {
		t.Fatalf("want 2 folders, got %d: %v", len(coll.Folders), coll.Folders)
	}
	usersFolder := mapBTFindFolder(t, coll, "Users")
	petsFolder := mapBTFindFolder(t, coll, "Pets")
	if usersFolder.ID == "" || petsFolder.ID == "" {
		t.Fatalf("folders must have non-empty IDs: Users=%q Pets=%q", usersFolder.ID, petsFolder.ID)
	}
	if usersFolder.ID == petsFolder.ID {
		t.Fatalf("distinct tags must yield distinct folder IDs, both = %q", usersFolder.ID)
	}

	listUsers := mapBTReqByName(t, coll, "List Users")
	createUser := mapBTReqByName(t, coll, "Create User")
	listPets := mapBTReqByName(t, coll, "List Pets")

	if listUsers.FolderID != usersFolder.ID {
		t.Errorf("List Users FolderID = %q, want Users folder %q", listUsers.FolderID, usersFolder.ID)
	}
	if createUser.FolderID != usersFolder.ID {
		t.Errorf("Create User FolderID = %q, want Users folder %q", createUser.FolderID, usersFolder.ID)
	}
	if listUsers.FolderID != createUser.FolderID {
		t.Errorf("both Users requests must share a FolderID: %q vs %q", listUsers.FolderID, createUser.FolderID)
	}
	if listPets.FolderID != petsFolder.ID {
		t.Errorf("List Pets FolderID = %q, want Pets folder %q", listPets.FolderID, petsFolder.ID)
	}

	// Requests stay one flat ordered slice.
	if len(coll.Requests) != 3 {
		t.Errorf("want 3 flat requests, got %d: %v", len(coll.Requests), mapBTReqNames(coll))
	}
}

// 2. baseUrl variable + URL templating.
func TestSpecToCollection_BaseUrlTemplating(t *testing.T) {
	s := &spec{
		Title:   "Based API",
		BaseURL: "https://api.example.com/v1",
		Ops: []operation{
			mapBTOp("Get User", "GET", "/users/{id}", ""),
		},
	}

	var report Report
	coll := specToCollection(s, &report)

	v, ok := mapBTVar(coll, "baseUrl")
	if !ok {
		t.Fatalf("want a baseUrl variable; variables = %v", coll.Variables)
	}
	if v.Value != "https://api.example.com/v1" {
		t.Errorf("baseUrl value = %q, want %q", v.Value, "https://api.example.com/v1")
	}
	if !v.Enabled {
		t.Errorf("baseUrl variable should be Enabled")
	}

	getUser := mapBTReqByName(t, coll, "Get User")
	if want := "{{baseUrl}}/users/{id}"; getUser.URL != want {
		t.Errorf("request URL = %q, want %q", getUser.URL, want)
	}
}

// 2b. No BaseURL → URL == op.Path and no baseUrl variable.
func TestSpecToCollection_NoBaseUrl(t *testing.T) {
	s := &spec{
		Title: "Plain API",
		Ops: []operation{
			mapBTOp("Get User", "GET", "/users/{id}", ""),
		},
	}

	var report Report
	coll := specToCollection(s, &report)

	if _, ok := mapBTVar(coll, "baseUrl"); ok {
		t.Errorf("no BaseURL ⇒ no baseUrl variable; variables = %v", coll.Variables)
	}
	getUser := mapBTReqByName(t, coll, "Get User")
	if getUser.URL != "/users/{id}" {
		t.Errorf("request URL = %q, want raw path %q", getUser.URL, "/users/{id}")
	}
}

// 3. Params/Headers from query and header parameters.
func TestSpecToCollection_ParamsAndHeaders(t *testing.T) {
	op := mapBTOp("Search", "GET", "/search", "")
	op.Query = []param{{Name: "page", Value: "1"}}
	op.Headers = []param{{Name: "X-Trace", Value: "abc"}}

	s := &spec{Title: "Param API", Ops: []operation{op}}

	var report Report
	coll := specToCollection(s, &report)

	req := mapBTReqByName(t, coll, "Search")

	page, ok := mapBTHasParam(req.Params, "page")
	if !ok {
		t.Fatalf("want a query Param %q; params = %v", "page", req.Params)
	}
	if !page.Enabled {
		t.Errorf("query Param %q should be Enabled", "page")
	}

	trace, ok := mapBTHasParam(req.Headers, "X-Trace")
	if !ok {
		t.Fatalf("want a Header %q; headers = %v", "X-Trace", req.Headers)
	}
	if !trace.Enabled {
		t.Errorf("Header %q should be Enabled", "X-Trace")
	}
}

// 4. Body: JSON content type → BodyJSON with non-empty Content; nil Body →
// BodyNone.
func TestSpecToCollection_BodyJSON(t *testing.T) {
	op := mapBTOp("Create", "POST", "/items", "")
	op.Body = &reqBody{
		ContentType: "application/json",
		Schema: &schema{
			Type:      "object",
			PropOrder: []string{"name"},
			Properties: map[string]*schema{
				"name": {Type: "string"},
			},
		},
	}

	s := &spec{Title: "Body API", Ops: []operation{op}}

	var report Report
	coll := specToCollection(s, &report)

	req := mapBTReqByName(t, coll, "Create")
	if req.Body.Type != model.BodyJSON {
		t.Errorf("Body.Type = %q, want %q", req.Body.Type, model.BodyJSON)
	}
	if req.Body.Content == "" {
		t.Errorf("BodyJSON Content should be non-empty (exampleJSON), got empty")
	}
}

// 4b. nil Body → BodyNone.
func TestSpecToCollection_BodyNone(t *testing.T) {
	s := &spec{
		Title: "NoBody API",
		Ops: []operation{
			mapBTOp("List", "GET", "/items", ""),
		},
	}

	var report Report
	coll := specToCollection(s, &report)

	req := mapBTReqByName(t, coll, "List")
	if req.Body.Type != model.BodyNone {
		t.Errorf("nil Body ⇒ Body.Type = %q, want %q", req.Body.Type, model.BodyNone)
	}
}

// 5. Auth: global bearer security → collection AuthBearer; an op with no own
// security → request AuthInherit.
func TestSpecToCollection_AuthBearerInherit(t *testing.T) {
	s := &spec{
		Title:    "Secured API",
		Security: []secReq{{Name: "bearerAuth"}},
		Schemes: map[string]secScheme{
			"bearerAuth": {Type: "bearer"},
		},
		Ops: []operation{
			// No own security (HasSecurity false) ⇒ inherits collection auth.
			mapBTOp("List", "GET", "/items", ""),
		},
	}

	var report Report
	coll := specToCollection(s, &report)

	if coll.Auth.Kind != model.AuthBearer {
		t.Errorf("collection Auth.Kind = %q, want %q", coll.Auth.Kind, model.AuthBearer)
	}

	req := mapBTReqByName(t, coll, "List")
	if req.Auth.Kind != model.AuthInherit {
		t.Errorf("op without own security ⇒ request Auth.Kind = %q, want %q", req.Auth.Kind, model.AuthInherit)
	}
}

// 5b. apiKey-in-header scheme → injected as a request Header, NOT as
// Basic/Bearer auth.
func TestSpecToCollection_ApiKeyHeader(t *testing.T) {
	s := &spec{
		Title:    "ApiKey API",
		Security: []secReq{{Name: "apiKeyAuth"}},
		Schemes: map[string]secScheme{
			"apiKeyAuth": {Type: "apiKey", In: "header", Name: "X-API-Key"},
		},
		Ops: []operation{
			mapBTOp("List", "GET", "/items", ""),
		},
	}

	var report Report
	coll := specToCollection(s, &report)

	// apiKey is NOT a model.Auth kind; collection must not become Basic/Bearer.
	if coll.Auth.Kind == model.AuthBasic || coll.Auth.Kind == model.AuthBearer {
		t.Errorf("apiKey scheme must not become Basic/Bearer; collection Auth.Kind = %q", coll.Auth.Kind)
	}

	req := mapBTReqByName(t, coll, "List")
	if _, ok := mapBTHasParam(req.Headers, "X-API-Key"); !ok {
		t.Errorf("apiKey-in-header ⇒ request must carry Header %q; headers = %v", "X-API-Key", req.Headers)
	}
	if req.Auth.Kind == model.AuthBasic || req.Auth.Kind == model.AuthBearer {
		t.Errorf("apiKey scheme must not set request Basic/Bearer auth; got %q", req.Auth.Kind)
	}
}

// 6. Top-level requests: an op with Tag=="" → FolderID=="".
func TestSpecToCollection_TopLevelRequest(t *testing.T) {
	s := &spec{
		Title: "Mixed API",
		Ops: []operation{
			mapBTOp("Root Op", "GET", "/ping", ""),
			mapBTOp("Tagged Op", "GET", "/users", "Users"),
		},
	}

	var report Report
	coll := specToCollection(s, &report)

	root := mapBTReqByName(t, coll, "Root Op")
	if root.FolderID != "" {
		t.Errorf("Tag=\"\" ⇒ FolderID should be \"\", got %q", root.FolderID)
	}

	tagged := mapBTReqByName(t, coll, "Tagged Op")
	if tagged.FolderID == "" {
		t.Errorf("tagged op should have a non-empty FolderID")
	}
}

// Collection name defaults to "Imported API" when Title is empty.
func TestSpecToCollection_DefaultName(t *testing.T) {
	s := &spec{
		Ops: []operation{
			mapBTOp("Ping", "GET", "/ping", ""),
		},
	}

	var report Report
	coll := specToCollection(s, &report)

	if coll.Name != "Imported API" {
		t.Errorf("empty Title ⇒ collection Name = %q, want %q", coll.Name, "Imported API")
	}
}
