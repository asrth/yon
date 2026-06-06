package ui

import (
	"fmt"
	"image/color"
	"os"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ultramcu/yon/internal/model"
	"github.com/ultramcu/yon/internal/postresp"
	"github.com/ultramcu/yon/internal/updater"
)

// maxDisplayBytes caps how much of a response body is rendered on screen. Larger
// bodies show only the head plus a notice and a "Save to file" button; the full
// body is retained in memory (the read-only-response rule).
const maxDisplayBytes = 256 * 1024

// largeBodyThreshold is the display-text size at or above which the coloured
// per-cell TextGrid is too expensive to build (its parseRows allocates one cell
// per rune — ~1.44M allocations for a 256 KB body — and the JSON colouring adds a
// SetStyleRange per token). For bodies this large the Pretty view is rendered
// into a lighter, virtualized read-only monospace widget.List instead (one
// Label per visible line; off-screen lines are never laid out). Smaller bodies
// keep the coloured TextGrid path. The split is "small body = coloured TextGrid,
// large body = lightweight read-only line list" and stays read-only per the read-only-response rule.
const largeBodyThreshold = 64 * 1024

// maxPopoutBytes caps the body shown in the pop-out window. Its default view is a
// selectable Entry, which re-layouts slowly past ~128 KB (~0.4 s); larger bodies
// show the head plus a notice, and "Save Output As…" still writes the full body.
const maxPopoutBytes = 128 * 1024

// Status-class colours for the status label.
var (
	colorStatus2xx = color.NRGBA{R: 0x2e, G: 0x7d, B: 0x32, A: 0xff} // green
	colorStatus3xx = color.NRGBA{R: 0x15, G: 0x65, B: 0xc0, A: 0xff} // blue
	colorStatus4xx = color.NRGBA{R: 0xef, G: 0x6c, B: 0x00, A: 0xff} // orange
	colorStatus5xx = color.NRGBA{R: 0xc6, G: 0x28, B: 0x28, A: 0xff} // red
	colorStatusErr = color.NRGBA{R: 0xc6, G: 0x28, B: 0x28, A: 0xff} // red
)

// colorRespHeaderKey is the cyan used for response-header keys in the left
// column, matching mockup-v2's `.rh .k` (--cyan #18C5E8).
var (
	colorRespHeaderKey = color.NRGBA{R: 0x18, G: 0xc5, B: 0xe8, A: 0xff}
	styleRespHeaderKey = &widget.CustomTextGridStyle{FGColor: colorRespHeaderKey}
)

// Test-result colours: green for a passed assertion, red for a failed one.
var (
	colorAssertPass = color.NRGBA{R: 0x2e, G: 0x7d, B: 0x32, A: 0xff} // green
	colorAssertFail = color.NRGBA{R: 0xc6, G: 0x28, B: 0x28, A: 0xff} // red
)

// responseView renders a model.Response read-only: a status/time/size line, a
// response-headers list, and the body in a read-only TextGrid (never an Entry,
// the read-only-response rule) with a Pretty/Raw toggle (Pretty default).
type responseView struct {
	parent fyne.Window

	container *fyne.Container

	statusLabel *canvas.Text
	metaLabel   *widget.Label
	headersGrid *widget.TextGrid
	bodyGrid    *widget.TextGrid
	saveBtn     *widget.Button
	noticeLabel *widget.Label

	// Pretty/Raw is a two-button segmented control (mockup-v2): pretty holds the
	// current mode; prettyBtn/rawBtn show which is active (HighImportance = on).
	pretty    bool
	prettyBtn *widget.Button
	rawBtn    *widget.Button

	// copyBtn copies the response body to the clipboard (shown once there is one).
	copyBtn *widget.Button
	// popoutBtn opens the response body in a separate, resizable window.
	popoutBtn *widget.Button

	// find is the in-pane text-search bar (Cmd/Ctrl+F). While open, the body is
	// shown in the TextGrid and search drives the match highlighting.
	find       *findBar
	search     gridSearch
	findActive bool

	// Status pill: a rounded coloured rectangle with a small status-class dot
	// behind the statusLabel text (mockup-v2 `.status-pill`). The pill background,
	// dot and text colour are all driven by statusColor(code).
	statusPillBG *canvas.Rectangle
	statusDot    *canvas.Circle
	statusPill   *fyne.Container

	// bodyList is the lightweight read-only viewer used for large bodies instead
	// of the per-cell TextGrid (the read-only-response rule: still read-only, monospace, scrollable).
	// It is virtualized — only on-screen lines are realised into a widget.
	bodyList  *widget.List
	bodyLines []string

	// bodyStack holds the body viewers; renderBody shows exactly ONE of:
	// bodyScroll (small/Raw TextGrid), bodyList (large body), bodyImageScroll (an
	// image preview) or bodyPDF (a PDF panel). The kind is chosen by
	// classifyBody(contentType, fullBody) — see renderBody.
	bodyStack  *fyne.Container
	bodyScroll *container.Scroll

	// bodyImage previews an image response (issue #16). It is built per-response
	// from rv.fullBody (canvas.NewImageFromResource over a static resource),
	// scaled to fit (ImageFillContain) inside bodyImageScroll so a large image
	// scrolls rather than forcing the pane open. The scroll is added to bodyStack
	// only while the image is the active viewer and removed by hideImage, so a
	// later text/PDF response leaves no *canvas.Image in the tree. Both fields are
	// nil when no image is shown. For an image the Pretty/Raw toggle means: Pretty
	// = the image preview, Raw = the raw-bytes text viewer (bytes stay inspectable).
	bodyImage       *canvas.Image
	bodyImageScroll *container.Scroll

	// bodyPDF previews a PDF response: an icon, a "PDF document · <size>" label
	// and Save…/Open buttons. yon does not render PDF pages itself (no new deps);
	// the panel hands the bytes to the OS via Open (updater.OpenFile to a temp
	// file) or Save Output As…. Like the image preview it is inserted into
	// bodyStack only while active and removed by hidePDF; nil when no PDF is shown.
	bodyPDF      *fyne.Container
	bodyPDFLabel *widget.Label

	// respTabs is the Body/Headers/Tests segmented sub-tab control (same component
	// as the request editor). Body is the default tab and gives bodyStack the full
	// pane; Headers holds the headersGrid. headersSeg is its button, for the count
	// badge.
	respTabs   *segTabs
	headersSeg *segButton

	// testsSeg is the Tests sub-tab's button (for its pass-count badge), and
	// testsBox is the VBox that setTestResults fills with assertion ✓/✗ rows, a
	// captured-values section, and a summary line.
	testsSeg *segButton
	testsBox *fyne.Container

	// fullBody is the complete response body, retained even when the display is
	// truncated, so "Save to file" always writes everything.
	fullBody []byte

	// contentType is the response's Content-Type media type (lower-cased, params
	// stripped), derived from the response headers in setResponse. It selects the
	// Pretty-mode formatter and syntax highlighter: JSON, XML, HTML, or plain.
	contentType string
}

