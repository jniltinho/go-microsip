// Package config resolves go-microsip's configuration without ever persisting a secret.
//
// Every field is resolved on its own and the first source that holds it wins:
//
//  1. per-field command-line overrides (Context.Flags),
//  2. the process environment (GO_MICROSIP_<SECTION>_<KEY>, see Field.EnvName),
//  3. an explicit .env file (only from --env-file; the current directory is never searched),
//  4. one TOML file: an explicit --config path replaces the default
//     $XDG_CONFIG_HOME/go-microsip/config.toml (fallback $HOME/.config, %APPDATA% on Windows),
//  5. values the user saved in the SQLite database (Context.SQLite, see SQLiteLayer),
//  6. built-in defaults.
//
// Only explicit user edits are written back to SQLite, and only for fields no higher source
// overrides (SQLiteEdits); the effective configuration is never persisted.
//
// Each resolved field records its Source. A missing required field (Missing) is distinct from a
// present but invalid one (Invalid). Errors name fields, sources, files and line numbers, never the
// offending values, because those may be secrets.
//
// # Secrets
//
// Save, Stored.TOML and the DoctorReport never contain a password. A password is taken, in order,
// from the environment variable named by account.password_env (process environment, then the .env
// file), from the private file account.password_file, or from a literal account.password in TOML
// (kept in memory only and flagged by Effective.LiteralPasswordWarning). Otherwise the window asks
// per session. A file that actually holds a secret must be a regular file owned by the current
// user with no group or other permission bits; the password file must also not be a symlink.
//
// # File permissions
//
// Configuration, .env and CA files that group or others can write are refused, because another
// user could inject settings such as the registrar host or a trust root.
//
// The names differ from the Rust candidate: the directory is go-microsip and the environment
// prefix is GO_MICROSIP_. RUST_MICROSIP_ variables are not read; the doctor report lists them.
package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jniltinho/go-microsip/internal/core/domain"
)

const (
	// SchemaVersion is the supported value of the top-level schema key.
	SchemaVersion = 1
	// EnvPrefix starts every environment variable of a field.
	EnvPrefix = "GO_MICROSIP_"
	// LegacyEnvPrefix is the Rust candidate's prefix; such variables are reported, not read.
	LegacyEnvPrefix = "RUST_MICROSIP_"
	// DefaultPasswordEnv is the variable read for the password when account.password_env is unset.
	DefaultPasswordEnv = "GO_MICROSIP_SIP_PASSWORD"
	// DefaultExpires is the default registration lifetime in seconds.
	DefaultExpires = 300
	// DirName is the application's directory under the configuration base.
	DirName = "go-microsip"
)

// Source says where an effective value came from.
type Source uint8

// Sources, highest precedence first.
const (
	Flag Source = iota
	Env
	DotEnv
	TOML
	SQLite
	Default
)

// String is the stable lower-case label used in diagnostics and JSON.
func (s Source) String() string {
	return [...]string{"flag", "env", "dotenv", "toml", "sqlite", "default"}[s]
}

// Field is one configurable, non-secret setting.
type Field uint8

// Fields, in schema order.
const (
	DisplayName  Field = iota // account.display_name
	Username                  // account.username
	AuthUsername              // account.auth_username
	Domain                    // account.domain
	Server                    // account.server (defaults to the domain)
	Port                      // account.port (defaults to the transport's port)
	Transport                 // account.transport: udp, tcp or tls
	Media                     // account.media: rtp or srtp
	Expires                   // account.expires, seconds
	PasswordEnv               // account.password_env: name of the variable holding the password
	PasswordFile              // account.password_file: absolute path of a private password file
	CAFile                    // account.ca_file: absolute path of a PEM CA bundle for TLS
	UITheme                   // ui.theme: system or a skin name
	DND                       // ui.dnd: do-not-disturb
	InputDevice               // audio.input_device
	OutputDevice              // audio.output_device
	fieldCount
)

var fieldKeys = [fieldCount]string{
	"account.display_name", "account.username", "account.auth_username", "account.domain",
	"account.server", "account.port", "account.transport", "account.media", "account.expires",
	"account.password_env", "account.password_file", "account.ca_file",
	"ui.theme", "ui.dnd", "audio.input_device", "audio.output_device",
}

// Fields returns every field in schema order.
func Fields() []Field {
	all := make([]Field, fieldCount)
	for i := range all {
		all[i] = Field(i)
	}
	return all
}

// Key is the dotted TOML key, such as account.server.
func (f Field) Key() string { return fieldKeys[f] }

// String is the key.
func (f Field) String() string { return f.Key() }

// EnvName is the environment variable of the field: the prefix plus the upper-cased key with the
// dot replaced by an underscore, such as GO_MICROSIP_ACCOUNT_SERVER.
func (f Field) EnvName() string {
	return EnvPrefix + strings.ToUpper(strings.ReplaceAll(f.Key(), ".", "_"))
}

