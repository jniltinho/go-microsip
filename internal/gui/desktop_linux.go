package gui

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Desktop entry for the portable binary and the AppImage (as the Rust app's crates/platform desktop entry did, and as goXterm does).
//
// Wayland compositors (KDE, GNOME) take a window's icon from the .desktop file named after the app id,
// not from the window (GTK 4 has no per-window icon), so without one the title bar and the dock show a
// generic icon. GLib also ignores an entry whose Exec program is not on PATH, so the entry names the
// running executable (the .AppImage file inside an AppImage) by absolute path. An entry go-microsip did not
// write (a distribution package, the user's own) is never touched, and none is written when a system
// entry exists.

const (
	desktopEntry  = "applications/" + appID + ".desktop"
	desktopIcon   = "icons/hicolor/256x256/apps/" + windowIconName + ".png"
	desktopMarker = "X-go-microsip-Generated=true"
)

// installDesktopEntry installs or refreshes the entry and its icon for the running executable and
// reports whether anything was written.
func installDesktopEntry(icon []byte) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	dirs := os.Getenv("XDG_DATA_DIRS")
	if dirs == "" {
		dirs = "/usr/local/share:/usr/share"
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	// inside an AppImage the entry starts the .AppImage file, not the binary in its temporary mount
	if image, dir := os.Getenv("APPIMAGE"), os.Getenv("APPDIR"); image != "" && dir != "" && strings.HasPrefix(exe, dir) {
		exe = image
	}
	return installDesktop(data, filepath.SplitList(dirs), exe, icon)
}

func installDesktop(data string, system []string, exe string, icon []byte) (bool, error) {
	for _, d := range system {
		if _, err := os.Stat(filepath.Join(d, desktopEntry)); err == nil {
			return false, nil
		}
	}
	content, ok := desktopText(exe)
	if !ok || len(icon) == 0 {
		return false, nil
	}
	path := filepath.Join(data, desktopEntry)
	old, err := os.ReadFile(path)
	switch {
	case err == nil && (string(old) == content || !strings.Contains(string(old), "\n"+desktopMarker+"\n")):
		return false, nil // up to date, or someone else's entry
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return false, err
	}
	iconPath := filepath.Join(data, desktopIcon)
	if cur, err := os.ReadFile(iconPath); err != nil || !bytes.Equal(cur, icon) {
		if err := writeAtomic(iconPath, icon); err != nil {
			return false, err
		}
	}
	return true, writeAtomic(path, []byte(content))
}

// writeAtomic writes through a temporary file so a crash never leaves a truncated entry.
func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// desktopText is the entry for exe, or false for a path that would need escaping in Exec.
func desktopText(exe string) (string, bool) {
	if !strings.HasPrefix(exe, "/") || strings.ContainsFunc(exe, func(r rune) bool {
		return unicode.IsControl(r) || strings.ContainsRune("\"`$\\%=", r)
	}) {
		return "", false
	}
	return "[Desktop Entry]\nType=Application\nName=go-microsip\nGenericName=SIP Softphone\n" +
		"Comment=Compact SIP softphone\nExec=\"" + exe + "\"\nIcon=" + windowIconName + "\n" +
		"Terminal=false\nCategories=Network;Telephony;\nStartupWMClass=" + appID + "\n" +
		desktopMarker + "\n", true
}
