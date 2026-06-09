package updater

import (
	"context"
	"fmt"
	"os"
)

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
	// Staging and backup paths live in the same parent directory (and thus on the
	// same volume) as installedBundle, so the final os.Rename calls are atomic.
	staging := installedBundle + ".new"
	old := installedBundle + ".old"

	// Clear any stale staging copy from a previous interrupted run.
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("swap: removing stale staging %q: %w", staging, err)
	}

	// Step 1: copy the verified new bundle into staging via ditto, preserving the
	// signature, symlinks, and extended attributes. Nothing has been renamed yet,
	// so the installed app is untouched on any failure here.
	if _, err := run(ctx, "ditto", newBundle, staging); err != nil {
		return fmt.Errorf("swap: ditto %q -> %q: %w", newBundle, staging, err)
	}
	if _, err := os.Stat(staging); err != nil {
		return fmt.Errorf("swap: staging %q missing after ditto: %w", staging, err)
	}

	// Step 2: clear any stale backup, then move the current install aside.
	if err := os.RemoveAll(old); err != nil {
		os.RemoveAll(staging)
		return fmt.Errorf("swap: removing stale backup %q: %w", old, err)
	}
	if err := os.Rename(installedBundle, old); err != nil {
		os.RemoveAll(staging)
		return fmt.Errorf("swap: moving %q aside to %q: %w", installedBundle, old, err)
	}

	// Step 3: move the staged copy into place. On failure, roll the original back
	// from the backup so the installed app is never left missing.
	if err := os.Rename(staging, installedBundle); err != nil {
		if rbErr := os.Rename(old, installedBundle); rbErr != nil {
			os.RemoveAll(staging)
			return fmt.Errorf("swap: moving %q into place at %q failed (%w); rollback from %q also failed: %v", staging, installedBundle, err, old, rbErr)
		}
		os.RemoveAll(staging)
		return fmt.Errorf("swap: moving %q into place at %q: %w", staging, installedBundle, err)
	}

	// Step 4: success. Best-effort removal of the backup; ignore its error.
	os.RemoveAll(old)
	return nil
}
