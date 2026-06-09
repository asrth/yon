package ui

import (
	"reflect"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/ultramcu/yon/internal/model"
)

// Blind Test author (Turtle) for issue #24: right-click an image response to
// save it, with the correct file extension. These tests are written from the
// CONTRACT only — the implementation does not exist in this worktree:
//
//   - responseDefaultFilename(contentType, body) is a pure helper mapping an
//     image/PDF/text response to "response.<ext>" using the Content-Type
//     subtype or the body's magic bytes (png/jpg/gif/webp/bmp; pdf; else txt).
//   - After setResponse with an image response, rv.bodyStack carries a widget
//     implementing fyne.SecondaryTappable whose right-click menu has a
//     "Save image…" item. A text/PDF response exposes no such item.
//
// Existing helpers reused (defined in other ui test files, NOT redefined here):
//   newImageBlindRV(t), tinyPNG(t,w,h), ct(value)            (responseview_image_blindtest_test.go)
//   devEncodeJPEG/devEncodeGIF(t,w,h)                        (responsekind_test.go)
//   walkObjects(app, root, visit)                            (sidebar_tap_routing_test.go)
//   popUpMenuIn(o)                                           (verbrow_tap_test.go)

// saveImageLabel is the exact menu-item label the contract requires, using the
// real ellipsis character (U+2026), not three ASCII dots.
const saveImageLabel = "Save image…"

// TestResponseDefaultFilename pins the pure filename helper: the chosen
// extension follows the image subtype / magic bytes, PDF maps to response.pdf,
// and everything else maps to response.txt.
func TestResponseDefaultFilename(t *testing.T) {
	png := tinyPNG(t, 3, 5)
	jpg := devEncodeJPEG(t, 4, 4)
	gif := devEncodeGIF(t, 2, 2)

	cases := []struct {
		name        string
		contentType string
		body        []byte
		want        string
	}{
		// Image by explicit Content-Type subtype.
		{"image/png", "image/png", nil, "response.png"},
		{"image/jpeg", "image/jpeg", nil, "response.jpg"},
		{"image/gif", "image/gif", nil, "response.gif"},
		{"image/webp", "image/webp", nil, "response.webp"},
		{"image/bmp", "image/bmp", nil, "response.bmp"},

		// Image by magic bytes under a generic Content-Type.
		{"magic png", "application/octet-stream", png, "response.png"},
		{"magic jpeg", "application/octet-stream", jpg, "response.jpg"},
		{"magic gif", "application/octet-stream", gif, "response.gif"},

		// PDF by Content-Type and by magic bytes.
		{"application/pdf", "application/pdf", nil, "response.pdf"},
		{"magic pdf", "", []byte("%PDF-1.4"), "response.pdf"},

		// Everything else → text.
		{"text/plain", "text/plain", []byte("hello"), "response.txt"},
		{"application/json", "application/json", []byte(`{"a":1}`), "response.txt"},
		{"empty", "", nil, "response.txt"},
	}

	for _, c := range cases {
		if got := responseDefaultFilename(c.contentType, c.body); got != c.want {
			t.Errorf("responseDefaultFilename(%q, %s) = %q, want %q",
				c.contentType, c.name, got, c.want)
		}
	}
}

// TestImageResponse_HasSaveImageContextMenu is the behavioural fail-before: an
// image response must expose a right-click "Save image…" menu on its preview.
//
// Written with ONLY existing/standard symbols (no responseDefaultFilename) so it
// COMPILES and FAILS on the current code: today rv.bodyStack carries no
// SecondaryTappable image widget, so no "Save image…" item appears.
func TestImageResponse_HasSaveImageContextMenu(t *testing.T) {
	rv := newImageBlindRV(t)
	pngBytes := tinyPNG(t, 8, 4)

	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("image/png"),
		Body:       pngBytes,
		Size:       int64(len(pngBytes)),
	})

	// Give the body region a live canvas so TappedSecondary can pop up a menu,
	// mirroring how duplicate_strengthen_test.go parents a widget in a window.
	w := test.NewWindow(rv.bodyStack)
	t.Cleanup(w.Close)

	st := findSecondaryTappable(rv.bodyStack)
	if st == nil {
		t.Fatal("image response: expected a fyne.SecondaryTappable widget in rv.bodyStack " +
			"(the image preview) so the body can be right-clicked to save")
	}

	st.TappedSecondary(&fyne.PointEvent{})

	if findMenuItemByLabel(w.Canvas(), saveImageLabel) == nil {
		t.Fatalf("image response: right-click should pop up a menu with a %q item", saveImageLabel)
	}
}

// TestTextResponse_HasNoSaveImageMenu guards that the Save-image menu is
// image-specific: a JSON/text response must NOT expose a "Save image…" item —
// either there is no SecondaryTappable image widget in rv.bodyStack, or
// right-clicking it yields no such item.
func TestTextResponse_HasNoSaveImageMenu(t *testing.T) {
	rv := newImageBlindRV(t)
	body := []byte(`{"a":1}`)

	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("application/json"),
		Body:       body,
		Size:       int64(len(body)),
	})

	w := test.NewWindow(rv.bodyStack)
	t.Cleanup(w.Close)

	st := findSecondaryTappable(rv.bodyStack)
	if st == nil {
		// No secondary-tappable body widget at all — there can be no
		// "Save image…" menu. Contract satisfied.
		return
	}

	st.TappedSecondary(&fyne.PointEvent{})

	if findMenuItemByLabel(w.Canvas(), saveImageLabel) != nil {
		t.Fatalf("text response: must NOT expose a %q menu item", saveImageLabel)
	}
}

// findSecondaryTappable returns the first fyne.SecondaryTappable object in
// root's rendered tree, or nil if none is present.
func findSecondaryTappable(root fyne.CanvasObject) fyne.SecondaryTappable {
	var found fyne.SecondaryTappable
	walkObjects(fyne.CurrentApp(), root, func(o fyne.CanvasObject) {
		if found != nil {
			return
		}
		if st, ok := o.(fyne.SecondaryTappable); ok {
			found = st
		}
	})
	return found
}

// findMenuItemByLabel returns the popped-up *fyne.MenuItem on c whose label
// matches, or nil if absent. Unlike the shared menuItemByLabel helper it does
// NOT fail the test when the item is missing, so it can assert absence. Reuses
// popUpMenuIn (verbrow_tap_test.go) to unwrap each overlay's PopUpMenu.
func findMenuItemByLabel(c fyne.Canvas, label string) *fyne.MenuItem {
	for _, o := range c.Overlays().List() {
		menu := popUpMenuIn(o)
		if menu == nil {
			continue
		}
		for _, obj := range menu.Items {
			f := reflect.ValueOf(obj).Elem().FieldByName("Item")
			if !f.IsValid() {
				continue
			}
			if mi, ok := f.Interface().(*fyne.MenuItem); ok && mi.Label == label {
				return mi
			}
		}
	}
	return nil
}
