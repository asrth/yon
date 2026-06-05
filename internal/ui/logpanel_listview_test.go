package ui

import (
	"strconv"
	"testing"
	"time"

	"fyne.io/fyne/v2"

	"github.com/ultramcu/yon/internal/model"
)

// Dev A's tests for the request-log LIST VIEW (issue #2): the panel's list
// reports the right length for N entries, and a row's double-tap maps to the
// CORRECT entry index even after the reused row widget is re-targeted to a
// different row (the classic widget.List item-reuse / stale-index hazard).
//
// These exercise the real row widget (logRowWidget) and its set/DoubleTapped
// path without depending on Dev B's openLogDetail: the row's onOpen callback is
// the seam openLogDetail is wired into in newLogPanel, so verifying the index
// handed to onOpen verifies the row→entry mapping the detail window relies on. A
// separate check drives the panel's list length() callback for N entries.
//
// Helper reused from a sibling test file: walkObjects (sidebar_tap_routing_test.go).

// newSpiedLogRow builds a logRowWidget like newLogPanel does, but routes its
// double-tap to a recorded index so the test can assert which entry would open.
func newSpiedLogRow() (*logRowWidget, *int) {
	got := -1
	r := newLogRowWidget(func(i int) { got = i })
	return r, &got
}

// TestLogRow_DoubleTapMapsToCurrentIndex is the stale-index guard: a single
// reused row widget is re-targeted (set) to several different ids — as
// widget.List's UpdateItem does on scroll/refresh — and each double-tap must
// report the id the row currently shows, never a previously captured one.
func TestLogRow_DoubleTapMapsToCurrentIndex(t *testing.T) {
	row, got := newSpiedLogRow()

	for _, id := range []int{0, 3, 1, 7, 2} {
		row.set(id, "line for row "+strconv.Itoa(id)) // text irrelevant to the mapping
		*got = -1
		row.DoubleTapped(&fyne.PointEvent{})
		if *got != id {
			t.Fatalf("double-tap after set(%d): opened index %d, want %d (stale-index bug)", id, *got, id)
		}
	}
}

// TestLogRow_DoubleTapBeforeSet asserts a freshly-built row (never set to a real
// id, idx == -1) opens nothing meaningful: it reports -1, which openDetail then
// rejects as out of range. Guards against a default-zero idx opening row 0.
func TestLogRow_DoubleTapBeforeSet(t *testing.T) {
	row, got := newSpiedLogRow()
	row.DoubleTapped(&fyne.PointEvent{})
	if *got != -1 {
		t.Fatalf("double-tap before set: opened index %d, want -1", *got)
	}
}

// TestLogPanel_ListLengthTracksReqLog drives the panel's list length() callback
// (the real widget.List built by newLogPanel) and checks it equals len(reqLog)
// for several N, including 0 (empty log → empty list).
func TestLogPanel_ListLengthTracksReqLog(t *testing.T) {
	w := newTestWindow(model.NewCollection("Log"))

	for _, n := range []int{0, 1, 5, 20} {
		w.reqLog = makeLogEntries(n)
		w.logPanel.list.Refresh()
		if got := w.logPanel.list.Length(); got != n {
			t.Errorf("list length for %d entries: got %d, want %d", n, got, n)
		}
	}
}

// TestLogPanel_UpdateItemSetsRowIndex verifies the panel's CreateItem/UpdateItem
// wiring: after the list lays out, each visible row widget carries its OWN id in
// idx (so a double-tap on it opens that row) and renders that entry's formatted
// text. This is the integration of set() into the panel, distinct from the
// unit-level stale-index test above.
func TestLogPanel_UpdateItemSetsRowIndex(t *testing.T) {
	w := newTestWindow(model.NewCollection("Log"))
	w.reqLog = makeLogEntries(4)

	list := w.logPanel.list
	list.Resize(fyne.NewSize(600, 400))
	list.Refresh()

	rows := findLogRows(list)
	if len(rows) == 0 {
		t.Fatal("no logRowWidget rendered; expected at least one visible row")
	}
	for _, r := range rows {
		if r.idx < 0 || r.idx >= len(w.reqLog) {
			t.Fatalf("rendered row idx %d out of range [0,%d)", r.idx, len(w.reqLog))
		}
		want := formatLogEntry(w.reqLog[r.idx])
		if got := r.label.Text; got != want {
			t.Errorf("row idx %d: label %q, want %q", r.idx, got, want)
		}
	}
}

// findLogRows returns every logRowWidget in root's rendered object tree, using
// the shared walkObjects tree walker.
func findLogRows(root fyne.CanvasObject) []*logRowWidget {
	var out []*logRowWidget
	walkObjects(fyne.CurrentApp(), root, func(o fyne.CanvasObject) {
		if r, ok := o.(*logRowWidget); ok {
			out = append(out, r)
		}
	})
	return out
}

// makeLogEntries builds n distinct logEntries so each row's formatted text is
// unique (the index is embedded in the Name), making the row→entry assertion
// unambiguous.
func makeLogEntries(n int) []logEntry {
	tm := time.Date(2026, 6, 5, 15, 4, 5, 0, time.UTC)
	out := make([]logEntry, n)
	for i := range out {
		out[i] = logEntry{
			Time:       tm,
			Name:       "R" + strconv.Itoa(i),
			Method:     "GET",
			URL:        "http://h/" + strconv.Itoa(i),
			Status:     200,
			StatusText: "OK",
			Duration:   time.Millisecond,
		}
	}
	return out
}
