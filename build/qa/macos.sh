#!/bin/sh
# macOS zip check (run inside the build container), without running the program and without the
# program that made the zip: it holds exactly the bundle's six files; its executable is a
# universal one whose x86_64 and arm64 slices are, byte for byte, the two given, each on a page
# boundary (so the arm64 slice's signature is the linker's); and the information file names the
# application, both link schemes, the reasons for the microphone and the local network, and a
# minimum macOS that is not below what either slice needs.
#   build/qa/macos.sh <zip> <darwin-amd64 slice> <darwin-arm64 slice>
set -eu
zip=$(realpath "$1")
intel=$(realpath "$2")
apple=$(realpath "$3")
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail() {
	echo "macos: $*" >&2
	exit 1
}
found=$(unzip -Z1 "$zip" | LC_ALL=C sort | tr '\n' ' ')
app=go-microsip.app/Contents
expected="$app/Info.plist $app/MacOS/go-microsip $app/PkgInfo $app/Resources/LICENSE.txt $app/Resources/THIRD_PARTY_NOTICES.txt $app/Resources/go-microsip.icns "
[ "$found" = "$expected" ] || fail "it holds: $found"
unzip -q "$zip" -d "$work"
cd "$work/$app"
[ -x MacOS/go-microsip ] || fail "the program in the bundle is not executable"
[ -s Resources/LICENSE.txt ] && [ -s Resources/THIRD_PARTY_NOTICES.txt ] || fail "the licence or the notices are empty"

# The fat header, big-endian: the magic number, the count, then cpu, subtype, offset, size and
# alignment of each slice.
word() { od -An -tx1 -j "$1" -N 4 MacOS/go-microsip | tr -d ' \n'; }
[ "$(word 0)" = cafebabe ] || fail "the program is not a universal executable"
[ "$(word 4)" = 00000002 ] || fail "the program holds $(word 4) slices, not two"
# The lowest macOS a slice runs on: its build-version command names the platform (1 is macOS)
# and the version, packed as major (16 bits), minor and patch, little-endian.
needs() {
	packed=$(od -An -tx1 -N 8192 "$1" | tr -d ' \n' | grep -o -E '32000000(18|20|28)00000001000000[0-9a-f]{8}' | head -n 1 | tail -c 9)
	[ -n "$packed" ] || fail "$(basename "$1") does not say which macOS it needs"
	echo $((0x$(echo "$packed" | cut -c7-8)$(echo "$packed" | cut -c5-6) * 10000 + 0x$(echo "$packed" | cut -c3-4) * 100 + 0x$(echo "$packed" | cut -c1-2)))
}
lowest=0
for index in 0 1; do
	at=$((8 + 20 * index))
	cpu=$(word "$at")
	offset=$((0x$(word $((at + 8)))))
	size=$((0x$(word $((at + 12)))))
	case $cpu in
	01000007) given=$intel ;;
	0100000c) given=$apple ;;
	*) fail "a slice for the processor $cpu" ;;
	esac
	[ $((offset % 16384)) -eq 0 ] || fail "the slice for $cpu does not start on a page"
	tail -c +"$((offset + 1))" MacOS/go-microsip | head -c "$size" | cmp -s - "$given" ||
		fail "the slice for $cpu is not $(basename "$given"), byte for byte"
	need=$(needs "$given")
	[ "$need" -le "$lowest" ] || lowest=$need
done
[ "$(word 8)" != "$(word 28)" ] || fail "the same processor twice"

key() { grep -A1 -F "<key>$1</key>" Info.plist | sed -n 's|.*<string>\(.*\)</string>.*|\1|p'; }
[ "$(key CFBundleIdentifier)" = io.github.jniltinho.go-microsip ] || fail "the bundle identifier is $(key CFBundleIdentifier)"
[ "$(key CFBundleExecutable)" = go-microsip ] || fail "the bundle's executable is $(key CFBundleExecutable)"
[ -n "$(key NSMicrophoneUsageDescription)" ] || fail "no reason for the microphone"
[ -n "$(key NSLocalNetworkUsageDescription)" ] || fail "no reason for the local network"
grep -q -F '<string>sip</string>' Info.plist && grep -q -F '<string>tel</string>' Info.plist || fail "the sip and tel schemes are not both declared"
! grep -q -F LSUIElement Info.plist || fail "the application is hidden from the Dock"
declared=$(key LSMinimumSystemVersion)
IFS=. read -r major minor patch <<EOT
$declared
EOT
[ $((major * 10000 + ${minor:-0} * 100 + ${patch:-0})) -ge "$lowest" ] ||
	fail "the bundle asks for macOS $declared, below what a slice needs"
echo "macos: the bundle's six files, a universal program whose slices are $(basename "$intel") and $(basename "$apple"), and the declarations, for macOS $declared and later"
