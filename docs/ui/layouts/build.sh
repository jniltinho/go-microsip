#!/bin/sh
# Rebuilds the layout proposals: the FigCraft sources (src/*.figcraft), one screenshot per screen
# (prints/<layout>/<screen>-<skin>.png, at 2x), one overview per layout and contrast.md.
#
# Needs python3 and FigCraft's headless CLI, which ships inside the FigCraft AppImage:
#   FIGCRAFT_CLI="/path/to/figcraft.AppImage cli" docs/ui/layouts/build.sh
#   FIGCRAFT_CLI=/path/to/figcraft-cli docs/ui/layouts/build.sh
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
cli="${FIGCRAFT_CLI:?set FIGCRAFT_CLI to figcraft-cli (or \"<FigCraft AppImage> cli\")}"
scripts="$(mktemp -d)"
trap 'rm -rf "$scripts"' EXIT

python3 "$here/generate.py" "$scripts" "$here/contrast.md"
mkdir -p "$here/src"
for script in "$scripts"/*.json; do
  layout="$(basename "$script" .json)"
  # A failed step prints one line with "ok": false and rolls everything back.
  $cli run --no-system-fonts "$script" --out "$here/src/$layout.figcraft" | grep '"ok":false' && exit 1
  rm -rf "$here/prints/$layout"
  mkdir -p "$here/prints/$layout"
  $cli export --no-system-fonts "$here/src/$layout.figcraft" -o "$here/prints/$layout" --scale 2
  $cli render --no-system-fonts "$here/src/$layout.figcraft" -o "$here/prints/$layout-overview.png"
done
