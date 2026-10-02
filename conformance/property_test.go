package conformance

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	spc "github.com/DaviMGDev/memento/context"
)

// modelState reads the observable state of the context under test through
// typed keys and keeps a shadow copy of the values for computing inverses.
type modelState struct {
	ctx    *spc.Context
	keys   map[string]spc.Key[string]
	values map[string]string
}

func (s *modelState) Snapshot() any {
	out := make(map[string]string, len(s.keys))
	for name, key := range s.keys {
		if v, ok := key.Lookup(s.ctx); ok {
			out[name] = v
		} else {
			out[name] = "<unbound>"
		}
	}
	return out
}

func (s *modelState) Equivalent(before, after any) bool {
	return reflect.DeepEqual(before, after)
}

func (s *modelState) set(name, value string) { s.values[name] = value }

func (s *modelState) get(name string) (string, bool) {
	v, ok := s.values[name]
	return v, ok
}

func (s *modelState) del(name string) { delete(s.values, name) }

func TestEffectWitnessHoldsOnRandomSequences(t *testing.T) {
	root := spc.NewContext(spc.RootFiber)
	ctx := root.Derive(1)
	acc := ctx.Effects()
	if acc == nil {
		t.Fatal("a derived context must own an accumulator")
	}

	storage := spc.NewKey[string]("storage")
	cache := spc.NewKey[string]("cache")
	resource := spc.NewKey[string]("resource")
	state := &modelState{
		ctx: ctx,
		keys: map[string]spc.Key[string]{
			"storage":  storage,
			"cache":    cache,
			"resource": resource,
		},
		values: make(map[string]string),
	}

	rng := rand.New(rand.NewSource(42))
	value := func() string { return fmt.Sprintf("v%d", rng.Intn(4)) }

	// A built-in effect: bind (or overwrite) a key through the typed API.
	bind := func(name string, key spc.Key[string]) Operation {
		return func() (func() error, error) {
			previous, had := state.get(name)
			v := value()
			if err := key.Bind(ctx, v); err != nil {
				return nil, err
			}
			state.set(name, v)
			return func() error {
				if had {
					state.set(name, previous)
				} else {
					state.del(name)
				}
				return nil
			}, nil
		}
	}

	// An explicit effect: install a custom resource through the escape
	// hatch and register its inverse.
	explicit := func() (func() error, error) {
		previous, had := state.get("resource")
		v := value()
		err := ctx.RegisterEffect(func() (func() error, error) {
			if err := resource.Bind(ctx, v); err != nil {
				return nil, err
			}
			return nil, nil
		})
		if err != nil {
			return nil, err
		}
		state.set("resource", v)
		return func() error {
			if had {
				state.set("resource", previous)
			} else {
				state.del("resource")
			}
			return nil
		}, nil
	}

	ops := []Operation{
		bind("storage", storage),
		bind("cache", cache),
		explicit,
	}
	if err := CheckEffectWitness(acc, state, ops, 300, rng); err != nil {
		t.Fatal(err)
	}

	for name, key := range state.keys {
		if _, ok := key.Lookup(ctx); ok {
			t.Fatalf("key %q is still bound after the witness run", name)
		}
	}
}

// alwaysUnequal is a state that never considers two snapshots equivalent,
// used to check that the harness reports a violation.
type alwaysUnequal struct{}

func (alwaysUnequal) Snapshot() any                     { return 1 }
func (alwaysUnequal) Equivalent(before, after any) bool { return false }

func TestEffectWitnessReportsViolations(t *testing.T) {
	ctx := spc.NewContext(spc.RootFiber).Derive(1)
	noop := func() (func() error, error) { return nil, nil }

	err := CheckEffectWitness(ctx.Effects(), alwaysUnequal{}, []Operation{noop}, 1, rand.New(rand.NewSource(7)))
	if err == nil {
		t.Fatal("CheckEffectWitness accepted a state that is never equivalent")
	}
}

func TestEffectWitnessRejectsMisuse(t *testing.T) {
	ctx := spc.NewContext(spc.RootFiber).Derive(1)
	noop := func() (func() error, error) { return nil, nil }
	state := &modelState{ctx: ctx, keys: map[string]spc.Key[string]{}, values: map[string]string{}}

	if err := CheckEffectWitness(nil, state, []Operation{noop}, 1, nil); err == nil {
		t.Fatal("CheckEffectWitness accepted a nil accumulator")
	}
	if err := CheckEffectWitness(ctx.Effects(), nil, []Operation{noop}, 1, nil); err == nil {
		t.Fatal("CheckEffectWitness accepted a nil state")
	}
	if err := CheckEffectWitness(ctx.Effects(), state, nil, 1, nil); err == nil {
		t.Fatal("CheckEffectWitness accepted no operations")
	}
	if err := CheckEffectWitness(ctx.Effects(), state, []Operation{noop}, 0, nil); err == nil {
		t.Fatal("CheckEffectWitness accepted a non-positive iteration count")
	}
}
