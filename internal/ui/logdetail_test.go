package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/ultramcu/yon/internal/model"
)

// TestLogDetailRequestText_HeadersAndJSONBody checks the request renderer:
// the "<Method> <URL>" line, "Key: Value" headers, and a JSON body that gets
// pretty-printed (via the shared prettyJSON, so the indented form appears).
func TestLogDetailRequestText_HeadersAndJSONBodyDevB(t *testing.T) {
	e := logEntry{
		Method: "POST",
		URL:    "https://api.example.com/users",
		ReqHeaders: []model.Param{
			{Key: "Authorization", Value: "Bearer tok", Enabled: true},
			{Key: "Content-Type", Value: "application/json", Enabled: true},
		},
		ReqBody: `{"name":"yon","tags":[1,2]}`,
	}

	got := logDetailRequestText(e)

	if !strings.HasPrefix(got, "POST https://api.example.com/users") {
		t.Fatalf("missing method/url line:\n%s", got)
	}
	if !strings.Contains(got, "Authorization: Bearer tok") {
		t.Errorf("missing Authorization header line:\n%s", got)
	}
	if !strings.Contains(got, "Content-Type: application/json") {
		t.Errorf("missing Content-Type header line:\n%s", got)
	}
	// Pretty-printed JSON is two-space indented; the raw compact body is not.
	if !strings.Contains(got, "\n  \"name\": \"yon\"") {
		t.Errorf("body not pretty-printed via prettyJSON:\n%s", got)
	}
}

// TestLogDetailRequestText_NonJSONBodyRaw checks that a body that is not JSON is
// shown as-is (no pretty-print, no panic).
func TestLogDetailRequestText_NonJSONBodyRaw(t *testing.T) {
	e := logEntry{
		Method:  "PUT",
		URL:     "https://x/y",
		ReqBody: "just some text",
	}
	got := logDetailRequestText(e)
	if !strings.Contains(got, "just some text") {
		t.Errorf("raw body missing:\n%s", got)
	}
}

// TestLogDetailResponseText_Success checks the response renderer on a successful
// send: status line with duration + size, headers, and a pretty-printed JSON
// body. No truncation note when RespBody covers the full Size.
func TestLogDetailResponseText_SuccessDevB(t *testing.T) {
	body := []byte(`{"ok":true}`)
	e := logEntry{
		Status:     200,
		StatusText: "OK",
		Duration:   12 * time.Millisecond,
		Size:       int64(len(body)),
		RespHeaders: []model.Param{
			{Key: "Content-Type", Value: "application/json", Enabled: true},
		},
		RespBody: body,
	}

	got := logDetailResponseText(e)

	if !strings.HasPrefix(got, "200 OK · 12 ms") {
		t.Errorf("status line wrong:\n%s", got)
	}
	if !strings.Contains(got, "Content-Type: application/json") {
		t.Errorf("missing response header:\n%s", got)
	}
	if !strings.Contains(got, "\n  \"ok\": true") {
		t.Errorf("response body not pretty-printed:\n%s", got)
	}
	if strings.Contains(got, "truncated") {
		t.Errorf("unexpected truncation note for full body:\n%s", got)
	}
}

// TestLogDetailResponseText_Error checks that a failed send shows only the
// error, with no status/header/body sections.
func TestLogDetailResponseText_ErrorDevB(t *testing.T) {
	e := logEntry{Err: "dial tcp: connection refused"}
	got := logDetailResponseText(e)
	if got != "ERROR: dial tcp: connection refused" {
		t.Errorf("error case wrong: %q", got)
	}
}

// TestLogDetailResponseText_Truncated checks the truncation note appears when
// the captured RespBody is shorter than the true Size.
func TestLogDetailResponseText_TruncatedDevB(t *testing.T) {
	stored := []byte("partial body")
	e := logEntry{
		Status:     200,
		StatusText: "OK",
		Size:       9_000_000, // true size much larger than stored
		RespBody:   stored,
	}
	got := logDetailResponseText(e)
	if !strings.Contains(got, "truncated, showing first") {
		t.Errorf("missing truncation note:\n%s", got)
	}
	if !strings.Contains(got, "partial body") {
		t.Errorf("missing stored body after note:\n%s", got)
	}
}

// TestCapLogBody verifies the response-body cap: bodies at/under the cap pass
// through (as a copy), and over-cap bodies are clipped to maxLogBodyBytes.
func TestCapLogBody(t *testing.T) {
	if got := capLogBody(nil); got != nil {
		t.Errorf("nil body should stay nil, got %v", got)
	}

	small := []byte("hello")
	got := capLogBody(small)
	if string(got) != "hello" {
		t.Errorf("small body mangled: %q", got)
	}
	// Must be a copy, not aliasing the input (so the log does not pin the source).
	got[0] = 'H'
	if small[0] != 'h' {
		t.Error("capLogBody did not copy the input slice")
	}

	big := make([]byte, maxLogBodyBytes+500)
	for i := range big {
		big[i] = 'x'
	}
	capped := capLogBody(big)
	if len(capped) != maxLogBodyBytes {
		t.Errorf("over-cap body len = %d, want %d", len(capped), maxLogBodyBytes)
	}
}

// TestResolveLogHeaders verifies enabled headers are resolved through the scope
// and disabled headers are dropped.
func TestResolveLogHeaders(t *testing.T) {
	resolve := func(s string) string {
		return strings.ReplaceAll(s, "{{host}}", "api.example.com")
	}
	in := []model.Param{
		{Key: "X-Target", Value: "{{host}}", Enabled: true},
		{Key: "X-Skip", Value: "no", Enabled: false},
	}
	out := resolveLogHeaders(in, resolve)
	if len(out) != 1 {
		t.Fatalf("want 1 resolved header, got %d: %+v", len(out), out)
	}
	if out[0].Key != "X-Target" || out[0].Value != "api.example.com" {
		t.Errorf("header not resolved: %+v", out[0])
	}
	if !out[0].Enabled {
		t.Error("retained header should be marked Enabled")
	}
}