// newResponseView builds an empty response pane bound to parent (used for the
// Save-to-file dialog).
func newResponseView(parent fyne.Window) *responseView {
	rv := &responseView{parent: parent}

	rv.statusLabel = canvas.NewText("No response yet", color.Gray{Y: 0x88})
	rv.statusLabel.TextStyle = fyne.TextStyle{Bold: true}
	rv.statusLabel.TextSize = theme.TextSize() - 1

	// Status pill (mockup-v2 `.status-pill`): rounded coloured rectangle + a small
	// status-class dot in front of the text. The pill colours track statusColor().
	rv.statusPillBG = canvas.NewRectangle(color.NRGBA{})
	rv.statusPillBG.CornerRadius = 7
	rv.statusDot = canvas.NewCircle(color.Gray{Y: 0x88})

	rv.metaLabel = widget.NewLabel("")

	// Pretty/Raw segmented control + Copy. pretty defaults true; the active
	// segment is HighImportance. The handlers call setPretty (which renders) and
	// only fire on user click — after the body viewers exist — so there is no
	// construction-time render.
	rv.pretty = true
	rv.prettyBtn = widget.NewButton("Pretty", func() { rv.setPretty(true) })
	rv.rawBtn = widget.NewButton("Raw", func() { rv.setPretty(false) })
	rv.prettyBtn.Importance = widget.HighImportance
	rv.rawBtn.Importance = widget.MediumImportance

	// Icon-only Copy / Save / Pop-out (compact so the bar's right cluster never
	// clips). All three are shown only once there is a response.
	rv.copyBtn = widget.NewButtonWithIcon("", theme.ContentCopyIcon(), rv.copyBody)
	rv.copyBtn.Importance = widget.LowImportance
	rv.copyBtn.Hide()

	// Save Output As… (native dialog) writes the full body to a chosen file.
	rv.saveBtn = widget.NewButtonWithIcon("", theme.DocumentSaveIcon(), rv.saveToFile)
	rv.saveBtn.Importance = widget.LowImportance
	rv.saveBtn.Hide()

	rv.popoutBtn = widget.NewButtonWithIcon("", theme.ViewFullScreenIcon(), rv.showPopout)
	rv.popoutBtn.Importance = widget.LowImportance
	rv.popoutBtn.Hide()

	rv.noticeLabel = widget.NewLabel("")
	rv.noticeLabel.Hide()

	rv.headersGrid = widget.NewTextGrid()
	rv.bodyGrid = widget.NewTextGrid()

	// Lightweight read-only viewer for large bodies. Each row is a non-editable,
	// non-wrapping monospace Label; widget.List only realises the visible rows, so
	// rendering cost is independent of body size (no per-rune cell allocation).
	rv.bodyList = widget.NewList(
		func() int { return len(rv.bodyLines) },
		func() fyne.CanvasObject {
			lbl := widget.NewLabel("")
			lbl.TextStyle = fyne.TextStyle{Monospace: true}
			lbl.Wrapping = fyne.TextWrapOff
			return lbl
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= 0 && id < len(rv.bodyLines) {
				obj.(*widget.Label).SetText(rv.bodyLines[id])
			}
		},
	)
	rv.bodyList.Hide()

	// Status pill: dot + text over a rounded coloured background. A thin spacer
	// keeps the pill snug around its content (mockup-v2 `.status-pill` padding).
	dotBox := container.New(layout.NewCenterLayout(), rv.statusDot)
	pillContent := container.New(
		layout.NewHBoxLayout(),
		dotBox, rv.statusLabel,
	)
	rv.statusPill = container.NewStack(
		rv.statusPillBG,
		container.NewPadded(pillContent),
	)

	// Response header row: section label + status pill + meta on the LEFT, the
	// Pretty/Raw toggle (and Save) pinned to the RIGHT (mockup-v2 `.resp-bar`).
	respTitle := canvas.NewText("RESPONSE", theme.Color(theme.ColorNamePlaceHolder))
	respTitle.TextStyle = fyne.TextStyle{Bold: true}
	respTitle.TextSize = theme.TextSize() - 3

	left := container.New(layout.NewHBoxLayout(),
		respTitle, rv.statusPill, rv.metaLabel)
	// Save (when truncated) · Copy · Pretty | Raw — a single flat row pinned right
	// (adjacent Pretty/Raw buttons read as a segmented control).
	right := container.New(layout.NewHBoxLayout(),
		rv.popoutBtn, rv.saveBtn, rv.copyBtn, rv.prettyBtn, rv.rawBtn)
	headerRow := container.NewBorder(nil, nil, left, right)

	rv.find = newFindBar(
		func(q string) { c, t := rv.search.search(q); rv.find.setCount(c, t) },
		func() { c, t := rv.search.move(1); rv.find.setCount(c, t) },
		func() { c, t := rv.search.move(-1); rv.find.setCount(c, t) },
		rv.closeFind,
	)

	header := container.NewVBox(headerRow, rv.noticeLabel, rv.find.container)

	// Body content: the coloured TextGrid (small bodies, scrolled) stacked with
	// the lightweight List (large bodies). renderBody shows exactly one. UNCHANGED
	// perf architecture — only its surrounding layout differs.
	rv.bodyScroll = container.NewScroll(rv.bodyGrid)

	// The image preview and PDF panel (issue #16) are built lazily and inserted
	// into bodyStack only while they are the active viewer, then removed again —
	// so a text or PDF response leaves no *canvas.Image in the tree, and the
	// default bodyStack is exactly {bodyScroll, bodyList} (the perf architecture).
	rv.bodyStack = container.NewStack(rv.bodyScroll, rv.bodyList)

	// Body / Headers sub-tabs (same segTabs component as the request editor) so the
	// body gets the FULL pane. Body is appended first and is therefore the active
	// tab; the response HEADERS (key cyan / value) move into the Headers tab.
	rv.respTabs = newSegTabs()
	rv.respTabs.Append("Body", rv.bodyStack)
	rv.headersSeg = rv.respTabs.Append("Headers", container.NewScroll(rv.headersGrid))
	// Tests tab: a scrollable VBox that setTestResults fills with the assertion
	// pass/fail rows + captured values + a summary line. Starts empty/neutral.
	rv.testsBox = container.NewVBox()
	rv.testsSeg = rv.respTabs.Append("Tests", container.NewScroll(rv.testsBox))

	rv.container = container.NewBorder(header, nil, nil, nil, rv.respTabs.object())

	// Initialise the pill to the neutral "no response yet" tint.
	rv.applyStatusPill(color.Gray{Y: 0x88})
	return rv
}

