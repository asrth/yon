// Command yon is a minimal, cross-platform desktop GUI for testing HTTP APIs:
// a lightweight, fast, offline, open-source client built in Go + Fyne.
//
// main wires the Fyne app to the ui controller and nothing else: per the UI-free-core rule
// only main and internal/ui import Fyne. The ui controller restores the previous
// Session (open Collections + their Tabs) on launch and saves it on shutdown.
package main

import (
	_ "embed"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/ultramcu/yon/internal/ui"
)

// fyneAppTOML is the app metadata, embedded so every build knows its own version.
// Fyne only fills app.Metadata() under `fyne package`, and the macOS release ships
// a plain `go build` binary, so without this the in-app update check would see
// Fyne's "0.0.1" placeholder and report itself as a development build.
//
//go:embed FyneApp.toml
var fyneAppTOML string

func main() {
	ui.SetBuildVersion(tomlVersion(fyneAppTOML))
	// Set the human-friendly name so "Yon" shows everywhere (e.g. the macOS menu
	// bar) even for a plain `go build`/`go run`, not just the packaged release —
	// otherwise Fyne falls back to the executable name ("yon").
	app.SetMetadata(fyne.AppMetadata{
		ID:      ui.AppID,
		Name:    "Yon",
		Version: tomlVersion(fyneAppTOML),
	})
	a := app.NewWithID(ui.AppID)
	// Any .yon file paths passed on the command line are opened on launch
	// (and take precedence over restoring the previous session).
	ui.New(a).Run(os.Args[1:]...)
}

// tomlVersion extracts the Version value from a FyneApp.toml's [Details] table,
// e.g. `  Version = "0.9.0"` -> "0.9.0". Returns "" if not found.
func tomlVersion(toml string) string {
	for _, line := range strings.Split(toml, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "Version") {
			continue
		}
		if i := strings.IndexByte(line, '"'); i >= 0 {
			if j := strings.IndexByte(line[i+1:], '"'); j >= 0 {
				return line[i+1 : i+1+j]
			}
		}
	}
	return ""
}
