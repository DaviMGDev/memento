// Command chat runs the Go-to-wasm echo REPL chat guest through the memento
// runtime.
//
// Build the guest first with ./build.sh; chat.wasm is embedded at compile
// time, so this works from anywhere in the module:
//
//	$ go run ./examples/chat
//	$ go run ./examples/chat -nick bubbles
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"time"

	mcontext "github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/plugins/wasm"
	"github.com/DaviMGDev/memento/runtime"
)

//go:embed chat.wasm
var chatWasm []byte

func main() {
	nick := flag.String("nick", "echo", "nickname the guest announces")
	flag.Parse()

	ctx := context.Background()

	engine, err := wasm.NewEngine(ctx)
	must(err)
	defer engine.Close(ctx)

	comp, err := wasm.NewComponent(ctx, engine, chatWasm,
		wasm.WithModuleName("chat"),
		wasm.WithStdin(os.Stdin),
		wasm.WithLogWriter(os.Stdout),
	)
	must(err)

	sched := runtime.New()
	defer sched.Close()

	fiber, err := sched.Insert(comp, *nick)
	must(err)

	// Activation hosts the chat loop and only returns once the user quits,
	// so an interactive session outlives the loader's quiescence window.
	// This demo therefore drives the scheduler directly instead of the
	// loader.
	must(waitState(sched, fiber, runtime.StateActive))

	// Unload: the guest's effect inverse says goodbye.
	must(sched.Remove(fiber))
	waitGone(sched, fiber)
}

func waitState(sched *runtime.Scheduler, id mcontext.FiberID, want runtime.State) error {
	for {
		info, ok := sched.Inspect(id)
		if !ok {
			return fmt.Errorf("chat: fiber %d disappeared while waiting for %q", id, want)
		}
		if info.State == want {
			return nil
		}
		if info.State == runtime.StateFailed {
			return fmt.Errorf("chat: fiber %d failed", id)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitGone(sched *runtime.Scheduler, id mcontext.FiberID) {
	for {
		if _, ok := sched.Inspect(id); !ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "chat:", err)
		os.Exit(1)
	}
}
