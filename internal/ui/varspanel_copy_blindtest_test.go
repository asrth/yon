package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/ultramcu/yon/internal/model"
)

// Blind tests for issue #1: the Variables inspector renders "[KEY] : [VALUE]"
// rows and DOUBLE-CLICKING a row copies that variable's REAL value to the
// clipboard — even for a Secret (masking is for the on-screen DISPLAY only).
//
// Contract under test (package ui, internal/ui/varspanel.go):
//   - varRowDisplay(v varView) (key, value string) returns the key and the
//     DISPLAY value: secretMask ("••••") when v.Secret, else v.Value.
//   - Each row is a double-tappable widget (implements fyne.DoubleTappable),
//     built per varView; its DoubleTapped copies v.Value (the REAL value, NOT
//     the mask) to fyne.CurrentApp().Clipboard().
//
// These tests are BLIND to the Dev's implementation. They reference only the
// contract symbols (varRowDisplay, varView, secretMask) plus the existing Window
// plumbing. The double-tappable row widget is discovered by WALKING the rendered
// panel for a fyne.DoubleTappable — so the test never needs the builder's name.
//
// Helpers reused from sibling test files in package ui:
//   - newVarsTestWindow (varspanel_window_blindtest_test.go)
//   - walkObjects       (sidebar_tap_routing_test.go)

// findDoubleTappables walks root's rendered object tree and returns every object
// that implements fyne.DoubleTappable, in walk order. Used to locate the row
// widgets without knowing the Dev's type/constructor name.
func findDoubleTappables(root fyne.CanvasObject) []fyne.DoubleTappable {
	var out []fyne.DoubleTappable
	walkObjects(fyne.CurrentApp(), root, func(o fyne.CanvasObject) {
		if dt, ok := o.(fyne.DoubleTappable); ok {
			out = append(out, dt)
		}
	})
	return out
}

// --- A. Display masks secrets, never the value-copy path ---------------------

// TestVarRowDisplay_MasksSecretInDisplayOnly pins varRowDisplay: it returns the
// key verbatim and the DISPLAY value, which is the clear Value for a normal row
// but the mask for a Secret (the clear secret never appears on screen).
func TestVarRowDisplay_MasksSecretInDisplayOnly(t *testing.T) {
	// Non-secret: display value is the real value.
	k, v := varRowDisplay(varView{Key: "baseUrl", Value: "http://x"})
	if k != "baseUrl" {
		t.Errorf("varRowDisplay key = %q, want %q", k, "baseUrl")
	}
	if v != "http://x" {
		t.Errorf("varRowDisplay non-secret value = %q, want %q", v, "http://x")
	}

	// Secret: display value is the mask, and MUST NOT leak the clear value.
	k, v = varRowDisplay(varView{Key: "apiKey", Value: "topsecret", Secret: true})
	if k != "apiKey" {
		t.Errorf("varRowDisplay secret key = %q, want %q", k, "apiKey")
	}
	if v != secretMask {
		t.Errorf("varRowDisplay secret display value = %q, want the mask %q", v, secretMask)
	}
	if strings.Contains(v, "topsecret") {
		t.Fatalf("SECRET LEAK: varRowDisplay secret display value %q contains the clear value", v)
	}
}

// --- B. Double-tap copies the REAL value -------------------------------------

// buildSingleVarRow installs exactly ONE enabled configured variable in the
// Window's active environment, shows + refreshes the Variables panel, and
// returns the sole double-tappable row widget mounted for it. Seeding a single
// row keeps the discovery deterministic (the test never knows the row type's
// name — it finds it by the fyne.DoubleTappable contract).
func buildSingleVarRow(t *testing.T, v model.Variable) fyne.DoubleTappable {
	t.Helper()
	w := newVarsTestWindow(t)

	w.envs = []model.Environment{{
		Name:      "Prod",
		Variables: []model.Variable{v},
	}}
	w.coll.ActiveEnvironment = "Prod"
	if env, ok := w.activeEnv(); !ok || env.Name != "Prod" {
		t.Fatalf("activeEnv() = (%+v, %v), want the Prod env active", env, ok)
	}

	w.toggleVarsPanel()
	if !w.varsVisible {
		t.Fatalf("toggleVarsPanel did not show the panel")
	}
	w.refreshVarsPanel()

	rows := findDoubleTappables(w.varsPanel.container)
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 double-tappable row for a single configured variable, found %d", len(rows))
	}
	return rows[0]
}

// TestRowDoubleTapCopiesRealValue is the key behaviour: double-tapping a row
// copies that variable's REAL value to the app clipboard. The non-secret case
// proves the wiring; the secret case proves double-click is an explicit copy of
// the CLEAR value, not the on-screen mask.
func TestRowDoubleTapCopiesRealValue(t *testing.T) {
	t.Run("non-secret copies the real value", func(t *testing.T) {
		app := test.NewApp()
		app.Clipboard().SetContent("") // start clean

		row := buildSingleVarRow(t, model.Variable{
			Key: "baseUrl", Value: "http://x", Enabled: true,
		})
		row.DoubleTapped(&fyne.PointEvent{})

		if got := fyne.CurrentApp().Clipboard().Content(); got != "http://x" {
			t.Fatalf("after double-tap, clipboard = %q, want the real value %q", got, "http://x")
		}
	})

	t.Run("secret copies the REAL value, not the mask", func(t *testing.T) {
		app := test.NewApp()
		app.Clipboard().SetContent("") // start clean

		row := buildSingleVarRow(t, model.Variable{
			Key: "apiKey", Value: "topsecret", Enabled: true, Secret: true,
		})
		row.DoubleTapped(&fyne.PointEvent{})

		got := fyne.CurrentApp().Clipboard().Content()
		if got != "topsecret" {
			t.Fatalf("after double-tap on a SECRET row, clipboard = %q, want the REAL value %q", got, "topsecret")
		}
		// Guard the mutation a naive stub would introduce: copying the DISPLAY
		// value (the mask) instead of the real value.
		if got == secretMask {
			t.Fatalf("double-tap copied the mask %q instead of the real secret value", secretMask)
		}
	})
}
