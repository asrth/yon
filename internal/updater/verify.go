package updater

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
)

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
	// Refuse outright on an unpinned (dev) build: we must never accept an
	// arbitrarily-signed bundle.
	if expectedTeamID == "" {
		return fmt.Errorf("verify %q: %w: build is unpinned (empty expectedTeamID)", appPath, ErrAutoInstallUnsupported)
	}

	// 1. Signature must be intact (deep + strict over the whole bundle).
	if out, err := run(ctx, "codesign", "--verify", "--deep", "--strict", appPath); err != nil {
		return fmt.Errorf("verify %q: codesign --verify --deep --strict failed: %w: %s", appPath, err, strings.TrimSpace(string(out)))
	}

	// 2. Identity: the signing Team ID must match our pinned one, and the
	// authority must be a Developer ID Application certificate.
	out, err := run(ctx, "codesign", "-dvv", appPath)
	if err != nil {
		return fmt.Errorf("verify %q: codesign -dvv failed: %w: %s", appPath, err, strings.TrimSpace(string(out)))
	}
	teamID := parseTeamID(out)
	if teamID == "" {
		return fmt.Errorf("verify %q: codesign -dvv reported no TeamIdentifier", appPath)
	}
	if teamID != expectedTeamID {
		return fmt.Errorf("verify %q: codesign -dvv TeamIdentifier %q does not match pinned %q", appPath, teamID, expectedTeamID)
	}
	if !bytes.Contains(out, []byte("Developer ID Application")) {
		return fmt.Errorf("verify %q: codesign -dvv authority is not a Developer ID Application certificate", appPath)
	}

	// 3. Gatekeeper assessment: the bundle must be accepted as a notarized
	// Developer ID executable.
	spOut, err := run(ctx, "spctl", "-a", "-t", "exec", "-vv", appPath)
	if err != nil {
		return fmt.Errorf("verify %q: spctl -a -t exec -vv failed: %w: %s", appPath, err, strings.TrimSpace(string(spOut)))
	}
	if !spctlAccepted(spOut) {
		return fmt.Errorf("verify %q: spctl -a -t exec -vv did not accept a Notarized Developer ID source: %s", appPath, strings.TrimSpace(string(spOut)))
	}

	return nil
}

// parseTeamID extracts the TeamIdentifier value (e.g. "AB12CD34EF") from
// `codesign -dvv` output, which prints a line like "TeamIdentifier=AB12CD34EF".
// Returns "" when not present.
//
// LANE A owns this function.
func parseTeamID(codesignOutput []byte) string {
	const prefix = "TeamIdentifier="
	scanner := bufio.NewScanner(bytes.NewReader(codesignOutput))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

// spctlAccepted reports whether `spctl -a -t exec -vv` output indicates an
// accepted assessment from a Notarized Developer ID source (the lines
// "accepted" and a "source=Notarized Developer ID").
//
// LANE A owns this function.
func spctlAccepted(spctlOutput []byte) bool {
	lower := strings.ToLower(string(spctlOutput))
	return strings.Contains(lower, "accepted") &&
		strings.Contains(lower, "source=notarized developer id")
}
