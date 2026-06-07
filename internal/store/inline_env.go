package store

import (
	"sort"

	"github.com/ultramcu/yon/internal/model"
)

// This file is the contract for issue #35 — environments stored INLINE in the
// .yon (model.Collection.Environments) as an alternative to the default
// sibling-file storage. The four functions below are the shared API the UI
// drives; the Dev A (store) lane owns and hardens them. Inline environments
// embed ALL their values (including secret-variable values) directly in the
// committable .yon — the UI warns about this when the user opts in.

// CollectionEnvironments returns the unified environment set for a collection:
// its inline environments (coll.Environments) merged with its sibling-file
// environments (LoadEnvironments), sorted by Name. The returned map reports
// which names are stored inline. On a name present in BOTH, the inline one wins
// (an environment should live in exactly one place; this is a safety net).
func CollectionEnvironments(collPath string, coll model.Collection) ([]model.Environment, map[string]bool, error) {
	inline := map[string]bool{}
	var out []model.Environment
	for _, e := range coll.Environments {
		if inline[e.Name] {
			continue
		}
		inline[e.Name] = true
		out = append(out, e)
	}
	sib, err := LoadEnvironments(collPath)
	if err != nil {
		return out, inline, err
	}
	for _, e := range sib {
		if inline[e.Name] {
			continue // inline wins
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, inline, nil
}

// SetEnvironmentInline stores env INSIDE the .yon: it adds or replaces env in
// coll.Environments, removes any sibling file for that name (migration
// sibling→inline), and writes the collection to disk. It mutates *coll.
func SetEnvironmentInline(collPath string, coll *model.Collection, env model.Environment) error {
	next := coll.Environments[:0:0]
	for _, e := range coll.Environments {
		if e.Name != env.Name {
			next = append(next, e)
		}
	}
	next = append(next, env)
	coll.Environments = next
	if err := Save(collPath, *coll); err != nil {
		return err
	}
	// Drop any sibling copy (and its .env secrets) so the env lives in one place.
	return DeleteEnvironment(collPath, env.Name)
}

// SetEnvironmentSibling stores env as a sibling file (the default; secret values
// go to the gitignored .env): it writes the sibling file, removes env from
// coll.Environments (migration inline→sibling), and writes the collection. It
// mutates *coll.
func SetEnvironmentSibling(collPath string, coll *model.Collection, env model.Environment) error {
	if err := SaveEnvironment(collPath, env); err != nil {
		return err
	}
	next := coll.Environments[:0:0]
	for _, e := range coll.Environments {
		if e.Name != env.Name {
			next = append(next, e)
		}
	}
	coll.Environments = next
	return Save(collPath, *coll)
}

// DeleteInlineEnvironment removes an inline environment by name and writes the
// collection. It mutates *coll. (Sibling environments are removed with
// DeleteEnvironment.)
func DeleteInlineEnvironment(collPath string, coll *model.Collection, name string) error {
	next := coll.Environments[:0:0]
	for _, e := range coll.Environments {
		if e.Name != name {
			next = append(next, e)
		}
	}
	coll.Environments = next
	return Save(collPath, *coll)
}
