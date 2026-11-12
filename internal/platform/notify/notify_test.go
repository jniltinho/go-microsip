package notify

import (
	"strings"
	"testing"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// The caller's text is the other party's: what would reorder or hide text is removed, what a
// notification server reads as markup is escaped, and the length is bounded.
func TestCallerTextIsPlain(t *testing.T) {
	for caller, want := range map[string]string{
		"1002":                               "1002",
		"Ana Lima":                           "Ana Lima",
		`<img src=x onerror=alert(1)><b>Ana`: "&lt;img src=x onerror=alert(1)&gt;&lt;b&gt;Ana",
		"Tom & Jerry":                        "Tom &amp; Jerry",
		"line\nbreak\ttab\x00":               "linebreaktab",
		"safe‮evil⁦x‏":                       "safeevilx",
		"":                                   "Unknown caller",
		" \t\n":                              "Unknown caller",
		"‮‏":                                 "Unknown caller",
	} {
		if got := callerText(caller); got != want {
			t.Errorf("%q: %q, want %q", caller, got, want)
		}
	}
	if long := callerText(strings.Repeat("é", 200)); len([]rune(long)) != maxCallerChars {
		t.Errorf("a long caller is %d characters, want %d", len([]rune(long)), maxCallerChars)
	}
}

func TestAToastNamesTheCallInItsButtons(t *testing.T) {
	xml := toastXML(7, callerText(`Ana <b>&"x"`))
	for _, want := range []string{`<text>Incoming call</text>`, `<text>Ana &lt;b&gt;&amp;"x"</text>`, `content="Answer" arguments="answer:7"`,
		`content="Reject" arguments="reject:7"`, `launch="show:7"`} {
		if !strings.Contains(xml, want) {
			t.Errorf("the toast lacks %s:\n%s", want, xml)
		}
	}
	for arguments, want := range map[string]ports.CallNoticeAction{"answer:7": {Call: 7, Answer: true}, "reject:12": {Call: 12}} {
		if got, ok := toastAction(arguments); !ok || got != want {
			t.Errorf("%q read as %+v, %v", arguments, got, ok)
		}
	}
	// A click on the notification itself, and anything that is not one of the two buttons.
	for _, arguments := range []string{"show:7", "", "answer", "answer:", "answer:x", "answer:-1", "hangup:7", "answer:7:8"} {
		if got, ok := toastAction(arguments); ok {
			t.Errorf("%q read as a decision: %+v", arguments, got)
		}
	}
}
