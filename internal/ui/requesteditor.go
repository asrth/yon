package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ultramcu/yon/internal/model"
	"github.com/ultramcu/yon/internal/postresp"
	"github.com/ultramcu/yon/internal/yonner"
)

// methodOptions lists the well-known HTTP methods offered in the Method
// drop-down. The selector is editable, so the user may also type any other
// (custom) verb.
var methodOptions = []string{
	string(model.MethodGet),
	string(model.MethodPost),
	string(model.MethodPut),
	string(model.MethodDelete),
	string(model.MethodPatch),
	string(model.MethodHead),
	string(model.MethodOptions),
}

// bodyTypeLabels lists the Body-type Select values, label → model.BodyType.
var bodyTypeLabels = []string{"None", "JSON", "XML", "Text"}

func bodyTypeFromLabel(label string) model.BodyType {
	switch label {
	case "JSON":
		return model.BodyJSON
	case "XML":
		return model.BodyXML
	case "Text":
		return model.BodyText
	default:
		return model.BodyNone
	}
}

func bodyTypeToLabel(t model.BodyType) string {
	switch t {
	case model.BodyJSON:
		return "JSON"
	case model.BodyXML:
		return "XML"
	case model.BodyText:
		return "Text"
	default:
		return "None"
	}
}

// requestTab is one open Request editor living in the window's DocTabs. It edits
// the Request at Collection index idx, commits edits back via the window, runs
// Send off the UI goroutine (one in-flight send at a time), and renders the
// Response into a read-only viewer.
type requestTab struct {
	win *Window
	idx int

	tab *tabCard

	// editing widgets
	nameEntry   *widget.Entry
	methodSel   *widget.SelectEntry
	urlEntry    *widget.Entry
	paramsTable *kvTable
	headerTable *kvTable
	authEditor  *authEditor
	bodyTypeSel *widget.Select
	bodyEntry   *widget.Entry
	optionsTab  *requestOptionsTab
	testsTab    *testsTab
	// curlEntry shows the equivalent curl command in the "cURL" sub-tab. Its text
	// is not part of the request model — refreshCurl rebuilds it on every commit,
	// so any user edit to it is harmless (overwritten on the next refresh).
	curlEntry *widget.Entry

	sendBtn *widget.Button

	// sub-tab items kept so their count badges ("Params 3"/"Headers 1") can be
	// updated when rows change.
	paramsSeg *segButton
	headerSeg *segButton
	segTabs   *segTabs

	response *responseView

	// syncing guards the two-way URL⇄Params mirror against feedback loops: while
	// one side is rewriting the other, the reverse handler is a no-op.
	syncing bool

	// dirty marks this tab as having unsaved edits — shows a ● on its DocTab.
	dirty bool
	// lastResp is the most recent successful Response (nil if none/error), shown
	// in the window status bar when this tab is the active one.
	lastResp *model.Response

	// in-flight send state: cancel is non-nil while a send is running. sendSeq is
	// a generation token bumped on every start and every cancel, so a returning
	// goroutine can tell whether its result is still the live one (a fast
	// cancel→resend must not let the stale send's result clobber the new one).
	cancel  context.CancelFunc
	sendSeq uint64
}

