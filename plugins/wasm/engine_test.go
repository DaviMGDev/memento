package wasm

import (
	"context"
	"os"
	"testing"
)

func TestEngineInit(t *testing.T) {
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("unexpected error creating engine: %v", err)
	}
	defer engine.Close(ctx)

	wasmBytes, err := os.ReadFile("/tmp/test.wasm")
	if err != nil {
		t.Fatalf("reading test wasm: %v", err)
	}

	mod, err := engine.runtime.Instantiate(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("instantiating test wasm: %v", err)
	}
	defer mod.Close(ctx)

	res, err := mod.ExportedFunction("hello").Call(ctx)
	if err != nil {
		t.Fatalf("calling hello: %v", err)
	}
	if len(res) != 1 || res[0] != 42 {
		t.Fatalf("expected 42, got %v", res)
	}
}
