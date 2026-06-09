package ui

import (
	"encoding/base64"
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// Blind Test author (UI lane) for issue #37: base64 image preview in the
// responseView. Written from the CONTRACT only, not the implementation:
//
//   - A `data:image/...;base64,...` body is auto-previewed as a *canvas.Image in
//     the body region (rv.bodyStack), exactly like a raw image response (#16).
//   - A bare base64 image body is NOT auto-previewed; instead a "Decode as image"
//     affordance is offered (driving the click headlessly is brittle, so here we
//     only pin that no image appears automatically — opt-in, not auto).
//   - A plain text/JSON body shows no image preview at all.
//
// Reuses helpers from responseview_image_blindtest_test.go: newImageBlindRV,
// tinyPNG, visibleImages, anyImagePresent, ct.

// b64uiBTDataURI builds a `data:image/png;base64,<...>` string from real PNG
// bytes of the given size.
func b64uiBTDataURI(t *testing.T, w, h int) string {
	t.Helper()
	png := tinyPNG(t, w, h)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// TestDataURIImage_AutoPreviews pins the headline #37 behaviour: a body that is a
// `data:image/png;base64,...` data-URI is auto-previewed as a *canvas.Image in
// the body region, even though the Content-Type is plain text. Detection is
// body-driven.
//
// FAIL-BEFORE: on current code renderBody does not decode base64, so the data-URI
// is rendered as text and no *canvas.Image appears in rv.bodyStack.
func TestDataURIImage_AutoPreviews(t *testing.T) {
	rv := newImageBlindRV(t)
	b64uiBTBody := b64uiBTDataURI(t, 8, 4)

	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("text/plain"),
		Body:       []byte(b64uiBTBody),
		Size:       int64(len(b64uiBTBody)),
	})

	if len(visibleImages(rv.bodyStack)) == 0 {
		t.Fatalf("data:image/png;base64 body: expected a visible *canvas.Image preview in the body, found none")
	}
}

// TestPlainTextNoAutoImage guards against over-eager decoding: a normal JSON body
// must NOT produce any *canvas.Image in the body region.
func TestPlainTextNoAutoImage(t *testing.T) {
	rv := newImageBlindRV(t)
	b64uiBTBody := []byte(`{"a":1}`)

	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("application/json"),
		Body:       b64uiBTBody,
		Size:       int64(len(b64uiBTBody)),
	})

	if anyImagePresent(rv.bodyStack) {
		t.Errorf("plain JSON body: unexpected *canvas.Image present in the body region")
	}
}

// TestBareBase64NoAutoImage pins the "bare = opt-in, not auto" decision: a body
// that is bare base64 of a real PNG (no data-URI prefix, no image Content-Type)
// must NOT be auto-previewed. The "Decode as image" affordance is hard to locate
// blindly, so here we only assert the absence of an automatic image preview and
// leave the button itself to the verifier.
func TestBareBase64NoAutoImage(t *testing.T) {
	rv := newImageBlindRV(t)
	bare := base64.StdEncoding.EncodeToString(tinyPNG(t, 8, 4))

	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("text/plain"),
		Body:       []byte(bare),
		Size:       int64(len(bare)),
	})

	if anyImagePresent(rv.bodyStack) {
		t.Errorf("bare base64 body: expected NO auto image preview (opt-in only), but a *canvas.Image was present")
	}
}
