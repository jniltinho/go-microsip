//go:build darwin

package gui

import "github.com/jniltinho/go-microsip/internal/buildinfo"

// The system WebKit view.
func init() { buildinfo.SetWindow("webkit") }
