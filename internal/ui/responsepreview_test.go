package ui

import (
	"bytes"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/ultramcu/yon/internal/model"
)

// Dev B — responseview.go integration for issue #16 (image/PDF previews).
//
// These smoke tests pin the routing setResponse → renderBody does by body kind
// (Dev A's classifyBody): an image body shows the image preview (a *canvas.Image
// in the body stack, the text viewers hidden) with its WxH in the meta line; a
// PDF body shows the PDF panel (Save button present); a JSON body is unchanged
// (the existing TextGrid text path, no image/PDF view).
//
// tinyPNG(t, w, h) (a small decodable PNG of the given size) is defined in
// responseview_image_blindtest_test.go and reused here.

// bodyStackHasVisibleImage reports whether the body stack currently shows the
// image preview (scrolled) and whether the text viewers are hidden.
func bodyStackHasVisibleImage(rv *responseView) (hasImage, textHidden bool) {
	hasImage = rv.bodyImage != nil && rv.bodyImageScroll != nil && rv.bodyImageScroll.Visible()
	textHidden = !rv.bodyScroll.Visible() && !rv.bodyList.Visible()
	return
}

func TestResponsePreview_ImageBodyShowsImageView(t *testing.T) {
	rv := bt2View(t)
	body := tinyPNG(t, 7, 3)

	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    []model.Param{{Key: "Content-Type", Value: "image/png"}},
		Body:       body,
		Size:       int64(len(body)),
	})

	hasImage, textHidden := bodyStackHasVisibleImage(rv)
	if !hasImage {
		t.Fatal("image body should show a *canvas.Image preview in the body stack")
	}
	if !textHidden {
		t.Error("image body should hide the text viewers (bodyScroll + bodyList)")
	}
	if rv.bodyPDF != nil && rv.bodyPDF.Visible() {
		t.Error("image body should not show the PDF panel")
	}
	// The image preview is built from the full response bytes.
	if rv.bodyImage.Resource == nil || !bytes.Equal(rv.bodyImage.Resource.Content(), body) {
		t.Error("bodyImage should be built from the full response body bytes")
	}
	// Dimensions appear in the meta line (7×3).
	if meta := rv.metaLabel.Text; !strings.Contains(meta, "7×3") {
		t.Errorf("meta = %q, want it to contain the image dimensions 7×3", meta)
	}
}

func TestResponsePreview_PDFBodyShowsPanelWithSave(t *testing.T) {
	rv := bt2View(t)
	body := []byte("%PDF-1.4\n stuff \n%%EOF")

	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    []model.Param{{Key: "Content-Type", Value: "application/pdf"}},
		Body:       body,
		Size:       int64(len(body)),
	})

	if rv.bodyPDF == nil || !rv.bodyPDF.Visible() {
		t.Fatal("PDF body should show the PDF panel")
	}
	if rv.bodyScroll.Visible() || rv.bodyList.Visible() {
		t.Error("PDF body should hide the text viewers")
	}
	if rv.bodyImageScroll != nil && rv.bodyImageScroll.Visible() {
		t.Error("PDF body should not show the image preview")
	}
	if !panelHasButtonLabelled(rv.bodyPDF, "Save") {
		t.Error("PDF panel should contain a Save button")
	}
}

func TestResponsePreview_JSONBodyUnchanged(t *testing.T) {
	rv := bt2View(t)
	body := []byte(`{"a":1,"b":[2,3]}`)

	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    []model.Param{{Key: "Content-Type", Value: "application/json"}},
		Body:       body,
		Size:       int64(len(body)),
	})

	// The existing text path: TextGrid scroll visible, neither preview shown.
	if !rv.bodyScroll.Visible() {
		t.Error("JSON body should keep the TextGrid text viewer visible")
	}
	if rv.bodyImageScroll != nil && rv.bodyImageScroll.Visible() {
		t.Error("JSON body should not show the image preview")
	}
	if rv.bodyPDF != nil && rv.bodyPDF.Visible() {
		t.Error("JSON body should not show the PDF panel")
	}
	if got := rv.bodyGrid.Text(); !strings.Contains(got, `"a"`) {
		t.Errorf("JSON body text = %q, want it to contain the JSON", got)
	}
}

// A later text response must not leave a stale image preview showing.
func TestResponsePreview_ImageThenJSONResetsPreview(t *testing.T) {
	rv := bt2View(t)

	img := tinyPNG(t, 4, 4)
	rv.setResponse(model.Response{
		Status: 200, StatusText: "OK",
		Headers: []model.Param{{Key: "Content-Type", Value: "image/png"}},
		Body:    img, Size: int64(len(img)),
	})
	if hasImage, _ := bodyStackHasVisibleImage(rv); !hasImage {
		t.Fatal("precondition: image preview should be showing")
	}

	js := []byte(`{"ok":true}`)
	rv.setResponse(model.Response{
		Status: 200, StatusText: "OK",
		Headers: []model.Param{{Key: "Content-Type", Value: "application/json"}},
		Body:    js, Size: int64(len(js)),
	})
	if rv.bodyImageScroll != nil && rv.bodyImageScroll.Visible() {
		t.Error("a JSON response after an image must hide the stale image preview")
	}
	if !rv.bodyScroll.Visible() {
		t.Error("a JSON response after an image must restore the text viewer")
	}
}

// setPending must hide the binary previews too.
func TestResponsePreview_PendingHidesPreviews(t *testing.T) {
	rv := bt2View(t)
	img := tinyPNG(t, 4, 4)
	rv.setResponse(model.Response{
		Status: 200, StatusText: "OK",
		Headers: []model.Param{{Key: "Content-Type", Value: "image/png"}},
		Body:    img, Size: int64(len(img)),
	})

	rv.setPending()
	if rv.bodyImageScroll != nil && rv.bodyImageScroll.Visible() {
		t.Error("setPending must hide the image preview")
	}
	if rv.bodyPDF != nil && rv.bodyPDF.Visible() {
		t.Error("setPending must hide the PDF panel")
	}
}

// For an image, Raw shows the raw-bytes text view so bytes are inspectable; Pretty
// returns to the image preview. (Dev B's documented Pretty/Raw choice for images.)
func TestResponsePreview_ImageRawShowsBytesPrettyShowsImage(t *testing.T) {
	rv := bt2View(t)
	img := tinyPNG(t, 5, 5)
	rv.setResponse(model.Response{
		Status: 200, StatusText: "OK",
		Headers: []model.Param{{Key: "Content-Type", Value: "image/png"}},
		Body:    img, Size: int64(len(img)),
	})

	rv.setPretty(false) // Raw
	if rv.bodyImageScroll != nil && rv.bodyImageScroll.Visible() {
		t.Error("Raw mode on an image should hide the image preview")
	}
	if !rv.bodyScroll.Visible() {
		t.Error("Raw mode on an image should show the raw-bytes text viewer")
	}

	rv.setPretty(true) // back to Pretty
	if rv.bodyImageScroll == nil || !rv.bodyImageScroll.Visible() {
		t.Error("Pretty mode on an image should show the image preview again")
	}
}

// panelHasButtonLabelled reports whether obj's tree contains a *widget.Button
// whose label contains want.
func panelHasButtonLabelled(obj fyne.CanvasObject, want string) bool {
	switch o := obj.(type) {
	case *widget.Button:
		return strings.Contains(o.Text, want)
	case *fyne.Container:
		for _, c := range o.Objects {
			if panelHasButtonLabelled(c, want) {
				return true
			}
		}
	}
	return false
}
