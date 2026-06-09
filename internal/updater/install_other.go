//go:build !darwin

package updater

import "context"

// AutoInstall is unsupported off macOS in phase 1 (issue #41); the caller falls
// back to the existing download+open flow. CanAutoInstall (in install.go) also
// reports false on these platforms, so this is a belt-and-suspenders guard.
func AutoInstall(ctx context.Context, dmgPath string, progress func(string)) error {
	return ErrAutoInstallUnsupported
}
