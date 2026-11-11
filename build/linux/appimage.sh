#!/bin/sh
# Build the x86_64 AppImage (run inside the build container): the binary, its desktop entry, icon
# and notices. GTK 4 and WebKitGTK 6.0 come from the system. Written as .new then renamed, so a
# running copy is replaced too.
#   build/linux/appimage.sh <binary> <dist dir> <name-version>
set -eu
bin=$1
dist=$2
name=$3
app=$(mktemp -d)/AppDir
install -D -m 0755 "$bin" "$app/usr/bin/go-microsip"
install -D -m 0644 LICENSE "$app/usr/share/doc/go-microsip/LICENSE"
install -D -m 0644 "$dist/THIRD_PARTY_NOTICES.txt" "$app/usr/share/doc/go-microsip/THIRD_PARTY_NOTICES.txt"
install -D -m 0644 build/linux/go-microsip.svg "$app/usr/share/icons/hicolor/scalable/apps/go-microsip.svg"
cp build/linux/go-microsip.png "$app/go-microsip.png"
install -m 0755 build/linux/AppRun "$app/AppRun"
cp build/linux/go-microsip.desktop "$app/"
desktop-file-validate "$app/go-microsip.desktop"
ARCH=x86_64 appimagetool --no-appstream --runtime-file /usr/local/share/appimage-runtime-x86_64 \
	"$app" "$dist/$name-x86_64.AppImage.new" >/dev/null
mv -f "$dist/$name-x86_64.AppImage.new" "$dist/$name-x86_64.AppImage"
echo "appimage: $dist/$name-x86_64.AppImage"
