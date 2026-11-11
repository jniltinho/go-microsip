//go:build windows

package gui

import "github.com/jniltinho/go-microsip/internal/buildinfo"

// WebView2, without cgo.
func init() { buildinfo.SetWindow("webview2") }
