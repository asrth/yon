//go:build darwin

package updater

import (
	"os"

	"golang.org/x/sys/unix"
)

func init() {
	// On macOS, replace the portable two-rename with an atomic exchange so the
	// install path is never momentarily absent during an update.
	replaceWithStaging = renameSwapReplace
}

// renameSwapReplace atomically swaps the staging bundle and the installed bundle
// in a single renamex_np(RENAME_SWAP) syscall: afterwards `installed` holds the
// new bundle and `staging` holds the old one, with no window where `installed`
// does not exist. The old bundle (now at staging) is then removed. If the atomic
// swap is unsupported (e.g. the volume's filesystem lacks RENAME_SWAP, or a
// cross-device path slipped through), it falls back to the portable two-rename.
func renameSwapReplace(staging, installed string) error {
	if err := unix.RenamexNp(staging, installed, unix.RENAME_SWAP); err != nil {
		// RENAME_SWAP unsupported on this filesystem (e.g. some network/FUSE
		// volumes) → fall back to the portable, still-safe two-rename.
		return twoRenameReplace(staging, installed)
	}
	// installed now holds the new bundle; staging holds the old one. Drop it.
	os.RemoveAll(staging)
	return nil
}
