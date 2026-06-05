package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/ultramcu/yon/internal/model"
)

// Blind Test author (Dev-Rabbit) for issue #16: image / PDF response previews in
// the responseView. These tests are written from the CONTRACT only, not the
// implementation:
//
//   - setResponse with an image body (Content-Type image/* or image magic bytes)
//     shows an image preview — a *canvas.Image — in the body region, and the
//     text viewers are no longer the active/visible body content.
//   - setResponse with application/pdf shows a PDF panel carrying a Save button
//     (or a label mentioning PDF).
//   - a normal text/JSON response keeps the existing text rendering with NO
//     *canvas.Image present.
//   - switching from an image response to a text response resets the image view
//     (no stale *canvas.Image) and renders the text.
//
// Contract symbols touched: newResponseView, responseView.setResponse,
// model.Response{Status,StatusText,Headers,Body,Duration,Size}, model.Param.
// The shared walkObjects tree-walker (sidebar_tap_routing_test.go) is reused.

// newImageBlindRV builds a fresh responseView on the headless test driver, the
// same construction path the other responseView tests use.
func newImageBlindRV(t *testing.T) *responseView {
	t.Helper()
	test.NewApp()
	w := test.NewWindow(nil)
	t.Cleanup(w.Close)
	return newResponseView(w)
}

// tinyPNG returns a freshly-encoded PNG of the given size — a real image with a
// valid PNG signature (magic bytes 0x89 P N G ...).
func tinyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x * 8), G: uint8(y * 16), B: 0x40, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// visibleImages returns every *canvas.Image present AND visible in root's
// rendered tree. These probes scope to rv.bodyStack (the Body region), not the
// whole responseView: the response toolbar's icon buttons (Copy / Save / Pop-out)
// each render a *canvas.Image, so the only *canvas.Image inside bodyStack is a
// genuine image preview — that is what makes "is an image previewed?" answerable
// by scanning for a canvas.Image.
func visibleImages(root fyne.CanvasObject) []*canvas.Image {
	var imgs []*canvas.Image
	walkObjects(fyne.CurrentApp(), root, func(o fyne.CanvasObject) {
		if im, ok := o.(*canvas.Image); ok && im.Visible() {
			imgs = append(imgs, im)
		}
	})
	return imgs
}

// anyImagePresent reports whether a *canvas.Image exists anywhere in root's
// rendered tree, regardless of visibility (catches a stale-but-hidden preview).
func anyImagePresent(root fyne.CanvasObject) bool {
	found := false
	walkObjects(fyne.CurrentApp(), root, func(o fyne.CanvasObject) {
		if _, ok := o.(*canvas.Image); ok {
			found = true
		}
	})
	return found
}

// visibleTextOf collects the text of every visible textual object in root: the
// canvas.Text, widget.Label and widget.Button captions, plus the joined content
// of any visible widget.TextGrid. Used to find PDF labels / Save buttons and to
// confirm the text path rendered the body.
func visibleTextOf(root fyne.CanvasObject) string {
	var b strings.Builder
	walkObjects(fyne.CurrentApp(), root, func(o fyne.CanvasObject) {
		if !o.Visible() {
			return
		}
		switch v := o.(type) {
		case *canvas.Text:
			b.WriteString(v.Text)
			b.WriteByte('\n')
		case *widget.Label:
			b.WriteString(v.Text)
			b.WriteByte('\n')
		case *widget.Button:
			b.WriteString(v.Text)
			b.WriteByte('\n')
		case *widget.TextGrid:
			b.WriteString(v.Text())
			b.WriteByte('\n')
		}
	})
	return b.String()
}

// hasVisibleSaveButtonOrPDFLabel reports whether the visible body region carries
// either a *widget.Button whose Text contains "Save" (the icon-only response-bar
// Save button has an empty Text, so this matches a captioned PDF Save button) or
// any visible textual object mentioning "PDF".
func hasVisibleSaveButtonOrPDFLabel(root fyne.CanvasObject) bool {
	found := false
	walkObjects(fyne.CurrentApp(), root, func(o fyne.CanvasObject) {
		if !o.Visible() {
			return
		}
		if btn, ok := o.(*widget.Button); ok && strings.Contains(btn.Text, "Save") {
			found = true
		}
	})
	if found {
		return true
	}
	return strings.Contains(strings.ToUpper(visibleTextOf(root)), "PDF")
}

