# echo — a minimal WASM plugin

A guest plugin for the [`plugins/wasm`](../../plugins/wasm) loader: it
declares that it provides the context key `echo`, then on activation reads
its configuration payload through the memento host ABI and echoes it back to
the host log. On unload, its effect inverse says goodbye.

## Try it

```console
$ ./build.sh                      # compile guest/echo.c -> echo.wasm
$ go run ./examples/echo          # or: cd .. && go run ./examples/echo
-- load: echo plugin
echo: hello wasm
-- reload: payload changed
echo: reverted effect
echo: hello wasm (v2)
-- unload: everything
echo: reverted effect
```

Pass a custom payload with `-payload`:

```console
$ go run ./examples/echo -payload "custom text"
```

Run the end-to-end test (no zig needed; `echo.wasm` is committed and
embedded):

```console
$ go test ./examples/echo -v
```

`build.sh` defaults to zig's bundled clang; any `clang` with `wasm-ld`
works too (see the comment at the top of the script).

## Host side

```go
engine, _ := wasm.NewEngine(ctx)
defer engine.Close(ctx)

registry := loader.NewRegistry()
wasm.Register(registry, "echo", engine, wasmBytes, wasm.WithLogWriter(os.Stdout))

sched := runtime.New()
defer sched.Close()
ldr := loader.New(sched, registry)

tree, _ := loader.NewTree(loader.Entry{
    ID:        "echo-1",
    Component: "echo",
    Payload:   "hello wasm",
    Enabled:   true,
})
ldr.Reconcile(tree)
```

## ABI surface

| Direction | Name | Signature |
|---|---|---|
| export | `memento_declare` | `() -> i32` |
| export | `memento_activate` | `() -> i32` |
| export | `memento_revert_effect` | `(i32) -> i32` |
| export | `memory` | linear memory |
| import | `memento.declare_inject` | `(i32, i32) -> i32` |
| import | `memento.declare_provide` | `(i32, i32) -> i32` |
| import | `memento.get_payload_len` | `() -> i32` |
| import | `memento.get_payload` | `(i32, i32) -> i32` |
| import | `memento.register_effect` | `(i32) -> i32` |
| import | `memento.log` | `(i32, i32) -> i32` |

Strings cross the boundary as `(pointer, length)` pairs into the module's
exported memory; every function returns `0` on success.
