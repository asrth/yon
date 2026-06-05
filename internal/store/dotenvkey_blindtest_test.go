package store

// Blind tests for issue #25: .env KEYS were written raw and parsed by splitting
// on the first '=' then trimming whitespace, so a key containing '=' was
// truncated and a key with leading/trailing whitespace lost it (values already
// round-trip fine). The fix quotes/escapes keys that need it (double-quoted,
// same escaping as values) with a quoted-key parse path, while NORMAL keys stay
// unquoted and existing .env files parse unchanged.
//
// These tests are package-internal (package store) so they can drive the
// unexported writeDotenv/readDotenv/parseDotenv directly, plus the PUBLIC store
// API (SaveEnvironment/LoadEnvironments) for the real-world manifestation. They
// deliberately avoid referencing quoteDotenvKey by name so the file compiles and
// runs on the pre-fix tree (capturing a real fail-before). Helpers are prefixed
// dek_ to avoid colliding with other test files in package store.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// dekRoundTrip writes a single key→value pair to a fresh temp .env via the
// unexported writeDotenv, then reads it back via readDotenv, returning the
// loaded map and the raw on-disk file text.
func dekRoundTrip(t *testing.T, pairs map[string]string) (map[string]string, string) {
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

// A. TestDotenvKeyRoundTrip — every key (paired with a value) survives a
// write→read round-trip EXACTLY, including keys that contain the '=' delimiter,
// leading/trailing whitespace, embedded quotes, a leading '#', or a newline.
func TestDotenvKeyRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{"delimiter-in-key", "a=b", "v1"},              // THE headline bug.
		{"lead-trail-space-key", " lead-trail ", "v2"}, // leading AND trailing spaces.
		{"embedded-quote-key", `a"b`, "v3"},            // embedded double quote.
		{"hash-leading-key", "#cfg", "v4"},             // leading '#'.
		{"newline-in-key", "line1\nline2", "v5"},       // embedded newline.
		{"normal-key", "__var.envbase.token", "v6"},    // ordinary key, must stay unquoted.
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, _ := dekRoundTrip(t, map[string]string{tc.key: tc.value})
			if len(got) != 1 {
				t.Fatalf("round-trip yielded %d entries, want 1: %#v", len(got), got)
			}
			if v, ok := got[tc.key]; !ok || v != tc.value {
				t.Errorf("key %q did not round-trip: got value %q (present=%v), want %q",
					tc.key, v, ok, tc.value)
			}
		})
	}

	// The NORMAL key must be written UNQUOTED in the file text: the line begins
	// with the bare key followed by '=' (not a leading double quote).
	t.Run("normal-key-unquoted-in-file", func(t *testing.T) {
		const key = "__var.envbase.token"
		_, raw := dekRoundTrip(t, map[string]string{key: "v6"})
		if want := key + "="; !dekContainsLine(raw, want) {
			t.Errorf("normal key not written unquoted: file text does not contain %q:\n%s", want, raw)
		}
		if dekContainsLine(raw, `"`+key) {
			t.Errorf("normal key was quoted but should be raw:\n%s", raw)
		}
	})

	// Multiple entries — a weird key and a normal key together — all round-trip.
	t.Run("mixed-keys-together", func(t *testing.T) {
		in := map[string]string{
			"a=b":                 "weird",
			"__var.envbase.token": "normal",
			" lead-trail ":        "spaced",
		}
		got, _ := dekRoundTrip(t, in)
		if len(got) != len(in) {
			t.Fatalf("mixed round-trip yielded %d entries, want %d: %#v", len(got), len(in), got)
		}
		for k, want := range in {
			if v, ok := got[k]; !ok || v != want {
				t.Errorf("key %q did not round-trip: got %q (present=%v), want %q", k, v, ok, want)
			}
		}
	})
}

// dekContainsLine reports whether text has a line that, after trimming a
// trailing '\r', begins with prefix. (writeDotenv writes one pair per '\n'
// line; a quoted key with an embedded newline is the only case where a single
// pair spans lines, which is not what these prefix checks target.)
func dekContainsLine(text, prefix string) bool {
	start := 0
	for i := 0; i <= len(text); i++ {
		if i == len(text) || text[i] == '\n' {
			line := text[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if len(line) >= len(prefix) && line[:len(prefix)] == prefix {
				return true
			}
			start = i + 1
		}
	}
	return false
}

// B. TestDotenvLegacyUnquotedStillParses — a hand-written classic .env with
// unquoted keys, a quoted VALUE, a comment and a blank line parses to exactly
// the expected map (backward compatibility: existing files parse unchanged).
func TestDotenvLegacyUnquotedStillParses(t *testing.T) {
	legacy := "# a comment line\n" +
		"\n" +
		"TOKEN=abc\n" +
		`__jumphost.x.password="p w"` + "\n"

	got := parseDotenv([]byte(legacy))

	want := map[string]string{
		"TOKEN":                 "abc",
		"__jumphost.x.password": "p w",
	}
	if len(got) != len(want) {
		t.Fatalf("parseDotenv returned %d entries, want %d: %#v", len(got), len(want), got)
	}
	for k, wv := range want {
		if v, ok := got[k]; !ok || v != wv {
			t.Errorf("legacy parse key %q = %q (present=%v), want %q", k, v, ok, wv)
		}
	}
}

// C. TestSecretVariableWeirdKeyEndToEnd — the real-world manifestation, via the
// PUBLIC store API. A SECRET variable whose Key contains '=' is saved, then
// loaded; its Value must come back intact. Pre-fix the '=' truncates the .env
// key so the value loads empty.
func TestSecretVariableWeirdKeyEndToEnd(t *testing.T) {
	collPath := filepath.Join(t.TempDir(), "api.yon")

	env := model.Environment{
		Name: "Local",
		Variables: []model.Variable{
			{Key: "a=b", Value: "sekret", Enabled: true, Secret: true},
		},
	}

	if err := SaveEnvironment(collPath, env); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}

	envs, err := LoadEnvironments(collPath)
	if err != nil {
		t.Fatalf("LoadEnvironments: %v", err)
	}

	var loaded model.Environment
	for _, e := range envs {
		if e.Name == "Local" {
			loaded = e
		}
	}
	if loaded.Name != "Local" {
		t.Fatalf("environment %q not found among %d loaded envs: %+v", "Local", len(envs), envs)
	}

	var v model.Variable
	for _, vv := range loaded.Variables {
		if vv.Key == "a=b" {
			v = vv
		}
	}
	if v.Key != "a=b" {
		t.Fatalf("secret variable with key %q not found in loaded env: %+v", "a=b", loaded)
	}
	if v.Value != "sekret" {
		t.Errorf("secret variable value not restored: got %q, want %q", v.Value, "sekret")
	}
}
