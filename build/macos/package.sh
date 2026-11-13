#!/bin/sh
# Build the macOS universal application and its zip (run inside the build container): the two
# slices cross-built without cgo, the window through the toolkit's purego backend, joined and
# bundled by build/macos/package. No Apple tool is used. The slices stay in <dist dir>/macos for
# the check of the zip; they are not published.
#   build/macos/package.sh <dist dir> <name-version> <ldflags> <version> <commit>
set -eu
dist=$1
name=$2
ldflags=$3
version=$4
commit=$5
slices=$dist/macos
mkdir -p "$slices"
for arch in amd64 arm64; do
	CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -tags "production purego" -trimpath \
		-ldflags "$ldflags" -o "$slices/go-microsip-darwin-$arch" .
done
out=$dist/$name-macos-universal.zip
go run ./build/macos/package -version "$version" -commit "$commit" -icon build/linux/go-microsip.png \
	-license LICENSE -notices "$dist/THIRD_PARTY_NOTICES.txt" -out "$out.new" \
	"$slices/go-microsip-darwin-amd64" "$slices/go-microsip-darwin-arm64"
mv -f "$out.new" "$out"
