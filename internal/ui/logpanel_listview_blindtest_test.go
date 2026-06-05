package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/ultramcu/yon/internal/model"
)

// Blind tests for the request-log LIST VIEW + detail wiring (issue #2, Dev A's
// logpanel.go pieces). Written from the contract only:
//
//   - logEntry gains DETAIL fields ReqHeaders []model.Param, ReqBody string,
//     RespHeaders []model.Param, RespBody []byte (alongside the existing summary
//     fields). formatLogEntry / formatLog are UNCHANGED — the detail fields must
//     not leak into the one-line summary.
//   - The log panel is a widget.List: one row per Window.reqLog entry, the row's
//     text is formatLogEntry(entry), and double-tapping the row for index i opens
//     a detail window via w.openLogDetail(reqLog[i]). Copy (formatLog) + Clear are
//     preserved.
//
// These tests depend ONLY on contract symbols (logEntry + its new fields,
// formatLogEntry, formatLog, Window.appendLog/clearLog/reqLog, the panel's
// widget.List, the row's fyne.DoubleTappable) and reuse the package's existing
// tree-walk helpers (walkObjects / contentContains via the Variables-panel and
// sidebar tests) — they are NOT redefined here. The newLogTestWindow helper from
// logpanel_window_blindtest_test.go is reused.

// --- A. summary backward-compat with detail fields populated -----------------

// TestFormatLogEntry_UnchangedWithDetailFields pins that the new detail fields are
// invisible to the summary formatter: a logEntry WITH ReqHeaders/ReqBody/
// RespHeaders/RespBody populated formats to the EXACT same summary line as the
// same entry without them, for both a success and an error entry, and via both
// formatLogEntry and formatLog. This guards the contract "formatLogEntry/formatLog
// UNCHANGED (still summary text)" so a future field can't silently bleed into the
// log line.
func TestFormatLogEntry_UnchangedWithDetailFields(t *testing.T) {
	tm := blindFixedTime()

	// Success entry: summary-only baseline vs. the same entry with every detail
	// field populated.
	base := logEntry{
		Time:       tm,
		Name:       "Ping",
		Method:     "GET",
		URL:        "http://h/json",
		Status:     200,
		StatusText: "OK",
		Duration:   3,
		Size:       1234,
	}
	withDetail := base
	withDetail.ReqHeaders = []model.Param{{Key: "X-Token", Value: "secret", Enabled: true}}
	withDetail.ReqBody = `{"hello":"world"}`
	withDetail.RespHeaders = []model.Param{{Key: "Content-Type", Value: "application/json", Enabled: true}}
	withDetail.RespBody = []byte(`{"ok":true,"detail":"should not leak"}`)

	if got, want := formatLogEntry(withDetail), formatLogEntry(base); got != want {
		t.Errorf("formatLogEntry leaked detail fields:\n with detail = %q\n summary-only = %q", got, want)
	}

	// The distinguishing detail values must be wholly absent from the summary.
	summary := formatLogEntry(withDetail)
	for _, leak := range []string{"X-Token", "secret", "hello", "world", "Content-Type", "should not leak"} {
		if contains(summary, leak) {
			t.Errorf("detail value %q leaked into summary line %q", leak, summary)
		}
	}

	// Error entry: same invariance under detail population.
	errBase := logEntry{
		Time:   tm,
		Name:   "Ping",
		Method: "POST",
		URL:    "http://h/x",
		Err:    "context deadline exceeded",
	}
	errDetail := errBase
	errDetail.ReqHeaders = []model.Param{{Key: "Authorization", Value: "Bearer t", Enabled: true}}
	errDetail.ReqBody = "request-body-detail"
	errDetail.RespHeaders = []model.Param{{Key: "Retry-After", Value: "1", Enabled: true}}
	errDetail.RespBody = []byte("response-body-detail")
	if got, want := formatLogEntry(errDetail), formatLogEntry(errBase); got != want {
		t.Errorf("formatLogEntry (error) leaked detail fields:\n with detail = %q\n summary-only = %q", got, want)
	}

	// formatLog over a mixed slice must equal the same slice with detail fields
	// stripped — i.e. the join is summary-only too.
	withSlice := []logEntry{withDetail, errDetail}
	baseSlice := []logEntry{base, errBase}
	if got, want := formatLog(withSlice), formatLog(baseSlice); got != want {
		t.Errorf("formatLog leaked detail fields:\n with detail = %q\n summary-only = %q", got, want)
	}
}

