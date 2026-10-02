package conformance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cucumber/godog"

	spc "github.com/DaviMGDev/memento/context"
	rt "github.com/DaviMGDev/memento/runtime"
)

func registerEffectSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a component that binds the key "([^"]*)" to a value$`, stepBindToValue)
	sc.Step(`^a component that binds the key "([^"]*)"$`, stepBindKey)
	sc.Step(`^the component then binds the key "([^"]*)"$`, stepThenBindKey)
	sc.Step(`^the component is unloaded$`, stepUnloaded)
	sc.Step(`^the key "([^"]*)" is unbound$`, stepKeyUnbound)
	sc.Step(`^the context state is equivalent to its state before the component loaded$`, stepStateEquivalent)
	sc.Step(`^the inverse of "([^"]*)" runs before the inverse of "([^"]*)"$`, stepInverseBefore)
	sc.Step(`^unload is requested twice$`, stepUnloadTwice)
	sc.Step(`^the runtime reverts the binding exactly once$`, stepRevertsOnce)
	sc.Step(`^a component whose second effect fails$`, stepSecondEffectFails)
	sc.Step(`^the component is activated$`, stepActivated)
	sc.Step(`^the component state is "([^"]*)"$`, stepStateIs)
	sc.Step(`^the first effect is reverted$`, stepFirstEffectReverted)
	sc.Step(`^the component is not activated again automatically$`, stepNotActivatedAgain)
	sc.Step(`^a component whose activation panics$`, stepActivationPanics)
	sc.Step(`^the effects installed before the panic are reverted$`, stepEffectsReverted)
	sc.Step(`^a component that sends a message to an external peer$`, stepSendsMessage)
	sc.Step(`^the tracked binding is unbound$`, stepTrackedBindingUnbound)
	sc.Step(`^the peer keeps the message$`, stepPeerKeepsMessage)
	sc.Step(`^a key whose comparator treats two handles as equivalent$`, stepComparatorKey)
	sc.Step(`^a component that renames such a handle$`, stepRenamesHandle)
	sc.Step(`^the handle state is equivalent under the comparator$`, stepHandleEquivalent)
}

func stepBindToValue(ctx context.Context, name string) error {
	w := worldFrom(ctx)
	w.before = w.recordSnapshot()

	key := w.key(name)
	comp := &testComponent{
		decls: rt.Declarations{Provide: []spc.AnyKey{key}},
		activate: func(inst *rt.Instance, _ any) error {
			w.mu.Lock()
			w.inst = inst
			w.mu.Unlock()
			if err := rt.Bind(inst, key, "value"); err != nil {
				return err
			}
			w.recordSet(name, "value")
			return inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					w.recordDel(name)
					return nil
				}, nil
			})
		},
	}
	if err := w.insert("", comp); err != nil {
		return err
	}
	return w.waitState(w.fiber, rt.StateActive)
}

func stepBindKey(ctx context.Context, name string) error {
	w := worldFrom(ctx)
	w.mu.Lock()
	w.lastKey = name
	w.mu.Unlock()

	key := w.key(name)
	comp := &testComponent{
		decls: rt.Declarations{Provide: []spc.AnyKey{key}},
		activate: func(inst *rt.Instance, _ any) error {
			w.mu.Lock()
			w.inst = inst
			w.mu.Unlock()
			if err := rt.Bind(inst, key, name); err != nil {
				return err
			}
			w.recordSet(name, name)
			return inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					w.appendOrder(name)
					w.recordDel(name)
					w.incReverts(name)
					return nil
				}, nil
			})
		},
	}
	if err := w.insert("", comp); err != nil {
		return err
	}
	return w.waitState(w.fiber, rt.StateActive)
}

func stepThenBindKey(ctx context.Context, name string) error {
	w := worldFrom(ctx)
	inst := w.instance()
	if inst == nil {
		return errors.New("no component has been activated")
	}
	key := w.key(name)
	if err := rt.Bind(inst, key, name); err != nil {
		return err
	}
	w.recordSet(name, name)
	w.mu.Lock()
	w.lastKey = name
	w.mu.Unlock()
	return inst.Context().RegisterEffect(func() (func() error, error) {
		return func() error {
			w.appendOrder(name)
			w.recordDel(name)
			w.incReverts(name)
			return nil
		}, nil
	})
}

