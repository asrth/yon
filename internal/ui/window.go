package ui

import (
	"fmt"
	"image/color"
	"net"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ultramcu/yon/internal/model"
	"github.com/ultramcu/yon/internal/store"
	"github.com/ultramcu/yon/internal/tunnel"
	"github.com/ultramcu/yon/internal/updater"
)

// Window hosts exactly one model.Collection: a sidebar listing its Requests and
// a DocTabs area showing the open Requests as Tabs. It owns the Collection's
// in-memory state, its on-disk path (empty when untitled), and its dirty flag.
type Window struct {
	app  *App
	win  fyne.Window
	coll model.Collection

	// path is the .yon file backing this Collection, or "" if never saved.
	path string

	// dirty is true when the in-memory Collection differs from disk.
	dirty bool

	// loadedStamp is the on-disk stamp (modtime+size) of path as Yon last read
	// or wrote it. It is set in newWindow for an on-disk collection and refreshed
	// after every successful save, so fileChangedOnDisk can detect an external
	// edit that a silent Save would clobber. Zero value for an untitled
	// collection (path == "").
	loadedStamp store.FileStamp

	sidebar *widget.List
	tabs    *tabStrip

	// filterQuery is the sidebar search box's current text, stored lower-cased and
	// trimmed. It is DISPLAY-ONLY: it changes only which rows sidebarRows() emits
	// (like folder collapse), never Collection.Requests, openTabs, selectedID, the
	// dirty flag, or anything persisted. Empty means no filter (the full grouped
	// view, collapse respected). See sidebarRows()/requestMatchesFilter.
	filterQuery string

	// filterEntry is the sidebar header's filter text box, kept so a clear control
	// (and tests) can drive/reset it.
	filterEntry *widget.Entry

	// rows is the cached grouped display order (folder headers + request rows)
	// the sidebar List renders. It is recomputed by refreshSidebar() on every
	// change so the List's length() and updateItem() callbacks agree within a
	// single refresh: length() returns len(rows) and updateItem() reads rows[i]
	// to decide whether to render a folder header or a request. A list item index
	// (the VISIBLE row position) is therefore distinct from a request's flat
	// Collection.Requests index (carried in sidebarRow.ReqIdx) and from
	// selectedID, which always stays a flat request index.
	rows []sidebarRow

	// rowHeight caches the uniform sidebar-row height (the List item template's
	// MinSize height) used by the drag-drop hit-test to map a pointer Y to a row.
	// 0 means "not computed yet"; sidebarRowHeight() fills it on first use.
	rowHeight float32

	// Bottom status bar (mockup-v9): the active tab's last response status/time/
	// size on the left, its method · path on the right.
	sbVersion *canvas.Text
	sbStatus  *canvas.Text
	sbMeta    *canvas.Text
	sbReqInfo *canvas.Text

	// sbTunnel is the footer jump-host indicator: a coloured dot + env name shown
	// only while the active environment resolves a complete jump host. It paints
	// green when that host's Tunnel is Connected and grey/red otherwise. The text
	// is hidden (empty) when there is no active jump host. updateTunnelIndicator
	// drives it; the surrounding tappable opens the Tunnels window.
	sbTunnel *canvas.Text

	// sidebarCount is the "N requests" badge in the collection header; kept so
	// add/delete can refresh the number without rebuilding the header.
	sidebarCount *widget.Label

	// sidebarTitle is the collection-name label in the header; kept so
	// updateTitle can refresh it (e.g. after Save As changes the file/name)
	// without rebuilding the header.
	sidebarTitle *widget.Label

	// selectedID is the currently-selected request's FLAT Collection.Requests
	// index (-1 = none) — NOT the visible list-row position. A request row paints
	// its cyan left-accent bar when its ReqIdx matches selectedID, on top of the
	// List's own (visible-row) selection tint. Keeping selectedID a flat index
	// means folder collapse/expand (which changes which rows are visible, and
	// hence visible-row positions) never invalidates the selection, and it stays
	// aligned with openTabs, which is also keyed by flat index.
	selectedID widget.ListItemID

	// openTabs maps a Collection request index to its open editor Tab, so we
	// don't open the same Request twice and can find a Tab to refresh/select.
	openTabs map[int]*requestTab

	// runtimeVars holds session-only values captured from earlier responses (a
	// request's Captures). They feed variables.Scope.Runtime (highest precedence)
	// via varScope, so a captured token resolves in the next send. These are NEVER
	// persisted to the .yon/.env — they live only for this window's session. It
	// starts nil (a nil map is safe in Lookup/varScope) and is lazily created by
	// startSend the first time a Capture writes a value.
	runtimeVars map[string]string

	// envs holds the environments loaded from the collection's sibling files
	// (empty for an unsaved collection). envSelect is the sidebar-header picker
	// that chooses the active environment; pendingEnvDeletes queues environment
	// files to remove on the next manager Save (set by Rename/Delete).
	envs              []model.Environment
	envSelect         *widget.Select
	pendingEnvDeletes []string

	// updateBanner is a dismissible notice shown at the top of the window when a
	// newer release is found; updateLabel is its text, and pendingRel/pendingAsset
	// hold the release whose asset the Download button fetches.
	updateBanner *fyne.Container
	updateLabel  *widget.Label
	pendingRel   updater.Release
	pendingAsset updater.Asset

	// varsPanel is the right-side Variables dock (Dev A's widget). varsVisible
	// tracks whether it is currently docked; it is seeded from Preferences on
	// construct so the panel re-opens at the same open/closed state next launch.
	// varsToggle is the footer "Variables" tappable that flips it.
	varsPanel   *varsPanel
	varsVisible bool
	varsToggle  *canvas.Text

	// reqLog is the combined HTTP request log: one entry per send (success or
	// error), newest last. It is capped at maxLogEntries (oldest dropped) and is
	// SESSION-ONLY — never persisted to disk. logPanel is the bottom-docked log
	// widget (Dev A's), logVisible tracks whether it is docked (seeded from
	// Preferences on construct so it re-opens at the same state next launch), and
	// logToggle is the footer "Log" tappable that flips it.
	reqLog     []logEntry
	logPanel   *logPanel
	logVisible bool
	logToggle  *canvas.Text

	// contentSplit is the inner sidebar|editor HSplit; it is reused (never
	// rebuilt) across Variables-panel toggles so its drag offset — the user's
	// sidebar/editor sizing — is preserved. contentTop/contentBottom are the
	// fixed banner and status-bar rows of the window's Border layout, kept so
	// applyContent can rebuild only the center (split, optionally wrapped in an
	// outer HSplit with the Variables panel) without rebuilding them.
	contentSplit  *container.Split
	contentTop    fyne.CanvasObject
	contentBottom fyne.CanvasObject
}

// prefKeyVarsPanelOpen persists whether the right-side Variables dock is open,
// so it re-opens at the same state on next launch (default closed/hidden).
const prefKeyVarsPanelOpen = "ui.varsPanelOpen"

// prefKeyLogPanelOpen persists whether the bottom-side Request Log dock is open,
// so it re-opens at the same state on next launch (default closed/hidden).
const prefKeyLogPanelOpen = "ui.logPanelOpen"

// maxLogEntries caps the in-memory request log so a long session can't grow it
// without bound; appendLog drops the oldest entries past this many.
const maxLogEntries = 1000

// newWindow builds (but does not Show) a Window for coll. path is "" for an
// untitled Collection.
func newWindow(app *App, coll model.Collection, path string) *Window {
	w := &Window{
		app:        app,
		coll:       coll,
		path:       path,
		openTabs:   make(map[int]*requestTab),
		selectedID: -1,
	}
	w.win = app.fyneApp.NewWindow("")
	w.win.SetIcon(appIcon)

	// Record the on-disk stamp of the backing file so a later Save can detect an
	// external edit and prompt instead of silently clobbering it. Untitled
	// collections have no file (and no stamp) until Save As adopts a path.
	if path != "" {
		w.loadedStamp, _ = store.StatStamp(path)
	}

	w.loadEnvironments()
	w.buildSidebar()
	w.buildTabs()
	w.buildMenu()

	// Build the Variables dock after buildSidebar/buildTabs so its constructor's
	// reads (env/collection/runtime vars) see a fully-wired window. Seed its
	// open/closed state from Preferences (default hidden).
	w.varsPanel = newVarsPanel(w)
	w.varsVisible = w.app.prefs().BoolWithFallback(prefKeyVarsPanelOpen, false)

	// Build the bottom Request Log dock alongside the Variables dock. Seed its
	// open/closed state from Preferences (default hidden), same as the vars dock.
	w.logPanel = newLogPanel(w)
	w.logVisible = w.app.prefs().BoolWithFallback(prefKeyLogPanelOpen, false)

	split := container.NewHSplit(
		container.NewBorder(w.buildSidebarHeader(), nil, nil, nil, w.sidebar),
		w.tabs.object(),
	)
	split.SetOffset(0.22)
	w.contentSplit = split
	w.contentTop = w.buildUpdateBanner()
	w.contentBottom = w.buildStatusBar()
	if w.varsVisible {
		w.varsPanel.refresh()
		w.syncVarsToggle()
	}
	if w.logVisible {
		w.logPanel.refresh()
		w.syncLogToggle()
	}
	w.applyContent()
	w.updateStatusBar()

	// Cmd/Ctrl+F opens find on the active response (also in Edit ▸ Find…); Esc
	// closes it. These fire when focus is on the response area; the Edit menu and
	// the find field's own handlers cover the cases where an Entry holds focus.
	w.win.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyF, Modifier: fyne.KeyModifierShortcutDefault},
		func(fyne.Shortcut) {
			if rt := w.activeTab(); rt != nil {
				rt.response.openFind()
			}
		},
	)
	w.win.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyEscape},
		func(fyne.Shortcut) {
			if rt := w.activeTab(); rt != nil {
				rt.response.closeFind()
			}
		},
	)
	// Cmd/Ctrl+W closes the active request tab (the standard close-tab shortcut;
	// not Cmd+Q, which macOS reserves for Quit). Fires when focus is on the tab /
	// response area; clicking a tab's × always works regardless of focus.
	w.win.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyW, Modifier: fyne.KeyModifierShortcutDefault},
		func(fyne.Shortcut) {
			if rt := w.activeTab(); rt != nil {
				w.closeTab(rt.tab)
			}
		},
	)
	w.win.Resize(fyne.NewSize(1100, 720))

	w.win.SetCloseIntercept(w.onCloseRequested)
	w.updateTitle()
	return w
}

