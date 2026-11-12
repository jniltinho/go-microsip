// Package gui is go-microsip's desktop window on Wails v3: the embedded Vue / Vite / Tailwind page
// (frontend/, built into dist/) in the system webview, with App as the service the page calls. It is
// the only package that imports Wails. Nothing here touches a display until Run is called, so the
// command line and the MCP server, which link this package, still run headless.
//
// The page reaches the softphone only through App (service.go), which presents the core's phone
// (internal/core/app); the engine's events reach the page as one Wails event. The page is confined
// (confine.go): a content security policy on every asset, no navigation away from the embedded
// page, no listener for the interface. --self-test proves the window, the call into Go and the
// confinement.
//
// Where the desktop shows tray icons, the close button hides the window to the tray (its icon and
// its menu bring the window back or quit); elsewhere it quits, and the page asks first during a
// call.
package gui

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/jniltinho/go-microsip/internal/core/app"
	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// The build constraint of this package is the window's: see cmd/window.go.

// dist is the built page, written by `make go-frontend` (frontend/ -> internal/gui/dist).
//
//go:embed all:dist
var assets embed.FS

// icon is the application's logo: the window and taskbar icon, and the desktop entry's.
//
//go:embed icon.png
var icon []byte

// appID is the GTK application id: Wayland compositors match it to the .desktop file for the
// window's icon, and the single-instance lock is named after it.
const appID = "io.github.jniltinho.go-microsip"

// handedLink names, in what a second instance hands to the running one, the number of its link.
const handedLink = "dial"

// windowIconName is the name of the application's icon in the icon theme.
const windowIconName = "go-microsip"

// Options are the window's flags.
type Options struct {
	// SafeGPU starts the webview with GPU acceleration off (--safe-gpu, GO_MICROSIP_SAFE_GPU=1).
	SafeGPU bool
	// SelfTest opens the window, waits until the page has called Go, checks that the page is
	// confined, and exits 0. It starts no phone: nothing of the user's is read, written or
	// registered.
	SelfTest bool
	// Trace is a file that receives every event sent to the page and what the page shows
	// (--ui-trace, for tests of the built window); empty writes none.
	Trace string
	// Dial is the user part of a sip: or tel: link the application was started with, already
	// checked; the window asks before it calls it. A running instance gets it instead.
	Dial string
	// Core are the core's options: where the configuration and the database are.
	Core app.Options
	// Audio opens the sound devices; nil when this build has no sound service.
	Audio ports.AudioDevices
	// Notice shows the desktop's notification of a ringing call; nil shows none.
	Notice ports.CallNotice
	// UserAgent is the product name sent to the PBX.
	UserAgent string
}

// errUnguarded: on a platform whose webview can be kept on the embedded page, it could not be.
var errUnguarded = errors.New("the window could not be confined to its own page (the webview's navigation guard could not be installed), so it was closed")

// selfTestTimeout bounds --self-test: the page must report ready by then.
const selfTestTimeout = 60 * time.Second

