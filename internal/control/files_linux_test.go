package control

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// posix reports who owns a file and its mode, as the platform's adapter does.
type posix struct{}

func (posix) CurrentUser() uint32 { return uint32(os.Geteuid()) }
func (posix) Inspect(info os.FileInfo) (owner, mode uint32) {
	return info.Sys().(*syscall.Stat_t).Uid, uint32(info.Mode().Perm())
}

func TestGrantFilesArePrivateAndNeverWrittenOver(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run", "grants")
	grant := IssuedGrant{ID: "grant-1-abc", Capability: strings.Repeat("a", 64)}
	path, err := WriteGrantFile(dir, grant)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 || filepath.Base(path) != "grant-1-abc.grant" {
		t.Errorf("the grant file: %v, %v", info, err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("its directory: %v, %v", info.Mode(), err)
	}
	if _, err := WriteGrantFile(dir, grant); err == nil {
		t.Error("a grant file was written over")
	}
	if got, err := ReadCapability(path, posix{}); err != nil || got != grant.Capability {
		t.Errorf("the capability read back: %v", err)
	}
	// A bootstrap's file gives the service its id and its capability, and a client the capability.
	boot := IssuedGrant{ID: "grant-1-def", Capability: strings.Repeat("b", 64), BootstrapID: "boot-1-xyz"}
	bootPath, err := WriteGrantFile(dir, boot)
	if err != nil {
		t.Fatal(err)
	}
	if id, capability, err := ReadBootstrap(bootPath, posix{}); err != nil || id != "boot-1-xyz" || capability != boot.Capability {
		t.Errorf("the bootstrap read back: %q, %v", id, err)
	}
	if got, err := ReadCapability(bootPath, posix{}); err != nil || got != boot.Capability {
		t.Errorf("a bootstrap file as a client's grant: %v", err)
	}
	if _, _, err := ReadBootstrap(path, posix{}); !errors.Is(err, ErrNotABootstrap) {
		t.Errorf("a session's grant as a bootstrap: %v", err)
	}
	// A directory others can enter is not written to.
	open := filepath.Join(t.TempDir(), "open")
	if err := os.Mkdir(open, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteGrantFile(open, grant); !errors.Is(err, ErrInsecure) {
		t.Errorf("a grant written where others can read: %v", err)
	}
}

func TestAGrantFileThatIsNotPrivateIsNotRead(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string, mode os.FileMode) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	secret := strings.Repeat("c", 64)
	good := write("good.grant", secret+"\n", 0o600)
	link := filepath.Join(dir, "link.grant")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		path string
		want error
	}{
		"a file the group can read":  {write("group.grant", secret, 0o640), ErrGrantNotPrivate},
		"a file others can read":     {write("others.grant", secret, 0o604), ErrGrantNotPrivate},
		"a symbolic link":            {link, ErrGrantNotPrivate},
		"a directory":                {dir, ErrGrantNotPrivate},
		"no file":                    {filepath.Join(dir, "missing"), ErrGrantUnreadable},
		"a file over the size limit": {write("big.grant", strings.Repeat("d", 513), 0o600), ErrNotAGrant},
		"a file that holds no token": {write("words.grant", "not a token\n", 0o600), ErrNotAGrant},
		"an empty file":              {write("empty.grant", "", 0o600), ErrNotAGrant},
		"JSON that is no bootstrap":  {write("other.json", `{"capability":"x"}`, 0o600), ErrNotABootstrap},
		"a bootstrap with more":      {write("more.json", `{"bootstrap_id":"b","capability":"c","extra":"e"}`, 0o600), ErrNotABootstrap},
	} {
		got, err := ReadCapability(c.path, posix{})
		if !errors.Is(err, c.want) || got != "" {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Errorf("%s: the error repeats the file", name)
		}
	}
	// A file of another user, as the adapter reports it.
	if _, err := ReadCapability(good, stranger{}); !errors.Is(err, ErrGrantNotPrivate) {
		t.Errorf("a file of another user: %v", err)
	}
}

// stranger says every file belongs to someone else.
type stranger struct{ posix }

func (stranger) Inspect(info os.FileInfo) (owner, mode uint32) {
	return uint32(os.Geteuid()) + 1, uint32(info.Mode().Perm())
}
