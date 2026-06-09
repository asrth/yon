// FILE: internal/updater/autoinstall_e2e_blindtest_test.go
package updater

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// e2BT is Lane D: a cross-lane end-to-end blind test. It drives the REAL
// verifyNotarizedBundle + swapAppBundle + autoInstallWith orchestration with
// fakes only at the OS boundary (mount/run/relaunch), proving the lanes
// compose. A regression in any single lane should break e2BTHappyPath.

// e2BTFakeRun returns a cmdRunner that simulates a notarized macOS bundle and a
// ditto copy. It switches on the command name (and a couple of arg shapes) so it
// is robust to the exact flag ordering each lane uses.
func e2BTFakeRun(t *testing.T) cmdRunner {
	t.Helper()
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch name {
		case "codesign":
			// -dvv prints signing info; --verify just succeeds.
			for _, a := range args {
				if a == "-dvv" {
					return []byte("TeamIdentifier=E2BTTEAM00\n" +
						"Authority=Developer ID Application: Yon (E2BTTEAM00)\n"), nil
				}
			}
			return []byte("ok"), nil
		case "spctl":
			return []byte("mountedApp: accepted\nsource=Notarized Developer ID\n"), nil
		case "ditto":
			// ditto <src> <dst>: emulate a recursive copy by transferring the one
			// file the test cares about (VERSION) from src into dst.
			if len(args) < 2 {
				t.Fatalf("e2BT: ditto called with too few args: %v", args)
			}
			src := args[len(args)-2]
			dst := args[len(args)-1]
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return nil, err
			}
			data, err := os.ReadFile(filepath.Join(src, "VERSION"))
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(dst, "VERSION"), data, 0o644); err != nil {
				return nil, err
			}
			return nil, nil
		default:
			t.Fatalf("e2BT: unexpected command %q args=%v", name, args)
			return nil, nil
		}
	}
}

func e2BTHappyPath(t *testing.T) {
	// Pin the Team ID the verifier requires, and restore it afterwards.
	oldTeam := expectedTeamID
	expectedTeamID = "E2BTTEAM00"
	defer func() { expectedTeamID = oldTeam }()

	// The currently-installed bundle, holding the OLD version.
	installedBundle := filepath.Join(t.TempDir(), "Yon.app")
	if err := os.MkdirAll(installedBundle, 0o755); err != nil {
		t.Fatalf("mkdir installed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installedBundle, "VERSION"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old VERSION: %v", err)
	}

	// The mounted DMG's bundle, holding the NEW version.
	mountedApp := filepath.Join(t.TempDir(), "mnt", "Yon.app")
	if err := os.MkdirAll(mountedApp, 0o755); err != nil {
		t.Fatalf("mkdir mounted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mountedApp, "VERSION"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write new VERSION: %v", err)
	}

	var cleanupRan bool
	var relaunchedPath string

	deps := installDeps{
		run: e2BTFakeRun(t),
		mount: func(ctx context.Context, dmgPath string) (string, func(), error) {
			return mountedApp, func() { cleanupRan = true }, nil
		},
		relaunch: func(appPath string) error {
			relaunchedPath = appPath
			return nil
		},
	}

	err := autoInstallWith(context.Background(), deps, "/tmp/Yon.dmg", installedBundle, func(string) {})
	if err != nil {
		t.Fatalf("autoInstallWith: unexpected error: %v", err)
	}

	// The swap must have replaced the installed bundle's contents with the new one.
	got, rerr := os.ReadFile(filepath.Join(installedBundle, "VERSION"))
	if rerr != nil {
		t.Fatalf("read installed VERSION after swap: %v", rerr)
	}
	if !bytes.Equal(got, []byte("new")) {
		t.Errorf("installed VERSION = %q after swap, want %q", got, "new")
	}

	// The orchestrator must relaunch the installed bundle, not the mounted one.
	if relaunchedPath != installedBundle {
		t.Errorf("relaunch called with %q, want %q", relaunchedPath, installedBundle)
	}

	// The mount cleanup must always run.
	if !cleanupRan {
		t.Error("mount cleanup did not run")
	}
}

// e2BTPlatformContract checks the public, platform-gated API on THIS (linux) CI
// build: AutoInstall must refuse with ErrAutoInstallUnsupported, and
// CanAutoInstall must report not-available.
func e2BTPlatformContract(t *testing.T) {
	err := AutoInstall(context.Background(), "/tmp/x.dmg", nil)
	if !errors.Is(err, ErrAutoInstallUnsupported) {
		t.Errorf("AutoInstall err = %v, want ErrAutoInstallUnsupported", err)
	}
	if _, ok := CanAutoInstall(); ok {
		t.Error("CanAutoInstall() ok = true on this build, want false")
	}
}

func Test_e2BT(t *testing.T) {
	t.Run("HappyPath", e2BTHappyPath)
	t.Run("PlatformContract", e2BTPlatformContract)
}