// Run opens the window and returns when the application quits.
func Run(o Options) error {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		return err
	}
	svc := &App{unavailable: "self-test: the phone engine is not started"}
	if !o.SelfTest { // a self-test leaves nothing behind in the user's profile
		if _, err := installDesktopEntry(icon); err != nil {
			slog.Warn("desktop entry not installed", "error", err) // only the Linux title-bar icon depends on it
		}
	}
	setWindowIconName(windowIconName)
	opts := application.Options{
		Name:        "go-microsip",
		Description: "Compact SIP softphone",
		Icon:        icon,
		LogLevel:    slog.LevelInfo, // never Debug: Wails logs binding arguments there
		Services:    []application.Service{application.NewService(svc)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(dist), Middleware: confine, DisableLogging: true},
		Linux:       application.LinuxOptions{ApplicationID: appID}, // the program name follows it
	}
	win := application.WebviewWindowOptions{
		Title: "go-microsip", Width: 360, Height: 560, MinWidth: 320, MinHeight: 480,
		// The page needs none of these (the web trust boundary): deny them all. Audio never crosses
		// the webview; the engine owns the devices.
		Permissions: map[application.PermissionType]application.Permission{
			application.PermissionMicrophone:    application.PermissionDeny,
			application.PermissionCamera:        application.PermissionDeny,
			application.PermissionGeolocation:   application.PermissionDeny,
			application.PermissionNotifications: application.PermissionDeny,
			application.PermissionClipboardRead: application.PermissionDeny,
		},
	}
	if o.SafeGPU || os.Getenv("GO_MICROSIP_SAFE_GPU") == "1" {
		safeGraphics(&win)
	}
	var main *application.WebviewWindow
	// reveal brings the window back from the tray, the taskbar or behind other windows.
	reveal := func() {
		if main != nil {
			main.Show()
			main.Restore()
			main.Focus()
		}
	}
	if !o.SelfTest {
		opts.SingleInstance = &application.SingleInstanceOptions{
			UniqueID: appID,
			// A second start shows this window; one with a link hands its number over for the
			// page to ask about. It comes from another process of the user and is checked again.
			OnSecondInstanceLaunch: func(second application.SecondInstanceData) {
				reveal()
				if number := second.AdditionalData[handedLink]; number != "" {
					svc.link(number)
				}
			},
		}
	}
	if !o.SelfTest && o.Dial != "" {
		opts.SingleInstance.AdditionalData = map[string]string{handedLink: o.Dial}
		svc.link(o.Dial) // no instance was running: this one asks, once its page listens
	}
	ui := application.New(opts) // a second instance ends here, after waking the first one
	svc.emit = func(ev UIEvent) { ui.Event.Emit(EventName, ev) }
	// Once the application is leaving, the close button no longer hides the window or asks the page.
	var leaving atomic.Bool
	quit := sync.OnceFunc(func() { leaving.Store(true); ui.Quit() }) // asked for more than once
	svc.quit = quit
	// Showing the window is asked of the toolkit's main loop and waited for there, so it is not
	// done by what forwards the engine's events, for the same reason as the tray's menu below.
	svc.attention = func() { go reveal() }
	// The System skin follows the desktop. WebKitGTK in a plain GTK window does not pass the
	// desktop's dark preference on to the page, so the page is told, at once and on every change.
	svc.dark = ui.Env.IsDarkMode
	ui.Event.OnApplicationEvent(events.Common.ThemeChanged, func(*application.ApplicationEvent) { svc.desktopChanged() })
	// The window exists before anything that may want it in sight (an incoming call) starts.
	main = ui.Window.NewWithOptions(win)
	// leave quits through the page's own Quit, which asks before it ends a call. A page that is
	// not there or does not answer cannot ask: the window asks then, in a dialog of its own,
	// whenever an engine is running. What this side knows of a call comes after the fact, so it
	// does not judge whether there is one: only a yes ends the engine.
	var asking atomic.Bool
	leave := func() {
		switch svc.leave() {
		case pageAsked:
			return
		case nothingToEnd:
			svc.end()
			return
		}
		if !asking.CompareAndSwap(false, true) {
			return // the question is already on the screen
		}
		question := ui.Dialog.Question().SetTitle("go-microsip").
			SetMessage("The window is not answering. Quit go-microsip? A call in progress will be ended.")
		yes := question.AddButton("Quit")
		yes.OnClick(func() { asking.Store(false); svc.end() })
		no := question.AddButton("Cancel")
		no.OnClick(func() { asking.Store(false) })
		question.SetDefaultButton(no)
		question.SetCancelButton(no)
		question.Show()
	}
	if !o.SelfTest { // a self-test shows no tray icon and closes like any window
		tray := ui.SystemTray.New()
		tray.SetIcon(icon)
		tray.SetTooltip("go-microsip")
		// The menu: the window, the latest calls (a choice calls the number again), and Quit.
		menu := func() *application.Menu {
			menu := application.NewMenu()
			menu.Add("Show go-microsip").OnClick(func(*application.Context) { reveal() })
			for _, call := range svc.recents() {
				number := call.Number
				// Choosing a call is asking for it: the window comes in sight and the number is
				// dialed; what the engine makes of it (busy, not registered) shows there.
				menu.Add(call.Label).OnClick(func(*application.Context) { reveal(); _ = svc.Dial(number) })
			}
			menu.AddSeparator()
			// The page's own Quit, in sight: it asks before it ends a call.
			menu.Add("Quit").OnClick(func(*application.Context) { reveal(); leave() })
			return menu
		}
		tray.SetMenu(menu())
		// The menu follows the history. It is rebuilt by a goroutine of its own: the toolkit sets
		// a menu on its main loop and waits for it, and what forwards the engine's events must
		// never wait for a loop that has ended (a call that ends while the application leaves).
		// Requests made meanwhile are one request.
		stale := make(chan struct{}, 1)
		go func() {
			for range stale {
				tray.SetMenu(menu())
			}
		}()
		// Before anything forwards events.
		svc.recentChanged = func() {
			select {
			case stale <- struct{}{}:
			default: // a rebuild is already asked for
			}
		}
		tray.OnClick(reveal)
		main.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			if leaving.Load() {
				return
			}
			e.Cancel() // the toolkit's own listener would destroy the window
			if trayHost() {
				main.Hide() // calls go on; the tray icon brings the window back
				return
			}
			leave() // no tray to come back from: closing is quitting
		})
	}
	if !o.SelfTest {
		// The phone holds the instance lock of the database until the window is gone: calls are
		// ended and the account unregistered, within the engine's bound, before Run returns.
		// The engine's warnings go to the standard error (the desktop's journal): why a registration
		// or a call failed. It never logs a secret.
		warnings := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
		phone, err := app.OpenPhone(o.Core, app.PhoneOptions{Audio: o.Audio, UserAgent: o.UserAgent, Log: warnings})
		if err != nil {
			svc.unavailable = err.Error() // the window says so
		} else {
			svc.phone = phone
			if o.Notice != nil { // before anything forwards events
				svc.notice = o.Notice
				go svc.decide(o.Notice.Actions())
			}
			if svc.recentChanged != nil {
				svc.recentChanged() // the history of earlier sessions
			}
			forwarded := make(chan struct{})
			go func() { defer close(forwarded); svc.forward() }()
			// Leaving: the calls end and the account unregisters, and the last events are through.
			defer func() { phone.Close(); <-forwarded }()
		}
	}
	pageGone := func(UIEvent) {} // where events go once the window has closed
	if o.Trace != "" && !o.SelfTest {
		record, err := openTrace(o.Trace)
		if err != nil {
			return err
		}
		svc.mu.Lock()
		emit := svc.emit
		svc.emit = func(ev UIEvent) { record.sent(ev); emit(ev) }
		svc.probed = record.page
		svc.mu.Unlock()
		pageGone = record.sent
		go func() {
			for range time.Tick(snapshotEvery) {
				main.ExecJS(snapshotScript)
				ui.Event.Emit(probeEvent)
			}
		}()
	}

	// Once the page is loaded the webview is told to stay on it.
	var guarding sync.Once
	var unguarded atomic.Bool
	guarded := make(chan bool, 1)
	main.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		guarding.Do(func() {
			go func() {
				ok := application.InvokeSyncWithResult(func() bool { return guardNavigation(main.NativeWindow()) })
				switch {
				case ok:
				case guardExpected && !o.SelfTest:
					// The page could leave: the window does not stay open like that. (A self-test
					// goes on, to report it.)
					unguarded.Store(true)
					slog.Error(errUnguarded.Error())
					ui.Dialog.Error().SetTitle("go-microsip").SetMessage(errUnguarded.Error() + ".").Show()
					quit()
				case !guardExpected:
					slog.Warn("no navigation guard on this platform: the content security policy alone confines the page")
				}
				guarded <- ok
			}()
		})
	})

	// --self-test: the probe's verdict and the timeout race for one result; the first one wins and
	// quits.
	result := make(chan error, 1)
	if o.SelfTest {
		finish := func(err error) {
			select {
			case result <- err:
				ui.Quit()
			default:
			}
		}
		var probing sync.Once
		isGuarded := false
		svc.ready = func() {
			probing.Do(func() {
				go func() {
					select {
					case isGuarded = <-guarded:
					case <-time.After(selfTestTimeout):
						return // the timer below reports it
					}
					main.ExecJS(probeScript(isGuarded))
					time.Sleep(2 * time.Second) // a navigation that was not refused has replaced the page by now
					ui.Event.Emit(probeEvent)
				}()
			})
		}
		svc.probed = func(report string) {
			listening, err := listeningSockets()
			summary, err := probeVerdict(report, guardExpected, isGuarded, navigationsBlocked(), listening, err)
			if err == nil {
				fmt.Println("self-test:", summary)
			}
			finish(err)
		}
		timer := time.AfterFunc(selfTestTimeout, func() {
			finish(errors.New("self-test: the page did not call the backend and answer the probe in time"))
		})
		defer timer.Stop()
	}
	err = ui.Run()
	svc.mu.Lock()
	svc.emit = pageGone
	svc.mu.Unlock()
	if err != nil {
		return err
	}
	if unguarded.Load() {
		return errUnguarded
	}
	if o.SelfTest {
		select {
		case err := <-result:
			return err
		default:
			return errors.New("self-test: the window closed before the page was ready")
		}
	}
	return nil
}
