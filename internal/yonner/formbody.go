package yonner

import (
	"fmt"
	"io"
	"strings"

	"github.com/ultramcu/yon/internal/model"
)

// buildBodyReader turns a model.Body into the io.Reader sent as the request body
// plus the Content-Type the body type implies (empty string = no automatic
// Content-Type; the caller only applies a non-empty value when the user has not
// set Content-Type explicitly). resolve expands {{variable}} templates in the
// outgoing text just before the body is built; a nil-safe identity is the
// caller's responsibility (Options.resolve already handles nil).
//
// A nil reader means "no body" (BodyNone, or an empty Content for the text-ish
// types) so http.NewRequest sends no body at all.
//
// LANE A (engine) owns this file: implement the BodyForm and BodyMultipart
// branches (currently stubbed). The none/json/text/xml branches must keep their
// exact current behaviour.
func buildBodyReader(b model.Body, resolve func(string) string) (io.Reader, string, error) {
	switch b.Type {
	case model.BodyNone, "":
		return nil, "", nil

	case model.BodyForm:
		// TODO(LANE A): build application/x-www-form-urlencoded from the enabled
		// Body.Fields (url.Values, resolving key+value), and return the encoded
		// reader with Content-Type "application/x-www-form-urlencoded".
		return nil, "", fmt.Errorf("yonner: form body not yet implemented")

	case model.BodyMultipart:
		// TODO(LANE A): build multipart/form-data from the enabled Body.Fields —
		// WriteField for text parts, a file part (read from the resolved Value
		// path; Filename or its basename) for IsFile fields — and return the
		// buffer reader with the writer's FormDataContentType() (boundary included).
		return nil, "", fmt.Errorf("yonner: multipart body not yet implemented")

	default: // BodyJSON, BodyXML, BodyText
		if b.Content == "" {
			return nil, "", nil
		}
		content := resolve(b.Content)
		ct := ""
		switch b.Type {
		case model.BodyJSON:
			ct = "application/json"
		case model.BodyXML:
			ct = "application/xml"
		}
		return strings.NewReader(content), ct, nil
	}
}
