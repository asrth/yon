package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/ultramcu/yon/internal/model"
)

// Blind tests for the request-log DETAIL text helpers (issue #2), written from
// the contract only. They depend solely on the contract symbols:
//   - logEntry's detail fields: ReqHeaders / ReqBody / RespHeaders / RespBody
//   - the PURE renderers logDetailRequestText / logDetailResponseText
//
// They never reach into Fyne (openLogDetail builds a window; the two text
// helpers are pure and Fyne-free, so the rendered text is the unit under test).
// Test names carry a _Blind suffix so they sit alongside the source owner's own
// logdetail_test.go without colliding.

// blindBodyToken asserts the meaningful body token survives in the rendered
// text. The helper is free to pretty-print valid JSON or show it raw; either
// form keeps the key/value token, so a substring check on the rendered text is
// the form-agnostic assertion.
func blindBodyToken(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("body: expected token %q somewhere in rendered text:\n%s", want, got)
	}
}

// A. The request text shows the "<Method> <URL>" line, every header as
// "Key: Value", and the body (pretty or raw) carrying its key.
func TestLogDetailRequestText_Blind(t *testing.T) {
	e := logEntry{
		Method: "POST",
		URL:    "http://h/x",
		ReqHeaders: []model.Param{
			{Key: "Content-Type", Value: "application/json", Enabled: true},
			{Key: "X-A", Value: "1", Enabled: true},
		},
		ReqBody: `{"a":1}`,
	}
	got := logDetailRequestText(e)

	for _, sub := range []string{
		"POST http://h/x",
		"Content-Type: application/json",
		"X-A: 1",
	} {
		if !strings.Contains(got, sub) {
			t.Errorf("request text: expected substring %q in:\n%s", sub, got)
		}
	}
	// The body must appear (pretty-printed or raw); assert its key survives.
	blindBodyToken(t, got, `"a"`)
}

// B. The success response text shows "<Status> <StatusText>", every response
// header, and the body (pretty-printed JSON) with its key surviving.
func TestLogDetailResponseText_Success_Blind(t *testing.T) {
	body := []byte(`{"ok":true}`)
	e := logEntry{
		Status:     200,
		StatusText: "OK",
		Duration:   3 * time.Millisecond,
		Size:       int64(len(body)),
		RespHeaders: []model.Param{
			{Key: "Content-Type", Value: "application/json", Enabled: true},
		},
		RespBody: body,
	}
	got := logDetailResponseText(e)

	for _, sub := range []string{
		"200 OK",
		"Content-Type: application/json",
	} {
		if !strings.Contains(got, sub) {
			t.Errorf("response text: expected substring %q in:\n%s", sub, got)
		}
	}
	// The body key must survive (pretty-printed or raw).
	blindBodyToken(t, got, "ok")

	// A successful response is not an error.
	if strings.Contains(got, "ERROR") {
		t.Errorf("response text success: unexpected ERROR clause in:\n%s", got)
	}
}

// C. A failed send (Err set) shows the error and is NOT rendered as a 200.
func TestLogDetailResponseText_Error_Blind(t *testing.T) {
	e := logEntry{
		Err: "context deadline exceeded",
	}
	got := logDetailResponseText(e)

	if !strings.Contains(got, "context deadline exceeded") {
		t.Errorf("error response text: expected %q in:\n%s", "context deadline exceeded", got)
	}
	if strings.Contains(got, "200") {
		t.Errorf("error response text: unexpected %q in:\n%s", "200", got)
	}
}

// D. A response whose RespBody was capped (the stored bytes are SHORTER than the
// true wire Size) shows a truncation note. The contract specifies a truncation
// note "if RespBody was capped"; capping is signalled by Size exceeding
// len(RespBody), which is the form-independent trigger this test arranges. The
// note's exact wording is not pinned — any common phrasing counts — and the
// stored body itself must still be shown.
func TestLogDetailResponseText_Truncated_Blind(t *testing.T) {
	stored := []byte("partial response body")
	e := logEntry{
		Status:     200,
		StatusText: "OK",
		Size:       9_000_000, // true on-the-wire size, far larger than the stored bytes
		RespBody:   stored,    // capped capture: shorter than Size
	}
	got := logDetailResponseText(e)

	// The shown body bytes must still be present.
	if !strings.Contains(got, "partial response body") {
		t.Errorf("truncated response: expected the stored body to be shown in:\n%s", got)
	}

	// And a note must signal that what is shown is not the whole body.
	lower := strings.ToLower(got)
	noteShown := strings.Contains(lower, "truncat") ||
		strings.Contains(lower, "capped") ||
		strings.Contains(lower, "showing first") ||
		strings.Contains(got, "…")
	if !noteShown {
		head := got
		if len(head) > 400 {
			head = head[:400]
		}
		t.Errorf("truncated response: expected a truncation note when RespBody is shorter than Size; got:\n%s", head)
	}
}

// E. (Integration) Driving a REAL send through the headless Fyne driver +
// httptest to assert the appended logEntry carries ReqHeaders / ReqBody /
// RespHeaders / RespBody is feasible via the smoke pattern, but it reaches deep
// into the send/UI plumbing owned by other devs and is heavy. The pure helpers
// pinned in A–D already exercise all four detail fields through the renderers,
// so the integration leg is intentionally omitted. If wired later, the shape
// would be: start an httptest server returning a JSON body + a header,
// OpenCollectionWindow, openRequestTab, drive the send, then assert
// w.reqLog[last] has the four fields populated and that the two renderers show
// them.
