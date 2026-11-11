// Package app is the part of the core that front ends call besides its domain and ports: the
// command line, the window and the MCP server reach configuration, storage and the engine only
// through it.
package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jniltinho/go-microsip/internal/core/config"
	"github.com/jniltinho/go-microsip/internal/core/ports"
	"github.com/jniltinho/go-microsip/internal/core/rustimport"
	"github.com/jniltinho/go-microsip/internal/core/store"
)

// CheckArguments refuses a command line that carries a password-like option (any option whose name
// contains "pass"). Run it on the raw arguments before anything parses or echoes them; the error
// never contains the argument.
func CheckArguments(args []string) error { return config.CheckArguments(args) }

// Options is what every operation needs from the command line and the platform.
type Options struct {
	// ConfigPath is the explicit TOML file (--config); it replaces the default one.
	ConfigPath string
	// EnvFile is the explicit .env file (--env-file).
	EnvFile string
	// Files reports the ownership and permissions of the files read.
	Files ports.FileAccess
	// Env is the environment; nil is the process environment.
	Env map[string]string
	// Warn receives a problem that does not stop the operation, such as an unreadable database.
	Warn func(message string)
	// Locks takes instance locks; nil when this build cannot.
	Locks ports.Locker
	// Secrets is the keyring; nil when this build has none.
	Secrets ports.Secrets
	// LegacySecrets reads the password the Rust application stored; nil when it cannot be read.
	LegacySecrets rustimport.LegacySecrets
}

func (o Options) warn(message string) {
	if o.Warn != nil {
		o.Warn(message)
	}
}

// DoctorReport is the sanitized configuration report: effective values with their sources, and
// secrets only as booleans.
type DoctorReport = config.DoctorReport

// StorePath is the database: <data home>/go-microsip/go-microsip.sqlite3, where the data home is
// XDG_DATA_HOME, else HOME/.local/share, and LOCALAPPDATA on Windows (the database must not roam).
// Only an absolute path from the environment counts, never the working directory; without one the
// result is empty.
func StorePath(env map[string]string) string {
	abs := func(name string) string {
		if v := env[name]; filepath.IsAbs(v) {
			return v
		}
		return ""
	}
	var home string
	if runtime.GOOS == "windows" {
		home = abs("LOCALAPPDATA")
	} else if home = abs("XDG_DATA_HOME"); home == "" {
		if user := abs("HOME"); user != "" {
			home = filepath.Join(user, ".local", "share")
		}
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, config.DirName, config.DirName+".sqlite3")
}

// base is the resolution context without the database: the environment and the two paths.
func (o Options) base() config.Context {
	c := config.ProcessContext(o.ConfigPath, o.EnvFile, o.Files)
	if o.Env != nil {
		c.Env = o.Env
	}
	return c
}

// context builds the resolution context, with the settings saved in an existing database. No
// database is created here.
func (o Options) context() config.Context {
	c := o.base()
	path := StorePath(c.Env)
	if info, err := os.Stat(path); path == "" || err != nil || !info.Mode().IsRegular() {
		return c
	}
	s, err := store.Open(path)
	if err == nil {
		defer s.Close()
		var values map[string]string
		if values, err = s.LoadConfig(); err == nil {
			c.SQLite = config.SQLiteLayer(values)
		}
	}
	if err != nil {
		o.warn("settings database not read: " + err.Error())
	}
	return c
}

// IsConfiguration reports whether err says the configuration or one of its files cannot be used.
func IsConfiguration(err error) bool {
	var e *config.Error
	return errors.As(err, &e)
}

// Doctor resolves the configuration and reports it. It never registers or dials.
func Doctor(o Options) DoctorReport { return config.Doctor(o.context()) }

// Validate checks that the effective configuration resolves to a valid account.
func Validate(o Options) error {
	_, err := config.Resolve(o.context())
	return err
}

// Export renders the effective configuration as TOML, without secrets.
func Export(o Options) (string, error) {
	cfg, err := config.Resolve(o.context())
	if err != nil {
		return "", err
	}
	return cfg.Config.TOML()
}

// ErrNoDataDirectory means neither XDG_DATA_HOME nor HOME (LOCALAPPDATA on Windows) names an
// absolute directory for the database.
var ErrNoDataDirectory = errors.New("no data directory (XDG_DATA_HOME/HOME) is available")

// ImportError is a failure of Import after the preview was built: Preview is still shown.
type ImportError struct {
	Preview string
	Err     error
}

func (e *ImportError) Error() string { return "nothing imported: " + e.Err.Error() }
func (e *ImportError) Unwrap() error { return e.Err }

// Import validates a TOML file and returns the sanitized preview of what it would store. With
// apply it then replaces the settings in the local database, in one transaction. The file is only
// read, a literal password in it is never imported, and neither SIP nor audio is touched.
func Import(o Options, file string, apply bool) (string, error) {
	preview, err := config.PreviewImport(file, o.Files)
	if err != nil {
		return "", err
	}
	text := preview.String()
	if !apply {
		return text, nil
	}
	path := StorePath(o.base().Env)
	if path == "" {
		return "", &ImportError{Preview: text, Err: ErrNoDataDirectory}
	}
	// An import from the Rust application that was interrupted is undone before the database is
	// written: what is stored now must not be replaced by that recovery later.
	if err := RecoverImport(o); err != nil {
		return "", &ImportError{Preview: text, Err: err}
	}
	s, err := store.Open(path)
	if err == nil {
		// shortcut: the keyring entry of the replaced account is not deleted yet; do it here once
		// the secrets adapter exists (the Rust application removes it at this point).
		err = errors.Join(s.ReplaceConfig(preview.SQLiteValues()), s.Close())
	}
	if err != nil {
		return "", &ImportError{Preview: text, Err: err}
	}
	return text + config.ImportApplied, nil
}
