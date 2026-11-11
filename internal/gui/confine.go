package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// contentSecurityPolicy is sent with every asset: the page runs and shows only what is embedded in
// the binary. No inline or remote script, no eval, no remote style, font or image, no frame, no
// form target; requests go to the page's own origin only, which is how it calls Go.
const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// confine adds the content security policy to everything the asset server answers.
func confine(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// probeEvent asks the page of a self-test to report what probeScript left in it (see api.ts).
const probeEvent = "go-microsip:probe"

// probeScript is what a self-test runs in the page: what a script that got in would try. An inline
// script must not run. With navigate set it then tries to leave the page; a page that is still
// there to answer the probe event did not leave. The page adds to the report what its own scripts
// may do (api.ts): eval must not run and, where the webview is guarded, a new window must not open.
// Those two are asked there because a script the window injects runs with more rights than the
// page's own. Without a guard neither leaving the page nor a new window is tried: both would
// happen, and the toolkit gives no way to refuse them on that platform.
func probeScript(navigate bool) string {
	js := `(() => {
  const r = { inline: false, guarded: ` + fmt.Sprint(navigate) + ` };
  const s = document.createElement("script");
  s.textContent = "window.__goMicrosipInline = 1";
  document.head.appendChild(s);
  r.inline = window.__goMicrosipInline === 1;
  document.documentElement.dataset.probe = JSON.stringify(r);`
	if navigate {
		js += `
  location.href = "https://example.invalid/navigation";`
	}
	return js + "\n})()"
}

// probeVerdict judges a self-test's probe: what the page reported, whether this platform's webview
// can be guarded (expected) and is (guarded), how many navigations the guard refused, and the
// listening TCP sockets of this process (nil error when they could be listed). A guard that is
// expected and not in place fails the self-test: the page could leave.
func probeVerdict(result string, expected, guarded bool, blocked int64, listening []string, listErr error) (summary string, err error) {
	if expected && !guarded {
		return "", errors.New("self-test: the page is not confined: the navigation guard could not be installed")
	}
	// The page must have answered every question: a missing answer is not a pass.
	var r struct{ Inline, Eval, Opened *bool }
	if jsonErr := json.Unmarshal([]byte(result), &r); jsonErr != nil || r.Inline == nil || r.Eval == nil || guarded && r.Opened == nil {
		return "", fmt.Errorf("self-test: the page did not report the probe: %q", result)
	}
	var bad []error
	if *r.Inline {
		bad = append(bad, errors.New("an inline script ran"))
	}
	if *r.Eval {
		bad = append(bad, errors.New("eval ran"))
	}
	if r.Opened != nil && *r.Opened {
		bad = append(bad, errors.New("a new window opened"))
	}
	summary = "inline script and eval refused"
	switch {
	case !guarded:
		summary += "; a new window and navigation not probed (no guard on this platform)"
	case blocked < 1:
		bad = append(bad, errors.New("the guard did not refuse the navigation attempt"))
	default:
		summary += "; a new window and navigation to another address refused, and the page stayed"
	}
	switch {
	case listErr != nil:
		summary += "; listening sockets not checked on this platform"
	case len(listening) > 0:
		bad = append(bad, fmt.Errorf("the process listens on %v", listening))
	default:
		summary += "; no listening socket"
	}
	if len(bad) > 0 {
		return "", fmt.Errorf("self-test: the page is not confined: %w", errors.Join(bad...))
	}
	return summary, nil
}
