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

// renderVarLine returns the one-line display text for a row, "Key = Value". For
// a Secret row the value is MASKED as secretMask ("Key = ••••") — the secret's
// clear Value MUST NOT appear in the output (privacy requirement). Pure and
// Fyne-free so the panel widgets and the blind tester share one source of truth.
func renderVarLine(v varView) string {
	value := v.Value
	if v.Secret {
		value = secretMask
	}
	return v.Key + " = " + value
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
	p.container = container.NewVScroll(body)

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
			p.envRows.Add(varLineWidget(v))
		}
	}
	p.envRows.Refresh()

	// Rebuild the runtime rows.
	p.runtimeRows.RemoveAll()
	if len(runtimeRows) == 0 {
		p.runtimeRows.Add(mutedText("No captured values yet — send a request with a Capture."))
	} else {
		for _, v := range runtimeRows {
			p.runtimeRows.Add(varLineWidget(v))
		}
	}
	p.runtimeRows.Refresh()
}

// varLineWidget builds the read-only label for one row, using renderVarLine for
// the text (so secrets are masked identically to the pure helper). A
// collection-scoped row is de-emphasised (placeholder colour) to set it apart
// from the higher-precedence env variables.
func varLineWidget(v varView) fyne.CanvasObject {
	line := renderVarLine(v)
	if v.Scope == scopeCollection {
		return mutedText(line)
	}
	return widget.NewLabel(line)
}

// mutedText returns a non-interactive, de-emphasised line using the placeholder
// theme colour (the codebase idiom for muted read-only text).
func mutedText(s string) fyne.CanvasObject {
	t := canvas.NewText(s, theme.Color(theme.ColorNamePlaceHolder))
	t.TextSize = theme.TextSize()
	return t
}