func stepUnloaded(ctx context.Context) error {
	w := worldFrom(ctx)
	id := w.fiber
	if err := w.sched.Remove(id); err != nil {
		return err
	}
	return w.waitGone(id)
}

func stepKeyUnbound(ctx context.Context, name string) error {
	w := worldFrom(ctx)
	if w.recordHas(name) {
		return fmt.Errorf("binding %q is still recorded", name)
	}
	if inst := w.instance(); inst != nil {
		if _, ok := w.key(name).Lookup(inst.Context()); ok {
			return fmt.Errorf("binding %q is still installed in the context", name)
		}
	}
	return nil
}

func stepStateEquivalent(ctx context.Context) error {
	w := worldFrom(ctx)
	if got := w.recordSnapshot(); !equalState(w.before, got) {
		return fmt.Errorf("state = %v, want the pre-load state %v", got, w.before)
	}
	return nil
}

func stepInverseBefore(ctx context.Context, first, second string) error {
	w := worldFrom(ctx)
	order := w.orderSnapshot()
	i, j := indexOf(order, first), indexOf(order, second)
	if i < 0 || j < 0 {
		return fmt.Errorf("inverse order %v does not contain both %q and %q", order, first, second)
	}
	if i > j {
		return fmt.Errorf("inverse of %q ran after %q (order %v)", first, second, order)
	}
	return nil
}

func indexOf(values []string, want string) int {
	for i, v := range values {
		if v == want {
			return i
		}
	}
	return -1
}

func stepUnloadTwice(ctx context.Context) error {
	w := worldFrom(ctx)
	id := w.fiber
	if err := w.sched.Retire(id); err != nil {
		return err
	}
	if err := w.waitState(id, rt.StateInactive); err != nil {
		return err
	}
	// The redundant request must be a no-op.
	if err := w.sched.Retire(id); err != nil {
		return err
	}
	return w.waitState(id, rt.StateInactive)
}

func stepRevertsOnce(ctx context.Context) error {
	w := worldFrom(ctx)
	w.mu.Lock()
	name := w.lastKey
	w.mu.Unlock()
	if got := w.revertCount(name); got != 1 {
		return fmt.Errorf("binding %q was reverted %d times, want exactly once", name, got)
	}
	return nil
}

func stepSecondEffectFails(ctx context.Context) error {
	w := worldFrom(ctx)
	key := w.key("storage")
	w.mu.Lock()
	w.lastKey = "storage"
	w.mu.Unlock()

	comp := &testComponent{
		decls: rt.Declarations{Provide: []spc.AnyKey{key}},
		activate: func(inst *rt.Instance, _ any) error {
			w.mu.Lock()
			w.inst = inst
			w.mu.Unlock()
			if err := rt.Bind(inst, key, "first"); err != nil {
				return err
			}
			w.recordSet("storage", "first")
			if err := inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					w.appendEvent("first-effect-reverted")
					w.recordDel("storage")
					return nil
				}, nil
			}); err != nil {
				return err
			}
			return errors.New("the second effect failed")
		},
	}
	return w.insert("", comp)
}

func stepActivated(ctx context.Context) error {
	w := worldFrom(ctx)
	return w.waitSettled(w.fiber)
}

func stepStateIs(ctx context.Context, want string) error {
	w := worldFrom(ctx)
	state, ok := w.stateOf(w.fiber)
	if !ok {
		return errors.New("the component has no fiber")
	}
	if state.String() != want {
		return fmt.Errorf("component state = %q, want %q", state, want)
	}
	return nil
}

func stepFirstEffectReverted(ctx context.Context) error {
	w := worldFrom(ctx)
	if !w.hasEvent("first-effect-reverted") {
		return errors.New("the first effect's inverse never ran")
	}
	return nil
}

