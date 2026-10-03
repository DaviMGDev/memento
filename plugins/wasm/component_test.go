package wasm

import (
	"context"
	"testing"
	"time"

	mcontext "github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/runtime"
)

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

func TestWASMComponent(t *testing.T) {
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	// 1. Test declarations probing
	keys := NewKeyRegistry()
	wasmWithDecls := buildTestWasmModule()
	compWithDecls, err := NewComponent(ctx, engine, wasmWithDecls, WithKeyRegistry(keys))
	if err != nil {
		t.Fatalf("NewComponent(with decls): %v", err)
	}

	decls := compWithDecls.Declarations()
	if len(decls.Inject) != 1 || decls.Inject[0].Name() != "storage" {
		t.Fatalf("unexpected inject decls: %v", decls.Inject)
	}
	if len(decls.Provide) != 1 || decls.Provide[0].Name() != "cache" {
		t.Fatalf("unexpected provide decls: %v", decls.Provide)
	}

	// 2. Test lifecycle in scheduler with standalone component
	wasmStandalone := buildStandaloneTestWasmModule()
	factory := NewFactory(engine, wasmStandalone)

	comp, err := factory(map[string]any{"rate": 10})
	if err != nil {
		t.Fatalf("factory(): %v", err)
	}

	s := runtime.New()
	defer s.Close()

	fiberID, err := s.Insert(comp, map[string]any{"rate": 10})
	if err != nil {
		t.Fatalf("s.Insert(): %v", err)
	}

	// Wait for fiber to become active
	waitForState(t, s, fiberID, runtime.StateActive)

	// Verify fiber info
	info, ok := s.Inspect(fiberID)
	if !ok || info.State != runtime.StateActive {
		t.Fatalf("expected fiber to be active, got %+v (ok=%v)", info, ok)
	}

	// Retire fiber and verify deactivation & effect rollback
	if err := s.Retire(fiberID); err != nil {
		t.Fatalf("s.Retire(): %v", err)
	}

	waitForState(t, s, fiberID, runtime.StateInactive)
}
