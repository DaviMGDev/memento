package conformance

import (
	"fmt"
	"math/rand"
	"reflect"

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

// OutcomeOperation applies one coeffect operation, yields its outcome, and
// returns the inverse that reverts it.
type OutcomeOperation func() (outcome any, inverse func() error, err error)

// CheckCoeffectCommutativity checks the coeffect witness: for every pair of
// operations, applying them in either order reaches equivalent states and
// yields equal outcomes.
func CheckCoeffectCommutativity(acc *context.Accumulator, state WitnessState, ops []OutcomeOperation) error {
	if acc == nil {
		return fmt.Errorf("conformance: coeffect commutativity requires an accumulator")
	}
	if state == nil {
		return fmt.Errorf("conformance: coeffect commutativity requires a state")
	}
	if len(ops) == 0 {
		return fmt.Errorf("conformance: coeffect commutativity requires at least one operation")
	}
	for i := range ops {
		for j := i; j < len(ops); j++ {
			if err := checkCommutativity(acc, state, ops[i], ops[j]); err != nil {
				return fmt.Errorf("conformance: coeffect commutativity violated for operations %d and %d: %w", i, j, err)
			}
		}
	}
	return nil
}

func checkCommutativity(acc *context.Accumulator, state WitnessState, first, second OutcomeOperation) error {
	if err := acc.Revert(); err != nil {
		return err
	}

	abOutcomes, abState, err := runPair(acc, state, first, second)
	if err != nil {
		return err
	}
	// runPair returns the outcomes in application order, so the second
	// order yields them swapped.
	baOutcomes, baState, err := runPair(acc, state, second, first)
	if err != nil {
		return err
	}

	if !reflect.DeepEqual(abOutcomes[0], baOutcomes[1]) || !reflect.DeepEqual(abOutcomes[1], baOutcomes[0]) {
		return fmt.Errorf("outcomes differ: %v vs %v", abOutcomes, baOutcomes)
	}
	if !state.Equivalent(abState, baState) {
		return fmt.Errorf("states differ: %v vs %v", abState, baState)
	}
	return nil
}

func runPair(acc *context.Accumulator, state WitnessState, first, second OutcomeOperation) ([]any, any, error) {
	outcome1, inverse1, err := first()
	if err != nil {
		return nil, nil, err
	}
	acc.Push(inverse1)

	outcome2, inverse2, err := second()
	if err != nil {
		return nil, nil, err
	}
	acc.Push(inverse2)

	snapshot := state.Snapshot()
	if err := acc.Revert(); err != nil {
		return nil, nil, err
	}
	return []any{outcome1, outcome2}, snapshot, nil
}
