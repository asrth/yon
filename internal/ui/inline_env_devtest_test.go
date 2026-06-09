package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/ultramcu/yon/internal/model"
	"github.com/ultramcu/yon/internal/store"
)

// openSavedWindow builds a Window backed by a freshly saved .yon on disk, so the
// store.SetEnvironment*/Delete* calls in persistEnvironments have a real path to
// write to. t.Cleanup closes the underlying window.
func openSavedWindow(t *testing.T, coll model.Collection) *Window {
	t.Helper()
	path := saveCollection(t, coll, "c.yon")
	a := New(test.NewApp())
	w := a.OpenCollectionWindow(coll, path)
	t.Cleanup(w.win.Close)
	return w
}

// TestPersistEnvironments_InlineRouting verifies that persistEnvironments routes
// a checked row to inline storage (embedded in coll.Environments) and an
// unchecked row to a sibling file, and that loadEnvironments then reports the
// inline name in w.inlineEnvs.
func TestPersistEnvironments_InlineRouting(t *testing.T) {
	w := openSavedWindow(t, model.NewCollection("C"))

	working := []model.Environment{
		{Name: "prod", Variables: []model.Variable{{Key: "k", Value: "v", Enabled: true}}},
		{Name: "dev", Variables: []model.Variable{{Key: "k", Value: "d", Enabled: true}}},
	}
	// prod inline (checked), dev sibling (unchecked).
	w.persistEnvironments(working, []bool{true, false}, nil)

	if !w.inlineEnvs["prod"] {
		t.Fatalf("prod should be inline after persist; inlineEnvs=%v", w.inlineEnvs)
	}
	if w.inlineEnvs["dev"] {
		t.Fatalf("dev should be sibling, not inline; inlineEnvs=%v", w.inlineEnvs)
	}

	// prod must live in the .yon's inline list, NOT as a sibling file.
	var inlineNames []string
	for _, e := range w.coll.Environments {
		inlineNames = append(inlineNames, e.Name)
	}
	if len(inlineNames) != 1 || inlineNames[0] != "prod" {
		t.Fatalf("coll.Environments = %v, want [prod]", inlineNames)
	}

	// dev must be a sibling file; prod must NOT be.
	sib, err := store.LoadEnvironments(w.path)
	if err != nil {
		t.Fatalf("LoadEnvironments: %v", err)
	}
	var sibNames []string
	for _, e := range sib {
		sibNames = append(sibNames, e.Name)
	}
	if len(sibNames) != 1 || sibNames[0] != "dev" {
		t.Fatalf("sibling envs = %v, want [dev]", sibNames)
	}

	// The merged set both selectors read still has both.
	if len(w.envs) != 2 {
		t.Fatalf("merged envs = %d, want 2 (%v)", len(w.envs), w.envs)
	}
}

// TestPersistEnvironments_ToggleMovesEnvironment verifies that flipping the
// checkbox on an existing environment MOVES it between locations: a sibling env
// toggled to inline disappears from the sibling files and appears in
// coll.Environments, and vice versa.
func TestPersistEnvironments_ToggleMovesEnvironment(t *testing.T) {
	w := openSavedWindow(t, model.NewCollection("C"))

	env := model.Environment{Name: "stg", Variables: []model.Variable{{Key: "k", Value: "v", Enabled: true}}}

	// Start as a sibling.
	w.persistEnvironments([]model.Environment{env}, []bool{false}, nil)
	if w.inlineEnvs["stg"] {
		t.Fatal("stg should start as sibling")
	}

	// Toggle to inline — must move into the .yon and out of the sibling files.
	w.persistEnvironments([]model.Environment{env}, []bool{true}, nil)
	if !w.inlineEnvs["stg"] {
		t.Fatal("stg should be inline after toggling on")
	}
	sib, _ := store.LoadEnvironments(w.path)
	if len(sib) != 0 {
		t.Fatalf("sibling files should be empty after move to inline, got %v", sib)
	}
	if len(w.coll.Environments) != 1 || w.coll.Environments[0].Name != "stg" {
		t.Fatalf("coll.Environments = %v, want [stg]", w.coll.Environments)
	}

	// Toggle back to sibling — must move out of the .yon.
	w.persistEnvironments([]model.Environment{env}, []bool{false}, nil)
	if w.inlineEnvs["stg"] {
		t.Fatal("stg should be sibling after toggling off")
	}
	if len(w.coll.Environments) != 0 {
		t.Fatalf("coll.Environments should be empty after move to sibling, got %v", w.coll.Environments)
	}
	sib, _ = store.LoadEnvironments(w.path)
	if len(sib) != 1 || sib[0].Name != "stg" {
		t.Fatalf("sibling envs = %v, want [stg]", sib)
	}
}

// TestPersistEnvironments_DeleteUsesPriorLocation verifies that a deleted inline
// environment is removed from the .yon (DeleteInlineEnvironment path) using the
// pre-edit w.inlineEnvs state, not left orphaned.
func TestPersistEnvironments_DeleteUsesPriorLocation(t *testing.T) {
	w := openSavedWindow(t, model.NewCollection("C"))

	env := model.Environment{Name: "gone", Variables: []model.Variable{{Key: "k", Value: "v", Enabled: true}}}
	w.persistEnvironments([]model.Environment{env}, []bool{true}, nil)
	if !w.inlineEnvs["gone"] {
		t.Fatal("setup: gone should be inline")
	}

	// Simulate the manager's Delete: queue the name, persist an empty working set.
	w.pendingEnvDeletes = []string{"gone"}
	w.persistEnvironments(nil, nil, nil)

	if len(w.coll.Environments) != 0 {
		t.Fatalf("inline env not removed: %v", w.coll.Environments)
	}
	if w.inlineEnvs["gone"] {
		t.Fatalf("gone still reported inline after delete: %v", w.inlineEnvs)
	}
	if len(w.envs) != 0 {
		t.Fatalf("merged envs should be empty, got %v", w.envs)
	}
}
