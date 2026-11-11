// Package buildinfo describes this build to every front end: version, commit and which optional
// parts were linked in.
package buildinfo

import "runtime"

// Info is the build's identity, as the doctor report and the status tool print it.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	// Window is the desktop backend linked in: "purego" or "cgo" on Linux, "webview2" on Windows,
	// or "none" for a build that has only the command line and the MCP server.
	Window string `json:"window"`
}

var current = Info{Version: "dev", Commit: "unknown", OS: runtime.GOOS, Arch: runtime.GOARCH, Window: "none"}

// Set records the version and commit given at build time.
func Set(version, commit string) { current.Version, current.Commit = version, commit }

// SetWindow records the desktop backend; the package that links the window calls it.
func SetWindow(backend string) { current.Window = backend }

// Get returns the build's identity.
func Get() Info { return current }
