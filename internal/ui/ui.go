// Package ui is the desktop shell: a tray icon and a small window showing the
// dashboard. The dashboard is served in-process through Wails' asset handler,
// so it is not reachable from the network.
package ui

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"outlook-render/internal/icon"
)

type Options struct {
	Dashboard   http.Handler
	Minimized   bool          // start with the window hidden (used when starting at login)
	Tooltip     func() string // tray tooltip, refreshed every few seconds
	CurlExample func() string
	Log         *slog.Logger
}

// AutostartArgs are passed to the exe when Windows starts it at login.
var AutostartArgs = []string{"--minimized"}

// Run shows the tray icon and window and blocks until the user quits.
func Run(o Options) error {
	var window *application.WebviewWindow
	show := func() {
		if window != nil {
			window.Show()
			window.Focus()
		}
	}

	app := application.New(application.Options{
		Name:        "outlook-render",
		Description: "Renders emails in classic Outlook and returns screenshots",
		Icon:        icon.PNG(256),
		Logger:      o.Log,
		Assets:      application.AssetOptions{Handler: o.Dashboard, DisableLogging: true},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               "outlook-render.app",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { show() },
		},
		Mac: application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
	})

	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "dashboard",
		Title:     "outlook-render",
		Width:     860,
		Height:    760,
		MinWidth:  520,
		MinHeight: 420,
		URL:       "/",
		Hidden:    o.Minimized,
	})
	// Closing the window hides it; the app keeps running in the tray.
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})

	tray := app.SystemTray.New()
	tray.SetIcon(icon.PNG(32))
	tray.SetTooltip("outlook-render")
	tray.OnClick(show)

	menu := app.NewMenu()
	menu.Add("Open dashboard").OnClick(func(*application.Context) { show() })
	menu.Add("Copy curl example").OnClick(func(*application.Context) {
		app.Clipboard.SetText(o.CurlExample())
	})
	menu.AddSeparator()
	enabled, _ := app.Autostart.IsEnabled()
	menu.AddCheckbox("Start at login", enabled).OnClick(func(ctx *application.Context) {
		if err := (Autostart{}).Set(ctx.IsChecked()); err != nil {
			o.Log.Error("changing start at login failed", "err", err)
		}
	})
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)

	go func() {
		for {
			time.Sleep(3 * time.Second)
			tray.SetTooltip(o.Tooltip())
		}
	}()

	return app.Run()
}

// CopyText puts text on the clipboard. Safe to call once Run has started.
func CopyText(s string) bool {
	app := application.Get()
	return app != nil && app.Clipboard.SetText(s)
}

// Autostart implements dashboard.Autostart through Wails (on Windows: the
// HKCU\...\Run registry key).
type Autostart struct{}

func (Autostart) Enabled() (bool, error) {
	app := application.Get()
	if app == nil {
		return false, nil
	}
	return app.Autostart.IsEnabled()
}

func (Autostart) Set(on bool) error {
	app := application.Get()
	if app == nil {
		return nil
	}
	if on {
		return app.Autostart.EnableWithOptions(application.AutostartOptions{Arguments: AutostartArgs})
	}
	return app.Autostart.Disable()
}
