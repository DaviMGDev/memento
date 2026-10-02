package runtime

import (
	"testing"

	"github.com/DaviMGDev/memento/context"
)

func TestFiberCarriesDeclarationsPayloadAndState(t *testing.T) {
	root := context.NewContext(context.RootFiber)
	storage := context.NewKey[string]("storage")
	cache := context.NewKey[int]("cache")
	decls := Declarations{
		Inject:  []context.AnyKey{storage},
		Provide: []context.AnyKey{cache},
	}

	f := newFiber(7, root, decls, "cfg")

	if f.ID() != 7 {
		t.Fatalf("ID() = %v, want 7", f.ID())
	}
	if f.State() != StateInactive {
		t.Fatalf("State() = %v, want inactive", f.State())
	}
	if f.Payload() != "cfg" {
		t.Fatalf("Payload() = %v, want \"cfg\"", f.Payload())
	}

	got := f.Declarations()
	if len(got.Inject) != 1 || got.Inject[0].ID() != storage.ID() {
		t.Fatalf("Inject = %v, want the storage key", got.Inject)
	}
	if len(got.Provide) != 1 || got.Provide[0].ID() != cache.ID() {
		t.Fatalf("Provide = %v, want the cache key", got.Provide)
	}

	if f.Context().Fiber() != 7 {
		t.Fatalf("context fiber = %v, want 7", f.Context().Fiber())
	}
	if f.Context().Parent() != root {
		t.Fatal("fiber context is not derived from the parent context")
	}
	if f.Context().Effects() == nil {
		t.Fatal("fiber context must own an effect accumulator")
	}
}

func TestFiberCommittedViewIsCapturedAndCopied(t *testing.T) {
	root := context.NewContext(context.RootFiber)
	one := context.NewKey[string]("one")
	two := context.NewKey[int]("two")
	f := newFiber(1, root, Declarations{Inject: []context.AnyKey{one, two}}, nil)

	if f.CommittedView() != nil {
		t.Fatal("a fresh fiber already has a committed view")
	}

	view := CommittedView{one.ID(): 10, two.ID(): 20}
	f.commit(view)

	got := f.CommittedView()
	if got[one.ID()] != 10 || got[two.ID()] != 20 {
		t.Fatalf("committed view = %v, want {one:10 two:20}", got)
	}

	got[one.ID()] = 99
	if f.CommittedView()[one.ID()] != 10 {
		t.Fatal("CommittedView leaked internal state through the returned copy")
	}

	view[one.ID()] = 42
	if f.CommittedView()[one.ID()] != 10 {
		t.Fatal("commit must copy the view, not alias it")
	}
}

func TestFiberStateTransitionsAndStrings(t *testing.T) {
	root := context.NewContext(context.RootFiber)
	f := newFiber(1, root, Declarations{}, nil)

	f.setState(StateLoading)
	if f.State() != StateLoading {
		t.Fatal("setState did not record loading")
	}
	f.setState(StateActive)
	f.setState(StateUnloading)
	f.setState(StateFailed)

	want := map[State]string{
		StateInactive:  "inactive",
		StateLoading:   "loading",
		StateActive:    "active",
		StateUnloading: "unloading",
		StateFailed:    "failed",
	}
	for s, name := range want {
		if s.String() != name {
			t.Fatalf("State(%d).String() = %q, want %q", s, s.String(), name)
		}
	}
}