// applyContent sets the window content to the standard banner|status-bar Border
// around a center region. When the Variables dock is hidden the center is the
// inner sidebar|editor split alone — byte-for-byte the original layout. When it
// is visible the center is a fresh outer HSplit of [that same split | the
// Variables panel]. The inner split object (w.contentSplit) is REUSED in both
// cases, so its drag offset (the user's sidebar/editor sizing) is never reset by
// a toggle; only the outer wrapper is rebuilt.
func (w *Window) applyContent() {
	center := fyne.CanvasObject(w.contentSplit)
	if w.varsVisible && w.varsPanel != nil {
		outer := container.NewHSplit(w.contentSplit, w.varsPanel.container)
		outer.SetOffset(0.78)
		center = outer
	}
	// The Request Log docks at the BOTTOM, composing with the optional right vars
	// dock: a fresh outer VSplit of [center | log panel]. When the log is hidden
	// the center is unchanged, so log+vars both hidden is byte-identical to the
	// original Border.
	if w.logVisible && w.logPanel != nil {
		vs := container.NewVSplit(center, w.logPanel.container)
		vs.SetOffset(0.75)
		center = vs
	}
	w.win.SetContent(container.NewBorder(w.contentTop, w.contentBottom, nil, nil, center))
}

// toggleVarsPanel shows/hides the right-side Variables dock. Becoming visible
// refreshes the panel first (so it reflects current vars), then the center is
// rebuilt via applyContent, the footer toggle restyled, and the new open/closed
// state persisted so it survives a restart.
func (w *Window) toggleVarsPanel() {
	w.varsVisible = !w.varsVisible
	if w.varsVisible && w.varsPanel != nil {
		w.varsPanel.refresh()
	}
	w.applyContent()
	w.syncVarsToggle()
	w.app.prefs().SetBool(prefKeyVarsPanelOpen, w.varsVisible)
}

// refreshVarsPanel re-renders the Variables dock, but only when it is actually
// docked — hidden, it is a cheap no-op (the panel re-reads on its next open).
func (w *Window) refreshVarsPanel() {
	if w.varsPanel != nil && w.varsVisible {
		w.varsPanel.refresh()
	}
}

// syncVarsToggle restyles the footer "Variables" toggle to reflect the dock's
// state: accented + bold while open, muted while closed. Safe before the toggle
// is built (no-op).
func (w *Window) syncVarsToggle() {
	if w.varsToggle == nil {
		return
	}
	if w.varsVisible {
		w.varsToggle.Color = theme.Color(theme.ColorNamePrimary)
		w.varsToggle.TextStyle = fyne.TextStyle{Bold: true}
	} else {
		w.varsToggle.Color = theme.Color(theme.ColorNamePlaceHolder)
		w.varsToggle.TextStyle = fyne.TextStyle{}
	}
	w.varsToggle.Refresh()
}

// appendLog records one finished send (success or error) in the combined request
// log, newest last, then refreshes the log dock (a no-op while it is hidden). The
// log is capped at maxLogEntries: once it would exceed that, the oldest entries
// are dropped so a long session can't grow it without bound.
func (w *Window) appendLog(e logEntry) {
	w.reqLog = append(w.reqLog, e)
	if len(w.reqLog) > maxLogEntries {
		w.reqLog = w.reqLog[len(w.reqLog)-maxLogEntries:]
	}
	w.refreshLogPanel()
}

// clearLog empties the request log and refreshes the dock. Wired to the log
// panel's Clear button (via the contract w.clearLog()).
func (w *Window) clearLog() {
	w.reqLog = nil
	w.refreshLogPanel()
}

// refreshLogPanel re-renders the Request Log dock, but only when it is actually
// docked — hidden, it is a cheap no-op (the panel re-reads on its next open).
func (w *Window) refreshLogPanel() {
	if w.logPanel != nil && w.logVisible {
		w.logPanel.refresh()
	}
}

// toggleLogPanel shows/hides the bottom Request Log dock. Becoming visible
// refreshes the panel first (so it reflects the current log), then the center is
// rebuilt via applyContent, the footer toggle restyled, and the new open/closed
// state persisted so it survives a restart.
func (w *Window) toggleLogPanel() {
	w.logVisible = !w.logVisible
	if w.logVisible && w.logPanel != nil {
		w.logPanel.refresh()
	}
	w.applyContent()
	w.syncLogToggle()
	w.app.prefs().SetBool(prefKeyLogPanelOpen, w.logVisible)
}

// syncLogToggle restyles the footer "Log" toggle to reflect the dock's state:
// accented + bold while open, muted while closed. Safe before the toggle is built
// (no-op).
func (w *Window) syncLogToggle() {
	if w.logToggle == nil {
		return
	}
	if w.logVisible {
		w.logToggle.Color = theme.Color(theme.ColorNamePrimary)
		w.logToggle.TextStyle = fyne.TextStyle{Bold: true}
	} else {
		w.logToggle.Color = theme.Color(theme.ColorNamePlaceHolder)
		w.logToggle.TextStyle = fyne.TextStyle{}
	}
	w.logToggle.Refresh()
}

// buildSidebarHeader is the collection header above the request list: a folder
// icon + collection name, a count badge of how many Requests it holds, and a
// compact "Add" button. Matches the v2 mockup's "Yon Test Server  [13]" row.
func (w *Window) buildSidebarHeader() fyne.CanvasObject {
	w.sidebarTitle = widget.NewLabelWithStyle(
		collectionDisplayName(w.coll.Name, w.path),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title := w.sidebarTitle

	w.sidebarCount = widget.NewLabel("")
	w.refreshSidebarCount()

	add := widget.NewButtonWithIcon("", theme.ContentAddIcon(), w.addRequest)
	add.Importance = widget.MediumImportance

	// "New Folder" affordance next to the request "Add" button, opening a name
	// dialog and creating an empty, expanded folder.
	newFolder := widget.NewButtonWithIcon("", theme.FolderNewIcon(), w.showNewFolder)
	newFolder.Importance = widget.MediumImportance

	folder := widget.NewIcon(theme.FolderIcon())
	header := container.NewBorder(
		nil, nil,
		container.NewHBox(folder, title),
		container.NewHBox(w.sidebarCount, newFolder, add),
		nil,
	)

	// Save / Save As toolbar row under the collection title.
	saveBtn := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() { w.save(nil) })
	saveBtn.Importance = widget.MediumImportance
	saveAsBtn := widget.NewButton("Save As…", func() { w.saveAs(nil) })
	saveAsBtn.Importance = widget.MediumImportance
	toolbar := container.NewHBox(saveBtn, saveAsBtn)

	// Environment picker row: a label + the compact selector ("No Environment" +
	// each environment + "Manage Environments…").
	envRow := container.NewBorder(nil, nil,
		widget.NewLabel("Env"), nil,
		w.buildEnvSelector(),
	)

	return container.NewVBox(header, toolbar, envRow, w.buildSidebarFilter(), widget.NewSeparator())
}

// buildSidebarFilter builds the DISPLAY-ONLY request filter row: a search Entry
// whose text filters which sidebar rows are shown, plus a "✕" clear button. Typing
// updates w.filterQuery (lower-cased + trimmed) and reruns the existing refresh
// path (refreshSidebar recomputes rows + the count badge); it never mutates the
// Collection, openTabs, selectedID, or the dirty flag. See sidebarRows().
func (w *Window) buildSidebarFilter() fyne.CanvasObject {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Filter requests…")
	entry.OnChanged = func(s string) {
		w.filterQuery = strings.ToLower(strings.TrimSpace(s))
		// Display-only: just recompute/redraw the grouped rows + count badge.
		w.refreshSidebar()
	}
	w.filterEntry = entry

	clear := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		// Empties the box; the Entry's OnChanged clears w.filterQuery and refreshes.
		entry.SetText("")
	})
	clear.Importance = widget.LowImportance

	return container.NewBorder(nil, nil, nil, clear, entry)
}

// refreshSidebarCount updates the request-count badge in the collection header.
// While a (display-only) filter is active AND the window is clean, it shows the
// number of MATCHING requests instead of the total, so the badge reflects what's
// visible; if dirty, it leaves the total as-is so the badge still signals size.
func (w *Window) refreshSidebarCount() {
	if w.sidebarCount == nil {
		return
	}
	if w.filterQuery != "" && !w.dirty {
		w.sidebarCount.SetText(fmt.Sprintf("%d", w.matchingRequestCount()))
		return
	}
	w.sidebarCount.SetText(fmt.Sprintf("%d", len(w.coll.Requests)))
}

// matchingRequestCount returns how many requests match the current filterQuery
// (all of them when the filter is empty). Display-only; reads nothing it mutates.
func (w *Window) matchingRequestCount() int {
	n := 0
	for i := range w.coll.Requests {
		if requestMatchesFilter(w.coll.Requests[i], w.filterQuery) {
			n++
		}
	}
	return n
}

