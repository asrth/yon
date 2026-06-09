package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ultramcu/yon/internal/model"
)

// formFieldTable is an editable table of model.FormField rows, used by the
// Form (application/x-www-form-urlencoded) and Multipart (multipart/form-data)
// body editors. It mirrors kvTable: a VBox of rows with an Enabled check, a Key
// entry and a Value entry, plus an "Add" button. Any edit calls onChange so the
// owner can commit the Request.
//
// In multipart mode each row gains a "File" check (marking the row IsFile) and a
// "Choose…" button that opens a file picker and writes the chosen path into the
// Value entry. In form mode that File column is absent (file parts only make
// sense for multipart bodies).
type formFieldTable struct {
	container *fyne.Container
	rowsBox   *fyne.Container
	rows      []*formFieldRow

	// multipart controls whether the per-row File check + Choose… button are
	// built; form mode omits them.
	multipart bool

	// win is the parent window used as the file picker's dialog parent. It may be
	// nil (e.g. in tests that build the table without a window); the Choose…
	// handler nil-guards it, so the button is harmless when unset.
	win fyne.Window

	onChange func()
}

// formFieldRow is one editable FormField row.
type formFieldRow struct {
	enabled *widget.Check
	key     *widget.Entry
	value   *widget.Entry
	// isFile + chooseBtn exist only in multipart mode; both are nil otherwise.
	isFile    *widget.Check
	chooseBtn *widget.Button
	// filename preserves a per-field override read back into FormField.Filename.
	filename string
	object   fyne.CanvasObject
}

// newFormFieldTable builds a table seeded from fields. When multipart is true
// each row carries a File check + Choose… picker. onChange may be nil. The
// parent window for the file picker is set separately via setWindow (so the
// contract constructor stays free of a window argument).
func newFormFieldTable(fields []model.FormField, multipart bool, onChange func()) *formFieldTable {
	t := &formFieldTable{multipart: multipart, onChange: onChange}
	t.rowsBox = container.NewVBox()

	for _, f := range fields {
		t.appendRow(f)
	}

	add := widget.NewButtonWithIcon("Add", theme.ContentAddIcon(), func() {
		t.appendRow(model.FormField{Enabled: true})
		t.fire()
	})
	add.Importance = widget.LowImportance

	header := t.buildHeader()

	t.container = container.NewBorder(header, add, nil, nil,
		container.NewVScroll(t.rowsBox))
	return t
}

// setWindow records the parent window used as the file picker's dialog parent.
func (t *formFieldTable) setWindow(w fyne.Window) { t.win = w }

// buildHeader returns the column-title row. Multipart adds a "File" column on the
// trailing side to label the per-row File check.
func (t *formFieldTable) buildHeader() fyne.CanvasObject {
	cols := container.NewGridWithColumns(2,
		widget.NewLabelWithStyle("Key", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Value", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	if t.multipart {
		return container.NewBorder(nil, nil,
			widget.NewLabelWithStyle("On", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			widget.NewLabelWithStyle("File", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			cols,
		)
	}
	return container.NewBorder(nil, nil,
		widget.NewLabelWithStyle("On", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		nil,
		cols,
	)
}

// appendRow adds a row widget for f (without firing onChange).
func (t *formFieldTable) appendRow(f model.FormField) {
	r := &formFieldRow{filename: f.Filename}
	// nil handler → SetChecked → wire OnChanged after, so seeding an enabled row
	// during construction doesn't fire onChange (commit) before the owning table
	// is wired up (nil deref), mirroring kvTable.
	r.enabled = widget.NewCheck("", nil)
	r.enabled.SetChecked(f.Enabled)
	r.enabled.OnChanged = func(bool) { t.fire() }

	r.key = widget.NewEntry()
	r.key.SetPlaceHolder("Key")
	r.key.SetText(f.Key)
	r.key.OnChanged = func(string) { t.fire() }

	r.value = widget.NewEntry()
	r.value.SetPlaceHolder("Value")
	r.value.SetText(f.Value)
	r.value.OnChanged = func(string) { t.fire() }

	del := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		t.removeRow(r)
		t.fire()
	})
	del.Importance = widget.LowImportance

	var trailing fyne.CanvasObject = del
	if t.multipart {
		r.isFile = widget.NewCheck("", nil)
		r.isFile.SetChecked(f.IsFile)
		r.isFile.OnChanged = func(file bool) {
			r.applyFileMode(file)
			t.fire()
		}

		r.chooseBtn = widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
			t.chooseFile(r)
		})
		r.chooseBtn.Importance = widget.LowImportance
		r.applyFileMode(f.IsFile) // Choose… is only enabled when File is checked

		trailing = container.NewHBox(r.isFile, r.chooseBtn, del)
	}

	r.object = container.NewBorder(nil, nil, r.enabled, trailing,
		container.NewGridWithColumns(2, r.key, r.value),
	)

	t.rows = append(t.rows, r)
	t.rowsBox.Add(r.object)
	t.rowsBox.Refresh()
}

