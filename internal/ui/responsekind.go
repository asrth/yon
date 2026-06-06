package ui

import (
	"bytes"
	"image"
	"net/http"
	"strings"

	// Side-effect registration so image.DecodeConfig can read the dimensions of
	// PNG, JPEG and GIF bodies without fully decoding them. WebP and BMP are not
	// registered by the std lib; imageDimensions returns ok=false for those (the
	// on-screen preview still works via Fyne's own decoders).
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// classifyBody categorises a response body by *what it is* — text, image or PDF.
// This is a different axis from the syntax-colouring values of bodyKind defined
// in responseview.go (kindText/kindJSON/kindXML/kindHTML), which pick a
// highlighter for textual bodies. The three category constants below reuse the
// bodyKind type but live in a distinct value range so they never collide with
// the colouring kinds, and so the caller can dispatch text-vs-image-vs-PDF.
const (
	bodyKindText  bodyKind = 100 + iota // textual body: the existing text viewer
	bodyKindImage                       // image body: show an image preview
	bodyKindPDF                         // PDF body: show a PDF preview
)

// bodyKindString renders a category for logs/tests.
func bodyKindString(k bodyKind) string {
	switch k {
	case bodyKindImage:
		return "image"
	case bodyKindPDF:
		return "pdf"
	default:
		return "text"
	}
}

// classifyBody decides whether a response body should be shown as an image, a
// PDF, or text. contentType is the lower-cased media type with params stripped
// (as setResponse derives via contentTypeOf); body is the full response body.
//
// Detection is deliberately tolerant but anchored on the server's word: a precise
// Content-Type always wins, and magic-byte sniffing is a fallback used ONLY when
// the Content-Type is absent or a generic "unknown binary" type (e.g.
// "application/octet-stream"). An explicit textual or structured type — text/*,
// application/json, application/xml, … — is trusted as-is and never sniffed, so a
// genuine text body that merely starts with "BM", "GIF8" or "%PDF-" is not
// misread as an image or PDF. An empty body is always text.
func classifyBody(contentType string, body []byte) bodyKind {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	// Strip any "; charset=…" parameter so "image/png; charset=binary" matches.
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}

	// 1. Authoritative Content-Type.
	if strings.HasPrefix(ct, "image/") {
		return bodyKindImage
	}
	if ct == "application/pdf" || ct == "application/x-pdf" {
		return bodyKindPDF
	}

	// 2. Magic-byte sniffing is a fallback ONLY for an absent or generic-binary
	// Content-Type — a server that mislabels an image or PDF as octet-stream (or
	// sends no type) is still previewed, while an explicit non-binary type stays
	// text. This is what keeps a "BMW…" text/plain body from rendering as a (BMP)
	// image.
	if !isGenericBinaryType(ct) {
		return bodyKindText
	}

	if len(body) == 0 {
		return bodyKindText
	}

	if hasPDFMagic(body) {
		return bodyKindPDF
	}
	if hasImageMagic(body) {
		return bodyKindImage
	}

	// 3. http.DetectContentType cross-check (handles image signatures we don't
	// special-case, and confirms the sniff). hasPDFMagic already handled PDF
	// above, so this only adds image coverage.
	if strings.HasPrefix(http.DetectContentType(body), "image/") {
		return bodyKindImage
	}

	return bodyKindText
}

// isGenericBinaryType reports whether ct carries no useful hint about the body's
// format — it is empty or one of the generic "unknown bytes" media types servers
// use when they don't know (or won't say) what they're sending. These are the
// only Content-Types for which classifyBody falls back to magic-byte sniffing;
// any explicit type (text/*, application/json, image/*, application/pdf, …) is
// trusted directly.
func isGenericBinaryType(ct string) bool {
	switch ct {
	case "",
		"application/octet-stream",
		"application/octetstream", // hyphen-less variant seen in the wild
		"application/binary",
		"binary/octet-stream",
		"application/unknown",
		"application/download",
		"application/x-download",
		"application/force-download":
		return true
	}
	return false
}

