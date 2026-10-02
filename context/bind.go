package context

import "errors"

// ErrZeroKey is returned when an operation is attempted with the zero key.
var ErrZeroKey = errors.New("context: the zero key identifies no binding")

// Bind installs v at k in c on behalf of c's fiber and registers the
// inverse that withdraws it with the fiber's accumulator. Binding a key the
// context already binds is an in-place overwrite: the provider identity
// stays the same, so dependents observe no change.
func (k Key[T]) Bind(c *Context, v T) error {
	if c == nil {
		return ErrNoFiber
	}
	if k.IsZero() {
		return ErrZeroKey
	}
	acc := c.Effects()
	if acc == nil {
		return ErrNoFiber
	}
	owner := c.Fiber()
	c.install(k.ID(), owner, v)

	id := k.ID()
	scope := c
	acc.Push(func() error {
		// Withdraw only this fiber's binding; a replacement provider's
		// binding must survive.
		scope.withdraw(id, owner)
		return nil
	})
	return nil
}

// LookupOwner returns the identity of the fiber that installed the nearest
// binding for id at c or any ancestor.
func (c *Context) LookupOwner(id KeyID) (FiberID, bool) {
	b := c.lookup(id)
	if b == nil {
		return RootFiber, false
	}
	return b.owner, true
}
