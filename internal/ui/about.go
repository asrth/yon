package ui

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// repoURL is Yon's source home, linked from the About box.
const repoURL = "https://github.com/asrth/yon"

// showAboutDialog presents a small "About Yon" card: the app icon, name, running
// version, slogan, a one-line description, a link to the source, and the licence.
func (a *App) showAboutDialog(parent fyne.Window) {
	logo := canvas.NewImageFromResource(appIcon)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(96, 96))

	title := widget.NewLabelWithStyle("Yon", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	thaiName := widget.NewLabelWithStyle("(โยน)", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})

	versionText := "Development build"
	if v := currentVersion(); v != "" {
		versionText = "Version " + v
	}
	version := widget.NewLabelWithStyle(versionText, fyne.TextAlignCenter, fyne.TextStyle{})

	slogan := widget.NewLabelWithStyle("Throw a request. Catch a response.",
		fyne.TextAlignCenter, fyne.TextStyle{Italic: true})

	desc := widget.NewLabelWithStyle(
		"A fast, lightweight, open-source desktop client for testing HTTP APIs.\nNo account, no cloud, no telemetry — your work stays in plain files.",
		fyne.TextAlignCenter, fyne.TextStyle{})

	var link fyne.CanvasObject
	if u, err := url.Parse(repoURL); err == nil {
		link = widget.NewHyperlink("github.com/asrth/yon", u)
	} else {
		link = widget.NewLabel(repoURL)
	}

	license := widget.NewLabelWithStyle("MIT License", fyne.TextAlignCenter, fyne.TextStyle{})

	poweredBy := widget.NewLabelWithStyle(
		"Powered by ADVANCED SOFTWARE AND ROBOTICS CO., LTD.",
		fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	content := container.NewVBox(
		container.NewCenter(logo),
		title,
		thaiName,
		version,
		slogan,
		widget.NewSeparator(),
		desc,
		container.NewCenter(link),
		license,
		widget.NewSeparator(),
		poweredBy,
	)

	dialog.ShowCustom("About Yon", "Close", content, parent)
}
