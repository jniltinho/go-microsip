// Package archtest checks the repository's layering (design D2 of migrate-to-go-wails): the core
// stays free of the desktop toolkit, of front ends and of operating-system code; front ends reach
// the core only through its contract; cgo stays in the two packages that need it; raw memory and
// system calls stay in the platform adapters and the OS-bound files beside them.
package archtest

import (
	"fmt"
	"path"
	"strings"
)

const module = "github.com/jniltinho/go-microsip"

// File is what the rules look at: a Go file's path from the repository root, its imports, and
// whether a build constraint names an operating system.
type File struct {
	Path    string
	Imports []string
	OSBound bool
}

var (
	// frontEnds are the packages that present the core to a person or an agent.
	frontEnds = []string{"cmd", "internal/gui", "internal/mcp", "internal/control"}
	// contract is what a front end may import from the core.
	contract = []string{"internal/core/domain", "internal/core/ports", "internal/core/app"}
	// engineLibraries are reached only through the core or a platform adapter.
	engineLibraries = []string{
		"github.com/emiago/", "github.com/pion/", "github.com/jfreymuth/pulse", "github.com/moutend/go-wca",
		"github.com/gen2brain/malgo", "modernc.org/sqlite", "database/sql", "github.com/zalando/go-keyring",
	}
	// domainImports is everything the domain may import: no I/O, no clock, no third party.
	domainImports = []string{"errors", "fmt", "strings", "strconv", "unicode", "unicode/utf8", "slices", "maps", "bytes", "math"}
	// cgoPackages may use cgo: the audio adapter (design D4) and the window's webview layer.
	cgoPackages = []string{"internal/platform/audio", "internal/gui"}
	// osBesides are the packages outside internal/platform whose OS-bound files talk to the
	// system themselves: the control endpoint and the window's desktop glue.
	osBesides = []string{"internal/control", "internal/gui"}
	// signalNames may import syscall for the names of signals, and nothing else of it is used
	// there: the commands that end on a signal and the lab's runner.
	signalNames = []string{"cmd", "internal/g0"}
)

// system reports whether an import reaches raw memory or the operating system directly.
func system(imp string) bool {
	return imp == "unsafe" || imp == "syscall" || strings.HasPrefix(imp, "golang.org/x/sys/")
}

func in(dir string, roots ...string) bool {
	for _, r := range roots {
		if dir == r || strings.HasPrefix(dir, r+"/") {
			return true
		}
	}
	return false
}

func hasPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// Violations returns every rule a file breaks, one line each.
func Violations(f File) []string {
	dir := path.Dir(f.Path)
	var out []string
	bad := func(format string, a ...any) { out = append(out, f.Path+": "+fmt.Sprintf(format, a...)) }
	core, domain, front := in(dir, "internal/core"), in(dir, "internal/core/domain"), in(dir, frontEnds...)

	if core && (f.OSBound || hasSuffixOS(f.Path)) {
		bad("the core is operating-system neutral: no OS-bound file")
	}
	for _, imp := range f.Imports {
		local := strings.TrimPrefix(imp, module+"/")
		ours := local != imp
		switch {
		case imp == "C" && !in(dir, cgoPackages...):
			bad("cgo is allowed only in %v", cgoPackages)
		case system(imp) && !in(dir, "internal/platform") &&
			!(in(dir, osBesides...) && (f.OSBound || hasSuffixOS(f.Path))) &&
			!(imp == "syscall" && in(dir, signalNames...)):
			bad("imports %s: raw memory and system calls stay in internal/platform and the OS-bound files of %v", imp, osBesides)
		case strings.HasPrefix(imp, "github.com/wailsapp/") && !in(dir, "internal/gui"):
			bad("imports %s: only internal/gui may import Wails", imp)
		case core && ours && (in(local, frontEnds...) || in(local, "internal/platform")):
			bad("imports %s: the core does not depend on front ends or platform adapters", imp)
		case domain && !ours && !contains(domainImports, imp):
			bad("imports %s: the domain does no I/O and uses no library", imp)
		case domain && ours && !in(local, "internal/core/domain"):
			bad("imports %s: the domain depends on nothing else", imp)
		case front && ours && in(local, "internal/core") && !in(local, contract...):
			bad("imports %s: front ends use only %v of the core", imp, contract)
		case front && ours && in(local, "internal/platform"):
			bad("imports %s: only main wires platform adapters", imp)
		case front && hasPrefix(imp, engineLibraries...):
			bad("imports %s: front ends do not reach SIP, media, audio, storage or secrets directly", imp)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func hasSuffixOS(p string) bool {
	base := strings.TrimSuffix(strings.TrimSuffix(path.Base(p), ".go"), "_test")
	for _, os := range []string{"linux", "windows", "darwin"} {
		if strings.HasSuffix(base, "_"+os) || strings.Contains(base, "_"+os+"_") {
			return true
		}
	}
	return false
}
