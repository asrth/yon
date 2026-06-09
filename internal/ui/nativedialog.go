package ui

import (
	"errors"

	"github.com/ncruces/zenity"
)

// Native OS file dialogs (Finder / Explorer / GTK) via ncruces/zenity. Each
// wrapper returns (path, ok, err): ok is false when the user cancels, and a
// non-nil err means the native backend is unavailable (e.g. Linux without
// `zenity`/`qarma` installed) so the caller should fall back to Fyne's dialog.
//
// zenity calls block, so callers run them on a goroutine and marshal the result
// back onto the UI thread with fyne.Do.

var yonFilter = zenity.FileFilters{
	{Name: "Yon collections", Patterns: []string{"*.yon"}, CaseFold: true},
}

// nativeOpenYon shows a native open dialog filtered to .yon files.
func nativeOpenYon(title string) (string, bool, error) {
	return nativeResult(zenity.SelectFile(zenity.Title(title), yonFilter))
}

// nativeOpenAny shows a native open dialog with no filter (any file type). Used
// to pick a file for a multipart/form-data file part. It returns (path, ok, err)
// like the other native wrappers.
func nativeOpenAny(title string) (string, bool, error) {
	return nativeResult(zenity.SelectFile(zenity.Title(title)))
}

// specFilter selects OpenAPI 3.0 / Swagger 2.0 spec files (YAML or JSON) in the
// native open dialog.
var specFilter = zenity.FileFilters{
	{Name: "OpenAPI / Swagger spec", Patterns: []string{"*.yaml", "*.yml", "*.json"}, CaseFold: true},
}

// nativeOpenSpec shows a native open dialog filtered to OpenAPI/Swagger specs
// (.yaml/.yml/.json). It returns (path, ok, err) like the other native wrappers.
func nativeOpenSpec(title string) (string, bool, error) {
	return nativeResult(zenity.SelectFile(zenity.Title(title), specFilter))
}

// nativeSaveYon shows a native save dialog (filtered to .yon) defaulting to name.
func nativeSaveYon(title, name string) (string, bool, error) {
	return nativeResult(zenity.SelectFileSave(
		zenity.Title(title), zenity.Filename(name), zenity.ConfirmOverwrite(), yonFilter,
	))
}

// nativeSaveAny shows a native save dialog with no filter (for the response body).
func nativeSaveAny(title, name string) (string, bool, error) {
	return nativeResult(zenity.SelectFileSave(
		zenity.Title(title), zenity.Filename(name), zenity.ConfirmOverwrite(),
	))
}

// nativeResult normalises a zenity (path, error) into (path, ok, err): a cancel
// is (", false, nil); an unavailable backend keeps the error for fallback.
func nativeResult(path string, err error) (string, bool, error) {
	if errors.Is(err, zenity.ErrCanceled) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return path, true, nil
}
