package ui

import (
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// btopWindow builds a fresh test Window over the given collection and clears the
// dirty flag so dirty assertions in each op test start from a clean baseline.
func btopWindow(t *testing.T, coll model.Collection) *Window {
	t.Helper()
	w := newTestWindow(coll)
	w.dirty = false
	return w
}

// SPEC 4: addFolder appends a folder with a unique id + trimmed name, marks the
// window dirty, and returns the id.
func TestBlind_AddFolder_AppendsTrimmedMarksDirtyReturnsID(t *testing.T) {
	w := btopWindow(t, model.NewCollection("C"))

	before := len(w.coll.Folders)
	id := w.addFolder("  My Folder  ")

	if id == "" {
		t.Fatalf("addFolder returned empty id")
	}
	if len(w.coll.Folders) != before+1 {
		t.Fatalf("folder count: got %d want %d", len(w.coll.Folders), before+1)
	}
	f := w.coll.Folders[len(w.coll.Folders)-1]
	if f.ID != id {
		t.Fatalf("appended folder id %q != returned id %q", f.ID, id)
	}
	if f.Name != "My Folder" {
		t.Fatalf("folder name not trimmed: got %q want %q", f.Name, "My Folder")
	}
	if !w.dirty {
		t.Fatalf("addFolder did not mark the window dirty")
	}

	// Uniqueness vs an existing folder.
	id2 := w.addFolder("Second")
	if id2 == id {
		t.Fatalf("addFolder produced a non-unique id %q", id2)
	}
}

// SPEC 5: deleteFolder removes the folder AND sets FolderID="" on its requests;
// total request count is UNCHANGED; other folders/requests untouched.
func TestBlind_DeleteFolder_ReparentsKeepsRequests(t *testing.T) {
	coll := model.NewCollection("C")
	coll.Folders = []model.Folder{
		{ID: "f1", Name: "One"},
		{ID: "f2", Name: "Two"},
	}
	coll.Requests = []model.Request{
		{Name: "a", Method: model.MethodGet, URL: "u/a", FolderID: "f1"},
		{Name: "b", Method: model.MethodGet, URL: "u/b", FolderID: "f2"},
		{Name: "c", Method: model.MethodGet, URL: "u/c", FolderID: "f1"},
		{Name: "d", Method: model.MethodGet, URL: "u/d", FolderID: ""},
	}
	w := btopWindow(t, coll)
	totalBefore := len(w.coll.Requests)

	w.deleteFolder("f1")

	if len(w.coll.Requests) != totalBefore {
		t.Fatalf("request count changed: got %d want %d", len(w.coll.Requests), totalBefore)
	}
	if _, ok := w.folderByID("f1"); ok {
		t.Fatalf("folder f1 still present after delete")
	}
	if _, ok := w.folderByID("f2"); !ok {
		t.Fatalf("folder f2 was removed; should be untouched")
	}
	// f1's requests reparented to top-level; others untouched.
	want := map[string]string{"a": "", "b": "f2", "c": "", "d": ""}
	for _, r := range w.coll.Requests {
		if got := r.FolderID; got != want[r.Name] {
			t.Fatalf("request %q FolderID: got %q want %q", r.Name, got, want[r.Name])
		}
	}
	if !w.dirty {
		t.Fatalf("deleteFolder did not mark dirty")
	}
}

// SPEC 6: moveRequestToFolder sets FolderID, does NOT reorder Requests; "" =
// top-level; bad reqIdx or unknown folderID is a no-op.
func TestBlind_MoveRequestToFolder_SetsWithoutReorder(t *testing.T) {
	coll := model.NewCollection("C")
	coll.Folders = []model.Folder{{ID: "f1", Name: "One"}}
	coll.Requests = []model.Request{
		{Name: "a", Method: model.MethodGet, URL: "u/a"},
		{Name: "b", Method: model.MethodGet, URL: "u/b"},
		{Name: "c", Method: model.MethodGet, URL: "u/c"},
	}
	w := btopWindow(t, coll)

	order := func() []string {
		ns := make([]string, len(w.coll.Requests))
		for i, r := range w.coll.Requests {
			ns[i] = r.Name
		}
		return ns
	}
	before := order()

	// Move req 1 into f1.
	w.moveRequestToFolder(1, "f1")
	if w.coll.Requests[1].FolderID != "f1" {
		t.Fatalf("move into folder: FolderID got %q want %q", w.coll.Requests[1].FolderID, "f1")
	}
	if !w.dirty {
		t.Fatalf("moveRequestToFolder did not mark dirty")
	}

	// Order/indices unchanged.
	for i, n := range before {
		if order()[i] != n {
			t.Fatalf("requests reordered: got %v want %v", order(), before)
		}
	}

	// Move back to top-level via "".
	w.moveRequestToFolder(1, "")
	if w.coll.Requests[1].FolderID != "" {
		t.Fatalf("move to top-level: FolderID got %q want \"\"", w.coll.Requests[1].FolderID)
	}

	// No-op: bad reqIdx.
	w.dirty = false
	w.moveRequestToFolder(99, "f1")
	w.moveRequestToFolder(-1, "f1")
	if w.dirty {
		t.Fatalf("moveRequestToFolder with bad reqIdx should be a no-op (not mark dirty)")
	}
	// No-op: unknown folderID.
	w.moveRequestToFolder(0, "nope")
	if w.coll.Requests[0].FolderID != "" {
		t.Fatalf("unknown folderID should be a no-op; req0 FolderID = %q", w.coll.Requests[0].FolderID)
	}
	if w.dirty {
		t.Fatalf("moveRequestToFolder with unknown folderID should be a no-op (not mark dirty)")
	}
	// Final order check.
	for i, n := range before {
		if order()[i] != n {
			t.Fatalf("requests reordered after no-ops: got %v want %v", order(), before)
		}
	}
}

// SPEC 7: toggleFolderCollapsed flips Collapsed and marks dirty.
func TestBlind_ToggleFolderCollapsed_FlipsMarksDirty(t *testing.T) {
	coll := model.NewCollection("C")
	coll.Folders = []model.Folder{{ID: "f1", Name: "One", Collapsed: false}}
	w := btopWindow(t, coll)

	w.toggleFolderCollapsed("f1")
	f, ok := w.folderByID("f1")
	if !ok {
		t.Fatalf("folder f1 vanished")
	}
	if !f.Collapsed {
		t.Fatalf("toggle did not set Collapsed=true")
	}
	if !w.dirty {
		t.Fatalf("toggleFolderCollapsed did not mark dirty")
	}

	w.dirty = false
	w.toggleFolderCollapsed("f1")
	f, _ = w.folderByID("f1")
	if f.Collapsed {
		t.Fatalf("second toggle did not set Collapsed=false")
	}
	if !w.dirty {
		t.Fatalf("second toggle did not mark dirty")
	}

	// Unknown id is a no-op.
	w.dirty = false
	w.toggleFolderCollapsed("ghost")
	if w.dirty {
		t.Fatalf("toggle on unknown id should be a no-op")
	}
}

// SPEC 8: sidebarRows grouping order with 2 folders (one collapsed) plus grouped
// and top-level requests.
func TestBlind_SidebarRows_GroupingOrder(t *testing.T) {
	coll := model.NewCollection("C")
	coll.Folders = []model.Folder{
		{ID: "f1", Name: "One", Collapsed: false},
		{ID: "f2", Name: "Two", Collapsed: true},
	}
	// Flat index layout chosen so request rows must carry the correct flat index.
	coll.Requests = []model.Request{
		{Name: "top0", Method: model.MethodGet, URL: "u/0", FolderID: ""},  // idx 0 top-level
		{Name: "f1a", Method: model.MethodGet, URL: "u/1", FolderID: "f1"}, // idx 1 in f1
		{Name: "f2a", Method: model.MethodGet, URL: "u/2", FolderID: "f2"}, // idx 2 in f2 (collapsed)
		{Name: "f1b", Method: model.MethodGet, URL: "u/3", FolderID: "f1"}, // idx 3 in f1
		{Name: "top1", Method: model.MethodGet, URL: "u/4", FolderID: ""},  // idx 4 top-level
	}
	w := btopWindow(t, coll)

	rows := w.sidebarRows()

	// Expected order:
	//   f1 header, f1's requests (idx 1, idx 3),
	//   f2 header (collapsed -> no requests),
	//   top-level requests (idx 0, idx 4).
	type exp struct {
		isFolder bool
		folderID string
		reqIdx   int
	}
	want := []exp{
		{true, "f1", -1},
		{false, "f1", 1},
		{false, "f1", 3},
		{true, "f2", -1},
		{false, "", 0},
		{false, "", 4},
	}
	if len(rows) != len(want) {
		t.Fatalf("row count: got %d want %d\nrows=%+v", len(rows), len(want), rows)
	}
	for i, e := range want {
		r := rows[i]
		if r.IsFolder != e.isFolder {
			t.Fatalf("row[%d] IsFolder: got %v want %v (%+v)", i, r.IsFolder, e.isFolder, r)
		}
		if e.isFolder {
			if r.FolderID != e.folderID {
				t.Fatalf("row[%d] header FolderID: got %q want %q", i, r.FolderID, e.folderID)
			}
		} else {
			if r.ReqIdx != e.reqIdx {
				t.Fatalf("row[%d] request ReqIdx: got %d want %d", i, r.ReqIdx, e.reqIdx)
			}
			if r.FolderID != e.folderID {
				t.Fatalf("row[%d] request FolderID: got %q want %q", i, r.FolderID, e.folderID)
			}
		}
	}

	// Header rows have IsFolder=true; request rows carry the correct flat index
	// back to the actual request.
	for _, r := range rows {
		if !r.IsFolder {
			if r.ReqIdx < 0 || r.ReqIdx >= len(w.coll.Requests) {
				t.Fatalf("request row ReqIdx out of range: %d", r.ReqIdx)
			}
		}
	}
}

// SPEC 9a: duplicateRequest keeps FolderID (copy stays in same folder).
func TestBlind_DuplicateRequest_KeepsFolderID(t *testing.T) {
	coll := model.NewCollection("C")
	coll.Folders = []model.Folder{{ID: "f1", Name: "One"}}
	coll.Requests = []model.Request{
		{Name: "orig", Method: model.MethodGet, URL: "u/o", FolderID: "f1"},
	}
	w := btopWindow(t, coll)

	w.duplicateRequest(0)

	if len(w.coll.Requests) != 2 {
		t.Fatalf("expected 2 requests after duplicate, got %d", len(w.coll.Requests))
	}
	// The copy is inserted at idx+1.
	dup := w.coll.Requests[1]
	if dup.FolderID != "f1" {
		t.Fatalf("duplicate FolderID: got %q want %q", dup.FolderID, "f1")
	}
	if w.coll.Requests[0].FolderID != "f1" {
		t.Fatalf("original FolderID changed: got %q want %q", w.coll.Requests[0].FolderID, "f1")
	}
}

// SPEC 9b: commitRequest preserves FolderID (editing a grouped request does not
// move it to top-level, even when the edited Request has FolderID == "").
func TestBlind_CommitRequest_PreservesFolderID(t *testing.T) {
	coll := model.NewCollection("C")
	coll.Folders = []model.Folder{{ID: "f1", Name: "One"}}
	coll.Requests = []model.Request{
		{Name: "orig", Method: model.MethodGet, URL: "u/o", FolderID: "f1"},
	}
	w := btopWindow(t, coll)

	// Editor supplies an updated Request WITHOUT a FolderID (the editor never sets
	// it). commitRequest must keep the request's existing folder.
	edited := model.Request{Name: "edited", Method: model.MethodPost, URL: "u/new", FolderID: ""}
	w.commitRequest(0, edited)

	got := w.coll.Requests[0]
	if got.Name != "edited" || got.Method != model.MethodPost || got.URL != "u/new" {
		t.Fatalf("edit not applied: %+v", got)
	}
	if got.FolderID != "f1" {
		t.Fatalf("commitRequest did not preserve FolderID: got %q want %q", got.FolderID, "f1")
	}
}
