package ui

import (
	"reflect"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/ultramcu/yon/internal/model"
)

// Independent BLIND tests for the sidebar search/filter feature, written from the
// SPEC only. Helper names are bt-prefixed to avoid collisions with the Dev's
// tests. These exercise requestMatchesFilter, sidebarRows() under a filter, the
// display-only guarantees, and flat-index integrity.

// btFilterWindow builds a Window over coll using the same construction the other
// ui tests use (test.NewApp + New + newWindow).
func btFilterWindow(coll model.Collection) *Window {
	return newWindow(New(test.NewApp()), coll, "")
}

// btFilterColl builds a small collection with distinctly-named requests so each
// field (Name / Method / URL / DisplayName) can be matched in isolation.
func btFilterColl() model.Collection {
	c := model.NewCollection("Filter Coll")
	c.Requests = []model.Request{
		{Name: "Alpha Login", Method: model.MethodPost, URL: "https://api.test/auth/login"},    // 0
		{Name: "Bravo Search", Method: model.MethodGet, URL: "https://api.test/zebra/list"},    // 1
		{Name: "Charlie Delete", Method: model.MethodDelete, URL: "https://api.test/widget/9"}, // 2
		{Name: "", Method: model.MethodPut, URL: "https://api.test/unicorn/42"},                // 3 (DisplayName derived)
	}
	return c
}

// btReqIdxByName returns the flat Requests index of the first request whose Name
// equals name, or -1.
func btReqIdxByName(c model.Collection, name string) int {
	for i := range c.Requests {
		if c.Requests[i].Name == name {
			return i
		}
	}
	return -1
}

// btReqRows returns only the non-folder (request) rows of rows.
func btReqRows(rows []sidebarRow) []sidebarRow {
	out := make([]sidebarRow, 0, len(rows))
	for _, r := range rows {
		if !r.IsFolder {
			out = append(out, r)
		}
	}
	return out
}

// btHasFolderHeader reports whether rows contains a folder-header row for id.
func btHasFolderHeader(rows []sidebarRow, id string) bool {
	for _, r := range rows {
		if r.IsFolder && r.FolderID == id {
			return true
		}
	}
	return false
}

// -- SPEC 1: requestMatchesFilter ------------------------------------------------

func TestBlindFilter_MatchHelper_PerField(t *testing.T) {
	// Distinct values so each query can only hit one field.
	req := model.Request{
		Name:   "MyLoginRequest",
		Method: model.MethodDelete,
		URL:    "https://example.com/sparrows/12",
	}

	cases := []struct {
		name string
		q    string // already lower-cased/trimmed, per helper contract
		want bool
		why  string
	}{
		{"name substring", "login", true, "Name contains 'login'"},
		{"method substring", "delete", true, "Method DELETE contains 'delete'"},
		{"url substring", "sparrows", true, "URL contains 'sparrows'"},
		{"displayname (==name)", "myloginrequest", true, "DisplayName==Name here"},
		{"non match", "zzznope", false, "no field contains this"},
		{"case-insensitive name", "loginrequest", true, "lower query vs mixed-case Name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := requestMatchesFilter(req, c.q); got != c.want {
				t.Fatalf("requestMatchesFilter(q=%q) = %v, want %v (%s)", c.q, got, c.want, c.why)
			}
		})
	}
}

func TestBlindFilter_MatchHelper_DisplayNameDerived(t *testing.T) {
	// Name empty so DisplayName() is derived from method+URL path. The derived
	// label should let the path segment match via DisplayName even though Name
	// is empty.
	req := model.Request{Name: "", Method: model.MethodGet, URL: "https://h.test/dragons/7"}
	if !requestMatchesFilter(req, "dragons") {
		t.Fatalf("expected match on derived DisplayName/URL containing 'dragons'; DisplayName=%q", req.DisplayName())
	}
}

func TestBlindFilter_MatchHelper_CaseInsensitiveBothDirections(t *testing.T) {
	// Mixed-case Name, lower-cased query (the documented input form).
	req := model.Request{Name: "MixedCaseNAME", Method: model.MethodGet, URL: "http://x"}
	if !requestMatchesFilter(req, "mixedcasename") {
		t.Fatalf("case-insensitive match failed for lower query against mixed-case Name")
	}
}

// -- SPEC 2: sidebarRows() with a filter active ----------------------------------

