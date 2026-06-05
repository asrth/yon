package ui

import (
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ultramcu/yon/internal/model"
)

// ---- Log model + pure formatter (Fyne-free, unit-testable) ----

// logEntry is one row in the combined HTTP request log: a single send and its
// outcome. URL is the resolved (templates-expanded) URL actually requested; Name
// is the request's display name (may be empty). On success Status/StatusText/
// Duration/Size describe the Response. A non-empty Err means the send FAILED, in
// which case the status/size fields are not shown — only the error is.
//
// The trailing detail fields are NOT part of the one-line summary
// (formatLogEntry / formatLog ignore them); they hold the full request and
// response captured at send time so the detail window (Dev B's openLogDetail)
// can show exactly what was sent and received for this row:
//   - ReqHeaders/ReqBody: the resolved headers + body actually sent.
//   - RespHeaders/RespBody: the response headers + body. RespBody is already
//     capped by the capture side, so the detail window can show it as-is.
type logEntry struct {
	Time       time.Time
	Name       string
	Method     string
	URL        string
	Status     int
	StatusText string
	Duration   time.Duration
	Size       int64
	Err        string

	// Detail fields (populated by the capture side, read by the detail window).
	ReqHeaders  []model.Param // resolved request headers sent
	ReqBody     string        // resolved request body sent
	RespHeaders []model.Param // response headers
	RespBody    []byte        // response body (already capped by the capture side)
}

// formatLogEntry renders one log line. It is PURE (no Fyne) so the panel and the
// blind tester share one source of truth.
//
// Layout, with the timestamp formatted as 15:04:05:
//
//	success → "<HH:MM:SS>  [<Name>]  <Method> <URL> → <Status> <StatusText> · <dur> · <size>"
//	error   → "<HH:MM:SS>  [<Name>]  <Method> <URL> → ERROR: <Err>"
//
// The "[<Name>]" clause is omitted when Name is empty, and the "· <size>" clause
// is omitted when Size <= 0. Duration/size use the package's formatDuration /
// formatSize so the log matches the response viewer.
func formatLogEntry(e logEntry) string {
	var b strings.Builder
	b.WriteString(e.Time.Format("15:04:05"))
	b.WriteString("  ")
	if e.Name != "" {
		b.WriteString("[")
		b.WriteString(e.Name)
		b.WriteString("]  ")
	}
	b.WriteString(e.Method)
	b.WriteString(" ")
	b.WriteString(e.URL)
	b.WriteString(" → ")

	if e.Err != "" {
		b.WriteString("ERROR: ")
		b.WriteString(e.Err)
		return b.String()
	}

	b.WriteString(strings.TrimSpace(statusClause(e.Status, e.StatusText)))
	b.WriteString(" · ")
	b.WriteString(formatDuration(e.Duration))
	if e.Size > 0 {
		b.WriteString(" · ")
		b.WriteString(formatSize(e.Size))
	}
	return b.String()
}

// statusClause renders "<Status> <StatusText>", dropping the space when there is
// no status text.
func statusClause(status int, text string) string {
	if text == "" {
		return strconv.Itoa(status)
	}
	return strconv.Itoa(status) + " " + text
}

// formatLog joins every entry's formatLogEntry line with "\n" (no trailing
// newline). An empty slice yields "". PURE.
func formatLog(entries []logEntry) string {
	if len(entries) == 0 {
		return ""
	}
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = formatLogEntry(e)
	}
	return strings.Join(lines, "\n")
}

// ---- Panel UI (read-only) ----

// logPanel is the read-only combined request log: a list view with one row per
// send this session (newest last), plus Copy and Clear actions. It owns only
// display state; refresh() re-reads the Window's reqLog, refreshes the list, and
// scrolls to the newest row. Double-clicking a row opens its detail window via
// the Window's openLogDetail.
type logPanel struct {
	win       *Window
	container fyne.CanvasObject

	list *widget.List // one row per win.reqLog entry, formatLogEntry per row
}

