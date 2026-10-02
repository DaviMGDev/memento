package runtime

import (
	"slices"
	"testing"
	"time"

	"github.com/DaviMGDev/memento/context"
)

func TestRegistryIntrospection(t *testing.T) {
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

	dependent := &testComponent{
		decls: Declarations{Inject: []context.AnyKey{storage}},
		activate: func(inst *Instance, payload any) error {
			return inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error { return nil }, nil
			})
		},
	}
	depID, err := s.Insert(dependent, nil)
	if err != nil {
		t.Fatalf("Insert(dependent) = %v", err)
	}
	waitForState(t, s, depID, StateActive)

	snap := s.Snapshot()
	if len(snap.Fibers) != 2 {
		t.Fatalf("snapshot holds %d fibers, want 2", len(snap.Fibers))
	}
	byID := make(map[context.FiberID]FiberInfo, len(snap.Fibers))
	for _, fi := range snap.Fibers {
		byID[fi.ID] = fi
	}

	providerInfo, ok := byID[providerID]
	if !ok || providerInfo.State != StateActive {
		t.Fatalf("provider info = (%+v, %v), want active", providerInfo, ok)
	}
	if !slices.Contains(providerInfo.Provided, storage.ID()) {
		t.Fatalf("provider owns %v, want the storage key", providerInfo.Provided)
	}

	depInfo, ok := byID[depID]
	if !ok || depInfo.State != StateActive {
		t.Fatalf("dependent info = (%+v, %v), want active", depInfo, ok)
	}
	if got := depInfo.CommittedView[storage.ID()]; got != providerID {
		t.Fatalf("dependent committed view = %v, want provider %v", got, providerID)
	}

	if got := snap.Providers[storage.ID()]; got != providerID {
		t.Fatalf("binding owner = %v, want provider %v", got, providerID)
	}

	// The snapshot is a copy: mutating it must not affect the runtime.
	snap.Providers[storage.ID()] = 999
	if len(snap.Fibers) > 0 {
		snap.Fibers[0].State = StateFailed
	}
	again := s.Snapshot()
	if got := again.Providers[storage.ID()]; got != providerID {
		t.Fatalf("runtime provider changed through the snapshot copy: %v", got)
	}
	for _, fi := range again.Fibers {
		if fi.ID == providerID && fi.State != StateActive {
			t.Fatalf("runtime fiber changed through the snapshot copy: %+v", fi)
		}
	}

	// After withdrawal the ownership is gone and the committed view is
	// discarded once teardown has finished.
	if err := s.Retire(providerID); err != nil {
		t.Fatalf("Retire(provider) = %v", err)
	}
	waitForState(t, s, depID, StateInactive)
	waitForState(t, s, providerID, StateInactive)

	after := s.Snapshot()
	if _, owned := after.Providers[storage.ID()]; owned {
		t.Fatal("the key is still owned after its provider withdrew")
	}
	for _, fi := range after.Fibers {
		if fi.ID == depID && len(fi.CommittedView) != 0 {
			t.Fatalf("dependent committed view after teardown = %v, want it discarded", fi.CommittedView)
		}
	}
}

func TestInspectUnknownFiber(t *testing.T) {
	s := New()
	defer s.Close()

	if info, ok := s.Inspect(42); ok {
		t.Fatalf("Inspect(unknown) = (%+v, true), want not found", info)
	}
	time.Sleep(time.Millisecond)
}
