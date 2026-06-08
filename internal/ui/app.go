// Package ui is Yon's Fyne front end: the only package besides main that
// imports Fyne (the UI-free-core rule). It renders Collections as windows, Requests as
// DocTabs, and Responses in a read-only TextGrid (the read-only-response rule), translating
// between Fyne widgets and the pure model/yonner/store packages.
package ui

import (
	"fmt"
	"os"

	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"

	"github.com/ultramcu/yon/internal/model"
	"github.com/ultramcu/yon/internal/oauth"
	"github.com/ultramcu/yon/internal/store"
	"github.com/ultramcu/yon/internal/tunnel"
)

// AppID is the Fyne application identifier; it scopes Preferences storage
// (session + settings) and must match the ID passed to app.NewWithID.
const AppID = "th.co.asr.yon"

// App is the top-level UI controller. It owns the Fyne app, the live set of
// Collection windows, and the persisted Settings, and coordinates session
// save/restore.
type App struct {
	fyneApp fyne.App

	settings Settings

	// windows holds every open Collection window keyed by its *Window so we can
	// enumerate them for session save and remove them on close.
	windows map[*Window]struct{}

	// tunnels is the app-wide SSH jump-host manager (Phase 1 core). One Tunnel
	// per distinct jump-host identity, shared and refcounted across windows.
	tunnels *tunnel.Manager

	// tunnelMu guards tunnelRelease, which is read/written from the UI goroutine
	// on env switches and window close. The map keeps each window's current
	// jump-host refcount handle: rebindTunnel/forgetWindow call its release func
	// to drop the previous binding.
	tunnelMu      sync.Mutex
	tunnelRelease map[*Window]func()

	// oauth is the app-wide OAuth 2.0 token manager (issue #30): it caches
	// access tokens per OAuth2 config for the session and refreshes them, shared
	// across windows so a token obtained in one place serves every send.
	oauth *oauth.Manager
}

// New constructs the UI controller around an already-created fyne.App (created
// in main with app.NewWithID(AppID)). It loads persisted Settings immediately
// so the first send already honours the user's timeout / TLS choices.
func New(a fyne.App) *App {
	a.SetIcon(appIcon)
	app := &App{
		fyneApp:       a,
		settings:      loadSettings(a.Preferences()),
		windows:       make(map[*Window]struct{}),
		tunnelRelease: make(map[*Window]func()),
	}
	// The jump-host manager wires the UI's TOFU prompt for unknown host keys;
	// without it the manager rejects first-time hosts (the headless default).
	app.tunnels = tunnel.New(tunnel.WithTOFU(app.tunnelTOFUPrompt))
	app.oauth = oauth.NewManager()
	app.applyTheme() // apply the saved appearance theme before any window is built
	return app
}

// applyTheme sets the Fyne app theme from the current Settings (Dark Pro / Warm
// / System). Called at startup and whenever the user changes it in Settings.
func (a *App) applyTheme() {
	a.fyneApp.Settings().SetTheme(themeByID(a.settings.ThemeID))
}

// Run starts the Fyne event loop after deciding what to open. If one or more
// .yon file paths are given (e.g. from the command line), those Collections are
// opened — one window each — and the saved Session is NOT restored (the explicit
// files take precedence). With no file arguments, Run restores the previous
// Session, or opens one fresh untitled Collection if there is none. An OnStopped
// hook saves the Session on shutdown. Run blocks until the app quits.
func (a *App) Run(files ...string) {
	a.fyneApp.Lifecycle().SetOnStopped(func() {
		a.saveSession()
		a.tunnels.Close() // tear down every live Tunnel on quit
	})

	// macOS delivers Finder double-clicks as an "open document" Apple Event
	// rather than on the command line, so install a handler for it. On
	// Windows/Linux this is a no-op (those arrive via files above). The event
	// fires on the Cocoa main thread, so hop onto the Fyne loop.
	//
	// It is registered twice: once here (best effort for a cold launch, where
	// the event may arrive before the loop is up) and again from OnStarted,
	// because AppKit overwrites the handler with its own default during launch —
	// the OnStarted registration is the one that ultimately sticks.
	openHandler := func(path string) { fyne.Do(func() { a.openFromOS(path) }) }
	registerOpenFilesHandler(openHandler)
	a.fyneApp.Lifecycle().SetOnStarted(func() {
		registerOpenFilesHandler(openHandler)
	})

	if len(files) > 0 {
		opened := 0
		for _, f := range files {
			if err := a.OpenPath(f); err != nil {
				fmt.Fprintf(os.Stderr, "Yon: cannot open %q: %v\n", f, err)
				continue
			}
			opened++
		}
		if opened == 0 {
			a.NewCollectionWindow()
		}
	} else if !a.restoreSession() {
		a.NewCollectionWindow()
	}

	if a.settings.CheckUpdatesOnLaunch {
		a.autoCheckUpdates()
	}

	a.fyneApp.Run()
}

// autoCheckUpdates runs a one-shot background update check against any open
// window's banner (one notice is enough). Called only when the user has opted in
// via Settings.
func (a *App) autoCheckUpdates() {
	for w := range a.windows {
		w.checkForUpdates(false)
		return
	}
}

// OpenPath loads a Collection from a .yon file and opens it in its own window.
// It returns an error if the file cannot be read or is not a valid .yon file
// (callers decide whether to surface it via a dialog or stderr).
func (a *App) OpenPath(path string) error {
	coll, err := store.Load(path)
	if err != nil {
		return err
	}
	a.OpenCollectionWindow(coll, path)
	a.rememberRecent(path)
	return nil
}

// openFromOS opens a .yon file requested by the operating system (a macOS
// "open document" Apple Event from Finder, `open`, or the Dock). Unlike the
// command-line path in Run, there is no terminal to print to, so a load error
// is surfaced in a dialog on an existing window (falling back to stderr if none
// is open yet). On success the Collection opens in its own window.
func (a *App) openFromOS(path string) {
	if err := a.OpenPath(path); err != nil {
		if w := a.anyWindow(); w != nil {
			dialog.ShowError(err, w.win)
			return
		}
		fmt.Fprintf(os.Stderr, "Yon: cannot open %q: %v\n", path, err)
	}
}

// anyWindow returns one open Collection window, or nil if none are open. Used
// to anchor dialogs that are not tied to a specific window.
func (a *App) anyWindow() *Window {
	for w := range a.windows {
		return w
	}
	return nil
}

// NewCollectionWindow opens a window for a fresh, untitled Collection and shows
// it. Returns the new Window so callers (e.g. session restore) can drive it.
func (a *App) NewCollectionWindow() *Window {
	coll := model.NewCollection("")
	w := newWindow(a, coll, "")
	a.windows[w] = struct{}{}
	w.win.Show()
	return w
}

// OpenCollectionWindow opens a window for a Collection already loaded from disk
// at path. Returns the Window.
func (a *App) OpenCollectionWindow(coll model.Collection, path string) *Window {
	w := newWindow(a, coll, path)
	a.windows[w] = struct{}{}
	w.win.Show()
	return w
}

// forgetWindow removes a closed window from the live set so it is not included
// in the next session save.
func (a *App) forgetWindow(w *Window) {
	delete(a.windows, w)

	// Drop the window's jump-host refcount so closing it tears down a Tunnel that
	// no other window still holds.
	a.tunnelMu.Lock()
	if release := a.tunnelRelease[w]; release != nil {
		release()
		delete(a.tunnelRelease, w)
	}
	a.tunnelMu.Unlock()
}

// prefs is a small accessor used across the ui package.
func (a *App) prefs() fyne.Preferences { return a.fyneApp.Preferences() }