// newLogPanel builds the panel UI and stores it in .container. The list shows one
// formatLogEntry line per send (newest last); a row's double-tap opens the detail
// window for THAT send. A bottom bar carries Copy (whole log) and Clear.
func newLogPanel(w *Window) *logPanel {
	p := &logPanel{win: w}

	header := widget.NewLabelWithStyle("Request log", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	// The List renders win.reqLog newest-last (same order as the slice). CreateItem
	// builds a reusable double-tappable row; UpdateItem morphs it to the current row
	// id — crucially writing the CURRENT index onto the widget (it.idx) so a later
	// double-tap opens the row under the pointer, never a stale captured index.
	p.list = widget.NewList(
		func() int { return len(p.win.reqLog) },
		func() fyne.CanvasObject {
			return newLogRowWidget(func(i int) { p.openDetail(i) })
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id < 0 || id >= len(p.win.reqLog) {
				return
			}
			row := o.(*logRowWidget)
			row.set(id, formatLogEntry(p.win.reqLog[id]))
		},
	)

	copyBtn := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		if a := fyne.CurrentApp(); a != nil {
			a.Clipboard().SetContent(formatLog(p.win.reqLog))
		}
	})
	copyBtn.Importance = widget.HighImportance

	clearBtn := widget.NewButtonWithIcon("Clear", theme.DeleteIcon(), func() {
		p.win.clearLog()
		p.refresh()
	})

	bottomBar := container.NewBorder(nil, nil, copyBtn, clearBtn)

	p.container = container.NewBorder(header, bottomBar, nil, nil, p.list)

	p.refresh()
	return p
}

// openDetail opens the detail window for the row at index i, guarding the index
// against the live reqLog so a refresh that shrank the log can't open a stale
// row. The index comes from the row widget's current id (set in UpdateItem), so
// it always names the row the user double-tapped.
func (p *logPanel) openDetail(i int) {
	if i < 0 || i >= len(p.win.reqLog) {
		return
	}
	p.win.openLogDetail(p.win.reqLog[i])
}

// refresh re-reads the Window's reqLog, refreshes the list, and scrolls to the
// newest (bottom) row when there are any. Safe to call anytime, including before
// the Window has any logged sends (empty log → empty list).
func (p *logPanel) refresh() {
	if p.list == nil {
		return // not built yet
	}
	p.list.Refresh()
	if len(p.win.reqLog) > 0 {
		p.list.ScrollToBottom()
	}
}

// ---- Row widget (read-only, double-tappable) ----

// logRowWidget is one read-only request-log row: a single muted-mono-ish text
// line (formatLogEntry of its entry) that is double-tappable (fyne.DoubleTappable)
// to open that send's detail window. It carries its CURRENT row index in idx,
// rewritten on every UpdateItem (see set), so DoubleTapped always reports the row
// under the pointer rather than a stale index captured at build time — the classic
// widget.List item-reuse hazard.
type logRowWidget struct {
	widget.BaseWidget
	label  *widget.Label
	idx    int
	onOpen func(int)
}

// newLogRowWidget builds a reusable log row; a double-tap calls onOpen with the
// row's CURRENT index (kept in sync by set during UpdateItem).
func newLogRowWidget(onOpen func(int)) *logRowWidget {
	r := &logRowWidget{label: widget.NewLabel(""), onOpen: onOpen, idx: -1}
	r.label.Truncation = fyne.TextTruncateEllipsis
	r.ExtendBaseWidget(r)
	return r
}

// set re-targets this reused row to id with the given pre-formatted text. Writing
// id onto the widget here is what keeps the double-tap honest: the widget that the
// pointer is over carries the row id it is currently rendering.
func (r *logRowWidget) set(id int, text string) {
	r.idx = id
	r.label.SetText(text)
}

// CreateRenderer renders the row as its single text label.
func (r *logRowWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.label)
}

// DoubleTapped opens the detail window for this row's CURRENT entry
// (fyne.DoubleTappable). It hands the row's live idx to onOpen, which validates
// it against the log before opening, so a reused/stale row can never open the
// wrong (or an out-of-range) entry.
func (r *logRowWidget) DoubleTapped(*fyne.PointEvent) {
	if r.onOpen != nil {
		r.onOpen(r.idx)
	}
}
