package ui

import "fmt"

// Labels for the base64 opt-in toggle (issue #37). base64DecodeLabel is the
// affordance offered for an ambiguous bare-base64 image body — the user clicks it
// to preview the decoded image; base64ShowTextLabel flips back to the text view.
const (
	base64DecodeLabel   = "Decode as image"
	base64ShowTextLabel = "Show text"
)

// base64MetaNote is appended to the response meta line when a data-URI image is
// auto-previewed (issue #37), e.g. "… · decoded from base64 · 64×64". It marks
// that the on-screen image came from a base64 body rather than raw image bytes.
const base64MetaNote = "   ·   decoded from base64"

// renderBase64 handles a textual body that is actually a base64-encoded image
// (issue #37). It is called from renderBody only when the body is not already a
// raw image/PDF and Find is inactive. It returns true when it has taken over the
// Body view (and renderBody must stop), or false to let renderBody fall through
// to the normal text path.
//
// Behaviour by detected source:
//
//   - base64None / not ok: not a base64 image — hide the opt-in button and let
//     the text path run (return false).
//   - base64DataURI (unambiguous): in Pretty mode auto-preview the DECODED image
//     and annotate the meta line "· decoded from base64 · W×H" (return true); in
//     Raw mode show the raw base64 text so the encoded bytes stay inspectable
//     (return false). The opt-in button is not shown — the data URI is decoded
//     automatically.
//   - base64Bare (ambiguous): render the text body as normal but show an opt-in
//     "Decode as image" button. While the user has opted in (base64Decoded) the
//     decoded image is previewed and the button reads "Show text" (return true);
//     otherwise the text path runs and the button reads "Decode as image"
//     (return false).
func (rv *responseView) renderBase64() bool {
	decoded, source, ok := base64ImageDecode(rv.contentType, rv.fullBody)
	if !ok || source == base64None {
		rv.hideDecodeButton()
		return false
	}

	switch source {
	case base64DataURI:
		// Unambiguous: no opt-in button, auto-decode in Pretty.
		rv.hideDecodeButton()
		if rv.pretty {
			rv.noticeLabel.Hide()
			rv.showImageBytes(decoded)
			rv.setBase64Meta(decoded)
			return true
		}
		// Raw: keep the raw base64 text visible; meta drops the decode note.
		rv.metaLabel.SetText(rv.baseMeta)
		return false

	case base64Bare:
		// Ambiguous: offer the opt-in. The meta line carries no decode note (the
		// body is shown as text unless the user opts in).
		rv.metaLabel.SetText(rv.baseMeta)
		if rv.base64Decoded {
			rv.showDecodeButton(base64ShowTextLabel)
			rv.noticeLabel.Hide()
			rv.showImageBytes(decoded)
			return true
		}
		rv.showDecodeButton(base64DecodeLabel)
		return false
	}

	rv.hideDecodeButton()
	return false
}

// setBase64Meta appends the "decoded from base64" note (and the decoded image's
// pixel dimensions, when readable) to the base meta line.
func (rv *responseView) setBase64Meta(decoded []byte) {
	meta := rv.baseMeta + base64MetaNote
	if w, h, ok := imageDimensions(decoded); ok {
		meta += fmt.Sprintf("   ·   %d×%d", w, h)
	}
	rv.metaLabel.SetText(meta)
}

// showDecodeButton shows the base64 opt-in toggle with the given label.
func (rv *responseView) showDecodeButton(label string) {
	if rv.decodeBtn == nil {
		return
	}
	rv.decodeBtn.SetText(label)
	rv.decodeBtn.Show()
}

// hideDecodeButton hides the base64 opt-in toggle (no-op before construction).
func (rv *responseView) hideDecodeButton() {
	if rv.decodeBtn != nil {
		rv.decodeBtn.Hide()
	}
}

// toggleBase64Decode flips the base64-image opt-in for an ambiguous bare-base64
// body and re-renders: the first click previews the decoded image ("Show text"
// afterwards), the next returns to the text view ("Decode as image").
func (rv *responseView) toggleBase64Decode() {
	rv.base64Decoded = !rv.base64Decoded
	rv.renderBody()
}
