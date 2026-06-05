package ui

import "testing"

// TestUrlPathOf pins the status-bar path extraction, including scheme-less hosts
// (a resolved {{server}} without https://) which should still show just the path.
func TestUrlPathOf(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"", ""},
		{"https://api.example.com/users", "/users"},
		{"/users", "/users"},
		{"167.99.78.232:8787/usage", "/usage"},      // scheme-less host:port → path only
		{"api.example.com/v1/things", "/v1/things"}, // scheme-less host → path only
		{"{{server}}", "{{server}}"},                // unresolved template → left as-is
	}
	for _, c := range cases {
		if got := urlPathOf(c.raw); got != c.want {
			t.Errorf("urlPathOf(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}