// hasPDFMagic reports whether body starts with the PDF signature "%PDF-".
func hasPDFMagic(body []byte) bool {
	return bytes.HasPrefix(body, []byte("%PDF-"))
}

// hasImageMagic reports whether body begins with the signature of a common
// image format: PNG, JPEG, GIF, BMP or WebP.
func hasImageMagic(body []byte) bool {
	switch {
	case bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")):
		return true
	case bytes.HasPrefix(body, []byte("\xFF\xD8\xFF")):
		return true
	case bytes.HasPrefix(body, []byte("GIF87a")), bytes.HasPrefix(body, []byte("GIF89a")):
		return true
	case bytes.HasPrefix(body, []byte("BM")):
		return true
	case len(body) >= 12 &&
		bytes.HasPrefix(body, []byte("RIFF")) &&
		bytes.Equal(body[8:12], []byte("WEBP")):
		return true
	}
	return false
}

// responseDefaultFilename returns the suggested save-as filename for a response
// body, picked from what the body actually is: "response.<ext>" for an image
// (the extension matching the concrete image format), "response.pdf" for a PDF,
// and "response.txt" for everything else. It is pure + Fyne-free so both the
// save dialogs and unit tests can call it directly.
func responseDefaultFilename(contentType string, body []byte) string {
	switch classifyBody(contentType, body) {
	case bodyKindImage:
		return "response." + imageExtension(contentType, body)
	case bodyKindPDF:
		return "response.pdf"
	default:
		return "response.txt"
	}
}

// imageExtension returns the bare file extension (no dot) for an image body —
// "png", "jpg", "gif", "webp" or "bmp". It prefers the Content-Type subtype
// (image/jpeg → jpg, image/png → png, …) and falls back to magic-byte sniffing
// when the type is generic/absent or an image subtype with no obvious mapping
// (e.g. image/svg+xml, which has no magic). An unrecognisable image defaults to
// "png". This is only meaningful for bodies classifyBody reports as
// bodyKindImage; callers gate on that.
func imageExtension(contentType string, body []byte) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}

	// 1. Map a concrete image/* subtype.
	if strings.HasPrefix(ct, "image/") {
		switch ct {
		case "image/png":
			return "png"
		case "image/jpeg", "image/jpg":
			return "jpg"
		case "image/gif":
			return "gif"
		case "image/webp":
			return "webp"
		case "image/bmp", "image/x-bmp", "image/x-ms-bmp":
			return "bmp"
		case "image/svg+xml":
			return "svg"
		case "image/tiff":
			return "tif"
		case "image/avif":
			return "avif"
		case "image/x-icon", "image/vnd.microsoft.icon":
			return "ico"
		}
		// An image/* subtype we still don't map: fall through to magic, then to
		// the png default.
	}

	// 2. Sniff the magic bytes (covers a generic/absent type or an unmapped
	// subtype that still carries a known signature).
	switch {
	case bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case bytes.HasPrefix(body, []byte("\xFF\xD8\xFF")):
		return "jpg"
	case bytes.HasPrefix(body, []byte("GIF87a")), bytes.HasPrefix(body, []byte("GIF89a")):
		return "gif"
	case len(body) >= 12 &&
		bytes.HasPrefix(body, []byte("RIFF")) &&
		bytes.Equal(body[8:12], []byte("WEBP")):
		return "webp"
	case bytes.HasPrefix(body, []byte("BM")):
		return "bmp"
	}

	// 3. Unknown image subtype with no recognisable magic (e.g. SVG): default png.
	return "png"
}

// imageDimensions returns the pixel width and height of an image body without
// fully decoding it, using image.DecodeConfig with the registered PNG/JPEG/GIF
// decoders. ok is false when the body is not a decodable image — including WebP
// and BMP, which the std lib does not register (the Fyne preview still displays
// those; only the WxH label is omitted).
func imageDimensions(body []byte) (w, h int, ok bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}
