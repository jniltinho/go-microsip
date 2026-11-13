// Package domain holds the contracts every part of go-microsip shares: validated account settings
// and dial targets, registration and call states with their allowed transitions, and the Command
// and Event types through which every front end drives the engine.
//
// It does no I/O and imports no library. Secrets and what a person dials never appear when a value
// of this package is formatted or logged.
package domain

import (
	"fmt"
	"strings"
)

// Transport is the SIP signaling transport of the account.
type Transport uint8

// Signaling transports.
const (
	UDP Transport = iota
	TCP
	TLS // with certificate and host-name validation
)

// String is the lower-case label used in SIP URIs and configuration.
func (t Transport) String() string {
	switch t {
	case TCP:
		return "tcp"
	case TLS:
		return "tls"
	}
	return "udp"
}

// DefaultPort is the registrar port usual for the transport.
func (t Transport) DefaultPort() uint16 {
	if t == TLS {
		return 5061
	}
	return 5060
}

// ParseTransport reads a configuration value; only the lower-case labels are accepted.
func ParseTransport(s string) (Transport, bool) {
	switch s {
	case "udp":
		return UDP, true
	case "tcp":
		return TCP, true
	case "tls":
		return TLS, true
	}
	return UDP, false
}

// MediaSecurity is the media protection policy. SRTP is valid only with TLS signaling.
type MediaSecurity uint8

// Media protection policies.
const (
	// PlainRTP is unencrypted audio; the window shows a persistent warning.
	PlainRTP MediaSecurity = iota
	// SRTP is mandatory SDES-SRTP; a peer without it is refused.
	SRTP
)

// String is the configuration label.
func (m MediaSecurity) String() string {
	if m == SRTP {
		return "srtp"
	}
	return "rtp"
}

// ParseMediaSecurity reads a configuration value.
func ParseMediaSecurity(s string) (MediaSecurity, bool) {
	switch s {
	case "rtp":
		return PlainRTP, true
	case "srtp":
		return SRTP, true
	}
	return PlainRTP, false
}

// ValidationError says why a setting or a dial string was refused. It never carries the value.
type ValidationError string

// Validation failures.
const (
	ErrEmpty             ValidationError = "a required field is empty"
	ErrTooLong           ValidationError = "a field is too long"
	ErrInvalidCharacters ValidationError = "a field contains unsupported characters"
	ErrInvalidPort       ValidationError = "the port must be between 1 and 65535"
	ErrSRTPRequiresTLS   ValidationError = "SRTP requires TLS signaling"
)

func (e ValidationError) Error() string { return string(e) }

// bounded checks that value is not empty, at most max bytes, and made only of allowed characters.
func bounded(value string, max int, allowed func(rune) bool) error {
	if value == "" {
		return ErrEmpty
	}
	if len(value) > max {
		return ErrTooLong
	}
	for _, c := range value {
		if !allowed(c) {
			return ErrInvalidCharacters
		}
	}
	return nil
}

func alnum(c rune) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func userChar(c rune) bool { return alnum(c) || strings.ContainsRune("-_.!~*'()&=+$,;?/#", c) }
func hostChar(c rune) bool { return alnum(c) || strings.ContainsRune("-.:[]", c) }

// AccountSettings are the account's settings without its password.
type AccountSettings struct {
	// DisplayName is the optional caller display name.
	DisplayName string
	// Username is the SIP user part: an extension or a user name.
	Username string
	// AuthUsername is the authentication user when it differs from Username; empty means unset.
	AuthUsername string
	// Domain is the SIP domain of the address of record.
	Domain string
	// Server is the registrar or proxy: a host name or an IP literal.
	Server string
	// Port is the registrar or proxy port.
	Port uint16
	// Transport is the signaling transport.
	Transport Transport
	// Media is the media protection policy.
	Media MediaSecurity
	// Expires is the registration lifetime asked of the registrar, in seconds.
	Expires uint32
}

