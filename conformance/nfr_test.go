package conformance

import (
	"testing"

	spc "github.com/DaviMGDev/memento/context"
)

// TestEffectRegistrationAllocationShape checks the allocation NFR: an
// effect registration costs a constant amount of bookkeeping — one closure
// per effect, no per-registration structure beyond the accumulator entry.
func TestEffectRegistrationAllocationShape(t *testing.T) {
	root := spc.NewContext(spc.RootFiber)
	ctx := root.Derive(1)

	inverse := func() error { return nil }
	installer := func() (func() error, error) { return inverse, nil }

	allocs := testing.AllocsPerRun(200, func() {
		_ = ctx.RegisterEffect(installer)
		_ = ctx.Effects().Revert()
	})
	if allocs > 2 {
		t.Fatalf("effect registration allocates %.1f allocations per effect, want at most 2", allocs)
	}
}