// buildSidebar creates the grouped request list. It renders the order produced
// by sidebarRows() (cached in w.rows): folder header rows interleaved with their
// request rows, then top-level requests last. A single List template must serve
// BOTH row kinds, so each list item is a sidebarItem — a container holding a
// folderRow AND a verbRow, exactly one shown per row (see sidebarItem.set). The
// List stays virtualized: a sidebarItem is built once in CreateItem and morphed
// per visible-row id in UpdateItem.
//
// IMPORTANT (visible-row vs flat index): the List's own length and selection are
// in VISIBLE-row units (positions in w.rows). selectedID, openTabs and every
// request operation are in FLAT Collection.Requests units. UpdateItem and
// OnSelected translate between them via w.rows[visibleID].
func (w *Window) buildSidebar() {
	w.rows = w.sidebarRows()
	w.sidebar = widget.NewList(
		func() int { return len(w.rows) },
		func() fyne.CanvasObject {
			it := newSidebarItem()
			// Request-row callbacks (only fired while the item shows its verbRow).
			// Right-click offers Duplicate, Delete, and "Move to folder…"; left-
			// click routes through selectByReqIdx so the full select/open path runs.
			it.req.onDuplicate = w.duplicateRequest
			it.req.onDelete = w.confirmDeleteRequest
			it.req.moveMenu = w.moveToFolderMenu
			it.req.onTap = func(reqIdx int) { w.selectByReqIdx(reqIdx) }
			// Dragging a request row reorders it (incl. across folders): DragEnd
			// hands us the dragged request and where the pointer ended up.
			it.req.onDragEnd = w.handleRequestDrop
			// Folder-row callbacks (only fired while the item shows its folderRow).
			// A primary tap (header or chevron) toggles collapse; right-click offers
			// Rename / Delete.
			it.folder.onToggle = w.toggleFolder
			it.folder.onRename = w.showRenameFolder
			it.folder.onDelete = w.confirmDeleteFolder
			return it
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id < 0 || id >= len(w.rows) {
				return
			}
			row := w.rows[id]
			it := o.(*sidebarItem)
			if row.IsFolder {
				f, ok := w.folderByID(row.FolderID)
				if !ok {
					return
				}
				it.setFolder(*f, w.folderRequestCount(row.FolderID))
				return
			}
			if row.ReqIdx < 0 || row.ReqIdx >= len(w.coll.Requests) {
				return
			}
			// A request row shows the accent when its FLAT index matches selectedID
			// (not when the visible-row id matches — those are different spaces).
			it.setRequest(row.ReqIdx, w.coll.Requests[row.ReqIdx], row.ReqIdx == w.selectedID)
		},
	)
	// The List's selection is by visible row. Translate it to the row's flat
	// request index: a folder header carries no request, so selecting one just
	// clears the List selection (its own tap already toggled collapse). A request
	// row drives the existing select/open path keyed by its flat ReqIdx.
	w.sidebar.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(w.rows) {
			return
		}
		row := w.rows[id]
		if row.IsFolder {
			w.sidebar.Unselect(id)
			return
		}
		w.selectedID = row.ReqIdx
		w.sidebar.Refresh() // repaint accent bars
		w.openRequestTab(row.ReqIdx)
	}
	w.sidebar.OnUnselected = func(widget.ListItemID) {
		w.selectedID = -1
		w.sidebar.Refresh()
	}
}

// selectByReqIdx selects the visible row whose flat request index is reqIdx,
// routing through widget.List.Select so OnSelected runs the full programmatic
// path (selectedID update + accent repaint + openRequestTab). Request rows carry
// a flat index but the List selects by VISIBLE position, so we map reqIdx →
// visible-row id through the cached w.rows. No-op if the request isn't currently
// visible (e.g. its folder is collapsed).
func (w *Window) selectByReqIdx(reqIdx int) {
	for i, r := range w.rows {
		if !r.IsFolder && r.ReqIdx == reqIdx {
			w.sidebar.Select(i)
			return
		}
	}
}

// unselectReqIdx clears the List's (visible-row) selection for the request whose
// flat index is reqIdx, mapping reqIdx → visible-row id through w.rows. Used when
// a request's tab closes or it is deleted, so its row can be clicked to reopen
// (widget.List.Select no-ops on an already-selected row). No-op if the request
// isn't currently visible.
func (w *Window) unselectReqIdx(reqIdx int) {
	for i, r := range w.rows {
		if !r.IsFolder && r.ReqIdx == reqIdx {
			w.sidebar.Unselect(i)
			return
		}
	}
}

// refreshSidebar recomputes the cached grouped rows and refreshes the list +
// count badge. Call it after ANY change that affects the sidebar (add/rename/
// delete folder, move request, collapse, add/delete/duplicate request) so the
// List's length() and updateItem() read a consistent row set.
func (w *Window) refreshSidebar() {
	w.rows = w.sidebarRows()
	if w.sidebar != nil {
		w.sidebar.Refresh()
	}
	w.refreshSidebarCount()
}

// handleRequestDrop completes a drag-to-reorder. srcReqIdx is the dragged
// request's flat index; dropAbsY is the absolute pointer Y where the drag ended.
// It maps dropAbsY to a visible sidebar row (and which half of it the pointer is
// in) from the LIVE rendered geometry, asks Dev A's pure dropTarget where the
// request should land, and calls moveRequest. Dropping a row onto itself (same
// folder, same slot) is guarded as a no-op so it never needlessly marks dirty.
//
// Limitations (v1): the target must be within the visible list area — we don't
// auto-scroll to off-screen rows. A drop below the last rendered row is treated
// as "top-level, at the end" (visibleRow out of range → dropTarget returns that).
// While a sidebar FILTER is active the displayed rows are a partial view of the
// flat order, so anchoring a reorder against them would be ambiguous — drag
// reorder is disabled (a no-op) until the filter is cleared.
func (w *Window) handleRequestDrop(srcReqIdx int, dropAbsY float32) {
	if w.filterQuery != "" {
		return // reordering against a filtered (partial) view is disabled, see doc
	}

	visibleRow, below := w.rowAtAbsY(dropAbsY)
	destFolderID, before := w.dropTarget(srcReqIdx, visibleRow, below)

	// No-op guard: dropping a request before itself, with no folder change, would
	// move nothing — skip it so the gesture doesn't dirty the collection.
	if before == srcReqIdx && srcReqIdx >= 0 && srcReqIdx < len(w.coll.Requests) &&
		w.coll.Requests[srcReqIdx].FolderID == destFolderID {
		return
	}

	w.moveRequest(srcReqIdx, destFolderID, before)
}

// rowAtAbsY maps an absolute canvas Y to the sidebar's visible-row index (an
// index into w.rows / sidebarRows) plus whether the pointer sits in that row's
// LOWER half. It works from the list's PUBLIC geometry rather than walking
// internal renderers: the list's absolute top (driver.AbsolutePositionForObject),
// its scroll offset (List.GetScrollOffset), and the uniform padded row height
// (the item template's MinSize plus theme padding — the sidebar gives every row
// the same height, so widget.List stacks them at i*paddedHeight in content space).
//
//	contentY = (absY - listTop) + scrollOffset     // Y within the scrolled content
//	row      = floor(contentY / paddedRowHeight)    // which stacked row that is
//	below    = (contentY mod paddedRowHeight) > rowHeight/2
//
// Returns (noMoveTarget, false) when absY is above the first row or at/below the
// end of the last row — i.e. no row contains it. dropTarget reads an out-of-range
// visibleRow as "top-level, at the end", the intended behaviour for a drop in the
// empty space past the last request.
func (w *Window) rowAtAbsY(absY float32) (visibleRow int, below bool) {
	if w.sidebar == nil || len(w.rows) == 0 {
		return noMoveTarget, false
	}

	listTop := fyne.CurrentApp().Driver().AbsolutePositionForObject(w.sidebar).Y
	contentY := (absY - listTop) + w.sidebar.GetScrollOffset()
	if contentY < 0 {
		return noMoveTarget, false // above the first row
	}

	rowHeight := w.sidebarRowHeight()
	padding := theme.Padding()
	paddedHeight := rowHeight + padding
	if paddedHeight <= 0 {
		return noMoveTarget, false
	}

	idx := int(contentY / paddedHeight)
	if idx < 0 || idx >= len(w.rows) {
		return noMoveTarget, false // below the last row
	}

	// Position WITHIN this row (0..paddedHeight). Falling in a row's trailing
	// padding gap counts as the lower half of that row.
	inRow := contentY - float32(idx)*paddedHeight
	below = inRow > rowHeight/2
	return idx, below
}

// sidebarRowHeight is the uniform height widget.List gives each sidebar row: the
// item template's MinSize height. Built once and cached so the drop hit-test
// doesn't reconstruct a widget per drag.
func (w *Window) sidebarRowHeight() float32 {
	if w.rowHeight == 0 {
		w.rowHeight = newSidebarItem().MinSize().Height
	}
	return w.rowHeight
}

// folderRequestCount returns how many requests currently belong to folder id,
// shown in the folder header badge.
func (w *Window) folderRequestCount(id string) int {
	n := 0
	for i := range w.coll.Requests {
		if w.coll.Requests[i].FolderID == id {
			n++
		}
	}
	return n
}

// buildTabs creates the custom browser-card tab strip holding open Request
// editors. Closing a card detaches its editor; the underlying Request stays in
// the Collection. Selecting a card refreshes the status bar.
func (w *Window) buildTabs() {
	w.tabs = newTabStrip()
	w.tabs.OnSelected = func(*tabCard) { w.updateStatusBar() }
	w.tabs.OnClose = w.closeTab
}

// openRequestTab opens (or re-selects) the Request at Collection index idx as a
// Tab. Idempotent: re-selecting an already-open Request just focuses its Tab.
func (w *Window) openRequestTab(idx int) {
	if idx < 0 || idx >= len(w.coll.Requests) {
		return
	}
	if rt, ok := w.openTabs[idx]; ok {
		w.tabs.Select(rt.tab)
		return
	}
	rt := newRequestTab(w, idx)
	w.openTabs[idx] = rt
	w.tabs.Append(rt.tab)
	w.tabs.Select(rt.tab)
}

