package domain

import (
	"strings"
	"testing"
)

// The cases of the Rust candidate's crates/platform/src/uri.rs (LIN-02), which this parser replaces.

func TestDialLinksThatAreAccepted(t *testing.T) {
	for link, want := range map[string]string{
		"sip:1002@pbx.example.test":          "1002",
		"sip:1002":                           "1002",
		"SIP:1002@pbx.example.test:5061":     "1002",
		"sip:alice.smith@[2001:db8::1]:5060": "alice.smith",
		"sip:1002@[::1]":                     "1002",
		"sip:1002@[::ffff:192.0.2.1]":        "1002",
		"sip:1002@[1:2:3:4:5:6:7:8]":         "1002",
		"sip:1002@[1:2:3:4:5:6:192.0.2.1]":   "1002",
		"sip:1002@[::]:5060":                 "1002",
		"sip:%2A97@10.0.0.1":                 "*97",
		"tel:+5511999990000":                 "+5511999990000",
		"tel:+1-555-(010).0199":              "+15550100199",
		"Tel:%2B123":                         "+123",
	} {
		target, err := ParseDialLink(link)
		if err != nil || target.Value() != want {
			t.Errorf("%q: target %q, error %v; want %q", link, target.Value(), err, want)
		}
	}
}

func TestDialLinksThatAreRefused(t *testing.T) {
	for _, c := range []struct {
		link string
		want LinkError
	}{
		{"sip:" + strings.Repeat("1", MaxLinkLen), ErrLinkTooLong},
		{"sip:1002\r\nVia: evil", ErrLinkInvalidCharacters},
		{"sip:1002\n", ErrLinkInvalidCharacters},
		{"sip:10 02", ErrLinkInvalidCharacters},
		{"sip:1002\x7f", ErrLinkInvalidCharacters},
		{"sip:10é2", ErrLinkInvalidCharacters},
		{"sip:1002%0d%0aVia:x", ErrLinkInvalidEncoding},
		{"sip:1002%00", ErrLinkInvalidEncoding},
		{"sip:1002%20", ErrLinkInvalidEncoding},
		{"sip:1002%c3%a9", ErrLinkInvalidEncoding},
		{"sip:1002%2", ErrLinkInvalidEncoding},
		{"sip:1002%zz", ErrLinkInvalidEncoding},
		{"sip:1002?Subject=x", ErrLinkUnsupportedParameters},
		{"sip:1002@h?Route=%3Csip:evil%3E", ErrLinkUnsupportedParameters},
		{"sip:1002@h;transport=tcp", ErrLinkUnsupportedParameters},
		{"tel:+123;phone-context=x", ErrLinkUnsupportedParameters},
		{"sips:1002@h", ErrLinkUnsupportedScheme},
		{"http://evil", ErrLinkUnsupportedScheme},
		{"1002", ErrLinkUnsupportedScheme},
		{"file:/etc/passwd", ErrLinkUnsupportedScheme},
		{"sip:1002@-bad", ErrLinkInvalidHost},
		{"sip:1002@h:0", ErrLinkInvalidHost},
		{"sip:1002@h:99999", ErrLinkInvalidHost},
		{"sip:1002@h:+1", ErrLinkInvalidHost},
		{"sip:1002@a..b", ErrLinkInvalidHost},
		{"sip:1002@[zz]", ErrLinkInvalidHost},
		{"sip:1002@[1]", ErrLinkInvalidHost},
		{"sip:1002@[:::]", ErrLinkInvalidHost},
		{"sip:1002@[192.0.2.1]", ErrLinkInvalidHost},
		{"sip:1002@[fe80::1%25eth0]", ErrLinkInvalidHost},
		{"sip:1002@[2001:db8::1", ErrLinkInvalidHost},
		{"sip:1002@[1:2:3:4:5:6:7]", ErrLinkInvalidHost},
		{"sip:1002@[1:2:3:4:5:6:7:8:9]", ErrLinkInvalidHost},
		{"sip:1002@[1::2::3]", ErrLinkInvalidHost},
		{"sip:1002@[12345::1]", ErrLinkInvalidHost},
		{"sip:1002@[::ffff:192.0.2.256]", ErrLinkInvalidHost},
		{"sip:1002@[1:2:3:4:5:6:7::8]", ErrLinkInvalidHost},
		{"sip:1002@h_x", ErrLinkInvalidHost},
		{"sip:a@b@c", ErrLinkInvalidHost},
		{"sip:", ErrLinkInvalidTarget},
		{"tel:", ErrLinkInvalidTarget},
		{"sip:@pbx", ErrLinkInvalidTarget},
		{"sip://1002", ErrLinkInvalidTarget},
		{"sip:user:secret@pbx", ErrLinkInvalidTarget},
		{"tel:1+2", ErrLinkInvalidTarget},
		{"sip:%25", ErrLinkInvalidTarget},
		{"sip:%2525", ErrLinkInvalidTarget},
	} {
		if _, err := ParseDialLink(c.link); err != c.want {
			t.Errorf("%q: error %v, want %v", c.link, err, c.want)
		}
	}
}

// A link is data: what a shell would run is refused like any other invalid target, and the
// refusal never repeats the link.
func TestShellMetacharactersInALinkAreRefused(t *testing.T) {
	for _, link := range []string{
		"sip:$(reboot)@h", "sip:`id`", "sip:1002|sh", "sip:1002&&rm", "tel:1;rm", "sip:1002>out",
		"sip:%24%28id%29", "sip:1002%3Bid", "sip:'1002'", `sip:"1002"`, "sip:..%2F..%2Fetc",
	} {
		_, err := ParseDialLink(link)
		if err == nil {
			t.Errorf("%q was accepted", link)
		} else if strings.Contains(err.Error(), link[4:]) {
			t.Errorf("%q: the refusal repeats the link: %v", link, err)
		}
	}
}

// Pseudo-random links over a hostile alphabet: nothing panics, and whatever is accepted is a
// dial target of the permitted characters.
func TestDialLinkFuzzIsBounded(t *testing.T) {
	alphabet := []rune("sip:tel%+@[]:;?&|$`'\"\\\r\n\t 0aZ#*-._/()\x00é")
	state := uint64(0x9e3779b97f4a7c15)
	for range 20_000 {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		link := []rune("tel:")
		if state&1 == 0 {
			link = []rune("sip:")
		}
		x := state
		for range state % 40 {
			x = x*6_364_136_223_846_793_005 + 1
			link = append(link, alphabet[(x>>33)%uint64(len(alphabet))])
		}
		target, err := ParseDialLink(string(link))
		if err != nil {
			continue
		}
		value := target.Value()
		if len(value) == 0 || len(value) > 64 || strings.ContainsFunc(value, func(c rune) bool {
			return !alnum(c) && !strings.ContainsRune("*#+-_.", c)
		}) {
			t.Fatalf("%q was accepted as %q", string(link), value)
		}
	}
}
