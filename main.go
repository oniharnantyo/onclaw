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
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/oniharnantyo/buddies/internal/cli"
	"github.com/oniharnantyo/buddies/internal/desktop"
	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/notifications"
	"github.com/oniharnantyo/buddies/internal/services"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	wailsnotifications "github.com/wailsapp/wails/v3/pkg/services/notifications"
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
// generator produces their frontend definitions. The per-session event names
// (SessionEventName) are dynamic and intentionally unregistered; the
// per-group event name (GroupEventName) is dynamic the same way, and its
// payload registers under the documented "<id>" placeholder so the generator
// emits the GroupEvent type for the frontend.
func init() {
	application.RegisterEvent[services.GatewayStateEvent](services.EventGatewayState)
	application.RegisterEvent[services.MCPOAuthCompletedEvent](services.EventMCPOAuthCompleted)
	application.RegisterEvent[services.SchedulesChangedEvent](services.EventSchedulesChanged)
	application.RegisterEvent[services.KnowledgeChangedEvent](services.EventKnowledgeChanged)
	application.RegisterEvent[services.TurnsChangedEvent](services.EventTurnsChanged)
	application.RegisterEvent[services.ChannelChangedEvent](services.EventChannelChanged)
	application.RegisterEvent[services.ChannelStateEvent](services.EventChannelState)
	application.RegisterEvent[services.GroupsChangedEvent](services.EventGroupsChanged)
	application.RegisterEvent[services.GroupEvent]("group:<id>:event")
	application.RegisterEvent[services.NotificationOpenEvent](services.EventNotificationOpen)
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

// wailsNotifier implements cli.Notifier over Wails' notifications service
// (add-desktop-notifications design D1/D6): lazy one-time authorization
// immediately before the first send, delivery on the host's own goroutine
// (the macOS authorization request blocks on the OS prompt, and the seam
// contract requires prompt returns), and click-to-open through the
// notification response callback. The engine's route resolver and the main
// window handle are late-bound — the engine is built inside cli.Assemble and
// the window after it — mirroring the scheduleRuns fields-bound-after-
// construction precedent; both bind before the application runs, which is
// the earliest a send or a click can arrive.
type wailsNotifier struct {
	svc     *wailsnotifications.NotificationService
	emitter *wailsEmitter

	// authorized latches the one-time lazy authorization (design D6): the
	// first send requests it — no prompt at startup — and a denial or error
	// leaves it false, dropping every send at debug from then on (no
	// re-prompting; the OS remembers the answer anyway).
	authorized atomic.Bool
	authOnce   sync.Once

	// resolveRoute maps a delivered notification's deterministic id back to
	// its route (design D5); showWindow is the tray-click window reveal.
	// Nil until bound, which only drops clicks that cannot resolve anyway.
	resolveRoute atomic.Pointer[func(string) (notifications.Route, bool)]
	showWindow   atomic.Pointer[func()]

	// window is the main window handle behind the engine's visibility query
	// (design D3); nil until the window exists, which reads as hidden —
	// suppression requires positive knowledge, so nothing is silenced.
	window atomic.Pointer[application.WebviewWindow]
}

// newWailsNotifier constructs the desktop notifier over the Wails
// notifications service singleton and registers the click-response callback.
func newWailsNotifier(emitter *wailsEmitter) *wailsNotifier {
	n := &wailsNotifier{svc: wailsnotifications.New(), emitter: emitter}
	n.svc.OnNotificationResponse(n.handleResponse)
	return n
}

// Send hands one built payload to the native service. The seam contract
// requires prompt returns — the one-time authorization and the native send
// block on OS surfaces — so the delivery runs on its own goroutine and Send
// only rejects payloads the service would refuse outright.
func (n *wailsNotifier) Send(p notifications.Payload) error {
	if p.ID == "" || p.Title == "" {
		return fmt.Errorf("notification payload missing id or title")
	}
	go n.deliver(p)
	return nil
}

// deliver performs the native send off the trigger source's goroutine: the
// one-time lazy authorization (design D6), then the send with
// ThreadID = conversation id so bursts stack per conversation. Every
// failure — denial, error, panic — logs at debug and drops; delivery is
// never propagated as a turn failure.
func (n *wailsNotifier) deliver(p notifications.Payload) {
	defer func() {
		if r := recover(); r != nil {
			slog.Debug("notifications: delivery panicked", "id", p.ID, "panic", r)
		}
	}()
	n.authOnce.Do(func() {
		ok, err := n.svc.RequestNotificationAuthorization()
		switch {
		case err != nil:
			slog.Debug("notifications: authorization request failed", "error", err)
		case !ok:
			slog.Debug("notifications: authorization denied")
		default:
			n.authorized.Store(true)
		}
	})
	if !n.authorized.Load() {
		return
	}
	if err := n.svc.SendNotification(wailsnotifications.NotificationOptions{
		ID:       p.ID,
		Title:    p.Title,
		Body:     p.Body,
		ThreadID: p.ThreadID,
	}); err != nil {
		slog.Debug("notifications: send dropped", "id", p.ID, "error", err)
	}
}

// handleResponse resolves one notification click (design D5): an error
// response or an unknown id — pruned, suppressed, or a failed delivery —
// drops; a known one reveals the main window, the same call the tray's
// OnClick uses, and emits the open event the frontend navigates on. Kind is
// the route kind's string form ("session"/"group"), the event payload's json
// contract.
func (n *wailsNotifier) handleResponse(result wailsnotifications.NotificationResult) {
	defer func() {
		if r := recover(); r != nil {
			slog.Debug("notifications: click handling panicked", "panic", r)
		}
	}()
	if result.Error != nil {
		slog.Debug("notifications: click response error", "error", result.Error)
		return
	}
	id := result.Response.ID
	resolve := n.resolveRoute.Load()
	if resolve == nil || id == "" {
		return
	}
	route, ok := (*resolve)(id)
	if !ok {
		return
	}
	if show := n.showWindow.Load(); show != nil {
		(*show)()
	}
	n.emitter.Emit(services.EventNotificationOpen, services.NotificationOpenEvent{
		Kind: string(route.Kind),
		ID:   route.ID,
	})
}

// setRouteResolver late-binds the engine's route lookup (design D5); called
// once after cli.Assemble returns.
func (n *wailsNotifier) setRouteResolver(resolve func(string) (notifications.Route, bool)) {
	n.resolveRoute.Store(&resolve)
}

// bindWindow installs the main window handle for the click reveal and the
// engine's visibility query; called once after the window is created.
func (n *wailsNotifier) bindWindow(window *application.WebviewWindow) {
	n.window.Store(window)
	show := func() { window.Show() }
	n.showWindow.Store(&show)
}

// WindowVisible answers the engine's suppression query (design D3) with the
// main window's native visibility state, queried fresh per call — Wails'
// WebviewWindow.IsVisible dispatches to the window's live implementation on
// every call (no cached flag). A not-yet-created or destroyed window reads
// as hidden, which delivers.
func (n *wailsNotifier) WindowVisible() bool {
	w := n.window.Load()
	if w == nil {
		return false
	}
	return w.IsVisible()
}

func main() {
	emitter := &wailsEmitter{}
	opener := &wailsURLOpener{}
	notifier := newWailsNotifier(emitter)
	cmd := cli.NewRootCommand(emitter, opener, func(ctx context.Context, app *cli.App) error {
		return runDesktop(ctx, app, emitter, opener, notifier)
	}, notifier)
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

// Measured window floor (design D5); the arithmetic lives with the window
// options below, where the floor is enforced, and applySavedFrame clamps
// restores to it.
const (
	minWindowWidth  = 1040
	minWindowHeight = 600
)

// runDesktop builds the Wails app: bound services, the main window
// (hide-on-close), and the system tray. It returns when the application
// quits; the assembled app is closed by the CLI action's defer.
func runDesktop(ctx context.Context, app *cli.App, emitter *wailsEmitter, opener *wailsURLOpener, notifier *wailsNotifier) error {
	// The click-to-open resolver binds after Assemble returns (design D5):
	// the engine is built inside Assemble, so the notification id → route
	// lookup cannot exist earlier.
	if app.NotificationsEngine != nil {
		notifier.setRouteResolver(app.NotificationsEngine.ResolveRoute)
	}

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
			application.NewService(app.Services.Groups),
			application.NewService(app.Services.Settings),
			application.NewService(app.Services.Gateway),
			application.NewService(app.Services.Schedule),
			application.NewService(app.Services.Knowledge),
			application.NewService(app.Services.Channels),
			// The notifications preferences service (add-desktop-notifications
			// design D5) and the Wails notifications service itself (its
			// platform startup installs the UNUserNotificationCenter delegate
			// and the click-response plumbing).
			application.NewService(app.Services.Notifications),
			application.NewService(notifier.svc),
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
		// Window floor (design D5), measured rather than guessed. The
		// narrowest supported layout is the nav rail collapsed (80px, w-20)
		// + the sessions column at its minimum drag width (176px,
		// MIN_RAIL_WIDTH in SessionRail.tsx) + the right bar at its minimum
		// (220px, MIN_SIDEBAR_WIDTH in RightBar.tsx, plus its m-2 margins
		// and border, 238px) + a usable composer: the pre-distill composer
		// rail already wrapped at a ~1440px window (default columns
		// 224+224+280 leave ~667px of conversation column) and the distill
		// removes ~150px of resting chrome (design D4), so the distilled
		// rail bottoms out at ~517px. The remaining shell chrome (p-2
		// padding, gap-2, island border) adds ~26px:
		// 80 + 177 + 238 + 517 + 26 ≈ 1038 → 1040. Below xl (1280px) the
		// right bar overlays instead of docking, which only frees width, so
		// the floor holds there too. Height has no measured wrap point;
		// 600 keeps the header + thread + composer stack usable.
		MinWidth:  minWindowWidth,
		MinHeight: minWindowHeight,
		// A cold launch has nothing to restore, and the platform default
		// (Wails' 800x600) sits below the floor — MinWidth/MinHeight only
		// constrain later resizes, they do not resize the window at birth.
		// Start at the floor; applySavedFrame overrides this when a frame
		// was stored.
		Width:  minWindowWidth,
		Height: minWindowHeight,
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
	// The main window handle feeds the click-to-open reveal and the engine's
	// visibility query (add-desktop-notifications design D3/D5).
	notifier.bindWindow(window)

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
	// The floor (D5) bounds restores as well: frames persisted before the
	// floor existed must come back at the floor, never smaller — otherwise a
	// relaunch after an upgrade could reopen an unusably narrow window.
	frame.Width = max(frame.Width, minWindowWidth)
	frame.Height = max(frame.Height, minWindowHeight)
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