// closeTab removes a card and forgets its editor. Called from the strip's
// OnClose when a card's "×" is tapped.
func (w *Window) closeTab(card *tabCard) {
	closedIdx := -1
	for idx, rt := range w.openTabs {
		if rt.tab == card {
			rt.cancelInFlight()
			delete(w.openTabs, idx)
			closedIdx = idx
			break
		}
	}
	w.tabs.Remove(card)
	// Drop the sidebar selection if it pointed at the closed tab, so its row can
	// be clicked again to reopen it — widget.List.Select no-ops on the already-
	// selected row, which would otherwise leave the request un-reopenable.
	if closedIdx >= 0 && closedIdx == w.selectedID {
		w.selectedID = -1
		w.unselectReqIdx(closedIdx) // closedIdx is a flat index; map to its visible row
	}
	w.updateStatusBar()
}

// addRequest appends a new empty Request to the Collection and opens it.
func (w *Window) addRequest() {
	req := model.Request{
		Method: model.MethodGet,
		Auth:   model.Auth{Kind: model.AuthInherit},
		Body:   model.Body{Type: model.BodyNone},
	}
	// append + mark dirty + refresh sidebar + open the new row. Shared with the
	// New Request from cURL flow via appendAndOpenRequest (see fromcurl.go).
	w.appendAndOpenRequest(req)
}

// confirmDeleteRequest asks the user to confirm removing the Request at idx, and
// on confirm calls deleteRequest. The dialog names the Request so it's clear
// which row is being removed. Wired to a verbRow's right-click Delete item.
func (w *Window) confirmDeleteRequest(idx int) {
	if idx < 0 || idx >= len(w.coll.Requests) {
		return
	}
	name := w.coll.Requests[idx].DisplayName()
	dialog.NewConfirm("Delete request",
		fmt.Sprintf("Delete request %q?", name),
		func(ok bool) {
			if ok {
				w.deleteRequest(idx)
			}
		}, w.win).Show()
}

// deleteRequest removes the Request at idx from the Collection and re-indexes the
// index-keyed state that the removal shifts.
//
// openTabs and selectedID are keyed by slice index, so deleting Requests[idx]
// shifts every later element down by one and those keys must move with them:
//   - If a tab is open for idx, cancel its in-flight send and remove its card.
//   - Rebuild openTabs so every key k > idx becomes k-1 (keys < idx unchanged),
//     built into a fresh map so the shift can't clobber an existing entry.
//   - selectedID: == idx → cleared to -1 (and unselected); > idx → decremented.
//
// No dialog here (confirmDeleteRequest owns the prompt) so it's directly testable.
func (w *Window) deleteRequest(idx int) {
	if idx < 0 || idx >= len(w.coll.Requests) {
		return
	}

	// Drop the deleted request's open tab (if any), cancelling its in-flight send.
	if rt, ok := w.openTabs[idx]; ok {
		rt.cancelInFlight()
		w.tabs.Remove(rt.tab)
		delete(w.openTabs, idx)
	}

	// Remove the request from the slice, shifting later elements down by one.
	w.coll.Requests = append(w.coll.Requests[:idx], w.coll.Requests[idx+1:]...)

	// Re-key openTabs into a fresh map: keys < idx stay, keys > idx drop by one.
	reindexed := make(map[int]*requestTab, len(w.openTabs))
	for k, rt := range w.openTabs {
		if k > idx {
			k--
		}
		reindexed[k] = rt
		rt.idx = k // keep each tab's own back-reference in sync
	}
	w.openTabs = reindexed

	// Adjust the selection to follow the shift.
	switch {
	case w.selectedID == idx:
		w.selectedID = -1
		// idx is a flat index; w.rows still holds the pre-delete cache here, so map
		// the deleted request's flat index to its (about-to-be-removed) visible row.
		w.unselectReqIdx(idx)
	case w.selectedID > idx:
		w.selectedID--
	}

	w.markDirty()
	w.refreshSidebar()
	w.updateStatusBar()
}

// duplicateName derives the copy's name: a named request gets a " copy" suffix
// so it's distinguishable in the sidebar; an unnamed one stays empty (its row
// shows the derived DisplayName). Pure, so it's directly testable.
func duplicateName(name string) string {
	if name == "" {
		return ""
	}
	return name + " copy"
}

// duplicateRequest inserts an independent copy of the Request at idx directly
// after it, then selects/opens the copy.
//
// The copy must be DEEP: a struct copy shares the Params/Headers backing arrays,
// so editing the duplicate's params would silently corrupt the original — we
// clone those slices to break the aliasing. Inserting at idx+1 shifts every later
// element up by one, so the index-keyed state must move with them (mirrors the
// re-index in deleteRequest, but upward):
//   - Rebuild openTabs into a fresh map so every key k > idx becomes k+1 (keys
//     <= idx unchanged), keeping the SAME *requestTab and syncing its rt.idx.
//   - selectedID > idx → incremented so it keeps pointing at the same Request.
//
// Selecting idx+1 last (after the re-index) drives OnSelected for the new copy.
func (w *Window) duplicateRequest(idx int) {
	if idx < 0 || idx >= len(w.coll.Requests) {
		return
	}

	// Deep copy: struct copy first, then clone the slices so the duplicate owns
	// independent Params/Headers and can't mutate the original through a shared
	// backing array. Auth/Body hold only scalars, so the struct copy suffices.
	src := w.coll.Requests[idx]
	dup := src
	dup.Params = append([]model.Param(nil), src.Params...)
	dup.Headers = append([]model.Param(nil), src.Headers...)
	dup.Name = duplicateName(src.Name)

	// Insert the copy at idx+1, shifting later elements up by one.
	w.coll.Requests = slices.Insert(w.coll.Requests, idx+1, dup)

	// Re-key openTabs into a fresh map: keys <= idx stay, keys > idx rise by one.
	reindexed := make(map[int]*requestTab, len(w.openTabs))
	for k, rt := range w.openTabs {
		if k > idx {
			k++
		}
		reindexed[k] = rt
		rt.idx = k // keep each tab's own back-reference in sync
	}
	w.openTabs = reindexed

	// A selection after the insertion point moves up with its Request.
	if w.selectedID > idx {
		w.selectedID++
	}

	w.markDirty()
	w.refreshSidebar()
	w.updateStatusBar()

	// Select via the sidebar so the new copy's selection + accent + tab all sync.
	// The copy is at flat index idx+1; selectByReqIdx maps that to its visible row
	// (the copy inherits the original's FolderID, so it sits under the same folder).
	w.selectByReqIdx(idx + 1)
}

// commitRequest writes an edited Request back into the Collection at idx and
// refreshes the sidebar label + Tab title. Called by the editor on every change.
func (w *Window) commitRequest(idx int, req model.Request) {
	if idx < 0 || idx >= len(w.coll.Requests) {
		return
	}
	// FolderID is grouping metadata the editor does not own (current() never sets
	// it), so preserve the request's existing folder across an edit — otherwise
	// editing a grouped request would silently move it back to top-level.
	req.FolderID = w.coll.Requests[idx].FolderID
	w.coll.Requests[idx] = req
	w.markDirty()
	w.sidebar.Refresh()
	if rt, ok := w.openTabs[idx]; ok {
		rt.tab.setRequest(req.Method, req.DisplayName(), rt.dirty)
	}
}

// --- Folder operations -------------------------------------------------------
//
// Folders are a one-level grouping layer over the flat Collection.Requests
// slice (see model.Folder): each Request carries a FolderID, but its index in
// Requests never changes, so all index-keyed state (openTabs, selectedID,
// deleteRequest, duplicateRequest, session) is untouched by these operations.
// These methods mutate the model + mark dirty only; rendering is Dev B's job.

// sidebarRow is one row of the grouped sidebar display order. A row is either a
// folder header (IsFolder true, FolderID set, ReqIdx == -1) or a request row
// (IsFolder false, ReqIdx is the flat Collection.Requests index, FolderID is the
// request's owning folder or "" for top-level).
type sidebarRow struct {
	FolderID string
	ReqIdx   int
	IsFolder bool
}

// addFolder appends a new Folder with the given (trimmed) name and a fresh
// unique id to the Collection, marks the window dirty, and returns the new id.
// It does not touch the UI.
func (w *Window) addFolder(name string) string {
	id := w.newFolderID()
	w.coll.Folders = append(w.coll.Folders, model.Folder{ID: id, Name: strings.TrimSpace(name)})
	w.markDirty()
	return id
}

// newFolderID returns a folder id unique within the current Collection, retrying
// on the (astronomically rare) collision so rapid creation can't produce dupes.
func (w *Window) newFolderID() string {
	for {
		id := model.NewFolderID()
		if _, ok := w.folderByID(id); !ok {
			return id
		}
	}
}

// renameFolder sets the named folder's Name (trimmed) and marks dirty. No-op if
// the id is unknown.
func (w *Window) renameFolder(id, name string) {
	if f, ok := w.folderByID(id); ok {
		f.Name = strings.TrimSpace(name)
		w.markDirty()
	}
}

// deleteFolder removes the folder from the Collection and reparents every
// request that was in it to top-level (FolderID = ""). Requests are KEPT — their
// flat indices are unchanged — so deleting a folder never loses requests. No-op
// if the id is unknown.
func (w *Window) deleteFolder(id string) {
	idx := w.folderIndex(id)
	if idx < 0 {
		return
	}
	w.coll.Folders = append(w.coll.Folders[:idx], w.coll.Folders[idx+1:]...)
	for i := range w.coll.Requests {
		if w.coll.Requests[i].FolderID == id {
			w.coll.Requests[i].FolderID = ""
		}
	}
	w.markDirty()
}