// --- B. the panel is a widget.List of the right length -----------------------

// TestLogPanelListLength pins that the log panel renders as a widget.List with one
// row per reqLog entry. It builds a Window (smoke pattern), appends 3 entries,
// refreshes the panel, then walks the panel's container tree for the *widget.List
// and asserts its Length() == 3. After clearLog the same list reports 0.
//
// Length() is the List's public length callback (widget.List.Length), which is the
// authoritative row count the List virtualizes against — independent of how many
// rows happen to be realized on the headless driver.
func TestLogPanelListLength(t *testing.T) {
	w := newLogTestWindow(t)

	if w.logPanel == nil {
		t.Fatal("w.logPanel is nil; expected the panel constructed up front")
	}

	list := findLogList(t, w)

	if list.Length == nil {
		t.Fatal("log panel widget.List has a nil Length callback")
	}
	if n := list.Length(); n != 0 {
		t.Fatalf("precondition: empty log should give list length 0, got %d", n)
	}

	for i := 0; i < 3; i++ {
		w.appendLog(logEntry{
			Name:   "req" + itoa(i),
			Method: "GET",
			URL:    "https://example.com/" + itoa(i),
		})
	}
	w.logPanel.refresh()

	if n := list.Length(); n != 3 {
		t.Errorf("after 3 appends, list length = %d, want 3", n)
	}
	if n := len(w.reqLog); n != 3 {
		t.Errorf("reqLog length = %d, want 3 (sanity)", n)
	}

	w.clearLog()
	w.logPanel.refresh()
	if n := list.Length(); n != 0 {
		t.Errorf("after clearLog, list length = %d, want 0", n)
	}
}

// --- C. double-tapping a row opens the CORRECT entry (stale-index guard) ------

// TestLogRowDoubleTapOpensCorrectEntry is the high-risk test: it pins that
// double-tapping the row at visible position 1 opens the detail for the entry at
// index 1 — NOT index 0 and not a stale captured index. This is the classic
// widget.List item-reuse / stale-index hazard: a row widget is built once and
// re-targeted per visible id, so if the double-tap fires with an index captured at
// build time (or otherwise not re-synced on update) it opens the wrong send.
//
// Observation approach (documented): the contract routes a row double-tap through
// w.openLogDetail(reqLog[i]). openLogDetail (Dev B's piece) opens a real detail
// window, which is awkward to assert on blindly and is out of Dev A's scope. So we
// observe at Dev A's seam instead: we realize the list's rows on the headless test
// driver (mount the panel container in a window and size it so widget.List builds
// its row widgets), walk for the rendered row widgets that implement
// fyne.DoubleTappable, pick the one currently RENDERING entry 1's summary text,
// and invoke its real DoubleTapped. We then assert that the index the row carries —
// the value that openDetail/openLogDetail would receive — resolves to entry 1 (by
// its distinguishing summary text), proving the row→entry mapping the double-tap
// uses is the live one, not a stale index.
//
// A stale-index mutation (e.g. UpdateItem failing to re-write the row's index, so
// DoubleTapped always reports the build-time / first index) makes this test fail.
func TestLogRowDoubleTapOpensCorrectEntry(t *testing.T) {
	w := newLogTestWindow(t)

	// Three DISTINGUISHABLE entries so a wrong/stale index is detectable by text.
	entries := []logEntry{
		{Name: "alpha", Method: "GET", URL: "https://example.com/alpha"},
		{Name: "bravo", Method: "POST", URL: "https://example.com/bravo"},
		{Name: "charlie", Method: "DELETE", URL: "https://example.com/charlie"},
	}
	for _, e := range entries {
		w.appendLog(e)
	}

	// Record which entry openDetail actually targets. We cannot reassign the
	// *Window.openLogDetail method from a test, so we observe through the index the
	// double-tapped row hands to the open path: the contract is openLogDetail(
	// reqLog[idx]), so reqLog[idx] IS the opened entry. Reading idx off the row the
	// user double-tapped (after a real DoubleTapped) is exactly what the open path
	// reads.
	const wantPos = 1
	wantText := formatLogEntry(w.reqLog[wantPos])

	row := realizeAndFindLogRow(t, w, wantText)

	// Fire the REAL double-tap (fyne.DoubleTappable) on the row rendering entry 1.
	row.DoubleTapped(&fyne.PointEvent{})

	// The row must carry index 1 — so the open path runs openLogDetail(reqLog[1]).
	if row.idx != wantPos {
		t.Fatalf("double-tapped row idx = %d, want %d (stale-index bug: the row "+
			"reports the wrong entry to openLogDetail)", row.idx, wantPos)
	}
	if got := formatLogEntry(w.reqLog[row.idx]); got != wantText {
		t.Fatalf("row idx %d resolves to entry %q, want entry %q", row.idx, got, wantText)
	}

	// Cross-check: it is specifically NOT entry 0 (the most common stale value).
	if row.idx == 0 {
		t.Fatalf("double-tap opened index 0, want %d (classic first-row stale index)", wantPos)
	}
	if formatLogEntry(w.reqLog[row.idx]) == formatLogEntry(w.reqLog[0]) {
		t.Fatalf("double-tapped row resolves to entry 0's text; want entry %d", wantPos)
	}
}