// applyStatusPill tints the pill background, dot and text from a status-class
// colour. The background is the same hue at low alpha (a tinted chip, like
// mockup-v2's `--green-bg`); the dot and text use the full colour.
func (rv *responseView) applyStatusPill(c color.Color) {
	rv.statusLabel.Color = c
	rv.statusDot.FillColor = c
	r, g, b, _ := c.RGBA()
	rv.statusPillBG.FillColor = color.NRGBA{
		R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0x26,
	}
	rv.statusPillBG.Refresh()
	rv.statusDot.Refresh()
	rv.statusLabel.Refresh()
}

// setPretty switches the Pretty/Raw segmented control (active = HighImportance)
// and re-renders the body. Called from the two segment buttons.
func (rv *responseView) setPretty(p bool) {
	rv.pretty = p
	if p {
		rv.prettyBtn.Importance = widget.HighImportance
		rv.rawBtn.Importance = widget.MediumImportance
	} else {
		rv.prettyBtn.Importance = widget.MediumImportance
		rv.rawBtn.Importance = widget.HighImportance
	}
	rv.prettyBtn.Refresh()
	rv.rawBtn.Refresh()
	rv.renderBody()
}

// copyBody copies the full response body to the clipboard — pretty-printed when
// in Pretty mode (JSON indented, XML/XHTML reformatted), otherwise the raw bytes.
// No-op when there is no body.
func (rv *responseView) copyBody() {
	if len(rv.fullBody) == 0 {
		return
	}
	data := string(rv.fullBody)
	if rv.pretty {
		data, _ = rv.prettyDisplay(rv.fullBody)
	}
	if app := fyne.CurrentApp(); app != nil {
		app.Clipboard().SetContent(data)
	}
}

// openFind shows the search bar over the response body. While find is open the
// body is rendered (plain) into the TextGrid so matches can be highlighted; the
// normal view (colour / virtualized list) is restored when find closes.
func (rv *responseView) openFind() {
	if rv.fullBody == nil {
		return
	}
	rv.findActive = true
	// Find highlights matches in the body grid, so surface the Body tab.
	if rv.respTabs != nil {
		rv.respTabs.Select(0)
	}
	text := rv.findDisplayText()
	rv.bodyList.Hide()
	rv.bodyGrid.SetText(text)
	rv.bodyScroll.Show()
	rv.search.bind(rv.bodyGrid, rv.bodyScroll, text, func() { rv.bodyGrid.SetText(text) })

	rv.find.container.Show()
	rv.find.container.Refresh()
	rv.parent.Canvas().Focus(rv.find.query)
	c, t := rv.search.search(rv.find.query.Text)
	rv.find.setCount(c, t)
}

// closeFind hides the search bar, clears highlights, and restores the normal
// response view.
func (rv *responseView) closeFind() {
	if !rv.findActive {
		return
	}
	rv.findActive = false
	rv.find.container.Hide()
	rv.search.clear()
	rv.renderBody()
}

// findDisplayText is the (capped, pretty-if-applicable) body text that find
// searches. In Pretty mode it matches what renderBody shows: JSON indented,
// XML/XHTML reformatted, otherwise the raw text.
func (rv *responseView) findDisplayText() string {
	body := rv.fullBody
	if len(body) > maxDisplayBytes {
		body = body[:maxDisplayBytes]
	}
	if rv.pretty {
		display, _ := rv.prettyDisplay(body)
		return display
	}
	return string(body)
}

// setPending shows an in-flight indicator while a send runs.
func (rv *responseView) setPending() {
	rv.statusLabel.Text = "Sending…"
	rv.applyStatusPill(color.Gray{Y: 0x88})
	rv.metaLabel.SetText("")
	rv.noticeLabel.Hide()
	rv.saveBtn.Hide()
	rv.copyBtn.Hide()
	rv.popoutBtn.Hide()
	rv.fullBody = nil
	rv.contentType = ""
	rv.clearBody()
	rv.headersGrid.SetText("")
	rv.setHeadersBadge(0)
	rv.clearTests()
}

