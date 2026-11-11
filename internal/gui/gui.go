// Package gui is go-microsip's desktop window on Wails v3: the embedded Vue / Vite / Tailwind page
// (frontend/, built into dist/) in the system webview, with App as the service the page calls. It is
// the only package that imports Wails. Nothing here touches a display until Run is called, so the
// command line and the MCP server, which link this package, still run headless.
//
// The page reaches the softphone only through App (service.go), which presents the core's phone
// (internal/core/app); the engine's events reach the page as one Wails event. The page is confined
// (confine.go): a content security policy on every asset, no navigation away from the embedded
// page, no listener for the interface. --self-test proves the window, the call into Go and the
// confinement. The tray arrives with milestone M4.
package gui

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sync"
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
	// Core are the core's options: where the configuration and the database are.
	Core app.Options
	// Audio opens the sound devices; nil when this build has no sound service.
	Audio ports.AudioDevices
	// UserAgent is the product name sent to the PBX.
	UserAgent string
}

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
	if !o.SelfTest {
		opts.SingleInstance = &application.SingleInstanceOptions{
			UniqueID: appID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if main != nil {
					main.Restore()
					main.Focus()
				}
			},
		}
	}
	ui := application.New(opts) // a second instance ends here, after waking the first one
	svc.emit = func(ev UIEvent) { ui.Event.Emit(EventName, ev) }
	svc.quit = ui.Quit
	// The System skin follows the desktop. WebKitGTK in a plain GTK window does not pass the
	// desktop's dark preference on to the page, so the page is told, at once and on every change.
	svc.dark = ui.Env.IsDarkMode
	ui.Event.OnApplicationEvent(events.Common.ThemeChanged, func(*application.ApplicationEvent) { svc.desktopChanged() })
	if !o.SelfTest {
		// The phone holds the instance lock of the database until the window is gone: calls are
		// ended and the account unregistered, within the engine's bound, before Run returns.
		phone, err := app.OpenPhone(o.Core, app.PhoneOptions{Audio: o.Audio, UserAgent: o.UserAgent})
		if err != nil {
			svc.unavailable = err.Error() // the window says so
		} else {
			svc.phone = phone
			forwarded := make(chan struct{})
			go func() { defer close(forwarded); svc.forward() }()
			// Leaving: the calls end and the account unregisters, and the last events are through.
			defer func() { phone.Close(); <-forwarded }()
		}
	}
	main = ui.Window.NewWithOptions(win)
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
	guarded := make(chan bool, 1)
	main.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		guarding.Do(func() {
			go func() {
				ok := application.InvokeSyncWithResult(func() bool { return guardNavigation(main.NativeWindow()) })
				switch {
				case ok:
				case guardExpected:
					slog.Error("the navigation guard could not be installed: the content security policy alone confines the page")
				default:
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
