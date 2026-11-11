package gui

import (
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// safeGraphics starts the webview with GPU acceleration off: a workaround for a driver that cannot
// render the window, not a promise that no GPU path is used. The variables only matter on Linux.
func safeGraphics(w *application.WebviewWindowOptions) {
	for k, v := range map[string]string{
		"GSK_RENDERER": "cairo", "WEBKIT_DISABLE_DMABUF_RENDERER": "1",
		"WEBKIT_DISABLE_COMPOSITING_MODE": "1", "LIBGL_ALWAYS_SOFTWARE": "1",
	} {
		_ = os.Setenv(k, v)
	}
	w.Linux.WebviewGpuPolicy = application.WebviewGpuPolicyNever
}
