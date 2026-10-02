package context_test

import (
	"testing"

	"github.com/DaviMGDev/memento/context"
)

type handle struct {
	ID   int
	Name string
}

func TestDefaultEquivalenceIsDeepEquality(t *testing.T) {
	key := context.NewKey[handle]("handle")
	a := handle{ID: 1, Name: "conn"}
	b := handle{ID: 1, Name: "conn"}

	if !key.Equivalent(a, b) {
		t.Fatal("identical handles are not deeply equal")
	}
	if key.Equivalent(a, handle{ID: 1, Name: "renamed"}) {
		t.Fatal("deep equality must treat a rename as a difference")
	}
}

func TestDeclaredComparatorBoundsRecovery(t *testing.T) {
	key := context.NewKeyWithComparator[handle]("handle", func(a, b handle) bool {
		return a.ID == b.ID
	})

	before := handle{ID: 7, Name: "pooled-07"}
	after := before
	after.Name = "pooled-renamed"

	if !key.Equivalent(before, after) {
		t.Fatal("the declared comparator must treat a rename as equivalent")
	}
	if key.Equivalent(handle{ID: 7}, handle{ID: 8}) {
		t.Fatal("the declared comparator compared different handles as equal")
	}
}

func TestRenamedHandleRecoversUpToDeclaredEquivalence(t *testing.T) {
	// features/effects.feature: "A key with declared equivalence recovers up to it".
	byID := context.NewKeyWithComparator[handle]("handle", func(a, b handle) bool {
		return a.ID == b.ID
	})
	exact := context.NewKey[handle]("handle-exact")

	original := handle{ID: 3, Name: "open"}
	renamed := handle{ID: 3, Name: "renamed-on-release"}

	if !byID.Equivalent(original, renamed) {
		t.Fatal("declared equivalence must treat the renamed handle as recovered")
	}
	if exact.Equivalent(original, renamed) {
		t.Fatal("the deep-equality default must treat the rename as a difference")
	}
}

func TestNilComparatorFallsBackToDeepEquality(t *testing.T) {
	key := context.NewKeyWithComparator[handle]("handle", nil)
	a := handle{ID: 1, Name: "conn"}

	if !key.Equivalent(a, handle{ID: 1, Name: "conn"}) {
		t.Fatal("a nil comparator must fall back to deep equality")
	}
	if key.Equivalent(a, handle{ID: 2, Name: "conn"}) {
		t.Fatal("deep equality compared different handles as equal")
	}
}
