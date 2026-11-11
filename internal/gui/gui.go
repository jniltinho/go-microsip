// Package gui is go-microsip's desktop window on Wails v3: the embedded Vue / Vite / Tailwind page
// (frontend/, built into dist/) in the system webview, with App as the service the page calls. It is
// the only package that imports Wails. Nothing here touches a display until Run is called, so the
// command line and the MCP server, which link this package, still run headless.
//
// G0 scope: the window opens, the page calls Go, and --self-test proves both. The softphone
// screens, the navigation guard and the tray arrive with milestones M2 and M4.
package gui

import (
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/jniltinho/go-microsip/internal/buildinfo"
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
	// SelfTest opens the window, waits until the page has called Go, and exits 0.
	SelfTest bool
}

// selfTestTimeout bounds --self-test: the page must report ready by then.
const selfTestTimeout = 60 * time.Second

// App is the service bound to the page.
type App struct {
	mu    sync.Mutex
	ready func()
}

// Version is the application's version and commit, for the title and About.
func (a *App) Version() string {
	b := buildinfo.Get()
	return b.Version + " (" + b.Commit + ")"
}

// Ready is called by the page once it has drawn; --self-test then quits.
func (a *App) Ready() {
	a.mu.Lock()
	ready := a.ready
	a.mu.Unlock()
	if ready != nil {
		ready()
	}
}

// Run opens the window and returns when the application quits.
func Run(o Options) error {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		return err
	}
	svc := &App{}
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
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(dist), DisableLogging: true},
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
	app := application.New(opts)

	// --self-test: readiness and the timeout race for one result; the first one wins and quits.
	result := make(chan error, 1)
	if o.SelfTest {
		svc.ready = func() {
			select {
			case result <- nil:
				app.Quit()
			default:
			}
		}
		timer := time.AfterFunc(selfTestTimeout, func() {
			select {
			case result <- errors.New("self-test: the page did not call the backend in time"):
				app.Quit()
			default:
			}
		})
		defer timer.Stop()
	}
	main = app.Window.NewWithOptions(win)
	if err := app.Run(); err != nil {
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