// FieldFromKey returns the field with this dotted key.
func FieldFromKey(key string) (Field, bool) {
	for i, k := range fieldKeys {
		if k == key {
			return Field(i), true
		}
	}
	return 0, false
}

// Theme is the window's skin, or ThemeSystem to follow the desktop.
type Theme string

// Themes: system plus the seven skins the window ships.
const (
	ThemeSystem Theme = "system"
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark" // the default
	ThemeXP     Theme = "xp"
	ThemeWin98  Theme = "win98"
	ThemeMacOS  Theme = "macos"
	ThemeUbuntu Theme = "ubuntu"
	ThemeMatrix Theme = "matrix"
)

// Themes lists the accepted values of ui.theme.
func Themes() []Theme {
	return []Theme{ThemeSystem, ThemeLight, ThemeDark, ThemeXP, ThemeWin98, ThemeMacOS, ThemeUbuntu, ThemeMatrix}
}

// ParseTheme reads ui.theme. A name this version does not know resolves to ThemeSystem, so a
// setting saved by a version with more skins still starts.
func ParseTheme(s string) Theme {
	for _, t := range Themes() {
		if string(t) == s {
			return t
		}
	}
	return ThemeSystem
}

// UI is the [ui] section.
type UI struct {
	Theme Theme
	// DND rejects new incoming calls.
	DND bool
}

// Audio is the [audio] section; an empty ID is the system default device.
type Audio struct {
	InputDevice, OutputDevice string
}

// Stored is the secret-free configuration: what Save writes and `config export` prints.
type Stored struct {
	// Account holds the validated account settings.
	Account domain.AccountSettings
	// PasswordEnv names the environment variable that holds the password.
	PasswordEnv string
	// PasswordFile is the absolute path of a private password file, or empty.
	PasswordFile string
	// CAFile is the absolute path of a PEM CA bundle for TLS, or empty.
	CAFile string
	UI     UI
	Audio  Audio
}

// Value returns the field's value as a string, an int64 or a bool; ok is false when it is unset.
func (s Stored) Value(f Field) (value any, ok bool) {
	text := func(v string) (any, bool) { return v, v != "" }
	a := s.Account
	switch f {
	case DisplayName:
		return a.DisplayName, true
	case Username:
		return a.Username, true
	case AuthUsername:
		return text(a.AuthUsername)
	case Domain:
		return a.Domain, true
	case Server:
		return a.Server, true
	case Port:
		return int64(a.Port), true
	case Transport:
		return a.Transport.String(), true
	case Media:
		return a.Media.String(), true
	case Expires:
		return int64(a.Expires), true
	case PasswordEnv:
		return s.PasswordEnv, true
	case PasswordFile:
		return text(s.PasswordFile)
	case CAFile:
		return text(s.CAFile)
	case UITheme:
		return string(s.UI.Theme), true
	case DND:
		return s.UI.DND, true
	case InputDevice:
		return text(s.Audio.InputDevice)
	case OutputDevice:
		return text(s.Audio.OutputDevice)
	}
	return nil, false
}

// stored is the field's value in the form SQLite and the layers keep, and whether it is set.
func (s Stored) stored(f Field) (string, bool) {
	switch v, ok := s.Value(f); v := v.(type) {
	case string:
		return v, ok
	case int64:
		return strconv.FormatInt(v, 10), ok
	case bool:
		return strconv.FormatBool(v), ok
	}
	return "", false
}

// TOML renders the configuration as schema-1 TOML: the schema, then the sections and their keys in
// alphabetical order, strings double-quoted. It never contains a password.
func (s Stored) TOML() (string, error) {
	sections := map[string][]string{}
	for _, f := range Fields() {
		v, ok := s.Value(f)
		if !ok {
			continue
		}
		section, key, _ := strings.Cut(f.Key(), ".")
		value := fmt.Sprint(v)
		if text, isText := v.(string); isText {
			if !utf8.ValidString(text) {
				return "", &Error{Kind: Serialize} // a path that is not text cannot be written
			}
			value = tomlString(text)
		}
		sections[section] = append(sections[section], key+" = "+value)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "schema = %d\n", SchemaVersion)
	for _, section := range []string{"account", "audio", "ui"} {
		if lines := sections[section]; len(lines) > 0 {
			sort.Strings(lines)
			fmt.Fprintf(&b, "\n[%s]\n%s\n", section, strings.Join(lines, "\n"))
		}
	}
	return b.String(), nil
}

