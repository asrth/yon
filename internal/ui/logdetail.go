package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ultramcu/yon/internal/model"
)

// ---- Pure, Fyne-free text renderers (shared with the blind tester) ----

// logDetailRequestText renders the REQUEST side of a log entry as plain text:
// the "<Method> <URL>" line, each captured header as "Key: Value", and the
// resolved body pretty-printed when it looks like JSON (else shown as-is). The
// window builds its read-only request view from this string, so the formatter
// is the single source of truth. PURE (no Fyne).
func logDetailRequestText(e logEntry) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(e.Method + " " + e.URL))

	if len(e.ReqHeaders) > 0 {
		b.WriteString("\n\nHeaders:\n")
		b.WriteString(formatHeaderLines(e.ReqHeaders))
	}

	if e.ReqBody != "" {
		b.WriteString("\n\nBody:\n")
		b.WriteString(prettyBody([]byte(e.ReqBody)))
	}
	return b.String()
}

// logDetailResponseText renders the RESPONSE side of a log entry as plain text.
// On a failed send (Err set) it shows "ERROR: <Err>" and nothing else. On
// success it shows the status line ("<Status> <StatusText> · <dur> · <size>"),
// the response headers as "Key: Value" lines, and the body pretty-printed when
// it looks like JSON (else raw). When the captured body was truncated to the
// log cap, a "(truncated, showing first N of M)" note precedes the body. PURE.
func logDetailResponseText(e logEntry) string {
	if e.Err != "" {
		return "ERROR: " + e.Err
	}

	var b strings.Builder
	b.WriteString(strings.TrimSpace(statusClause(e.Status, e.StatusText)))
	b.WriteString(" · ")
	b.WriteString(formatDuration(e.Duration))
	if e.Size > 0 {
		b.WriteString(" · ")
		b.WriteString(formatSize(e.Size))
	}

	if len(e.RespHeaders) > 0 {
		b.WriteString("\n\nHeaders:\n")
		b.WriteString(formatHeaderLines(e.RespHeaders))
	}

	if len(e.RespBody) > 0 {
		b.WriteString("\n\nBody:\n")
		// Truncation note. Two signals, either of which means the shown body is not
		// the whole thing: (1) the true Size exceeds the stored bytes — the capture
		// side (capLogBody) clipped it; (2) the stored body itself is at/over the
		// cap. We use min(true size, stored len) as "shown" and the larger of the
		// two as the original total.
		shown := int64(len(e.RespBody))
		total := shown
		if e.Size > total {
			total = e.Size
		}
		if total > shown || shown >= maxLogBodyBytes {
			b.WriteString("(truncated, showing first ")
			b.WriteString(formatSize(shown))
			b.WriteString(" of ")
			b.WriteString(formatSize(total))
			b.WriteString(")\n")
		}
		b.WriteString(prettyBody(e.RespBody))
	}
	return b.String()
}

// formatHeaderLines joins headers as "Key: Value" lines (one per row, no
// trailing newline), in their captured order. PURE.
func formatHeaderLines(headers []model.Param) string {
	lines := make([]string, len(headers))
	for i, h := range headers {
		lines[i] = h.Key + ": " + h.Value
	}
	return strings.Join(lines, "\n")
}

// prettyBody pretty-prints a body for the detail view: JSON via the shared
// prettyJSON (reused, not reinvented); else the raw bytes as a string. PURE.
func prettyBody(body []byte) string {
	if out, ok := prettyJSON(body); ok {
		return string(out)
	}
	return string(body)
}

// ---- Detail window ----

// openLogDetail opens a secondary window showing the full request and response
// captured for one log entry: method/URL + sent headers + sent body on top, and
// status/headers/received body (or the error) below, in a resizable VSplit. Both
// bodies live in read-only, selectable multiline views so the user can copy; a
// Copy button copies the response body. Matches the app's secondary-window idiom
// (a.fyneApp.NewWindow + Resize + Show).
func (w *Window) openLogDetail(e logEntry) {
	title := strings.TrimSpace("Request — " + e.Method + " " + e.URL)
	win := w.app.fyneApp.NewWindow(title)
	win.SetIcon(appIcon)

	reqView := readOnlyMultiline(logDetailRequestText(e))
	respView := readOnlyMultiline(logDetailResponseText(e))

	reqHeader := widget.NewLabelWithStyle("Request", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	respHeader := widget.NewLabelWithStyle("Response", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	// Copy the response body (raw captured bytes) to the clipboard.
	copyBtn := widget.NewButtonWithIcon("Copy response", theme.ContentCopyIcon(), func() {
		if a := fyne.CurrentApp(); a != nil {
			a.Clipboard().SetContent(logDetailResponseText(e))
		}
	})
	copyBtn.Importance = widget.HighImportance

	reqPane := container.NewBorder(reqHeader, nil, nil, nil, container.NewVScroll(reqView))
	respPane := container.NewBorder(
		respHeader,
		container.NewBorder(nil, nil, copyBtn, nil),
		nil, nil,
		container.NewVScroll(respView),
	)

	split := container.NewVSplit(reqPane, respPane)
	split.SetOffset(0.45)

	win.SetContent(split)
	win.Resize(fyne.NewSize(720, 560))
	win.Show()
}

// readOnlyMultiline returns a selectable multiline Entry seeded with text and
// made effectively read-only (any keystroke restores the seeded text, so the
// view stays copy-only). Wrapping is off so long header/body lines can be
// drag-selected without reflow.
func readOnlyMultiline(text string) *widget.Entry {
	e := widget.NewMultiLineEntry()
	e.Wrapping = fyne.TextWrapOff
	e.SetText(text)
	// Keep it copy-only: revert any edit back to the captured text. SetText does
	// not re-fire OnChanged in a way that loops here (the value already matches).
	e.OnChanged = func(s string) {
		if s != text {
			e.SetText(text)
		}
	}
	return e
}
