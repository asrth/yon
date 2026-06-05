package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// These tests cover issue #1: the Variables inspector renders "[KEY] : [VALUE]"
// rows and a double-click copies that variable's REAL value to the clipboard.
// They exercise the two halves independently of the live panel: varRowDisplay
// (the pure key/value split + secret masking) and the row widget's DoubleTapped
// (the copy gesture). Both must FAIL before the feature and PASS after.

// TestVarRowDisplayMasksSecret pins the display contract: a non-secret row shows
// its key and clear value; a Secret row shows the mask in the VALUE column and
// the clear value MUST NOT appear in the display strings — while the real Value
// (the copy source) stays available on the varView untouched.
func TestVarRowDisplayMasksSecret(t *testing.T) {
	// Non-secret: key as-is, value verbatim.
	plain := varView{Key: "baseUrl", Value: "http://localhost:7878", Scope: scopeEnv}
	if k, v := varRowDisplay(plain); k != "baseUrl" || v != "http://localhost:7878" {
		t.Errorf("varRowDisplay(plain) = (%q, %q), want (%q, %q)",
			k, v, "baseUrl", "http://localhost:7878")
	}

	// Secret: key as-is, value MASKED — the clear value must be absent from the
	// display, yet still present on the varView as the copy source.
	const secretValue = "topsecret-do-not-show"
	secret := varView{Key: "apiKey", Value: secretValue, Secret: true, Scope: scopeEnv}
	k, v := varRowDisplay(secret)
	if k != "apiKey" {
		t.Errorf("varRowDisplay secret key = %q, want %q", k, "apiKey")
	}
	if v != secretMask {
		t.Errorf("varRowDisplay secret value = %q, want the mask %q", v, secretMask)
	}
	if v == secretValue {
		t.Fatalf("SECRET LEAK: display value is the clear secret %q", secretValue)
	}
	// The real value remains the copy source on the varView itself.
	if secret.Value != secretValue {
		t.Errorf("varView.Value changed to %q, want the real value %q intact for copying",
			secret.Value, secretValue)
	}
}

// TestVarRowDoubleTapCopiesRealValue pins the copy gesture: double-tapping a row
// copies its REAL value to the clipboard — and for a SECRET row that means the
// clear value, NOT the on-screen mask (double-click is the explicit reveal-copy).
func TestVarRowDoubleTapCopiesRealValue(t *testing.T) {
	app := test.NewApp()
	clip := app.Clipboard()

	// A plain row copies its clear value.
	plain := varView{Key: "baseUrl", Value: "http://localhost:7878", Scope: scopeEnv}
	var copied varView
	row := newVarRowWidget(plain, func(v varView) {
		copied = v
		clip.SetContent(v.Value)
	})
	row.DoubleTapped(&fyne.PointEvent{})
	if copied.Key != "baseUrl" {
		t.Errorf("onCopy got key %q, want %q", copied.Key, "baseUrl")
	}
	if got := clip.Content(); got != plain.Value {
		t.Errorf("clipboard = %q after double-tap, want the real value %q", got, plain.Value)
	}

	// A SECRET row copies the CLEAR value, not the mask.
	const secretValue = "topsecret-clear"
	secret := varView{Key: "apiKey", Value: secretValue, Secret: true, Scope: scopeEnv}
	secretRow := newVarRowWidget(secret, func(v varView) {
		clip.SetContent(v.Value)
	})
	secretRow.DoubleTapped(&fyne.PointEvent{})
	if got := clip.Content(); got != secretValue {
		t.Errorf("clipboard = %q after secret double-tap, want the CLEAR value %q (not the mask %q)",
			got, secretValue, secretMask)
	}
	if clip.Content() == secretMask {
		t.Fatalf("double-tap copied the mask %q instead of the real secret value", secretMask)
	}
}

// TestVarsPanelCopyValueUsesRealValueAndFlashes pins the panel's own copy
// callback (the one wired into every row): it copies the real value to the
// clipboard AND flashes a "Copied <key>" confirmation in the window status bar,
// which the next updateStatusBar is free to overwrite (transient, never stuck).
func TestVarsPanelCopyValueUsesRealValueAndFlashes(t *testing.T) {
	w := newVarsTestWindow(t)
	app := fyne.CurrentApp()

	secret := varView{Key: "token", Value: "abc123-real", Secret: true, Scope: scopeRuntime}
	w.varsPanel.copyValue(secret)

	if got := app.Clipboard().Content(); got != "abc123-real" {
		t.Errorf("copyValue clipboard = %q, want the real value %q", got, "abc123-real")
	}
	if w.sbStatus == nil {
		t.Fatal("status bar text object is nil; cannot confirm the flash")
	}
	if got, want := w.sbStatus.Text, "Copied token"; got != want {
		t.Errorf("status flash = %q, want %q", got, want)
	}

	// The flash is transient: a normal status update fully rewrites it, so it can
	// never leave the bar stuck on "Copied …".
	w.updateStatusBar()
	if w.sbStatus.Text == "Copied token" {
		t.Error("status flash persisted after updateStatusBar; it must be transient")
	}
}