func TestBlindFilter_SidebarRows_EmptyQueryEqualsUnfiltered(t *testing.T) {
	w := btFilterWindow(btFilterColl())
	fid := w.addFolder("Folder One")
	// Put requests 0 and 1 into the folder; collapse it to make collapse matter.
	w.moveRequestToFolder(0, fid)
	w.moveRequestToFolder(1, fid)
	w.toggleFolderCollapsed(fid)

	w.filterQuery = ""
	unfiltered := w.sidebarRows()

	// Re-asserting empty query gives the SAME grouped output (collapse respected).
	w.filterQuery = ""
	again := w.sidebarRows()
	if !reflect.DeepEqual(unfiltered, again) {
		t.Fatalf("empty-query sidebarRows not stable:\n a=%+v\n b=%+v", unfiltered, again)
	}

	// Collapsed folder => header present but no child request rows for it.
	if !btHasFolderHeader(unfiltered, fid) {
		t.Fatalf("collapsed folder header missing in unfiltered rows: %+v", unfiltered)
	}
	for _, r := range unfiltered {
		if !r.IsFolder && r.FolderID == fid {
			t.Fatalf("collapsed folder should contribute no child rows, found %+v", r)
		}
	}
}

func TestBlindFilter_SidebarRows_OnlyMatchingRows(t *testing.T) {
	c := btFilterColl()
	w := btFilterWindow(c)

	// Top-level only (no folders). Filter to "bravo" -> only request 1.
	w.filterQuery = "bravo"
	rows := w.sidebarRows()
	reqRows := btReqRows(rows)
	if len(reqRows) != 1 {
		t.Fatalf("expected exactly 1 matching request row, got %d: %+v", len(reqRows), rows)
	}
	if reqRows[0].ReqIdx != 1 {
		t.Fatalf("matching row ReqIdx=%d, want 1 (Bravo Search)", reqRows[0].ReqIdx)
	}
	// No folder headers since there are no folders.
	for _, r := range rows {
		if r.IsFolder {
			t.Fatalf("unexpected folder header in folderless collection: %+v", r)
		}
	}
}

func TestBlindFilter_SidebarRows_FolderExpandedWhenMatchEvenIfCollapsed(t *testing.T) {
	c := btFilterColl()
	w := btFilterWindow(c)

	fMatch := w.addFolder("HasMatch")
	fNoMatch := w.addFolder("NoMatch")

	// "Alpha Login" (idx 0) into fMatch, "Charlie Delete" (idx 2) into fNoMatch.
	w.moveRequestToFolder(0, fMatch)
	w.moveRequestToFolder(2, fNoMatch)
	// Collapse the folder that DOES contain the match — filter must override it.
	w.toggleFolderCollapsed(fMatch)

	w.filterQuery = "alpha"
	rows := w.sidebarRows()

	// fMatch header present AND its matching child visible despite Collapsed.
	if !btHasFolderHeader(rows, fMatch) {
		t.Fatalf("folder with a match should show its header (expanded): %+v", rows)
	}
	var sawChild bool
	for _, r := range rows {
		if !r.IsFolder && r.FolderID == fMatch && r.ReqIdx == 0 {
			sawChild = true
		}
	}
	if !sawChild {
		t.Fatalf("collapsed folder with a match must show the matching child row; rows=%+v", rows)
	}

	// fNoMatch must be omitted entirely (no header, no children).
	if btHasFolderHeader(rows, fNoMatch) {
		t.Fatalf("folder with no matching request must be omitted entirely; rows=%+v", rows)
	}
	for _, r := range rows {
		if r.FolderID == fNoMatch {
			t.Fatalf("no rows for a non-matching folder expected, found %+v", r)
		}
	}
}

func TestBlindFilter_SidebarRows_TopLevelMatchesAppear(t *testing.T) {
	c := btFilterColl()
	w := btFilterWindow(c)
	fid := w.addFolder("F")
	w.moveRequestToFolder(0, fid) // Alpha into folder; 1,2,3 stay top-level.

	// "unicorn" matches request 3 (top-level, derived DisplayName/URL).
	w.filterQuery = "unicorn"
	rows := w.sidebarRows()
	reqRows := btReqRows(rows)
	if len(reqRows) != 1 || reqRows[0].ReqIdx != 3 {
		t.Fatalf("expected only top-level request 3 to match; got %+v", reqRows)
	}
	// The folder holds no match -> omitted.
	if btHasFolderHeader(rows, fid) {
		t.Fatalf("folder with no match should not appear; rows=%+v", rows)
	}
}

