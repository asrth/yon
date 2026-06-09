package updater

import (
	"context"
	"fmt"
	"os"
)

// replaceWithStaging moves the prepared staging bundle into installedBundle's
// place. It defaults to the portable two-rename (twoRenameReplace) and is
// overridden on macOS (swap_darwin.go) with an atomic renamex_np(RENAME_SWAP)
// so the install path is never momentarily absent.
var replaceWithStaging = twoRenameReplace

// swapAppBundle replaces installedBundle (e.g. /Applications/Yon.app) with the
// verified bundle at newBundle (e.g. the Yon.app on a mounted dmg):
//
//  1. copy newBundle to a staging path NEXT TO installedBundle on the same
//     volume (so the final swap stays on one filesystem) using `ditto` via run,
//     which preserves the signature, symlinks, and extended attributes;
//  2. hand the staging copy to replaceWithStaging, which moves it into place
//     (atomically on macOS; via a two-rename + rollback elsewhere).
//
// The installed app is never left missing on a successful or failed swap: the
// macOS path is a single atomic exchange, and the portable fallback always
// leaves the original in place or restores it from "<installedBundle>.old".
//
// LANE B owns this file.
func swapAppBundle(ctx context.Context, run cmdRunner, newBundle, installedBundle string) error {
	// Staging lives in the same parent directory (and thus on the same volume) as
	// installedBundle, so the replace stays on one filesystem.
	staging := installedBundle + ".new"

	// Clear any stale staging copy from a previous interrupted run.
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("swap: removing stale staging %q: %w", staging, err)
	}

	// Copy the verified new bundle into staging via ditto, preserving the
	// signature, symlinks, and extended attributes. Nothing has moved yet, so the
	// installed app is untouched on any failure here.
	if _, err := run(ctx, "ditto", newBundle, staging); err != nil {
		return fmt.Errorf("swap: ditto %q -> %q: %w", newBundle, staging, err)
	}
	if _, err := os.Stat(staging); err != nil {
		return fmt.Errorf("swap: staging %q missing after ditto: %w", staging, err)
	}

	// Move staging into place. On any failure the installed app is left intact or
	// restored (see replaceWithStaging); clean up staging on error.
	if err := replaceWithStaging(staging, installedBundle); err != nil {
		os.RemoveAll(staging)
		return err
	}
	return nil
}

// twoRenameReplace is the portable replace: move the current install aside to
// "<installed>.old", move staging into place, then remove ".old"; on a failed
// second rename it rolls the original back from ".old". It has a tiny window
// between the two renames during which installed is absent — macOS avoids this
// via renameSwapReplace (swap_darwin.go); this remains the fallback.
func twoRenameReplace(staging, installed string) error {
	old := installed + ".old"

	// Clear any stale backup, then move the current install aside.
	if err := os.RemoveAll(old); err != nil {
		return fmt.Errorf("swap: removing stale backup %q: %w", old, err)
	}
	if err := os.Rename(installed, old); err != nil {
		return fmt.Errorf("swap: moving %q aside to %q: %w", installed, old, err)
	}

	// Move the staged copy into place. On failure, roll the original back from the
	// backup so the installed app is never left missing.
	if err := os.Rename(staging, installed); err != nil {
		if rbErr := os.Rename(old, installed); rbErr != nil {
			return fmt.Errorf("swap: moving %q into place at %q failed (%w); rollback from %q also failed: %v", staging, installed, err, old, rbErr)
		}
		return fmt.Errorf("swap: moving %q into place at %q: %w", staging, installed, err)
	}

	// Success. Best-effort removal of the backup; ignore its error.
	os.RemoveAll(old)
	return nil
}
