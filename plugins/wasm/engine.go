package wasm

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
)

// Engine wraps the wazero runtime for executing WASM plugin modules.
type Engine struct {
	runtime wazero.Runtime
}

// NewEngine creates a new WASM execution engine with default configuration.
func NewEngine(ctx context.Context) (*Engine, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r := wazero.NewRuntime(ctx)
	if err := RegisterHostModule(ctx, r); err != nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("wasm: registering host module: %w", err)
	}
	return &Engine{runtime: r}, nil
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
