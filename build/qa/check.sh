#!/bin/sh
# Static checks (run inside the build container): gofmt, then go vet for every build the project
# ships or supports: CGO-free Linux with the window, cgo Linux, Linux without a window, and Windows.
#   build/qa/check.sh <packages...>
set -eu
unformatted=$(gofmt -l main.go cmd internal build/winicon build/macos)
if [ -n "$unformatted" ]; then
	echo "$unformatted"
	echo "run gofmt -w" >&2
	exit 1
fi
CGO_ENABLED=0 go vet -tags purego "$@"
CGO_ENABLED=1 go vet "$@"
# Without cgo and without the purego tag there is no window: every other package must still build.
packages=$(CGO_ENABLED=0 go list "$@") # on its own line: a failure here stops the check
# shellcheck disable=SC2046 # the package list is a word list
CGO_ENABLED=0 go vet $(printf '%s\n' "$packages" | grep -v '/internal/gui$')
CGO_ENABLED=0 GOOS=windows go vet "$@"
# The core must build for a system no adapter exists for yet.
if [ -d internal/core ]; then
	CGO_ENABLED=0 GOOS=darwin go vet ./internal/core/...
fi
echo "check: gofmt and go vet OK (purego, cgo, no window, windows, core on darwin)"
