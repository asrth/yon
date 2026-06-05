package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

// Dev A's own tests for the pure body classifier (responsekind.go). Symbols:
//   - bodyKind constants bodyKindText / bodyKindImage / bodyKindPDF
//   - classifyBody(contentType string, body []byte) bodyKind
//   - imageDimensions(body []byte) (w, h int, ok bool)
//   - bodyKindString(bodyKind) string

// devEncodePNG builds a w×h PNG and returns its bytes.
func devEncodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func devEncodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

func devEncodeGIF(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, w, h), []color.Color{color.Black, color.White})
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatalf("gif.Encode: %v", err)
	}
	return buf.Bytes()
}

// --- classifyBody: by Content-Type ---

func TestClassifyBody_ImageByContentType(t *testing.T) {
	cases := []string{
		"image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp",
		"IMAGE/PNG", "image/svg+xml",
		"image/png; charset=binary", // params stripped
	}
	for _, ct := range cases {
		if got := classifyBody(ct, []byte("not really an image")); got != bodyKindImage {
			t.Errorf("classifyBody(%q, …) = %v, want image", ct, got)
		}
	}
}

func TestClassifyBody_PDFByContentType(t *testing.T) {
	for _, ct := range []string{"application/pdf", "application/x-pdf", "APPLICATION/PDF"} {
		if got := classifyBody(ct, []byte("garbage")); got != bodyKindPDF {
			t.Errorf("classifyBody(%q, …) = %v, want pdf", ct, got)
		}
	}
}

// --- classifyBody: by magic bytes with a generic Content-Type ---

func TestClassifyBody_ImageByMagicBytes_GenericType(t *testing.T) {
	cases := map[string][]byte{
		"png":  devEncodePNG(t, 3, 5),
		"jpeg": devEncodeJPEG(t, 4, 4),
		"gif":  devEncodeGIF(t, 2, 2),
		"bmp":  append([]byte("BM"), make([]byte, 64)...),
		"webp": append([]byte("RIFF\x00\x00\x00\x00WEBP"), make([]byte, 8)...),
	}
	for _, ct := range []string{"", "application/octet-stream"} {
		for name, body := range cases {
			if got := classifyBody(ct, body); got != bodyKindImage {
				t.Errorf("classifyBody(%q, %s) = %v, want image", ct, name, got)
			}
		}
	}
}

func TestClassifyBody_PDFByMagicBytes_GenericType(t *testing.T) {
	body := []byte("%PDF-1.7\n%âãÏÓ\n1 0 obj\n")
	for _, ct := range []string{"", "application/octet-stream"} {
		if got := classifyBody(ct, body); got != bodyKindPDF {
			t.Errorf("classifyBody(%q, pdf) = %v, want pdf", ct, got)
		}
	}
}

// --- classifyBody: text / JSON / empty ---

func TestClassifyBody_TextAndJSON(t *testing.T) {
	cases := []struct {
		ct   string
		body string
	}{
		{"text/plain", "hello world"},
		{"application/json", `{"a":1,"b":[2,3]}`},
		{"application/xml", "<root><a>1</a></root>"},
		{"text/html", "<!doctype html><html></html>"},
		{"", "just some text that isn't an image or pdf"},
	}
	for _, c := range cases {
		if got := classifyBody(c.ct, []byte(c.body)); got != bodyKindText {
			t.Errorf("classifyBody(%q, %q) = %v, want text", c.ct, c.body, got)
		}
	}
}

func TestClassifyBody_EmptyBody(t *testing.T) {
	for _, ct := range []string{"", "application/octet-stream", "text/plain"} {
		if got := classifyBody(ct, nil); got != bodyKindText {
			t.Errorf("classifyBody(%q, nil) = %v, want text", ct, got)
		}
		if got := classifyBody(ct, []byte{}); got != bodyKindText {
			t.Errorf("classifyBody(%q, []) = %v, want text", ct, got)
		}
	}
}

// --- classifyBody: content-type / body mismatch (magic bytes win on generic type) ---

func TestClassifyBody_MagicWinsOverGenericType(t *testing.T) {
	pngBody := devEncodePNG(t, 3, 5)
	if got := classifyBody("application/octet-stream", pngBody); got != bodyKindImage {
		t.Errorf("octet-stream + PNG magic = %v, want image", got)
	}

	pdf := []byte("%PDF-1.4 ...")
	if got := classifyBody("application/octet-stream", pdf); got != bodyKindPDF {
		t.Errorf("octet-stream + PDF magic = %v, want pdf", got)
	}

	// An explicit image/* Content-Type wins even when the body is plainly text.
	if got := classifyBody("image/png", []byte("plain text, not an image")); got != bodyKindImage {
		t.Errorf("image/png + text body = %v, want image (explicit type wins)", got)
	}
}

