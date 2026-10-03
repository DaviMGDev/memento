package wasm

import (
	"context"
	"testing"

	"github.com/DaviMGDev/memento/loader"
	"github.com/DaviMGDev/memento/runtime"
)

func TestWASMReconcileIntegration(t *testing.T) {
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	reg := loader.NewRegistry()
	wasmBytes := buildStandaloneTestWasmModule()

	if err := Register(reg, "wasm-plugin", engine, wasmBytes); err != nil {
		t.Fatalf("Register: %v", err)
	}

	s := runtime.New()
	defer s.Close()

	ldr := loader.New(s, reg)

	tree, err := loader.NewTree(loader.Entry{
		ID:        "wasm-1",
		Component: "wasm-plugin",
		Payload:   map[string]any{"timeout": 5},
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}

	// 1. Initial reconcile: load and activate
	if err := ldr.Reconcile(tree); err != nil {
		t.Fatalf("ldr.Reconcile(tree): %v", err)
	}

	fibID, ok := ldr.Fiber("wasm-1")
	if !ok {
		t.Fatal("expected fiber for wasm-1 to exist")
	}

	waitForState(t, s, fibID, runtime.StateActive)

	info, ok := s.Inspect(fibID)
	if !ok || info.State != runtime.StateActive {
		t.Fatalf("expected fiber wasm-1 to be active, got %+v", info)
	}

	// 2. Second reconcile: disable/remove entry and verify clean unload
	emptyTree, err := loader.NewTree()
	if err != nil {
		t.Fatalf("NewTree(): %v", err)
	}

	if err := ldr.Reconcile(emptyTree); err != nil {
		t.Fatalf("ldr.Reconcile(emptyTree): %v", err)
	}

	if _, ok := ldr.Fiber("wasm-1"); ok {
		t.Fatal("expected fiber wasm-1 to be removed from loader")
	}

	if _, ok := s.Inspect(fibID); ok {
		t.Fatal("expected fiber wasm-1 to be removed from scheduler")
	}
}
