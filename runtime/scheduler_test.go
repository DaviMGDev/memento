package runtime

import (
	"testing"
	"time"

	"github.com/DaviMGDev/memento/context"
)

type testComponent struct {
	decls    Declarations
	activate func(inst *Instance, payload any) error
}

func (c *testComponent) Declarations() Declarations { return c.decls }

func (c *testComponent) Activate(inst *Instance, payload any) error {
	if c.activate == nil {
		return nil
	}
	return c.activate(inst, payload)
}

func waitForState(t *testing.T, s *Scheduler, id context.FiberID, want State) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := s.Inspect(id); ok && info.State == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	info, ok := s.Inspect(id)
	t.Fatalf("fiber %d did not reach %q; last = %+v (ok=%v)", id, want, info, ok)
}

func TestControlPlaneKeepsProcessingWhileComponentCodeBlocks(t *testing.T) {
	s := New()
	defer s.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	blocking := &testComponent{
		activate: func(inst *Instance, payload any) error {
			close(started)
			<-release
			return nil
		},
	}

	blockedID, err := s.Insert(blocking, nil)
	if err != nil {
		t.Fatalf("Insert(blocking) = %v", err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the activation body never ran on a worker")
	}

	// The loop is not blocked on the worker: it keeps serving inserts,
	// retires, and inspections while the activation body is parked.
	simple := &testComponent{}
	simpleID, err := s.Insert(simple, nil)
	if err != nil {
		t.Fatalf("Insert(simple) = %v", err)
	}
	waitForState(t, s, simpleID, StateActive)

	if err := s.Retire(simpleID); err != nil {
		t.Fatalf("Retire(simple) = %v", err)
	}
	waitForState(t, s, simpleID, StateInactive)

	close(release)
	waitForState(t, s, blockedID, StateActive)
}

func TestBindingEventsFlowThroughTheLoop(t *testing.T) {
	s := New()
	defer s.Close()

	valueKey := context.NewKey[string]("value")
	comp := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{valueKey}},
		activate: func(inst *Instance, payload any) error {
			return Bind(inst, valueKey, "bound")
		},
	}

	id, err := s.Insert(comp, nil)
	if err != nil {
		t.Fatalf("Insert() = %v", err)
	}
	waitForState(t, s, id, StateActive)

	// The provider index is updated by the control plane once the fiber is
	// active, not by the worker that installed the binding.
	if got := s.providers[valueKey.ID()]; got != id {
		t.Fatalf("provider index = %v, want %v", got, id)
	}
	info, ok := s.Inspect(id)
	if !ok || info.State != StateActive {
		t.Fatalf("Inspect() = (%+v, %v), want active", info, ok)
	}
	if len(info.Provided) != 1 || info.Provided[0] != valueKey.ID() {
		t.Fatalf("Provided = %v, want the value key", info.Provided)
	}
}
