package ui

import (
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ultramcu/yon/internal/model"
)

// secretMask is the placeholder rendered in place of a Secret variable's value.
// A Secret's clear value MUST NOT appear in the panel (privacy requirement), so
// renderVarLine always substitutes this mask for a Secret row.
const secretMask = "••••"

// ---- Pure collector (Fyne-free, unit-testable) ----

// varView is one resolved variable row shown in the inspector. Scope is one of
// "env", "collection", or "runtime". It carries the variable's Key/Value/Secret
// so the panel can render and mask without re-reading the source model.
type varView struct {
	Key    string
	Value  string
	Secret bool
	Scope  string // "env", "collection", or "runtime"
}

// Scope label constants, kept as the contract between the collector and the
// panel (and the blind tester).
const (
	scopeEnv        = "env"
	scopeCollection = "collection"
	scopeRuntime    = "runtime"
)

// collectVariableView projects the active environment, the collection variables,
// and the session runtime captures into two ordered, deduplicated row slices.
//
// configured holds the CONFIGURED variables in precedence/display order:
//   - the active env's ENABLED variables first (Scope "env"), in slice order;
//   - then the collection's ENABLED variables (Scope "collection"), in slice
//     order, EXCEPT any whose Key already appears as an env key — env wins on a
//     key clash (dedupe by Key), mirroring variables.Scope.Lookup precedence.
//
// A zero/empty env (no active environment) contributes no env rows; pass the
// zero model.Environment in that case.
//
// runtimeRows holds one row per runtime entry (Scope "runtime", Secret=false),
// SORTED by Key for a deterministic display. A nil runtime map yields an empty
// (nil) slice.
//
// Only ENABLED variables are shown — a disabled variable does not resolve in the
// engine, so surfacing it would mislead. Secret/Value are carried verbatim;
// masking is the renderer's job (see renderVarLine), never the collector's.
func collectVariableView(env model.Environment, collVars []model.Variable, runtime map[string]string) (configured, runtimeRows []varView) {
	seen := make(map[string]bool)

	for _, v := range env.Variables {
		if !v.Enabled {
			continue
		}
		seen[v.Key] = true
		configured = append(configured, varView{
			Key:    v.Key,
			Value:  v.Value,
			Secret: v.Secret,
			Scope:  scopeEnv,
		})
	}

	for _, v := range collVars {
		if !v.Enabled || seen[v.Key] {
			continue // disabled, or env already owns this key (env wins)
		}
		configured = append(configured, varView{
			Key:    v.Key,
			Value:  v.Value,
			Secret: v.Secret,
			Scope:  scopeCollection,
		})
	}

	for k, val := range runtime {
		runtimeRows = append(runtimeRows, varView{
			Key:   k,
			Value: val,
			Scope: scopeRuntime,
		})
	}
	sort.Slice(runtimeRows, func(i, j int) bool {
		return runtimeRows[i].Key < runtimeRows[j].Key
	})

	return configured, runtimeRows
}

// varRowDisplay returns a row's display strings: the key as-is, and the DISPLAY
// value — secretMask for a Secret row (its clear Value MUST NOT appear on screen,
// privacy requirement), otherwise the verbatim Value. This is the single
// Fyne-free source of truth for what the user SEES; the real Value (the copy
// source on double-click) is taken straight from varView.Value, never from here.
func varRowDisplay(v varView) (key, value string) {
	value = v.Value
	if v.Secret {
		value = secretMask
	}
	return v.Key, value
}

// renderVarLine returns the one-line display text for a row, "Key = Value". For
// a Secret row the value is MASKED as secretMask ("Key = ••••") — the secret's
// clear Value MUST NOT appear in the output (privacy requirement). Pure and
// Fyne-free so the panel widgets and the blind tester share one source of truth;
// it is built on varRowDisplay so the masking rule lives in exactly one place.
func renderVarLine(v varView) string {
	key, value := varRowDisplay(v)
	return key + " = " + value
}

// ---- Panel UI (read-only) ----

// varsPanel is the read-only Variables inspector: a scrollable column with an
// Environment section (the configured env+collection variables, secrets masked)
// and a Tests (runtime) section (session-captured values). It owns only display
// state; refresh() re-reads the Window and rebuilds the rows.
type varsPanel struct {
	win       *Window
	container fyne.CanvasObject

	envSubheader *widget.Label   // "Environment · <name>" / "No active environment"
	envRows      *fyne.Container // configured rows live here
	runtimeRows  *fyne.Container // runtime rows live here
}

