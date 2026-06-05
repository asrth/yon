package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/ultramcu/yon/internal/model"
)

// TestSidebarHeader_FilenameFallbackWhenNameEmpty pins that a Collection with an
// empty Name, opened from a file, shows the file's base name in the sidebar
// header — not "Untitled". Regression test for the header always reading
// "Untitled" for name-less .yon files (the window title already did this; the
// header did not).
func TestSidebarHeader_FilenameFallbackWhenNameEmpty(t *testing.T) {
	a := test.NewApp()
	w := newWindow(New(a), model.NewCollection(""), "/tmp/api.yon")
	t.Cleanup(w.win.Close)

	if got := w.sidebarTitle.Text; got != "api.yon" {
		t.Fatalf("sidebar header = %q, want %q", got, "api.yon")
	}
}

// TestSidebarHeader_UntitledWhenNoNameNoPath pins the final fallback: an unsaved,
// name-less Collection still reads "Untitled".
func TestSidebarHeader_UntitledWhenNoNameNoPath(t *testing.T) {
	a := test.NewApp()
	w := newWindow(New(a), model.NewCollection(""), "")
	t.Cleanup(w.win.Close)

	if got := w.sidebarTitle.Text; got != "Untitled" {
		t.Fatalf("sidebar header = %q, want %q", got, "Untitled")
	}
}

// TestCollectionDisplayName pins the precedence of the shared display-name
// helper used by both the title bar and the sidebar header.
func TestCollectionDisplayName(t *testing.T) {
	cases := []struct {
		name, path, want string
	}{
		{"My API", "/tmp/api.yon", "My API"}, // explicit name wins
		{"", "/tmp/api.yon", "api.yon"},      // empty name → file base
		{"", "", "Untitled"},                 // nothing → Untitled
	}
	for _, c := range cases {
		if got := collectionDisplayName(c.name, c.path); got != c.want {
			t.Errorf("collectionDisplayName(%q,%q) = %q, want %q", c.name, c.path, got, c.want)
		}
	}
}
