package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/ultramcu/yon/internal/model"
)

// --- BLIND test suite for Lane B (UI body editor, issue #48 form-data bodies).
// Authored purely from the contract + existing UI test patterns; the
// implementation file internal/ui/bodyform.go (and the body-editor code in
// requesteditor.go) was NOT read. Tests use the unique prefix bfuiBT to avoid
// collisions with the Dev's tests (carried as TestUI_bfuiBT... because go vet
// rejects a lowercase letter immediately after "Test"; -run bfuiBT still matches
// the substring).

// bfuiBTContains reports whether s is in list.
func bfuiBTContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestUI_bfuiBTBodyTypeLabelsRoundTrip pins CONTRACT 1: bodyTypeFromLabel and
// bodyTypeToLabel map the new Form/Multipart labels to/from the model types, and
// the body-type Select options list both labels.
func TestUI_bfuiBTBodyTypeLabelsRoundTrip(t *testing.T) {
	// Label → type.
	if got := bodyTypeFromLabel("Form"); got != model.BodyForm {
		t.Fatalf("bodyTypeFromLabel(%q) = %q, want %q", "Form", got, model.BodyForm)
	}
	if got := bodyTypeFromLabel("Multipart"); got != model.BodyMultipart {
		t.Fatalf("bodyTypeFromLabel(%q) = %q, want %q", "Multipart", got, model.BodyMultipart)
	}

	// Type → label.
	if got := bodyTypeToLabel(model.BodyForm); got != "Form" {
		t.Fatalf("bodyTypeToLabel(BodyForm) = %q, want %q", got, "Form")
	}
	if got := bodyTypeToLabel(model.BodyMultipart); got != "Multipart" {
		t.Fatalf("bodyTypeToLabel(BodyMultipart) = %q, want %q", got, "Multipart")
	}

	// Full label↔type round-trip for both new types.
	for _, tt := range []struct {
		label string
		typ   model.BodyType
	}{
		{"Form", model.BodyForm},
		{"Multipart", model.BodyMultipart},
	} {
		if got := bodyTypeToLabel(bodyTypeFromLabel(tt.label)); got != tt.label {
			t.Fatalf("label round-trip %q -> %q", tt.label, got)
		}
		if got := bodyTypeFromLabel(bodyTypeToLabel(tt.typ)); got != tt.typ {
			t.Fatalf("type round-trip %q -> %q", tt.typ, got)
		}
	}

	// Both labels appear as Select options.
	if !bfuiBTContains(bodyTypeLabels, "Form") {
		t.Fatalf("bodyTypeLabels = %v, missing %q", bodyTypeLabels, "Form")
	}
	if !bfuiBTContains(bodyTypeLabels, "Multipart") {
		t.Fatalf("bodyTypeLabels = %v, missing %q", bodyTypeLabels, "Multipart")
	}
}

// bfuiBTSeed is the shared seeding for the table value() tests: an ordered set of
// fields exercising order, an empty row (to be dropped), a disabled-with-content
// row (to be kept), and a file row (IsFile + Filename).
func bfuiBTSeed() []model.FormField {
	return []model.FormField{
		{Key: "alpha", Value: "1", Enabled: true},
		{Key: "", Value: "", Enabled: true},                                                    // fully empty -> dropped
		{Key: "beta", Value: "2", Enabled: false},                                              // disabled w/ content -> kept
		{Key: "upload", Value: "/tmp/pic.png", Enabled: true, IsFile: true, Filename: "p.png"}, // file row
	}
}

