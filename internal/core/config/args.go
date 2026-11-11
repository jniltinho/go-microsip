package config

import (
	"errors"
	"strings"
)

// ErrPasswordNotAccepted is returned for a password-like command-line option. It does not carry the
// argument, which may be the secret itself.
var ErrPasswordNotAccepted = errors.New(
	"passwords are not accepted on the command line; use password_env or a private password_file")

// CheckArguments refuses a command line that carries a password-like option: any option whose name
// contains "pass", such as --password, --sip-pass=... or -pass. Run it on the raw arguments before
// anything parses or echoes them. Option values and positional arguments are not inspected, so a
// path such as --config /x/password.toml is fine.
func CheckArguments(args []string) error {
	for _, arg := range args {
		if arg == "--" {
			return nil
		}
		name, _, _ := strings.Cut(arg, "=")
		if strings.HasPrefix(name, "-") && strings.Contains(strings.ToLower(name), "pass") {
			return ErrPasswordNotAccepted
		}
	}
	return nil
}
