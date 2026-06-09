// FILE: internal/updater/swap_blindtest_test.go
package updater

// Lane B (swap) BLIND contract tests for issue #41. Written purely from the
// swapAppBundle contract; swap.go was NOT opened. The fake `ditto` cmdRunner
// materialises the staging copy the swap logic then renames into place, so the
// real atomic-rename + rollback logic runs against a genuine on-disk layout.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// swBTMarker is the relative path of the marker file written inside each bundle
// directory; its contents distinguish the OLD install from the NEW staged copy.
const swBTMarker = "marker.txt"

// swBTReadMarker returns the marker contents inside bundle (dir), or "" if it is
// absent / unreadable.
func swBTReadMarker(t *testing.T, bundle string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(bundle, swBTMarker))
	if err != nil {
		return ""
	}
	return string(b)
}

// swBTWriteBundle creates a bundle directory at path containing a marker file
// with the given content.
func swBTWriteBundle(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("swBT: mkdir bundle %q: %v", path, err)
	}
	if err := os.WriteFile(filepath.Join(path, swBTMarker), []byte(content), 0o644); err != nil {
		t.Fatalf("swBT: write marker in %q: %v", path, err)
	}
}

// swBTSiblings lists the entries in dir that are NOT name, used to assert that
// no ".new"/".old" scratch siblings are left behind after a swap.
func swBTSiblings(t *testing.T, dir, name string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("swBT: read parent dir %q: %v", dir, err)
	}
	var out []string
	for _, e := range ents {
		if e.Name() != name {
			out = append(out, e.Name())
		}
	}
	return out
}

// swBTDittoOK returns a cmdRunner that emulates `ditto src staging` by actually
// creating the staging directory (args[2]) with a NEW marker, so the swap's
// rename step has a real same-volume copy to move into place. Any other command
// is a no-op success.
func swBTDittoOK(t *testing.T, newContent string) cmdRunner {
	t.Helper()
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ditto" {
			if len(args) < 2 {
				t.Fatalf("swBT: ditto invoked with too few args: %v", args)
			}
			staging := args[len(args)-1]
			swBTWriteBundle(t, staging, newContent)
		}
		return nil, nil
	}
}

func Test_swBT_SwapHappyPath(t *testing.T) {
	parent := t.TempDir()
	installed := filepath.Join(parent, "Yon.app")
	swBTWriteBundle(t, installed, "OLD")

	// newBundle path need not exist on disk: the fake ditto produces the
	// staging copy from it. Use a sibling-ish path the runner can read as src.
	newBundle := filepath.Join(t.TempDir(), "New-Yon.app")
	swBTWriteBundle(t, newBundle, "NEW")

	run := swBTDittoOK(t, "NEW")

	if err := swapAppBundle(context.Background(), run, newBundle, installed); err != nil {
		t.Fatalf("swapAppBundle returned error on happy path: %v", err)
	}

	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("installed bundle missing after swap: %v", err)
	}
	if got := swBTReadMarker(t, installed); got != "NEW" {
		t.Errorf("installed marker = %q, want %q (old not replaced)", got, "NEW")
	}

	// No ".new"/".old" scratch siblings should remain next to the install.
	if extras := swBTSiblings(t, parent, "Yon.app"); len(extras) != 0 {
		t.Errorf("leftover siblings after swap: %v", extras)
	}
}

func Test_swBT_SwapDittoFailureLeavesInstallUntouched(t *testing.T) {
	parent := t.TempDir()
	installed := filepath.Join(parent, "Yon.app")
	swBTWriteBundle(t, installed, "OLD")

	newBundle := filepath.Join(t.TempDir(), "New-Yon.app")
	swBTWriteBundle(t, newBundle, "NEW")

	wantErr := errors.New("swBT ditto boom")
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ditto" {
			return []byte("ditto: failed"), wantErr
		}
		return nil, nil
	}

	err := swapAppBundle(context.Background(), run, newBundle, installed)
	if err == nil {
		t.Fatal("swapAppBundle should return an error when ditto fails")
	}

	// Install must be completely untouched: still present, still OLD.
	if _, statErr := os.Stat(installed); statErr != nil {
		t.Fatalf("installed bundle missing after failed swap: %v", statErr)
	}
	if got := swBTReadMarker(t, installed); got != "OLD" {
		t.Errorf("installed marker = %q after ditto failure, want %q (untouched)", got, "OLD")
	}
	if extras := swBTSiblings(t, parent, "Yon.app"); len(extras) != 0 {
		t.Errorf("leftover siblings after failed swap: %v", extras)
	}
}
