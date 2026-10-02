package loader

import (
	"errors"
	"fmt"

	"github.com/DaviMGDev/memento/runtime"
)

// Factory instantiates a component from an entry's configuration payload.
//
// Compiled Go cannot import modules by URL, so the registry replaces module
// resolution: it is populated at link time and maps each component
// reference an entry may name to the code that instantiates it.
type Factory func(payload any) (runtime.Component, error)

// Registry maps component references to factories.
//
// Registration is expected at initialization time, before reconciliation
// begins; the registry is not synchronized for concurrent writes.
type Registry struct {
	factories map[string]Factory
}

// NewRegistry returns an empty component registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register binds ref to f. Empty references, nil factories, and duplicate
// references are refused.
func (r *Registry) Register(ref string, f Factory) error {
	if ref == "" {
		return errors.New("loader: cannot register an empty component reference")
	}
	if f == nil {
		return fmt.Errorf("loader: cannot register a nil factory for %q", ref)
	}
	if _, exists := r.factories[ref]; exists {
		return fmt.Errorf("loader: component reference %q is already registered", ref)
	}
	r.factories[ref] = f
	return nil
}

// Resolve returns the factory registered for ref, or a descriptive error
// when the reference is unknown.
func (r *Registry) Resolve(ref string) (Factory, error) {
	f, ok := r.factories[ref]
	if !ok {
		return nil, fmt.Errorf("loader: unknown component reference %q", ref)
	}
	return f, nil
}

// Has reports whether ref is registered.
func (r *Registry) Has(ref string) bool {
	_, ok := r.factories[ref]
	return ok
}
