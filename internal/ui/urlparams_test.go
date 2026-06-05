package ui

import (
	"reflect"
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

func TestSplitJoinURLQuery(t *testing.T) {
	cases := []struct{ raw, base, query string }{
		{"https://api.com/usage?a=1&b=2", "https://api.com/usage", "a=1&b=2"},
		{"https://api.com/usage", "https://api.com/usage", ""},
		{"https://api.com/usage?", "https://api.com/usage", ""},
		{"?a=1", "", "a=1"},
	}
	for _, c := range cases {
		base, query := splitURLQuery(c.raw)
		if base != c.base || query != c.query {
			t.Errorf("splitURLQuery(%q) = (%q,%q), want (%q,%q)", c.raw, base, query, c.base, c.query)
		}
		// join is the inverse for these inputs.
		if got := joinURLQuery(c.base, c.query); got != c.raw && !(c.raw == "https://api.com/usage?" && got == "https://api.com/usage") {
			t.Errorf("joinURLQuery(%q,%q) = %q, want %q", c.base, c.query, got, c.raw)
		}
	}
}

func TestParseQueryParams(t *testing.T) {
	got := parseQueryParams("account=x&token=y&bare&enc=John%20Doe")
	want := []model.Param{
		{Key: "account", Value: "x", Enabled: true},
		{Key: "token", Value: "y", Enabled: true},
		{Key: "bare", Value: "", Enabled: true},
		{Key: "enc", Value: "John Doe", Enabled: true}, // percent-decoded
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseQueryParams =\n %#v\nwant\n %#v", got, want)
	}
	if parseQueryParams("") != nil {
		t.Fatalf("empty query should parse to nil")
	}
}

func TestEncodeQueryParams(t *testing.T) {
	in := []model.Param{
		{Key: "account", Value: "x", Enabled: true},
		{Key: "disabled", Value: "no", Enabled: false},    // skipped
		{Key: "server", Value: "{{host}}", Enabled: true}, // template kept readable
		{Key: "", Value: "", Enabled: true},               // empty row skipped
	}
	if got, want := encodeQueryParams(in), "account=x&server={{host}}"; got != want {
		t.Fatalf("encodeQueryParams = %q, want %q", got, want)
	}
}

func TestMergeQueryIntoParams(t *testing.T) {
	// Disabled "b" sits in the middle; query carries the enabled a,c plus a new d.
	old := []model.Param{
		{Key: "a", Value: "1", Enabled: true},
		{Key: "b", Value: "2", Enabled: false},
		{Key: "c", Value: "3", Enabled: true},
	}
	query := []model.Param{
		{Key: "a", Value: "1", Enabled: true},
		{Key: "c", Value: "3", Enabled: true},
		{Key: "d", Value: "4", Enabled: true},
	}
	got := mergeQueryIntoParams(old, query)
	wantKeys := []string{"a", "b", "c", "d"}
	if len(got) != len(wantKeys) {
		t.Fatalf("merge = %#v, want keys %v", got, wantKeys)
	}
	for i, k := range wantKeys {
		if got[i].Key != k {
			t.Fatalf("merge order = %#v, want keys %v", got, wantKeys)
		}
	}
	if got[1].Enabled { // the preserved "b" stays disabled
		t.Fatalf("preserved disabled row should stay disabled: %#v", got[1])
	}
}
