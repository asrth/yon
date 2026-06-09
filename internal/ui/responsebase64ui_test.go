package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// Dev B — issue #37 UI wiring (base64 image preview). The detector
// base64ImageDecode is Dev A's stub returning false in this tree, so the
// auto-preview / opt-in paths cannot be driven end-to-end here. These tests pin
// the pieces Dev B owns: showImageBytes builds the preview from the GIVEN bytes
// and records them for Save; the base64 meta note formats correctly; and with the
// stub detector a plain text body renders as text with the opt-in button hidden.

// TestShowImageBytes_UsesGivenBytes verifies showImageBytes builds the preview
// from b (not rv.fullBody) and records b as previewBytes for the Save path.
func TestShowImageBytes_UsesGivenBytes(t *testing.T) {
	rv := bt2View(t)
	raw := tinyPNG(t, 5, 4)
	decoded := tinyPNG(t, 9, 2)

	rv.fullBody = raw
	rv.showImageBytes(decoded)

	if rv.bodyImage == nil || rv.bodyImage.Resource == nil {
		t.Fatal("showImageBytes should build a *canvas.Image preview")
	}
	if !bytes.Equal(rv.bodyImage.Resource.Content(), decoded) {
		t.Error("preview should be built from the bytes passed to showImageBytes, not rv.fullBody")
	}
	if !bytes.Equal(rv.previewBytes, decoded) {
		t.Error("previewBytes should track the bytes passed to showImageBytes (used by Save image…)")
	}
}

// TestShowImage_UsesFullBody verifies the issue-#16 entry point still previews
// the raw body (the refactor kept showImage as showImageBytes(rv.fullBody)).
func TestShowImage_UsesFullBody(t *testing.T) {
	rv := bt2View(t)
	raw := tinyPNG(t, 6, 6)
	rv.fullBody = raw
	rv.showImage()

	if !bytes.Equal(rv.previewBytes, raw) {
		t.Error("showImage should preview rv.fullBody and set previewBytes to it")
	}
}

// TestHideImage_ClearsPreviewBytes ensures hideImage drops previewBytes so a
// later text/PDF response leaves no stale decoded image behind the Save path.
func TestHideImage_ClearsPreviewBytes(t *testing.T) {
	rv := bt2View(t)
	rv.showImageBytes(tinyPNG(t, 3, 3))
	rv.hideImage()
	if rv.previewBytes != nil {
		t.Error("hideImage should clear previewBytes")
	}
}

// TestBase64Meta_Note formats the auto-preview meta note with the decoded image's
// dimensions appended to the base meta line.
func TestBase64Meta_Note(t *testing.T) {
	rv := bt2View(t)
	rv.baseMeta = "   12 ms   ·   40 B"
	rv.setBase64Meta(tinyPNG(t, 11, 13))

	meta := rv.metaLabel.Text
	if !strings.Contains(meta, "decoded from base64") {
		t.Errorf("meta = %q, want it to note the base64 decode", meta)
	}
	if !strings.Contains(meta, "11×13") {
		t.Errorf("meta = %q, want the decoded image dimensions 11×13", meta)
	}
}

// TestDecodeButton_HiddenForPlainText verifies that with the stub detector a
// plain JSON body renders as text and the opt-in button stays hidden (no false
// "Decode as image" affordance), and the existing text view is shown.
func TestDecodeButton_HiddenForPlainText(t *testing.T) {
	rv := bt2View(t)
	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    []model.Param{{Key: "Content-Type", Value: "application/json"}},
		Body:       []byte(`{"a":1}`),
	})
	if rv.decodeBtn.Visible() {
		t.Error("plain text body should not show the Decode as image button")
	}
	if rv.bodyImageScroll != nil && rv.bodyImageScroll.Visible() {
		t.Error("plain text body should not show an image preview")
	}
}

// TestToggleBase64Decode_FlipsState pins the opt-in toggle plumbing: each call
// flips base64Decoded (the flag renderBase64 reads to preview the decoded image
// vs. show text). setResponse resets it to false.
func TestToggleBase64Decode_FlipsState(t *testing.T) {
	rv := bt2View(t)
	rv.fullBody = []byte("not-empty")
	rv.contentType = "text/plain"

	if rv.base64Decoded {
		t.Fatal("base64Decoded should start false")
	}
	rv.toggleBase64Decode()
	if !rv.base64Decoded {
		t.Error("first toggle should opt in (base64Decoded=true)")
	}
	rv.toggleBase64Decode()
	if rv.base64Decoded {
		t.Error("second toggle should opt back out (base64Decoded=false)")
	}

	rv.base64Decoded = true
	rv.setResponse(model.Response{Status: 200, StatusText: "OK", Body: []byte("x")})
	if rv.base64Decoded {
		t.Error("setResponse should reset the base64 opt-in to false for a new response")
	}
}

// TestButtonLabels documents the exact button labels for the blind tester.
func TestButtonLabels(t *testing.T) {
	if base64DecodeLabel != "Decode as image" {
		t.Errorf("decode label = %q, want %q", base64DecodeLabel, "Decode as image")
	}
	if base64ShowTextLabel != "Show text" {
		t.Errorf("show-text label = %q, want %q", base64ShowTextLabel, "Show text")
	}
}
