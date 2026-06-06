package ui

import (
	"strings"
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// Blind Test author for issue #32: a long response line must NOT blow up the
// response view's minimum width. The bug: the body viewers do not scroll
// horizontally, so a single very long line forces responseView.container's
// MinSize().Width to grow with the content — which in turn forces the whole
// window wide and unable to shrink back.
//
// Contract (post-fix): the response viewers scroll bi-directionally, so
// rv.container.MinSize().Width stays bounded regardless of line length.
//
// These tests are written from the CONTRACT only — they touch only existing
// responseView fields (container) and the public setResponse / model.Response
// API, so they COMPILE and FAIL on the current (buggy) code and PASS after the
// fix.
//
// Measured on the buggy code: short body {"a":1} -> MinSize width ~459 px; one
// 2000-char line -> ~16104 px (a ~15000 px content-driven blow-up). The
// post-fix delta should be near-zero. The threshold below (wShort + 800) leaves
// generous slack for scrollbar / padding while clearly separating "bounded"
// from "content-driven blow-up".

// newResizeBlindRV builds a fresh responseView on the headless test driver via
// the same construction path the other responseView tests use. Named uniquely
// to avoid colliding with newImageBlindRV.
func newResizeBlindRV(t *testing.T) *responseView {
	t.Helper()
	return newImageBlindRV(t)
}

// minWidthSlack is the maximum allowed growth (px) of the response container's
// minimum width between a short body and a long-line body. The pre-fix blow-up
// is ~15000 px; the post-fix growth should be near-zero, so 800 px cleanly
// separates the two while tolerating scrollbar / padding differences.
const minWidthSlack = float32(800)

// TestResponseView_LongLineDoesNotBlowUpMinWidth pins the contract: a single
// very long response line must not balloon rv.container.MinSize().Width. On the
// buggy code wLong is thousands of px larger than wShort and this FAILS; after
// the fix both stay small/bounded and it PASSES.
func TestResponseView_LongLineDoesNotBlowUpMinWidth(t *testing.T) {
	rv := newResizeBlindRV(t)

	// Short body baseline.
	shortBody := []byte(`{"a":1}`)
	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("application/json"),
		Body:       shortBody,
		Size:       int64(len(shortBody)),
	})
	wShort := rv.container.MinSize().Width

	// Long-line body: a single line carrying a 2000-char token value.
	longBody := []byte(`{"token":"` + strings.Repeat("X", 2000) + `"}`)
	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("application/json"),
		Body:       longBody,
		Size:       int64(len(longBody)),
	})
	wLong := rv.container.MinSize().Width

	if wLong > wShort+minWidthSlack {
		t.Errorf("long response line blew up the response view min width: "+
			"wShort=%.0f px, wLong=%.0f px (delta=%.0f px, allowed slack=%.0f px). "+
			"A long line must scroll, not force the container/window wider.",
			wShort, wLong, wLong-wShort, minWidthSlack)
	}
}

// TestResponseView_PrettyJSONManyLongLinesStaysBounded pins the same contract
// for a Pretty-printed JSON body with MANY long lines: the widest line must not
// drive the container's min width up. Bounded post-fix, ballooned pre-fix.
func TestResponseView_PrettyJSONManyLongLinesStaysBounded(t *testing.T) {
	rv := newResizeBlindRV(t)

	// Short body baseline (reuse the same baseline metric).
	shortBody := []byte(`{"a":1}`)
	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("application/json"),
		Body:       shortBody,
		Size:       int64(len(shortBody)),
	})
	wShort := rv.container.MinSize().Width

	// A JSON object with many keys, each holding a long value — Pretty rendering
	// keeps each value on its own (long) line.
	var b strings.Builder
	b.WriteString("{")
	for i := 0; i < 12; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"key`)
		b.WriteString(strings.Repeat("k", 3))
		b.WriteString(`":"`)
		b.WriteString(strings.Repeat("V", 1500))
		b.WriteString(`"`)
	}
	b.WriteString("}")
	longBody := []byte(b.String())

	rv.setResponse(model.Response{
		Status:     200,
		StatusText: "OK",
		Headers:    ct("application/json"),
		Body:       longBody,
		Size:       int64(len(longBody)),
	})
	wLong := rv.container.MinSize().Width

	if wLong > wShort+minWidthSlack {
		t.Errorf("Pretty JSON with many long lines blew up the response view min width: "+
			"wShort=%.0f px, wLong=%.0f px (delta=%.0f px, allowed slack=%.0f px). "+
			"Long lines must scroll, not force the container/window wider.",
			wShort, wLong, wLong-wShort, minWidthSlack)
	}
}
