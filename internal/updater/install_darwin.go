//go:build darwin

package updater

import (
	"context"
	"os/exec"
)

// AutoInstall is the macOS entry point for a one-click update: it wires the real
// exec runner, dmg mounter, and relaunch helper into the platform-neutral
// autoInstallWith orchestrator. It downloads must already have produced dmgPath.
//
// LANE C owns this file (the real macOS glue: mount/relaunch/exec).
func AutoInstall(ctx context.Context, dmgPath string, progress func(string)) error {
	bundle, ok := CanAutoInstall()
	if !ok {
		return ErrAutoInstallUnsupported
	}
	deps := installDeps{
		run:      execRunner,
		mount:    mountDMG,
		relaunch: relaunchApp,
	}
	return autoInstallWith(ctx, deps, dmgPath, bundle, progress)
}

// execRunner runs name+args and returns their combined output.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// mountDMG attaches dmgPath read-only and returns the path to the Yon.app inside
// it plus a cleanup that detaches the image (always safe to call).
//
// LANE C: implement with `hdiutil attach -nobrowse -readonly` (locate Yon.app on
// the mount point) and a `hdiutil detach` cleanup.
func mountDMG(ctx context.Context, dmgPath string) (string, func(), error) {
	return "", func() {}, ErrAutoInstallUnsupported
}

// relaunchApp spawns a detached helper that waits for THIS process to exit and
// then opens appPath, so the new version starts after the old one is gone.
//
// LANE C: implement with a tiny detached `sh` helper (Setpgid) that polls the
// parent PID, then `open` appPath.
func relaunchApp(appPath string) error {
	return ErrAutoInstallUnsupported
}
