package runtime

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DaviMGDev/memento/context"
)

func TestActivationCompletesBeforeHonoringAWithdrawal(t *testing.T) {
	s := New()
	defer s.Close()

	storage := context.NewKey[string]("storage")
	provider := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			return Bind(inst, storage, "s3")
		},
	}
	providerID, err := s.Insert(provider, nil)
	if err != nil {
		t.Fatalf("Insert(provider) = %v", err)
	}
	waitForState(t, s, providerID, StateActive)

	release := make(chan struct{})
	activationDone := make(chan struct{})
	dependent := &testComponent{
		decls: Declarations{Inject: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			if _, ok := Get(inst, storage); !ok {
				return fmt.Errorf("storage was not readable")
			}
			if err := inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error { return nil }, nil
			}); err != nil {
				return err
			}
			<-release
			close(activationDone)
			return nil
		},
	}
	depID, err := s.Insert(dependent, nil)
	if err != nil {
		t.Fatalf("Insert(dependent) = %v", err)
	}
	waitForState(t, s, depID, StateLoading)

	// The dependency is withdrawn while the dependent is still installing.
	if err := s.Retire(providerID); err != nil {
		t.Fatalf("Retire(provider) = %v", err)
	}

	// Inertia: the in-flight activation is not abandoned, and the
	// provider's inverses wait behind it.
	time.Sleep(20 * time.Millisecond)
	if info, _ := s.Inspect(depID); info.State != StateLoading {
		t.Fatalf("dependent state = %v, want the activation to run to completion", info.State)
	}
	if info, _ := s.Inspect(providerID); info.State != StateUnloading {
		t.Fatalf("provider state = %v, want unloading while it drains", info.State)
	}

	close(release)
	select {
	case <-activationDone:
	case <-time.After(time.Second):
		t.Fatal("the activation never completed")
	}

	waitForState(t, s, depID, StateInactive)
	waitForState(t, s, providerID, StateInactive)
}

func TestUnloadCompletesBeforeReactivation(t *testing.T) {
	s := New()
	defer s.Close()

	storage := context.NewKey[string]("storage")
	provider := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			return Bind(inst, storage, payload.(string))
		},
	}
	providerID, err := s.Insert(provider, "first")
	if err != nil {
		t.Fatalf("Insert(provider) = %v", err)
	}
	waitForState(t, s, providerID, StateActive)

	releaseUndo := make(chan struct{})
	undone := make(chan struct{}, 4)
	var activations int32
	dependent := &testComponent{
		decls: Declarations{Inject: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			atomic.AddInt32(&activations, 1)
			return inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					<-releaseUndo
					undone <- struct{}{}
					return nil
				}, nil
			})
		},
	}
	depID, err := s.Insert(dependent, nil)
	if err != nil {
		t.Fatalf("Insert(dependent) = %v", err)
	}
	waitForState(t, s, depID, StateActive)

	// Reloading the provider withdraws the key; the dependent begins
	// unloading and parks inside its inverse.
	if err := s.Reload(providerID, "second"); err != nil {
		t.Fatalf("Reload(provider) = %v", err)
	}
	waitForState(t, s, depID, StateUnloading)
	if info, _ := s.Inspect(providerID); info.State != StateUnloading {
		t.Fatalf("provider state = %v, want unloading behind its dependent", info.State)
	}

	time.Sleep(20 * time.Millisecond)
	if info, _ := s.Inspect(depID); info.State != StateUnloading {
		t.Fatalf("dependent state = %v, want the unload to run to completion", info.State)
	}

	// The dependency is already coming back, but the parked unload must
	// finish first; only then do both fibers activate again.
	releaseUndo <- struct{}{}
	select {
	case <-undone:
	case <-time.After(time.Second):
		t.Fatal("the parked inverse never ran")
	}

	waitForState(t, s, providerID, StateActive)
	waitForState(t, s, depID, StateActive)

	if got := atomic.LoadInt32(&activations); got != 2 {
		t.Fatalf("dependent activations = %d, want 2 (unload, then activate again)", got)
	}
	info, _ := s.Inspect(depID)
	if got := info.CommittedView[storage.ID()]; got != providerID {
		t.Fatalf("dependent committed to %v, want the reloaded provider %v", got, providerID)
	}
}
