package updater

import "context"

// verifyNotarizedBundle verifies that appPath is a Yon.app signed by our pinned
// Developer ID (expectedTeamID) and notarized by Apple, using run to invoke
// codesign and spctl. It returns nil ONLY when every check passes:
//
//   - codesign --verify --deep --strict appPath succeeds (intact signature);
//   - codesign -dvv reports TeamIdentifier == expectedTeamID and a
//     "Developer ID Application" authority;
//   - spctl -a -t exec -vv accepts the bundle as a Notarized Developer ID app.
//
// A non-nil, descriptive error otherwise (so the caller aborts BEFORE touching
// the installed app). With an empty expectedTeamID it must refuse (return an
// error) rather than accept anything.
//
// LANE A owns this file.
func verifyNotarizedBundle(ctx context.Context, run cmdRunner, appPath string) error {
	// TODO(LANE A): run codesign --verify; codesign -dvv -> parseTeamID compare;
	// spctl -a -t exec -vv -> spctlAccepted. Refuse on empty expectedTeamID.
	return ErrAutoInstallUnsupported
}

// parseTeamID extracts the TeamIdentifier value (e.g. "AB12CD34EF") from
// `codesign -dvv` output, which prints a line like "TeamIdentifier=AB12CD34EF".
// Returns "" when not present.
//
// LANE A owns this function.
func parseTeamID(codesignOutput []byte) string {
	// TODO(LANE A).
	return ""
}

// spctlAccepted reports whether `spctl -a -t exec -vv` output indicates an
// accepted assessment from a Notarized Developer ID source (the lines
// "accepted" and a "source=Notarized Developer ID").
//
// LANE A owns this function.
func spctlAccepted(spctlOutput []byte) bool {
	// TODO(LANE A).
	return false
}
