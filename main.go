//go:build !server

// buddies desktop entrypoint (design D4): assembles the application through
// internal/cli, binds every service to the Wails app, and hosts the window
// plus the system tray. Closing the window hides it; the tray's Quit is the
// only full exit (spec: desktop background lifecycle).
package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/oniharnantyo/buddies/internal/cli"
	"github.com/oniharnantyo/buddies/internal/desktop"
	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/services"
)

// Wails uses Go's `embed` package to embed the frontend files into the
// binary. Any files in the frontend/dist folder will be embedded into the
// binary and made available to the frontend.
//
//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

// The darwin tray wants a monochrome template glyph (it follows light/dark
// appearance); the colored app icon serves every other platform.
//
//go:embed build/appicon-tray.png
var trayIcon []byte

// init registers the typed custom events the services emit so the binding
// generator produces their frontend definitions (the per-session event names
// are dynamic and intentionally unregistered).
func init() {
	application.RegisterEvent[services.GatewayStateEvent](services.EventGatewayState)
	application.RegisterEvent[services.MCPOAuthCompletedEvent](services.EventMCPOAuthCompleted)
	application.RegisterEvent[services.SchedulesChangedEvent](services.EventSchedulesChanged)
	application.RegisterEvent[services.KnowledgeChangedEvent](services.EventKnowledgeChanged)
}

// wailsEmitter adapts services.Emitter onto the Wails event bus. The app
// handle is installed right after application.New; events emitted before
// that are dropped (nothing subscribes before Run, and the frontend reads
// initial state through the services).
type wailsEmitter struct {
	app atomic.Pointer[application.App]
}

func (e *wailsEmitter) Emit(name string, data ...any) {
	if a := e.app.Load(); a != nil {
		a.Event.Emit(name, data...)
	}
}

// wailsURLOpener adapts services.URLOpener onto Wails' browser manager (the
// MCP OAuth authorization page; design D8). The app handle is installed
// right after application.New, mirroring wailsEmitter; opening before that
// fails the flow — nothing can open a browser without the application.
type wailsURLOpener struct {
	app atomic.Pointer[application.App]
}

func (o *wailsURLOpener) Open(url string) error {
	a := o.app.Load()
	if a == nil {
		return fmt.Errorf("open %s: application not started", url)
	}
	return a.Browser.OpenURL(url)
}

