package wasm

import (
	"context"
	"sync"
	"testing"

	mcontext "github.com/DaviMGDev/memento/context"
	"github.com/tetratelabs/wazero"
)

func TestHostABI(t *testing.T) {
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	if err := RegisterHostModule(ctx, r); err != nil {
		t.Fatalf("failed to register host module: %v", err)
	}

	wasmBytes := buildTestWasmModule()
	compiled, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("failed to compile test module: %v", err)
	}

	mod, err := r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test-comp"))
	if err != nil {
		t.Fatalf("failed to instantiate test module: %v", err)
	}
	defer mod.Close(ctx)

	rootCtx := mcontext.NewContext(mcontext.RootFiber)
	fiberCtx := rootCtx.Derive(1)

	var modMu sync.Mutex
	st := &execState{
		payloadBytes: []byte(`{"key":"value"}`),
		fiberCtx:     fiberCtx,
		mod:          mod,
		modMu:        &modMu,
	}
	execCtx := withExecState(ctx, st)

	// Test declarations
	declFn := mod.ExportedFunction("memento_declare")
	if declFn == nil {
		t.Fatal("memento_declare export not found")
	}
	res, err := declFn.Call(execCtx)
	if err != nil {
		t.Fatalf("calling memento_declare: %v", err)
	}
	if len(res) > 0 && res[0] != 0 {
		t.Fatalf("memento_declare returned non-zero %v", res)
	}

	if len(st.injected) != 1 || st.injected[0] != "storage" {
		t.Fatalf("expected injected ['storage'], got %v", st.injected)
	}
	if len(st.provided) != 1 || st.provided[0] != "cache" {
		t.Fatalf("expected provided ['cache'], got %v", st.provided)
	}

	// Test activation
	actFn := mod.ExportedFunction("memento_activate")
	if actFn == nil {
		t.Fatal("memento_activate export not found")
	}
	res, err = actFn.Call(execCtx)
	if err != nil {
		t.Fatalf("calling memento_activate: %v", err)
	}
	if len(res) > 0 && res[0] != 0 {
		t.Fatalf("memento_activate returned non-zero %v", res)
	}

	// Verify effect was registered on fiberCtx
	if fiberCtx.Effects() == nil || fiberCtx.Effects().Pending() != 1 {
		t.Fatalf("expected 1 registered effect, got %v", fiberCtx.Effects().Pending())
	}

	// Test deactivation / inverse rollback
	if err := fiberCtx.Effects().Revert(); err != nil {
		t.Fatalf("reverting effects: %v", err)
	}
	if fiberCtx.Effects().Pending() != 0 {
		t.Fatalf("expected 0 effects after revert, got %d", fiberCtx.Effects().Pending())
	}
}
