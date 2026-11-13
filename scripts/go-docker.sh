#!/bin/sh
# Run a command in the Go build image (build/linux.Dockerfile), so the host needs only Docker.
#
#   scripts/go-docker.sh go test ./internal/...
#   GO_DOCKER_FLAGS="--network lab_net" scripts/go-docker.sh ./dist/go/go-microsip g0 register
#
# The checkout is mounted at /src. base/ and artifacts/ are local-only and are hidden from the
# container. Go's build and module caches persist in ~/.cache/go-microsip-build between runs.
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
image=${GO_IMAGE:-go-microsip-build:13}
cache=${GO_CACHE:-$HOME/.cache/go-microsip-build}
mkdir -p "$cache"

hide=
for d in base artifacts; do
	if [ -d "$root/$d" ]; then
		hide="$hide --mount type=tmpfs,destination=/src/$d"
	fi
done

# shellcheck disable=SC2086 # hide and GO_DOCKER_FLAGS are word lists
exec docker run --rm ${GO_DOCKER_FLAGS:-} --user "$(id -u):$(id -g)" -e HOME=/tmp \
	-e GOCACHE=/cache/go-build -e GOMODCACHE=/cache/gomod \
	-e GOFLAGS="${GOFLAGS:--buildvcs=false}" \
	-e npm_config_cache=/cache/npm \
	-v "$root:/src" -v "$cache:/cache" $hide -w /src "$image" "$@"
