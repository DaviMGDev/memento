package runtime

import (
	"testing"
	"time"

	"github.com/DaviMGDev/memento/context"
)

func TestInPlaceOverwriteIsNeutral(t *testing.T) {
	s := New()
	defer s.Close()

	storage := context.NewKey[string]("storage")
	providerInst := make(chan *Instance, 1)
	provider := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			providerInst <- inst
			return Bind(inst, storage, "v1")
		},
	}
	providerID, err := s.Insert(provider, nil)
	if err != nil {
		t.Fatalf("Insert(provider) = %v", err)
	}
	waitForState(t, s, providerID, StateActive)

	dependentInst := make(chan *Instance, 1)
	dependent := &testComponent{
		decls: Declarations{Inject: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			dependentInst <- inst
			return nil
		},
	}
	depID, err := s.Insert(dependent, nil)
	if err != nil {
		t.Fatalf("Insert(dependent) = %v", err)
	}
	waitForState(t, s, depID, StateActive)

	before, _ := s.Inspect(depID)

	// The provider overwrites its own binding in place: same owner, new
	// value. This is a value change, not a provider change.
	inst := <-providerInst
	if err := Bind(inst, storage, "v2"); err != nil {
		t.Fatalf("overwrite = %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	after, _ := s.Inspect(depID)
	if after.State != StateActive {
		t.Fatalf("dependent state after overwrite = %v, want active", after.State)
	}
	if !viewEqual(before.CommittedView, after.CommittedView) {
		t.Fatal("an in-place overwrite changed the committed view")
	}

	// The dependent keeps reading the same provider, now at its new value.
	dep := <-dependentInst
	if got, ok := Get(dep, storage); !ok || got != "v2" {
		t.Fatalf("dependent read = (%q, %v), want (\"v2\", true)", got, ok)
	}
}

func TestReplacedProviderReactivatesTheDependent(t *testing.T) {
	s := New()
	defer s.Close()

	storage := context.NewKey[string]("storage")
	newProvider := func(value string) *testComponent {
		return &testComponent{
			decls: Declarations{Provide: []context.AnyKey{storage}},
			activate: func(inst *Instance, payload any) error {
				return Bind(inst, storage, value)
			},
		}
	}

	firstID, err := s.Insert(newProvider("first"), nil)
	if err != nil {
		t.Fatalf("Insert(first provider) = %v", err)
	}
	waitForState(t, s, firstID, StateActive)

	dependentInst := make(chan *Instance, 1)
	dependent := &testComponent{
		decls: Declarations{Inject: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			dependentInst <- inst
			return nil
		},
	}
	depID, err := s.Insert(dependent, nil)
	if err != nil {
		t.Fatalf("Insert(dependent) = %v", err)
	}
	waitForState(t, s, depID, StateActive)
	<-dependentInst

	// The first provider is withdrawn; the dependent deactivates.
	if err := s.Retire(firstID); err != nil {
		t.Fatalf("Retire(first) = %v", err)
	}
	waitForState(t, s, firstID, StateInactive)
	waitForState(t, s, depID, StateInactive)

	// A different provider takes the key over.
	if err := s.Remove(firstID); err != nil {
		t.Fatalf("Remove(first) = %v", err)
	}
	if _, ok := s.Inspect(firstID); ok {
		t.Fatal("removed provider is still registered")
	}
	secondID, err := s.Insert(newProvider("second"), nil)
	if err != nil {
		t.Fatalf("Insert(second provider) = %v", err)
	}
	waitForState(t, s, secondID, StateActive)
	waitForState(t, s, depID, StateActive)

	info, _ := s.Inspect(depID)
	if got := info.CommittedView[storage.ID()]; got != secondID {
		t.Fatalf("dependent committed to %v, want the new provider %v", got, secondID)
	}
	dep := <-dependentInst
	if got, ok := Get(dep, storage); !ok || got != "second" {
		t.Fatalf("dependent read = (%q, %v), want (\"second\", true)", got, ok)
	}
}

func TestUndeclaredKeyChangesAreIgnored(t *testing.T) {
	s := New()
	defer s.Close()

	storage := context.NewKey[string]("storage")
	cache := context.NewKey[string]("cache")

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

	dependent := &testComponent{
		decls: Declarations{Inject: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			return nil
		},
	}
	depID, err := s.Insert(dependent, nil)
	if err != nil {
		t.Fatalf("Insert(dependent) = %v", err)
	}
	waitForState(t, s, depID, StateActive)
	before, _ := s.Inspect(depID)

	cacheProvider := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{cache}},
		activate: func(inst *Instance, payload any) error {
			return Bind(inst, cache, "c1")
		},
	}
	cacheID, err := s.Insert(cacheProvider, nil)
	if err != nil {
		t.Fatalf("Insert(cache provider) = %v", err)
	}
	waitForState(t, s, cacheID, StateActive)

	time.Sleep(20 * time.Millisecond)
	after, _ := s.Inspect(depID)
	if after.State != StateActive {
		t.Fatalf("dependent state = %v, want it undisturbed by an undeclared key", after.State)
	}
	if !viewEqual(before.CommittedView, after.CommittedView) {
		t.Fatal("an undeclared key change altered the committed view")
	}
}
