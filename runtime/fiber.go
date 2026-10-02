package runtime

import "github.com/DaviMGDev/memento/context"

// Declarations is a component's inject/provide surface.
type Declarations struct {
	// Inject lists the keys the component needs before it can activate.
	Inject []context.AnyKey
	// Provide lists the keys the component binds during activation.
	Provide []context.AnyKey
}

// CommittedView maps each key the component declared as injected to the
// provider fiber that owned the binding when the instance activated. It is
// what lets a dependent keep reading its declared keys during teardown,
// after the live resolution has already moved on.
type CommittedView map[context.KeyID]context.FiberID

func copyView(view CommittedView) CommittedView {
	if view == nil {
		return nil
	}
	out := make(CommittedView, len(view))
	for k, v := range view {
		out[k] = v
	}
	return out
}

// Fiber is one instance of a component: its declarations, its configuration
// payload, its lifecycle state, its accumulator of inverses (owned by its
// context), and its committed view.
//
// The scheduler owns fibers; accessors are safe on the scheduler goroutine
// and after quiescence.
type Fiber struct {
	id      context.FiberID
	decls   Declarations
	payload any
	comp    Component
	ctx     *context.Context
	state   State
	view    CommittedView

	// Scheduler-owned control fields; never touched by component code.
	retired       bool
	pendingRemove bool
	pendingDrains int
	err           error
}

// newFiber creates an instance owned by id, running against a context
// derived from parent.
func newFiber(id context.FiberID, parent *context.Context, decls Declarations, payload any) *Fiber {
	return &Fiber{
		id:      id,
		decls:   decls,
		payload: payload,
		ctx:     parent.Derive(id),
		state:   StateInactive,
	}
}

// ID returns the instance identity.
func (f *Fiber) ID() context.FiberID { return f.id }

// Context returns the context this instance runs against.
func (f *Fiber) Context() *context.Context { return f.ctx }

// Declarations returns the instance's inject/provide surface.
func (f *Fiber) Declarations() Declarations { return f.decls }

// Payload returns the configuration payload the instance was created with.
func (f *Fiber) Payload() any { return f.payload }

// State returns the instance's current lifecycle state.
func (f *Fiber) State() State { return f.state }

// CommittedView returns a copy of the view captured at activation.
func (f *Fiber) CommittedView() CommittedView { return copyView(f.view) }

// commit captures view as the instance's committed view. The scheduler
// calls it when activation begins, before the activation body runs, so
// teardown can rely on the same resolution the body saw.
func (f *Fiber) commit(view CommittedView) {
	f.view = copyView(view)
}

func (f *Fiber) setState(s State) { f.state = s }

// injectIDs returns the identities of the declared injected keys.
func (f *Fiber) injectIDs() []context.KeyID { return keyIDs(f.decls.Inject) }

// provideIDs returns the identities of the declared provided keys.
func (f *Fiber) provideIDs() []context.KeyID { return keyIDs(f.decls.Provide) }

// provides reports whether id is one of the keys the fiber declared it
// provides.
func (f *Fiber) provides(id context.KeyID) bool {
	for _, k := range f.provideIDs() {
		if k == id {
			return true
		}
	}
	return false
}

func keyIDs(keys []context.AnyKey) []context.KeyID {
	if len(keys) == 0 {
		return nil
	}
	ids := make([]context.KeyID, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.ID())
	}
	return ids
}