// applyFileMode enables/disables the Choose… button to match the File check: a
// file part has a path picker; a plain text part does not.
func (r *formFieldRow) applyFileMode(file bool) {
	if r.chooseBtn == nil {
		return
	}
	if file {
		r.chooseBtn.Enable()
	} else {
		r.chooseBtn.Disable()
	}
}

// chooseFile opens a file picker and, on success, writes the chosen path into the
// row's Value entry (Value holds the filesystem path for a file part) and records
// the basename as the part's Filename. It tries the native picker first and falls
// back to Fyne's in-app dialog when the native backend is unavailable. With no
// parent window (tests) it is a no-op.
func (t *formFieldTable) chooseFile(r *formFieldRow) {
	if t.win == nil {
		return
	}
	go func() {
		path, ok, err := nativeOpenAny("Choose file")
		fyne.Do(func() {
			switch {
			case err != nil:
				t.chooseFileFyne(r) // native unavailable → in-app dialog
			case !ok:
				// cancelled — nothing to do
			default:
				t.setRowFile(r, path)
			}
		})
	}()
}

// chooseFileFyne is the Fyne in-app fallback for chooseFile().
func (t *formFieldTable) chooseFileFyne(r *formFieldRow) {
	if t.win == nil {
		return
	}
	d := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil || rc == nil {
			return
		}
		path := rc.URI().Path()
		_ = rc.Close()
		t.setRowFile(r, path)
	}, t.win)
	d.Show()
}

// setRowFile records a chosen file path on a row: the path goes in the Value
// entry, the basename becomes the Filename, and the File check is forced on. Then
// it fires onChange so the owner commits.
func (t *formFieldTable) setRowFile(r *formFieldRow, path string) {
	r.filename = baseName(path)
	r.value.SetText(path) // OnChanged fires onChange
	if r.isFile != nil && !r.isFile.Checked {
		r.isFile.SetChecked(true) // OnChanged → applyFileMode + fire
	}
}

// baseName returns the last path element of p (the filename), handling both
// forward- and back-slash separators so a Windows path's basename is right too.
func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}

// setValue replaces every row with fields, without firing onChange.
func (t *formFieldTable) setValue(fields []model.FormField) {
	for _, r := range t.rows {
		t.rowsBox.Remove(r.object)
	}
	t.rows = nil
	for _, f := range fields {
		t.appendRow(f)
	}
	t.rowsBox.Refresh()
}

// removeRow drops a row from the table.
func (t *formFieldTable) removeRow(target *formFieldRow) {
	for i, r := range t.rows {
		if r == target {
			t.rows = append(t.rows[:i], t.rows[i+1:]...)
			break
		}
	}
	t.rowsBox.Remove(target.object)
	t.rowsBox.Refresh()
}

// fire notifies the owner of a change.
func (t *formFieldTable) fire() {
	if t.onChange != nil {
		t.onChange()
	}
}

// value reads the table back into a []model.FormField, preserving order. Fully
// empty rows (no key, no value) are skipped so an accidental blank row isn't
// persisted; a disabled row with content is kept (Enabled false), since disabling
// is "keep but don't send". IsFile/Filename are only set in multipart mode.
func (t *formFieldTable) value() []model.FormField {
	var out []model.FormField
	for _, r := range t.rows {
		if r.key.Text == "" && r.value.Text == "" {
			continue
		}
		f := model.FormField{
			Key:     r.key.Text,
			Value:   r.value.Text,
			Enabled: r.enabled.Checked,
		}
		if t.multipart && r.isFile != nil && r.isFile.Checked {
			f.IsFile = true
			f.Filename = r.filename
		}
		out = append(out, f)
	}
	return out
}
