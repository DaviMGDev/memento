// Package main's test drives the chat guest through the loader with a
// scripted stdin transcript: build the module with ./build.sh, then run
// `go test ./examples/chat`.
package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/DaviMGDev/memento/loader"
	"github.com/DaviMGDev/memento/plugins/wasm"
	"github.com/DaviMGDev/memento/runtime"
)

// buffer is a concurrency-safe transcript sink.
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

func TestChatPlugin(t *testing.T) {
	ctx := context.Background()

	engine, err := wasm.NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	var logged buffer
	registry := loader.NewRegistry()
	err = wasm.Register(registry, "chat", engine, chatWasm,
		wasm.WithStdin(strings.NewReader("hello there\n:quit\n")),
		wasm.WithLogWriter(&logged),
	)
	if err != nil {
		t.Fatalf("wasm.Register: %v", err)
	}

	sched := runtime.New()
	defer sched.Close()
	ldr := loader.New(sched, registry)

	tree, err := loader.NewTree(loader.Entry{
		ID:        "chat-1",
		Component: "chat",
		Payload:   "alice",
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("loader.NewTree: %v", err)
	}

	// 1. Load and activate: the scripted session runs to :quit.
	if err := ldr.Reconcile(tree); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if _, ok := ldr.Fiber("chat-1"); !ok {
		t.Fatal("expected fiber for chat-1 to exist")
	}

	got := logged.String()
	for _, want := range []string{"alice joined", "you> ", "alice: hello there"} {
		if !strings.Contains(got, want) {
			t.Fatalf("transcript missing %q:\n%s", want, got)
		}
	}

	// 2. Unload: the effect inverse closes the session.
	empty, err := loader.NewTree()
	if err != nil {
		t.Fatalf("loader.NewTree(): %v", err)
	}
	if err := ldr.Reconcile(empty); err != nil {
		t.Fatalf("Reconcile(empty): %v", err)
	}
	if got := logged.String(); !strings.Contains(got, "chat: session closed") {
		t.Fatalf("transcript missing farewell:\n%s", got)
	}
}
