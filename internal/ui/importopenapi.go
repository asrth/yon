package ui

import (
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"

	"github.com/ultramcu/yon/internal/importer"
)

// ImportOpenAPIData converts an OpenAPI 3.0 / Swagger 2.0 document (raw YAML or
// JSON bytes) into a Collection and opens it in a new, untitled window. The empty
// path means the Collection has no backing .yon file yet, so the user must Save As
// to write one. It returns the new Window and the conversion Report on success, or
// the error from importer.ImportOpenAPI otherwise. Kept free of dialogs/IO so it
// can be unit-tested. Mirrors ImportCollectionData.
func (a *App) ImportOpenAPIData(data []byte) (*Window, importer.Report, error) {
	coll, report, err := importer.ImportOpenAPI(data)
	if err != nil {
		return nil, report, err
	}
	w := a.OpenCollectionWindow(coll, "")
	return w, report, nil
}

// importOpenAPI drives the File ▸ Import OpenAPI / Swagger… action: it shows a
// native open dialog filtered to *.yaml/*.yml/*.json (falling back to Fyne's
// in-app dialog when the native backend is unavailable), reads the chosen file,
// converts it into a new untitled Collection window, and — when the conversion
// skipped requests or recorded notes — shows a summary dialog. Any error is
// reported via dialog. Mirrors importCollection.
func (w *Window) importOpenAPI() {
	go func() {
		path, ok, err := nativeOpenSpec("Import OpenAPI / Swagger")
		fyne.Do(func() {
			switch {
			case err != nil:
				w.importOpenAPIFyne() // native unavailable → in-app dialog
			case !ok:
				// cancelled — nothing to do
			default:
				w.importOpenAPIPath(path)
			}
		})
	}()
}

// importOpenAPIFyne is the Fyne in-app fallback for importOpenAPI().
func (w *Window) importOpenAPIFyne() {
	d := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil || rc == nil {
			return
		}
		path := rc.URI().Path()
		_ = rc.Close()
		w.importOpenAPIPath(path)
	}, w.win)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".yaml", ".yml", ".json"}))
	d.Show()
}

// importOpenAPIPath reads the file at path, imports it, and surfaces the outcome:
// an error dialog on failure, or a notes dialog when the import had anything to
// report. Shared by the native and Fyne open flows. Reuses importReportSummary —
// the same report-presenting helper the Postman import uses.
func (w *Window) importOpenAPIPath(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		dialog.ShowError(err, w.win)
		return
	}
	_, report, err := w.app.ImportOpenAPIData(data)
	if err != nil {
		dialog.ShowError(err, w.win)
		return
	}
	if summary := importReportSummary(report); summary != "" {
		dialog.ShowInformation("Imported with notes", summary, w.win)
	}
}
