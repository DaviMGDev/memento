package wasm

import (
	"context"
	"testing"
)

func TestEngineInit(t *testing.T) {
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("unexpected error creating engine: %v", err)
	}
	defer engine.Close(ctx)

	wasmBytes := buildStandaloneTestWasmModule()

	mod, err := engine.runtime.Instantiate(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("instantiating test wasm: %v", err)
	}
	defer mod.Close(ctx)

	fn := mod.ExportedFunction("memento_activate")
	if fn == nil {
		t.Fatal("missing memento_activate export")
	}
}
