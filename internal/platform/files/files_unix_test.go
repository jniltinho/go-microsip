//go:build unix

package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectReadsOwnerAndMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	var a Access
	owner, mode := a.Inspect(info)
	if owner != a.CurrentUser() || mode != 0o640 {
		t.Errorf("owner %d mode %o, want %d and 640", owner, mode, a.CurrentUser())
	}
}
