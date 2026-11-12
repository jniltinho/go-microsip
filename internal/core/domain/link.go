package domain

import (
	"strconv"
	"strings"
)

// MaxLinkLen is the longest sip: or tel: link accepted, in bytes.
const MaxLinkLen = 256

// LinkError says why an external link was refused. The link's text is never part of it.
type LinkError string

// Error is the fixed message of the refusal.
func (e LinkError) Error() string { return string(e) }

// The refusals of ParseDialLink, in the order they are checked.
const (
	ErrLinkTooLong               LinkError = "the link is too long"
	ErrLinkInvalidCharacters     LinkError = "the link contains unsupported characters"
	ErrLinkUnsupportedParameters LinkError = "link parameters and headers are not supported"
	ErrLinkUnsupportedScheme     LinkError = "only sip: and tel: links are supported"
	ErrLinkInvalidEncoding       LinkError = "the link has an invalid percent escape"
	ErrLinkInvalidHost           LinkError = "the link host is invalid"
	ErrLinkInvalidTarget         LinkError = "the link does not contain a valid number or user"
)

// ParseDialLink reads a "sip:user@host", "sip:user" or "tel:+123" link into a dial target that the
// window must still confirm before dialing. The link is data: it is never passed to a shell.
//
// The scheme is case-insensitive. Only the user part is dialed, always through the configured
// account: a sip: host is validated and then dropped, so a link cannot send signalling to another
// server. The visual separators of tel: ("-", ".", "(", ")") are removed. Parameters, headers,
// passwords, whitespace and control characters (CR and LF included, raw or percent-encoded) are
// refused.
func ParseDialLink(link string) (DialTarget, error) {
	if len(link) > MaxLinkLen {
		return DialTarget{}, ErrLinkTooLong
	}
	for i := 0; i < len(link); i++ {
		if !graphic(link[i]) {
			return DialTarget{}, ErrLinkInvalidCharacters
		}
	}
	if strings.ContainsAny(link, ";?") {
		return DialTarget{}, ErrLinkUnsupportedParameters
	}
	scheme, rest, found := strings.Cut(link, ":")
	if !found {
		return DialTarget{}, ErrLinkUnsupportedScheme
	}
	var user string
	var err error
	switch strings.ToLower(scheme) {
	case "sip":
		name, host, hasHost := strings.Cut(rest, "@")
		if hasHost && !validLinkHost(host) {
			return DialTarget{}, ErrLinkInvalidHost
		}
		if user, err = percentDecode(name); err != nil {
			return DialTarget{}, err
		}
	case "tel":
		if user, err = percentDecode(rest); err != nil {
			return DialTarget{}, err
		}
		user = strings.NewReplacer("-", "", ".", "", "(", "", ")", "").Replace(user)
	default:
		return DialTarget{}, ErrLinkUnsupportedScheme
	}
	// The user part as it is: a dial target is not trimmed here, and the characters above are all
	// printable, so nothing is hidden by the trimming ParseDialTarget does for typed text.
	target, err := ParseDialTarget(user)
	if err != nil {
		return DialTarget{}, ErrLinkInvalidTarget
	}
	return target, nil
}

// graphic is a printable ASCII byte other than the space.
func graphic(b byte) bool { return b > ' ' && b < 0x7f }

// percentDecode decodes in one pass and accepts only printable ASCII results.
func percentDecode(value string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(value); {
		b := value[i]
		if b == '%' {
			if i+3 > len(value) {
				return "", ErrLinkInvalidEncoding
			}
			n, err := strconv.ParseUint(value[i+1:i+3], 16, 8)
			if err != nil || !hex(value[i+1]) || !hex(value[i+2]) {
				return "", ErrLinkInvalidEncoding
			}
			b = byte(n)
			i += 3
		} else {
			i++
		}
		if !graphic(b) {
			return "", ErrLinkInvalidEncoding
		}
		out.WriteByte(b)
	}
	return out.String(), nil
}

func hex(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

// validLinkHost accepts "name", "name:port", "[v6]" and "[v6]:port" with conservative characters.
func validLinkHost(host string) bool {
	var name, port string
	hasPort := false
	if v6, ok := strings.CutPrefix(host, "["); ok {
		addr, tail, closed := strings.Cut(v6, "]")
		if !closed || addr == "" {
			return false
		}
		for i := 0; i < len(addr); i++ {
			if !hex(addr[i]) && addr[i] != ':' && addr[i] != '.' {
				return false
			}
		}
		name = addr
		if tail != "" {
			if port, hasPort = strings.CutPrefix(tail, ":"); !hasPort {
				return false
			}
		}
	} else {
		name, port, hasPort = strings.Cut(host, ":")
		for _, label := range strings.Split(name, ".") {
			if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return false
			}
		}
		for i := 0; i < len(name); i++ {
			if c := rune(name[i]); !alnum(c) && c != '-' && c != '.' {
				return false
			}
		}
	}
	if len(name) > 253 {
		return false
	}
	if hasPort {
		if len(port) == 0 || len(port) > 5 {
			return false
		}
		for i := 0; i < len(port); i++ {
			if port[i] < '0' || port[i] > '9' {
				return false
			}
		}
		if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
			return false
		}
	}
	return true
}