func ct(value string) []model.Param {
	return []model.Param{{Key: "Content-Type", Value: value, Enabled: true}}
}

// TestSetResponse_ImageShowsImageView pins: an image response (Content-Type
// image/png, a real PNG body) shows a *canvas.Image preview in the body region,
// and the body text grid is NOT the active rendered content for the image.
func TestSetResponse_ImageShowsImageView(t *testing.T) {
	rv := newImageBlindRV(t)
	pngBytes := tinyPNG(t, 8, 4)

	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("image/png"),
		Body:       pngBytes,
		Size:       int64(len(pngBytes)),
	})

	imgs := visibleImages(rv.bodyStack)
	if len(imgs) == 0 {
		t.Fatalf("image response: expected a visible *canvas.Image preview in the body, found none")
	}

	// The text path must not be the active body view: the body TextGrid must not
	// be showing the raw PNG bytes as text.
	bodyText := rv.bodyGrid.Text()
	if strings.Contains(bodyText, "PNG") || bytes.Contains([]byte(bodyText), pngBytes[:8]) {
		t.Errorf("image response: body TextGrid still rendering raw PNG bytes as text:\n%q", bodyText)
	}
}

// TestSetResponse_TextHasNoImageView pins: a JSON response keeps the text path
// and shows NO *canvas.Image anywhere in the response view.
func TestSetResponse_TextHasNoImageView(t *testing.T) {
	rv := newImageBlindRV(t)
	body := []byte(`{"a":1}`)

	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("application/json"),
		Body:       body,
		Size:       int64(len(body)),
	})

	if anyImagePresent(rv.bodyStack) {
		t.Errorf("JSON response: unexpected *canvas.Image present in the response view")
	}

	// The text path rendered the body (Pretty JSON keeps the "a" key / "1" value).
	got := visibleTextOf(rv.bodyStack)
	if !strings.Contains(got, "\"a\"") || !strings.Contains(got, "1") {
		t.Errorf("JSON response: body text not rendered; visible text =\n%s", got)
	}
}

// TestSetResponse_PDFShowsPanel pins: an application/pdf response shows a PDF
// panel in the body region — a Save button or a label mentioning PDF.
func TestSetResponse_PDFShowsPanel(t *testing.T) {
	rv := newImageBlindRV(t)
	body := []byte("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF")

	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("application/pdf"),
		Body:       body,
		Size:       int64(len(body)),
	})

	if !hasVisibleSaveButtonOrPDFLabel(rv.bodyStack) {
		t.Errorf("PDF response: expected a visible Save button or a PDF label in the body region; visible text =\n%s",
			visibleTextOf(rv.bodyStack))
	}

	// A PDF is not an image — no image preview for it.
	if len(visibleImages(rv.bodyStack)) != 0 {
		t.Errorf("PDF response: unexpected *canvas.Image preview shown for a PDF body")
	}
}

// TestImageThenTextResets pins the reset path: after an image response, a
// following text/JSON response clears the image view (no stale *canvas.Image)
// and renders the text.
func TestImageThenTextResets(t *testing.T) {
	rv := newImageBlindRV(t)

	// First: an image response (establishes the image preview).
	pngBytes := tinyPNG(t, 8, 4)
	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("image/png"),
		Body:       pngBytes,
		Size:       int64(len(pngBytes)),
	})
	if len(visibleImages(rv.bodyStack)) == 0 {
		t.Fatalf("precondition: image response did not produce a visible *canvas.Image preview")
	}

	// Then: a text/JSON response must reset the image view.
	body := []byte(`{"ok":true}`)
	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("application/json"),
		Body:       body,
		Size:       int64(len(body)),
	})

	if anyImagePresent(rv.bodyStack) {
		t.Errorf("reset: stale *canvas.Image still present after switching to a text response")
	}
	got := visibleTextOf(rv.bodyStack)
	if !strings.Contains(got, "\"ok\"") {
		t.Errorf("reset: text body not rendered after image→text switch; visible text =\n%s", got)
	}
}
