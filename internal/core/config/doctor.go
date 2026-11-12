package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// DoctorField is one effective field of a DoctorReport.
type DoctorField struct {
	Key string `json:"key"`
	// Value is a string, an integer or a boolean, never a secret; nil when unset.
	Value  any    `json:"value"`
	Source string `json:"source"`
}

// DoctorReport is the sanitized report of `doctor`: the effective values with their sources, and
// secrets only as booleans. Building it never registers or dials.
type DoctorReport struct {
	// OK tells whether the configuration resolved and validated.
	OK bool `json:"ok"`
	// Error is the value-free message when OK is false.
	Error *string `json:"error"`
	// ConfigFile is the TOML file that was loaded.
	ConfigFile *string `json:"config_file"`
	// PasswordPresent: a password is available without prompting.
	PasswordPresent bool `json:"password_present"`
	// PasswordSource names the source of the password.
	PasswordSource *string `json:"password_source"`
	// LiteralPasswordWarning: a literal password was found in TOML.
	LiteralPasswordWarning bool `json:"literal_password_warning"`
	// UnknownTheme: ui.theme names a skin this version does not have; the window starts with System.
	UnknownTheme bool `json:"unknown_theme"`
	// IgnoredLegacyEnv names the RUST_MICROSIP_ variables that are set and not read.
	IgnoredLegacyEnv []string `json:"ignored_legacy_env"`
	// Fields are the effective fields; empty when resolution failed.
	Fields []DoctorField `json:"fields"`
}

// Doctor resolves the configuration and reports it.
func Doctor(c Context) DoctorReport {
	r := DoctorReport{IgnoredLegacyEnv: []string{}, Fields: []DoctorField{}}
	cfg, err := Resolve(c)
	if err != nil {
		msg := err.Error()
		r.Error = &msg
		// The .env file may be the unreadable part: report the process environment at least.
		for name := range c.Env {
			if strings.HasPrefix(name, LegacyEnvPrefix) {
				r.IgnoredLegacyEnv = append(r.IgnoredLegacyEnv, name)
			}
		}
		sort.Strings(r.IgnoredLegacyEnv)
		return r
	}
	r.OK, r.LiteralPasswordWarning, r.UnknownTheme = true, cfg.LiteralPasswordWarning, cfg.UnknownTheme
	r.IgnoredLegacyEnv = append(r.IgnoredLegacyEnv, cfg.LegacyEnv...)
	if cfg.ConfigFile != "" {
		r.ConfigFile = &cfg.ConfigFile
	}
	if cfg.PasswordSource != "" {
		source := string(cfg.PasswordSource)
		r.PasswordPresent, r.PasswordSource = true, &source
	}
	for _, f := range Fields() {
		field := DoctorField{Key: f.Key(), Source: cfg.Source(f).String()}
		if v, ok := cfg.Config.Value(f); ok {
			field.Value = v
		}
		r.Fields = append(r.Fields, field)
	}
	return r
}

// JSON renders the report as one JSON object on one line.
func (r DoctorReport) JSON() string {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(r) // strings, numbers and booleans only: it cannot fail
	return strings.TrimSuffix(out.String(), "\n")
}

// String renders the report as text, one field per line.
func (r DoctorReport) String() string {
	var b strings.Builder
	legacy := func() {
		if len(r.IgnoredLegacyEnv) > 0 {
			fmt.Fprintf(&b, "warning: %s* variables are not read; rename them to %s*: %s\n",
				LegacyEnvPrefix, EnvPrefix, strings.Join(r.IgnoredLegacyEnv, ", "))
		}
	}
	if r.Error != nil {
		fmt.Fprintf(&b, "configuration: INVALID\nerror: %s\n", *r.Error)
		legacy()
		return b.String()
	}
	file := "(none)"
	if r.ConfigFile != nil {
		file = *r.ConfigFile
	}
	fmt.Fprintf(&b, "configuration: OK\nconfig file: %s\n", file)
	for _, f := range r.Fields {
		value := "(unset)"
		switch v := f.Value.(type) {
		case string:
			value = fmt.Sprintf("%q", v)
		case int64, bool:
			value = fmt.Sprint(v)
		}
		fmt.Fprintf(&b, "%s = %s [%s]\n", f.Key, value, f.Source)
	}
	present, source := "absent", "none (prompt per session)"
	if r.PasswordSource != nil {
		present, source = "present", *r.PasswordSource
	}
	fmt.Fprintf(&b, "password: %s [%s]\n", present, source)
	if r.LiteralPasswordWarning {
		b.WriteString("warning: literal password in TOML; prefer password_env\n")
	}
	if r.UnknownTheme {
		b.WriteString("warning: ui.theme names a skin this version does not have; the window starts with System\n")
	}
	legacy()
	return b.String()
}
