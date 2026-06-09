package updater

// Self-install / auto-update support (issue #41). Phase 1 targets macOS: after
// downloading the notarized .dmg, Yon can verify it is our signed + notarized
// build, atomically replace the installed /Applications/Yon.app, and relaunch.
//
// The security-critical and orchestration logic here is platform-neutral and
// parameterised by an injected cmdRunner / installDeps so it is unit-testable on
// any OS; the real macOS glue (mount, relaunch, exec) lives in install_darwin.go
// behind a build tag, with a stub in install_other.go for every other platform.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// expectedTeamID is the Apple Developer Team ID the self-updater requires the
// downloaded build to be signed by. It is injected at release-build time via
// -ldflags "-X 'github.com/ultramcu/yon/internal/updater.expectedTeamID=XXXXXXXXXX'"
// sourced from the APPLE_TEAM_ID secret. It is EMPTY for dev / plain `go build`
// builds, which disables auto-install (the app uses the download+open fallback),
// so a self-replace is only ever attempted by an officially built, signed app.
var expectedTeamID string

// ErrAutoInstallUnsupported is returned when an in-place update is not possible
// for this build/platform; the caller falls back to the download+open flow.
var ErrAutoInstallUnsupported = errors.New("updater: auto-install not supported on this build")

// cmdRunner runs an external command and returns its combined output. The real
// darwin implementation shells out (codesign/spctl/ditto/hdiutil); tests inject
// a fake to drive the verify/swap logic deterministically off-macOS.
type cmdRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// installDeps are the side-effecting operations the orchestrator needs, injected
// so autoInstallWith is testable without a real macOS.
type installDeps struct {
	// run invokes external tools (codesign/spctl/ditto).
	run cmdRunner
	// mount attaches the .dmg and returns the path to the Yon.app inside it plus
	// a cleanup func that detaches it; cleanup is always safe to call.
	mount func(ctx context.Context, dmgPath string) (appPath string, cleanup func(), err error)
	// relaunch starts the freshly-installed app after the current process exits.
	relaunch func(appPath string) error
}

// ExpectedTeamID returns the pinned Team ID (empty on dev builds). Exposed for
// the UI to decide whether to offer the one-click path and for diagnostics.
func ExpectedTeamID() string { return expectedTeamID }

// locateAppBundle returns the macOS .app bundle root that contains exePath, e.g.
// "/Applications/Yon.app" for ".../Yon.app/Contents/MacOS/Yon". ok is false when
// exePath is not inside a ".app" bundle.
//
// LANE C owns this function.
func locateAppBundle(exePath string) (bundle string, ok bool) {
	// Walk parent directories from exePath upward, returning the first component
	// whose name ends in ".app". This handles both the executable itself and any
	// nested path inside the bundle, and is filepath.Separator-aware via
	// filepath.Dir / filepath.Base.
	for p := exePath; ; {
		if strings.HasSuffix(filepath.Base(p), ".app") {
			return p, true
		}
		parent := filepath.Dir(p)
		if parent == p {
			// Reached the filesystem root without finding a ".app" component.
			return "", false
		}
		p = parent
	}
}

// CanAutoInstall reports the installed .app bundle and whether an in-place update
// is possible for THIS build: expectedTeamID must be pinned (official build), the
// running executable must live inside a .app bundle, and that bundle must be
// writable by the current user. ok=false → the caller uses the download+open
// fallback. (Always false off macOS; see install_other.go for the entry point.)
//
// LANE C owns this function.
func CanAutoInstall() (bundle string, ok bool) {
	// Dev / plain `go build` builds have no pinned Team ID: never self-replace.
	if expectedTeamID == "" {
		return "", false
	}
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	// Resolve symlinks so a symlinked launcher still maps back to the real
	// .app bundle on disk.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	bundle, ok = locateAppBundle(exe)
	if !ok {
		return "", false
	}
	// The atomic swap renames the bundle within its PARENT directory, so the
	// parent (e.g. /Applications) must be writable by the current user.
	if !dirWritable(filepath.Dir(bundle)) {
		return "", false
	}
	return bundle, true
}

// dirWritable reports whether dir is writable by the current user by attempting
// to create (and immediately remove) a temp file in it.
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".yon-write-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// autoInstallWith runs the verify -> swap -> relaunch sequence for installedBundle
// using deps: mount the dmg, verify the bundle inside is our notarized Developer
// ID build (verifyNotarizedBundle), atomically swap it into place
// (swapAppBundle), then relaunch. Any error BEFORE the swap must leave the
// installed app untouched. progress (nil-safe) reports human-readable steps.
//
// LANE C owns this function.
func autoInstallWith(ctx context.Context, deps installDeps, dmgPath, installedBundle string, progress func(string)) error {
	reportProgress(progress, "Mounting update…")
	appPath, cleanup, err := deps.mount(ctx, dmgPath)
	if err != nil {
		return fmt.Errorf("updater: mount update: %w", err)
	}
	defer cleanup()

	// Verify BEFORE touching the installed app: any failure here leaves the
	// current installation completely untouched.
	reportProgress(progress, "Verifying signature…")
	if err := verifyNotarizedBundle(ctx, deps.run, appPath); err != nil {
		return fmt.Errorf("updater: verify update: %w", err)
	}

	reportProgress(progress, "Installing…")
	if err := swapAppBundle(ctx, deps.run, appPath, installedBundle); err != nil {
		return fmt.Errorf("updater: install update: %w", err)
	}

	reportProgress(progress, "Relaunching…")
	return deps.relaunch(installedBundle)
}

// reportProgress calls progress if non-nil (small helper for the orchestrator).
func reportProgress(progress func(string), msg string) {
	if progress != nil {
		progress(msg)
	}
}
