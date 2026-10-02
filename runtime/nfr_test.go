package runtime

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/DaviMGDev/memento/context"
)

// TestBindingChangesNotifyOnlyAffectedSubscribers checks the notification
// NFR: a change is classified against the per-key subscriber index, so only
// fibers that declared the changed key are re-evaluated.
func TestBindingChangesNotifyOnlyAffectedSubscribers(t *testing.T) {
	s := New()
	defer s.Close()

	storage := context.NewKey[string]("storage")
	cache := context.NewKey[string]("cache")

	storageInst := make(chan *Instance, 1)
	storageProvider := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{storage}},
		activate: func(inst *Instance, _ any) error {
			storageInst <- inst
			return Bind(inst, storage, "v1")
		},
	}
	cacheProvider := &testComponent{
		decls: Declarations{Provide: []context.AnyKey{cache}},
		activate: func(inst *Instance, _ any) error {
			return Bind(inst, cache, "c1")
		},
	}
	storageID, err := s.Insert(storageProvider, nil)
	if err != nil {
		t.Fatalf("Insert(storage provider) = %v", err)
	}
	cacheID, err := s.Insert(cacheProvider, nil)
	if err != nil {
		t.Fatalf("Insert(cache provider) = %v", err)
	}
	waitForState(t, s, storageID, StateActive)
	waitForState(t, s, cacheID, StateActive)

	var storageActivations, cacheActivations int32
	storageDep := &testComponent{
		decls: Declarations{Inject: []context.AnyKey{storage}},
		activate: func(*Instance, any) error {
			atomic.AddInt32(&storageActivations, 1)
			return nil
		},
	}
	cacheDep := &testComponent{
		decls: Declarations{Inject: []context.AnyKey{cache}},
		activate: func(*Instance, any) error {
			atomic.AddInt32(&cacheActivations, 1)
			return nil
		},
	}
	storageDepID, err := s.Insert(storageDep, nil)
	if err != nil {
		t.Fatalf("Insert(storage dependent) = %v", err)
	}
	cacheDepID, err := s.Insert(cacheDep, nil)
	if err != nil {
		t.Fatalf("Insert(cache dependent) = %v", err)
	}
	waitForState(t, s, storageDepID, StateActive)
	waitForState(t, s, cacheDepID, StateActive)

	// An in-place overwrite re-evaluates the storage subscriber; the cache
	// subscriber is not part of the affected set.
	inst := <-storageInst
	if err := Bind(inst, storage, "v2"); err != nil {
		t.Fatalf("overwrite = %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	if got := atomic.LoadInt32(&storageActivations); got != 1 {
		t.Fatalf("storage dependent activations = %d, want 1 (neutral change)", got)
	}
	if got := atomic.LoadInt32(&cacheActivations); got != 1 {
		t.Fatalf("cache dependent activations = %d, want 1 (never notified)", got)
	}
	if info, _ := s.Inspect(storageDepID); info.State != StateActive {
		t.Fatalf("storage dependent state = %v, want active", info.State)
	}
	if info, _ := s.Inspect(cacheDepID); info.State != StateActive {
		t.Fatalf("cache dependent state = %v, want active", info.State)
	}

	// The index is keyed by the changed key and holds exactly the fibers
	// that declared it.
	if got := len(s.subscribers[storage.ID()]); got != 1 {
		t.Fatalf("subscribers[storage] holds %d fibers, want 1", got)
	}
	if got := len(s.subscribers[cache.ID()]); got != 1 {
		t.Fatalf("subscribers[cache] holds %d fibers, want 1", got)
	}
	if got := len(s.subscribers); got != 2 {
		t.Fatalf("the subscriber index has %d keys, want 2", got)
	}
}