// setError shows a transport/timeout/cancel error in place of a response.
func (rv *responseView) setError(err error) {
	rv.statusLabel.Text = "Error"
	rv.applyStatusPill(colorStatusErr)
	rv.metaLabel.SetText(err.Error())
	rv.noticeLabel.Hide()
	rv.saveBtn.Hide()
	rv.copyBtn.Hide()
	rv.popoutBtn.Hide()
	rv.fullBody = nil
	rv.contentType = ""
	rv.clearBody()
	rv.headersGrid.SetText("")
	rv.setHeadersBadge(0)
	rv.clearTests()
}

// setResponse renders a completed Response.
func (rv *responseView) setResponse(resp model.Response) {
	rv.fullBody = resp.Body
	rv.contentType = contentTypeOf(resp.Headers)
	rv.copyBtn.Show()
	rv.saveBtn.Show()
	rv.popoutBtn.Show()

	rv.statusLabel.Text = fmt.Sprintf("%d %s", resp.Status, resp.StatusText)
	rv.applyStatusPill(statusColor(resp.Status))

	meta := fmt.Sprintf("   %s   ·   %s",
		formatDuration(resp.Duration), formatSize(resp.Size))
	// Issue #16: append the pixel dimensions of an image response, when readable
	// (PNG/JPEG/GIF; WebP/BMP preview but report no size via image.DecodeConfig).
	if classifyBody(rv.contentType, rv.fullBody) == bodyKindImage {
		if w, h, ok := imageDimensions(rv.fullBody); ok {
			meta += fmt.Sprintf("   ·   %d×%d", w, h)
		}
	}
	rv.metaLabel.SetText(meta)

	rv.renderHeaders(resp.Headers)
	rv.renderBody()
}

// contentTypeOf returns the Content-Type value from the response headers (the
// first match, case-insensitive on the key), or "" when absent. The raw value is
// returned verbatim; isXMLContentType / isHTMLContentType / isJSONContentType
// lower-case it and strip any `; charset=…` parameter themselves.
func contentTypeOf(headers []model.Param) string {
	for _, h := range headers {
		if strings.EqualFold(h.Key, "Content-Type") {
			return h.Value
		}
	}
	return ""
}

// renderHeaders writes the response headers into the left-column TextGrid,
// sorted by key for stable display, with each header key coloured cyan (mockup-v2
// `.rh .k`) and the value left in the default foreground.
func (rv *responseView) renderHeaders(headers []model.Param) {
	sorted := append([]model.Param(nil), headers...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })

	var b strings.Builder
	for _, h := range sorted {
		b.WriteString(h.Key)
		b.WriteString(": ")
		b.WriteString(h.Value)
		b.WriteByte('\n')
	}
	rv.headersGrid.SetText(b.String())

	// Colour the key (everything up to the first ": ") of each row cyan. Header
	// keys are ASCII, so the rune column equals the byte index for the key span.
	for r, h := range sorted {
		if r >= len(rv.headersGrid.Rows) {
			break
		}
		cells := rv.headersGrid.Rows[r].Cells
		end := len([]rune(h.Key))
		if end > len(cells) {
			end = len(cells)
		}
		for c := 0; c < end; c++ {
			cells[c].Style = styleRespHeaderKey
		}
	}
	rv.headersGrid.Refresh()

	rv.setHeadersBadge(len(sorted))
}

// setHeadersBadge shows the header count on the Headers sub-tab ("Headers 5"),
// or just "Headers" when there are none. Safe before the segment exists (no-op).
func (rv *responseView) setHeadersBadge(n int) {
	if rv.headersSeg != nil {
		rv.headersSeg.setLabel(tabBadge("Headers", n))
	}
}

// testsSummary formats the assertion pass tally, e.g. "3/4 passed". Pure +
// Fyne-free so it can be unit-tested directly.
func testsSummary(passed, total int) string {
	return fmt.Sprintf("%d/%d passed", passed, total)
}

// countAssertionsPassed returns how many of results passed. Pure + Fyne-free.
func countAssertionsPassed(results []postresp.AssertionResult) int {
	n := 0
	for _, r := range results {
		if r.Passed {
			n++
		}
	}
	return n
}

// setTestsBadge shows the passed count on the Tests sub-tab. When there is at
// least one assertion it shows "Tests 3/4"; with no assertions it shows just
// "Tests". Safe before the segment exists (no-op).
func (rv *responseView) setTestsBadge(passed, total int) {
	if rv.testsSeg == nil {
		return
	}
	if total <= 0 {
		rv.testsSeg.setLabel("Tests")
		return
	}
	rv.testsSeg.setLabel(fmt.Sprintf("Tests %d/%d", passed, total))
}

// clearTests empties the Tests tab and resets its badge — used when a send is
// pending or errors, so stale results from a previous response don't linger.
func (rv *responseView) clearTests() {
	if rv.testsBox == nil {
		return
	}
	rv.testsBox.Objects = nil
	rv.testsBox.Refresh()
	rv.setTestsBadge(0, 0)
}

