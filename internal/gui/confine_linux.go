package gui

import (
	"bufio"
	"os"
	"strings"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
)

// guard is the navigation guard of the window's webview. There is one window, and a purego
// callback cannot be freed, so this is process state.
var guard struct {
	origin  string // scheme and host of the page the webview loaded, such as wails://localhost
	blocked atomic.Int64
	ignore  func(decision uintptr)
	action  func(decision uintptr) uintptr
	request func(action uintptr) uintptr
	uri     func(request uintptr) string
}

// guardNavigation makes the webview of window (a GtkWindow) refuse to load anything but the page
// it has loaded: every navigation to another origin and every new window is ignored. It must run on
// the GTK thread, once the page is loaded, and reports whether the guard is in place.
//
// The toolkit binding has no call for this, so WebKitGTK is asked directly, as for the window's
// icon. The origin is read from the webview, not written here: it differs between platforms.
func guardNavigation(window unsafe.Pointer) (ok bool) {
	defer func() { // a missing library or symbol panics in RegisterLibFunc
		if recover() != nil {
			ok = false
		}
	}()
	open := func(name string) uintptr {
		lib, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			panic(err)
		}
		return lib
	}
	gtk, gobject, webkit := open("libgtk-4.so.1"), open("libgobject-2.0.so.0"), open("libwebkitgtk-6.0.so.4")
	var (
		windowChild func(uintptr) uintptr
		firstChild  func(uintptr) uintptr
		nextSibling func(uintptr) uintptr
		typeName    func(uintptr) string
		connect     func(object uintptr, signal string, handler, data, destroy uintptr, flags int32) uint64
		viewURI     func(uintptr) string
	)
	purego.RegisterLibFunc(&windowChild, gtk, "gtk_window_get_child")
	purego.RegisterLibFunc(&firstChild, gtk, "gtk_widget_get_first_child")
	purego.RegisterLibFunc(&nextSibling, gtk, "gtk_widget_get_next_sibling")
	purego.RegisterLibFunc(&typeName, gobject, "g_type_name_from_instance")
	purego.RegisterLibFunc(&connect, gobject, "g_signal_connect_data")
	purego.RegisterLibFunc(&viewURI, webkit, "webkit_web_view_get_uri")
	purego.RegisterLibFunc(&guard.ignore, webkit, "webkit_policy_decision_ignore")
	purego.RegisterLibFunc(&guard.action, webkit, "webkit_navigation_policy_decision_get_navigation_action")
	purego.RegisterLibFunc(&guard.request, webkit, "webkit_navigation_action_get_request")
	purego.RegisterLibFunc(&guard.uri, webkit, "webkit_uri_request_get_uri")

	var find func(widget uintptr) uintptr
	find = func(widget uintptr) uintptr {
		for ; widget != 0; widget = nextSibling(widget) {
			if typeName(widget) == "WebKitWebView" {
				return widget
			}
			if view := find(firstChild(widget)); view != 0 {
				return view
			}
		}
		return 0
	}
	view := find(windowChild(uintptr(window)))
	if view == 0 {
		return false
	}
	if guard.origin = originOf(viewURI(view)); guard.origin == "" {
		return false
	}
	return connect(view, "decide-policy", purego.NewCallback(decidePolicy), 0, 0, 0) != 0
}

// originOf is the scheme and host of a URI, or "" when it has none.
func originOf(uri string) string {
	scheme, rest, ok := strings.Cut(uri, "://")
	if !ok || scheme == "" {
		return ""
	}
	host, _, _ := strings.Cut(rest, "/")
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

// sameOrigin reports whether uri is a page of origin.
func sameOrigin(uri, origin string) bool {
	return origin != "" && (uri == origin || strings.HasPrefix(uri, origin+"/"))
}

// decidePolicy is WebKit's "decide-policy" handler: 0 is a navigation, 1 a new window, anything
// else a response to a load already allowed. Returning 1 says the decision was taken here.
func decidePolicy(_, decision uintptr, kind int32, _ uintptr) uintptr {
	if kind != 0 && kind != 1 {
		return 0
	}
	if kind == 0 && sameOrigin(guard.uri(guard.request(guard.action(decision))), guard.origin) {
		return 0
	}
	guard.ignore(decision)
	guard.blocked.Add(1)
	return 1
}

// navigationsBlocked is how many navigations the guard has refused.
func navigationsBlocked() int64 { return guard.blocked.Load() }

// listeningSockets lists the TCP sockets of this process that accept connections, as the kernel
// names them (address:port in hexadecimal).
func listeningSockets() ([]string, error) {
	fds, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return nil, err
	}
	mine := map[string]bool{}
	for _, fd := range fds {
		if target, err := os.Readlink("/proc/self/fd/" + fd.Name()); err == nil {
			if inode, ok := strings.CutPrefix(target, "socket:["); ok {
				mine[strings.TrimSuffix(inode, "]")] = true
			}
		}
	}
	var listening []string
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(table)
		if err != nil {
			continue // no IPv6 here
		}
		lines := bufio.NewScanner(f)
		for lines.Scan() {
			// sl local_address rem_address st tx:rx tr:when retrnsmt uid timeout inode ...
			if field := strings.Fields(lines.Text()); len(field) > 9 && field[3] == "0A" && mine[field[9]] {
				listening = append(listening, field[1])
			}
		}
		f.Close()
	}
	return listening, nil
}
