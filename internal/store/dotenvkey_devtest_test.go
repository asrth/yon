package store

// Dev tests for issue #25: .env KEYS were written raw and parsed by splitting on
// the first '=' then trimming whitespace, so a key containing '=' was truncated
// and a key with leading/trailing whitespace lost it (values already round-trip
// fine). The fix double-quotes/escapes keys that need it (same escaping as
// values, reversed by unescapeDotenvValue) with a quoted-key parse path, while
// NORMAL keys stay unquoted and existing .env files parse byte-identically.
//
// These tests drive the unexported writeDotenv/readDotenv/parseDotenv directly
// and assert BOTH the parsed round-trip AND that the on-disk text is sane.
// Helpers are prefixed dk_ to avoid colliding with the blindtest file's dek_*.
//
// FAIL-BEFORE / PASS-AFTER: on the pre-fix tree the '=' and leading/trailing
// space cases below truncate or trim the key, so the round-trip value is lost
// (TestDevDotenvKeyRoundTrip fails); after the fix every case round-trips.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dkRoundTrip writes pairs to a fresh temp .env via writeDotenv, reads them back
// via readDotenv, and returns the loaded map plus the raw on-disk text.
func dkRoundTrip(t *testing.T, pairs map[string]string) (map[string]string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := writeDotenv(path, pairs); err != nil {
		t.Fatalf("writeDotenv: %v", err)
	}
	got, err := readDotenv(path)
	if err != nil {
		t.Fatalf("readDotenv: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw .env: %v", err)
	}
	return got, string(raw)
}

// TestDevDotenvKeyRoundTrip — each key (with a value) survives write→read
// EXACTLY, and the on-disk text is sane: weird keys are double-quoted, the
// normal key is left raw. Each case is a single pair so len(got)==1 verifies the
// line parsed as exactly one entry.
func TestDevDotenvKeyRoundTrip(t *testing.T) {
	cases := []struct {
		name       string
		key        string
		value      string
		wantQuoted bool // expect the key written as a leading double-quote
	}{
		{"delimiter-in-key", "a=b", "v1", true},            // THE headline bug.
		{"leading-trailing-space", " x ", "v2", true},      // edge whitespace.
		{"embedded-quote", `a"b`, "v3", true},              // contains the quote char.
		{"leading-hash", "#cfg", "v4", true},               // comment hazard.
		{"embedded-newline", "l1\nl2", "v5", true},         // newline.
		{"normal-key", "__var.envbase.token", "v6", false}, // ordinary key: stays raw.
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, raw := dkRoundTrip(t, map[string]string{tc.key: tc.value})
			if len(got) != 1 {
				t.Fatalf("round-trip yielded %d entries, want 1: %#v\nraw:\n%s", len(got), got, raw)
			}
			if v, ok := got[tc.key]; !ok || v != tc.value {
				t.Errorf("key %q did not round-trip: got value %q (present=%v), want %q\nraw:\n%s",
					tc.key, v, ok, tc.value, raw)
			}

			// On-disk sanity: the raw text must NOT contain a literal newline
			// inside a quoted key (it is escaped as \n), so every pair is one
			// physical line. Check the first physical line's quoting shape.
			firstLine := raw
			if i := strings.IndexByte(raw, '\n'); i >= 0 {
				firstLine = raw[:i]
			}
			gotQuoted := strings.HasPrefix(firstLine, `"`)
			if gotQuoted != tc.wantQuoted {
				t.Errorf("key %q: on-disk quoted=%v, want %v\nraw:\n%s", tc.key, gotQuoted, tc.wantQuoted, raw)
			}
			// A weird key must escape every embedded newline (no stray physical
			// line break splitting the single pair).
			if strings.Count(strings.TrimRight(raw, "\n"), "\n") != 0 {
				t.Errorf("key %q: single pair spans multiple physical lines:\n%q", tc.key, raw)
			}
		})
	}
}

// TestDevDotenvNormalKeyStaysUnquoted — a plain key is written bare, exactly
// `weatherKey=v`, never `"weatherKey"=v`.
func TestDevDotenvNormalKeyStaysUnquoted(t *testing.T) {
	_, raw := dkRoundTrip(t, map[string]string{"weatherKey": "v"})
	if got := strings.TrimRight(raw, "\n"); got != "weatherKey=v" {
		t.Errorf("normal key on-disk = %q, want %q", got, "weatherKey=v")
	}
}

// TestDevDotenvMixedKeys — weird and normal keys together all round-trip.
func TestDevDotenvMixedKeys(t *testing.T) {
	in := map[string]string{
		"a=b":                 "weird",
		"__var.envbase.token": "normal",
		" x ":                 "spaced",
		`q"k`:                 "quoted",
	}
	got, raw := dkRoundTrip(t, in)
	if len(got) != len(in) {
		t.Fatalf("mixed round-trip yielded %d entries, want %d: %#v\nraw:\n%s", len(got), len(in), got, raw)
	}
	for k, want := range in {
		if v, ok := got[k]; !ok || v != want {
			t.Errorf("key %q did not round-trip: got %q (present=%v), want %q", k, v, ok, want)
		}
	}
}

// TestDevDotenvLegacyUnquotedParses — a hand-written classic .env (unquoted
// keys, quoted/unquoted values, comment, blank line) parses unchanged.
func TestDevDotenvLegacyUnquotedParses(t *testing.T) {
	legacy := "# comment\n" +
		"\n" +
		"TOKEN=abc\n" +
		"__var.x.k=plain\n" +
		`__jumphost.h.password="p w"` + "\n"

	got := parseDotenv([]byte(legacy))
	want := map[string]string{
		"TOKEN":                 "abc",
		"__var.x.k":             "plain",
		"__jumphost.h.password": "p w",
	}
	if len(got) != len(want) {
		t.Fatalf("legacy parse returned %d entries, want %d: %#v", len(got), len(want), got)
	}
	for k, wv := range want {
		if v, ok := got[k]; !ok || v != wv {
			t.Errorf("legacy key %q = %q (present=%v), want %q", k, v, ok, wv)
		}
	}
}

// TestDevDotenvMalformedQuotedKeySkipped — a quoted key with no closing quote,
// or no '=' after the closing quote, is skipped (never blocks loading) while a
// valid line in the same file still parses.
func TestDevDotenvMalformedQuotedKeySkipped(t *testing.T) {
	in := `"no-close=oops` + "\n" +
		`"closed-but-no-eq" oops` + "\n" +
		"GOOD=ok\n"
	got := parseDotenv([]byte(in))
	if len(got) != 1 {
		t.Fatalf("parse returned %d entries, want 1: %#v", len(got), got)
	}
	if got["GOOD"] != "ok" {
		t.Errorf("good line not parsed: got %#v", got)
	}
}