// newRequestTab builds an editor Tab for the Request at idx.
func newRequestTab(w *Window, idx int) *requestTab {
	rt := &requestTab{win: w, idx: idx}
	req := w.coll.Requests[idx]

	rt.nameEntry = widget.NewEntry()
	rt.nameEntry.SetPlaceHolder("Request name (optional)")
	rt.nameEntry.SetText(req.Name)
	rt.nameEntry.OnChanged = func(string) { rt.commit() }

	// Editable method drop-down: the user can pick a well-known verb or type an
	// arbitrary custom one. Set its initial value before wiring OnChanged —
	// otherwise SetText fires commit() before the Param/Header/Auth/Body widgets
	// below exist, dereferencing nil (panics on tab construction).
	rt.methodSel = widget.NewSelectEntry(methodOptions)
	if req.Method == "" {
		rt.methodSel.SetText(string(model.MethodGet))
	} else {
		rt.methodSel.SetText(string(req.Method))
	}
	rt.methodSel.OnChanged = func(s string) {
		// Uppercase the verb in place so "patch" → "PATCH"; guard the SetText to
		// avoid a redundant re-entrant change event.
		if up := strings.ToUpper(s); up != s {
			rt.methodSel.SetText(up)
			return // SetText re-enters OnChanged with the uppercased value
		}
		rt.commit()
	}

	// Two-way URL⇄Params sync: merge any query string already in the saved URL
	// into the Params, and show the URL with that query reconstructed from the
	// enabled params. current() stores the query only in Params, so it is never
	// sent twice. Seed syncedParams here so both the URL field and the Params
	// table below start from the same data.
	urlBase, urlQuery := splitURLQuery(req.URL)
	syncedParams := append(parseQueryParams(urlQuery), req.Params...)

	rt.urlEntry = widget.NewEntry()
	rt.urlEntry.SetPlaceHolder("https://api.example.com/path")
	// SetText before wiring OnChanged so this seed does not fire the sync handler
	// (which references rt.paramsTable, built further below).
	rt.urlEntry.SetText(joinURLQuery(urlBase, encodeQueryParams(syncedParams)))
	rt.urlEntry.OnChanged = func(s string) { rt.onURLEdited(s) }
	// Enter in the URL field sends the request (unless one is already in flight).
	rt.urlEntry.OnSubmitted = func(string) {
		if rt.cancel == nil {
			rt.startSend()
		}
	}

	rt.sendBtn = widget.NewButtonWithIcon("Send", theme.MailSendIcon(), rt.onSend)
	rt.sendBtn.Importance = widget.HighImportance

	// Top bar: [Method ▾][URL.................][✈ Send] — the Method Select renders
	// as a "GET ▾" pill and Send is the primary (cyan) action. The method box is
	// given a fixed width so the longest verb ("OPTIONS") and the dropdown arrow
	// are never clipped (the SelectEntry's own MinSize is too narrow). The "Copy as
	// cURL" command now lives in the "cURL" sub-tab below, not a top-bar button.
	methodBox := container.NewGridWrap(
		fyne.NewSize(112, rt.methodSel.MinSize().Height), rt.methodSel)
	topBar := container.NewBorder(nil, nil, methodBox, rt.sendBtn, rt.urlEntry)

	rt.paramsTable = newKVTable(syncedParams, func() { rt.onParamsEdited() })
	rt.headerTable = newKVTable(req.Headers, func() { rt.commit() })
	rt.authEditor = newAuthEditor(req.Auth, true, func() { rt.commit() })
	bodyPane := rt.buildBody(req)

	rt.segTabs = newSegTabs()
	rt.paramsSeg = rt.segTabs.Append("Params", rt.paramsTable.container)
	rt.headerSeg = rt.segTabs.Append("Headers", rt.headerTable.container)
	rt.segTabs.Append("Auth", container.NewVScroll(rt.authEditor.container))
	rt.segTabs.Append("Body", bodyPane)
	// Build the Options sub-tab before refreshTabBadges/anything calls current():
	// newRequestOptionsTab wires its controls to rt.commit(), and current() reads
	// rt.optionsTab.value(), so optionsTab must exist before either can fire.
	rt.optionsTab = newRequestOptionsTab(rt, req)
	rt.segTabs.Append("Options", rt.optionsTab.container)
	// Tests sub-tab (Captures + Assertions). Built after Options for the same
	// reason: newTestsTab wires its row controls to rt.commit(), and current()
	// reads rt.testsTab, so the tab must exist before either can fire. Seeding the
	// rows happens before their OnChanged handlers are wired, so opening a request
	// with saved captures/assertions does not dirty a fresh tab.
	rt.testsTab = newTestsTab(rt, req)
	rt.segTabs.Append("Tests", rt.testsTab.container)
	// cURL sub-tab (read-and-copy view of the equivalent curl command). Appended
	// last; seeded with refreshCurl below once every sub-editor (incl. options/
	// tests) exists, so current() — which curlCommand() reads — is complete.
	rt.segTabs.Append("cURL", rt.buildCurlTab())
	rt.refreshCurl()
	rt.refreshTabBadges() // initial counts from the seeded Request

	editorTop := container.NewVBox(
		rt.nameEntry,
		topBar,
	)
	requestPane := container.NewBorder(editorTop, nil, nil, nil, rt.segTabs.object())

	rt.response = newResponseView(w.win)

	// Request editor on top, Response below, resizable.
	split := container.NewVSplit(requestPane, rt.response.container)
	split.SetOffset(0.5)

	// Build the card with a clean (non-dirty) label, matching the old DocTab
	// which was created with the plain title: setting the method drop-down above
	// fires a construction-time commit that flips rt.dirty, but a freshly opened
	// tab must not show the unsaved "●" until the user actually edits it.
	rt.tab = newTabCard(split)
	rt.tab.setRequest(req.Method, req.DisplayName(), false)
	rt.dirty = false
	return rt
}

