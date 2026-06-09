package updater

// BLIND Lane A (verify) tests for issue #41. Written purely from the published
// contract for parseTeamID / spctlAccepted / verifyNotarizedBundle; verify.go was
// not read. A programmable fake cmdRunner drives verifyNotarizedBundle off-macOS.

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func Test_vfBTParseTeamID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"present", "TeamIdentifier=AB12CD34EF", "AB12CD34EF"},
		{"absent", "Authority=Developer ID Application: X", ""},
		{"empty", "", ""},
		{
			"surrounding lines",
			"Identifier=com.ultramcu.yon\n" +
				"Authority=Developer ID Application: Example (AB12CD34EF)\n" +
				"TeamIdentifier=AB12CD34EF\n" +
				"Sealed Resources version=2",
			"AB12CD34EF",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseTeamID([]byte(c.in)); got != c.want {
				t.Errorf("parseTeamID(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func Test_vfBTSpctlAccepted(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{
			"accepted and notarized",
			"/Applications/Yon.app: accepted\nsource=Notarized Developer ID",
			true,
		},
		{
			"accepted but not notarized",
			"/Applications/Yon.app: accepted\nsource=Developer ID",
			false,
		},
		{
			"rejected",
			"/Applications/Yon.app: rejected\nsource=no usable signature",
			false,
		},
		{
			"empty",
			"",
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := spctlAccepted([]byte(c.in)); got != c.want {
				t.Errorf("spctlAccepted(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// vfBTFakeResponse is a canned reply for one command invocation.
type vfBTFakeResponse struct {
	out []byte
	err error
}

// vfBTFakeRunner returns a cmdRunner that selects a canned response by matching
// the command name and its arguments. The first matcher whose substrings all
// appear (in order, joined into the full command line) wins; an unmatched command
// fails the test so missing wiring is loud rather than silent.
func vfBTFakeRunner(t *testing.T, matchers []vfBTMatcher) cmdRunner {
	t.Helper()
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		for _, m := range matchers {
			if m.matches(line) {
				return m.resp.out, m.resp.err
			}
		}
		t.Fatalf("unexpected command: %q", line)
		return nil, nil
	}
}

type vfBTMatcher struct {
	// any of the contained substrings must be present in the command line.
	contains []string
	resp     vfBTFakeResponse
}

func (m vfBTMatcher) matches(line string) bool {
	for _, s := range m.contains {
		if !strings.Contains(line, s) {
			return false
		}
	}
	return true
}

const (
	vfBTTeamID  = "AB12CD34EF"
	vfBTDvvGood = "Identifier=com.ultramcu.yon\n" +
		"TeamIdentifier=" + vfBTTeamID + "\n" +
		"Authority=Developer ID Application: Example Inc (" + vfBTTeamID + ")\n"
	vfBTSpctlGood = "/path/Yon.app: accepted\nsource=Notarized Developer ID\n"
)

// happy-path matchers used as a baseline; individual tests override one entry.
func vfBTHappyMatchers() []vfBTMatcher {
	return []vfBTMatcher{
		{contains: []string{"codesign", "--verify"}, resp: vfBTFakeResponse{}},
		{contains: []string{"codesign", "-dvv"}, resp: vfBTFakeResponse{out: []byte(vfBTDvvGood)}},
		{contains: []string{"spctl"}, resp: vfBTFakeResponse{out: []byte(vfBTSpctlGood)}},
	}
}

func Test_vfBTVerifyNotarizedBundle(t *testing.T) {
	t.Run("empty expected team id", func(t *testing.T) {
		old := expectedTeamID
		defer func() { expectedTeamID = old }()
		expectedTeamID = ""

		run := vfBTFakeRunner(t, vfBTHappyMatchers())
		if err := verifyNotarizedBundle(context.Background(), run, "/path/Yon.app"); err == nil {
			t.Error("verifyNotarizedBundle should error when expectedTeamID is empty")
		}
	})

	t.Run("happy path", func(t *testing.T) {
		old := expectedTeamID
		defer func() { expectedTeamID = old }()
		expectedTeamID = vfBTTeamID

		run := vfBTFakeRunner(t, vfBTHappyMatchers())
		if err := verifyNotarizedBundle(context.Background(), run, "/path/Yon.app"); err != nil {
			t.Errorf("verifyNotarizedBundle happy path: unexpected error %v", err)
		}
	})

	t.Run("wrong team id", func(t *testing.T) {
		old := expectedTeamID
		defer func() { expectedTeamID = old }()
		expectedTeamID = vfBTTeamID

		m := vfBTHappyMatchers()
		m[1] = vfBTMatcher{
			contains: []string{"codesign", "-dvv"},
			resp: vfBTFakeResponse{out: []byte(
				"TeamIdentifier=ZZ99ZZ99ZZ\n" +
					"Authority=Developer ID Application: Other (ZZ99ZZ99ZZ)\n")},
		}
		run := vfBTFakeRunner(t, m)
		if err := verifyNotarizedBundle(context.Background(), run, "/path/Yon.app"); err == nil {
			t.Error("verifyNotarizedBundle should error on mismatched TeamIdentifier")
		}
	})

	t.Run("codesign verify fails", func(t *testing.T) {
		old := expectedTeamID
		defer func() { expectedTeamID = old }()
		expectedTeamID = vfBTTeamID

		m := vfBTHappyMatchers()
		m[0] = vfBTMatcher{
			contains: []string{"codesign", "--verify"},
			resp: vfBTFakeResponse{
				out: []byte("code object is not signed at all"),
				err: errors.New("exit status 1"),
			},
		}
		run := vfBTFakeRunner(t, m)
		if err := verifyNotarizedBundle(context.Background(), run, "/path/Yon.app"); err == nil {
			t.Error("verifyNotarizedBundle should error when codesign --verify fails")
		}
	})

	t.Run("spctl rejects", func(t *testing.T) {
		old := expectedTeamID
		defer func() { expectedTeamID = old }()
		expectedTeamID = vfBTTeamID

		m := vfBTHappyMatchers()
		m[2] = vfBTMatcher{
			contains: []string{"spctl"},
			resp: vfBTFakeResponse{
				out: []byte("/path/Yon.app: rejected\nsource=no usable signature\n"),
				err: errors.New("exit status 3"),
			},
		}
		run := vfBTFakeRunner(t, m)
		if err := verifyNotarizedBundle(context.Background(), run, "/path/Yon.app"); err == nil {
			t.Error("verifyNotarizedBundle should error when spctl rejects")
		}
	})
}