// moveRequestToFolder sets the FolderID of the Request at reqIdx (folderID "" =
// top-level). It guards reqIdx range and that folderID exists (or is ""), and
// does NOT reorder Requests, so flat indices / openTabs stay valid. No-op if
// reqIdx is out of range or folderID names no existing folder.
func (w *Window) moveRequestToFolder(reqIdx int, folderID string) {
	if reqIdx < 0 || reqIdx >= len(w.coll.Requests) {
		return
	}
	if folderID != "" {
		if _, ok := w.folderByID(folderID); !ok {
			return
		}
	}
	w.coll.Requests[reqIdx].FolderID = folderID
	w.markDirty()
}

// toggleFolderCollapsed flips the named folder's Collapsed flag and marks dirty.
// No-op if the id is unknown.
func (w *Window) toggleFolderCollapsed(id string) {
	if f, ok := w.folderByID(id); ok {
		f.Collapsed = !f.Collapsed
		w.markDirty()
	}
}

// folderByID returns a pointer to the Folder with the given id (so callers can
// mutate it in place) and whether it was found.
func (w *Window) folderByID(id string) (*model.Folder, bool) {
	idx := w.folderIndex(id)
	if idx < 0 {
		return nil, false
	}
	return &w.coll.Folders[idx], true
}

// folderIndex returns the index of the folder with id in w.coll.Folders, or -1.
func (w *Window) folderIndex(id string) int {
	for i := range w.coll.Folders {
		if w.coll.Folders[i].ID == id {
			return i
		}
	}
	return -1
}

// requestMatchesFilter reports whether req matches the (already lower-cased,
// trimmed) filter query q, by case-insensitive substring match against any of the
// request's DisplayName(), URL, Method, or Name. q is expected pre-normalised by
// the caller (lower-cased + trimmed). An empty q matches everything — callers gate
// on q != "" so the unfiltered path is untouched, but defining empty as "matches"
// keeps the helper total.
func requestMatchesFilter(req model.Request, q string) bool {
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(req.DisplayName()), q) ||
		strings.Contains(strings.ToLower(req.URL), q) ||
		strings.Contains(strings.ToLower(string(req.Method)), q) ||
		strings.Contains(strings.ToLower(req.Name), q)
}

// sidebarRows computes the grouped display order for the sidebar. For each
// folder in Collection.Folders order it emits a header row, then (unless the
// folder is collapsed) that folder's requests in Requests-order; finally all
// top-level requests (FolderID == "") in Requests-order. Each request row
// carries its flat Collection.Requests index in ReqIdx, so a row maps straight
// back to the request. A collapsed folder contributes only its header.
//
// DISPLAY-ONLY filtering: when w.filterQuery != "", this emits only request rows
// whose request matches the query (see requestMatchesFilter), keeping flat ReqIdx
// values intact. A folder header is emitted only if that folder holds ≥1 matching
// request, and such a folder is shown EXPANDED regardless of its Collapsed flag
// (so the matches are visible); folders with no match are omitted. Filtering never
// touches Collection.Requests, the dirty flag, openTabs, or selectedID — it only
// changes which rows render, so all existing refresh paths just work. When
// filterQuery == "" the output is exactly the unfiltered grouped view (collapse
// respected, all requests shown).
func (w *Window) sidebarRows() []sidebarRow {
	q := w.filterQuery
	filtering := q != ""

	rows := make([]sidebarRow, 0, len(w.coll.Folders)+len(w.coll.Requests))
	for fi := range w.coll.Folders {
		f := w.coll.Folders[fi]

		if filtering {
			// Emit this folder only if it has a matching request, and show it
			// expanded (ignore Collapsed) so the matches are visible.
			folderRows := make([]sidebarRow, 0)
			for i := range w.coll.Requests {
				if w.coll.Requests[i].FolderID == f.ID && requestMatchesFilter(w.coll.Requests[i], q) {
					folderRows = append(folderRows, sidebarRow{FolderID: f.ID, ReqIdx: i})
				}
			}
			if len(folderRows) == 0 {
				continue
			}
			rows = append(rows, sidebarRow{FolderID: f.ID, ReqIdx: -1, IsFolder: true})
			rows = append(rows, folderRows...)
			continue
		}

		rows = append(rows, sidebarRow{FolderID: f.ID, ReqIdx: -1, IsFolder: true})
		if f.Collapsed {
			continue
		}
		for i := range w.coll.Requests {
			if w.coll.Requests[i].FolderID == f.ID {
				rows = append(rows, sidebarRow{FolderID: f.ID, ReqIdx: i})
			}
		}
	}
	for i := range w.coll.Requests {
		if w.coll.Requests[i].FolderID == "" {
			if filtering && !requestMatchesFilter(w.coll.Requests[i], q) {
				continue
			}
			rows = append(rows, sidebarRow{ReqIdx: i})
		}
	}
	return rows
}

// markDirty flags unsaved changes and updates the title bar.
func (w *Window) markDirty() {
	if !w.dirty {
		w.dirty = true
		w.updateTitle()
	}
}

// renameCollection sets the Collection's own Name (stored in the .yon, used by
// the window title and sidebar header in preference to the file name), marks the
// window dirty, and refreshes the title/header. An empty name clears it, so the
// display falls back to the file name (see collectionDisplayName). updateTitle is
// called unconditionally so the header updates even when the window was already
// dirty.
func (w *Window) renameCollection(name string) {
	w.coll.Name = strings.TrimSpace(name)
	w.dirty = true
	w.updateTitle()
}

// showRenameCollection opens a dialog to edit the Collection's Name, prefilled
// with the name currently shown (renamePrefill) so it clearly edits the existing
// collection rather than appearing blank (which felt like creating a new one).
// The confirm button says "Rename", not "Save", to reinforce that.
func (w *Window) showRenameCollection() {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Collection name")
	entry.SetText(w.renamePrefill())
	dialog.ShowForm("Rename Collection", "Rename", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Name", entry)},
		func(ok bool) {
			if ok {
				w.renameCollection(entry.Text)
			}
		}, w.win)
}

// renamePrefill is the value the Rename dialog starts with: the collection's
// current Name, or — when it has none but is backed by a file — the file's base
// name without the .yon extension, so the field shows the visible name instead
// of being blank. An untitled, unnamed collection prefills empty (placeholder).
func (w *Window) renamePrefill() string {
	if w.coll.Name != "" {
		return w.coll.Name
	}
	if w.path != "" {
		base := filepath.Base(w.path)
		return strings.TrimSuffix(base, filepath.Ext(base))
	}
	return ""
}

// collectionDisplayName is the human-readable name shown for a Collection in
// the window title and the sidebar header: the Collection's own Name, else the
// file's base name when it was loaded/saved to disk, else "Untitled".
func collectionDisplayName(name, path string) string {
	switch {
	case name != "":
		return name
	case path != "":
		return filepath.Base(path)
	default:
		return "Untitled"
	}
}

// updateTitle reflects the Collection name, file, and dirty marker in the window
// title and keeps the sidebar header label in sync (e.g. after Save As changes
// the file the empty-name fallback resolves to).
func (w *Window) updateTitle() {
	name := collectionDisplayName(w.coll.Name, w.path)
	if w.sidebarTitle != nil {
		w.sidebarTitle.SetText(name)
	}
	marker := ""
	if w.dirty {
		marker = " •"
	}
	w.win.SetTitle(fmt.Sprintf("Yon — %s%s", name, marker))
}

// onCloseRequested handles the window close button: if dirty, prompt to save.
func (w *Window) onCloseRequested() {
	if !w.dirty {
		w.finishClose()
		return
	}
	dialog.ShowConfirm("Unsaved changes",
		"Save changes to this Collection before closing?",
		func(save bool) {
			if save {
				w.save(func(ok bool) {
					if ok {
						w.finishClose()
					}
					// if save was cancelled/failed, keep the window open.
				})
				return
			}
			w.finishClose()
		}, w.win)
}

// finishClose tears down the window: cancels in-flight sends and drops it from
// the app's live set so it leaves the next session save.
//
// Session persistence is driven from here because closing the last window via
// the red X tears the window down before Fyne's OnStopped hook runs, so by the
// time that hook calls saveSession the live set is already empty (and the
// empty-session guard would skip it). We therefore persist at the right moment:
//   - Closing the LAST window == quitting, so save while it is still in the live
//     set, capturing this collection for next launch.
//   - Closing one of several windows: re-persist the remaining set after this
//     one is forgotten so the closed window drops out (no over-restore).
func (w *Window) finishClose() {
	for _, rt := range w.openTabs {
		rt.cancelInFlight()
	}
	if len(w.app.windows) == 1 {
		// closing the last window == quitting: persist it now (still in a.windows)
		w.app.saveSession()
	}
	w.app.forgetWindow(w)
	if len(w.app.windows) > 0 {
		// closing one of several: re-persist the remaining set so the closed one drops
		w.app.saveSession()
	}
	w.win.Close()
}

// ---- Menu + file actions ----

// saveShortcut / saveAsShortcut are the Save (Cmd/Ctrl+S) and Save As
// (Cmd/Ctrl+Shift+S) accelerators. Unlike Find / Copy / Paste (see buildMainMenu),
// Save is a menu accelerator: nothing in a focused Entry or pop-out window binds
// Cmd+S, so there is no shortcut to hijack — and a menu accelerator (unlike a
// canvas shortcut) still fires while a text field is focused, which is exactly
// when the user reaches for Save.
//
// Known macOS limitation (same app-global behaviour noted on the Edit menu): the
// native menu is bound to the window that last called SetMainMenu, so with
// several collection windows open Cmd+S can route to that window's save() rather
// than the front one's. It is acceptable here because Save/Save As are
// non-destructive and each acts on its own window's file; the front window is
// usually the most recently shown. A focus-time SetMainMenu rebuild would remove
// even this edge.
var (
	saveShortcut   = &desktop.CustomShortcut{KeyName: fyne.KeyS, Modifier: fyne.KeyModifierShortcutDefault}
	saveAsShortcut = &desktop.CustomShortcut{KeyName: fyne.KeyS, Modifier: fyne.KeyModifierShortcutDefault | fyne.KeyModifierShift}
)

