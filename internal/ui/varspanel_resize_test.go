package ui

import (
	"strings"
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// TestVarsPanel_LongValueDoesNotBlowUpMinWidth pins issue #36: a long variable
// value (e.g. a captured JWT access_token) must NOT balloon the Variables
// panel's minimum width, which previously forced the whole window wider with no
// way to shrink back. The panel scrolls bi-directionally, so a long row scrolls
// horizontally and the container min width stays bounded.
//
// On the buggy (vertical-only scroll) code the long-value width is thousands of
// px larger than the short-value baseline; the fix collapses it back. The
// wShort+800 threshold cleanly separates "bounded" from "content-driven blow-up".
func TestVarsPanel_LongValueDoesNotBlowUpMinWidth(t *testing.T) {
	w := newScopeWindow(t, model.NewCollection("T"))

	// Short runtime value baseline.
	w.runtimeVars = map[string]string{"x": "abc"}
	w.varsPanel.refresh()
	wShort := w.varsPanel.container.MinSize().Width

	// A long captured token value.
	w.runtimeVars = map[string]string{"access_token": strings.Repeat("e", 600)}
	w.varsPanel.refresh()
	wLong := w.varsPanel.container.MinSize().Width

	if wLong > wShort+800 {
		t.Errorf("long variable value blew up the Variables panel min width: "+
			"wShort=%.0f px, wLong=%.0f px (delta=%.0f px). A long value must scroll, "+
			"not force the panel/window wider.", wShort, wLong, wLong-wShort)
	}
}
