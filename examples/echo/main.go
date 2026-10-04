// Command echo loads the echo WASM plugin through the memento loader and
// demonstrates activation, payload reload, and unload with effect inversion.
//
// The plugin is built with ./build.sh and embedded at compile time, so
// `go run ./examples/echo` works from anywhere in the module:
//
//	$ go run ./examples/echo
//	$ go run ./examples/echo -payload "custom text"
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"os"

	"github.com/DaviMGDev/memento/loader"
	"github.com/DaviMGDev/memento/plugins/wasm"
	"github.com/DaviMGDev/memento/runtime"
)

//go:embed echo.wasm
var echoWasm []byte

func main() {
	payload := flag.String("payload", "hello wasm", "configuration payload handed to the plugin")
	flag.Parse()

	ctx := context.Background()

	engine, err := wasm.NewEngine(ctx)
	must(err)
	defer engine.Close(ctx)

	registry := loader.NewRegistry()
	must(wasm.Register(registry, "echo", engine, echoWasm, wasm.WithLogWriter(os.Stdout)))

	sched := runtime.New()
	defer sched.Close()
	ldr := loader.New(sched, registry)

	apply := func(label string, entries ...loader.Entry) {
		fmt.Printf("-- %s\n", label)
		tree, err := loader.NewTree(entries...)
		must(err)
		must(ldr.Reconcile(tree))
	}

	apply("load: echo plugin",
		loader.Entry{ID: "echo-1", Component: "echo", Payload: *payload, Enabled: true},
	)

	apply("reload: payload changed",
		loader.Entry{ID: "echo-1", Component: "echo", Payload: *payload + " (v2)", Enabled: true},
	)

	apply("unload: everything")
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "echo:", err)
		os.Exit(1)
	}
}
