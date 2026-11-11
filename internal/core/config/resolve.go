package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// Context is everything Resolve reads. Nothing else is taken from the process except the files it
// names.
type Context struct {
	// Env is a snapshot of the environment; it also provides HOME, XDG_CONFIG_HOME and APPDATA.
	Env map[string]string
	// Flags are per-field command-line overrides, the highest precedence.
	Flags map[Field]string
	// ConfigPath is the explicit TOML path (--config); it replaces the default.
	ConfigPath string
	// EnvFile is the explicit .env path (--env-file).
	EnvFile string
	// SQLite holds the values saved in the database, below TOML and above defaults (SQLiteLayer).
	SQLite map[Field]string
	// Files reports the ownership and permissions of the files read.
	Files ports.FileAccess
}

// ProcessContext snapshots the process environment for the given paths.
func ProcessContext(configPath, envFile string, files ports.FileAccess) Context {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return Context{Env: env, ConfigPath: configPath, EnvFile: envFile, Files: files}
}

// DefaultConfigPath is the TOML path used without --config: under XDG_CONFIG_HOME, else under
// HOME/.config, on Windows under APPDATA. Only an absolute base counts; otherwise it is empty.
func (c Context) DefaultConfigPath() string {
	base := ""
	if abs := func(name string) string {
		if v := c.Env[name]; filepath.IsAbs(v) {
			return v
		}
		return ""
	}; runtime.GOOS == "windows" {
		base = abs("APPDATA")
	} else if base = abs("XDG_CONFIG_HOME"); base == "" {
		if home := abs("HOME"); home != "" {
			base = filepath.Join(home, ".config")
		}
	}
	if base == "" {
		return ""
	}
	return filepath.Join(base, DirName, "config.toml")
}

// SQLiteLayer converts the string map the store returns into Context.SQLite. Unknown keys are
// ignored.
func SQLiteLayer(values map[string]string) map[Field]string {
	out := map[Field]string{}
	for k, v := range values {
		if f, ok := FieldFromKey(k); ok {
			out[f] = v
		}
	}
	return out
}

type layer struct {
	source Source
	values map[Field]string
}

// inputs is every source read from disk and the environment, before any parsing.
type inputs struct {
	layers     []layer
	dotenv     map[string]string
	dotenvFile *loaded
	configFile string
	literal    string // account.password of the TOML file
	hasLiteral bool
	legacy     []string
}

func load(c Context) (*inputs, error) {
	envLayer := func(env map[string]string) map[Field]string {
		out := map[Field]string{}
		for _, f := range Fields() {
			if v, ok := env[f.EnvName()]; ok {
				out[f] = v
			}
		}
		return out
	}
	in := &inputs{dotenv: map[string]string{}}
	if c.EnvFile != "" {
		l, err := readRequired(c.EnvFile, maxConfigBytes, false, c.Files)
		if err != nil {
			return nil, err
		}
		if in.dotenv, err = ParseDotEnv(l.text); err != nil {
			var syntax *DotEnvError
			errors.As(err, &syntax)
			return nil, &Error{Kind: DotEnvSyntax, Path: c.EnvFile, Line: syntax.Line}
		}
		in.dotenvFile = l
	}

	var file *loaded
	var err error
	if in.configFile = c.ConfigPath; in.configFile != "" {
		file, err = readRequired(in.configFile, maxConfigBytes, false, c.Files)
	} else if in.configFile = c.DefaultConfigPath(); in.configFile != "" {
		file, err = readBounded(in.configFile, maxConfigBytes, false, c.Files)
	}
	if err != nil {
		return nil, err
	}
	tomlValues := map[Field]string{}
	if file == nil {
		in.configFile = ""
	} else {
		if tomlValues, in.literal, in.hasLiteral, err = tomlLayer(file.text, in.configFile); err != nil {
			return nil, err
		}
		if in.hasLiteral {
			if err := requirePrivate(in.configFile, file.info, c.Files); err != nil {
				return nil, err
			}
		}
	}
	in.layers = []layer{
		{Flag, c.Flags}, {Env, envLayer(c.Env)}, {DotEnv, envLayer(in.dotenv)}, {TOML, tomlValues}, {SQLite, c.SQLite},
	}
	for _, env := range []map[string]string{c.Env, in.dotenv} {
		for name := range env {
			if strings.HasPrefix(name, LegacyEnvPrefix) {
				in.legacy = append(in.legacy, name)
			}
		}
	}
	sort.Strings(in.legacy)
	return in, nil
}

// LayerSources says which source holds each field, without parsing or validating the values. Use it
// to check overrides when Resolve fails (on first run, before an account exists). A field absent
// from the map is a default.
func LayerSources(c Context) (map[Field]Source, error) {
	in, err := load(c)
	if err != nil {
		return nil, err
	}
	out := map[Field]Source{}
	for _, f := range Fields() {
		for _, l := range in.layers {
			if _, ok := l.values[f]; ok {
				out[f] = l.source
				break
			}
		}
	}
	return out, nil
}

