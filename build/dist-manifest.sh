#!/bin/sh
# Write SHA256SUMS and build-manifest.json for the milestone artifacts in the dist directory.
#   build/dist-manifest.sh <dist dir> <name-version> <version> <commit> <window backend> <image>
set -eu
dist=$1
name=$2
version=$3
commit=$4
window=$5
image=$6
if [ -z "$(git status --porcelain)" ]; then dirty=false; else dirty=true; fi
cd "$dist"
cat >build-manifest.json <<EOF
{
  "version": "$version",
  "commit": "$commit",
  "dirty": $dirty,
  "linux_window_backend": "$window",
  "build_image": "$image",
  "artifacts": [
    "$name-linux-amd64",
    "$name-x86_64.AppImage",
    "$name-windows-amd64.exe",
    "$name-windows-amd64.zip"
  ]
}
EOF
sha256sum "$name-linux-amd64" "$name-x86_64.AppImage" "$name-windows-amd64.exe" \
	"$name-windows-amd64.zip" THIRD_PARTY_NOTICES.txt sbom.cdx.json build-manifest.json >SHA256SUMS
sha256sum --check --strict --quiet SHA256SUMS
echo "dist: $(wc -l <SHA256SUMS) files in SHA256SUMS, commit $commit, dirty $dirty"
