#!/bin/sh
# Build the Windows amd64 executable and its portable zip (run inside the build container):
# WebView2, cross-built without cgo. -H windowsgui: no console window.
#   build/windows/package.sh <dist dir> <name-version> <ldflags>
set -eu
dist=$1
name=$2
ldflags=$3
exe=$dist/$name-windows-amd64.exe
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags production -trimpath \
	-ldflags "$ldflags -H windowsgui" -o "$exe" .
stage=$(mktemp -d)
folder=$name-windows-amd64
mkdir "$stage/$folder"
cp "$exe" "$stage/$folder/go-microsip.exe"
cp LICENSE "$stage/$folder/LICENSE.txt"
cp "$dist/THIRD_PARTY_NOTICES.txt" "$stage/$folder/"
out=$(pwd)/$dist/$folder.zip
(cd "$stage" && zip -q -r -X "$out.new" "$folder")
mv -f "$out.new" "$out"
echo "windows: $exe and $dist/$folder.zip"
