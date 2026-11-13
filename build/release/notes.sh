#!/bin/sh
# Prints the notes of a GitHub release: docs/release/vVERSION.md, then the checksums of the
# published files. A release page resolves a relative link against itself, so the links to files
# of the repository are made absolute, at the tag; one this does not know how to rewrite fails.
#   sh build/release/notes.sh VERSION OWNER/REPO [SHA256SUMS]
set -eu
version=$1 repo=$2 sums=${3:-}
notes=$(dirname "$0")/../../docs/release/v$version.md
base=https://github.com/$repo/blob/v$version
if [ -f "$notes" ]; then
	body=$(sed -e "s|](\.\./\.\./|]($base/|g" -e "s|](\.\./|]($base/docs/|g" "$notes")
else
	body="go-microsip $version"
fi
if left=$(printf '%s\n' "$body" | grep -oE '\]\([^)]*\)' | grep -vE '^\]\((https?://|#)'); then
	echo "relative links the release page cannot resolve:" >&2
	printf '%s\n' "$left" >&2
	exit 1
fi
printf '%s\n' "$body"
if [ -n "$sums" ]; then
	printf '\n## Checksums (SHA-256)\n\n```\n'
	cat "$sums"
	printf '```\n'
fi
