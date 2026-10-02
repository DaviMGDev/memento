package runtime

import (
	"errors"
	"fmt"
	"sync"

	"github.com/DaviMGDev/memento/context"
)

// ErrClosed is returned by Scheduler operations after the scheduler closes.
var ErrClosed = errors.New("runtime: scheduler is closed")

// FiberInfo is a read-only snapshot of one fiber for diagnostics.
type FiberInfo struct {
	ID            context.FiberID
	State         State
	Declarations  Declarations
	Payload       any
	CommittedView CommittedView
	Provided      []context.KeyID
	Retired       bool
	Err           error
}

type eventKind uint8

const (
	evInsert eventKind = iota
	evRetire
	evRemove
	evReload
	evBinding
	evInspect
	evActivationDone
	evDeactivationDone
	evClose
)

type reply struct {
	id   context.FiberID
	info FiberInfo
	ok   bool
	err  error
}

type event struct {
	kind eventKind

	id      context.FiberID
	comp    Component
	payload any

	key       context.KeyID
	prev      context.FiberID
	curr      context.FiberID
	installed bool

	err error
	ack chan reply
}

func (ev event) reply(r reply) {
	if ev.ack != nil {
		ev.ack <- r
	}
}

// Scheduler is the single control plane of a runtime.
//
// One goroutine owns the registry, the per-key subscriber index, the
// provider index, and target recomputation, and processes registry events
// sequentially; it never blocks on component code. Component activation
// bodies and inverses run on worker goroutines and report completion back
// to the loop as events.
type Scheduler struct {
	root   *context.Context
	events chan event

	fibers map[context.FiberID]*Fiber
	nextID context.FiberID

	// providers maps a key to the fiber that most recently installed its
	// binding, whether or not that fiber is active. Resolution gates on the
	// fiber's state through activeProvider.
	providers map[context.KeyID]context.FiberID
	// claims maps a key to the registered fiber that declared it provides;
	// admission refuses a second claimant.
	claims map[context.KeyID]context.FiberID
	// subscribers maps a key to the fibers that declared it injected.
	subscribers map[context.KeyID]map[context.FiberID]struct{}
	// waiters maps a dependent fiber to the providers whose inverses are
	// waiting for it to reach a terminal state.
	waiters map[context.FiberID][]*Fiber

	closeOnce sync.Once
	closed    chan struct{}
}

// New starts a scheduler and its control-plane goroutine.
func New() *Scheduler {
	s := &Scheduler{
		root:        context.NewContext(context.RootFiber),
		events:      make(chan event, 256),
		fibers:      make(map[context.FiberID]*Fiber),
		providers:   make(map[context.KeyID]context.FiberID),
		claims:      make(map[context.KeyID]context.FiberID),
		subscribers: make(map[context.KeyID]map[context.FiberID]struct{}),
		waiters:     make(map[context.FiberID][]*Fiber),
		closed:      make(chan struct{}),
	}
	s.root.SetObserver(s)
	go s.loop()
	return s
}

// Close stops the scheduler. In-flight component code is not interrupted;
// its completion events are ignored once the loop has stopped.
func (s *Scheduler) Close() error {
	s.closeOnce.Do(func() {
		select {
		case s.events <- event{kind: evClose}:
		case <-s.closed:
			return
		}
		<-s.closed
	})
	return nil
}

// Insert registers a new component instance and returns its fiber identity.
// The instance activates as soon as its declarations are satisfied.
func (s *Scheduler) Insert(c Component, payload any) (context.FiberID, error) {
	r := s.call(event{kind: evInsert, comp: c, payload: payload})
	return r.id, r.err
}

// Retire requests that the fiber stop providing and unload. The request is
// inertial: an in-flight transition completes first.
func (s *Scheduler) Retire(id context.FiberID) error {
	return s.call(event{kind: evRetire, id: id}).err
}

// Remove retires the fiber and deletes it from the registry once it has
// reached a terminal state.
func (s *Scheduler) Remove(id context.FiberID) error {
	return s.call(event{kind: evRemove, id: id}).err
}

// Reload replaces the fiber's configuration payload and reloads it: the
// fiber deactivates, then activates again against the same environment once
// the transition completes. A failed fiber is not reloaded; a revision
// re-inserts it as a fresh instance.
func (s *Scheduler) Reload(id context.FiberID, payload any) error {
	return s.call(event{kind: evReload, id: id, payload: payload}).err
}

// Inspect returns a snapshot of one fiber.
func (s *Scheduler) Inspect(id context.FiberID) (FiberInfo, bool) {
	r := s.call(event{kind: evInspect, id: id})
	return r.info, r.ok
}

// BindingChanged implements context.Observer.
func (s *Scheduler) BindingChanged(id context.KeyID, previous, current context.FiberID, installed bool) {
	s.post(event{kind: evBinding, key: id, prev: previous, curr: current, installed: installed})
}

func (s *Scheduler) post(ev event) {
	select {
	case s.events <- ev:
	case <-s.closed:
	}
}

func (s *Scheduler) call(ev event) reply {
	ev.ack = make(chan reply, 1)
	select {
	case s.events <- ev:
	case <-s.closed:
		return reply{err: ErrClosed}
	}
	return <-ev.ack
}

