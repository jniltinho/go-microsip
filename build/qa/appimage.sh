#!/bin/sh
# AppImage check (run inside the build container): unpacked without running the program, the
# AppImage starts with the pinned runtime and holds the same program as the plain executable,
# its launcher, desktop entry, icon, the license and the notices, and nothing else.
#   build/qa/appimage.sh <AppImage> <plain executable>
set -eu
image=$(realpath "$1")
plain=$(realpath "$2")
runtime=/usr/local/share/appimage-runtime-x86_64
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"
# appimagetool writes the payload's MD5 digest into a section of the runtime: 16 bytes at most
# may differ from the pinned file.
differ=$(head -c "$(stat -c %s "$runtime")" "$image" | cmp -l - "$runtime" | wc -l)
[ "$differ" -le 16 ] || { echo "appimage: the runtime in front is not the pinned one ($differ bytes differ)" >&2; exit 1; }
"$image" --appimage-extract >/dev/null
cmp squashfs-root/usr/bin/go-microsip "$plain" ||
	{ echo "appimage: the program inside is not $2" >&2; exit 1; }
found=$(cd squashfs-root && find . -type f -o -type l | LC_ALL=C sort | tr '\n' ' ')
expected="./.DirIcon ./AppRun ./go-microsip.desktop ./go-microsip.png ./usr/bin/go-microsip ./usr/share/doc/go-microsip/LICENSE ./usr/share/doc/go-microsip/THIRD_PARTY_NOTICES.txt ./usr/share/icons/hicolor/scalable/apps/go-microsip.svg "
[ "$found" = "$expected" ] || { echo "appimage: it holds: $found" >&2; exit 1; }
echo "appimage: the pinned runtime in front, the same program as $(basename "$plain"), and the 8 expected files"