// builder parses the layered values; the first layer that holds a field wins.
type builder struct {
	layers     []layer
	provenance map[Field]Source
	err        error
}

// take returns the raw value of the first source holding the field. A value that does not parse is
// an error, never a fall-through to a lower source.
func (b *builder) take(f Field) (raw string, source Source, ok bool) {
	for _, l := range b.layers {
		if raw, ok := l.values[f]; ok {
			b.provenance[f] = l.source
			return raw, l.source, true
		}
	}
	return "", Default, false
}

func (b *builder) fail(err error) {
	if b.err == nil {
		b.err = err
	}
}

// parse applies parse to the field's value, or returns fallback when no source holds it.
func parse[T any](b *builder, f Field, fallback T, parse func(string) (T, bool)) T {
	raw, source, ok := b.take(f)
	if !ok {
		return fallback
	}
	v, valid := parse(raw)
	if !valid {
		b.fail(&Error{Kind: Invalid, Field: f, Origin: source})
		return fallback
	}
	return v
}

// required is parse for a field that must be present.
func (b *builder) required(f Field) string {
	raw, _, ok := b.take(f)
	if !ok {
		b.fail(&Error{Kind: Missing, Field: f})
	}
	return raw
}

func text(s string) (string, bool) { return s, true }

// optional accepts an empty value as "unset".
func optional(check func(string) (string, bool)) func(string) (string, bool) {
	return func(s string) (string, bool) {
		if s == "" {
			return "", true
		}
		return check(s)
	}
}

func device(s string) (string, bool) {
	return s, len(s) <= 256 && strings.IndexFunc(s, unicode.IsControl) < 0
}

func absPath(s string) (string, bool) { return s, filepath.IsAbs(s) }

func envVarName(s string) (string, bool) { return s, len(s) <= 128 && identifier(s) }

func boolean(s string) (bool, bool) {
	switch s {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	}
	return false, false
}

func number(lo, hi uint64) func(string) (uint64, bool) {
	return func(s string) (uint64, bool) {
		v, err := strconv.ParseUint(s, 10, 64)
		return v, err == nil && v >= lo && v <= hi
	}
}

// build parses and validates the layered values.
func build(layers []layer) (Stored, map[Field]Source, error) {
	b := &builder{layers: layers, provenance: map[Field]Source{}}
	// The first error in schema order is the one reported, as fields are read in that order below
	// except where one field's default depends on another.
	transport := parse(b, Transport, domain.UDP, domain.ParseTransport)
	media := parse(b, Media, domain.PlainRTP, domain.ParseMediaSecurity)
	domainName := b.required(Domain)
	account := domain.AccountSettings{
		DisplayName:  parse(b, DisplayName, "", text),
		Username:     b.required(Username),
		AuthUsername: parse(b, AuthUsername, "", text),
		Domain:       domainName,
		Server:       parse(b, Server, domainName, text),
		Port:         uint16(parse(b, Port, uint64(transport.DefaultPort()), number(0, 65535))),
		Transport:    transport,
		Media:        media,
		Expires:      uint32(parse(b, Expires, DefaultExpires, number(1, 86_400))),
	}
	if b.err == nil {
		if err := account.Validate(); err != nil {
			b.fail(&Error{Kind: Validation, Err: err})
		}
	}
	config := Stored{
		Account:      account,
		PasswordEnv:  parse(b, PasswordEnv, DefaultPasswordEnv, envVarName),
		PasswordFile: parse(b, PasswordFile, "", optional(absPath)),
		CAFile:       parse(b, CAFile, "", optional(absPath)),
		UI: UI{
			Theme: parse(b, UITheme, ThemeDark, func(s string) (Theme, bool) { return ParseTheme(s), true }),
			DND:   parse(b, DND, false, boolean),
		},
		Audio: Audio{
			InputDevice:  parse(b, InputDevice, "", optional(device)),
			OutputDevice: parse(b, OutputDevice, "", optional(device)),
		},
	}
	return config, b.provenance, b.err
}

