package ui

import (
	"bytes"
	"encoding/base64"
	"mime"
	"strings"
)

// base64Source describes how a response body encodes an image as base64 — or
// that it does not. base64ImageDecode returns one of these so the caller can
// decide how confident to be about previewing the decoded bytes.
type base64Source int

const (
	// base64None: the body is not (recognisably) a base64-encoded image.
	base64None base64Source = iota
	// base64DataURI: the body is an unambiguous `data:<media>;base64,…` URI.
	// The UI may auto-preview these.
	base64DataURI
	// base64Bare: the whole body is bare base64 that decodes to image magic
	// bytes. This is ambiguous (it could be coincidental), so the UI offers an
	// opt-in button rather than auto-previewing.
	base64Bare
)

// minBareDecodedLen is the smallest decoded length we accept for a *bare*
// base64 image. A handful of base64 characters can coincidentally decode to a
// few bytes that happen to start with the weak BMP "BM" magic; requiring a small
// floor of decoded bytes drops the most trivial accidental matches. It is kept
// modest (not hundreds of bytes) so a genuinely small image — a favicon, a tiny
// PNG — still previews; the strong PNG/JPEG/GIF/WebP signatures plus the fact
// that the bare path is opt-in (a "Decode as image" button, never auto) keep the
// false-positive risk low. Data URIs are exempt entirely — their
// `data:…;base64,` framing is explicit enough that even a tiny SVG/icon is
// trustworthy.
const minBareDecodedLen = 64

// base64ImageDecode reports whether a response body is a base64-encoded image
// and, if so, returns the decoded bytes plus how it was encoded.
//
//   - A `data:<media>;base64,<payload>` URI is unambiguous → base64DataURI,
//     accepted when the media type is image/* OR the decoded payload has image
//     magic (so an SVG data URI, which has no binary magic, still works).
//   - A body that is *entirely* bare base64 and decodes to recognised image
//     magic bytes → base64Bare. The magic-byte gate is the false-positive guard:
//     a JWT or random token won't decode to image magic.
//   - Anything else → base64None, ok=false.
//
// It is pure and std-lib only; contentType is currently unused but kept in the
// signature so the caller can pass the response's media type without a churn if
// future heuristics want it.
func base64ImageDecode(contentType string, body []byte) (decoded []byte, source base64Source, ok bool) {
	_ = contentType

	trimmed := bytes.TrimSpace(body)
	// Tolerate a body that is a JSON string value: strip one layer of "…".
	if len(trimmed) >= 2 && trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		trimmed = trimmed[1 : len(trimmed)-1]
	}
	if len(trimmed) == 0 {
		return nil, base64None, false
	}

	// 1. Data URI — unambiguous.
	if dec, ok := decodeDataURI(trimmed); ok {
		return dec, base64DataURI, true
	}

	// 2. Bare base64 of an image — ambiguous.
	if dec, ok := decodeBareBase64Image(trimmed); ok {
		return dec, base64Bare, true
	}

	return nil, base64None, false
}

// decodeDataURI handles `data:<mediatype>;base64,<payload>` (case-insensitive
// scheme/marker). It accepts when the media type is image/* OR the decoded
// payload passes hasImageMagic.
func decodeDataURI(trimmed []byte) (decoded []byte, ok bool) {
	// Need a case-insensitive "data:" prefix and a ";base64," marker.
	const scheme = "data:"
	if len(trimmed) < len(scheme) || !strings.EqualFold(string(trimmed[:len(scheme)]), scheme) {
		return nil, false
	}

	// Find ";base64," case-insensitively. The header (between "data:" and the
	// first ";") is short, so a lower-cased scan of a bounded prefix is fine; to
	// stay simple and robust we lower-case the whole header region only up to the
	// marker by searching on a lower-cased copy of the leading bytes.
	lower := strings.ToLower(string(trimmed))
	marker := ";base64,"
	mi := strings.Index(lower, marker)
	if mi < 0 || mi < len(scheme) {
		return nil, false
	}

	mediaType := strings.TrimSpace(string(trimmed[len(scheme):mi]))
	payload := string(trimmed[mi+len(marker):])
	payload = stripWhitespace(payload)
	if payload == "" {
		return nil, false
	}

	dec, decodeOK := decodeBase64StdOrRaw(payload)
	if !decodeOK {
		return nil, false
	}

	if isImageMediaType(mediaType) || hasImageMagic(dec) {
		return dec, true
	}
	return nil, false
}

// isImageMediaType reports whether a data-URI media type is image/* (e.g.
// "image/png", "image/svg+xml"). Parameters such as ";charset=…" never reach
// here (we split on the ";base64," marker), but we parse defensively anyway.
func isImageMediaType(mediaType string) bool {
	if mediaType == "" {
		return false
	}
	if mt, _, err := mime.ParseMediaType(mediaType); err == nil {
		mediaType = mt
	}
	return strings.HasPrefix(strings.ToLower(mediaType), "image/")
}

// decodeBareBase64Image handles a body that is *entirely* base64 (commonly
// line-wrapped) and decodes to image magic bytes. The "only base64 chars" check
// rejects normal prose/JSON, and the hasImageMagic + minimum-length gates reject
// JWTs and random tokens.
func decodeBareBase64Image(trimmed []byte) (decoded []byte, ok bool) {
	s := stripWhitespace(string(trimmed))
	// Non-trivially long: too short and we can't meet minBareDecodedLen anyway,
	// and short tokens are the most likely coincidental matches. Base64 expands
	// 3 bytes → 4 chars, so the encoded form must be at least ~4/3 * the minimum.
	if len(s) < minBareDecodedLen*4/3 {
		return nil, false
	}
	if !isAllBase64(s) {
		return nil, false
	}

	dec, decodeOK := decodeBase64Any(s)
	if !decodeOK {
		return nil, false
	}
	if len(dec) < minBareDecodedLen {
		return nil, false
	}
	if !hasImageMagic(dec) {
		return nil, false
	}
	return dec, true
}

// isAllBase64 reports whether s consists only of standard- or URL-safe base64
// alphabet characters plus '=' padding. (Whitespace must already be removed.)
func isAllBase64(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '+' || c == '/': // std alphabet
		case c == '-' || c == '_': // URL-safe alphabet
		case c == '=': // padding
		default:
			return false
		}
	}
	return true
}

// decodeBase64StdOrRaw tries standard padded base64, then unpadded raw — the two
// variants seen in data-URI payloads.
func decodeBase64StdOrRaw(s string) (decoded []byte, ok bool) {
	if dec, err := base64.StdEncoding.DecodeString(s); err == nil {
		return dec, true
	}
	if dec, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return dec, true
	}
	return nil, false
}

// decodeBase64Any tries every common base64 variant: standard padded/raw and
// URL-safe padded/raw. The URL-safe variants let a body that uses '-'/'_'
// instead of '+'/'/' still decode.
func decodeBase64Any(s string) (decoded []byte, ok bool) {
	if dec, ok := decodeBase64StdOrRaw(s); ok {
		return dec, true
	}
	if dec, err := base64.URLEncoding.DecodeString(s); err == nil {
		return dec, true
	}
	if dec, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return dec, true
	}
	return nil, false
}

// stripWhitespace removes ASCII whitespace (space, tab, CR, LF, form feed,
// vertical tab) from s. Base64 payloads are routinely line-wrapped, and the
// decoders reject embedded whitespace, so we strip it before decoding.
func stripWhitespace(s string) string {
	if !strings.ContainsAny(s, " \t\r\n\f\v") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\r', '\n', '\f', '\v':
			// skip
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
