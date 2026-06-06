package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/ultramcu/yon/internal/model"
)

// uiBTOpenAPISpec is a minimal but valid OpenAPI 3.0 document (JSON, which is a
// subset of YAML so it parses either way) describing a single GET operation
// served from one server URL. Written from the issue #27 contract only.
const uiBTOpenAPISpec = `{
  "openapi": "3.0.0",
  "info": {
    "title": "uiBT API",
    "version": "1.0.0"
  },
  "servers": [
    { "url": "https://api.example.com" }
  ],
  "paths": {
    "/users": {
      "get": {
        "summary": "List users",
        "operationId": "listUsers",
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  }
}`

// TestImportOpenAPIData_OpensWindow feeds a small valid OpenAPI 3.0 document and
// asserts ImportOpenAPIData returns no error and a non-nil window (a collection
// window opened), and — robustly — that the opened collection carries at least
// one mapped request. Mirrors the Postman ImportCollectionData harness.
func TestImportOpenAPIData_OpensWindow(t *testing.T) {
	a := test.NewApp()
	app := New(a)

	before := len(app.windows)

	w, _, err := app.ImportOpenAPIData([]byte(uiBTOpenAPISpec))
	if err != nil {
		t.Fatalf("ImportOpenAPIData returned error on valid spec: %v", err)
	}
	if w == nil {
		t.Fatal("ImportOpenAPIData returned a nil window on success")
	}
	t.Cleanup(w.win.Close)

	if got := len(w.coll.Requests); got < 1 {
		t.Fatalf("imported request count = %d, want >= 1", got)
	}

	if len(app.windows) != before+1 {
		t.Fatalf("app window count = %d, want %d (new window should be tracked)", len(app.windows), before+1)
	}
	if _, ok := app.windows[w]; !ok {
		t.Fatal("returned window is not tracked in app.windows")
	}
}

// TestImportOpenAPIData_BadSpec asserts junk bytes yield a non-nil error and a
// nil window (nothing should be opened).
func TestImportOpenAPIData_BadSpec(t *testing.T) {
	a := test.NewApp()
	app := New(a)

	before := len(app.windows)

	w, _, err := app.ImportOpenAPIData([]byte("not a spec"))
	if err == nil {
		t.Fatal("ImportOpenAPIData returned nil error on junk input")
	}
	if w != nil {
		t.Cleanup(w.win.Close)
		t.Fatal("ImportOpenAPIData returned a non-nil window on error")
	}
	if len(app.windows) != before {
		t.Fatalf("app window count changed on failed import: got %d, want %d", len(app.windows), before)
	}
}

// uiBTImportOpenAPILabel is the exact File-menu item label required by the
// contract, using the real ellipsis character (U+2026).
const uiBTImportOpenAPILabel = "Import OpenAPI / Swagger…"

// TestFileMenu_ImportOpenAPIItem asserts the File menu carries an item with the
// exact label "Import OpenAPI / Swagger…" wired to a non-nil Action, built the
// same way other ui tests walk buildMainMenu.
func TestFileMenu_ImportOpenAPIItem(t *testing.T) {
	w := newScopeWindow(t, model.NewCollection("T"))

	var file *fyne.Menu
	for _, m := range w.buildMainMenu().Items {
		if m.Label == "File" {
			file = m
		}
	}
	if file == nil {
		t.Fatal("no File menu")
	}

	var item *fyne.MenuItem
	for _, it := range file.Items {
		if it.Label == uiBTImportOpenAPILabel {
			item = it
			break
		}
	}
	if item == nil {
		t.Fatalf("no File menu item labelled %q", uiBTImportOpenAPILabel)
	}
	if item.Action == nil {
		t.Fatalf("File menu item %q has a nil Action", uiBTImportOpenAPILabel)
	}
}