// tomlString quotes a string as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range s {
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteRune(c)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\r':
			b.WriteString(`\r`)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, c)
		default:
			b.WriteRune(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// PasswordSource says where the effective password came from; empty when there is none.
type PasswordSource string

// Password sources, in the order they are tried.
const (
	PasswordFromEnv     PasswordSource = "env"          // the process variable named by password_env
	PasswordFromDotEnv  PasswordSource = "dotenv"       // the same variable in the private .env file
	PasswordFromFile    PasswordSource = "file"         // the private password_file
	PasswordFromLiteral PasswordSource = "toml_literal" // a literal account.password (discouraged)
)

// Effective is the fully resolved configuration and, when a source provided it, the password.
// Formatting it never shows the password. Persist Config only deliberately: its values may come
// from temporary overrides.
type Effective struct {
	// Config holds the effective, validated, secret-free values.
	Config Stored
	// Password is set when PasswordSource is not empty.
	Password       domain.Secret
	PasswordSource PasswordSource
	// LiteralPasswordWarning: the TOML file held a literal password; recommend password_env.
	LiteralPasswordWarning bool
	// ConfigFile is the TOML file that was loaded, or empty.
	ConfigFile string
	// LegacyEnv lists the RUST_MICROSIP_ variable names that were found and not read.
	LegacyEnv []string

	provenance map[Field]Source
}

// Provenance is the source of every field that is not a default; pass it to SQLiteEdits.
func (e *Effective) Provenance() map[Field]Source { return e.provenance }

// Source says where the effective value of the field came from.
func (e *Effective) Source(f Field) Source {
	if s, ok := e.provenance[f]; ok {
		return s
	}
	return Default
}

// ErrorKind classifies a configuration failure.
type ErrorKind uint8

// Configuration failures.
const (
	FileMissing       ErrorKind = iota // an explicitly requested file does not exist
	IO                                 // a file could not be read or written
	FileTooLarge                       // a file exceeds its size bound
	NotUTF8                            // a file is not valid UTF-8
	InsecureFile                       // a file has unsafe ownership, type or permissions
	TOMLSyntax                         // TOML syntax error
	DotEnvSyntax                       // .env syntax error
	UnsupportedSchema                  // missing or unsupported schema version
	UnknownKey                         // a TOML key outside the schema
	WrongType                          // a TOML value of the wrong type
	Missing                            // a required field has no value in any source
	Invalid                            // a field value could not be parsed
	Validation                         // the account settings failed domain validation
	EmptyPassword                      // the password file is empty
	Serialize                          // TOML serialization failed
	Overridden                         // an edited field is fixed by a higher source; nothing was saved
)

// FileProblem says why a file was refused; its text tells the user how to fix it.
type FileProblem string

// Reasons a file is refused.
const (
	Symlink      FileProblem = "it is a symbolic link; point the setting at the real file"
	NotRegular   FileProblem = "it is not a regular file"
	Writable     FileProblem = "it is writable by group or others; run `chmod go-w` on it"
	NotOwned     FileProblem = "it contains a secret but is owned by another user; run `chown` to take ownership"
	ForeignOwner FileProblem = "it is owned by another user; run `chown` to take ownership"
	NotPrivate   FileProblem = "it contains a secret but is accessible by group or others; run `chmod 600` on it"
)

// Error is a configuration failure. Its text never includes a configuration value.
type Error struct {
	Kind ErrorKind
	// Path is the file, for the file and syntax kinds.
	Path string
	// Line is the 1-based line of a syntax error, 0 when unknown.
	Line int
	// Key is the dotted TOML key of UnknownKey and WrongType.
	Key string
	// Field and Origin name the setting and the source that supplied it.
	Field  Field
	Origin Source
	// Problem says why an InsecureFile was refused.
	Problem FileProblem
	// Err is the cause: the domain.ValidationError of Validation, the operating system's error of IO.
	Err error
}

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Err }

func (e *Error) Error() string {
	switch e.Kind {
	case FileMissing:
		return "file not found: " + e.Path
	case IO:
		return fmt.Sprintf("cannot access %s: %v", e.Path, e.Err)
	case FileTooLarge:
		return "file too large: " + e.Path
	case NotUTF8:
		return "file is not valid UTF-8: " + e.Path
	case InsecureFile:
		return fmt.Sprintf("refusing %s: %s", e.Path, e.Problem)
	case TOMLSyntax:
		return fmt.Sprintf("invalid TOML in %s at line %d", e.Path, e.Line)
	case DotEnvSyntax:
		return fmt.Sprintf("invalid .env syntax in %s at line %d", e.Path, e.Line)
	case UnsupportedSchema:
		return fmt.Sprintf("%s: missing or unsupported schema version (expected `schema = %d`)", e.Path, SchemaVersion)
	case UnknownKey:
		return fmt.Sprintf("%s: unknown key `%s`", e.Path, e.Key)
	case WrongType:
		return fmt.Sprintf("%s: wrong value type for `%s`", e.Path, e.Key)
	case Missing:
		return fmt.Sprintf("missing required setting `%s`", e.Field)
	case Invalid:
		return fmt.Sprintf("invalid value for `%s` from %s", e.Field, e.Origin)
	case Validation:
		return fmt.Sprintf("invalid account settings: %v", e.Err)
	case EmptyPassword:
		return "password file is empty: " + e.Path
	case Overridden:
		return fmt.Sprintf("`%s` is set by %s; change it there (nothing was saved)", e.Field, e.Origin)
	}
	return "cannot serialize configuration"
}
