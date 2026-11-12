package gui

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// The Windows counterpart of the Linux desktop entry: go-microsip is made known to Windows as an
// application that can open sip: and tel: links, for this user only (HKEY_CURRENT_USER), as the
// entry declares the two types on Linux (the owner's decision of 2026-11-12). That makes it a
// candidate in Settings, Default apps; it does not make it the default, which stays the user's
// choice. A link never calls by itself: the window asks.

const (
	linkClass    = `Software\Classes\go-microsip.link`
	capabilities = `Software\go-microsip\Capabilities`
)

// registration is every registry value go-microsip writes, for the executable at exe.
func registration(exe string) map[string]map[string]string {
	return map[string]map[string]string{
		linkClass:                         {"": "URL:go-microsip link", "URL Protocol": ""},
		linkClass + `\shell\open\command`: {"": `"` + exe + `" "%1"`},
		capabilities:                      {"ApplicationName": "go-microsip", "ApplicationDescription": "Compact SIP softphone"},
		capabilities + `\URLAssociations`: {"sip": "go-microsip.link", "tel": "go-microsip.link"},
		`Software\RegisteredApplications`: {"go-microsip": capabilities},
	}
}

// installDesktopEntry registers the running executable for sip: and tel: links and reports
// whether anything was written. The icon comes from the executable itself.
func installDesktopEntry([]byte) (bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	if strings.ContainsAny(exe, `"%`) { // a path that cannot be quoted in a command line
		return false, nil
	}
	wrote := false
	for path, values := range registration(exe) {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.QUERY_VALUE|registry.SET_VALUE)
		if err != nil {
			return wrote, err
		}
		for name, value := range values {
			if current, _, err := key.GetStringValue(name); err == nil && current == value {
				continue
			}
			if err := key.SetStringValue(name, value); err != nil {
				key.Close()
				return wrote, err
			}
			wrote = true
		}
		key.Close()
	}
	return wrote, nil
}