// -- SPEC 3: display-only guarantees --------------------------------------------

func TestBlindFilter_DisplayOnly_NoDirtyNoMutation(t *testing.T) {
	c := btFilterColl()
	w := btFilterWindow(c)
	fid := w.addFolder("F")
	w.moveRequestToFolder(1, fid)

	// Snapshot Requests AFTER setup (addFolder/move mark dirty). Then reset dirty
	// so we can attribute any later dirty solely to filtering.
	before := append([]model.Request(nil), w.coll.Requests...)
	w.selectedID = 2
	w.dirty = false

	w.filterQuery = "bravo"
	_ = w.sidebarRows()
	w.filterQuery = "charlie"
	_ = w.sidebarRows()
	w.filterQuery = ""
	_ = w.sidebarRows()

	if w.dirty {
		t.Fatalf("filtering must NOT set dirty")
	}
	if len(w.coll.Requests) != len(before) {
		t.Fatalf("Requests length changed: got %d, want %d", len(w.coll.Requests), len(before))
	}
	if !reflect.DeepEqual(w.coll.Requests, before) {
		t.Fatalf("Requests order/contents changed by filtering:\n before=%+v\n after =%+v", before, w.coll.Requests)
	}
	if w.selectedID != 2 {
		t.Fatalf("selectedID changed by filtering: got %d, want 2", w.selectedID)
	}
}

func TestBlindFilter_DisplayOnly_FilteredOutTabStaysOpen(t *testing.T) {
	c := btFilterColl()
	w := btFilterWindow(c)

	// Open a tab for request 2 (Charlie Delete), then filter to something that
	// excludes it.
	w.openRequestTab(2)
	if _, ok := w.openTabs[2]; !ok {
		t.Fatalf("precondition: openRequestTab(2) should register an open tab")
	}
	tabsBefore := len(w.openTabs)

	w.filterQuery = "alpha" // excludes request 2
	rows := w.sidebarRows()
	for _, r := range btReqRows(rows) {
		if r.ReqIdx == 2 {
			t.Fatalf("request 2 should be filtered OUT of rows; rows=%+v", rows)
		}
	}

	if _, ok := w.openTabs[2]; !ok {
		t.Fatalf("filtering out request 2 must NOT close its open tab")
	}
	if len(w.openTabs) != tabsBefore {
		t.Fatalf("openTabs count changed by filtering: got %d, want %d", len(w.openTabs), tabsBefore)
	}
}

func TestBlindFilter_DisplayOnly_ClearRestoresFullRows(t *testing.T) {
	c := btFilterColl()
	w := btFilterWindow(c)
	fid := w.addFolder("F")
	w.moveRequestToFolder(0, fid)
	w.moveRequestToFolder(1, fid)

	w.filterQuery = ""
	full := w.sidebarRows()

	w.filterQuery = "alpha"
	_ = w.sidebarRows() // filtered view (subset)

	w.filterQuery = "" // clear
	restored := w.sidebarRows()

	if !reflect.DeepEqual(full, restored) {
		t.Fatalf("clearing filter did not restore full grouped rows:\n full=%+v\n got =%+v", full, restored)
	}
}

// -- SPEC 4: flat-index integrity ------------------------------------------------

func TestBlindFilter_FlatIndexIntegrity(t *testing.T) {
	c := btFilterColl()
	w := btFilterWindow(c)
	fid := w.addFolder("Grp")
	// Move Charlie (idx 2) into the folder so the matching row's ReqIdx must point
	// past intervening requests — a naive per-folder counter would mis-map it.
	w.moveRequestToFolder(2, fid)

	target := "Charlie Delete"
	wantIdx := btReqIdxByName(c, target) // 2
	if wantIdx < 0 {
		t.Fatalf("test setup: %q not found", target)
	}

	w.filterQuery = "charlie"
	rows := w.sidebarRows()
	reqRows := btReqRows(rows)
	if len(reqRows) != 1 {
		t.Fatalf("expected exactly one matching row, got %d: %+v", len(reqRows), rows)
	}
	gotIdx := reqRows[0].ReqIdx
	if gotIdx != wantIdx {
		t.Fatalf("filtered row ReqIdx=%d, want %d", gotIdx, wantIdx)
	}
	// And that flat index maps back to the right request by name.
	if got := w.coll.Requests[gotIdx].Name; got != target {
		t.Fatalf("ReqIdx %d maps to %q, want %q", gotIdx, got, target)
	}
}
