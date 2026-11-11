package gui

import "github.com/ebitengine/purego"

// setWindowIconName gives the windows of this process a themed icon name.
//
// GTK 4 on Wayland sends the compositor an icon for each window (xdg-toplevel-icon); without a name
// it sends an empty one. KWin then falls back to the desktop entry for the title bar, but the
// Plasma task bar shows the empty icon: a blank entry. The name is the icon installDesktopEntry
// puts in the user's icon theme. The toolkit binding has no call for this, so GTK is asked
// directly; if the library or the symbol is not there, the window simply keeps GTK's default.
func setWindowIconName(name string) {
	defer func() { _ = recover() }() // a missing symbol panics in RegisterLibFunc
	gtk, err := purego.Dlopen("libgtk-4.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return
	}
	var setDefaultIconName func(string)
	purego.RegisterLibFunc(&setDefaultIconName, gtk, "gtk_window_set_default_icon_name")
	setDefaultIconName(name)
}