func (w *Window) buildMenu() {
	w.win.SetMainMenu(w.buildMainMenu())
}

// buildMainMenu assembles the window's menu bar. Split from buildMenu (which
// installs it) so the menu — and its Save/Save As accelerators — is unit-testable.
func (w *Window) buildMainMenu() *fyne.MainMenu {
	saveItem := fyne.NewMenuItem("Save", func() { w.save(nil) })
	saveItem.Shortcut = saveShortcut
	saveAsItem := fyne.NewMenuItem("Save As…", func() { w.saveAs(nil) })
	saveAsItem.Shortcut = saveAsShortcut
	fileMenu := fyne.NewMenu("File",
		fyne.NewMenuItem("New", func() { w.app.NewCollectionWindow() }),
		fyne.NewMenuItem("Open…", w.open),
		fyne.NewMenuItem("Import Collection (JSON)…", w.importCollection),
		fyne.NewMenuItem("Import OpenAPI / Swagger…", w.importOpenAPI),
		fyne.NewMenuItem("New Request from cURL…", w.newRequestFromCurl),
		w.recentMenuItem(),
		fyne.NewMenuItemSeparator(),
		saveItem,
		saveAsItem,
	)
	// No accelerators on Find / Copy / Paste: on macOS a main-menu accelerator is
	// registered app-globally and bound to the menu's window, so it would hijack
	// Cmd+F / Cmd+C / Cmd+V from a focused pop-out window or Entry and act on the
	// wrong target. Those are handled per-window instead — Cmd+F via each window's
	// canvas/find-field, Cmd+C/V by the focused Entry itself — so these menu items
	// provide discoverable, mouse-driven access. (Save is exempt; see saveShortcut.)
	editMenu := fyne.NewMenu("Edit",
		fyne.NewMenuItem("Copy", w.editCopy),
		fyne.NewMenuItem("Paste", w.editPaste),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Find…", w.openFindActive),
	)
	collMenu := fyne.NewMenu("Collection",
		fyne.NewMenuItem("Rename Collection…", w.showRenameCollection),
		fyne.NewMenuItem("Collection Auth…", w.showCollectionAuth),
		fyne.NewMenuItem("Environments…", w.showEnvironmentManager),
	)
	// View ▸ Variables toggles the right-side Variables dock (the footer
	// "Variables" tap does the same); both call toggleVarsPanel.
	// View ▸ Variables toggles the right-side Variables dock; View ▸ Request Log
	// toggles the bottom Request Log dock (the footer "Variables"/"Log" taps do the
	// same). They call toggleVarsPanel / toggleLogPanel respectively.
	viewMenu := fyne.NewMenu("View",
		fyne.NewMenuItem("Variables", w.toggleVarsPanel),
		fyne.NewMenuItem("Request Log", w.toggleLogPanel),
		fyne.NewMenuItem("Tunnels…", func() { w.app.openTunnelsWindow(w) }),
	)
	// macOS: Fyne moves items labelled exactly "About" and "Settings…" into the
	// system application menu (the one named after the app) — and "About" replaces
	// the default About entry — so they appear where macOS users expect, with no
	// duplicate. "Check for Updates…" is not a recognised special label, so Fyne
	// leaves it here in the Help menu. On Windows/Linux there is no application
	// menu, so all three items simply show under Help.
	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("About", func() { w.app.showAboutDialog(w.win) }),
		fyne.NewMenuItem("Settings…", func() { w.app.showSettingsDialog(w.win) }),
		fyne.NewMenuItem("Check for Updates…", func() { w.checkForUpdates(true) }),
	)
	return fyne.NewMainMenu(fileMenu, editMenu, collMenu, viewMenu, helpMenu)
}

// openFindActive opens find on the active request's response (Edit ▸ Find…, Cmd/Ctrl+F).
func (w *Window) openFindActive() {
	if rt := w.activeTab(); rt != nil {
		rt.response.openFind()
	}
}

// editCopy / editPaste forward the clipboard action to the focused widget so the
// Edit menu items act on whatever entry (URL, params, body, find…) has focus.
func (w *Window) editCopy() {
	w.dispatchShortcut(&fyne.ShortcutCopy{Clipboard: fyne.CurrentApp().Clipboard()})
}

func (w *Window) editPaste() {
	w.dispatchShortcut(&fyne.ShortcutPaste{Clipboard: fyne.CurrentApp().Clipboard()})
}

func (w *Window) dispatchShortcut(s fyne.Shortcut) {
	if f, ok := w.win.Canvas().Focused().(fyne.Shortcutable); ok {
		f.TypedShortcut(s)
	}
}

// open shows a native (OS) file picker filtered to *.yon and loads the chosen
// Collection into a new window. Falls back to Fyne's in-app dialog when the
// native backend is unavailable (e.g. Linux without zenity).
func (w *Window) open() {
	go func() {
		path, ok, err := nativeOpenYon("Open Collection")
		fyne.Do(func() {
			switch {
			case err != nil:
				w.openFyne() // native unavailable → in-app dialog
			case !ok:
				// cancelled — nothing to do
			default:
				if e := w.app.OpenPath(path); e != nil {
					dialog.ShowError(e, w.win)
					return
				}
				w.buildMenu() // refresh Open Recent
			}
		})
	}()
}

// openFyne is the Fyne in-app fallback for open().
func (w *Window) openFyne() {
	d := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil || rc == nil {
			return
		}
		path := rc.URI().Path()
		_ = rc.Close()
		if err := w.app.OpenPath(path); err != nil {
			dialog.ShowError(err, w.win)
			return
		}
		w.buildMenu()
	}, w.win)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".yon"}))
	d.Show()
}

// recentMenuItem builds the "Open Recent" submenu from the app's recent-files
// list. Each entry opens that Collection; a missing file is reported and pruned.
func (w *Window) recentMenuItem() *fyne.MenuItem {
	item := fyne.NewMenuItem("Open Recent", nil)
	recents := w.app.recentFiles()
	if len(recents) == 0 {
		none := fyne.NewMenuItem("(none)", nil)
		none.Disabled = true
		item.ChildMenu = fyne.NewMenu("", none)
		return item
	}
	items := make([]*fyne.MenuItem, 0, len(recents)+2)
	for _, p := range recents {
		p := p
		mi := fyne.NewMenuItem(filepath.Base(p), func() { w.openRecent(p) })
		items = append(items, mi)
	}
	items = append(items,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Clear Menu", func() {
			w.app.clearRecent()
			w.buildMenu()
		}),
	)
	item.ChildMenu = fyne.NewMenu("", items...)
	return item
}

// openRecent opens a remembered path; if it can no longer be loaded (moved or
// deleted) it reports the error and removes it from the recent list.
func (w *Window) openRecent(path string) {
	if err := w.app.OpenPath(path); err != nil {
		dialog.ShowError(err, w.win)
		w.app.removeRecent(path)
	}
	w.buildMenu()
}

// save writes the Collection to its existing path, or falls back to Save As when
// untitled. done (may be nil) is called with whether the save succeeded.
func (w *Window) save(done func(bool)) {
	if w.path == "" {
		w.saveAs(done)
		return
	}
	// The file changed on disk since Yon read/wrote it: a plain Save would
	// silently clobber that external edit (the data-loss bug). Prompt instead.
	if w.fileChangedOnDisk() {
		w.confirmOverwriteChanged(done)
		return
	}
	w.writeToCurrentPath(done)
}

// writeToCurrentPath saves the in-memory Collection to w.path, refreshes the
// loaded stamp, and does the usual dirty/title bookkeeping. Callers must have
// already confirmed it is safe to overwrite (path set, no unresolved external
// change). done (may be nil) reports success.
func (w *Window) writeToCurrentPath(done func(bool)) {
	if err := store.Save(w.path, w.coll); err != nil {
		dialog.ShowError(err, w.win)
		if done != nil {
			done(false)
		}
		return
	}
	w.loadedStamp, _ = store.StatStamp(w.path)
	w.dirty = false
	w.clearTabsDirty()
	w.updateTitle()
	if done != nil {
		done(true)
	}
}

// confirmOverwriteChanged warns that w.path changed on disk since it was opened
// and offers three choices: Overwrite (write the in-memory copy, losing the
// external edit), Reload (discard the in-memory copy and reopen the disk
// version in a fresh window, closing this one), or Cancel (do nothing). It must
// run on the UI thread. done (may be nil) reports whether a save happened.
func (w *Window) confirmOverwriteChanged(done func(bool)) {
	msg := widget.NewLabel(
		"This collection's file changed on disk since it was opened.\n" +
			"Saving now would overwrite those external changes.\n\n" +
			"Choose what to do:",
	)
	msg.Wrapping = fyne.TextWrapWord

	var d dialog.Dialog
	overwrite := widget.NewButtonWithIcon("Overwrite", theme.WarningIcon(), func() {
		d.Hide()
		w.writeToCurrentPath(done)
	})
	overwrite.Importance = widget.DangerImportance
	reload := widget.NewButtonWithIcon("Reload from disk", theme.ViewRefreshIcon(), func() {
		d.Hide()
		// Discard the in-memory copy: reopen the disk version in a fresh window,
		// then close this one. Nothing is saved.
		if err := w.app.OpenPath(w.path); err != nil {
			dialog.ShowError(err, w.win)
			if done != nil {
				done(false)
			}
			return
		}
		w.win.Close()
		if done != nil {
			done(false)
		}
	})
	cancel := widget.NewButton("Cancel", func() {
		d.Hide()
		if done != nil {
			done(false)
		}
	})

	buttons := container.NewHBox(layout.NewSpacer(), cancel, reload, overwrite)
	content := container.NewVBox(msg, buttons)
	d = dialog.NewCustomWithoutButtons("File changed on disk", content, w.win)
	d.Show()
}

