package runtime

import "github.com/DaviMGDev/memento/context"

// Component is the behavior half of a fiber.
type Component interface {
	// Declarations returns the keys the component needs and provides.
	Declarations() Declarations
	// Activate runs the activation body against inst, installing effects
	// through the instance. It runs on a worker goroutine. Returning an
	// error, or panicking, fails the activation: effects already installed
	// are rolled back and the fiber lands in StateFailed.
	Activate(inst *Instance, payload any) error
}

// Instance is the component-facing handle to its fiber: typed reads of the
// bindings it declared, and typed mutations that register their inverses.
type Instance struct {
	f *Fiber
	s *Scheduler
}

// FiberID returns the identity of the instance.
func (i *Instance) FiberID() context.FiberID { return i.f.id }

// Context returns the instance's context: the scope its private bindings
// and explicit effects are installed in.
func (i *Instance) Context() *context.Context { return i.f.ctx }

// Payload returns the configuration payload the instance is running with.
func (i *Instance) Payload() any { return i.f.payload }

// Get reads the value of k as resolved for the instance. Declared keys
// resolve through the instance's committed view, so a dependent keeps
// reading a withdrawing dependency during its own teardown; undeclared keys
// resolve through the instance's own context chain.
func Get[T any](i *Instance, k context.Key[T]) (T, bool) {
	if provider, ok := i.f.view[k.ID()]; ok {
		if p := i.s.fiber(provider); p != nil {
			if v, ok := k.Lookup(p.ctx); ok {
				return v, true
			}
		}
	}
	return k.Lookup(i.f.ctx)
}

// Bind installs v at k in the instance's context and registers the inverse
// that withdraws it.
func Bind[T any](i *Instance, k context.Key[T], v T) error {
	return k.Bind(i.f.ctx, v)
}
