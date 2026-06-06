package importer

import (
	"fmt"
	"strings"

	"github.com/ultramcu/yon/internal/model"
)

// specToCollection maps a normalized *spec into a Yon model.Collection:
// operations → requests grouped by tag into folders, BaseURL → a {{baseUrl}}
// collection variable, parameters → query/header params, body → a Body with a
// generated example (exampleJSON), and security → Auth. Non-representable bits
// are noted via report.add.
//
// Output is deterministic: folders appear in first-seen tag order and requests
// stay in document order, matching the order of s.Ops.
func specToCollection(s *spec, report *Report) model.Collection {
	// 1. Name.
	name := s.Title
	if name == "" {
		name = "Imported API"
		report.add("The spec has no info.title; named the collection \"Imported API\".")
	}
	coll := model.NewCollection(name)

	// 2. Base URL → a {{baseUrl}} collection variable, or note its absence.
	urlPrefix := ""
	if s.BaseURL != "" {
		coll.Variables = append(coll.Variables, model.Variable{
			Key:     "baseUrl",
			Value:   s.BaseURL,
			Enabled: true,
		})
		urlPrefix = "{{baseUrl}}"
	} else {
		report.add("The spec declares no server URL; request URLs use the raw path only.")
	}

	// 6 (collection-level auth). Resolve the effective collection security so we
	// can both set coll.Auth and let per-request auth fall back to Inherit.
	collAuth := resolveAuth(s.Security, s.Schemes)
	coll.Auth = collAuth

	// 3. Folders: one per distinct non-empty tag, in first-seen order.
	tagToFolder := map[string]string{}
	for _, op := range s.Ops {
		if op.Tag == "" {
			continue
		}
		if _, seen := tagToFolder[op.Tag]; seen {
			continue
		}
		id := model.NewFolderID()
		tagToFolder[op.Tag] = id
		coll.Folders = append(coll.Folders, model.Folder{ID: id, Name: op.Tag})
	}

	// Track whether we have already added the shared apiKey collection variable.
	apiKeyVarAdded := false

	// 4/5/6. Each operation → a request.
	for _, op := range s.Ops {
		req := model.Request{
			Name:     op.Name,
			Method:   mapMethod(op.Method),
			URL:      urlPrefix + op.Path,
			FolderID: tagToFolder[op.Tag],
		}

		// Query params.
		for _, q := range op.Query {
			req.Params = append(req.Params, model.Param{
				Key:     q.Name,
				Value:   q.Value,
				Enabled: true,
			})
		}
		// Header params.
		for _, h := range op.Headers {
			req.Headers = append(req.Headers, model.Param{
				Key:     h.Name,
				Value:   h.Value,
				Enabled: true,
			})
		}

		// 5. Body.
		req.Body = mapBody(op.Body, op.Name, report)

		// 6. Auth (per request), including apiKey injection as header/query.
		req.Auth = mapRequestAuth(op, s, collAuth, report, &req, &coll, &apiKeyVarAdded)

		coll.Requests = append(coll.Requests, req)
	}

	return coll
}

// mapMethod maps an upper-case HTTP verb to a model.Method, preferring the
// well-known consts and casting anything unusual verbatim.
func mapMethod(method string) model.Method {
	switch strings.ToUpper(method) {
	case "GET":
		return model.MethodGet
	case "POST":
		return model.MethodPost
	case "PUT":
		return model.MethodPut
	case "PATCH":
		return model.MethodPatch
	case "DELETE":
		return model.MethodDelete
	case "HEAD":
		return model.MethodHead
	case "OPTIONS":
		return model.MethodOptions
	default:
		return model.Method(strings.ToUpper(method))
	}
}

// mapBody converts a normalized request body into a model.Body, choosing the
// body type from the content type and recording downgrade notes. JSON-ish types
// get a generated example; XML-ish and other types are downgraded (no example).
func mapBody(body *reqBody, owner string, report *Report) model.Body {
	if body == nil {
		return model.Body{Type: model.BodyNone}
	}

	ct := strings.ToLower(strings.TrimSpace(body.ContentType))
	switch {
	case ct == "application/json" || strings.HasSuffix(ct, "+json"):
		return model.Body{Type: model.BodyJSON, Content: exampleJSON(body.Schema)}
	case ct == "application/xml" || ct == "text/xml" || strings.HasSuffix(ct, "+xml"):
		report.add(fmt.Sprintf("XML body on %q has no example generator; left the body empty.", owner))
		return model.Body{Type: model.BodyXML, Content: ""}
	default:
		report.add(fmt.Sprintf("Body content type %q on %q is unsupported; imported as empty text.", body.ContentType, owner))
		return model.Body{Type: model.BodyText, Content: ""}
	}
}

