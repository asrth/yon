// FILE: internal/updater/install_blindtest_test.go
package updater

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// gtBTSetTeamID pins expectedTeamID for the duration of a test and restores it
// via t.Cleanup, so the package global never leaks between tests.
func gtBTSetTeamID(t *testing.T, id string) {
	t.Helper()
	prev := expectedTeamID
	expectedTeamID = id
	t.Cleanup(func() { expectedTeamID = prev })
}

func Test_gtBTLocateAppBundle(t *testing.T) {
	cases := []struct {
		name    string
		exePath string
		want    string
		wantOK  bool
	}{
		{
			name:    "exe inside bundle",
			exePath: "/Applications/Yon.app/Contents/MacOS/Yon",
			want:    "/Applications/Yon.app",
			wantOK:  true,
		},
		{
			name:    "nested deeper inside bundle",
			exePath: "/Applications/Yon.app/Contents/Frameworks/Foo.framework/bin/helper",
			want:    "/Applications/Yon.app",
			wantOK:  true,
		},
		{
			name:    "bundle root itself",
			exePath: "/Applications/Yon.app",
			want:    "/Applications/Yon.app",
			wantOK:  true,
		},
		{
			name:    "not a bundle",
			exePath: "/usr/bin/yon",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "first .app component wins",
			exePath: "/Outer.app/Inner.app/Contents/MacOS/Yon",
			want:    "/Outer.app/Inner.app",
			wantOK:  true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := locateAppBundle(c.exePath)
			if got != c.want || ok != c.wantOK {
				t.Errorf("locateAppBundle(%q) = (%q, %v), want (%q, %v)",
					c.exePath, got, ok, c.want, c.wantOK)
			}
		})
	}
}

func Test_gtBTCanAutoInstall(t *testing.T) {
	// Dev / unpinned build: must always refuse regardless of where the binary is.
	t.Run("unpinned team id refuses", func(t *testing.T) {
		gtBTSetTeamID(t, "")
		if bundle, ok := CanAutoInstall(); ok || bundle != "" {
			t.Errorf("CanAutoInstall() = (%q, %v) with empty expectedTeamID, want (\"\", false)", bundle, ok)
		}
	})

	// Even with a pinned id, the test binary is NOT inside a .app bundle, so
	// CanAutoInstall must still report false (locateAppBundle fails on os.Executable).
	t.Run("pinned but test binary not in a bundle refuses", func(t *testing.T) {
		gtBTSetTeamID(t, "AB12CD34EF")
		if bundle, ok := CanAutoInstall(); ok || bundle != "" {
			t.Errorf("CanAutoInstall() = (%q, %v) for a non-bundle test binary, want (\"\", false)", bundle, ok)
		}
	})
}

// gtBTNewInstalledBundle creates a fake installed Yon.app under a fresh temp
// directory and drops a marker file inside it. Returns the bundle path. The
// marker lets a test assert the install was (or was not) replaced.
func gtBTNewInstalledBundle(t *testing.T, marker string) string {
	t.Helper()
	bundle := filepath.Join(t.TempDir(), "Yon.app")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("creating installed bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "marker"), []byte(marker), 0o644); err != nil {
		t.Fatalf("writing marker: %v", err)
	}
	return bundle
}

// gtBTReadMarker reads the marker file from a bundle, returning "" if absent.
func gtBTReadMarker(t *testing.T, bundle string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(bundle, "marker"))
	if err != nil {
		return ""
	}
	return string(b)
}

func Test_gtBTAutoInstallWith_HappyPath(t *testing.T) {
	gtBTSetTeamID(t, "AB12CD34EF")

	var calls []string

	mountedApp := filepath.Join(t.TempDir(), "Mounted", "Yon.app")
	if err := os.MkdirAll(mountedApp, 0o755); err != nil {
		t.Fatalf("creating mounted app: %v", err)
	}

	installedBundle := gtBTNewInstalledBundle(t, "old")

	cleanupRan := false

	deps := installDeps{
		// Fake run drives the REAL verifyNotarizedBundle + swapAppBundle:
		// codesign/spctl outputs satisfy verify, and ditto materialises the
		// staging dir so the atomic rename in swapAppBundle succeeds.
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			switch name {
			case "codesign":
				// args[0] is the flag(s); -dvv must report the pinned team id
				// and a Developer ID Application authority.
				if len(args) > 0 && args[0] == "-dvv" {
					calls = append(calls, "verify")
					return []byte("Authority=Developer ID Application: Acme\nTeamIdentifier=AB12CD34EF\n"), nil
				}
				// --verify --deep --strict
				return []byte(""), nil
			case "spctl":
				return []byte("source=Notarized Developer ID\naccepted\n"), nil
			case "ditto":
				// args = [src, dst]; create the staging copy on disk.
				calls = append(calls, "swap")
				if len(args) < 2 {
					t.Fatalf("ditto: expected src dst, got %v", args)
				}
				if err := os.MkdirAll(args[1], 0o755); err != nil {
					t.Fatalf("ditto staging mkdir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(args[1], "marker"), []byte("new"), 0o644); err != nil {
					t.Fatalf("ditto staging marker: %v", err)
				}
				return []byte(""), nil
			default:
				t.Fatalf("unexpected run command: %s %v", name, args)
				return nil, nil
			}
		},
		mount: func(ctx context.Context, dmgPath string) (string, func(), error) {
			calls = append(calls, "mount")
			return mountedApp, func() { cleanupRan = true }, nil
		},
		relaunch: func(appPath string) error {
			calls = append(calls, "relaunch:"+appPath)
			return nil
		},
	}

	err := autoInstallWith(context.Background(), deps, "/tmp/Yon.dmg", installedBundle, nil)
	if err != nil {
		t.Fatalf("autoInstallWith: unexpected error: %v", err)
	}

	// Order: mount -> verify -> swap -> relaunch(installedBundle).
	want := []string{"mount", "verify", "swap", "relaunch:" + installedBundle}
	if len(calls) != len(want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("call order = %v, want %v", calls, want)
		}
	}

	if !cleanupRan {
		t.Error("mount cleanup was not run (deferred cleanup expected)")
	}

	// The installed bundle should now carry the new bundle's marker.
	if got := gtBTReadMarker(t, installedBundle); got != "new" {
		t.Errorf("installed bundle marker = %q after swap, want %q", got, "new")
	}
}

