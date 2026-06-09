package store

import "os"

// FileStamp identifies a file's on-disk state cheaply (modtime + size). Two
// stamps for the same unchanged file compare equal with ==; any external write
// that changes the content also changes the size and/or modtime, so a differing
// stamp signals the file moved underneath us. This is deliberately cheaper than
// hashing — good enough to detect that Yon's in-memory copy is stale.
type FileStamp struct {
	ModTimeNano int64
	Size        int64
}

// StatStamp returns the file's stamp and ok=true if it exists; ok=false on a
// missing file or any stat error (with the FileStamp zero value). It does not
// read the file contents — only os.Stat — so it is cheap to call on every save.
func StatStamp(path string) (FileStamp, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return FileStamp{}, false
	}
	return FileStamp{
		ModTimeNano: fi.ModTime().UnixNano(),
		Size:        fi.Size(),
	}, true
}
