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

// mapState observes a map-valued key and keeps a shadow copy for computing
// inverses.
type mapState struct {
	ctx  *spc.Context
	key  spc.Key[map[string]string]
	live map[string]string
}

func (s *mapState) Snapshot() any {
	value, ok := s.key.Lookup(s.ctx)
	if !ok {
		return map[string]string{}
	}
	out := make(map[string]string, len(value))
	for k, v := range value {
		out[k] = v
	}
	return out
}

func (s *mapState) Equivalent(before, after any) bool {
	return reflect.DeepEqual(before, after)
}

func (s *mapState) shadow() map[string]string {
	out := make(map[string]string, len(s.live))
	for k, v := range s.live {
		out[k] = v
	}
	return out
}

func TestCoeffectCommutativityHoldsOnIndependentOperations(t *testing.T) {
	ctx := spc.NewContext(spc.RootFiber).Derive(1)
	acc := ctx.Effects()
	key := spc.NewKey[map[string]string]("map")
	state := &mapState{ctx: ctx, key: key, live: map[string]string{}}

	put := func(k, v string) OutcomeOperation {
		return func() (any, func() error, error) {
			previous := state.shadow()
			next := state.shadow()
			next[k] = v
			if err := key.Bind(ctx, next); err != nil {
				return nil, nil, err
			}
			state.live = next
			return "ok", func() error {
				state.live = previous
				return nil
			}, nil
		}
	}
	get := func(k string) OutcomeOperation {
		return func() (any, func() error, error) {
			v, ok := state.live[k]
			return []any{v, ok}, nil, nil
		}
	}

	ops := []OutcomeOperation{put("a", "1"), put("b", "2"), get("c")}
	if err := CheckCoeffectCommutativity(acc, state, ops); err != nil {
		t.Fatal(err)
	}
}

func TestCoeffectCommutativityDetectsDependentOperations(t *testing.T) {
	ctx := spc.NewContext(spc.RootFiber).Derive(1)
	acc := ctx.Effects()
	key := spc.NewKey[map[string]string]("map")
	state := &mapState{ctx: ctx, key: key, live: map[string]string{}}

	put := func(k, v string) OutcomeOperation {
		return func() (any, func() error, error) {
			previous := state.shadow()
			next := state.shadow()
			next[k] = v
			if err := key.Bind(ctx, next); err != nil {
				return nil, nil, err
			}
			state.live = next
			return "ok", func() error {
				state.live = previous
				return nil
			}, nil
		}
	}

	// Two writes to the same entry are dependent: the orders disagree.
	ops := []OutcomeOperation{put("a", "1"), put("a", "2")}
	if err := CheckCoeffectCommutativity(acc, state, ops); err == nil {
		t.Fatal("CheckCoeffectCommutativity accepted dependent operations")
	}
}

func TestLIFOReversalOnRandomSequences(t *testing.T) {
	ctx := spc.NewContext(spc.RootFiber).Derive(1)
	acc := ctx.Effects()
	rng := rand.New(rand.NewSource(11))

	for i := 0; i < 200; i++ {
		var applied, reverted []int
		length := 1 + rng.Intn(8)
		for j := 0; j < length; j++ {
			j := j
			applied = append(applied, j)
			acc.Push(func() error {
				reverted = append(reverted, j)
				return nil
			})
		}
		if err := acc.Revert(); err != nil {
			t.Fatalf("iteration %d: Revert() = %v", i, err)
		}
		if len(reverted) != len(applied) {
			t.Fatalf("iteration %d: reverted %d effects, want %d", i, len(reverted), len(applied))
		}
		for k := range applied {
			if reverted[k] != applied[len(applied)-1-k] {
				t.Fatalf("iteration %d: revert order %v, want the reverse of %v", i, reverted, applied)
			}
		}
	}
}

func randomPayload(rng *rand.Rand) string { return fmt.Sprintf("p%d", rng.Intn(5)) }

func TestReconciliationConvergesOnRandomRevisions(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	for run := 0; run < 15; run++ {
		w := newWorld()
		if err := w.ensureConfigFactories(); err != nil {
			t.Fatal(err)
		}
		w.setEntry("database", "database", "v0", true)
		w.setEntry("console", "console", "c0", true)
		if err := w.applyConfig(); err != nil {
			t.Fatalf("run %d: initial reconcile: %v", run, err)
		}

		for step := 0; step < 12; step++ {
			switch rng.Intn(5) {
			case 0:
				ref := "database"
				if rng.Intn(2) == 0 {
					ref = "cache"
				}
				w.setEntry("database", ref, randomPayload(rng), true)
			case 1:
				w.setEntry("console", "console", randomPayload(rng), rng.Intn(2) == 0)
			case 2:
				w.mu.Lock()
				delete(w.entries, "console")
				w.mu.Unlock()
			case 3:
				w.setEntry("console", "console", randomPayload(rng), true)
			case 4:
				w.setEntry("database", "database", randomPayload(rng), rng.Intn(2) == 0)
			}
			if err := w.applyConfig(); err != nil {
				t.Fatalf("run %d step %d: reconcile: %v", run, step, err)
			}
		}

		fresh := newWorld()
		if err := fresh.ensureConfigFactories(); err != nil {
			t.Fatal(err)
		}
		w.mu.Lock()
		for id, entry := range w.entries {
			fresh.entries[id] = entry
		}
		w.mu.Unlock()
		if err := fresh.applyConfig(); err != nil {
			t.Fatalf("run %d: from-scratch reconcile: %v", run, err)
		}

		if got, want := w.recordSnapshot(), fresh.recordSnapshot(); !equalState(got, want) {
			t.Fatalf("run %d: incremental bindings %v, from-scratch %v", run, got, want)
		}
		incremental, fromScratch := w.snapshotConfig(), fresh.snapshotConfig()
		if len(incremental) != len(fromScratch) {
			t.Fatalf("run %d: incremental has %d entries, from-scratch has %d", run, len(incremental), len(fromScratch))
		}
		for id, snap := range incremental {
			other, ok := fromScratch[id]
			if !ok || other.state != snap.state {
				t.Fatalf("run %d: entry %q incremental %v, from-scratch %v", run, id, snap.state, other.state)
			}
		}

		w.close()
		fresh.close()
	}
}
