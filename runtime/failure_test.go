package runtime

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DaviMGDev/memento/context"
)

func TestActivationErrorRollsBackAndFailsTerminally(t *testing.T) {
	s := New()
	defer s.Close()

	storage := context.NewKey[string]("storage")
	rolledBack := make(chan string, 4)
	instCh := make(chan *Instance, 1)

	comp := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			instCh <- inst
			if err := Bind(inst, storage, "s3"); err != nil {
				return err
			}
			if err := inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					rolledBack <- "custom"
					return nil
				}, nil
			}); err != nil {
				return err
			}
			return errors.New("activation blew up")
		},
	}
	id, err := s.Insert(comp, nil)
	if err != nil {
		t.Fatalf("Insert() = %v", err)
	}
	waitForState(t, s, id, StateFailed)

	info, _ := s.Inspect(id)
	if info.Err == nil || !strings.Contains(info.Err.Error(), "activation blew up") {
		t.Fatalf("recorded failure = %v, want the activation error", info.Err)
	}

	select {
	case msg := <-rolledBack:
		if msg != "custom" {
			t.Fatalf("rollback ran %q, want the custom inverse", msg)
		}
	default:
		t.Fatal("the custom effect was not rolled back")
	}

	inst := <-instCh
	if _, ok := storage.Lookup(inst.Context()); ok {
		t.Fatal("the binding installed before the failure survived the rollback")
	}
	if len(s.providers) != 0 {
		t.Fatalf("providers = %v, want none after a failed activation", s.providers)
	}

	// A failed fiber is terminal: reloading it does not retry activation.
	if err := s.Reload(id, "again"); err != nil {
		t.Fatalf("Reload(failed) = %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if info, _ := s.Inspect(id); info.State != StateFailed {
		t.Fatalf("failed fiber state = %v, want it to stay failed", info.State)
	}

	// A revision removes the failed fiber and a fresh instance then takes
	// the key over.
	if err := s.Remove(id); err != nil {
		t.Fatalf("Remove(failed) = %v", err)
	}
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
}

func TestFailedFiberStaysFailedWhenSatisfiedAgain(t *testing.T) {
	s := New()
	defer s.Close()

	trigger := context.NewKey[string]("trigger")
	storage := context.NewKey[string]("storage")

	triggerProvider := func() *testComponent {
		return &testComponent{
			decls: Declarations{Provide: []context.AnyKey{trigger}},
			activate: func(inst *Instance, payload any) error {
				return Bind(inst, trigger, "go")
			},
		}
	}
	firstID, err := s.Insert(triggerProvider(), nil)
	if err != nil {
		t.Fatalf("Insert(trigger) = %v", err)
	}
	waitForState(t, s, firstID, StateActive)

	broken := &testComponent{
		decls: Declarations{
			Inject:  []context.AnyKey{trigger},
			Provide: []context.AnyKey{storage},
		},
		activate: func(inst *Instance, payload any) error {
			return errors.New("broken")
		},
	}
	brokenID, err := s.Insert(broken, nil)
	if err != nil {
		t.Fatalf("Insert(broken) = %v", err)
	}
	waitForState(t, s, brokenID, StateFailed)

	// Withdraw the dependency and provide it again: the failed fiber must
	// not be re-activated automatically.
	if err := s.Retire(firstID); err != nil {
		t.Fatalf("Retire(trigger) = %v", err)
	}
	waitForState(t, s, firstID, StateInactive)
	if err := s.Remove(firstID); err != nil {
		t.Fatalf("Remove(trigger) = %v", err)
	}
	secondID, err := s.Insert(triggerProvider(), nil)
	if err != nil {
		t.Fatalf("Insert(trigger again) = %v", err)
	}
	waitForState(t, s, secondID, StateActive)

	time.Sleep(20 * time.Millisecond)
	if info, _ := s.Inspect(brokenID); info.State != StateFailed {
		t.Fatalf("failed fiber state = %v, want terminal failed", info.State)
	}
}

func TestActivationPanicIsContainedAndRollsBack(t *testing.T) {
	s := New()
	defer s.Close()

	storage := context.NewKey[string]("storage")
	rolledBack := make(chan struct{}, 1)

	comp := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			if err := Bind(inst, storage, "s3"); err != nil {
				return err
			}
			if err := inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					rolledBack <- struct{}{}
					return nil
				}, nil
			}); err != nil {
				return err
			}
			panic("boom")
		},
	}
	id, err := s.Insert(comp, nil)
	if err != nil {
		t.Fatalf("Insert() = %v", err)
	}
	waitForState(t, s, id, StateFailed)

	info, _ := s.Inspect(id)
	if info.Err == nil || !strings.Contains(info.Err.Error(), "panicked") {
		t.Fatalf("recorded failure = %v, want a panic-derived error", info.Err)
	}
	select {
	case <-rolledBack:
	case <-time.After(time.Second):
		t.Fatal("effects installed before the panic were not rolled back")
	}

	// The panic did not escape onto the scheduler: it still serves requests.
	if _, err := s.Insert(&testComponent{}, nil); err != nil {
		t.Fatalf("scheduler stopped serving after a panic: %v", err)
	}
}

func TestDeactivationErrorsAreRecordedAndRemainingInversesRun(t *testing.T) {
	s := New()
	defer s.Close()

	order := make(chan string, 4)
	comp := &testComponent{
		activate: func(inst *Instance, payload any) error {
			if err := inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					order <- "first"
					return errors.New("undo one failed")
				}, nil
			}); err != nil {
				return err
			}
			return inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					order <- "second"
					return nil
				}, nil
			})
		},
	}
	id, err := s.Insert(comp, nil)
	if err != nil {
		t.Fatalf("Insert() = %v", err)
	}
	waitForState(t, s, id, StateActive)

	if err := s.Retire(id); err != nil {
		t.Fatalf("Retire() = %v", err)
	}
	waitForState(t, s, id, StateInactive)

	// LIFO: the second effect reverts first, then the failing first effect.
	first := <-order
	second := <-order
	if first != "second" || second != "first" {
		t.Fatalf("revert order = [%s %s], want [second first]", first, second)
	}

	info, _ := s.Inspect(id)
	if info.Err == nil || !strings.Contains(info.Err.Error(), "undo one failed") {
		t.Fatalf("recorded teardown error = %v, want the inverse's error", info.Err)
	}
}

func TestRevisionReplacesAFailedFiberWithAFreshInstance(t *testing.T) {
	s := New()
	defer s.Close()

	key := context.NewKey[string]("key")

	broken := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{key}},
		activate: func(inst *Instance, payload any) error {
			return errors.New("broken")
		},
	}
	brokenID, err := s.Insert(broken, nil)
	if err != nil {
		t.Fatalf("Insert(broken) = %v", err)
	}
	waitForState(t, s, brokenID, StateFailed)

	if err := s.Remove(brokenID); err != nil {
		t.Fatalf("Remove(broken) = %v", err)
	}
	if _, ok := s.Inspect(brokenID); ok {
		t.Fatal("the failed fiber survived removal")
	}

	healthy := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{key}},
		activate: func(inst *Instance, payload any) error {
			return Bind(inst, key, "ok")
		},
	}
	freshID, err := s.Insert(healthy, nil)
	if err != nil {
		t.Fatalf("Insert(healthy) = %v", err)
	}
	if freshID == brokenID {
		t.Fatal("the revision reused the failed instance's identity")
	}
	waitForState(t, s, freshID, StateActive)
}
