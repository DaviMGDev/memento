package conformance

import (
	"fmt"
	"math/rand"

	"github.com/DaviMGDev/memento/context"
)

// Operation applies one effect to the state under test and returns the
// inverse that reverts it. A nil inverse with a nil error means the
// operation registered its own inverse, as the built-in context operations
// do.
type Operation func() (inverse func() error, err error)

// WitnessState is the observable state a witness obligation is checked
// against: Snapshot reads it, and Equivalent decides whether two snapshots
// are observationally equivalent.
type WitnessState interface {
	Snapshot() any
	Equivalent(before, after any) bool
}

// CheckEffectWitness runs randomized effect sequences against acc and
// checks the effect witness: for every sequence, applying the effects and
// then reverting them returns a state equivalent to the starting one.
//
// The harness pushes each operation's inverse onto the accumulator and
// reverts the accumulator after every sequence, so exactly-once and LIFO
// recovery are exercised along with the witness itself. A deterministic rng
// makes failures reproducible.
func CheckEffectWitness(acc *context.Accumulator, state WitnessState, ops []Operation, iterations int, rng *rand.Rand) error {
	if acc == nil {
		return fmt.Errorf("conformance: effect witness requires an accumulator")
	}
	if state == nil {
		return fmt.Errorf("conformance: effect witness requires a state")
	}
	if len(ops) == 0 {
		return fmt.Errorf("conformance: effect witness requires at least one operation")
	}
	if iterations <= 0 {
		return fmt.Errorf("conformance: effect witness requires a positive iteration count")
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(1))
	}

	for i := 0; i < iterations; i++ {
		before := state.Snapshot()
		length := 1 + rng.Intn(8)
		for j := 0; j < length; j++ {
			inverse, err := ops[rng.Intn(len(ops))]()
			if err != nil {
				return fmt.Errorf("conformance: effect witness iteration %d step %d: %w", i, j, err)
			}
			acc.Push(inverse)
		}
		if err := acc.Revert(); err != nil {
			return fmt.Errorf("conformance: effect witness iteration %d: revert: %w", i, err)
		}
		if after := state.Snapshot(); !state.Equivalent(before, after) {
			return fmt.Errorf("conformance: effect witness violated at iteration %d: before %v, after %v", i, before, after)
		}
	}
	return nil
}