// TestUI_bfuiBTFieldTableValueMultipart pins CONTRACT 2 (multipart=true): value()
// preserves order, drops the fully-empty row, keeps the disabled-with-content
// row, and emits IsFile/Filename for the file row.
func TestUI_bfuiBTFieldTableValueMultipart(t *testing.T) {
	test.NewApp()

	tbl := newFormFieldTable(bfuiBTSeed(), true, nil)
	got := tbl.value()

	want := []model.FormField{
		{Key: "alpha", Value: "1", Enabled: true},
		{Key: "beta", Value: "2", Enabled: false},
		{Key: "upload", Value: "/tmp/pic.png", Enabled: true, IsFile: true, Filename: "p.png"},
	}
	if len(got) != len(want) {
		t.Fatalf("value() len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("value()[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

// TestUI_bfuiBTFieldTableValueForm pins CONTRACT 2 (multipart=false): order/empty/
// disabled handling is identical, but IsFile/Filename are NOT set even though the
// seed marked the file row IsFile=true with a Filename.
func TestUI_bfuiBTFieldTableValueForm(t *testing.T) {
	test.NewApp()

	tbl := newFormFieldTable(bfuiBTSeed(), false, nil)
	got := tbl.value()

	want := []model.FormField{
		{Key: "alpha", Value: "1", Enabled: true},
		{Key: "beta", Value: "2", Enabled: false},
		{Key: "upload", Value: "/tmp/pic.png", Enabled: true}, // IsFile/Filename NOT set in form mode
	}
	if len(got) != len(want) {
		t.Fatalf("value() len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("value()[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
	// Explicitly assert no file metadata leaked through in form mode.
	for i, f := range got {
		if f.IsFile || f.Filename != "" {
			t.Fatalf("form-mode value()[%d] carries file metadata: %#v", i, f)
		}
	}
}

// TestUI_bfuiBTFieldTableTreeReachable pins CONTRACT 3: the table's laid-out
// widget tree (via its exported container) is reachable in a window and the
// seeded key/value entries surface as Entry widgets carrying the seeded text.
func TestUI_bfuiBTFieldTableTreeReachable(t *testing.T) {
	test.NewApp()

	tbl := newFormFieldTable(bfuiBTSeed(), true, nil)

	w := test.NewWindow(tbl.container)
	defer w.Close()
	w.Resize(fyne.NewSize(600, 400))

	// Collect the text of every Entry in the laid-out tree.
	seen := map[string]bool{}
	walkObjects(fyne.CurrentApp(), w.Canvas().Content(), func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok {
			seen[e.Text] = true
		}
	})

	// Every non-empty seeded key/value should be reachable as an Entry's text.
	for _, want := range []string{"alpha", "1", "beta", "2", "upload", "/tmp/pic.png"} {
		if !seen[want] {
			t.Fatalf("seeded entry text %q not reachable in laid-out tree; saw %v", want, seen)
		}
	}
}

// TestUI_bfuiBTRequestTabCurrentForm pins the requestTab-level round-trip: a tab
// built for a Form (and Multipart) Request seeds the field table from Body.Fields
// and current() returns Type + Fields (Content empty); JSON still returns
// Type + Content.
func TestUI_bfuiBTRequestTabCurrentForm(t *testing.T) {
	formFields := []model.FormField{
		{Key: "a", Value: "1", Enabled: true},
		{Key: "b", Value: "2", Enabled: false},
	}

	t.Run("form", func(t *testing.T) {
		rt := newRequestTabForTest(t, model.Request{
			Method: model.MethodPost,
			URL:    "https://api.com/f",
			Body:   model.Body{Type: model.BodyForm, Fields: formFields},
		})
		cur := rt.current()
		if cur.Body.Type != model.BodyForm {
			t.Fatalf("current().Body.Type = %q, want %q", cur.Body.Type, model.BodyForm)
		}
		if cur.Body.Content != "" {
			t.Fatalf("current().Body.Content = %q, want empty for form body", cur.Body.Content)
		}
		if len(cur.Body.Fields) != len(formFields) {
			t.Fatalf("current().Body.Fields = %#v, want %#v", cur.Body.Fields, formFields)
		}
		for i := range formFields {
			if cur.Body.Fields[i] != formFields[i] {
				t.Fatalf("current().Body.Fields[%d] = %#v, want %#v", i, cur.Body.Fields[i], formFields[i])
			}
		}
	})

	t.Run("multipart", func(t *testing.T) {
		mpFields := []model.FormField{
			{Key: "text", Value: "hi", Enabled: true},
			{Key: "file", Value: "/tmp/x.bin", Enabled: true, IsFile: true, Filename: "x.bin"},
		}
		rt := newRequestTabForTest(t, model.Request{
			Method: model.MethodPost,
			URL:    "https://api.com/m",
			Body:   model.Body{Type: model.BodyMultipart, Fields: mpFields},
		})
		cur := rt.current()
		if cur.Body.Type != model.BodyMultipart {
			t.Fatalf("current().Body.Type = %q, want %q", cur.Body.Type, model.BodyMultipart)
		}
		if cur.Body.Content != "" {
			t.Fatalf("current().Body.Content = %q, want empty for multipart body", cur.Body.Content)
		}
		if len(cur.Body.Fields) != len(mpFields) {
			t.Fatalf("current().Body.Fields = %#v, want %#v", cur.Body.Fields, mpFields)
		}
		for i := range mpFields {
			if cur.Body.Fields[i] != mpFields[i] {
				t.Fatalf("current().Body.Fields[%d] = %#v, want %#v", i, cur.Body.Fields[i], mpFields[i])
			}
		}
	})

	t.Run("json_unchanged", func(t *testing.T) {
		rt := newRequestTabForTest(t, model.Request{
			Method: model.MethodPost,
			URL:    "https://api.com/j",
			Body:   model.Body{Type: model.BodyJSON, Content: `{"k":1}`},
		})
		cur := rt.current()
		if cur.Body.Type != model.BodyJSON {
			t.Fatalf("current().Body.Type = %q, want %q", cur.Body.Type, model.BodyJSON)
		}
		if cur.Body.Content != `{"k":1}` {
			t.Fatalf("current().Body.Content = %q, want JSON content carried as before", cur.Body.Content)
		}
		if len(cur.Body.Fields) != 0 {
			t.Fatalf("current().Body.Fields = %#v, want none for JSON body", cur.Body.Fields)
		}
	})
}
