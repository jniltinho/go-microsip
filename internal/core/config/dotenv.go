package config

import (
	"fmt"
	"strings"
	"unicode"
)

// DotEnvError is a .env syntax error; it carries only the line number, never the content.
type DotEnvError struct {
	// Line is 1-based.
	Line int
}

func (e *DotEnvError) Error() string { return fmt.Sprintf("invalid .env syntax at line %d", e.Line) }

func identifier(s string) bool {
	for i, c := range s {
		letter := c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return s != ""
}

// ParseDotEnv parses KEY=VALUE lines.
//
// Supported: blank lines, # comments, an optional "export " prefix, CRLF line endings, unquoted
// values (trimmed; " #" starts a comment), single-quoted literal values, and double-quoted values
// with the \n, \r, \t, \" and \\ escapes. Multi-line values and variable expansion are not
// supported. A later duplicate wins. The result may hold secrets: do not log it. The caller bounds
// the size.
func ParseDotEnv(text string) (map[string]string, error) {
	out := map[string]string{}
	for index, raw := range strings.Split(text, "\n") {
		bad := &DotEnvError{Line: index + 1}
		line := strings.TrimLeftFunc(strings.TrimSuffix(raw, "\r"), unicode.IsSpace)
		if line == "" || line[0] == '#' {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "export "); ok {
			line = strings.TrimLeftFunc(rest, unicode.IsSpace)
		}
		key, rest, ok := strings.Cut(line, "=")
		key = strings.TrimRightFunc(key, unicode.IsSpace)
		if !ok || !identifier(key) {
			return nil, bad
		}
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		var value, tail string
		switch {
		case strings.HasPrefix(rest, "'"):
			end := strings.IndexByte(rest[1:], '\'')
			if end < 0 {
				return nil, bad
			}
			value, tail = rest[1:1+end], rest[2+end:]
		case strings.HasPrefix(rest, `"`):
			var b strings.Builder
			closed := false
			body := rest[1:]
			for i := 0; i < len(body) && !closed; i++ {
				switch c := body[i]; c {
				case '"':
					closed, tail = true, body[i+1:]
				case '\\':
					if i++; i >= len(body) {
						return nil, bad
					}
					escaped, known := map[byte]byte{'n': '\n', 'r': '\r', 't': '\t', '"': '"', '\\': '\\'}[body[i]]
					if !known {
						return nil, bad
					}
					b.WriteByte(escaped)
				default:
					b.WriteByte(c)
				}
			}
			if !closed {
				return nil, bad
			}
			value = b.String()
		default:
			end := len(rest)
			for i, c := range rest {
				if c == '#' && (i == 0 || unicode.IsSpace(rune(rest[i-1]))) {
					end = i
					break
				}
			}
			value = strings.TrimRightFunc(rest[:end], unicode.IsSpace)
		}
		if tail = strings.TrimLeftFunc(tail, unicode.IsSpace); tail != "" && tail[0] != '#' {
			return nil, bad
		}
		out[key] = value
	}
	return out, nil
}
