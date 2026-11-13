# Developer entry points. Every recipe runs in the build image (scripts/go-docker.sh): the host
# needs only Docker and make. See docs/DEVELOPMENT.md. The Rust application this one replaced is
# under archive/rust/ and is not built from here.
#
#   make help      list targets
#   make dist      the artifacts into dist/go/

SHELL := /bin/sh
.DEFAULT_GOAL := help

.PHONY: help image check test build dist lab scan

help: ## Show this help
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z_-]+:.*## / { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

image: go-image ## Build the pinned build image
check: go-check ## gofmt and go vet
test: go-test   ## Unit tests with the race detector
build: go-build ## The Linux binary
dist: go-dist   ## Every artifact, with checksums, notices and the bill of materials
lab: go-lab     ## The lab's scenarios against the disposable PBX

scan: ## Publication scan of the index and the history (add artifact folders: make scan SCAN=dist/go)
	sh build/release/scan-publication.sh $(SCAN)

include build/go.mk
