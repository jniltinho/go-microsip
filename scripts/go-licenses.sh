#!/bin/sh
# Write the third-party notices of the Go binary and the embedded frontend (run inside the build
# container): every Go module linked into the Linux or Windows binary and every npm package of the
# production bundle, each with its version and license text.
#   scripts/go-licenses.sh <output file>
set -eu
out=$1
modules=$(mktemp)
{
	CGO_ENABLED=0 go list -tags "production purego" -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' .
	CGO_ENABLED=0 GOOS=windows go list -tags production -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' .
} | sort -u >"$modules"

license_of() {
	for f in "$1"/LICENSE* "$1"/LICENCE* "$1"/COPYING* "$1"/license* "$1"/UNLICENSE*; do
		if [ -f "$f" ]; then
			cat "$f"
			return 0
		fi
	done
	return 1
}

{
	echo "go-microsip third-party notices"
	echo "==============================="
	echo
	echo "go-microsip is GPL-3.0-or-later (see LICENSE). It includes the components below."
	echo
	echo "## Go modules"
	while read -r path version dir; do
		echo
		echo "--- $path $version"
		license_of "$dir" || {
			echo "go-licenses: no license file for $path" >&2
			exit 1
		}
	done <"$modules"
	echo
	echo "## npm packages (embedded frontend)"
	(cd frontend && npm ls --omit=dev --all --parseable 2>/dev/null | tail -n +2 | sort -u) |
		while read -r dir; do
			echo
			echo "--- $(node -p "const p=require('$dir/package.json'); p.name+' '+p.version+' ('+(p.license||'see package')+')'")"
			license_of "$dir" || echo "(no license file in the package; license as declared above)"
		done
} >"$out"
echo "licenses: $(grep -c '^--- ' "$out") components in $out"