// Resolve builds the effective configuration from every source. On an error the caller must not
// register the account.
func Resolve(c Context) (*Effective, error) {
	in, err := load(c)
	if err != nil {
		return nil, err
	}
	config, provenance, err := build(in.layers)
	if err != nil {
		return nil, err
	}
	e := &Effective{
		Config: config, LiteralPasswordWarning: in.hasLiteral, ConfigFile: in.configFile,
		LegacyEnv: in.legacy, provenance: provenance,
	}
	// The password: the environment, then a private .env, then a private file, then a TOML literal.
	fromDotEnv := in.dotenv[config.PasswordEnv]
	if fromDotEnv != "" {
		if err := requirePrivate(c.EnvFile, in.dotenvFile.info, c.Files); err != nil {
			return nil, err
		}
	}
	switch {
	case c.Env[config.PasswordEnv] != "":
		e.Password, e.PasswordSource = domain.NewSecret(c.Env[config.PasswordEnv]), PasswordFromEnv
	case fromDotEnv != "":
		e.Password, e.PasswordSource = domain.NewSecret(fromDotEnv), PasswordFromDotEnv
	case config.PasswordFile != "":
		if e.Password, err = readPasswordFile(config.PasswordFile, c.Files); err != nil {
			return nil, err
		}
		e.PasswordSource = PasswordFromFile
	case in.hasLiteral:
		e.Password, e.PasswordSource = domain.NewSecret(in.literal), PasswordFromLiteral
	}
	return e, nil
}

// SQLiteEdits returns the database writes for a settings edit, keyed as the store expects; a nil
// value removes the key.
//
// sources is Effective.Provenance (or LayerSources when resolution failed); before is the
// configuration the form started from. Only fields whose value changed are returned, so values
// that come from flags, the environment, .env or TOML are never written back. A changed field held
// by one of those sources is refused, because saving it would have no effect.
//
// With before nil (first run, nothing resolved yet) every set field counts as edited, except the
// fields a higher source holds: those are skipped, and the window shows them as fixed by it.
func SQLiteEdits(sources map[Field]Source, before *Stored, after Stored) (map[string]*string, error) {
	if err := after.Account.Validate(); err != nil {
		return nil, &Error{Kind: Validation, Err: err}
	}
	edits := map[string]*string{}
	for _, f := range Fields() {
		value, set := after.stored(f)
		changed := set
		if before != nil {
			old, was := before.stored(f)
			changed = old != value || was != set
		}
		if !changed {
			continue
		}
		source, held := sources[f]
		if held && source != SQLite && source != Default {
			if before == nil {
				continue
			}
			return nil, &Error{Kind: Overridden, Field: f, Origin: source}
		}
		if set {
			edits[f.Key()] = &value
		} else {
			edits[f.Key()] = nil
		}
	}
	return edits, nil
}

// safeKey is the printable form of a TOML key the user wrote, for error messages.
func safeKey(section, key string) string {
	ok := func(s string) bool {
		return len(s) <= 64 && strings.IndexFunc(s, func(c rune) bool {
			return !(c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z')
		}) < 0
	}
	switch {
	case !ok(section) || !ok(key):
		return "<unprintable key>"
	case section == "":
		return key
	}
	return section + "." + key
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// tomlLayer parses schema-1 TOML into per-field strings, with the types already checked, and the
// literal password when the file has one.
func tomlLayer(text, path string) (values map[Field]string, literal string, hasLiteral bool, err error) {
	var root map[string]any
	if err := toml.Unmarshal([]byte(text), &root); err != nil {
		line := 0
		var syntax *toml.DecodeError
		if errors.As(err, &syntax) {
			line, _ = syntax.Position()
		}
		return nil, "", false, &Error{Kind: TOMLSyntax, Path: path, Line: line}
	}
	if schema, ok := root["schema"].(int64); !ok || schema != SchemaVersion {
		return nil, "", false, &Error{Kind: UnsupportedSchema, Path: path}
	}
	wrong := func(key string) error { return &Error{Kind: WrongType, Path: path, Key: key} }
	values = map[Field]string{}
	for _, section := range sortedKeys(root) {
		if section == "schema" {
			continue
		}
		if section != "account" && section != "ui" && section != "audio" {
			return nil, "", false, &Error{Kind: UnknownKey, Path: path, Key: safeKey("", section)}
		}
		table, ok := root[section].(map[string]any)
		if !ok {
			return nil, "", false, wrong(section)
		}
		for _, key := range sortedKeys(table) {
			value := table[key]
			if section == "account" && key == "password" {
				if literal, hasLiteral = value.(string); !hasLiteral {
					return nil, "", false, wrong("account.password")
				}
				continue
			}
			f, known := FieldFromKey(section + "." + key)
			if !known {
				return nil, "", false, &Error{Kind: UnknownKey, Path: path, Key: safeKey(section, key)}
			}
			switch v := value.(type) {
			case string:
				ok = f != Port && f != Expires && f != DND
				values[f] = v
			case int64:
				ok = f == Port || f == Expires
				values[f] = strconv.FormatInt(v, 10)
			case bool:
				ok = f == DND
				values[f] = strconv.FormatBool(v)
			default:
				ok = false
			}
			if !ok {
				return nil, "", false, wrong(f.Key())
			}
		}
	}
	return values, literal, hasLiteral, nil
}