// resolveAuth maps the first supported requirement in reqs (bearer/basic) into a
// model.Auth for use at the collection level. apiKey/oauth2 and unknown schemes
// are NOT representable as a collection model.Auth, so this returns AuthNone for
// them (per-request handling injects apiKey headers / oauth2 bearer placeholders).
func resolveAuth(reqs []secReq, schemes map[string]secScheme) model.Auth {
	for _, r := range reqs {
		switch schemes[r.Name].Type {
		case "bearer":
			return model.Auth{Kind: model.AuthBearer}
		case "basic":
			return model.Auth{Kind: model.AuthBasic}
		}
	}
	return model.Auth{Kind: model.AuthNone}
}

// mapRequestAuth resolves the effective auth for one operation. The effective
// security requirements are the operation's own when it declares them, else the
// spec's global default — and apiKey/oauth2 handling applies to whichever set is
// in force (so a global apiKey requirement is injected into every request, not
// just ones with their own security).
//
//   - No effective requirements (no own security and no global) → AuthInherit.
//   - HasSecurity with an empty slice → AuthNone (explicitly unauthenticated).
//   - apiKey requirement → injected as a header/query Param on the request and a
//     shared {{apiKey}} collection variable; the request Auth defers to the
//     collection (AuthInherit).
//   - oauth2 requirement → AuthBearer with a {{token}} placeholder + a note.
//   - bearer/basic requirement → that Auth, collapsed to AuthInherit when it
//     equals the collection's auth.
//
// It may mutate req (header/query injection) and coll (the apiKey variable).
func mapRequestAuth(op operation, s *spec, collAuth model.Auth, report *Report, req *model.Request, coll *model.Collection, apiKeyVarAdded *bool) model.Auth {
	// An operation that declares security with an empty list is explicitly
	// unauthenticated, overriding any global default.
	if op.HasSecurity && len(op.Security) == 0 {
		return model.Auth{Kind: model.AuthNone}
	}

	// Effective requirements: the operation's own, else the global default.
	effective := s.Security
	if op.HasSecurity {
		effective = op.Security
	}
	if len(effective) == 0 {
		return model.Auth{Kind: model.AuthInherit}
	}

	// Handle apiKey / oauth2 schemes that can't map to a bearer/basic model.Auth.
	for _, r := range effective {
		switch s.Schemes[r.Name].Type {
		case "apiKey":
			injectAPIKey(s.Schemes[r.Name], report, req, coll, apiKeyVarAdded)
			// apiKey isn't a model.Auth; defer the request's Auth to the
			// collection (which is None unless a bearer/basic global exists).
			return model.Auth{Kind: model.AuthInherit}
		case "oauth2":
			report.add(fmt.Sprintf("OAuth2 scheme %q is not supported; mapped to a Bearer token placeholder ({{token}}).", r.Name))
			return model.Auth{Kind: model.AuthBearer, Token: "{{token}}"}
		}
	}

	// Otherwise resolve a bearer/basic requirement. When the operation has no
	// own security it inherits the collection auth; an own requirement equal to
	// the collection collapses to AuthInherit too.
	if !op.HasSecurity {
		return model.Auth{Kind: model.AuthInherit}
	}
	auth := resolveAuth(effective, s.Schemes)
	if auth == collAuth {
		return model.Auth{Kind: model.AuthInherit}
	}
	return auth
}

// injectAPIKey injects an apiKey security scheme into req as a header or query
// Param valued "{{apiKey}}", adds the shared apiKey collection variable once,
// and records a note. An unknown In is noted and skipped.
func injectAPIKey(sc secScheme, report *Report, req *model.Request, coll *model.Collection, apiKeyVarAdded *bool) {
	added := false
	switch sc.In {
	case "header":
		req.Headers = append(req.Headers, model.Param{Key: sc.Name, Value: "{{apiKey}}", Enabled: true})
		report.add(fmt.Sprintf("API key scheme injected as the %q request header (value {{apiKey}}).", sc.Name))
		added = true
	case "query":
		req.Params = append(req.Params, model.Param{Key: sc.Name, Value: "{{apiKey}}", Enabled: true})
		report.add(fmt.Sprintf("API key scheme injected as the %q query parameter (value {{apiKey}}).", sc.Name))
		added = true
	default:
		report.add(fmt.Sprintf("API key scheme has unsupported location %q; skipped.", sc.In))
	}

	if added && !*apiKeyVarAdded {
		coll.Variables = append(coll.Variables, model.Variable{Key: "apiKey", Value: "", Enabled: true})
		*apiKeyVarAdded = true
	}
}