func stepNotActivatedAgain(ctx context.Context) error {
	w := worldFrom(ctx)
	if err := w.sched.Reload(w.fiber, nil); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	state, ok := w.stateOf(w.fiber)
	if !ok || state != rt.StateFailed {
		return fmt.Errorf("state = %v (ok=%v), want the component to stay failed", state, ok)
	}
	return nil
}

func stepActivationPanics(ctx context.Context) error {
	w := worldFrom(ctx)
	key := w.key("storage")
	comp := &testComponent{
		decls: rt.Declarations{Provide: []spc.AnyKey{key}},
		activate: func(inst *rt.Instance, _ any) error {
			w.mu.Lock()
			w.inst = inst
			w.mu.Unlock()
			if err := rt.Bind(inst, key, "first"); err != nil {
				return err
			}
			w.recordSet("storage", "first")
			if err := inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					w.appendEvent("panic-effect-reverted")
					w.recordDel("storage")
					return nil
				}, nil
			}); err != nil {
				return err
			}
			panic("boom")
		},
	}
	return w.insert("", comp)
}

func stepEffectsReverted(ctx context.Context) error {
	w := worldFrom(ctx)
	if !w.hasEvent("panic-effect-reverted") {
		return errors.New("effects installed before the panic were not reverted")
	}
	return nil
}

func stepSendsMessage(ctx context.Context) error {
	w := worldFrom(ctx)
	key := w.key("storage")
	comp := &testComponent{
		decls: rt.Declarations{Provide: []spc.AnyKey{key}},
		activate: func(inst *rt.Instance, _ any) error {
			w.mu.Lock()
			w.inst = inst
			w.mu.Unlock()
			if err := rt.Bind(inst, key, "value"); err != nil {
				return err
			}
			w.recordSet("storage", "value")
			if err := inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					w.recordDel("storage")
					return nil
				}, nil
			}); err != nil {
				return err
			}
			w.emit("peer", "message")
			return nil
		},
	}
	if err := w.insert("", comp); err != nil {
		return err
	}
	return w.waitState(w.fiber, rt.StateActive)
}

func stepTrackedBindingUnbound(ctx context.Context) error {
	return stepKeyUnbound(ctx, "storage")
}

func stepPeerKeepsMessage(ctx context.Context) error {
	w := worldFrom(ctx)
	if got := len(w.emissions("peer")); got != 1 {
		return fmt.Errorf("peer holds %d messages, want the one emission", got)
	}
	return nil
}

func stepComparatorKey(ctx context.Context) error {
	w := worldFrom(ctx)
	w.handleKeys["handle"] = spc.NewKeyWithComparator[handle]("handle", func(a, b handle) bool {
		return a.ID == b.ID
	})
	return nil
}

func stepRenamesHandle(ctx context.Context) error {
	w := worldFrom(ctx)
	key := w.handleKeys["handle"]

	w.mu.Lock()
	w.original = handle{ID: 1, Name: "before"}
	w.current = handle{ID: 1, Name: "renamed"}
	w.mu.Unlock()

	comp := &testComponent{
		decls: rt.Declarations{Provide: []spc.AnyKey{key}},
		activate: func(inst *rt.Instance, _ any) error {
			w.mu.Lock()
			w.inst = inst
			w.mu.Unlock()
			// The component renamed the handle; its inverse restores the
			// handle's identity, not its exact representation.
			return inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					w.mu.Lock()
					w.current = handle{ID: 1, Name: "restored"}
					w.mu.Unlock()
					return nil
				}, nil
			})
		},
	}
	if err := w.insert("", comp); err != nil {
		return err
	}
	return w.waitState(w.fiber, rt.StateActive)
}

func stepHandleEquivalent(ctx context.Context) error {
	w := worldFrom(ctx)
	key := w.handleKeys["handle"]

	w.mu.Lock()
	original, current := w.original, w.current
	w.mu.Unlock()

	if original.Name == current.Name {
		return errors.New("the handle was never renamed, so the comparator is not exercised")
	}
	if !key.Equivalent(original, current) {
		return fmt.Errorf("handle state %+v is not equivalent to %+v under the declared comparator", current, original)
	}
	return nil
}
