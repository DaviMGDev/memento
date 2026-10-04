#!/usr/bin/env sh
# Builds echo.wasm as a freestanding core WebAssembly module.
#
# Uses zig's bundled clang and wasm-ld, so no extra toolchain is needed.
# Any clang with wasm-ld works too:
#
#   clang --target=wasm32 -O2 -nostdlib -Wl,--no-entry -Wl,--export-memory \
#         -o echo.wasm guest/echo.c
set -eu
cd "$(dirname "$0")"
exec zig cc -target wasm32-freestanding -O2 -nostdlib \
	-Wl,--no-entry -Wl,--export-memory \
	-o echo.wasm guest/echo.c
