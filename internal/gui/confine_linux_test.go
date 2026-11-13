package gui

import (
	"net"
	"testing"
)

// Only pages of the origin the webview loaded are allowed, whatever that origin is on this
// platform.
func TestOnlyThePagesOwnOriginIsAllowed(t *testing.T) {
	for uri, want := range map[string]string{
		"wails://localhost/":                 "wails://localhost",
		"wails://localhost/index.html#calls": "wails://localhost",
		"http://wails.localhost/":            "http://wails.localhost",
		"about:blank":                        "",
		"":                                   "",
		"wails:///nohost":                    "",
	} {
		if got := originOf(uri); got != want {
			t.Errorf("originOf(%q) = %q, want %q", uri, got, want)
		}
	}
	const origin = "wails://localhost"
	for uri, want := range map[string]bool{
		"wails://localhost":                           true,
		"wails://localhost/":                          true,
		"wails://localhost/assets/index.js":           true,
		"wails://localhost.example.invalid/":          false,
		"wails://localhostx/":                         false,
		"https://example.invalid/":                    false,
		"https://example.invalid/?wails://localhost/": false,
		"about:blank":                                 false,
		"data:text/html,<script>1</script>":           false,
		"":                                            false,
	} {
		if got := sameOrigin(uri, origin); got != want {
			t.Errorf("sameOrigin(%q) = %v, want %v", uri, got, want)
		}
	}
	if sameOrigin("anything", "") {
		t.Error("without an origin everything was allowed")
	}
}

// A listening socket of this process is found, and gone once it is closed: the self-test's "no
// listening socket" means something.
func TestListeningSocketsAreFound(t *testing.T) {
	before, err := listeningSockets()
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	during, err := listeningSockets()
	l.Close()
	if err != nil || len(during) != len(before)+1 {
		t.Errorf("with a listener: %v (before: %v), %v", during, before, err)
	}
	if after, _ := listeningSockets(); len(after) != len(before) {
		t.Errorf("after closing it: %v (before: %v)", after, before)
	}
}
