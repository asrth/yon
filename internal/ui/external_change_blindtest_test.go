package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/ultramcu/yon/internal/model"
	"github.com/ultramcu/yon/internal/store"
)

// TestFileChangedOnDisk_TrueAfterExternalEdit opens a window bound to a real
// .yon file, confirms it reads as unchanged, then rewrites the file on disk
// (with an extra request, so the size differs) and confirms the window now
// reports the file as changed underneath it.
func TestFileChangedOnDisk_TrueAfterExternalEdit(t *testing.T) {
	ecBTColl := model.NewCollection("ecBT")
	ecBTColl.Requests = []model.Request{
		{Name: "first", Method: model.MethodGet, URL: "http://x/1"},
	}
	ecBTPath := saveCollection(t, ecBTColl, "ecBT.yon")

	a := New(test.NewApp())
	w := a.OpenCollectionWindow(ecBTColl, ecBTPath)

	if w.fileChangedOnDisk() {
		t.Fatal("freshly opened file reports changed-on-disk; want false")
	}

	ecBTBigger := model.NewCollection("ecBT")
	ecBTBigger.Requests = []model.Request{
		{Name: "first", Method: model.MethodGet, URL: "http://x/1"},
		{Name: "second", Method: model.MethodPost, URL: "http://x/2"},
	}
	if err := store.Save(ecBTPath, ecBTBigger); err != nil {
		t.Fatalf("external rewrite: %v", err)
	}

	if !w.fileChangedOnDisk() {
		t.Fatal("after an external edit, fileChangedOnDisk() = false; want true")
	}
}

// TestFileChangedOnDisk_FalseForUntitled pins that a window with no path (never
// saved to disk) never reports a clobbering external change.
func TestFileChangedOnDisk_FalseForUntitled(t *testing.T) {
	ecBTColl := model.NewCollection("ecBT-untitled")
	w := newScopeWindow(t, ecBTColl)
	if w.fileChangedOnDisk() {
		t.Fatal("untitled window reports changed-on-disk; want false")
	}
}
