package runtime

import (
	"fmt"
	"testing"
	"time"

	"github.com/DaviMGDev/memento/context"
)

func TestActivationWaitsForEveryDeclaredKey(t *testing.T) {
	s := New()
	defer s.Close()

	storage := context.NewKey[string]("storage")
	config := context.NewKey[int]("config")

	readStorage := make(chan string, 1)
	dependent := &testComponent{
		decls: Declarations{Inject: []context.AnyKey{storage, config}},
		activate: func(inst *Instance, payload any) error {
			v, ok := Get(inst, storage)
			if !ok {
				return fmt.Errorf("storage was not readable during activation")
			}
			readStorage <- v
			return nil
		},
	}
	depID, err := s.Insert(dependent, nil)
	if err != nil {
		t.Fatalf("Insert(dependent) = %v", err)
	}
	waitForState(t, s, depID, StateInactive)

	storageProvider := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			return Bind(inst, storage, "s3")
		},
	}
	storageID, err := s.Insert(storageProvider, nil)
	if err != nil {
		t.Fatalf("Insert(storage provider) = %v", err)
	}
	waitForState(t, s, storageID, StateActive)

	// One provided key out of two is not enough.
	time.Sleep(20 * time.Millisecond)
	if info, ok := s.Inspect(depID); !ok || info.State != StateInactive {
		t.Fatalf("dependent = (%+v, %v), want inactive until every key is provided", info, ok)
	}

	configProvider := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{config}},
		activate: func(inst *Instance, payload any) error {
			return Bind(inst, config, 7)
		},
	}
	configID, err := s.Insert(configProvider, nil)
	if err != nil {
		t.Fatalf("Insert(config provider) = %v", err)
	}
	waitForState(t, s, configID, StateActive)
	waitForState(t, s, depID, StateActive)

	select {
	case v := <-readStorage:
		if v != "s3" {
			t.Fatalf("activation read %q, want \"s3\"", v)
		}
	case <-time.After(time.Second):
		t.Fatal("activation never read its declared key")
	}

	info, ok := s.Inspect(depID)
	if !ok {
		t.Fatal("dependent disappeared")
	}
	if got := info.CommittedView[storage.ID()]; got != storageID {
		t.Fatalf("committed storage provider = %v, want %v", got, storageID)
	}
	if got := info.CommittedView[config.ID()]; got != configID {
		t.Fatalf("committed config provider = %v, want %v", got, configID)
	}
}

func TestProviderArrivingAfterTheDependentActivatesIt(t *testing.T) {
	s := New()
	defer s.Close()

	key := context.NewKey[string]("late")

	dependent := &testComponent{
		decls: Declarations{Inject: []context.AnyKey{key}},
		activate: func(inst *Instance, payload any) error {
			return nil
		},
	}
	depID, err := s.Insert(dependent, nil)
	if err != nil {
		t.Fatalf("Insert(dependent) = %v", err)
	}
	waitForState(t, s, depID, StateInactive)

	provider := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{key}},
		activate: func(inst *Instance, payload any) error {
			return Bind(inst, key, "here")
		},
	}
	providerID, err := s.Insert(provider, nil)
	if err != nil {
		t.Fatalf("Insert(provider) = %v", err)
	}
	waitForState(t, s, providerID, StateActive)
	waitForState(t, s, depID, StateActive)
}