// --- classifyBody: an explicit textual type is trusted, never magic-sniffed ---

// TestClassifyBody_ExplicitTextNeverSniffed pins the BMP-prefix regression: a
// real text body that happens to start with an image/PDF magic prefix ("BM",
// "GIF8", "%PDF-") must stay text whenever the server gives an explicit, non
// generic Content-Type. Magic sniffing is only a fallback for generic/absent
// types — see isGenericBinaryType.
func TestClassifyBody_ExplicitTextNeverSniffed(t *testing.T) {
	cases := []struct {
		ct   string
		body string
	}{
		{"text/plain", "BMW recall notice: please return your vehicle."}, // "BM" prefix
		{"text/plain", "BM"}, // bare BMP prefix
		{"text/html", "<p>GIF89a is a format</p>"},
		{"text/plain", "%PDF- is the PDF signature, but this is prose"},
		{"application/json", `{"note":"BMW"}`},
		{"text/csv", "BM,model,year\nX5,2024"},
		{"text/markdown", "BM heading"},
	}
	for _, c := range cases {
		if got := classifyBody(c.ct, []byte(c.body)); got != bodyKindText {
			t.Errorf("classifyBody(%q, %q) = %v, want text (explicit type must not be sniffed)",
				c.ct, c.body, got)
		}
	}
}

// TestClassifyBody_GenericTypeStillSniffs guards the other side: a generic or
// absent Content-Type must STILL resolve a real image/PDF via magic bytes, so
// the fix above doesn't over-correct and break mislabelled binaries.
func TestClassifyBody_GenericTypeStillSniffs(t *testing.T) {
	png := devEncodePNG(t, 3, 5)
	pdf := []byte("%PDF-1.7\n%âãÏÓ\n")
	for _, ct := range []string{"", "application/octet-stream", "application/octetstream", "application/binary", "binary/octet-stream", "application/x-download", "application/force-download"} {
		if got := classifyBody(ct, png); got != bodyKindImage {
			t.Errorf("classifyBody(%q, PNG) = %v, want image", ct, got)
		}
		if got := classifyBody(ct, pdf); got != bodyKindPDF {
			t.Errorf("classifyBody(%q, PDF) = %v, want pdf", ct, got)
		}
	}
}

// --- imageDimensions: correct WxH for std-lib formats, ok=false otherwise ---

func TestImageDimensions_PNGJPEGGIF(t *testing.T) {
	cases := []struct {
		name         string
		body         []byte
		wantW, wantH int
	}{
		{"png", devEncodePNG(t, 3, 5), 3, 5},
		{"jpeg", devEncodeJPEG(t, 8, 6), 8, 6},
		{"gif", devEncodeGIF(t, 4, 7), 4, 7},
	}
	for _, c := range cases {
		w, h, ok := imageDimensions(c.body)
		if !ok {
			t.Errorf("imageDimensions(%s) ok=false, want true", c.name)
			continue
		}
		if w != c.wantW || h != c.wantH {
			t.Errorf("imageDimensions(%s) = %dx%d, want %dx%d", c.name, w, h, c.wantW, c.wantH)
		}
	}
}

func TestImageDimensions_NonImage(t *testing.T) {
	cases := map[string][]byte{
		"text":      []byte("not an image at all"),
		"empty":     nil,
		"pdf":       []byte("%PDF-1.7\n"),
		"truncated": devEncodePNG(t, 3, 5)[:8], // signature only, no header
	}
	for name, body := range cases {
		if _, _, ok := imageDimensions(body); ok {
			t.Errorf("imageDimensions(%s) ok=true, want false", name)
		}
	}
}

func TestBodyKindString(t *testing.T) {
	cases := map[bodyKind]string{
		bodyKindText:  "text",
		bodyKindImage: "image",
		bodyKindPDF:   "pdf",
		bodyKind(999): "text",
	}
	for k, want := range cases {
		if got := bodyKindString(k); got != want {
			t.Errorf("bodyKindString(%d) = %q, want %q", int(k), got, want)
		}
	}
}
