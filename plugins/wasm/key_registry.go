package wasm

import (
	"fmt"
	"sync"

	mcontext "github.com/DaviMGDev/memento/context"
)

// KeyRegistry maintains a mapping between string key identifiers used by
// WASM modules and Memento typed context keys.
type KeyRegistry struct {
	mu   sync.RWMutex
	keys map[string]mcontext.AnyKey
}

// NewKeyRegistry creates an empty KeyRegistry.
func NewKeyRegistry() *KeyRegistry {
	return &KeyRegistry{
		keys: make(map[string]mcontext.AnyKey),
	}
}

// Register maps name to key. If name is already registered, an error is returned.
func (r *KeyRegistry) Register(name string, key mcontext.AnyKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "" {
		return fmt.Errorf("wasm: key name cannot be empty")
	}
	if key == nil {
		return fmt.Errorf("wasm: cannot register nil key for %q", name)
	}
	if _, exists := r.keys[name]; exists {
		return fmt.Errorf("wasm: key %q already registered", name)
	}
	r.keys[name] = key
	return nil
}

// GetOrCreate returns the key registered for name, or lazily instantiates
// a default context.Key[any] if not previously registered.
func (r *KeyRegistry) GetOrCreate(name string) mcontext.AnyKey {
	r.mu.Lock()
	defer r.mu.Unlock()
	if k, ok := r.keys[name]; ok {
		return k
	}
	k := mcontext.NewKey[any](name)
	r.keys[name] = k
	return k
}

// Get returns the registered key for name, reporting whether it was found.
func (r *KeyRegistry) Get(name string) (mcontext.AnyKey, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.keys[name]
	return k, ok
}
