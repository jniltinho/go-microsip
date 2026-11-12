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

func TestAToastNamesTheRunAndTheCallInItsButtons(t *testing.T) {
	now := newToasts()
	run := now.run
	xml := now.xml(7, callerText(`Ana <b>&"x"`))
	for _, want := range []string{`<text>Incoming call</text>`, `<text>Ana &lt;b&gt;&amp;"x"</text>`, `content="Answer" arguments="answer:` + run + `:7"`,
		`content="Reject" arguments="reject:` + run + `:7"`, `launch="show:` + run + `:7"`} {
		if !strings.Contains(xml, want) {
			t.Errorf("the toast lacks %s:\n%s", want, xml)
		}
	}
	// A click on the notification itself, and anything that is not one of the two buttons.
	for _, arguments := range []string{"show:" + run + ":7", "", "answer", "answer:7", "answer:" + run + ":", "answer:" + run + ":x",
		"answer:" + run + ":-1", "hangup:" + run + ":7", "answer:" + run + ":7:8", "answer:" + run + ":8"} {
		if got, ok := now.pressed(arguments); ok {
			t.Errorf("%q read as a decision: %+v", arguments, got)
		}
	}
	// A notification left by an earlier run names a call number that this run uses again.
	earlier := newToasts()
	earlier.xml(7, "Bia")
	if earlier.run == run || len(run) != 16 {
		t.Fatalf("two runs are named %q and %q", earlier.run, run)
	}
	if got, ok := now.pressed("answer:" + earlier.run + ":7"); ok {
		t.Errorf("a button of an earlier run decided call 7: %+v", got)
	}
	// The button decides its call once, and nothing after the call stopped ringing.
	if got, ok := now.pressed("answer:" + run + ":7"); !ok || got != (ports.CallNoticeAction{Call: 7, Answer: true}) {
		t.Errorf("the Answer button read as %+v, %v", got, ok)
	}
	if got, ok := now.pressed("reject:" + run + ":7"); ok {
		t.Errorf("a second button decided the call again: %+v", got)
	}
	now.xml(12, "Caio")
	now.closed(12)
	if got, ok := now.pressed("reject:" + run + ":12"); ok {
		t.Errorf("a button decided a call that no longer rings: %+v", got)
	}
	now.xml(13, "Dora")
	if got, ok := now.pressed("reject:" + run + ":13"); !ok || got != (ports.CallNoticeAction{Call: 13}) {
		t.Errorf("the Reject button read as %+v, %v", got, ok)
	}
}
