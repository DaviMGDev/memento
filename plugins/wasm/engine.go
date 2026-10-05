package wasm

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Engine wraps the wazero runtime for executing WASM plugin modules.
type Engine struct {
	runtime  wazero.Runtime
	bindings *bindingTable
}

// NewEngine creates a new WASM execution engine with default configuration.
//
// The WASI snapshot preview 1 host module is instantiated so guests built for
// wasi (notably Go's wasip1 port) can be loaded; core wasm guests that never
// import WASI are unaffected.
func NewEngine(ctx context.Context) (*Engine, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r := wazero.NewRuntime(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("wasm: instantiating WASI: %w", err)
	}
	if err := RegisterHostModule(ctx, r); err != nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("wasm: registering host module: %w", err)
	}
	return &Engine{runtime: r, bindings: newBindingTable()}, nil
}

// Close releases resources associated with the WASM runtime.
func (e *Engine) Close(ctx context.Context) error {
	if e == nil || e.runtime == nil {
		return nil
	}
	if err := e.runtime.Close(ctx); err != nil {
		return fmt.Errorf("closing wasm runtime: %w", err)
	}
	return nil
}
