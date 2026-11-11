//go:build unix

package lock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

func TestLockIsExclusiveAndReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "instance.lock")
	unlock, err := Files{}.Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("lock directory: %v %v", info, err)
	}
	if _, err := (Files{}).Lock(path); !errors.Is(err, ports.ErrLocked) {
		t.Errorf("a second lock: %v, want ErrLocked", err)
	}
	unlock()
	again, err := Files{}.Lock(path)
	if err != nil {
		t.Fatalf("after the release: %v", err)
	}
	again()
}