// saveAs prompts for a path (native dialog, Fyne fallback) and saves there,
// adopting that path as the Collection's file.
func (w *Window) saveAs(done func(bool)) {
	go func() {
		path, ok, err := nativeSaveYon("Save Collection As", "collection.yon")
		fyne.Do(func() {
			switch {
			case err != nil:
				w.saveAsFyne(done) // native unavailable → in-app dialog
			case !ok:
				if done != nil {
					done(false)
				}
			default:
				w.saveToPath(store.EnsureExt(path), done)
			}
		})
	}()
}

// saveAsFyne is the Fyne in-app fallback for saveAs().
func (w *Window) saveAsFyne(done func(bool)) {
	d := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil || wc == nil {
			if done != nil {
				done(false)
			}
			return
		}
		path := store.EnsureExt(wc.URI().Path())
		_ = wc.Close() // store.Save reopens by path; avoid double-writer.
		w.saveToPath(path, done)
	}, w.win)
	d.SetFileName("collection.yon")
	d.SetFilter(storage.NewExtensionFileFilter([]string{".yon"}))
	d.Show()
}

// saveToPath writes the Collection to path and adopts it: clears dirty, updates
// the title, remembers the file as recent, and rebuilds the menu.
func (w *Window) saveToPath(path string, done func(bool)) {
	if err := store.Save(path, w.coll); err != nil {
		dialog.ShowError(err, w.win)
		if done != nil {
			done(false)
		}
		return
	}
	w.path = path
	w.loadedStamp, _ = store.StatStamp(path)
	w.dirty = false
	w.clearTabsDirty()
	w.updateTitle()
	w.app.rememberRecent(path)
	w.buildMenu()
	if done != nil {
		done(true)
	}
}

// fileChangedOnDisk reports whether the collection's file exists and its
// on-disk stamp differs from what Yon last read/wrote — i.e. an external edit
// Yon would clobber on Save. False for an untitled collection or a missing file
// (nothing to clobber).
func (w *Window) fileChangedOnDisk() bool {
	if w.path == "" {
		return false
	}
	stamp, ok := store.StatStamp(w.path)
	return ok && stamp != w.loadedStamp
}

// ---- Sidebar verb-chip row ----

// methodAbbrev returns the compact UPPERCASE verb tag shown in the sidebar and
// the request-bar pill (DELETE → "DEL" to stay narrow; others as-is).
func methodAbbrev(m model.Method) string {
	switch m {
	case model.MethodDelete:
		return "DEL"
	default:
		s := string(m)
		if s == "" {
			return "GET"
		}
		return s
	}
}

// verbRowAccent is the cyan left-accent bar colour for the selected row.
var verbRowAccent = color.NRGBA{R: 0x18, G: 0xC5, B: 0xE8, A: 0xff}

// verbRow is one sidebar entry: a thin left accent bar (cyan when selected), a
// fixed-width coloured monospace verb tag, and the Request's display name. It is
// a custom widget so widget.List can use it directly as a list item.
type verbRow struct {
	widget.BaseWidget
	accent *canvas.Rectangle
	tag    *canvas.Text
	name   *widget.Label
	object fyne.CanvasObject

	// id is the Collection request index this row currently shows; set() rebinds
	// it per id in UpdateItem. onDelete/onDuplicate (wired by buildSidebar) are
	// invoked with id when the matching context-menu item is chosen. onTap is
	// invoked with id on a primary (left) click — see Tapped for why the row must
	// be Tappable.
	id          int
	onDelete    func(id int)
	onDuplicate func(id int)
	onTap       func(id int)
	// moveMenu builds the "Move to folder…" submenu (folders + "Top level") for the
	// request at id, or nil to omit the item. Returning a *fyne.Menu lets it hang
	// off the context menu as a true ChildMenu submenu.
	moveMenu func(id int) *fyne.Menu

	// --- Drag & drop (reorder a request, incl. across folders) ---
	//
	// onDragEnd is fired once on DragEnd with this row's request index and the LAST
	// absolute pointer Y seen during the drag. The window (handleRequestDrop) maps
	// that Y to a target row and performs the move. nil disables dragging.
	onDragEnd func(srcReqIdx int, dropAbsY float32)
	// dragging is true between the first Dragged and DragEnd; dragStartY/dragLastY
	// record the gesture's first and most-recent absolute pointer Y so DragEnd can
	// (a) hand the drop position to onDragEnd and (b) ignore a negligible move (a
	// jittery click) so it never reorders. We DON'T reorder mid-drag — just track.
	dragging   bool
	dragStartY float32
	dragLastY  float32
}

// dragDeadzone is the minimum absolute vertical pointer travel (in px) before a
// drag counts as a real reorder gesture. Below it, DragEnd is treated as a stray
// click-jitter and does nothing — so a plain tap (which never fires Dragged) and
// a tiny accidental wobble both still just select/open the row.
const dragDeadzone float32 = 5

// newVerbRow builds an empty row; set() fills it per Request in UpdateItem.
func newVerbRow() *verbRow {
	r := &verbRow{}

	r.accent = canvas.NewRectangle(color.Transparent)
	r.accent.SetMinSize(fyne.NewSize(3, 1))

	r.tag = canvas.NewText("", methodColorSlate)
	r.tag.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	r.tag.TextSize = theme.CaptionTextSize()
	// Fixed-ish width so names line up regardless of verb length (GET/POST/DEL…).
	tagBox := container.NewGridWrap(fyne.NewSize(40, theme.IconInlineSize()), r.tag)

	r.name = widget.NewLabel("")
	r.name.Truncation = fyne.TextTruncateEllipsis

	// [accent][verb tag][name........] — the accent sits in the Border's Left
	// slot so it stretches to the full row height (an HBox would leave a sliver).
	content := container.NewBorder(nil, nil, tagBox, nil, r.name)
	r.object = container.NewBorder(nil, nil, r.accent, nil, content)
	r.ExtendBaseWidget(r)
	return r
}

// CreateRenderer renders the row's pre-built object.
func (r *verbRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.object)
}

// set re-binds the row to the Request at index id, recolouring the verb tag via
// methodColor and showing the cyan accent bar when this row is the selected one.
// id is remembered so the right-click Delete acts on the row's current Request.
func (r *verbRow) set(id widget.ListItemID, req model.Request, selected bool) {
	r.id = id
	r.tag.Text = methodAbbrev(req.Method)
	r.tag.Color = methodColor(req.Method)
	r.tag.Refresh()

	r.name.SetText(req.DisplayName())

	if selected {
		r.accent.FillColor = verbRowAccent
	} else {
		r.accent.FillColor = color.Transparent
	}
	r.accent.Refresh()
}

// Tapped handles a primary (left) click by invoking onTap with this row's
// request index. verbRow MUST implement fyne.Tappable: it already implements
// SecondaryTappable (for Delete), and the glfw tap router resolves a click to
// the deepest object implementing ANY tappable interface. Without Tapped, a
// left click would resolve to this row — which has no primary handler — and
// shadow the enclosing widget.List's own primary-tap selection, so rows would
// never open. Routing through onTap (wired to sidebar.Select) restores that.
func (r *verbRow) Tapped(*fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap(r.id)
	}
}

// TappedSecondary shows the row's right-click context menu: "Duplicate" then
// "Delete", each calling its wired callback with this row's request index. Items
// appear only when their callback is set; if none is, there's no menu to show.
// Mirrors the tappable-widget pattern used by tabClose/segButton.
func (r *verbRow) TappedSecondary(e *fyne.PointEvent) {
	if r.onDuplicate == nil && r.onDelete == nil && r.moveMenu == nil {
		return
	}
	id := r.id
	var items []*fyne.MenuItem
	if r.onDuplicate != nil {
		items = append(items, fyne.NewMenuItem("Duplicate", func() { r.onDuplicate(id) }))
	}
	if r.moveMenu != nil {
		// "Move to folder…" hangs a submenu of every folder + "Top level" off the
		// context menu; each child calls moveRequestToFolder for this row's index.
		mv := fyne.NewMenuItem("Move to folder…", nil)
		mv.ChildMenu = r.moveMenu(id)
		items = append(items, mv)
	}
	if r.onDelete != nil {
		items = append(items, fyne.NewMenuItem("Delete", func() { r.onDelete(id) }))
	}
	menu := fyne.NewMenu("", items...)
	if c := fyne.CurrentApp().Driver().CanvasForObject(r); c != nil {
		widget.ShowPopUpMenuAtPosition(menu, c, e.AbsolutePosition)
	}
}

// Dragged tracks an in-progress drag of this request row. We do NOT reorder here;
// we only record the absolute pointer Y so DragEnd knows where the gesture ended
// (and the first Y so it can measure total travel). verbRow implementing
// fyne.Draggable is what makes the glfw driver deliver these events at all — and
// the driver only dispatches Dragged once the pointer actually MOVES while
// pressed, so a plain tap never reaches here and keeps opening the row.
func (r *verbRow) Dragged(e *fyne.DragEvent) {
	if !r.dragging {
		r.dragging = true
		r.dragStartY = e.AbsolutePosition.Y
	}
	r.dragLastY = e.AbsolutePosition.Y
}

