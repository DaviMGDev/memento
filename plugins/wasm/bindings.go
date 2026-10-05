package wasm

import (
	"sync"

	mcontext "github.com/DaviMGDev/memento/context"
)

// bindingTable maps a provided key to the execState that registered it, so
// that invocation can route to the provider's module. One provider per key is
// a kernel invariant, so a plain map keyed by key identity is enough.
type bindingTable struct {
	mu    sync.RWMutex
	byKey map[mcontext.KeyID]*execState
}

func newBindingTable() *bindingTable {
	return &bindingTable{byKey: make(map[mcontext.KeyID]*execState)}
}

// set records st as the live provider for id, replacing any previous record.
func (t *bindingTable) set(id mcontext.KeyID, st *execState) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.byKey[id] = st
}

// get returns the execState registered for id.
func (t *bindingTable) get(id mcontext.KeyID) (*execState, bool) {
	if t == nil {
		return nil, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	st, ok := t.byKey[id]
	return st, ok
}

// forget removes every entry registered by st; the module teardown calls it
// after the guest's inverses have run.
func (t *bindingTable) forget(st *execState) {
	if t == nil || st == nil {
		return
	}
	st.mu.Lock()
	ids := append([]mcontext.KeyID(nil), st.bound...)
	st.mu.Unlock()
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, id := range ids {
		if t.byKey[id] == st {
			delete(t.byKey, id)
		}
	}
}
