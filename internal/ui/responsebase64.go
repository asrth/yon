package ui

import (
	"bytes"
	"encoding/base64"
	"strings"
)

// base64Source describes how a response body encodes a base64 image (issue #37).
type base64Source int

const (
	// base64None: the body is not a (detectable) base64 image.
	base64None base64Source = iota
	// base64DataURI: the body is a data URI, "data:image/...;base64,<...>". This
	// is unambiguous, so the UI decodes and previews it automatically.
	base64DataURI
	// base64Bare: the body is a bare base64 string that decodes to image magic
	// bytes. This is ambiguous (a long token can look like base64), so the UI
	// only offers an opt-in "Decode as image" action rather than auto-previewing.
	base64Bare
)

// base64ImageDecode reports whether body is a base64-encoded image and, if so,
// returns the decoded image bytes and how it was encoded:
//
//   - base64DataURI when body is a "data:image/<type>;base64,<data>" data URI
//     whose decoded payload begins with image magic bytes;
//   - base64Bare when the whole (trimmed) body is valid base64 that decodes to
//     image magic bytes;
//   - base64None otherwise (decoded is nil).
//
// The decoded payload is always magic-byte-checked (hasImageMagic) so a random
// base64 blob / JWT-looking token is not mistaken for an image. contentType is
// accepted for parity with classifyBody but the detection is body-driven.
//
// LANE: detection/decode (Dev A owns + hardens this stub).
func base64ImageDecode(contentType string, body []byte) (decoded []byte, source base64Source, ok bool) {
	_ = bytes.TrimSpace
	_ = base64.StdEncoding
	_ = strings.TrimSpace
	return nil, base64None, false
}
