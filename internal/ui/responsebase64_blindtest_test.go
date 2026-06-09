package ui

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// Blind tests for the detection-lane contract (responsebase64.go):
//
//	func base64ImageDecode(contentType string, body []byte) (decoded []byte, source base64Source, ok bool)
//	source ∈ { base64None(0), base64DataURI, base64Bare }
//
// Written from the CONTRACT only. Real image bytes come from the shared
// devEncodePNG / devEncodeJPEG / devEncodeGIF helpers (responsekind_test.go).
// Fixtures here are prefixed b64BT to stay unique.

// b64BTwrap line-wraps s into 64-char lines joined by "\n" (how base64 is
// commonly emitted, e.g. PEM/MIME bodies).
func b64BTwrap(s string) string {
	const width = 64
	var lines []string
	for len(s) > width {
		lines = append(lines, s[:width])
		s = s[width:]
	}
	lines = append(lines, s)
	return strings.Join(lines, "\n")
}

// 1. data: URI carrying a PNG → base64DataURI, decoded round-trips.
func TestBase64ImageDecode_DataURIPNG(t *testing.T) {
	png := devEncodePNG(t, 4, 4)
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)

	decoded, source, ok := base64ImageDecode("", []byte(dataURI))
	if !ok {
		t.Fatalf("ok = false, want true for data:image/png URI")
	}
	if source != base64DataURI {
		t.Fatalf("source = %v, want base64DataURI", source)
	}
	if !bytes.Equal(decoded, png) {
		t.Fatalf("decoded (%d bytes) does not round-trip to original PNG (%d bytes)", len(decoded), len(png))
	}
}

// 2. Bare whole-body base64 of a PNG → base64Bare, decoded round-trips.
func TestBase64ImageDecode_BareBase64PNG(t *testing.T) {
	png := devEncodePNG(t, 5, 3)
	bare := base64.StdEncoding.EncodeToString(png)

	decoded, source, ok := base64ImageDecode("", []byte(bare))
	if !ok {
		t.Fatalf("ok = false, want true for bare base64 PNG")
	}
	if source != base64Bare {
		t.Fatalf("source = %v, want base64Bare", source)
	}
	if !bytes.Equal(decoded, png) {
		t.Fatalf("decoded (%d bytes) does not round-trip to original PNG (%d bytes)", len(decoded), len(png))
	}
}

//  3. Whitespace/line-wrapped bare base64 (64-char lines + surrounding spaces)
//     is still detected as base64Bare and decodes correctly.
func TestBase64ImageDecode_WhitespaceWrapped(t *testing.T) {
	png := devEncodePNG(t, 6, 6)
	wrapped := "  \n" + b64BTwrap(base64.StdEncoding.EncodeToString(png)) + "\n  "

	decoded, source, ok := base64ImageDecode("", []byte(wrapped))
	if !ok {
		t.Fatalf("ok = false, want true for whitespace-wrapped base64 PNG")
	}
	if source != base64Bare {
		t.Fatalf("source = %v, want base64Bare", source)
	}
	if !bytes.Equal(decoded, png) {
		t.Fatalf("decoded (%d bytes) does not round-trip to original PNG (%d bytes)", len(decoded), len(png))
	}
}

// 4. Negatives: things that are NOT verifiable base64 images → (nil, base64None, false).
func TestBase64ImageDecode_NotAnImage(t *testing.T) {
	// A realistic 3-part dotted JWT — the dots are not valid std base64, and
	// the decoded segments are JSON/signature, never image magic.
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
		"eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IllvbiIsImlhdCI6MTUxNjIzOTAyMn0." +
		"SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"

	// Valid base64 that decodes to plain text LONGER than the bare-path length
	// floor — so it must be rejected by the magic-byte gate, not merely for being
	// too short.
	plainTextB64 := base64.StdEncoding.EncodeToString(
		[]byte(strings.Repeat("hello world not an image, ", 8)))

	cases := []struct {
		name string
		ct   string
		body []byte
	}{
		{"jwt", "", []byte(jwt)},
		{"base64-of-plain-text", "", []byte(plainTextB64)},
		{"plain-json", "application/json", []byte(`{"a":1}`)},
		{"empty", "", []byte("")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoded, source, ok := base64ImageDecode(tc.ct, tc.body)
			if ok {
				t.Fatalf("ok = true, want false for %q", tc.name)
			}
			if source != base64None {
				t.Fatalf("source = %v, want base64None for %q", source, tc.name)
			}
			if decoded != nil {
				t.Fatalf("decoded = %d bytes, want nil for %q", len(decoded), tc.name)
			}
		})
	}
}

// 5. data: URIs for JPEG and GIF decode with base64DataURI and round-trip.
func TestBase64ImageDecode_DataURIJpegGif(t *testing.T) {
	jpg := devEncodeJPEG(t, 8, 8)
	gif := devEncodeGIF(t, 8, 8)

	cases := []struct {
		name    string
		dataURI string
		want    []byte
	}{
		{"jpeg", "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpg), jpg},
		{"gif", "data:image/gif;base64," + base64.StdEncoding.EncodeToString(gif), gif},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoded, source, ok := base64ImageDecode("", []byte(tc.dataURI))
			if !ok {
				t.Fatalf("ok = false, want true for %s data URI", tc.name)
			}
			if source != base64DataURI {
				t.Fatalf("source = %v, want base64DataURI for %s", source, tc.name)
			}
			if !bytes.Equal(decoded, tc.want) {
				t.Fatalf("decoded does not round-trip to original %s bytes", tc.name)
			}
		})
	}
}

//  6. (lenient) A data: URI wrapped in JSON-string double quotes is still
//     detected. Kept lenient: this pins quote-stripping if the contract does it.
func TestBase64ImageDecode_QuotedJSONValue(t *testing.T) {
	png := devEncodePNG(t, 4, 4)
	quoted := `"data:image/png;base64,` + base64.StdEncoding.EncodeToString(png) + `"`

	decoded, source, ok := base64ImageDecode("", []byte(quoted))
	if !ok {
		t.Fatalf("ok = false, want true for quoted data URI")
	}
	if source != base64DataURI {
		t.Fatalf("source = %v, want base64DataURI", source)
	}
	if !bytes.Equal(decoded, png) {
		t.Fatalf("decoded does not round-trip to original PNG")
	}
}