// Validate checks bounds, character sets and the SRTP-needs-TLS rule, and returns the first
// ValidationError found.
func (a AccountSettings) Validate() error {
	if err := bounded(a.Username, 64, userChar); err != nil {
		return err
	}
	if a.AuthUsername != "" {
		if err := bounded(a.AuthUsername, 64, userChar); err != nil {
			return err
		}
	}
	if err := bounded(a.Domain, 253, hostChar); err != nil {
		return err
	}
	if err := bounded(a.Server, 253, hostChar); err != nil {
		return err
	}
	if len(a.DisplayName) > 64 {
		return ErrInvalidCharacters
	}
	for _, c := range a.DisplayName {
		if c < 0x20 || c == 0x7f || c >= 0x80 && c <= 0x9f || c == '"' || c == '\\' {
			return ErrInvalidCharacters
		}
	}
	if a.Port == 0 {
		return ErrInvalidPort
	}
	if a.Media == SRTP && a.Transport != TLS {
		return ErrSRTPRequiresTLS
	}
	return nil
}

// Secret is a password. Formatting it in any way prints "[REDACTED]"; only Reveal returns it.
type Secret struct{ value string }

// NewSecret wraps a password.
func NewSecret(value string) Secret { return Secret{value} }

// Reveal returns the password, for the one place that sends it.
func (s Secret) Reveal() string { return s.value }

// Empty reports whether no password is set.
func (s Secret) Empty() bool { return s.value == "" }

// Format prints "[REDACTED]" for every verb: fmt would otherwise reach the field with %d, %x or %q.
func (Secret) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte("[REDACTED]")) }
func (Secret) MarshalText() ([]byte, error) { return []byte("[REDACTED]"), nil }

// DialTarget is a validated SIP user part to call, such as 1002, *97 or +5511… . Formatting it
// prints "DialTarget(..)": what a person dials stays out of logs. Value returns it.
type DialTarget struct{ value string }

// ParseDialTarget validates what the user typed: after trimming, 1 to 64 characters of letters,
// digits and *#+-_. , with + only in the first position.
func ParseDialTarget(typed string) (DialTarget, error) {
	value := strings.TrimSpace(typed)
	err := bounded(value, 64, func(c rune) bool { return alnum(c) || strings.ContainsRune("*#+-_.", c) })
	if err != nil {
		return DialTarget{}, err
	}
	if strings.Contains(value[1:], "+") {
		return DialTarget{}, ErrInvalidCharacters
	}
	return DialTarget{value}, nil
}

// Value is the validated user part.
func (d DialTarget) Value() string { return d.value }

// Format prints "DialTarget(..)" for every verb.
func (DialTarget) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte("DialTarget(..)")) }
func (DialTarget) MarshalText() ([]byte, error) { return []byte("DialTarget(..)"), nil }

// DTMFKey is one key accepted during a call. Formatting it prints "DTMFKey(..)".
type DTMFKey struct{ key rune }

// NewDTMFKey accepts 0-9, *, # and A-D (lower case is folded).
func NewDTMFKey(key rune) (DTMFKey, error) {
	if key >= 'a' && key <= 'd' {
		key -= 'a' - 'A'
	}
	if key >= '0' && key <= '9' || key == '*' || key == '#' || key >= 'A' && key <= 'D' {
		return DTMFKey{key}, nil
	}
	return DTMFKey{}, ErrInvalidCharacters
}

// Event is the RFC 4733 event number, 0 to 15.
func (k DTMFKey) Event() uint8 {
	switch {
	case k.key == '*':
		return 10
	case k.key == '#':
		return 11
	case k.key >= 'A':
		return uint8(12 + k.key - 'A')
	}
	return uint8(k.key - '0')
}

// Rune is the key as typed.
func (k DTMFKey) Rune() rune { return k.key }

// Format prints "DTMFKey(..)" for every verb.
func (DTMFKey) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte("DTMFKey(..)")) }
func (DTMFKey) MarshalText() ([]byte, error) { return []byte("DTMFKey(..)"), nil }