func Test_gtBTAutoInstallWith_MountError(t *testing.T) {
	gtBTSetTeamID(t, "AB12CD34EF")

	var calls []string
	installedBundle := gtBTNewInstalledBundle(t, "old")

	deps := installDeps{
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, "run:"+name)
			return nil, nil
		},
		mount: func(ctx context.Context, dmgPath string) (string, func(), error) {
			calls = append(calls, "mount")
			return "", func() {}, os.ErrPermission
		},
		relaunch: func(appPath string) error {
			calls = append(calls, "relaunch")
			return nil
		},
	}

	err := autoInstallWith(context.Background(), deps, "/tmp/Yon.dmg", installedBundle, nil)
	if err == nil {
		t.Fatal("autoInstallWith: expected error on mount failure, got nil")
	}

	// Only mount should have been attempted: verify/swap (run) and relaunch never fire.
	if len(calls) != 1 || calls[0] != "mount" {
		t.Errorf("calls = %v, want only [mount]", calls)
	}

	// Install untouched.
	if got := gtBTReadMarker(t, installedBundle); got != "old" {
		t.Errorf("installed bundle marker = %q, want %q (untouched)", got, "old")
	}
}

func Test_gtBTAutoInstallWith_VerifyFails(t *testing.T) {
	gtBTSetTeamID(t, "AB12CD34EF")

	var calls []string
	mountedApp := filepath.Join(t.TempDir(), "Mounted", "Yon.app")
	if err := os.MkdirAll(mountedApp, 0o755); err != nil {
		t.Fatalf("creating mounted app: %v", err)
	}
	installedBundle := gtBTNewInstalledBundle(t, "old")

	cleanupRan := false

	deps := installDeps{
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			switch name {
			case "codesign":
				if len(args) > 0 && args[0] == "-dvv" {
					return []byte("Authority=Developer ID Application: Acme\nTeamIdentifier=AB12CD34EF\n"), nil
				}
				return []byte(""), nil
			case "spctl":
				// Gatekeeper REJECTS: not accepted / not notarized.
				calls = append(calls, "spctl-reject")
				return []byte("rejected\nsource=no usable signature\n"), os.ErrPermission
			case "ditto":
				// Must never be reached when verify fails.
				calls = append(calls, "swap")
				return []byte(""), nil
			default:
				t.Fatalf("unexpected run command: %s %v", name, args)
				return nil, nil
			}
		},
		mount: func(ctx context.Context, dmgPath string) (string, func(), error) {
			calls = append(calls, "mount")
			return mountedApp, func() { cleanupRan = true }, nil
		},
		relaunch: func(appPath string) error {
			calls = append(calls, "relaunch")
			return nil
		},
	}

	err := autoInstallWith(context.Background(), deps, "/tmp/Yon.dmg", installedBundle, nil)
	if err == nil {
		t.Fatal("autoInstallWith: expected error on verify failure, got nil")
	}

	// swap (ditto) and relaunch must not have happened.
	for _, c := range calls {
		if c == "swap" {
			t.Error("swapAppBundle ran despite verify failure")
		}
		if c == "relaunch" {
			t.Error("relaunch ran despite verify failure")
		}
	}

	// Install untouched: old marker intact, no staging/backup left behind.
	if got := gtBTReadMarker(t, installedBundle); got != "old" {
		t.Errorf("installed bundle marker = %q, want %q (untouched)", got, "old")
	}
	if _, err := os.Stat(installedBundle + ".new"); !os.IsNotExist(err) {
		t.Errorf("staging %q should not exist after verify failure", installedBundle+".new")
	}
	if _, err := os.Stat(installedBundle + ".old"); !os.IsNotExist(err) {
		t.Errorf("backup %q should not exist after verify failure", installedBundle+".old")
	}

	// mount cleanup still runs (deferred).
	if !cleanupRan {
		t.Error("mount cleanup was not run after verify failure")
	}
}