// buildBody constructs the Body sub-tab: a type Select plus a multiline Entry
// (editing input may use an Entry per the read-only-response rule). The editor is hidden for None.
// For XML a "Format" button pretty-indents the editor contents in place.
func (rt *requestTab) buildBody(req model.Request) fyne.CanvasObject {
	rt.bodyEntry = widget.NewMultiLineEntry()
	rt.bodyEntry.SetText(req.Body.Content)
	rt.bodyEntry.OnChanged = func(string) { rt.commit() }
	rt.bodyEntry.Wrapping = fyne.TextWrapOff

	// "Format" pretty-prints the XML body; only shown for the XML body type.
	formatBtn := widget.NewButton("Format", rt.formatXMLBody)
	formatBtn.Importance = widget.LowImportance

	rt.bodyTypeSel = widget.NewSelect(bodyTypeLabels, func(label string) {
		if label == "None" {
			rt.bodyEntry.Hide()
		} else {
			rt.bodyEntry.Show()
		}
		if label == "XML" {
			formatBtn.Show()
		} else {
			formatBtn.Hide()
		}
		rt.commit()
	})
	rt.bodyTypeSel.SetSelected(bodyTypeToLabel(req.Body.Type))
	if req.Body.Type == model.BodyNone || req.Body.Type == "" {
		rt.bodyEntry.Hide()
	}
	if req.Body.Type != model.BodyXML {
		formatBtn.Hide()
	}

	return container.NewBorder(
		container.NewHBox(widget.NewLabel("Body type:"), rt.bodyTypeSel, formatBtn),
		nil, nil, nil,
		rt.bodyEntry,
	)
}

// formatXMLBody pretty-indents the current XML body in place using formatXML
// (provided by xmlformat.go). On malformed input formatXML returns ok=false and
// the editor is left untouched.
func (rt *requestTab) formatXMLBody() {
	out, ok := formatXML([]byte(rt.bodyEntry.Text))
	if !ok {
		return
	}
	rt.bodyEntry.SetText(string(out))
}

// current reads the editor widgets back into a model.Request. The URL field's
// query string is mirrored in the Params table (two-way sync), so only the base
// URL is stored here — yonner appends the params when building the request, and
// storing the query in both places would send it twice.
func (rt *requestTab) current() model.Request {
	urlBase, _ := splitURLQuery(rt.urlEntry.Text)
	// Options can be read very early (a construction-time commit may fire before
	// the Options sub-tab is built), so nil-guard it: nil optionsTab → nil
	// Options → "inherit the global Settings".
	var options *model.RequestOptions
	if rt.optionsTab != nil {
		options = rt.optionsTab.value()
	}
	// Captures/Assertions come from the Tests sub-tab. Nil-guard it the same way
	// (a construction-time commit may fire before the Tests tab is built); the
	// tab's readers already return nil slices when empty, so the Request's
	// omitempty drops the JSON keys for a request with no tests.
	var captures []model.Capture
	var assertions []model.Assertion
	if rt.testsTab != nil {
		captures = rt.testsTab.captures()
		assertions = rt.testsTab.assertions()
	}
	return model.Request{
		Name:    rt.nameEntry.Text,
		Method:  model.Method(strings.ToUpper(rt.methodSel.Text)),
		URL:     urlBase,
		Params:  rt.paramsTable.value(),
		Headers: rt.headerTable.value(),
		Auth:    rt.authEditor.value(),
		Body: model.Body{
			Type:    bodyTypeFromLabel(rt.bodyTypeSel.Selected),
			Content: rt.bodyEntry.Text,
		},
		Options:    options,
		Captures:   captures,
		Assertions: assertions,
	}
}

