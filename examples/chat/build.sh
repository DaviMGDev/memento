#!/usr/bin/env sh
# Builds chat.wasm from the Go guest using Go's wasip1 port.
#
# The -buildmode=c-shared build produces a WASI reactor module: the loader
# initializes it via _rt0_wasm_wasip1_lib and then drives the memento_*
# exports.
set -eu
cd "$(dirname "$0")"
GOOS=wasip1 GOARCH=wasm go build \
	-buildmode=c-shared \
	-trimpath -ldflags="-s -w" \
	-o chat.wasm ./guest
ls -l chat.wasm