func main() {
	emitter := &wailsEmitter{}
	opener := &wailsURLOpener{}
	cmd := cli.NewRootCommand(emitter, opener, func(ctx context.Context, app *cli.App) error {
		return runDesktop(ctx, app, emitter, opener)
	})
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

// runDesktop builds the Wails app: bound services, the main window
// (hide-on-close), and the system tray. It returns when the application
// quits; the assembled app is closed by the CLI action's defer.
func runDesktop(ctx context.Context, app *cli.App, emitter *wailsEmitter, opener *wailsURLOpener) error {
	// The single-instance callback fires on the first instance when a second
	// launch arrives — possibly before the window exists. The handle is
	// filled once the window is created; the callback re-shows it (D1).
	var windowHandle atomic.Pointer[application.WebviewWindow]

	windowTheme := NewWindowThemeService()
	wailsApp := application.New(application.Options{
		Name:        services.AppName,
		Description: "A local agent gateway that keeps running in the background",
		// A second launch must surface the running window, not start a
		// second gateway against the same store (D1).
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.oniharnantyo.buddies",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if w := windowHandle.Load(); w != nil {
					w.Show()
				}
			},
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Services: []application.Service{
			application.NewService(app.Services.AppInfo),
			application.NewService(app.Services.Agents),
			application.NewService(app.Services.Chat),
			application.NewService(app.Services.Settings),
			application.NewService(app.Services.Gateway),
			application.NewService(app.Services.Schedule),
			application.NewService(app.Services.Knowledge),
			application.NewService(services.NewClipboardService()),
			application.NewService(windowTheme),
		},
		Mac: application.MacOptions{
			// Closing the window must not terminate the process: the
			// gateway keeps running in the tray.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})
	emitter.app.Store(wailsApp)
	opener.app.Store(wailsApp)

	// Quit (tray, dock, or signal) is the only full exit: stop the gateway
	// and close the store while the application shuts down.
	wailsApp.OnShutdown(func() {
		if err := app.Close(); err != nil {
			log.Printf("shutdown: %v", err)
		}
	})

	windowOptions := application.WebviewWindowOptions{
		Name:  "main",
		Title: services.AppName,
		// The webview is an app shell, not a browser: no right-click
		// context menu (D4).
		DefaultContextMenuDisabled: true,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 25,
			// Window-level Liquid Glass (native NSGlassEffectView on macOS
			// 26+; behind-window NSVisualEffectView vibrancy fallback below
			// that). Its material draws a permanent specular edge highlight
			// around the window — accepted, it is part of the Apple look.
			// The glass follows the system appearance, so the page layers
			// theme-aware -apple-system-glass-material surfaces on top of it
			// (styles.css html[data-glass]) to stay correct in both app
			// themes. The webview must be transparent for any of it to show,
			// which Wails gates behind the private_mac_apis build tag — the
			// default public-API build compiles that call as a no-op
			// (build/darwin/Taskfile.yml sets the tag on every darwin desktop
			// build). See stores/platform.ts and design.md.
			Backdrop:   application.MacBackdropLiquidGlass,
			CornerType: application.MacWindowCornerTypeSquare,
			LiquidGlass: application.MacLiquidGlass{
				Style:    application.LiquidGlassStyleAutomatic,
				Material: application.NSVisualEffectMaterialUnderWindowBackground,
			},
			TitleBar: application.MacTitleBarHiddenInset,
		},
		URL: "/",
	}
	applySavedFrame(app, wailsApp, &windowOptions)
	window := wailsApp.Window.NewWithOptions(windowOptions)
	windowHandle.Store(window)

	// Closing the window hides it; the process keeps running.
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})
	// Frame persistence (D5): move/resize rewrite ~/.buddies/window.json.
	// The file is ~50 bytes, so the writes stay synchronous; a destroyed
	// window reports a zero size, which must not clobber the stored frame.
	persistFrame := func() {
		width, height := window.Size()
		if width <= 0 || height <= 0 {
			return
		}
		x, y := window.Position()
		if err := desktop.Save(windowFramePath(app), desktop.Frame{
			X: x, Y: y, Width: width, Height: height,
		}); err != nil {
			log.Printf("save window frame: %v", err)
		}
	}
	window.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) {
		persistFrame()
	})
	window.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) {
		persistFrame()
	})
	// The appearance service drives the native glass from the app theme.
	windowTheme.setHandle(window.NativeWindow())

	setupTray(wailsApp, window, app)

	setupMenu(wailsApp, window)

	// The gateway starts with the app and keeps running regardless of
	// window visibility (spec: background lifecycle).
	if err := app.Services.Gateway.Start(ctx); err != nil {
		log.Printf("start gateway: %v", err)
	}

	return wailsApp.Run()
}

// applySavedFrame restores the persisted window frame (D5) so the window is
// born in place instead of show-then-moved. Screens are enumerated once the
// platform run loop starts, so the list may be empty here; Clamp passes the
// frame through unchanged then. Best-effort by contract: no stored frame,
// no restore, the platform default placement stands.
func applySavedFrame(app *cli.App, wailsApp *application.App, options *application.WebviewWindowOptions) {
	frame, ok := desktop.Load(windowFramePath(app))
	if !ok {
		return
	}
	screens := wailsApp.Screen.GetAll()
	bounds := make([]desktop.Screen, len(screens))
	for i, s := range screens {
		bounds[i] = desktop.Screen{Bounds: desktop.Rect{
			X:      s.Bounds.X,
			Y:      s.Bounds.Y,
			Width:  s.Bounds.Width,
			Height: s.Bounds.Height,
		}}
	}
	frame = desktop.Clamp(frame, bounds)
	options.Width, options.Height = frame.Width, frame.Height
	options.X, options.Y = frame.X, frame.Y
	// X/Y are only honored with WindowXY; the default (zero) value centers.
	options.InitialPosition = application.WindowXY
}

