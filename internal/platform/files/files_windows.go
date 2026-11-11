// Package files reports who owns a file and who may touch it, for the configuration's checks.
package files

import "os"

// Access implements ports.FileAccess on Windows.
//
// shortcut: Windows has no numeric owner or mode bits, so every file reads as private and owned by
// the user, as in the Rust candidate; check the file's DACL here when the Windows adapters land (M4).
type Access struct{}

// CurrentUser is always 0.
func (Access) CurrentUser() uint32 { return 0 }

// Inspect reports the file as private and owned by the current user.
func (Access) Inspect(os.FileInfo) (owner, mode uint32) { return 0, 0o600 }