// onURLEdited handles a change to the URL field: it reparses the query string
// into the Params table (enabled rows mirror the URL query; disabled rows are
// preserved as "kept but not sent"), then commits. The syncing guard stops the
// table rebuild from echoing back as a URL rewrite.
func (rt *requestTab) onURLEdited(s string) {
	if !rt.syncing {
		rt.syncing = true
		_, query := splitURLQuery(s)
		merged := mergeQueryIntoParams(rt.paramsTable.value(), parseQueryParams(query))
		rt.paramsTable.setValue(merged)
		rt.syncing = false
	}
	rt.commit()
}

// onParamsEdited handles a change to the Params table: it rewrites the URL
// field's query string from the enabled rows (keeping the base path), then
// commits. The syncing guard stops the URL rewrite from echoing back as a table
// rebuild.
func (rt *requestTab) onParamsEdited() {
	if !rt.syncing {
		rt.syncing = true
		base, _ := splitURLQuery(rt.urlEntry.Text)
		if next := joinURLQuery(base, encodeQueryParams(rt.paramsTable.value())); next != rt.urlEntry.Text {
			rt.urlEntry.SetText(next)
		}
		rt.syncing = false
	}
	rt.commit()
}

// commit writes the editor's Request back into the Collection (marks dirty,
// updates sidebar + tab title) and refreshes the Params/Headers count badges.
func (rt *requestTab) commit() {
	rt.dirty = true
	rt.refreshTabBadges()
	rt.refreshCurl() // keep the cURL sub-tab live as the user edits
	rt.win.commitRequest(rt.idx, rt.current())
	rt.win.updateStatusBar()
}

// curlCommand builds the equivalent curl command for the current request,
// matching what startSend would actually send: it expands {{variable}} templates
// with the active environment + collection variables (so the copied curl uses the
// resolved values rather than literal, URL-escaped {{names}}) and overlays this
// request's per-request overrides (--max-time, -k, and -L / no -L).
func (rt *requestTab) curlCommand() string {
	opts := rt.win.app.settings.options()
	opts.Resolve = rt.win.varScope().Resolve
	req := rt.current()
	opts = yonner.ApplyRequestOptions(opts, req.Options)
	return yonner.ToCurl(req, rt.win.coll, opts)
}

// buildCurlTab constructs the "cURL" sub-tab: a wrapped, multiline Entry showing
// the equivalent curl command and a Copy button below it. The Entry has no
// OnChanged (its text is not part of the request model — refreshCurl overwrites
// it on every commit), so editing it neither dirties the tab nor is persisted.
func (rt *requestTab) buildCurlTab() fyne.CanvasObject {
	rt.curlEntry = widget.NewMultiLineEntry()
	rt.curlEntry.Wrapping = fyne.TextWrapBreak

	copyBtn := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		if a := fyne.CurrentApp(); a != nil {
			a.Clipboard().SetContent(rt.curlEntry.Text)
		}
	})
	copyBtn.Importance = widget.HighImportance

	return container.NewBorder(nil, copyBtn, nil, nil, rt.curlEntry)
}

// refreshCurl rebuilds the cURL sub-tab's text from the current request. Called
// from commit(), so it is nil-guarded: a construction-time commit can fire before
// buildCurlTab has assigned curlEntry. SetText carries no OnChanged, so the
// refresh never commits back (a fresh tab stays non-dirty).
func (rt *requestTab) refreshCurl() {
	if rt.curlEntry == nil {
		return
	}
	rt.curlEntry.SetText(rt.curlCommand())
}

