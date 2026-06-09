package store

// Blind CONTRACT tests for issue #35 (inline environments), store lane.
//
// Written from the doc-comment contract of inline_env.go / environments.go
// ONLY — not from the implementation. They pin the merge/migration/no-double-
// storage properties so they stay green when the skeleton is hardened and would
// catch a regression. Fixture names are prefixed "ieBT" to stay unique.

import (
	"sort"
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// ieBTEnv builds an Environment with a single enabled, non-secret variable.
func ieBTEnv(name, key, value string) model.Environment {
	return model.Environment{
		Name:      name,
		Variables: []model.Variable{{Key: key, Value: value, Enabled: true}},
	}
}

// ieBTVarValue returns the value of variable key in env, or "" plus false when
// the variable is absent.
func ieBTVarValue(env model.Environment, key string) (string, bool) {
	for _, v := range env.Variables {
		if v.Key == key {
			return v.Value, true
		}
	}
	return "", false
}

// ieBTFind returns the (first) environment named name from envs, or false.
func ieBTFind(envs []model.Environment, name string) (model.Environment, bool) {
	for _, e := range envs {
		if e.Name == name {
			return e, true
		}
	}
	return model.Environment{}, false
}

// ieBTHasName reports whether any environment in envs is named name.
func ieBTHasName(envs []model.Environment, name string) bool {
	_, ok := ieBTFind(envs, name)
	return ok
}

// ieBTCountName counts how many environments in envs are named name.
func ieBTCountName(envs []model.Environment, name string) int {
	n := 0
	for _, e := range envs {
		if e.Name == name {
			n++
		}
	}
	return n
}

// ieBTSavedColl saves an empty collection at dir/api.yon and returns the path
// and the in-memory collection.
func ieBTSavedColl(t *testing.T) (string, model.Collection) {
	t.Helper()
	dir := t.TempDir()
	collPath := dir + "/api.yon"
	c := model.NewCollection("ieBT")
	if err := Save(collPath, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return collPath, c
}

// 1. Inline round-trip: SetEnvironmentInline persists into the .yon and surfaces
// via Load + CollectionEnvironments with inline["Test"]==true.
func TestIeBTInlineRoundTrip(t *testing.T) {
	collPath, c := ieBTSavedColl(t)

	envTest := ieBTEnv("Test", "host", "test.example.com")
	if err := SetEnvironmentInline(collPath, &c, envTest); err != nil {
		t.Fatalf("SetEnvironmentInline: %v", err)
	}

	// On-disk truth: reload and confirm the env is stored inline in the .yon.
	reloaded, err := Load(collPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := ieBTFind(reloaded.Environments, "Test")
	if !ok {
		t.Fatalf("reloaded.Environments missing %q; got %+v", "Test", reloaded.Environments)
	}
	if v, ok := ieBTVarValue(got, "host"); !ok || v != "test.example.com" {
		t.Fatalf("inline var host = %q,%v; want %q", v, ok, "test.example.com")
	}

	// CollectionEnvironments surfaces it and flags it inline.
	all, inline, err := CollectionEnvironments(collPath, reloaded)
	if err != nil {
		t.Fatalf("CollectionEnvironments: %v", err)
	}
	if !ieBTHasName(all, "Test") {
		t.Fatalf("CollectionEnvironments missing %q; got %+v", "Test", all)
	}
	if !inline["Test"] {
		t.Fatalf("inline[%q] = %v; want true", "Test", inline["Test"])
	}
}

// 2. Merge inline+sibling: both appear, sorted by Name, with correct inline flags.
func TestIeBTMergeInlineAndSibling(t *testing.T) {
	collPath, c := ieBTSavedColl(t)

	if err := SetEnvironmentInline(collPath, &c, ieBTEnv("Inline", "k", "iv")); err != nil {
		t.Fatalf("SetEnvironmentInline: %v", err)
	}
	if err := SaveEnvironment(collPath, ieBTEnv("Sib", "k", "sv")); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}

	all, inline, err := CollectionEnvironments(collPath, c)
	if err != nil {
		t.Fatalf("CollectionEnvironments: %v", err)
	}

	if !ieBTHasName(all, "Inline") || !ieBTHasName(all, "Sib") {
		t.Fatalf("want both Inline and Sib; got %+v", all)
	}
	if !inline["Inline"] {
		t.Fatalf("inline[%q] = %v; want true", "Inline", inline["Inline"])
	}
	if inline["Sib"] {
		t.Fatalf("inline[%q] = %v; want false", "Sib", inline["Sib"])
	}

	// Property the minimal skeleton might get wrong: results sorted by Name.
	names := make([]string, len(all))
	for i, e := range all {
		names[i] = e.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("CollectionEnvironments not sorted by Name: %v", names)
	}
}

// 3. Migration sibling→inline: env moves into the .yon and the sibling file is
// gone. No double-storage.
func TestIeBTMigrateSiblingToInline(t *testing.T) {
	collPath, c := ieBTSavedColl(t)

	envProd := ieBTEnv("Prod", "host", "prod.example.com")
	if err := SaveEnvironment(collPath, envProd); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	// Sanity: it really is a sibling first.
	if sibs, err := LoadEnvironments(collPath); err != nil {
		t.Fatalf("LoadEnvironments (pre): %v", err)
	} else if !ieBTHasName(sibs, "Prod") {
		t.Fatalf("setup: %q not a sibling before migration; got %+v", "Prod", sibs)
	}

	if err := SetEnvironmentInline(collPath, &c, envProd); err != nil {
		t.Fatalf("SetEnvironmentInline: %v", err)
	}

	// In-memory *coll now holds it inline.
	if !ieBTHasName(c.Environments, "Prod") {
		t.Fatalf("after migration c.Environments missing %q; got %+v", "Prod", c.Environments)
	}
	// On-disk truth: reload the .yon and confirm inline persistence.
	reloaded, err := Load(collPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ieBTHasName(reloaded.Environments, "Prod") {
		t.Fatalf("reloaded .yon missing inline %q; got %+v", "Prod", reloaded.Environments)
	}
	// Sibling file must be gone (the migration removed it).
	sibs, err := LoadEnvironments(collPath)
	if err != nil {
		t.Fatalf("LoadEnvironments (post): %v", err)
	}
	if ieBTHasName(sibs, "Prod") {
		t.Fatalf("sibling %q still present after sibling→inline migration: %+v", "Prod", sibs)
	}

	// No double-storage: CollectionEnvironments returns exactly one "Prod".
	all, _, err := CollectionEnvironments(collPath, reloaded)
	if err != nil {
		t.Fatalf("CollectionEnvironments: %v", err)
	}
	if n := ieBTCountName(all, "Prod"); n != 1 {
		t.Fatalf("CollectionEnvironments has %d copies of %q; want 1", n, "Prod")
	}
}

// 4. Migration inline→sibling: env leaves the .yon and becomes a sibling. No
// double-storage.
func TestIeBTMigrateInlineToSibling(t *testing.T) {
	collPath, c := ieBTSavedColl(t)

	envX := ieBTEnv("X", "host", "x.example.com")
	if err := SetEnvironmentInline(collPath, &c, envX); err != nil {
		t.Fatalf("SetEnvironmentInline: %v", err)
	}
	if err := SetEnvironmentSibling(collPath, &c, envX); err != nil {
		t.Fatalf("SetEnvironmentSibling: %v", err)
	}

	// In-memory *coll no longer holds it inline.
	if ieBTHasName(c.Environments, "X") {
		t.Fatalf("after inline→sibling c.Environments still has %q; got %+v", "X", c.Environments)
	}
	// On-disk truth: reload the .yon, inline must be gone.
	reloaded, err := Load(collPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ieBTHasName(reloaded.Environments, "X") {
		t.Fatalf("reloaded .yon still has inline %q; got %+v", "X", reloaded.Environments)
	}
	// Sibling file now exists.
	sibs, err := LoadEnvironments(collPath)
	if err != nil {
		t.Fatalf("LoadEnvironments: %v", err)
	}
	if !ieBTHasName(sibs, "X") {
		t.Fatalf("sibling %q missing after inline→sibling migration: %+v", "X", sibs)
	}

	// No double-storage: exactly one "X" overall.
	all, _, err := CollectionEnvironments(collPath, reloaded)
	if err != nil {
		t.Fatalf("CollectionEnvironments: %v", err)
	}
	if n := ieBTCountName(all, "X"); n != 1 {
		t.Fatalf("CollectionEnvironments has %d copies of %q; want 1", n, "X")
	}
}

// 5. Inline wins on a name collision. We write each side DIRECTLY (inline via the
// model field + Save, sibling via SaveEnvironment) so both copies of "Dup" exist
// on disk, then assert CollectionEnvironments returns one whose value is inline.
func TestIeBTInlineWinsOnCollision(t *testing.T) {
	collPath, c := ieBTSavedColl(t)

	// Sibling copy first.
	if err := SaveEnvironment(collPath, ieBTEnv("Dup", "v", "SIB")); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	// Inline copy written directly into the model + persisted, bypassing the
	// migration helper so BOTH copies coexist on disk.
	c.Environments = append(c.Environments, ieBTEnv("Dup", "v", "INLINE"))
	if err := Save(collPath, c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	all, inline, err := CollectionEnvironments(collPath, c)
	if err != nil {
		t.Fatalf("CollectionEnvironments: %v", err)
	}
	if n := ieBTCountName(all, "Dup"); n != 1 {
		t.Fatalf("CollectionEnvironments has %d copies of %q; want exactly 1 (inline wins)", n, "Dup")
	}
	got, _ := ieBTFind(all, "Dup")
	if v, ok := ieBTVarValue(got, "v"); !ok || v != "INLINE" {
		t.Fatalf("collision winner var v = %q,%v; want %q (inline wins)", v, ok, "INLINE")
	}
	if !inline["Dup"] {
		t.Fatalf("inline[%q] = %v; want true", "Dup", inline["Dup"])
	}
}

// 6. DeleteInlineEnvironment removes the inline env from the model and from the
// unified set.
func TestIeBTDeleteInlineEnvironment(t *testing.T) {
	collPath, c := ieBTSavedColl(t)

	if err := SetEnvironmentInline(collPath, &c, ieBTEnv("Gone", "k", "v")); err != nil {
		t.Fatalf("SetEnvironmentInline: %v", err)
	}
	if !ieBTHasName(c.Environments, "Gone") {
		t.Fatalf("setup: %q not inline before delete; got %+v", "Gone", c.Environments)
	}

	if err := DeleteInlineEnvironment(collPath, &c, "Gone"); err != nil {
		t.Fatalf("DeleteInlineEnvironment: %v", err)
	}

	if ieBTHasName(c.Environments, "Gone") {
		t.Fatalf("c.Environments still has %q after delete; got %+v", "Gone", c.Environments)
	}
	// On-disk truth too.
	reloaded, err := Load(collPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ieBTHasName(reloaded.Environments, "Gone") {
		t.Fatalf("reloaded .yon still has %q after delete; got %+v", "Gone", reloaded.Environments)
	}

	all, _, err := CollectionEnvironments(collPath, reloaded)
	if err != nil {
		t.Fatalf("CollectionEnvironments: %v", err)
	}
	if ieBTHasName(all, "Gone") {
		t.Fatalf("CollectionEnvironments still has %q after delete; got %+v", "Gone", all)
	}
}

// 7. ActiveEnvironment is untouched by an inline↔sibling migration round-trip.
func TestIeBTActiveEnvironmentUntouched(t *testing.T) {
	collPath, c := ieBTSavedColl(t)
	c.ActiveEnvironment = "Test"

	envTest := ieBTEnv("Test", "k", "v")
	if err := SetEnvironmentInline(collPath, &c, envTest); err != nil {
		t.Fatalf("SetEnvironmentInline: %v", err)
	}
	if c.ActiveEnvironment != "Test" {
		t.Fatalf("ActiveEnvironment = %q after inline; want %q", c.ActiveEnvironment, "Test")
	}

	if err := SetEnvironmentSibling(collPath, &c, envTest); err != nil {
		t.Fatalf("SetEnvironmentSibling: %v", err)
	}
	if c.ActiveEnvironment != "Test" {
		t.Fatalf("ActiveEnvironment = %q after sibling; want %q", c.ActiveEnvironment, "Test")
	}

	// On-disk truth survives the round-trip too.
	reloaded, err := Load(collPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.ActiveEnvironment != "Test" {
		t.Fatalf("reloaded ActiveEnvironment = %q; want %q", reloaded.ActiveEnvironment, "Test")
	}
}
