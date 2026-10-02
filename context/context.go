package context

import "sync/atomic"

// FiberID identifies the component instance (the fiber) a context belongs
// to. The context package treats it as an opaque identity: the runtime
// issues FiberID values and compares them. RootFiber identifies contexts
// that are not owned by a component instance.
type FiberID uint64

// RootFiber is the identity of contexts not owned by a component instance.
const RootFiber FiberID = 0

// binding is one installed key-value pair: the value and the identity of
// the fiber that installed it.
type binding struct {
	value any
	owner FiberID
}

// bindingTable is an immutable snapshot of one context's own bindings.
// Mutations publish a fresh table atomically, so lookups never take a lock.
type bindingTable map[KeyID]*binding

// Context is a node in the context tree.
//
// A context carries a reference to its parent, the fiber it belongs to, and
// its own bindings; views over inherited bindings are computed by walking
// toward the root, so derived contexts are cheap and isolation is achieved
// by deriving. A binding installed at a context shadows an ancestor's
// binding for that context's subtree only.
type Context struct {
	parent   *Context
	fiber    FiberID
	effects  *Accumulator
	observer Observer
	table    atomic.Pointer[bindingTable]
}

// NewContext returns a root context owned by fiber.
func NewContext(fiber FiberID) *Context {
	return newContext(nil, fiber)
}

// Derive returns a fresh child of c owned by fiber.
func (c *Context) Derive(fiber FiberID) *Context {
	return newContext(c, fiber)
}

func newContext(parent *Context, fiber FiberID) *Context {
	c := &Context{parent: parent, fiber: fiber}
	if parent != nil {
		c.effects = &Accumulator{}
	}
	empty := bindingTable{}
	c.table.Store(&empty)
	return c
}

// Parent returns the parent of c, or nil for a root context.
func (c *Context) Parent() *Context {
	if c == nil {
		return nil
	}
	return c.parent
}

// Fiber returns the identity of the fiber c belongs to.
func (c *Context) Fiber() FiberID {
	if c == nil {
		return RootFiber
	}
	return c.fiber
}

// Effects returns the accumulator of inverses owned by the fiber this
// context belongs to. Root contexts have no fiber and return nil.
func (c *Context) Effects() *Accumulator {
	if c == nil {
		return nil
	}
	return c.effects
}

// lookup returns the nearest binding for id at c or any ancestor.
func (c *Context) lookup(id KeyID) *binding {
	if id.IsZero() {
		return nil
	}
	for cur := c; cur != nil; cur = cur.parent {
		table := cur.table.Load()
		if table == nil {
			continue
		}
		if b, ok := (*table)[id]; ok {
			return b
		}
	}
	return nil
}

// Lookup returns the nearest binding for k starting at c and walking toward
// the root. It reports false when no context in the chain binds k, or when
// the stored value does not match the key's value type (which can only be
// caused by installing through a type-erased path).
func (k Key[T]) Lookup(c *Context) (T, bool) {
	var zero T
	b := c.lookup(k.ID())
	if b == nil {
		return zero, false
	}
	v, ok := b.value.(T)
	if !ok {
		return zero, false
	}
	return v, true
}

// install stores v under id in c's own table on behalf of owner,
// shadowing any inherited binding for c's subtree. It returns the binding
// it replaced, or nil if c had no local binding for id. The returned
// binding carries the previous provider's identity: an install whose
// previous owner differs from owner is a provider replacement, while an
// install by the same owner is an in-place value overwrite.
func (c *Context) install(id KeyID, owner FiberID, v any) *binding {
	if id.IsZero() {
		panic("context: install with the zero key identity")
	}
	old := c.table.Load()
	next := make(bindingTable, len(*old)+1)
	for k, b := range *old {
		next[k] = b
	}
	prev := next[id]
	next[id] = &binding{value: v, owner: owner}
	c.table.Store(&next)

	var prevOwner FiberID
	if prev != nil {
		prevOwner = prev.owner
	}
	c.notifyBinding(id, prevOwner, owner, true)
	return prev
}

// withdraw removes id from c's own table when the stored binding belongs
// to owner. It returns the removed binding and whether one was removed;
// inherited bindings are untouched, so the nearest ancestor binding
// becomes visible again. A withdraw by any other fiber is refused, which
// keeps a stale inverse from tearing down a replacement provider's
// binding.
func (c *Context) withdraw(id KeyID, owner FiberID) (*binding, bool) {
	if id.IsZero() {
		return nil, false
	}
	old := c.table.Load()
	removed, ok := (*old)[id]
	if !ok || removed.owner != owner {
		return nil, false
	}
	next := make(bindingTable, len(*old))
	for k, b := range *old {
		if k != id {
			next[k] = b
		}
	}
	c.table.Store(&next)
	c.notifyBinding(id, owner, RootFiber, false)
	return removed, true
}
