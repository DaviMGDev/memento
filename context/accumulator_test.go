package context

import (
	"errors"
	"slices"
	"testing"
)

func TestAccumulatorRevertsLIFOExactlyOnce(t *testing.T) {
	var acc Accumulator
	var order []int
	for _, n := range []int{1, 2, 3} {
		n := n
		acc.Push(func() error {
			order = append(order, n)
			return nil
		})
	}

	if got := acc.Pending(); got != 3 {
		t.Fatalf("Pending() = %d, want 3", got)
	}
	if err := acc.Revert(); err != nil {
		t.Fatalf("Revert() = %v, want nil", err)
	}
	if want := []int{3, 2, 1}; !slices.Equal(order, want) {
		t.Fatalf("revert order = %v, want %v", order, want)
	}
	if got := acc.Pending(); got != 0 {
		t.Fatalf("Pending() after Revert = %d, want 0", got)
	}

	// A redundant unload request must not run anything again.
	if err := acc.Revert(); err != nil {
		t.Fatalf("second Revert() = %v, want nil", err)
	}
	if len(order) != 3 {
		t.Fatalf("second Revert re-ran inverses: order = %v", order)
	}
}

func TestAccumulatorRecordsErrorsAndKeepsReverting(t *testing.T) {
	var acc Accumulator
	var order []int
	acc.Push(func() error {
		order = append(order, 1)
		return errors.New("first failed")
	})
	acc.Push(func() error {
		order = append(order, 2)
		return nil
	})

	err := acc.Revert()
	if err == nil || err.Error() != "first failed" {
		t.Fatalf("Revert() = %v, want the first inverse's error", err)
	}
	if want := []int{2, 1}; !slices.Equal(order, want) {
		t.Fatalf("revert order = %v, want %v (a failure must not stop the rest)", order, want)
	}
	if got := acc.Pending(); got != 0 {
		t.Fatalf("Pending() = %d, want drained accumulator", got)
	}
}

func TestAccumulatorContainsPanics(t *testing.T) {
	var acc Accumulator
	ran := false
	acc.Push(func() error {
		panic("boom")
	})
	acc.Push(func() error {
		ran = true
		return nil
	})

	err := acc.Revert()
	if err == nil {
		t.Fatal("Revert() = nil, want a panic-derived error")
	}
	if !ran {
		t.Fatal("the inverse after a panicking one did not run")
	}
}

func TestDerivedContextsOwnAnAccumulator(t *testing.T) {
	root := NewContext(1)
	if root.Effects() != nil {
		t.Fatal("a root context has no fiber and must have no accumulator")
	}

	child := root.Derive(2)
	acc := child.Effects()
	if acc == nil {
		t.Fatal("a derived context must own an accumulator")
	}
	acc.Push(func() error { return errors.New("ran") })

	if err := child.Effects().Revert(); err == nil {
		t.Fatal("Revert on the context's accumulator did not run the pushed inverse")
	}
	if child.Effects() != acc {
		t.Fatal("Effects must return the same accumulator on every call")
	}
}
