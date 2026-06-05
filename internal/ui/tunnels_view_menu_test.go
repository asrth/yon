package ui

import (
	"testing"

	"fyne.io/fyne/v2"

	"github.com/ultramcu/yon/internal/model"
)

// TestTunnelsMenuMovedToView pins issue #15: the "Tunnels…" item lives under the
// View menu (alongside Variables and Request Log) and no longer under Collection.
// The action is unchanged — it still opens the Tunnels window via
// w.app.openTunnelsWindow — so invoking the View item must not panic.
func TestTunnelsMenuMovedToView(t *testing.T) {
	w := newScopeWindow(t, model.NewCollection("T"))

	menuByLabel := func(main *fyne.MainMenu, label string) *fyne.Menu {
		for _, m := range main.Items {
			if m.Label == label {
				return m
			}
		}
		return nil
	}
	itemByLabel := func(m *fyne.Menu, label string) *fyne.MenuItem {
		if m == nil {
			return nil
		}
		for _, it := range m.Items {
			if it.Label == label {
				return it
			}
		}
		return nil
	}

	main := w.buildMainMenu()

	collMenu := menuByLabel(main, "Collection")
	if collMenu == nil {
		t.Fatal("no Collection menu")
	}
	viewMenu := menuByLabel(main, "View")
	if viewMenu == nil {
		t.Fatal("no View menu")
	}

	// Collection must NOT list Tunnels… anymore.
	if itemByLabel(collMenu, "Tunnels…") != nil {
		t.Error("Collection menu still lists Tunnels…; it should have moved to View")
	}

	// View must now list Tunnels…, with a non-nil wired action.
	tunnels := itemByLabel(viewMenu, "Tunnels…")
	if tunnels == nil {
		t.Fatal("View menu does not list Tunnels…")
	}
	if tunnels.Action == nil {
		t.Fatal("View ▸ Tunnels… has a nil Action")
	}

	// Invoking the action must open the Tunnels window without panicking
	// (action unchanged — still w.app.openTunnelsWindow(w)).
	tunnels.Action()
}
