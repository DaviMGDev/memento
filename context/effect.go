package context

import (
	"errors"
	"fmt"
)

// ErrNoFiber is returned when an effect is registered on a context that
// belongs to no component instance (a root context), so there is no fiber
// to own the inverse.
var ErrNoFiber = errors.New("context: effect registration requires a fiber-owned context")

// EffectFunc is the explicit-effect escape hatch: it installs a custom
// resource the runtime does not manage and returns the inverse that
// reclaims it. The returned inverse carries the paradigm's effect-witness
// obligation: it must restore the context state the callback was applied
// to, up to the key's declared equivalence. Returning a nil inverse with a
// nil error declares an effect that needs no reclamation.
type EffectFunc func() (undo func() error, err error)

// RegisterEffect runs e and pushes the inverse it returns onto the
// registering fiber's accumulator, so the custom resource is reclaimed in
// LIFO order alongside built-in effects. An install error pushes nothing
// and is returned to the caller; a panic propagates to the activation
// machinery, which contains it like any other activation panic.
func (c *Context) RegisterEffect(e EffectFunc) error {
	if c == nil {
		return ErrNoFiber
	}
	if e == nil {
		return fmt.Errorf("context: RegisterEffect requires a non-nil EffectFunc")
	}
	acc := c.Effects()
	if acc == nil {
		return ErrNoFiber
	}
	undo, err := e()
	if err != nil {
		return err
	}
	acc.Push(undo)
	return nil
}
