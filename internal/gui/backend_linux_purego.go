//go:build linux && purego

package gui

import "github.com/jniltinho/go-microsip/internal/buildinfo"

// The CGO-free Linux backend: GTK 4 and WebKitGTK 6.0 loaded at run time.
func init() { buildinfo.SetWindow("purego") }
