//go:build darwin

package updater

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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
	// Attach the image read-only and quietly (-nobrowse keeps it out of Finder),
	// mounting at a fresh random directory under /tmp so we never collide with an
	// already-mounted "Yon" volume under /Volumes.
	out, err := execRunner(ctx, "hdiutil", "attach", "-nobrowse", "-readonly",
		"-mountrandom", "/tmp", dmgPath)
	if err != nil {
		return "", func() {}, fmt.Errorf("hdiutil attach: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// hdiutil prints one tab-separated row per mounted entity, e.g.:
	//   /dev/disk4          	Apple_partition_scheme
	//   /dev/disk4s1        	Apple_partition_map
	//   /dev/disk4s2        	Apple_HFS         	/tmp/dmg.XXXXXX
	// The mount point is the trailing field that is an absolute path. Scan every
	// line and take the last whitespace-separated field that starts with "/" and
	// is not a /dev device node — that is the filesystem mount point.
	mountPoint := ""
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		last := fields[len(fields)-1]
		if strings.HasPrefix(last, "/") && !strings.HasPrefix(last, "/dev/") {
			mountPoint = last
		}
	}
	if mountPoint == "" {
		return "", func() {}, fmt.Errorf("hdiutil attach: could not find mount point in output: %s", strings.TrimSpace(string(out)))
	}

	// cleanup detaches the image; best-effort, with a forced fallback so a busy
	// volume still gets released. Safe to call even after a partial mount.
	cleanup := func() {
		if _, err := execRunner(context.Background(), "hdiutil", "detach", mountPoint); err != nil {
			_, _ = execRunner(context.Background(), "hdiutil", "detach", "-force", mountPoint)
		}
	}

	appPath := filepath.Join(mountPoint, "Yon.app")
	if _, err := os.Stat(appPath); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("hdiutil attach: Yon.app not found on mounted image at %s: %w", mountPoint, err)
	}
	return appPath, cleanup, nil
}

// relaunchApp spawns a detached helper that waits for THIS process to exit and
// then opens appPath, so the new version starts after the old one is gone.
//
// LANE C: implement with a tiny detached `sh` helper (Setpgid) that polls the
// parent PID, then `open` appPath.
func relaunchApp(appPath string) error {
	// We cannot `open` the new app while this process still owns the bundle, so
	// write a tiny detached helper that waits for THIS pid to exit (kill -0 fails
	// once we are gone) and then opens the freshly-installed app.
	script := fmt.Sprintf("#!/bin/sh\nwhile kill -0 %d 2>/dev/null; do sleep 0.2; done\nopen \"%s\"\n",
		os.Getpid(), appPath)

	f, err := os.CreateTemp(os.TempDir(), "yon-relaunch-*.sh")
	if err != nil {
		return fmt.Errorf("relaunch: create helper: %w", err)
	}
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		return fmt.Errorf("relaunch: write helper: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("relaunch: close helper: %w", err)
	}

	// Run detached in its own process group (Setpgid) so it survives this
	// process exiting; do not Wait — we return immediately and let the caller
	// quit, after which the helper opens the new app.
	cmd := exec.Command("/bin/sh", f.Name())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("relaunch: start helper: %w", err)
	}
	return nil
}
