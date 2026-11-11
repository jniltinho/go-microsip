#!/bin/sh
# Lab call through the softphone's binary with real sound (openspec/changes/migrate-to-go-wails,
# task 3.8): `go-microsip run --call 600` with PipeWire's virtual sound cards against the
# disposable PBX, in plain and in secure mode.
#
#   sh docker/asterisk/go-call.sh         10 seconds of measured audio per account
#   sh docker/asterisk/go-call.sh 300     the five-minute threshold run
#
# Each run gets new fictional accounts and a new lab CA under the git-ignored
# artifacts/go-call/<time>/, builds the binary and the test without cgo in the Go build image, and
# tears the PBX down afterwards. Only disposable lab paths are mounted; never HOME, base/, the
# session's D-Bus or sound, or the Docker socket. The lab image must exist:
# sh docker/asterisk/lab.sh build.
set -eu
cd "$(dirname "$0")/../.."
LAB_UID="$(id -u)"
LAB_GID="$(id -g)"
export LAB_UID LAB_GID
if [ "$LAB_UID" -eq 0 ]; then echo "ERROR: run the lab as a non-root user." >&2; exit 1; fi
test ! -L artifacts
image=rust-microsip-asterisk:22.11.0-lab.1

run="$(pwd)/artifacts/go-call/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p artifacts/go-call
mkdir -m 700 "$run" "$run/lab"

docker run --rm --network none --user "$LAB_UID:$LAB_GID" --read-only \
	--cap-drop ALL --security-opt no-new-privileges \
	--tmpfs /tmp:rw,noexec,nosuid,nodev,mode=1777,size=32m \
	--mount "type=bind,source=$run/lab,target=/lab" \
	"$image" generate >/dev/null

mkdir -p dist/go/call
scripts/go-docker.sh sh -c 'CGO_ENABLED=0 go build -trimpath -o dist/go/call/go-microsip . &&
	CGO_ENABLED=0 go test -c -o dist/go/call/audiocall.test ./internal/platform/audio &&
	cp build/qa/audio-call.sh dist/go/call/'

# The session bus looks its user up: a passwd file with only the lab user, not the host's.
mkdir "$run/etc"
printf 'lab:x:%s:%s:lab:/tmp:/bin/sh\n' "$LAB_UID" "$LAB_GID" >"$run/etc/passwd"
printf 'lab:x:%s:\n' "$LAB_GID" >"$run/etc/group"

GO_CALL_LAB="$run/lab"
GO_CALL_ETC="$run/etc"
GO_CALL_SPIKE="$(pwd)/dist/go/call"
GO_CALL_IMAGE="${GO_IMAGE:-go-microsip-build:13}"
GO_CALL_SECONDS="${1:-10}"
export GO_CALL_LAB GO_CALL_ETC GO_CALL_SPIKE GO_CALL_IMAGE GO_CALL_SECONDS
compose() { docker compose -f docker/asterisk/compose.yaml -f docker/asterisk/compose.go-call.yaml "$@"; }

cleanup() {
	compose logs --no-color >"$run/compose.log" 2>&1 || :
	compose down --volumes --remove-orphans >/dev/null 2>&1 || :
	echo "run directory: ${run#"$(pwd)"/}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

status=0
timeout --signal=TERM --kill-after=20s 1500s docker compose \
	-f docker/asterisk/compose.yaml -f docker/asterisk/compose.go-call.yaml \
	run --rm test-runner || status=$?
exit "$status"