// newVarsPanel builds the panel UI and stores it in .container. It is read-only
// (labels, not entries) and reflects the Window's current state on construction;
// callers re-sync it via refresh() whenever the env/collection/runtime change.
func newVarsPanel(w *Window) *varsPanel {
	p := &varsPanel{win: w}

	header := widget.NewLabelWithStyle("Variables", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	p.envSubheader = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	p.envRows = container.NewVBox()

	runtimeSubheader := widget.NewLabelWithStyle("Tests (runtime)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	p.runtimeRows = container.NewVBox()

	body := container.NewVBox(
		header,
		p.envSubheader,
		p.envRows,
		widget.NewSeparator(),
		runtimeSubheader,
		p.runtimeRows,
	)
	// Bi-directional scroll (not vertical-only): a long value — e.g. a captured
	// JWT token — must scroll horizontally inside the panel rather than letting
	// the row's wide MinSize drive the panel (and the window) wider with no way
	// to shrink back (issue #36, same class as #32).
	p.container = container.NewScroll(body)

	p.refresh()
	return p
}

// refresh re-reads the Window's active environment, collection variables, and
// session runtime captures, recomputes the rows via collectVariableView, and
// rebuilds the displayed labels. Safe to call anytime (including before the
// Window has any environment — it falls back to the "no active environment"
// state and an empty runtime section).
func (p *varsPanel) refresh() {
	if p.container == nil {
		return // not built yet
	}

	env, ok := p.win.activeEnv()
	configured, runtimeRows := collectVariableView(env, p.win.coll.Variables, p.win.runtimeVars)

	if ok {
		p.envSubheader.SetText("Environment · " + env.Name)
	} else {
		p.envSubheader.SetText("No active environment")
	}

	// Rebuild the configured (env + collection) rows.
	p.envRows.RemoveAll()
	if len(configured) == 0 {
		p.envRows.Add(mutedText("No variables."))
	} else {
		for _, v := range configured {
			p.envRows.Add(varLineWidget(v, p.copyValue))
		}
	}
	p.envRows.Refresh()

	// Rebuild the runtime rows.
	p.runtimeRows.RemoveAll()
	if len(runtimeRows) == 0 {
		p.runtimeRows.Add(mutedText("No captured values yet — send a request with a Capture."))
	} else {
		for _, v := range runtimeRows {
			p.runtimeRows.Add(varLineWidget(v, p.copyValue))
		}
	}
	p.runtimeRows.Refresh()
}

// copyValue copies a variable's REAL value to the clipboard and flashes a brief
// "Copied <key>" confirmation in the footer status bar. Double-clicking a row is
// the explicit "give me the value" action, so even a Secret copies its CLEAR
// Value (v.Value) — masking is only to stop shoulder-surfing the on-screen value,
// never to block the deliberate copy. Passed into each row widget so the row
// needs no direct Window/clipboard reference of its own; safe before the bar is
// built (flashStatus no-ops). A nil app clipboard is tolerated (test/headless).
func (p *varsPanel) copyValue(v varView) {
	if app := fyne.CurrentApp(); app != nil {
		app.Clipboard().SetContent(v.Value)
	}
	if p.win != nil {
		p.win.flashStatus("Copied " + v.Key)
	}
}

// varRowWidget is the read-only Variables row widget: it renders "[KEY] : [VALUE]" —
// the key accented/bold, a muted ":" separator, then the display value (masked
// for secrets, de-emphasised for collection-scoped rows) — and is double-tappable
// (fyne.DoubleTappable). A double-click copies the row's REAL value via the
// onCopy callback (see varsPanel.copyValue). It is purely a display row: it takes
// no keyboard focus and has no single-tap behaviour, only the double-tap copy.
type varRowWidget struct {
	widget.BaseWidget
	v      varView
	onCopy func(varView)
}

// newVarRowWidget builds the row widget for v, copying its real value via onCopy
// on a double-tap.
func newVarRowWidget(v varView, onCopy func(varView)) *varRowWidget {
	r := &varRowWidget{v: v, onCopy: onCopy}
	r.ExtendBaseWidget(r)
	return r
}

// CreateRenderer lays the row out as key : value. The key is accented + bold; the
// separator and value are muted for a collection-scoped row, and the value column
// shows the DISPLAY value (secretMask for secrets) — the clear secret never
// appears on screen. Built from canvas.Text so it stays a non-interactive,
// read-only look (no Entry chrome) while the widget itself handles the double-tap.
func (r *varRowWidget) CreateRenderer() fyne.WidgetRenderer {
	_, value := varRowDisplay(r.v)

	key := canvas.NewText(r.v.Key, theme.Color(theme.ColorNamePrimary))
	key.TextStyle = fyne.TextStyle{Bold: true}
	key.TextSize = theme.TextSize()

	sep := canvas.NewText(":", theme.Color(theme.ColorNamePlaceHolder))
	sep.TextSize = theme.TextSize()

	// A collection-scoped row is de-emphasised (placeholder colour) to set it apart
	// from the higher-precedence env variables; other scopes use the normal text.
	valColor := theme.Color(theme.ColorNameForeground)
	if r.v.Scope == scopeCollection {
		valColor = theme.Color(theme.ColorNamePlaceHolder)
	}
	val := canvas.NewText(value, valColor)
	val.TextSize = theme.TextSize()

	row := container.NewHBox(key, sep, val)
	return widget.NewSimpleRenderer(row)
}

// DoubleTapped copies the row's REAL value (fyne.DoubleTappable). Even for a
// Secret row this hands over the clear Value — the double-click is the explicit
// reveal-and-copy gesture; the on-screen mask only guards passive viewing.
func (r *varRowWidget) DoubleTapped(*fyne.PointEvent) {
	if r.onCopy != nil {
		r.onCopy(r.v)
	}
}

// varLineWidget builds the read-only "[KEY] : [VALUE]" row widget for one row,
// double-tappable to copy that variable's value via onCopy. Secrets are masked on
// screen identically to renderVarLine (both go through varRowDisplay); a
// collection-scoped row is de-emphasised to set it apart from env variables.
func varLineWidget(v varView, onCopy func(varView)) fyne.CanvasObject {
	return newVarRowWidget(v, onCopy)
}

// mutedText returns a non-interactive, de-emphasised line using the placeholder
// theme colour (the codebase idiom for muted read-only text). Used for the
// empty-state rows ("No variables.", "No captured values yet…").
func mutedText(s string) fyne.CanvasObject {
	t := canvas.NewText(s, theme.Color(theme.ColorNamePlaceHolder))
	t.TextSize = theme.TextSize()
	return t
}
