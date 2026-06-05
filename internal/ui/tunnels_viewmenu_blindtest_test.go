package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/ultramcu/yon/internal/model"
)

// Blind tests for issue #15: the "Tunnels…" menu item moves from the Collection
// menu to the View menu (alongside Variables + Request Log). Its action is
// unchanged: w.app.openTunnelsWindow(w). Written from the contract, blind to the
// Dev's edit.
//
// Fail-before (on the pre-fix tree where Tunnels… still sits under Collection):
//   - TestTunnelsItemUnderViewNotCollection FAILS on "View menu has no Tunnels…"
//     and on "Collection menu still contains Tunnels…".
//   - TestTunnelsViewActionWired FAILS at the lookup ("View ▸ Tunnels… not found").

const tunnelsLabel = "Tunnels…"

// menuByName returns the top-level menu with the given Label, or nil.
func menuByName(mm *fyne.MainMenu, name string) *fyne.Menu {
	if mm == nil {
		return nil
	}
	for _, m := range mm.Items {
		if m != nil && m.Label == name {
			return m
		}
	}
	return nil
}

// itemByLabel returns the menu item with the given Label within menu, or nil.
func itemByLabel(menu *fyne.Menu, label string) *fyne.MenuItem {
	if menu == nil {
		return nil
	}
	for _, it := range menu.Items {
		if it != nil && it.Label == label {
			return it
		}
	}
	return nil
}

// buildTunnelsTestWindow builds a Window via the smoke pattern shared by the
// other ui menu tests.
func buildTunnelsTestWindow(t *testing.T) *Window {
	t.Helper()
	fyneApp := test.NewApp()
	app := New(fyneApp)
	return app.OpenCollectionWindow(model.NewCollection("Tunnels"), "/tmp/tunnels.yon")
}

// TestTunnelsItemUnderViewNotCollection pins the relocation: Tunnels… lives in
// View (with a non-nil action) and no longer appears in Collection, while View
// keeps its existing Variables + Request Log items.
func TestTunnelsItemUnderViewNotCollection(t *testing.T) {
	w := buildTunnelsTestWindow(t)
	mm := w.buildMainMenu()

	view := menuByName(mm, "View")
	if view == nil {
		t.Fatal("no View menu in the main menu")
	}
	coll := menuByName(mm, "Collection")
	if coll == nil {
		t.Fatal("no Collection menu in the main menu")
	}

	// View must contain Tunnels… with a wired action.
	tunnelsInView := itemByLabel(view, tunnelsLabel)
	if tunnelsInView == nil {
		t.Fatalf("View menu has no %q item: it was not moved into View", tunnelsLabel)
	}
	if tunnelsInView.Action == nil {
		t.Fatalf("View ▸ %q has a nil Action: clicking it would do nothing", tunnelsLabel)
	}

	// Collection must NOT contain Tunnels… any more.
	if itemByLabel(coll, tunnelsLabel) != nil {
		t.Fatalf("Collection menu still contains a %q item: it was not removed", tunnelsLabel)
	}

	// Sanity: View still carries its original items.
	if itemByLabel(view, "Variables") == nil {
		t.Error("View menu lost its Variables item")
	}
	if itemByLabel(view, "Request Log") == nil {
		t.Error("View menu lost its Request Log item")
	}
}

// TestTunnelsViewActionWired invokes the View ▸ Tunnels… action and asserts it
// does not panic (it opens the Tunnels window via w.app.openTunnelsWindow(w)).
func TestTunnelsViewActionWired(t *testing.T) {
	w := buildTunnelsTestWindow(t)
	mm := w.buildMainMenu()

	view := menuByName(mm, "View")
	if view == nil {
		t.Fatal("no View menu in the main menu")
	}
	item := itemByLabel(view, tunnelsLabel)
	if item == nil {
		t.Fatalf("View ▸ %q not found", tunnelsLabel)
	}
	if item.Action == nil {
		t.Fatalf("View ▸ %q has a nil Action", tunnelsLabel)
	}

	// Must not panic — opens the Tunnels window.
	item.Action()
}
