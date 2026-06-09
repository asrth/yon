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
	// TODO(LANE C): walk the path components up to the one ending in ".app".
	return "", false
}

// CanAutoInstall reports the installed .app bundle and whether an in-place update
// is possible for THIS build: expectedTeamID must be pinned (official build), the
// running executable must live inside a .app bundle, and that bundle must be
// writable by the current user. ok=false → the caller uses the download+open
// fallback. (Always false off macOS; see install_other.go for the entry point.)
//
// LANE C owns this function.
func CanAutoInstall() (bundle string, ok bool) {
	// TODO(LANE C): os.Executable -> resolve symlinks -> locateAppBundle ->
	// require expectedTeamID != "" and a writable bundle.
	return "", false
}

// autoInstallWith runs the verify -> swap -> relaunch sequence for installedBundle
// using deps: mount the dmg, verify the bundle inside is our notarized Developer
// ID build (verifyNotarizedBundle), atomically swap it into place
// (swapAppBundle), then relaunch. Any error BEFORE the swap must leave the
// installed app untouched. progress (nil-safe) reports human-readable steps.
//
// LANE C owns this function.
func autoInstallWith(ctx context.Context, deps installDeps, dmgPath, installedBundle string, progress func(string)) error {
	// TODO(LANE C): mount -> defer cleanup -> verifyNotarizedBundle ->
	// swapAppBundle -> relaunch, reporting progress and returning the first error.
	return ErrAutoInstallUnsupported
}

// reportProgress calls progress if non-nil (small helper for the orchestrator).
func reportProgress(progress func(string), msg string) {
	if progress != nil {
		progress(msg)
	}
}