// enabledParamCount counts the rows that are both enabled and have content
// (key or value present) — i.e. the params/headers that will actually be sent.
func enabledParamCount(ps []model.Param) int {
	n := 0
	for _, p := range ps {
		if p.Enabled && (p.Key != "" || p.Value != "") {
			n++
		}
	}
	return n
}

// tabBadge renders a sub-tab title with a trailing count when non-empty, e.g.
// "Params 3"; an empty/zero count shows just the base label ("Params").
func tabBadge(base string, n int) string {
	if n <= 0 {
		return base
	}
	return fmt.Sprintf("%s %d", base, n)
}

// refreshTabBadges updates the Params/Headers sub-tab titles with the current
// enabled+present row counts. Safe to call before the segments exist (no-op).
func (rt *requestTab) refreshTabBadges() {
	if rt.paramsSeg == nil || rt.headerSeg == nil {
		return
	}
	rt.paramsSeg.setLabel(tabBadge("Params", enabledParamCount(rt.paramsTable.value())))
	rt.headerSeg.setLabel(tabBadge("Headers", enabledParamCount(rt.headerTable.value())))
}

// onSend toggles between starting a send and cancelling the in-flight one.
func (rt *requestTab) onSend() {
	if rt.cancel != nil {
		rt.cancelInFlight()
		return
	}
	rt.startSend()
}

// startSend launches a send off the UI goroutine. One in-flight send per Tab:
// the button flips to "Cancel" until the send completes or is cancelled.
func (rt *requestTab) startSend() {
	req := rt.current()
	coll := rt.win.coll
	opts := rt.win.app.settings.options()
	// Expand {{variable}} templates (URL, params, headers, body, auth) using the
	// window's active environment + collection variables.
	opts.Resolve = rt.win.varScope().Resolve
	// When the active environment defines a complete SSH jump host, dial every
	// Request THROUGH it; otherwise opts.DialContext stays nil and the send uses
	// the default transport unchanged.
	if jh, ok := rt.win.app.activeJumpHost(rt.win); ok {
		opts.DialContext = rt.win.app.tunnels.DialContext(jh)
	}
	// Overlay this request's per-request overrides (timeout, TLS, redirects) onto
	// the global options for the SAME req that is sent below.
	opts = yonner.ApplyRequestOptions(opts, req.Options)

	ctx, cancel := context.WithCancel(context.Background())
	rt.sendSeq++
	seq := rt.sendSeq
	rt.cancel = cancel
	rt.setSending(true)
	rt.response.setPending()

	go func() {
		resp, err := yonner.Send(ctx, req, coll, opts)
		// Marshal the result back onto the UI goroutine (Fyne 2.7 requires UI
		// updates on the main goroutine — fyne.Do schedules this safely).
		fyne.Do(func() {
			// Ignore a result that a later cancel or a newer send has superseded.
			if rt.sendSeq != seq {
				return
			}
			rt.cancel = nil
			rt.setSending(false)
			// Resolve the request URL once for the combined request log, so the log
			// records the real target (env/collection value) rather than {{templates}}.
			resolve := rt.win.varScope().Resolve
			resolvedURL := resolve(req.URL)
			// Capture the request EXACTLY as sent (resolved headers + body) so the
			// log-detail window can show what actually went on the wire. Headers and
			// body are templated; resolve them with the same scope used for the send.
			reqHeaders := resolveLogHeaders(req.Headers, resolve)
			var reqBody string
			if req.Body.Type != model.BodyNone && req.Body.Type != "" {
				reqBody = resolve(req.Body.Content)
			}
			if err != nil {
				rt.lastResp = nil
				rt.response.setError(err)
				rt.win.appendLog(logEntry{
					Time:       time.Now(),
					Name:       req.DisplayName(),
					Method:     string(req.Method),
					URL:        resolvedURL,
					ReqHeaders: reqHeaders,
					ReqBody:    reqBody,
					Err:        err.Error(),
				})
				rt.win.updateStatusBar()
				return
			}
			r := resp
			rt.lastResp = &r
			rt.response.setResponse(resp)

			// Post-response: run Captures, then Assertions, against this Response.
			// Captured values are SESSION-ONLY runtime vars (never persisted): merge
			// them into the window's runtimeVars so the next send resolves them
			// (highest precedence — see varScope). Assertions are evaluated and the
			// pass/fail results plus the captured values are rendered in the
			// response's Tests tab.
			vars, _ := postresp.RunCaptures(resp, req.Captures)
			if len(vars) > 0 {
				if rt.win.runtimeVars == nil {
					rt.win.runtimeVars = make(map[string]string, len(vars))
				}
				for k, v := range vars {
					rt.win.runtimeVars[k] = v
				}
				// Captured runtime vars changed — refresh the Variables dock (no-op
				// when hidden) so it reflects the new highest-precedence values.
				rt.win.refreshVarsPanel()
			}
			results := postresp.RunAssertions(resp, req.Assertions)
			rt.response.setTestResults(results, vars)

			rt.win.appendLog(logEntry{
				Time:        time.Now(),
				Name:        req.DisplayName(),
				Method:      string(req.Method),
				URL:         resolvedURL,
				ReqHeaders:  reqHeaders,
				ReqBody:     reqBody,
				Status:      resp.Status,
				StatusText:  resp.StatusText,
				Duration:    resp.Duration,
				Size:        resp.Size,
				RespHeaders: resp.Headers,
				RespBody:    capLogBody(resp.Body),
			})

			rt.win.updateStatusBar()
		})
	}()
}