// windowFramePath keeps the window frame next to the rest of the app state
// (D5); the layout root already exists by the time the desktop runs.
func windowFramePath(app *cli.App) string {
	return filepath.Join(app.Layout.Root, "window.json")
}

// setupMenu installs the application menu (design D5): the app submenu gains
// a "Settings…" entry (⌘,) directly below About, and the standard File, Edit,
// and Window menus keep the native shortcuts; the default Help and View menus
// are dropped. "Settings…" reveals the window and signals the frontend shell
// to navigate to settings.
func setupMenu(wailsApp *application.App, window *application.WebviewWindow) {
	menu := wailsApp.NewMenu()
	appSubmenu := menu.AddSubmenu(services.AppName)
	appSubmenu.AddRole(application.About)
	appSubmenu.Add("Settings…").SetAccelerator("cmdorctrl+,").OnClick(func(*application.Context) {
		window.Show()
		// Untyped signal matching the frontend's SETTINGS_OPEN_EVENT; with
		// no payload it needs no RegisterEvent (contrast init above).
		wailsApp.Event.Emit("settings:open")
	})
	appSubmenu.AddSeparator()
	appSubmenu.AddRole(application.ServicesMenu)
	appSubmenu.AddSeparator()
	appSubmenu.AddRole(application.Hide)
	appSubmenu.AddRole(application.HideOthers)
	appSubmenu.AddRole(application.UnHide)
	appSubmenu.AddSeparator()
	appSubmenu.AddRole(application.Quit)

	// File's Close item calls window.Close(), which the WindowClosing hook
	// above turns into hide-on-close; Edit keeps clipboard/undo alive in
	// inputs.
	menu.AddRole(application.FileMenu)
	menu.AddRole(application.EditMenu)
	menu.AddRole(application.WindowMenu)
	wailsApp.Menu.Set(menu)
}

// setupTray builds the system tray: a template glyph on macOS (appearance
// aware), the app icon elsewhere, an Open item, a dynamic gateway state +
// pause/resume item, and Quit.
func setupTray(wailsApp *application.App, window *application.WebviewWindow, app *cli.App) {
	tray := wailsApp.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayIcon)
	} else {
		tray.SetIcon(appIcon)
	}

	menu := wailsApp.NewMenu()
	menu.Add("Open " + services.AppName).OnClick(func(*application.Context) {
		window.Show()
	})

	gatewayItem := menu.Add(gatewayMenuItemLabel(app.Gateway.State()))
	gatewayItem.OnClick(func(*application.Context) {
		toggleGateway(wailsApp.Context(), app)
	})

	menu.AddSeparator()
	menu.Add("Quit " + services.AppName).OnClick(func(*application.Context) {
		wailsApp.Quit()
	})
	tray.SetMenu(menu)
	tray.OnClick(func() { window.Show() })

	// Reflect gateway transitions in the tray label. The gateway invokes
	// listeners from arbitrary goroutines; the menu item implementation
	// marshals label updates to the main thread.
	app.Gateway.Subscribe(func(_, to gateway.State) {
		gatewayItem.SetLabel(gatewayMenuItemLabel(to))
	})
}

// toggleGateway flips the gateway between running and paused. The stopped
// state has no toggle (the gateway starts with the app and only Quit stops
// it); errors are logged, never fatal.
func toggleGateway(ctx context.Context, app *cli.App) {
	var err error
	switch app.Gateway.State() {
	case gateway.StateRunning:
		err = app.Services.Gateway.Pause(ctx)
	case gateway.StatePaused:
		err = app.Services.Gateway.Resume(ctx)
	}
	if err != nil {
		log.Printf("toggle gateway: %v", err)
	}
}

// gatewayMenuItemLabel renders the tray's gateway entry for the state: the
// label names the action the item performs.
func gatewayMenuItemLabel(state gateway.State) string {
	switch state {
	case gateway.StateRunning:
		return "Gateway: running — pause"
	case gateway.StatePaused:
		return "Gateway: paused — resume"
	default:
		return "Gateway: stopped"
	}
}
