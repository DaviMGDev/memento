package runtime

import (
	"fmt"
	"testing"
	"time"

	"github.com/DaviMGDev/memento/context"
)

func TestDependentsDeactivateBeforeProviderInverses(t *testing.T) {
	s := New()
	defer s.Close()

	storage := context.NewKey[string]("storage")
	order := make(chan string, 8)

	provider := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			if err := Bind(inst, storage, "s3"); err != nil {
				return err
			}
			return inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					order <- "provider-undo"
					return nil
				}, nil
			})
		},
	}
	providerID, err := s.Insert(provider, nil)
	if err != nil {
		t.Fatalf("Insert(provider) = %v", err)
	}
	waitForState(t, s, providerID, StateActive)

	dependent := &testComponent{
		decls: Declarations{Inject: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			return inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					// Teardown still reads the withdrawing dependency
					// through the committed view.
					v, ok := Get(inst, storage)
					order <- fmt.Sprintf("dependent-undo read=%v ok=%v", v, ok)
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

	if err := s.Retire(providerID); err != nil {
		t.Fatalf("Retire(provider) = %v", err)
	}
	waitForState(t, s, depID, StateInactive)
	waitForState(t, s, providerID, StateInactive)

	first := <-order
	if first != "dependent-undo read=s3 ok=true" {
		t.Fatalf("first inverse = %q, want the dependent reading s3 while it withdraws", first)
	}
	second := <-order
	if second != "provider-undo" {
		t.Fatalf("second inverse = %q, want the provider's inverse after the dependent stopped", second)
	}
	select {
	case extra := <-order:
		t.Fatalf("unexpected third inverse: %q", extra)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestSeveralDependentsAllDrainBeforeProviderInverses(t *testing.T) {
	s := New()
	defer s.Close()

	storage := context.NewKey[string]("storage")
	done := make(chan string, 8)
	providerUndone := make(chan struct{}, 1)

	provider := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			if err := Bind(inst, storage, "s3"); err != nil {
				return err
			}
			return inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					providerUndone <- struct{}{}
					return nil
				}, nil
			})
		},
	}
	providerID, err := s.Insert(provider, nil)
	if err != nil {
		t.Fatalf("Insert(provider) = %v", err)
	}
	waitForState(t, s, providerID, StateActive)

	for i := 0; i < 3; i++ {
		i := i
		dependent := &testComponent{
			decls: Declarations{Inject: []context.AnyKey{storage}},
			activate: func(inst *Instance, payload any) error {
				return inst.Context().RegisterEffect(func() (func() error, error) {
					return func() error {
						done <- fmt.Sprintf("dependent-%d", i)
						return nil
					}, nil
				})
			},
		}
		id, err := s.Insert(dependent, nil)
		if err != nil {
			t.Fatalf("Insert(dependent %d) = %v", i, err)
		}
		waitForState(t, s, id, StateActive)
	}

	if err := s.Retire(providerID); err != nil {
		t.Fatalf("Retire(provider) = %v", err)
	}

	// All three dependents must complete their teardown before the
	// provider's inverse runs.
	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("only %d dependents finished teardown", i)
		}
	}
	select {
	case <-providerUndone:
	case <-time.After(time.Second):
		t.Fatal("the provider's inverse never ran")
	}
}
