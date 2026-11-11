package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Every Go file of the project obeys the layering.
func TestLayers(t *testing.T) {
	root := "../.."
	n := 0
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, root+"/"))
		if d.IsDir() {
			// Not this project's Go code: the frozen Rust tree and its vendor folder, the vendored
			// library, local-only references, build output and dependencies.
			switch rel {
			case "vendor", "third_party", "base", "archive", "dist", "artifacts", "target", "apps", "crates",
				"frontend", "internal/gui/dist":
				return filepath.SkipDir
			}
			if strings.HasPrefix(d.Name(), ".") && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil // tests may use anything
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			return err
		}
		file := File{Path: rel}
		for _, i := range f.Imports {
			path, _ := strconv.Unquote(i.Path.Value)
			file.Imports = append(file.Imports, path)
		}
		for _, g := range f.Comments {
			for _, c := range g.List {
				if strings.HasPrefix(c.Text, "//go:build") && strings.ContainsAny(c.Text, "lwd") &&
					(strings.Contains(c.Text, "linux") || strings.Contains(c.Text, "windows") || strings.Contains(c.Text, "darwin")) {
					file.OSBound = true
				}
			}
		}
		n++
		for _, v := range Violations(file) {
			t.Error(v)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 10 {
		t.Fatalf("only %d Go files were checked: the walk is broken", n)
	}
}

// Each rule fires on a deliberate violation and stays quiet on the allowed neighbor, so a broken
// walk or a typo in a rule cannot pass silently.
func TestRulesFire(t *testing.T) {
	m := module + "/"
	for name, c := range map[string]struct {
		file File
		want string // "" means no violation
	}{
		"wails in core":                {File{Path: "internal/core/engine/x.go", Imports: []string{"github.com/wailsapp/wails/v3/pkg/application"}}, "only internal/gui may import Wails"},
		"wails in cmd":                 {File{Path: "cmd/x.go", Imports: []string{"github.com/wailsapp/wails/v3/pkg/application"}}, "only internal/gui may import Wails"},
		"wails in gui":                 {File{Path: "internal/gui/x.go", Imports: []string{"github.com/wailsapp/wails/v3/pkg/application"}}, ""},
		"core imports gui":             {File{Path: "internal/core/app/x.go", Imports: []string{m + "internal/gui"}}, "does not depend on front ends"},
		"core imports platform":        {File{Path: "internal/core/engine/x.go", Imports: []string{m + "internal/platform/audio"}}, "does not depend on front ends"},
		"core imports ports":           {File{Path: "internal/core/engine/x.go", Imports: []string{m + "internal/core/ports", "github.com/emiago/sipgo"}}, ""},
		"domain does I/O":              {File{Path: "internal/core/domain/x.go", Imports: []string{"os"}}, "the domain does no I/O"},
		"domain uses a library":        {File{Path: "internal/core/domain/x.go", Imports: []string{"github.com/emiago/sipgo/sip"}}, "the domain does no I/O"},
		"domain imports the engine":    {File{Path: "internal/core/domain/x.go", Imports: []string{m + "internal/core/engine"}}, "depends on nothing else"},
		"domain plain":                 {File{Path: "internal/core/domain/x.go", Imports: []string{"errors", "strings"}}, ""},
		"front end imports the store":  {File{Path: "internal/gui/x.go", Imports: []string{m + "internal/core/store"}}, "front ends use only"},
		"cli imports the engine":       {File{Path: "cmd/x.go", Imports: []string{m + "internal/core/engine"}}, "front ends use only"},
		"front end uses the contract":  {File{Path: "internal/mcp/x.go", Imports: []string{m + "internal/core/app", m + "internal/core/domain"}}, ""},
		"front end imports an adapter": {File{Path: "cmd/x.go", Imports: []string{m + "internal/platform/audio"}}, "only main wires"},
		"front end imports sip":        {File{Path: "internal/control/x.go", Imports: []string{"github.com/emiago/sipgo"}}, "do not reach SIP"},
		"front end imports audio":      {File{Path: "internal/gui/x.go", Imports: []string{"github.com/jfreymuth/pulse"}}, "do not reach SIP"},
		"front end imports sql":        {File{Path: "internal/mcp/x.go", Imports: []string{"modernc.org/sqlite"}}, "do not reach SIP"},
		"cgo in core":                  {File{Path: "internal/core/media/x.go", Imports: []string{"C"}}, "cgo is allowed only"},
		"cgo in cmd":                   {File{Path: "cmd/x.go", Imports: []string{"C"}}, "cgo is allowed only"},
		"cgo in audio":                 {File{Path: "internal/platform/audio/x.go", Imports: []string{"C"}}, ""},
		"OS file in core":              {File{Path: "internal/core/config/paths_linux.go"}, "operating-system neutral"},
		"OS constraint in core":        {File{Path: "internal/core/config/paths.go", OSBound: true}, "operating-system neutral"},
		"OS file in platform":          {File{Path: "internal/platform/audio/pulse_linux.go"}, ""},
		"main wires everything":        {File{Path: "main.go", Imports: []string{m + "internal/platform/audio", m + "internal/core/app", m + "cmd"}}, ""},
	} {
		got := strings.Join(Violations(c.file), "; ")
		switch {
		case c.want == "" && got != "":
			t.Errorf("%s: unexpected violation %q", name, got)
		case c.want != "" && !strings.Contains(got, c.want):
			t.Errorf("%s: got %q, want a violation naming %q", name, got, c.want)
		}
	}
}
