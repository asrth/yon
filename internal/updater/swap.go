package updater

import "context"

// swapAppBundle atomically replaces installedBundle (e.g. /Applications/Yon.app)
// with the verified bundle at newBundle (e.g. the Yon.app on a mounted dmg):
//
//  1. copy newBundle to a staging path NEXT TO installedBundle on the same
//     volume (so the final rename is atomic) using `ditto` via run, which
//     preserves the signature, symlinks, and extended attributes;
//  2. rename installedBundle aside to "<installedBundle>.old";
//  3. rename the staging copy into installedBundle's place;
//  4. on success remove the ".old"; on failure at step 3, roll the ".old" back.
//
// The installed app is never left missing: every failure path either leaves the
// original in place or restores it. Returns the first error encountered.
//
// LANE B owns this file.
func swapAppBundle(ctx context.Context, run cmdRunner, newBundle, installedBundle string) error {
	// TODO(LANE B): ditto copy to staging on the same volume, then the two-rename
	// atomic swap with rollback; clean up staging/.old appropriately.
	return ErrAutoInstallUnsupported
}