// setTestResults renders the post-response Captures + Assertions into the Tests
// tab: a summary line ("3/4 passed"), one ✓/✗ row per assertion (red on fail,
// with its Source/Expr/Op/Expected, the Actual value, and any Err), and a
// "Captured" section listing each captured name=value. Captured values are
// session-only (shown for feedback; never persisted). The Tests-tab badge shows
// the pass tally.
func (rv *responseView) setTestResults(results []postresp.AssertionResult, captured map[string]string) {
	if rv.testsBox == nil {
		return
	}

	passed := countAssertionsPassed(results)
	total := len(results)

	objs := make([]fyne.CanvasObject, 0, total+len(captured)+4)

	// Summary line (bold).
	if total > 0 {
		objs = append(objs, widget.NewLabelWithStyle(
			testsSummary(passed, total), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	} else {
		objs = append(objs, widget.NewLabel("No assertions."))
	}

	for _, r := range results {
		objs = append(objs, rv.assertionRowObject(r))
	}

	// Captured section: name = value for each session-only captured variable.
	if len(captured) > 0 {
		objs = append(objs, widget.NewSeparator())
		objs = append(objs, widget.NewLabelWithStyle(
			"Captured", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
		// Stable order so the list doesn't reshuffle between sends.
		names := make([]string, 0, len(captured))
		for name := range captured {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			line := canvas.NewText(fmt.Sprintf("%s = %s", name, captured[name]), colorRespHeaderKey)
			line.TextStyle = fyne.TextStyle{Monospace: true}
			objs = append(objs, line)
		}
	}

	rv.testsBox.Objects = objs
	rv.testsBox.Refresh()
	rv.setTestsBadge(passed, total)
}

// assertionRowObject builds the single-line display for one assertion result: a
// coloured ✓/✗ + the check description, and (on the next line) the Actual value
// plus any engine Err, in red when the assertion failed.
func (rv *responseView) assertionRowObject(r postresp.AssertionResult) fyne.CanvasObject {
	col := colorAssertPass
	mark := "✓"
	if !r.Passed {
		col = colorAssertFail
		mark = "✗"
	}

	title := canvas.NewText(fmt.Sprintf("%s  %s", mark, describeAssertion(r.Assertion)), col)
	title.TextStyle = fyne.TextStyle{Bold: true}

	detailText := fmt.Sprintf("actual: %s", r.Actual)
	if r.Err != "" {
		detailText += "   error: " + r.Err
	}
	detail := canvas.NewText(detailText, col)
	detail.TextSize = theme.TextSize() - 1

	return container.NewVBox(title, detail)
}

// describeAssertion renders an assertion as a compact human string, e.g.
// "status equals 200" or "jsonBody data.id exists". Pure + Fyne-free.
func describeAssertion(a model.Assertion) string {
	src := string(a.Source)
	if a.Expr != "" {
		src += " " + a.Expr
	}
	s := fmt.Sprintf("%s %s", src, a.Op)
	if a.Expected != "" {
		s += " " + a.Expected
	}
	return s
}

// bodyKind classifies a response body for the Pretty path: which formatter and
// which syntax highlighter (if any) to use. It is derived from the Content-Type
// and, for XML/HTML, whether the body actually reformats cleanly.
type bodyKind int

const (
	kindText bodyKind = iota // no highlighting (plain SetText / line list)
	kindJSON                 // prettyJSON + buildJSONRows
	kindXML                  // formatXML + buildXMLRows
	kindHTML                 // buildXMLRows tag highlighting, no reflow
)

// prettyDisplay computes the Pretty-mode display text for body and the highlight
// kind to colour it with, dispatching on the response Content-Type:
//
//   - JSON (or a body that simply parses as JSON when the type is unknown) is
//     indented with prettyJSON and coloured as JSON.
//   - XML (isXMLContentType) is reformatted with formatXML; if it is well-formed
//     it is re-indented and coloured as XML, otherwise the original text is shown
//     and still tag-highlighted (kindXML over the raw text).
//   - HTML (isHTMLContentType) gets the same tag highlighting but is never
//     reflowed (HTML is often not well-formed XML); formatXML is attempted only
//     so genuinely XHTML bodies indent, and a failure falls back to the original
//     text, still tag-highlighted (kindHTML).
//   - anything else is plain text (kindText).
//
// It returns the display string and the kind; the caller decides whether the
// body is small enough to colour (large bodies fall back to the plain line list
// regardless of kind, preserving the virtualization rule).
func (rv *responseView) prettyDisplay(body []byte) (string, bodyKind) {
	ct := rv.contentType

	switch {
	case isHTMLContentType(ct):
		// HTML first: application/xhtml+xml satisfies isXMLContentType too, but
		// HTML must not be forcibly reflowed. XHTML may reformat; plain HTML
		// usually will not. Either way colour the markup, but never mangle it —
		// show the original text on a formatXML failure.
		if formatted, ok := formatXML(body); ok {
			return string(formatted), kindHTML
		}
		return string(body), kindHTML

	case isXMLContentType(ct):
		if formatted, ok := formatXML(body); ok {
			return string(formatted), kindXML
		}
		// Not well-formed: show as-is but still tag-highlight.
		return string(body), kindXML

	case ct == "" || isJSONContentType(ct):
		// JSON content type, or unknown type that happens to be valid JSON
		// (keeps the original "sniff JSON" behaviour for untyped bodies).
		if indented, ok := prettyJSON(body); ok {
			return string(indented), kindJSON
		}
		return string(body), kindText

	default:
		// A known, non-markup type (e.g. text/plain): no highlighting, but still
		// honour a JSON body mislabelled by the server.
		if indented, ok := prettyJSON(body); ok {
			return string(indented), kindJSON
		}
		return string(body), kindText
	}
}

// renderBody (re)renders the body honouring the Pretty/Raw toggle and the
// 256 KB display cap. It is called on send completion and on toggle.
func (rv *responseView) renderBody() {
	if rv.fullBody == nil {
		rv.clearBody()
		return
	}

	// Issue #16: route binary bodies to a preview instead of raw bytes. An image
	// shows the image preview in Pretty mode and the raw-bytes text view in Raw
	// mode (so the bytes stay inspectable); a PDF always shows the PDF panel. Find
	// keeps the text path so matches can be highlighted.
	if !rv.findActive {
		switch classifyBody(rv.contentType, rv.fullBody) {
		case bodyKindImage:
			if rv.pretty {
				rv.noticeLabel.Hide()
				rv.showImage()
				return
			}
			// Raw on an image: fall through to the text path (raw bytes).
		case bodyKindPDF:
			rv.noticeLabel.Hide()
			rv.showPDF()
			return
		}
	}

	body := rv.fullBody
	truncated := false
	if len(body) > maxDisplayBytes {
		body = body[:maxDisplayBytes]
		truncated = true
	}

	pretty := rv.pretty
	var display string
	kind := kindText

	if pretty {
		display, kind = rv.prettyDisplay(body)
	} else {
		display = string(body)
	}

	if pretty && len(display) >= largeBodyThreshold {
		// Large body in Pretty mode — the path the user reported as laggy. The
		// expensive part is the TextGrid itself: parseRows calls runewidth on a
		// freshly-allocated string per rune (~256K string allocations per 256 KB)
		// and the colouring pass adds a style per token. Render into the
		// lightweight virtualized List instead: only the on-screen lines become
		// widgets, so cost is independent of body size. Syntax colour is dropped
		// on this path (the trade-off) for JSON, XML and HTML alike; the view
		// stays read-only, monospace and scrollable per the read-only-response
		// rule. Raw mode keeps the plain TextGrid so a user can opt back into the
		// exact-bytes grid view.
		rv.showLargeBody(display)
	} else {
		// Small bodies, and Raw bodies of any size, keep the (optionally coloured)
		// TextGrid path.
		rv.showSmallBody(display, kind)
	}

	if truncated {
		rv.noticeLabel.SetText(fmt.Sprintf(
			"Showing first %s of %s — body truncated for display. Use Save Output As… for the full body.",
			formatSize(int64(maxDisplayBytes)), formatSize(int64(len(rv.fullBody)))))
		rv.noticeLabel.Show()
	} else {
		rv.noticeLabel.Hide()
	}
}

// showSmallBody renders display into the TextGrid and makes it the visible Body
// viewer (hiding the large-body List). For a coloured body it builds the styled
// rows in one zero-allocation pass — buildJSONRows for JSON, buildXMLRows for
// XML/HTML — and assigns them directly, avoiding the SetText parse + per-token
// SetStyleRange round-trip. Plain-text bodies keep plain SetText.
func (rv *responseView) showSmallBody(display string, kind bodyKind) {
	rv.bodyLines = nil
	rv.bodyList.Hide()
	rv.hideImage()
	rv.hidePDF()

	switch kind {
	case kindJSON:
		rv.bodyGrid.Rows = buildJSONRows(display)
		rv.bodyGrid.Refresh()
	case kindXML, kindHTML:
		rv.bodyGrid.Rows = buildXMLRows(display)
		rv.bodyGrid.Refresh()
	default:
		rv.bodyGrid.SetText(display)
	}
	rv.bodyList.Refresh()
	rv.bodyScroll.Show()
}

// showLargeBody renders display into the lightweight virtualized List and makes
// it the visible Body viewer (hiding the TextGrid). The TextGrid is cleared so
// it neither holds a stale large body nor pays its per-cell cost.
func (rv *responseView) showLargeBody(display string) {
	rv.bodyGrid.SetText("")
	rv.bodyScroll.Hide()
	rv.hideImage()
	rv.hidePDF()

	rv.bodyLines = strings.Split(display, "\n")
	rv.bodyList.Refresh()
	rv.bodyList.ScrollToTop()
	rv.bodyList.Show()
}

// clearBody resets every Body viewer to empty and shows the (empty) TextGrid —
// so a stale image or PDF preview never lingers behind a later text response or a
// pending/error state.
func (rv *responseView) clearBody() {
	rv.bodyLines = nil
	rv.bodyGrid.SetText("")
	rv.bodyList.Refresh()
	rv.bodyList.Hide()
	rv.hideImage()
	rv.hidePDF()
	rv.bodyScroll.Show()
}

// hideImage removes the image preview from the body stack (so no *canvas.Image
// lingers in the tree behind a later response) and drops its backing bytes.
func (rv *responseView) hideImage() {
	if rv.bodyImageScroll != nil {
		rv.removeFromStack(rv.bodyImageScroll)
		rv.bodyImageScroll = nil
	}
	rv.bodyImage = nil
}

// hidePDF removes the PDF panel from the body stack.
func (rv *responseView) hidePDF() {
	if rv.bodyPDF != nil {
		rv.removeFromStack(rv.bodyPDF)
		rv.bodyPDF = nil
		rv.bodyPDFLabel = nil
	}
}

// removeFromStack drops obj from bodyStack.Objects (no-op if absent) and refreshes.
func (rv *responseView) removeFromStack(obj fyne.CanvasObject) {
	objs := rv.bodyStack.Objects[:0]
	for _, o := range rv.bodyStack.Objects {
		if o != obj {
			objs = append(objs, o)
		}
	}
	rv.bodyStack.Objects = objs
	rv.bodyStack.Refresh()
}

// showImage builds an image preview from the full response body and makes it the
// visible Body viewer, hiding the text viewers and removing any PDF panel. The
// preview is inserted into bodyStack only while it is active (and removed by
// hideImage), so a later text/PDF response leaves no image behind. The image is
// wrapped in a fresh static resource so canvas.Image loads the new bytes.
func (rv *responseView) showImage() {
	rv.bodyLines = nil
	rv.bodyGrid.SetText("")
	rv.bodyScroll.Hide()
	rv.bodyList.Hide()
	rv.hidePDF()
	rv.hideImage() // drop a previous image before building the new one

	rv.bodyImage = canvas.NewImageFromResource(fyne.NewStaticResource("response", rv.fullBody))
	rv.bodyImage.FillMode = canvas.ImageFillContain
	rv.bodyImage.SetMinSize(fyne.NewSize(120, 120))
	// Wrap the preview so a right-click offers "Save image…". The wrapper renders
	// exactly rv.bodyImage (widget.NewSimpleRenderer), so the *canvas.Image stays
	// reachable by a tree walk — issue #16's visibleImages still finds it.
	wrapped := newImagePreview(rv.bodyImage, rv.saveImage)
	rv.bodyImageScroll = container.NewScroll(container.NewCenter(wrapped))

	rv.bodyStack.Add(rv.bodyImageScroll)
	rv.bodyStack.Refresh()
}

// showPDF builds the PDF panel and makes it the visible Body viewer, hiding the
// text viewers and removing any image preview. Like the image preview the panel
// is inserted into bodyStack only while active.
func (rv *responseView) showPDF() {
	rv.bodyLines = nil
	rv.bodyGrid.SetText("")
	rv.bodyScroll.Hide()
	rv.bodyList.Hide()
	rv.hideImage()
	rv.hidePDF() // rebuild fresh so the size line tracks the current body

	rv.bodyPDF = rv.buildPDFPanel()
	rv.bodyPDFLabel.SetText(fmt.Sprintf("PDF document  ·  %s", formatSize(int64(len(rv.fullBody)))))

	rv.bodyStack.Add(rv.bodyPDF)
	rv.bodyStack.Refresh()
}

// buildPDFPanel constructs the static PDF preview panel: a "PDF" glyph, a size
// label and Save…/Open buttons. yon ships no PDF renderer (no new deps); the
// panel offers Save Output As… (reusing saveToFile, defaulting response.pdf) and
// Open, which writes the bytes to a temp file and hands it to the OS default app
// (updater.OpenFile: macOS `open`, Linux `xdg-open`, Windows `start`).
//
// The panel deliberately uses text-only widgets (no widget.Icon / icon buttons,
// each of which renders a canvas.Image): the only *canvas.Image the body region
// ever holds is a genuine image preview, so "is an image being previewed?" stays
// answerable by scanning the body for a canvas.Image.
func (rv *responseView) buildPDFPanel() *fyne.Container {
	glyph := canvas.NewText("PDF", theme.Color(theme.ColorNamePlaceHolder))
	glyph.TextStyle = fyne.TextStyle{Bold: true}
	glyph.TextSize = theme.TextSize() * 2
	glyph.Alignment = fyne.TextAlignCenter

	rv.bodyPDFLabel = widget.NewLabel("PDF document")
	rv.bodyPDFLabel.Alignment = fyne.TextAlignCenter

	saveBtn := widget.NewButton("Save…", rv.savePDF)
	openBtn := widget.NewButton("Open", rv.openPDF)
	saveBtn.Importance = widget.HighImportance
	buttons := container.NewHBox(layout.NewSpacer(), saveBtn, openBtn, layout.NewSpacer())

	col := container.NewVBox(
		layout.NewSpacer(),
		container.NewCenter(glyph),
		rv.bodyPDFLabel,
		buttons,
		layout.NewSpacer(),
	)
	return container.NewCenter(col)
}

// savePDF writes the full PDF body to a user-chosen file, defaulting the name to
// response.pdf (reusing the same native/Fyne save path as Save Output As…).
func (rv *responseView) savePDF() {
	if rv.fullBody == nil {
		return
	}
	go func() {
		path, ok, err := nativeSaveAny("Save PDF",
			responseDefaultFilename(rv.contentType, rv.fullBody))
		fyne.Do(func() {
			switch {
			case err != nil:
				rv.saveToFileFyne()
			case !ok:
				// cancelled
			default:
				if werr := os.WriteFile(path, rv.fullBody, 0o644); werr != nil {
					dialog.ShowError(werr, rv.parent)
				}
			}
		})
	}()
}

// openPDF writes the body to a temp .pdf and opens it in the OS default viewer
// (updater.OpenFile). The temp file is left for the viewer to read (cleaning it
// up immediately would race the launcher); the OS tempdir is reclaimed by the
// system. Errors surface in a dialog.
func (rv *responseView) openPDF() {
	if rv.fullBody == nil {
		return
	}
	go func() {
		f, err := os.CreateTemp("", "yon-response-*.pdf")
		if err == nil {
			_, err = f.Write(rv.fullBody)
			cerr := f.Close()
			if err == nil {
				err = cerr
			}
		}
		if err == nil {
			err = updater.OpenFile(f.Name())
		}
		if err != nil {
			fyne.Do(func() { dialog.ShowError(err, rv.parent) })
		}
	}()
}

// saveToFile writes the full (un-truncated) body to a user-chosen file via a
// native OS save dialog, falling back to Fyne's in-app dialog if unavailable.
func (rv *responseView) saveToFile() {
	if rv.fullBody == nil {
		return
	}
	go func() {
		path, ok, err := nativeSaveAny("Save Response Body",
			responseDefaultFilename(rv.contentType, rv.fullBody))
		fyne.Do(func() {
			switch {
			case err != nil:
				rv.saveToFileFyne()
			case !ok:
				// cancelled
			default:
				if werr := os.WriteFile(path, rv.fullBody, 0o644); werr != nil {
					dialog.ShowError(werr, rv.parent)
				}
			}
		})
	}()
}

// saveImage writes the full image body to a user-chosen file, defaulting the
// filename to the format-correct responseDefaultFilename (e.g. response.png /
// response.jpg). It reuses the same native/Fyne save path as saveToFile and
// shares its Fyne in-app fallback on a native-dialog error. Invoked from the
// "Save image…" right-click menu on the inline image preview.
func (rv *responseView) saveImage() {
	if rv.fullBody == nil {
		return
	}
	go func() {
		path, ok, err := nativeSaveAny("Save Image",
			responseDefaultFilename(rv.contentType, rv.fullBody))
		fyne.Do(func() {
			switch {
			case err != nil:
				rv.saveToFileFyne()
			case !ok:
				// cancelled
			default:
				if werr := os.WriteFile(path, rv.fullBody, 0o644); werr != nil {
					dialog.ShowError(werr, rv.parent)
				}
			}
		})
	}()
}

// saveToFileFyne is the Fyne in-app fallback for saveToFile().
func (rv *responseView) saveToFileFyne() {
	dialog.ShowFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil || wc == nil {
			return
		}
		defer wc.Close()
		if _, werr := wc.Write(rv.fullBody); werr != nil {
			dialog.ShowError(werr, rv.parent)
		}
	}, rv.parent)
}

// showPopout opens the response body in a separate, resizable window. Its default
// view is a selectable Textbox the user can drag-select and copy from; pressing
// Cmd/Ctrl+F swaps it for a TextGrid that highlights matches (Entry cannot
// highlight ranges) and reveals the find bar, and Esc swaps back. The body is
// capped at maxPopoutBytes for responsiveness; Save Output As… writes it all.
func (rv *responseView) showPopout() {
	if rv.fullBody == nil {
		return
	}
	app := fyne.CurrentApp()
	if app == nil {
		return
	}

	full := len(rv.fullBody)
	body := rv.fullBody
	if full > maxPopoutBytes {
		body = body[:maxPopoutBytes]
	}
	text := string(body)
	if rv.pretty {
		text, _ = rv.prettyDisplay(body)
	}

	// Default view: a selectable Textbox (drag-select + copy). It intercepts
	// Cmd/Ctrl+F itself so find opens even while the Textbox holds focus.
	entry := newShortcutEntry(true)
	entry.SetText(text)

	// Find view: a TextGrid that supports match highlighting, shown only while
	// find is open. It overlays the entry in a Stack; one is hidden at a time.
	grid := widget.NewTextGrid()
	gridScroll := container.NewScroll(grid)
	gridScroll.Hide()

	var search gridSearch
	search.bind(grid, gridScroll, text, func() { grid.SetText(text) })

	win := app.NewWindow("Response")
	win.SetIcon(appIcon)

	var find *findBar
	openFind := func() {
		entry.Hide()
		grid.SetText(text)
		gridScroll.Show()
		find.container.Show()
		find.container.Refresh()
		win.Canvas().Focus(find.query)
		c, t := search.search(find.query.Text)
		find.setCount(c, t)
	}
	closeFind := func() {
		find.container.Hide()
		search.clear()
		gridScroll.Hide()
		entry.Show()
	}
	find = newFindBar(
		func(q string) { c, t := search.search(q); find.setCount(c, t) },
		func() { c, t := search.move(1); find.setCount(c, t) },
		func() { c, t := search.move(-1); find.setCount(c, t) },
		closeFind,
	)
	entry.onFind = openFind

	copyBtn := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		app.Clipboard().SetContent(string(rv.fullBody))
	})
	saveBtn := widget.NewButtonWithIcon("Save Output As…", theme.DocumentSaveIcon(), rv.saveToFile)
	buttons := container.NewHBox(copyBtn, saveBtn)

	var bar fyne.CanvasObject
	if full > maxPopoutBytes {
		notice := widget.NewLabel(fmt.Sprintf(
			"Showing first %s of %s — use Save Output As… for the full body.",
			formatSize(int64(maxPopoutBytes)), formatSize(int64(full))))
		bar = container.NewBorder(nil, nil, notice, buttons)
	} else {
		bar = container.NewBorder(nil, nil, nil, buttons)
	}

	stack := container.NewStack(entry, gridScroll)
	win.SetContent(container.NewBorder(
		container.NewVBox(bar, find.container), nil, nil, nil, stack))
	win.Resize(fyne.NewSize(960, 720))
	addFindShortcuts(win, openFind, closeFind)
	win.Show()
}

