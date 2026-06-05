package ui

import (
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// These tests exercise the PURE, Fyne-free helpers behind the Options tab:
// requestOptionsFromControls (raw control values → tri-state struct) and its
// inverse seedOptionControls. No Fyne app is constructed.

func intp(n int) *int    { return &n }
func boolp(b bool) *bool { return &b }

// TestRequestOptionsFromControls_AllDefault: every control at its Default/blank
// inherit state must produce a nil *RequestOptions, so an untouched Options tab
// leaves Request.Options nil (backward compat).
func TestRequestOptionsFromControls_AllDefault(t *testing.T) {
	if got := requestOptionsFromControls("", optDefault, optDefault); got != nil {
		t.Fatalf("all-default = %+v, want nil", got)
	}
	// Empty-string selects (a never-set Select) must also inherit.
	if got := requestOptionsFromControls("", "", ""); got != nil {
		t.Fatalf("all-blank = %+v, want nil", got)
	}
}

// TestRequestOptionsFromControls_Timeout covers the numeric Timeout entry's
// tri-state: blank/invalid/negative inherit (nil), ≥0 overrides, 0 = no-timeout.
func TestRequestOptionsFromControls_Timeout(t *testing.T) {
	cases := []struct {
		text string
		want *int
	}{
		{"", nil},
		{"abc", nil},
		{"-1", nil},
		{"0", intp(0)}, // explicit "no timeout"
		{"30", intp(30)},
	}
	for _, c := range cases {
		got := requestOptionsFromControls(c.text, optDefault, optDefault)
		if c.want == nil {
			if got != nil {
				t.Errorf("timeout %q = %+v, want nil struct", c.text, got)
			}
			continue
		}
		if got == nil || got.TimeoutSeconds == nil || *got.TimeoutSeconds != *c.want {
			t.Errorf("timeout %q = %+v, want TimeoutSeconds=%d", c.text, got, *c.want)
		}
		if got.FollowRedirects != nil || got.InsecureTLS != nil {
			t.Errorf("timeout %q leaked other fields: %+v", c.text, got)
		}
	}
}

// TestRequestOptionsFromControls_Selects covers the two tri-state Selects.
func TestRequestOptionsFromControls_Selects(t *testing.T) {
	// Follow redirects.
	if got := requestOptionsFromControls("", optFollow, optDefault); got == nil ||
		got.FollowRedirects == nil || *got.FollowRedirects != true {
		t.Errorf("follow=%q = %+v, want FollowRedirects=&true", optFollow, got)
	}
	if got := requestOptionsFromControls("", optNoFollow, optDefault); got == nil ||
		got.FollowRedirects == nil || *got.FollowRedirects != false {
		t.Errorf("follow=%q = %+v, want FollowRedirects=&false", optNoFollow, got)
	}
	// Insecure TLS.
	if got := requestOptionsFromControls("", optDefault, optTLSAllow); got == nil ||
		got.InsecureTLS == nil || *got.InsecureTLS != true {
		t.Errorf("tls=%q = %+v, want InsecureTLS=&true", optTLSAllow, got)
	}
	if got := requestOptionsFromControls("", optDefault, optTLSVerify); got == nil ||
		got.InsecureTLS == nil || *got.InsecureTLS != false {
		t.Errorf("tls=%q = %+v, want InsecureTLS=&false", optTLSVerify, got)
	}
}

// TestSeedOptionControls covers the inverse mapping, including the nil (no
// overrides) case that must seed every control to Default/blank.
func TestSeedOptionControls(t *testing.T) {
	tt, fs, ts := seedOptionControls(nil)
	if tt != "" || fs != optDefault || ts != optDefault {
		t.Errorf("seed(nil) = (%q,%q,%q), want (\"\",Default,Default)", tt, fs, ts)
	}

	ro := &model.RequestOptions{
		TimeoutSeconds:  intp(45),
		FollowRedirects: boolp(false),
		InsecureTLS:     boolp(true),
	}
	tt, fs, ts = seedOptionControls(ro)
	if tt != "45" || fs != optNoFollow || ts != optTLSAllow {
		t.Errorf("seed(%+v) = (%q,%q,%q), want (\"45\",%q,%q)", ro, tt, fs, ts, optNoFollow, optTLSAllow)
	}

	// A 0 timeout (explicit no-timeout) must round-trip as "0", not blank.
	tt, _, _ = seedOptionControls(&model.RequestOptions{TimeoutSeconds: intp(0)})
	if tt != "0" {
		t.Errorf("seed(timeout=0) text = %q, want \"0\"", tt)
	}
}

// TestOptionsRoundTrip: seedOptionControls(requestOptionsFromControls(x)) must
// reproduce x's controls, and the all-default round trip must yield a nil
// struct then blank/Default controls again.
func TestOptionsRoundTrip(t *testing.T) {
	cases := []struct {
		timeout string
		follow  string
		tls     string
	}{
		{"", optDefault, optDefault}, // all default → nil
		{"30", optFollow, optTLSVerify},
		{"0", optNoFollow, optTLSAllow}, // explicit no-timeout + overrides
		{"", optFollow, optDefault},     // partial override
	}
	for _, c := range cases {
		ro := requestOptionsFromControls(c.timeout, c.follow, c.tls)
		gotT, gotF, gotTLS := seedOptionControls(ro)
		if gotT != c.timeout || gotF != c.follow || gotTLS != c.tls {
			t.Errorf("round-trip (%q,%q,%q) -> %+v -> (%q,%q,%q)",
				c.timeout, c.follow, c.tls, ro, gotT, gotF, gotTLS)
		}
	}
}
