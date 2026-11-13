#!/bin/sh
# Go G0 spike against the disposable Asterisk lab (openspec/changes/migrate-to-go-wails, 2.3-2.7).
#
#   sh docker/asterisk/go-g0.sh                 every scenario
#   sh docker/asterisk/go-g0.sh echo/           only scenarios whose name starts with "echo/"
#   G0_DEBUG=1 sh docker/asterisk/go-g0.sh ...  SIP library diagnostics in the log
#
# Each run gets new fictional accounts and a new three-day lab CA under the git-ignored
# artifacts/go-g0/<time>/, builds the runner without cgo in the Go build image, and tears the PBX
# down afterwards. Only disposable lab paths are mounted; never HOME, base/, D-Bus or the Docker
# socket. The lab image must exist: sh docker/asterisk/lab.sh build.
set -eu
cd "$(dirname "$0")/../.."
LAB_UID="$(id -u)"
LAB_GID="$(id -g)"
export LAB_UID LAB_GID
if [ "$LAB_UID" -eq 0 ]; then echo "ERROR: run the lab as a non-root user." >&2; exit 1; fi
test ! -L artifacts
image=go-microsip-asterisk:22.11.0-lab.1

run="$(pwd)/artifacts/go-g0/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p artifacts/go-g0
mkdir -m 700 "$run" "$run/lab"
mkdir "$run/out"

docker run --rm --network none --user "$LAB_UID:$LAB_GID" --read-only \
	--cap-drop ALL --security-opt no-new-privileges \
	--tmpfs /tmp:rw,noexec,nosuid,nodev,mode=1777,size=32m \
	--mount "type=bind,source=$run/lab,target=/lab" \
	"$image" generate >/dev/null

mkdir -p dist/go/g0
scripts/go-docker.sh sh -c 'CGO_ENABLED=0 go build -trimpath -o dist/go/g0/g0lab ./internal/g0/cmd/g0lab'

GO_G0_LAB="$run/lab"
GO_G0_SPIKE="$(pwd)/dist/go/g0"
GO_G0_OUT="$run/out"
export GO_G0_LAB GO_G0_SPIKE GO_G0_OUT
compose() { docker compose -f docker/asterisk/compose.yaml -f docker/asterisk/compose.go-g0.yaml "$@"; }

cleanup() {
	compose logs --no-color >"$run/compose.log" 2>&1 || :
	compose down --volumes --remove-orphans >/dev/null 2>&1 || :
	echo "run directory: ${run#"$(pwd)"/}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

set -- ${1:+-run "$1"} ${G0_DEBUG:+-debug}
status=0
timeout --signal=TERM --kill-after=20s 900s docker compose \
	-f docker/asterisk/compose.yaml -f docker/asterisk/compose.go-g0.yaml \
	run --rm test-runner -lab /lab -server pbx -out /out/report.json "$@" || status=$?
exit "$status"
