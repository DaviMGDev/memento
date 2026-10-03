package wasm

import (
	"fmt"

	"github.com/DaviMGDev/memento/loader"
)

// Register registers a WASM component factory in the loader registry under the given ref.
func Register(reg *loader.Registry, ref string, engine *Engine, wasmBytes []byte, opts ...ComponentOption) error {
	if reg == nil {
		return fmt.Errorf("wasm: nil registry")
	}
	factory := NewFactory(engine, wasmBytes, opts...)
	if err := reg.Register(ref, factory); err != nil {
		return fmt.Errorf("wasm: registering factory for %q: %w", ref, err)
	}
	return nil
}