// maxLogBodyBytes caps how many response-body bytes a logEntry retains so the
// session log cannot balloon memory when many large responses are logged. The
// true byte count is preserved separately in logEntry.Size; the detail window
// notes the truncation. 1 MiB is generous for an at-a-glance detail view.
const maxLogBodyBytes = 1 << 20

// capLogBody returns the first maxLogBodyBytes of body as a fresh slice (so the
// log does not pin the full, possibly-large response buffer in memory). A body
// at or under the cap is copied as-is; nil stays nil.
func capLogBody(body []byte) []byte {
	if len(body) == 0 {
		return nil
	}
	n := len(body)
	if n > maxLogBodyBytes {
		n = maxLogBodyBytes
	}
	out := make([]byte, n)
	copy(out, body[:n])
	return out
}

// resolveLogHeaders renders the request's headers as SENT: each ENABLED header's
// Key and Value run through resolve so {{templates}} match what went on the wire.
// Disabled rows are dropped (they are not sent). The Enabled flag is set true on
// the returned Params since every retained row was applied.
func resolveLogHeaders(headers []model.Param, resolve func(string) string) []model.Param {
	if len(headers) == 0 {
		return nil
	}
	out := make([]model.Param, 0, len(headers))
	for _, h := range headers {
		if !h.Enabled {
			continue
		}
		out = append(out, model.Param{
			Key:     resolve(h.Key),
			Value:   resolve(h.Value),
			Enabled: true,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cancelInFlight cancels a running send (if any) and resets the button. Safe to
// call when nothing is in flight.
func (rt *requestTab) cancelInFlight() {
	if rt.cancel == nil {
		return
	}
	rt.cancel()
	rt.cancel = nil
	rt.sendSeq++ // invalidate the in-flight send's pending result
	rt.setSending(false)
}

// setSending switches the Send button between "Send" and "Cancel".
func (rt *requestTab) setSending(sending bool) {
	if sending {
		rt.sendBtn.SetText("Cancel")
		rt.sendBtn.SetIcon(theme.CancelIcon())
		rt.sendBtn.Importance = widget.DangerImportance
	} else {
		rt.sendBtn.SetText("Send")
		rt.sendBtn.SetIcon(theme.MailSendIcon())
		rt.sendBtn.Importance = widget.HighImportance
	}
	rt.sendBtn.Refresh()
}