// --- helpers (local to this file) --------------------------------------------

// findLogList walks the log panel container for the one *widget.List the panel
// renders the log rows with, failing the test if there isn't exactly one.
func findLogList(t *testing.T, w *Window) *widget.List {
	t.Helper()
	var found *widget.List
	count := 0
	walkObjects(fyne.CurrentApp(), w.logPanel.container, func(o fyne.CanvasObject) {
		if l, ok := o.(*widget.List); ok {
			found = l
			count++
		}
	})
	if count == 0 {
		t.Fatal("log panel container has no *widget.List: the panel is not a list view")
	}
	if count > 1 {
		t.Fatalf("log panel container has %d *widget.List objects, want exactly 1", count)
	}
	return found
}

// realizeAndFindLogRow mounts the log panel in a sized test window so widget.List
// builds and lays out its row widgets, then walks the rendered tree for the
// double-tappable log row whose label currently shows wantText. It asserts the
// found row both implements fyne.DoubleTappable AND is the package's row widget
// type (so its current index is observable). Fails the test if no such row is
// realized.
func realizeAndFindLogRow(t *testing.T, w *Window, wantText string) *logRowWidget {
	t.Helper()

	// Mount the panel in its own window and give it real size so the List realizes
	// its rows (the headless driver only builds row widgets for a laid-out list).
	tw := test.NewWindow(w.logPanel.container)
	t.Cleanup(tw.Close)
	tw.Resize(fyne.NewSize(900, 400))
	w.logPanel.refresh()

	var match *logRowWidget
	var anyRow bool
	walkObjects(fyne.CurrentApp(), w.logPanel.container, func(o fyne.CanvasObject) {
		lr, ok := o.(*logRowWidget)
		if !ok {
			return
		}
		anyRow = true
		// The row must be double-tappable per the contract.
		if _, ok := any(lr).(fyne.DoubleTappable); !ok {
			t.Fatal("log row widget does not implement fyne.DoubleTappable")
		}
		if lr.label != nil && lr.label.Text == wantText {
			match = lr
		}
	})

	if !anyRow {
		t.Fatal("no log row widgets were realized by the list; cannot test double-tap routing")
	}
	if match == nil {
		t.Fatalf("no realized log row renders the wanted entry text %q", wantText)
	}
	return match
}

// contains is a tiny substring helper kept local so the summary-leak assertions
// don't import strings just for this.
func contains(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
