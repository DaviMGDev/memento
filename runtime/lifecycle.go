package runtime

import (
	"errors"
	"fmt"

	"github.com/DaviMGDev/memento/context"
)

// startActivation commits the resolved view, puts the fiber in LOADING, and
// runs the activation body on a worker.
func (s *Scheduler) startActivation(f *Fiber, view CommittedView) {
	f.commit(view)
	f.setState(StateLoading)

	comp := f.comp
	inst := &Instance{f: f, s: s}
	payload := f.payload
	go func() {
		err := activate(comp, inst, payload)
		if err != nil {
			// Roll back what this activation installed before reporting.
			if rbErr := f.ctx.Effects().Revert(); rbErr != nil {
				err = errors.Join(err, rbErr)
			}
		}
		s.post(event{kind: evActivationDone, id: f.id, err: err})
	}()
}

func activate(c Component, inst *Instance, payload any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("runtime: activation panicked: %v", r)
		}
	}()
	return c.Activate(inst, payload)
}

// startUnload stops the fiber providing, notifies its dependents, and runs
// the inverse worker once every dependent has reached a terminal state.
func (s *Scheduler) startUnload(f *Fiber) {
	f.setState(StateUnloading)

	// A provider that has entered unloading provides nothing: dependents
	// recompute an unsatisfied target view while the bindings are still
	// physically in place, which is what lets their teardown read them.
	keys := s.unregisterProvides(f)
	for _, k := range keys {
		s.classify(k)
	}

	// The provider's inverses wait until every fiber that resolved one of
	// its keys has stopped.
	for _, d := range s.dependentsOf(f) {
		if d.state == StateInactive || d.state == StateFailed {
			continue
		}
		s.waiters[d.id] = append(s.waiters[d.id], f)
		f.pendingDrains++
	}
	if f.pendingDrains == 0 {
		s.runDeactivation(f)
	}
}

func (s *Scheduler) runDeactivation(f *Fiber) {
	f.pendingDrains = 0
	go func() {
		err := f.ctx.Effects().Revert()
		s.post(event{kind: evDeactivationDone, id: f.id, err: err})
	}()
}

// dependentsOf returns the fibers whose committed view names p as a
// provider, regardless of their current state.
func (s *Scheduler) dependentsOf(p *Fiber) []*Fiber {
	var out []*Fiber
	for _, f := range s.fibers {
		if f == p {
			continue
		}
		for _, provider := range f.view {
			if provider == p.id {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// releaseWaits reports that a dependent reached a terminal state, releasing
// every provider waiting on it.
func (s *Scheduler) releaseWaits(dep context.FiberID) {
	providers := s.waiters[dep]
	delete(s.waiters, dep)
	for _, p := range providers {
		p.pendingDrains--
		if p.pendingDrains == 0 && p.state == StateUnloading {
			s.runDeactivation(p)
		}
	}
}

// registerProvides advertises the fiber's bound declared keys, making them
// resolvable by dependents.
func (s *Scheduler) registerProvides(f *Fiber) {
	for _, k := range f.provideIDs() {
		owner, ok := f.ctx.LookupOwner(k)
		if !ok || owner != f.id {
			// Declared but not bound: nothing to advertise.
			continue
		}
		s.providers[k] = f.id
		s.classify(k)
	}
}

// unregisterProvides stops advertising the fiber's keys and returns those
// that were advertised.
func (s *Scheduler) unregisterProvides(f *Fiber) []context.KeyID {
	var keys []context.KeyID
	for _, k := range f.provideIDs() {
		if s.providers[k] == f.id {
			delete(s.providers, k)
			keys = append(keys, k)
		}
	}
	return keys
}

func (s *Scheduler) deleteFiber(f *Fiber) {
	delete(s.fibers, f.id)
	s.unsubscribe(f)
	for _, k := range f.provideIDs() {
		if s.claims[k] == f.id {
			delete(s.claims, k)
		}
		if s.providers[k] == f.id {
			delete(s.providers, k)
		}
	}
}

// finishRemove deletes a fiber whose removal was requested once it is in a
// terminal state.
func (s *Scheduler) finishRemove(f *Fiber) bool {
	if !f.pendingRemove {
		return false
	}
	if f.state != StateInactive && f.state != StateFailed {
		return false
	}
	s.deleteFiber(f)
	return true
}
