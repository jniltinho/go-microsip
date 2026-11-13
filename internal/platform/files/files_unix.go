//go:build unix

// Package files reports who owns a file and who may touch it, for the configuration's checks.
package files

import (
	"os"
	"syscall"
)

// Access implements ports.FileAccess with the file's Unix owner and mode.
type Access struct{}

// CurrentUser is the effective user ID.
func (Access) CurrentUser() uint32 { return uint32(os.Geteuid()) }

// Inspect returns the owner and the permission bits of the file.
func (Access) Inspect(info os.FileInfo) (owner, mode uint32) {
	mode = uint32(info.Mode().Perm())
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Uid, mode
	}
	return ^uint32(0), mode // no owner known: nobody's file, so it is refused
}
