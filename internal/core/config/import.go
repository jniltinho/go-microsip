package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// ImportApplied is printed after a successful `config import --apply`.
const ImportApplied = "Imported into the local database. The TOML file was not modified; " +
	"settings it still provides keep precedence over the database.\n"

// ImportPreview is the validated, secret-free content of a TOML file to import.
type ImportPreview struct {
	// File is the file that was read.
	File string
	// Values are the fields present in the file; never a password.
	Values map[Field]string
	// LiteralPasswordIgnored: the file had a literal account.password; it is not imported.
	LiteralPasswordIgnored bool
}

// SQLiteValues returns the values keyed as the store expects.
func (p ImportPreview) SQLiteValues() map[string]string {
	out := map[string]string{}
	for f, v := range p.Values {
		out[f.Key()] = v
	}
	return out
}

// String is the preview shown before --apply.
func (p ImportPreview) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "import preview of %s (nothing written yet):\n", p.File)
	values := p.SQLiteValues()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s = %q\n", k, values[k])
	}
	if p.LiteralPasswordIgnored {
		b.WriteString("warning: the literal password is not imported; use password_env or a private password_file\n")
	}
	b.WriteString("Run again with --apply to replace the settings stored in the local database.\n")
	return b.String()
}

// PreviewImport reads and fully validates path as an import source. It writes nothing; the caller
// applies SQLiteValues in one transaction only after confirmation.
//
// The file must pass the same checks as --config (a regular file, bounded, not writable by group
// or others, private when it holds a literal password) and, together with the built-in defaults,
// form a valid account. Settings files of other applications are never read.
func PreviewImport(path string, files ports.FileAccess) (*ImportPreview, error) {
	l, err := readRequired(path, maxConfigBytes, false, files)
	if err != nil {
		return nil, err
	}
	values, _, literal, err := tomlLayer(l.text, path)
	if err != nil {
		return nil, err
	}
	if literal {
		if err := requirePrivate(path, l.info, files); err != nil {
			return nil, err
		}
	}
	if _, _, err := build([]layer{{TOML, values}}); err != nil {
		return nil, err
	}
	return &ImportPreview{File: path, Values: values, LiteralPasswordIgnored: literal}, nil
}
