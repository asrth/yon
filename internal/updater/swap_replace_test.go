package updater

import (
	"os"
	"path/filepath"
	"testing"
)

// swapReplaceBundle creates a directory at path containing a "VERSION" file with
// the given marker, mimicking an .app bundle.
func swapReplaceBundle(t *testing.T, path, marker string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}
	if err := os.WriteFile(filepath.Join(path, "VERSION"), []byte(marker), 0o644); err != nil {
		t.Fatalf("write VERSION in %q: %v", path, err)
	}
}

// swapReplaceMarker reads the "VERSION" marker from a bundle dir.
func swapReplaceMarker(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(path, "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION in %q: %v", path, err)
	}
	return string(b)
}

// Test_twoRenameReplace_HappyPath: with a real staging bundle, twoRenameReplace
// moves the new bundle into place (old marker → new marker) and leaves no .old.
func Test_twoRenameReplace_HappyPath(t *testing.T) {
	dir := t.TempDir()
	installed := filepath.Join(dir, "Yon.app")
	staging := installed + ".new"
	swapReplaceBundle(t, installed, "old")
	swapReplaceBundle(t, staging, "new")

	if err := twoRenameReplace(staging, installed); err != nil {
		t.Fatalf("twoRenameReplace: %v", err)
	}
	if got := swapReplaceMarker(t, installed); got != "new" {
		t.Errorf("installed marker = %q, want %q", got, "new")
	}
	if _, err := os.Stat(installed + ".old"); !os.IsNotExist(err) {
		t.Errorf("%q.old should be removed on success", installed)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Errorf("staging %q should be gone (renamed into place)", staging)
	}
}

// Test_twoRenameReplace_RollbackRestoresOriginal forces the second rename to
// fail (staging does not exist) AFTER the first rename has moved the install
// aside, and asserts the original is rolled back from ".old" — so the installed
// bundle is never left missing. This pins the rollback path.
func Test_twoRenameReplace_RollbackRestoresOriginal(t *testing.T) {
	dir := t.TempDir()
	installed := filepath.Join(dir, "Yon.app")
	staging := installed + ".new" // intentionally NOT created → rename #2 fails
	swapReplaceBundle(t, installed, "old")

	err := twoRenameReplace(staging, installed)
	if err == nil {
		t.Fatal("twoRenameReplace should fail when staging does not exist")
	}
	// The original must be restored in place (rollback), not lost.
	if got := swapReplaceMarker(t, installed); got != "old" {
		t.Errorf("after rollback, installed marker = %q, want %q", got, "old")
	}
	if _, err := os.Stat(installed + ".old"); !os.IsNotExist(err) {
		t.Errorf("%q.old should not linger after a successful rollback", installed)
	}
}
