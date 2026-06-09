package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStatStamp_DetectsChange verifies that an external edit which changes the
// file's content (and size) yields a different FileStamp.
func TestStatStamp_DetectsChange(t *testing.T) {
	fsBTPath := filepath.Join(t.TempDir(), "fsBT-change.yon")
	if err := os.WriteFile(fsBTPath, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	s1, ok := StatStamp(fsBTPath)
	if !ok {
		t.Fatal("StatStamp on existing file returned ok=false")
	}

	if err := os.WriteFile(fsBTPath, []byte("a much longer body than before"), 0o644); err != nil {
		t.Fatalf("external rewrite: %v", err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(fsBTPath, later, later); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	s2, ok2 := StatStamp(fsBTPath)
	if !ok2 {
		t.Fatal("StatStamp after rewrite returned ok=false")
	}
	if s1 == s2 {
		t.Fatalf("stamp did not change after external edit: %+v == %+v", s1, s2)
	}
}

// TestStatStamp_StableWhenUnchanged verifies stamping the same untouched file
// twice yields equal stamps.
func TestStatStamp_StableWhenUnchanged(t *testing.T) {
	fsBTPath := filepath.Join(t.TempDir(), "fsBT-stable.yon")
	if err := os.WriteFile(fsBTPath, []byte("steady contents"), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	s1, ok1 := StatStamp(fsBTPath)
	s2, ok2 := StatStamp(fsBTPath)
	if !ok1 || !ok2 {
		t.Fatalf("StatStamp ok flags: %v, %v (want both true)", ok1, ok2)
	}
	if s1 != s2 {
		t.Fatalf("stamp changed for an unchanged file: %+v != %+v", s1, s2)
	}
}

// TestStatStamp_MissingFile verifies that a non-existent path reports ok=false.
func TestStatStamp_MissingFile(t *testing.T) {
	fsBTMissing := filepath.Join(t.TempDir(), "fsBT-nope.yon")
	if _, ok := StatStamp(fsBTMissing); ok {
		t.Fatal("StatStamp on a missing file returned ok=true, want false")
	}
}
