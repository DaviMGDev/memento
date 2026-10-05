# chat — a Go→wasm echo REPL

An interactive echo chat guest written in Go and compiled to WebAssembly
with Go's `wasip1` port. It provides the context key `chat` and binds the
nickname as its value on activation, registers an effect on activation, then
hosts a chat loop: lines arrive on WASI stdin and are echoed back through the
memento `log` import. `:quit` (or Ctrl-D) ends the session, and unload runs
the effect inverse.

## Try it

```console
$ ./build.sh                  # GOOS=wasip1 GOARCH=wasm -> chat.wasm
$ go run ./examples/chat
chat: echo joined (type :quit or press Ctrl-D to leave)
you> hello there
echo: hello there
you> :quit
chat: session closed
```

Pass a nickname with `-nick`; it travels to the guest as the activation
payload:

```console
$ go run ./examples/chat -nick bubbles
```

Run the scripted end-to-end test (no rebuild needed; `chat.wasm` is
committed and embedded):

```console
$ go test ./examples/chat -v
```

## How Go reaches wasm here

`GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared` produces a **WASI
reactor** module. Two host-side pieces make it work with the memento loader:

- **WASI host functions** — the engine instantiates
  `wasi_snapshot_preview1`, so the Go runtime's imports (clock, random,
  `fd_write`, ...) resolve. `wasm.WithStdin` / `WithStdout` /
  `WithStderr` wire the guest's file descriptors.
- **Reactor initialization** — the component calls the module's init export
  after instantiation: `_initialize` (wasi-libc) or
  `_rt0_wasm_wasip1_lib` (Go). Without it, calling any `memento_*` export
  aborts.

Guest exports/imports are declared with `//go:wasmexport` and
`//go:wasmimport`; see [`guest/chat.go`](guest/chat.go).

## Loader vs. scheduler

The scripted test goes through `loader.Reconcile`, which waits for the
runtime to be quiescent. An interactive session blocks activation until the
user quits, outliving the loader's quiescence window — so the demo drives
the scheduler directly (`Insert` / `Inspect` / `Remove`) and the guest's
activation *is* the chat loop.
