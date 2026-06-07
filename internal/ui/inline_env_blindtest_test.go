package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/ultramcu/yon/internal/model"
	"github.com/ultramcu/yon/internal/store"
)

// Blind UI-lane tests for issue #35 (inline environments), written from the
// CONTRACT only. An environment stored INLINE in the .yon (coll.Environments)
// must be visible in the window exactly like a sibling-file environment: it
// shows up in the selector, and it resolves as the active environment. The
// window is built WITH a path because inline-vs-sibling is only meaningful for
// a saved collection (an untitled collection has no sibling files and no
// on-disk .yon to carry inline environments).
//
// Fail-before: on current code loadEnvironments() reads ONLY sibling files
// (store.LoadEnvironments) and ignores coll.Environments, so an inline
// environment is invisible — tests 1 and 2 fail. They pass once the UI lane
// wires store.CollectionEnvironments into loadEnvironments.

// ieuiBTInlineEnv is the shared inline-environment fixture.
func ieuiBTInlineEnv() model.Environment {
	return model.Environment{
		Name: "Inline",
		Variables: []model.Variable{
			{Key: "baseUrl", Value: "https://inline", Enabled: true},
		},
	}
}

// TestInlineEnvironmentAppearsInSelector pins that an environment stored inline
// in the .yon (coll.Environments) is offered by the window's environment
// selector. Fail-before: loadEnvironments() only reads sibling files, so the
// inline env is absent from environmentNames().
func TestInlineEnvironmentAppearsInSelector(t *testing.T) {
	coll := model.NewCollection("ieuiBT-selector")
	coll.Environments = []model.Environment{ieuiBTInlineEnv()}
	path := saveCollection(t, coll, "ieuiBT-selector.yon")

	a := New(test.NewApp())
	w := a.OpenCollectionWindow(coll, path)
	t.Cleanup(w.win.Close)

	if !containsIeuiBT(w.environmentNames(), "Inline") {
		t.Fatalf("environmentNames() = %v, want it to contain inline env %q",
			w.environmentNames(), "Inline")
	}
}

// TestInlineActiveEnvironmentResolves pins that when the active environment is
// an inline one, activeEnv() resolves it (with its variables). Fail-before: the
// inline env is never loaded, so ActiveEnvironment "Inline" matches nothing and
// activeEnv() returns ok == false.
func TestInlineActiveEnvironmentResolves(t *testing.T) {
	coll := model.NewCollection("ieuiBT-active")
	coll.Environments = []model.Environment{ieuiBTInlineEnv()}
	coll.ActiveEnvironment = "Inline"
	path := saveCollection(t, coll, "ieuiBT-active.yon")

	a := New(test.NewApp())
	w := a.OpenCollectionWindow(coll, path)
	t.Cleanup(w.win.Close)

	env, ok := w.activeEnv()
	if !ok {
		t.Fatalf("activeEnv() ok = false, want the inline active env to resolve")
	}
	if env.Name != "Inline" {
		t.Fatalf("activeEnv() Name = %q, want %q", env.Name, "Inline")
	}
	if got := ieuiBTVarValue(env, "baseUrl"); got != "https://inline" {
		t.Fatalf("active env baseUrl = %q, want %q", got, "https://inline")
	}
}

// TestInlineAndSiblingEnvironmentsBothAppear pins the merged view: an inline
// environment AND a sibling-file environment for the same collection both show
// up in the selector. Fail-before: only the sibling env appears (the inline one
// is dropped). After the fix, loadEnvironments merges both.
func TestInlineAndSiblingEnvironmentsBothAppear(t *testing.T) {
	coll := model.NewCollection("ieuiBT-mixed")
	coll.Environments = []model.Environment{ieuiBTInlineEnv()}
	path := saveCollection(t, coll, "ieuiBT-mixed.yon")

	// Write a sibling-file environment alongside the saved collection.
	sibling := model.Environment{
		Name: "Sibling",
		Variables: []model.Variable{
			{Key: "baseUrl", Value: "https://sibling", Enabled: true},
		},
	}
	if err := store.SaveEnvironment(path, sibling); err != nil {
		t.Fatalf("seed sibling environment: %v", err)
	}

	a := New(test.NewApp())
	w := a.OpenCollectionWindow(coll, path)
	t.Cleanup(w.win.Close)

	names := w.environmentNames()
	if !containsIeuiBT(names, "Inline") {
		t.Fatalf("environmentNames() = %v, want it to contain inline env %q", names, "Inline")
	}
	if !containsIeuiBT(names, "Sibling") {
		t.Fatalf("environmentNames() = %v, want it to contain sibling env %q", names, "Sibling")
	}
}

// containsIeuiBT reports whether want is in names.
func containsIeuiBT(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// ieuiBTVarValue returns the value of the variable named key in env, or "".
func ieuiBTVarValue(env model.Environment, key string) string {
	for _, v := range env.Variables {
		if v.Key == key {
			return v.Value
		}
	}
	return ""
}
