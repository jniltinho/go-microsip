//go:build linux && cgo && !purego

package gui

import "github.com/jniltinho/go-microsip/internal/buildinfo"

// The cgo Linux backend: GTK 4 and WebKitGTK 6.0 linked at build time.
func init() { buildinfo.SetWindow("cgo") }