// imagePreview wraps the inline response image (a *canvas.Image) so a right-click
// offers a "Save image…" context menu. It renders exactly the wrapped image via
// widget.NewSimpleRenderer, so the *canvas.Image stays a reachable child in the
// rendered tree — issue #16's image-walk (visibleImages over rv.bodyStack) still
// finds the preview. It is only built in showImage, so the menu never appears for
// a text or PDF response.
type imagePreview struct {
	widget.BaseWidget
	img    *canvas.Image
	onSave func()
}

// newImagePreview builds the right-clickable wrapper around img; onSave runs when
// the "Save image…" menu item is chosen.
func newImagePreview(img *canvas.Image, onSave func()) *imagePreview {
	p := &imagePreview{img: img, onSave: onSave}
	p.ExtendBaseWidget(p)
	return p
}

// CreateRenderer renders the wrapped image directly, keeping the *canvas.Image a
// reachable child of the widget tree.
func (p *imagePreview) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.img)
}

// TappedSecondary shows a one-item "Save image…" context menu at the click point,
// mirroring the sidebar's right-click idiom (verbRow.TappedSecondary).
func (p *imagePreview) TappedSecondary(e *fyne.PointEvent) {
	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Save image…", func() {
			if p.onSave != nil {
				p.onSave()
			}
		}),
	}
	menu := fyne.NewMenu("", items...)
	if c := fyne.CurrentApp().Driver().CanvasForObject(p); c != nil {
		widget.ShowPopUpMenuAtPosition(menu, c, e.AbsolutePosition)
	}
}

// statusColor maps an HTTP status code to its class colour.
func statusColor(code int) color.Color {
	switch {
	case code >= 200 && code < 300:
		return colorStatus2xx
	case code >= 300 && code < 400:
		return colorStatus3xx
	case code >= 400 && code < 500:
		return colorStatus4xx
	case code >= 500:
		return colorStatus5xx
	default:
		return color.Gray{Y: 0x88}
	}
}

// formatDuration renders a send duration compactly.
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%d µs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2f s", d.Seconds())
}

// formatSize renders a byte count as B / KB / MB.
func formatSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
