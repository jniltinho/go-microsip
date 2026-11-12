package archtest

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jniltinho/go-microsip/internal/core/config"
)

// The skins of the window (design D7 of migrate-to-go-wails): one stylesheet per skin, one token
// contract. This fails when fewer or other skins than the seven ship, when a skin defines another set
// of tokens than the others (the skin and the token are named), when the page's registry or the
// configuration's accepted values name other skins than the stylesheets do.
func TestSkinsShareOneTokenContract(t *testing.T) {
	dir := filepath.Join("..", "..", "frontend", "src", "themes")
	files, err := filepath.Glob(filepath.Join(dir, "*.css"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no skin stylesheets in %s: %v", dir, err)
	}
	block := regexp.MustCompile(`(?s):root\[data-theme="([a-z0-9]+)"\]\s*\{(.*?)\}`)
	token := regexp.MustCompile(`(?m)^\s*(--[a-z0-9-]+)\s*:`)
	tokens := map[string][]string{}
	for _, file := range files {
		text, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range block.FindAllStringSubmatch(string(text), -1) {
			id := m[1]
			if _, twice := tokens[id]; twice {
				t.Errorf("skin %q is defined more than once", id)
			}
			if want := id + ".css"; filepath.Base(file) != want {
				t.Errorf("skin %q is defined in %s, want %s", id, filepath.Base(file), want)
			}
			var names []string
			for _, d := range token.FindAllStringSubmatch(m[2], -1) {
				names = append(names, d[1])
			}
			sort.Strings(names)
			if dup := slices.Compact(slices.Clone(names)); len(dup) != len(names) {
				t.Errorf("skin %q declares a token twice", id)
			}
			tokens[id] = names
		}
	}
	shipped := []string{"dark", "light", "macos", "matrix", "ubuntu", "win98", "xp"}
	var found []string
	for id := range tokens {
		found = append(found, id)
	}
	sort.Strings(found)
	if !slices.Equal(found, shipped) {
		t.Fatalf("skins with a stylesheet: %v, want %v", found, shipped)
	}
	contract := tokens["light"]
	if len(contract) < 20 {
		t.Fatalf("the light skin declares %d tokens: the stylesheet was not read as expected", len(contract))
	}
	for _, id := range shipped {
		for _, name := range contract {
			if !slices.Contains(tokens[id], name) {
				t.Errorf("skin %q lacks the token %s", id, name)
			}
		}
		for _, name := range tokens[id] {
			if !slices.Contains(contract, name) {
				t.Errorf("skin %q adds the token %s", id, name)
			}
		}
	}

	// The page's registry and the configuration accept exactly these skins, and "system".
	registry, err := os.ReadFile(filepath.Join(dir, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	var registered []string
	for _, m := range regexp.MustCompile(`\{ id: "([a-z0-9]+)", label:`).FindAllStringSubmatch(string(registry), -1) {
		registered = append(registered, m[1])
	}
	sort.Strings(registered)
	if !slices.Equal(registered, shipped) {
		t.Errorf("skins in the page's registry: %v, want %v", registered, shipped)
	}
	var accepted []string
	for _, theme := range config.Themes() {
		accepted = append(accepted, string(theme))
	}
	sort.Strings(accepted)
	if want := append([]string{}, append(shipped, "system")...); !slices.Equal(accepted, sortedCopy(want)) {
		t.Errorf("values the configuration accepts for ui.theme: %v, want the skins and system", accepted)
	}
	if !strings.Contains(string(registry), `SYSTEM = "system"`) {
		t.Error(`the page's registry has no "system" choice`)
	}
}

func sortedCopy(s []string) []string {
	out := slices.Clone(s)
	sort.Strings(out)
	return out
}