// DragEnd finishes a drag: if the gesture actually moved (beyond dragDeadzone, to
// reject click-jitter) it hands the row's request index and the last pointer Y to
// onDragEnd, which resolves the drop target and performs the move. Drag state is
// always cleared so the next gesture starts fresh.
func (r *verbRow) DragEnd() {
	dragging := r.dragging
	lastY := r.dragLastY
	moved := absF32(r.dragLastY-r.dragStartY) >= dragDeadzone

	// Reset before invoking the callback (refreshSidebar may rebind this row).
	r.dragging = false
	r.dragStartY = 0
	r.dragLastY = 0

	if dragging && moved && r.onDragEnd != nil {
		r.onDragEnd(r.id, lastY)
	}
}

// absF32 is a tiny float32 abs (math.Abs is float64-only).
func absF32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// clearTabsDirty drops the unsaved marker from every open tab (called on save).
func (w *Window) clearTabsDirty() {
	for idx, rt := range w.openTabs {
		rt.dirty = false
		if idx >= 0 && idx < len(w.coll.Requests) {
			req := w.coll.Requests[idx]
			rt.tab.setRequest(req.Method, req.DisplayName(), false)
		}
	}
}

// ---- Bottom status bar ----

// buildStatusBar is the thin bar docked at the bottom of the window: the active
// tab's last response status / time / size on the left, its method · path on the
// right, plus the app version (mockup-v9 `.statusbar`).
func (w *Window) buildStatusBar() fyne.CanvasObject {
	muted := theme.Color(theme.ColorNamePlaceHolder)
	mk := func(s string) *canvas.Text {
		t := canvas.NewText(s, muted)
		t.TextSize = theme.CaptionTextSize()
		return t
	}
	// Static app version, far left: "v0.11.1" for a release, "dev" otherwise.
	ver := "dev"
	if v := currentVersion(); v != "" {
		ver = "v" + v
	}
	w.sbVersion = mk(ver)
	w.sbStatus = mk("Ready")
	w.sbMeta = mk("")
	w.sbReqInfo = mk("")

	// Footer jump-host indicator: a clickable coloured dot + env name that opens
	// the Tunnels window. canvas.Text isn't tappable, so wrap it in a tiny
	// tappable container. It starts hidden; updateTunnelIndicator shows it only
	// when the active environment has a complete jump host.
	w.sbTunnel = mk("")
	tunnelTap := newTappable(w.sbTunnel, func() { w.app.openTunnelsWindow(w) })

	// Footer "Variables" toggle: a tappable label that shows/hides the right-side
	// Variables dock (also reachable via View ▸ Variables). canvas.Text isn't
	// tappable on its own, so wrap it like the tunnel indicator. syncVarsToggle
	// (called below) styles it from the seeded w.varsVisible pref.
	w.varsToggle = mk("Variables")
	varsTap := newTappable(w.varsToggle, w.toggleVarsPanel)

	// Footer "Log" toggle: a tappable label that shows/hides the bottom Request
	// Log dock (also reachable via View ▸ Request Log); both call toggleLogPanel.
	// Wrapped like the vars toggle since canvas.Text isn't tappable on its own.
	w.logToggle = mk("Log")
	logTap := newTappable(w.logToggle, w.toggleLogPanel)

	// Left: version · status · time · size · jump-host · vars · log. Right: method · path.
	left := container.NewHBox(w.sbVersion, w.sbStatus, w.sbMeta, tunnelTap, varsTap, logTap)
	bar := container.NewBorder(nil, nil, left, w.sbReqInfo)

	w.syncVarsToggle()
	w.syncLogToggle()

	bg := canvas.NewRectangle(theme.Color(theme.ColorNameMenuBackground))
	return container.NewStack(bg, container.NewPadded(bar))
}

// activeTab returns the requestTab whose card is currently selected (nil if
// none is open).
func (w *Window) activeTab() *requestTab {
	sel := w.tabs.Selected()
	if sel == nil {
		return nil
	}
	for _, rt := range w.openTabs {
		if rt.tab == sel {
			return rt
		}
	}
	return nil
}

// updateStatusBar refreshes the bottom bar from the active tab's request +
// last response. Safe to call before the bar is built (no-op).
func (w *Window) updateStatusBar() {
	if w.sbStatus == nil {
		return
	}
	muted := theme.Color(theme.ColorNamePlaceHolder)
	rt := w.activeTab()
	if rt == nil || rt.idx < 0 || rt.idx >= len(w.coll.Requests) {
		w.sbStatus.Text, w.sbStatus.Color = "Ready", muted
		w.sbMeta.Text = ""
		w.sbReqInfo.Text = ""
	} else {
		req := w.coll.Requests[rt.idx]
		// Resolve {{variables}} so the status bar shows the real path (the
		// environment/collection value) rather than the literal {{key}} template.
		resolvedURL := w.varScope().Resolve(req.URL)
		w.sbReqInfo.Text = fmt.Sprintf("%s · %s", req.Method, urlPathOf(resolvedURL))
		if r := rt.lastResp; r != nil {
			w.sbStatus.Text = fmt.Sprintf("● %d %s", r.Status, r.StatusText)
			w.sbStatus.Color = statusColor(r.Status)
			w.sbMeta.Text = fmt.Sprintf("   %s · %s", formatDuration(r.Duration), formatSize(r.Size))
		} else {
			w.sbStatus.Text, w.sbStatus.Color = "Ready", muted
			w.sbMeta.Text = ""
		}
	}
	w.sbStatus.Refresh()
	w.sbMeta.Refresh()
	w.sbReqInfo.Refresh()
	w.updateTunnelIndicator()
}

// flashStatus shows a brief transient message in the footer status text (e.g.
// "Copied apiKey" after a Variables-row double-click). It writes straight into
// sbStatus and is intentionally NOT persisted anywhere: the next updateStatusBar
// — driven by the active tab's request/response on the very next change — fully
// rewrites sbStatus.Text/Color, so the flash is self-clearing and can never leave
// the bar stuck. Safe before the bar is built (no-op).
func (w *Window) flashStatus(msg string) {
	if w.sbStatus == nil {
		return
	}
	w.sbStatus.Text = msg
	w.sbStatus.Color = theme.Color(theme.ColorNamePrimary)
	w.sbStatus.Refresh()
}

// updateTunnelIndicator refreshes the footer jump-host dot. It shows a coloured
// dot + the active environment's name when activeJumpHost reports a complete
// jump host (green if that host's Tunnel is Connected, grey otherwise), and
// hides the text when there is none. Safe to call before the bar is built.
func (w *Window) updateTunnelIndicator() {
	if w.sbTunnel == nil {
		return
	}
	jh, ok := w.app.activeJumpHost(w)

	// State for the dot colour: the matching Tunnel's state if one exists, else
	// Disconnected.
	state := tunnel.Disconnected
	if ok && w.app.tunnels != nil {
		want := jh.User + "@" + net.JoinHostPort(jh.Host, strconv.Itoa(jumpHostPort(jh.Port)))
		for _, s := range w.app.tunnels.Status() {
			if s.JumpHost == want {
				state = s.State
				break
			}
		}
	}

	envName := ""
	if env, envOK := w.activeEnv(); envOK {
		envName = env.Name
	}
	text, show := tunnelIndicatorText(envName, ok, state)
	w.sbTunnel.Text = text
	w.sbTunnel.Color = tunnelDotColor(state)
	if show {
		w.sbTunnel.Show()
	} else {
		w.sbTunnel.Hide()
	}
	w.sbTunnel.Refresh()
}

// tunnelIndicatorText builds the footer indicator string and whether to show it.
// Fyne-free for the blind tester: a "<glyph> <env>" line gated on a complete
// jump host (show == hasJumpHost). The glyph encodes the tunnel state so the
// indicator is visibly distinct per state even without colour (the caller also
// sets a matching dot colour). envName falls back to "tunnel" when blank so the
// indicator is never an orphan glyph.
func tunnelIndicatorText(envName string, hasJumpHost bool, state tunnel.State) (text string, show bool) {
	if !hasJumpHost {
		return "", false
	}
	name := envName
	if name == "" {
		name = "tunnel"
	}
	return tunnelStateGlyph(state) + " " + name, true
}

// tunnelStateGlyph maps a tunnel state to a single distinct status glyph for the
// footer indicator: a filled dot when Connected, a half dot while Connecting, an
// X on Error, and a hollow dot when Disconnected.
func tunnelStateGlyph(state tunnel.State) string {
	switch state {
	case tunnel.Connected:
		return "●"
	case tunnel.Connecting:
		return "◍"
	case tunnel.Error:
		return "✕"
	default:
		return "○"
	}
}

// tunnelDotColor maps a tunnel state to the footer dot colour: success-green
// when Connected, error-red on Error, muted grey otherwise.
func tunnelDotColor(state tunnel.State) color.Color {
	switch state {
	case tunnel.Connected:
		return theme.Color(theme.ColorNameSuccess)
	case tunnel.Error:
		return theme.Color(theme.ColorNameError)
	default:
		return theme.Color(theme.ColorNamePlaceHolder)
	}
}

// jumpHostPort returns the jump host's port or the SSH default (22) when unset,
// for matching a model.JumpHost against a Tunnel's "user@host:port" display.
func jumpHostPort(p int) int {
	if p == 0 {
		return 22
	}
	return p
}

// urlPathOf returns the path component of a request URL for the status bar,
// falling back to the raw string when it can't be parsed.
func urlPathOf(raw string) string {
	if raw == "" {
		return ""
	}
	// With a scheme, parse normally and take the path component.
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil && u.Path != "" {
			return u.Path
		}
		return raw
	}
	// Already a bare path.
	if strings.HasPrefix(raw, "/") {
		return raw
	}
	// No scheme (e.g. a {{server}} value without https://): parse as an authority
	// "host[:port]/path" so the status bar shows just the path, not host+path.
	// Falls back to the raw string when it isn't a parseable authority (e.g. an
	// unresolved {{template}}).
	if u, err := url.Parse("//" + raw); err == nil && u.Path != "" {
		return u.Path
	}
	return raw
}
