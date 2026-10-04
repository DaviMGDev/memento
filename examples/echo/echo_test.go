// Package main's test loads the echo WASM plugin end to end: build the
// module with ./build.sh, then run `go test ./examples/echo`.
package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	mcontext "github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/loader"
	"github.com/DaviMGDev/memento/plugins/wasm"
	"github.com/DaviMGDev/memento/runtime"
)

// buffer is a concurrency-safe sink for guest log output.
type buffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitForState(t *testing.T, s *runtime.Scheduler, id mcontext.FiberID, want runtime.State) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := s.Inspect(id); ok && info.State == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	info, ok := s.Inspect(id)
	t.Fatalf("fiber %d did not reach %q; last = %+v (ok=%v)", id, want, info, ok)
}

func TestEchoPlugin(t *testing.T) {
	ctx := context.Background()

	engine, err := wasm.NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	var logged buffer
	registry := loader.NewRegistry()
	if err := wasm.Register(registry, "echo", engine, echoWasm, wasm.WithLogWriter(&logged)); err != nil {
		t.Fatalf("wasm.Register: %v", err)
	}

	sched := runtime.New()
	defer sched.Close()
	ldr := loader.New(sched, registry)

	tree, err := loader.NewTree(loader.Entry{
		ID:        "echo-1",
		Component: "echo",
		Payload:   "hello wasm",
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("loader.NewTree: %v", err)
	}

	// 1. Load and activate: the plugin echoes its payload to the log.
	if err := ldr.Reconcile(tree); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	fiberID, ok := ldr.Fiber("echo-1")
	if !ok {
		t.Fatal("expected fiber for echo-1 to exist")
	}
	waitForState(t, sched, fiberID, runtime.StateActive)

	if got := logged.String(); !strings.Contains(got, "echo: hello wasm\n") {
		t.Fatalf("expected echoed payload in log, got %q", got)
	}

	// 2. Unload: the effect inverse runs and says goodbye.
	empty, err := loader.NewTree()
	if err != nil {
		t.Fatalf("loader.NewTree(): %v", err)
	}
	if err := ldr.Reconcile(empty); err != nil {
		t.Fatalf("Reconcile(empty): %v", err)
	}
	if _, ok := ldr.Fiber("echo-1"); ok {
		t.Fatal("expected fiber for echo-1 to be removed")
	}
	if got := logged.String(); !strings.Contains(got, "echo: reverted effect\n") {
		t.Fatalf("expected revert message in log, got %q", got)
	}
}
