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

// Blind tests for the pure body classifier (responsekind.go).
// Written from the contract only — independent of the implementation.
//
// Contract symbols exercised here:
//   - type bodyKind with category constants bodyKindText, bodyKindImage, bodyKindPDF
//   - func classifyBody(contentType string, body []byte) bodyKind
//   - func imageDimensions(body []byte) (w, h int, ok bool)
//
// Helper names are bt-prefixed to avoid colliding with the sibling test
// fixtures in responsekind_test.go.

// --- fixture helpers -------------------------------------------------------

// btPNG encodes a w×h opaque image as PNG.
func btPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 13), G: uint8(y * 7), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// btJPEG encodes a w×h image as JPEG.
func btJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: uint8(x * 5), B: uint8(y * 9), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

// btGIF encodes a w×h image as GIF.
func btGIF(t *testing.T, w, h int) []byte {
	t.Helper()
	pal := color.Palette{color.Black, color.White, color.RGBA{R: 255, A: 255}}
	img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetColorIndex(x, y, uint8((x+y)%len(pal)))
		}
	}
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, &gif.Options{NumColors: len(pal)}); err != nil {
		t.Fatalf("gif.Encode: %v", err)
	}
	return buf.Bytes()
}

// --- classifyBody ----------------------------------------------------------

func TestClassifyBody(t *testing.T) {
	realPNG := btPNG(t, 2, 2)
	realJPEG := btJPEG(t, 2, 2)
	realGIF := btGIF(t, 2, 2)

	cases := []struct {
		name        string
		contentType string
		body        []byte
		want        bodyKind
	}{
		// content-type image/* always wins, regardless of body.
		{"png type, junk body", "image/png", []byte("not really an image"), bodyKindImage},
		{"jpeg type, junk body", "image/jpeg", []byte("xx"), bodyKindImage},
		{"gif type, junk body", "image/gif", []byte("xx"), bodyKindImage},
		{"webp type, junk body", "image/webp", []byte("xx"), bodyKindImage},
		{"image type with params", "image/png; charset=binary", []byte("xx"), bodyKindImage},

		// Magic bytes win under a generic / empty content-type (the core bug).
		{"octet-stream + real PNG", "application/octet-stream", realPNG, bodyKindImage},
		{"empty type + real PNG", "", realPNG, bodyKindImage},
		{"octet-stream + real JPEG", "application/octet-stream", realJPEG, bodyKindImage},
		{"empty type + real JPEG", "", realJPEG, bodyKindImage},
		{"octet-stream + real GIF", "application/octet-stream", realGIF, bodyKindImage},
		{"empty type + real GIF", "", realGIF, bodyKindImage},

		// PDF: by content-type, and by magic bytes under generic type.
		{"pdf type, junk body", "application/pdf", []byte("anything at all"), bodyKindPDF},
		{"pdf type, empty body", "application/pdf", nil, bodyKindPDF},
		{"octet-stream + %PDF- body", "application/octet-stream", []byte("%PDF-1.5\n1 0 obj\n"), bodyKindPDF},
		{"empty type + %PDF- body", "", []byte("%PDF-1.7\n%âãÏÓ\n"), bodyKindPDF},

		// Text fallbacks.
		{"json type", "application/json", []byte("{}"), bodyKindText},
		{"html type", "text/html", []byte("<html></html>"), bodyKindText},
		{"plain type", "text/plain", []byte("hello"), bodyKindText},
		{"empty type + empty body", "", nil, bodyKindText},
		{"empty type + empty slice", "", []byte{}, bodyKindText},
		{"octet-stream + plain text", "application/octet-stream", []byte("just words here"), bodyKindText},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyBody(tc.contentType, tc.body); got != tc.want {
				t.Errorf("classifyBody(%q, %d bytes) = %v, want %v",
					tc.contentType, len(tc.body), got, tc.want)
			}
		})
	}
}

// The three kinds must be distinct values so the caller can dispatch on them.
func TestBodyCategoryConstantsDistinct(t *testing.T) {
	if bodyKindText == bodyKindImage || bodyKindText == bodyKindPDF || bodyKindImage == bodyKindPDF {
		t.Fatalf("bodyKind category constants must be distinct: text=%v image=%v pdf=%v",
			bodyKindText, bodyKindImage, bodyKindPDF)
	}
}

// --- imageDimensions -------------------------------------------------------

func TestImageDimensions(t *testing.T) {
	t.Run("png", func(t *testing.T) {
		w, h, ok := imageDimensions(btPNG(t, 7, 3))
		if !ok || w != 7 || h != 3 {
			t.Errorf("png: got (%d,%d,%v), want (7,3,true)", w, h, ok)
		}
	})

	t.Run("jpeg", func(t *testing.T) {
		w, h, ok := imageDimensions(btJPEG(t, 12, 5))
		if !ok || w != 12 || h != 5 {
			t.Errorf("jpeg: got (%d,%d,%v), want (12,5,true)", w, h, ok)
		}
	})

	t.Run("gif", func(t *testing.T) {
		w, h, ok := imageDimensions(btGIF(t, 4, 9))
		if !ok || w != 4 || h != 9 {
			t.Errorf("gif: got (%d,%d,%v), want (4,9,true)", w, h, ok)
		}
	})

	t.Run("non-image bytes", func(t *testing.T) {
		if w, h, ok := imageDimensions([]byte("not an image")); ok {
			t.Errorf("non-image: got (%d,%d,%v), want ok=false", w, h, ok)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		if w, h, ok := imageDimensions(nil); ok {
			t.Errorf("empty: got (%d,%d,%v), want ok=false", w, h, ok)
		}
	})
}