func (s *Scheduler) loop() {
	defer close(s.closed)
	for ev := range s.events {
		switch ev.kind {
		case evClose:
			return
		case evInsert:
			s.handleInsert(ev)
		case evRetire:
			s.handleRetire(ev)
		case evRemove:
			s.handleRemove(ev)
		case evReload:
			s.handleReload(ev)
		case evBinding:
			s.handleBinding(ev)
		case evInspect:
			s.handleInspect(ev)
		case evActivationDone:
			s.handleActivationDone(ev)
		case evDeactivationDone:
			s.handleDeactivationDone(ev)
		}
	}
}

func (s *Scheduler) handleInsert(ev event) {
	if ev.comp == nil {
		ev.reply(reply{err: errors.New("runtime: nil component")})
		return
	}
	decls := ev.comp.Declarations()
	if err := checkDeclarations(decls); err != nil {
		ev.reply(reply{err: err})
		return
	}
	if err := s.admit(decls); err != nil {
		ev.reply(reply{err: err})
		return
	}

	s.nextID++
	id := s.nextID
	f := newFiber(id, s.root, decls, ev.payload)
	f.comp = ev.comp
	s.fibers[id] = f

	for _, k := range f.injectIDs() {
		s.subscribe(k, id)
	}
	for _, k := range f.provideIDs() {
		s.claims[k] = id
	}

	if view, ok := s.targetView(f); ok {
		s.startActivation(f, view)
	}
	ev.reply(reply{id: id})
}

func (s *Scheduler) handleRetire(ev event) {
	f := s.fibers[ev.id]
	if f == nil {
		ev.reply(reply{err: fmt.Errorf("runtime: unknown fiber %d", ev.id)})
		return
	}
	f.retired = true
	if f.state == StateActive {
		s.startUnload(f)
	}
	ev.reply(reply{})
}

func (s *Scheduler) handleRemove(ev event) {
	f := s.fibers[ev.id]
	if f == nil {
		ev.reply(reply{err: fmt.Errorf("runtime: unknown fiber %d", ev.id)})
		return
	}
	f.retired = true
	f.pendingRemove = true
	switch f.state {
	case StateInactive, StateFailed:
		s.deleteFiber(f)
	case StateActive:
		s.startUnload(f)
	}
	ev.reply(reply{})
}

func (s *Scheduler) handleReload(ev event) {
	f := s.fibers[ev.id]
	if f == nil {
		ev.reply(reply{err: fmt.Errorf("runtime: unknown fiber %d", ev.id)})
		return
	}
	f.payload = ev.payload
	f.retired = false
	switch f.state {
	case StateActive:
		s.startUnload(f)
	case StateInactive:
		if view, ok := s.targetView(f); ok {
			s.startActivation(f, view)
		}
	}
	ev.reply(reply{})
}

func (s *Scheduler) handleInspect(ev event) {
	f := s.fibers[ev.id]
	if f == nil {
		ev.reply(reply{ok: false})
		return
	}
	ev.reply(reply{ok: true, info: s.snapshot(f)})
}

func (s *Scheduler) handleBinding(ev event) {
	if ev.installed {
		// A binding becomes observable only once its provider is active;
		// activation completion registers the provider.
		f := s.fibers[ev.curr]
		if f == nil || f.state != StateActive || !f.provides(ev.key) {
			return
		}
		s.providers[ev.key] = f.id
		s.classify(ev.key)
		return
	}
	// A withdrawal by the current provider retires its provision early.
	if ev.prev != context.RootFiber && s.providers[ev.key] == ev.prev {
		delete(s.providers, ev.key)
		s.classify(ev.key)
	}
}

func (s *Scheduler) handleActivationDone(ev event) {
	f := s.fibers[ev.id]
	if f == nil {
		return
	}
	if ev.err != nil {
		f.err = ev.err
		f.setState(StateFailed)
		s.releaseWaits(f.id)
		s.finishRemove(f)
		return
	}
	// Inertia: the target is re-checked only after the activation completes.
	view, ok := s.targetView(f)
	if !ok || !viewEqual(view, f.view) {
		s.startUnload(f)
		return
	}
	f.setState(StateActive)
	s.registerProvides(f)
}

func (s *Scheduler) handleDeactivationDone(ev event) {
	f := s.fibers[ev.id]
	if f == nil {
		return
	}
	if ev.err != nil {
		f.err = errors.Join(f.err, ev.err)
	}
	f.setState(StateInactive)
	s.releaseWaits(f.id)
	if s.finishRemove(f) {
		return
	}
	// Inertia in the other direction: if the target flipped back while the
	// unload was in flight, the fiber activates again now.
	if !f.retired {
		if view, ok := s.targetView(f); ok {
			s.startActivation(f, view)
		}
	}
}

func (s *Scheduler) snapshot(f *Fiber) FiberInfo {
	return FiberInfo{
		ID:            f.id,
		State:         f.state,
		Declarations:  f.decls,
		Payload:       f.payload,
		CommittedView: f.CommittedView(),
		Provided:      s.providedBy(f),
		Retired:       f.retired,
		Err:           f.err,
	}
}
