package store

import (
	"sort"

	"github.com/ultramcu/yon/internal/model"
)

// Inline environments (issue #35) are an alternative to the default sibling-file
// storage handled by environments.go. An INLINE environment is embedded verbatim
// in coll.Environments and therefore written straight into the committable .yon by
// store.Save — INCLUDING any secret variable values. This trades the secret-safety
// of the sibling/.env scheme for portability: the whole environment travels with
// the committed file. The UI warns the user before storing secrets inline.
//
// The four functions below own the inline lane and the migration between the two
// storage locations. The single hard invariant they preserve is NO DOUBLE-STORAGE:
// a given environment Name lives in exactly one place — inline OR sibling — never
// both. Migration always commits the new location before removing the old, so a
// failure mid-migration can leave the environment in its ORIGINAL place but never
// duplicated across both.

// CollectionEnvironments returns the effective set of environments for the
// collection at collPath, merging the two storage locations:
//
//   - inline environments from coll.Environments, used verbatim (secret values
//     included, exactly as stored in the .yon); and
//   - sibling environments from LoadEnvironments(collPath), whose secret values
//     are filled in from the collection's .env.
//
// When a Name is present in BOTH places the INLINE copy wins (it is the
// authoritative committed value, and the no-double-storage invariant means a
// sibling of the same name is at most stale residue). The returned slice is
// sorted by Name with a stable order. The second return value is a set flagging
// which returned Names came from the inline location (true == inline).
//
// An empty collPath (an unsaved collection has no on-disk sibling home) yields
// just the inline environments with a nil error. Any error from LoadEnvironments
// is propagated unchanged.
func CollectionEnvironments(collPath string, coll model.Collection) ([]model.Environment, map[string]bool, error) {
	inline := map[string]bool{}

	// Index the inline environments by Name. A duplicate Name within
	// coll.Environments (which the inline mutators below never create) is
	// resolved last-wins, matching how a JSON map would collapse it.
	byName := make(map[string]model.Environment, len(coll.Environments))
	order := make([]string, 0, len(coll.Environments))
	for _, env := range coll.Environments {
		if _, seen := byName[env.Name]; !seen {
			order = append(order, env.Name)
		}
		byName[env.Name] = env
		inline[env.Name] = true
	}

	// Merge in the sibling environments. Inline wins on a name collision, so a
	// sibling whose Name already exists inline is skipped (and stays flagged
	// inline, not sibling). collPath == "" short-circuits to inline-only with a
	// nil error.
	if collPath != "" {
		siblings, err := LoadEnvironments(collPath)
		if err != nil {
			return nil, nil, err
		}
		for _, env := range siblings {
			if _, isInline := byName[env.Name]; isInline {
				continue
			}
			if _, seen := byName[env.Name]; !seen {
				order = append(order, env.Name)
			}
			byName[env.Name] = env
		}
	}

	envs := make([]model.Environment, 0, len(order))
	for _, name := range order {
		envs = append(envs, byName[name])
	}
	sort.SliceStable(envs, func(i, j int) bool { return envs[i].Name < envs[j].Name })

	return envs, inline, nil
}

// SetEnvironmentInline stores env INLINE in the collection at collPath, making
// the inline copy the single home for env.Name.
//
// It adds env to coll.Environments, or REPLACES any existing inline entry with
// the same Name (verbatim — secret values are intentionally kept). It then
// persists the collection with Save so the new inline copy is committed BEFORE
// any sibling is removed, and finally deletes any sibling file + .env secrets for
// env.Name via DeleteEnvironment (the sibling→inline migration step).
//
// DeleteEnvironment is a no-op when there is no sibling, so a freshly-created
// inline environment migrates cleanly. Ordering the Save before the delete means
// a failure can leave env in its ORIGINAL place (sibling, if it had one) but never
// in both at once. coll.ActiveEnvironment is a name only and is left untouched, so
// the active selection still resolves after migration.
//
// collPath may be "" for an unsaved collection that has not yet been Saved by the
// caller's flow; in that case only the in-memory coll.Environments is updated and
// the sibling delete is skipped (there is no on-disk home to migrate from).
func SetEnvironmentInline(collPath string, coll *model.Collection, env model.Environment) error {
	setInline(coll, env)

	if collPath == "" {
		return nil
	}

	// Commit the new inline location first.
	if err := Save(collPath, *coll); err != nil {
		return err
	}
	// Then remove any prior sibling copy (and its .env secrets). No-op when absent.
	if err := DeleteEnvironment(collPath, env.Name); err != nil {
		return err
	}
	return nil
}

// SetEnvironmentSibling stores env as a SIBLING file for the collection at
// collPath, making the sibling the single home for env.Name.
//
// It first writes the sibling via SaveEnvironment (which splits secret values out
// to the gitignored .env), so the new sibling location is committed BEFORE the
// inline copy is removed. It then drops any inline entry with the same Name from
// coll.Environments and persists the collection with Save (the inline→sibling
// migration step).
//
// Secret values that were carried inline land back in the .env via SaveEnvironment,
// so migration preserves them. Ordering the SaveEnvironment before the inline
// removal means a failure can leave env inline but never in both. collPath must be
// non-empty: an unsaved collection has nowhere to persist a sibling, and
// SaveEnvironment returns an error for "" which is propagated here (coll is left
// unchanged in that case, so nothing is lost).
func SetEnvironmentSibling(collPath string, coll *model.Collection, env model.Environment) error {
	// Commit the new sibling location (and its .env secrets) first. If this fails
	// we leave coll untouched so env stays exactly where it was (inline).
	if err := SaveEnvironment(collPath, env); err != nil {
		return err
	}
	// Then remove the inline copy and persist the collection.
	if removeInline(coll, env.Name) {
		if err := Save(collPath, *coll); err != nil {
			return err
		}
	}
	return nil
}

// DeleteInlineEnvironment removes the inline environment named name from the
// collection at collPath and persists the result with Save. It only touches the
// inline lane: a sibling environment of the same name (which the no-double-storage
// invariant means should not coexist) is left to DeleteEnvironment.
//
// It is a no-op — nil error, no write — when no inline environment has that name,
// so callers need not check first. coll.ActiveEnvironment is left untouched.
// collPath may be "" for an unsaved collection, in which case only the in-memory
// slice is updated and Save is skipped.
func DeleteInlineEnvironment(collPath string, coll *model.Collection, name string) error {
	if !removeInline(coll, name) {
		return nil
	}
	if collPath == "" {
		return nil
	}
	return Save(collPath, *coll)
}

// setInline adds env to coll.Environments or replaces the existing entry with the
// same Name (preserving slice position). env is stored verbatim.
func setInline(coll *model.Collection, env model.Environment) {
	for i := range coll.Environments {
		if coll.Environments[i].Name == env.Name {
			coll.Environments[i] = env
			return
		}
	}
	coll.Environments = append(coll.Environments, env)
}

// removeInline deletes every inline environment named name from coll.Environments,
// preserving the order of the rest, and reports whether anything was removed.
func removeInline(coll *model.Collection, name string) bool {
	out := coll.Environments[:0]
	removed := false
	for _, env := range coll.Environments {
		if env.Name == name {
			removed = true
			continue
		}
		out = append(out, env)
	}
	if !removed {
		return false
	}
	// Avoid retaining backing-array references to the dropped element(s).
	for i := len(out); i < len(coll.Environments); i++ {
		coll.Environments[i] = model.Environment{}
	}
	coll.Environments = out
	return true
}
