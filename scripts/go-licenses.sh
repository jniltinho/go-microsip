#!/bin/sh
# Write the third-party notices of the Go binary and the embedded frontend (run inside the build
# container): every Go module linked into the Linux or Windows binary, every npm package of the
# production bundle and every bundled font, each with its version and license text. With a second
# file name, the same components are also written there as a CycloneDX 1.5 bill of materials.
#   scripts/go-licenses.sh <output file> [<sbom file> <version>]
set -eu
out=$1
sbom=${2:-}
release=${3:-} # the loop below reads each module's version into $version
modules=$(mktemp)
parts=$(mktemp) # one line per component: kind, name, version, declared license
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
		printf 'golang\t%s\t%s\t\n' "$path" "$version" >>"$parts"
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
			node -p "const p=require('$dir/package.json'); ['npm',p.name,p.version,typeof p.license==='string'?p.license:''].join('\t')" >>"$parts"
			license_of "$dir" || echo "(no license file in the package; license as declared above)"
		done
	echo
	echo "## Fonts (embedded frontend, unmodified; see frontend/src/fonts/README.md)"
	for f in frontend/src/fonts/licenses/*.txt; do
		echo
		echo "--- font $(basename "$f" .txt)"
		printf 'font\t%s\t\t\n' "$(basename "$f" .txt)" >>"$parts"
		cat "$f"
	done
} >"$out"
echo "licenses: $(grep -c '^--- ' "$out") components in $out"
[ -n "$sbom" ] || exit 0
# The bill of materials names what the notices name, no more and no less: it is refused otherwise.
node - "$parts" "$out" "$sbom" "$release" <<'EOF'
const fs = require("fs");
const [parts, notices, out, version] = process.argv.slice(2);
const components = fs.readFileSync(parts, "utf8").split("\n").filter(Boolean).map((line) => {
  const [kind, name, ver, license] = line.split("\t");
  const c = { type: kind === "font" ? "data" : "library", name };
  if (ver) c.version = ver;
  if (kind !== "font") c.purl = `pkg:${kind}/${name.replace(/^@/, "%40")}@${ver}`;
  if (license) c.licenses = [{ expression: license }];
  return c;
});
const noticed = fs.readFileSync(notices, "utf8").split("\n").filter((l) => l.startsWith("--- "))
  .map((l) => l.slice(4).replace(/ \(.*\)$/, "").replace(/^font /, ""));
const listed = components.map((c) => (c.version ? `${c.name} ${c.version}` : c.name));
const missing = noticed.filter((n) => !listed.includes(n)), extra = listed.filter((n) => !noticed.includes(n));
if (missing.length || extra.length || !components.length) {
  console.error(`sbom: not the components of the notices: missing ${JSON.stringify(missing)}, extra ${JSON.stringify(extra)}`);
  process.exit(1);
}
fs.writeFileSync(out, JSON.stringify({
  bomFormat: "CycloneDX", specVersion: "1.5", version: 1,
  metadata: { component: { type: "application", name: "go-microsip", version,
    licenses: [{ license: { id: "GPL-3.0-or-later" } }] } },
  components,
}, null, 2) + "\n");
const count = (t) => components.filter((c) => (c.purl || "font").startsWith(t)).length;
console.log(`sbom: ${components.length} components in ${out}: ${count("pkg:golang")} Go modules, ${count("pkg:npm")} npm packages, ${count("font")} fonts`);
EOF
