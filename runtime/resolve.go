package runtime

import "github.com/DaviMGDev/memento/context"

func (s *Scheduler) fiber(id context.FiberID) *Fiber { return s.fibers[id] }

func (s *Scheduler) subscribe(k context.KeyID, id context.FiberID) {
	m := s.subscribers[k]
	if m == nil {
		m = make(map[context.FiberID]struct{})
		s.subscribers[k] = m
	}
	m[id] = struct{}{}
}

func (s *Scheduler) unsubscribe(f *Fiber) {
	for _, k := range f.injectIDs() {
		m := s.subscribers[k]
		if m == nil {
			continue
		}
		delete(m, f.id)
		if len(m) == 0 {
			delete(s.subscribers, k)
		}
	}
}

// activeProvider returns the fiber whose ACTIVE binding resolves id.
func (s *Scheduler) activeProvider(k context.KeyID) (context.FiberID, bool) {
	p, ok := s.providers[k]
	if !ok {
		return 0, false
	}
	f := s.fibers[p]
	if f == nil || f.state != StateActive {
		return 0, false
	}
	return p, true
}

// targetView resolves every declared injected key against the currently
// active providers. It reports false when one of them is unsatisfied.
func (s *Scheduler) targetView(f *Fiber) (CommittedView, bool) {
	if f.retired {
		return nil, false
	}
	injects := f.injectIDs()
	if len(injects) == 0 {
		return CommittedView{}, true
	}
	view := make(CommittedView, len(injects))
	for _, k := range injects {
		p, ok := s.activeProvider(k)
		if !ok {
			return nil, false
		}
		view[k] = p
	}
	return view, true
}

// classify re-evaluates every fiber that declared the changed key injected.
func (s *Scheduler) classify(k context.KeyID) {
	for id := range s.subscribers[k] {
		if f := s.fibers[id]; f != nil {
			s.refresh(f)
		}
	}
}

// refresh recomputes one fiber's target and starts the transition its state
// calls for, if any. It is idempotent: a neutral change leaves the fiber
// where it is.
func (s *Scheduler) refresh(f *Fiber) {
	switch f.state {
	case StateInactive:
		if view, ok := s.targetView(f); ok {
			s.startActivation(f, view)
		}
	case StateActive:
		view, ok := s.targetView(f)
		if !ok || !viewEqual(view, f.view) {
			s.startUnload(f)
		}
	}
}

func (s *Scheduler) providedBy(f *Fiber) []context.KeyID {
	var out []context.KeyID
	for k, p := range s.providers {
		if p == f.id {
			out = append(out, k)
		}
	}
	return out
}

func viewEqual(a, b CommittedView) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
