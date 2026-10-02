package conformance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	spc "github.com/DaviMGDev/memento/context"
	rt "github.com/DaviMGDev/memento/runtime"
)

func registerCoeffectSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a component that declares the key "([^"]*)"$`, stepDeclaresKey)
	sc.Step(`^a component that declares the key "([^"]*)" is loaded and inactive$`, stepDeclaresLoadedInactive)
	sc.Step(`^a component that declares the key "([^"]*)" is active$`, stepDeclaresActive)
	sc.Step(`^no provider of "([^"]*)" is active$`, stepNoActiveProvider)
	sc.Step(`^the component is loaded$`, stepComponentLoaded)
	sc.Step(`^a provider of "([^"]*)" activates$`, stepProviderActivates)
	sc.Step(`^the component activates$`, stepComponentActivates)
	sc.Step(`^the provider of "([^"]*)" starts unloading$`, stepProviderStartsUnloading)
	sc.Step(`^the component deactivates before the provider runs its inverses$`, stepDeactivatesBeforeProvider)
	sc.Step(`^the component reads "([^"]*)" during its own teardown$`, stepReadsDuringTeardown)
	sc.Step(`^the provider runs its inverses only after the component is inactive$`, stepProviderInversesAfterInactive)
	sc.Step(`^its provider overwrites the bound value in place$`, stepOverwriteInPlace)
	sc.Step(`^the component stays active$`, stepStaysActive)
	sc.Step(`^a different provider provides "([^"]*)"$`, stepDifferentProvider)
	sc.Step(`^the component deactivates$`, stepComponentDeactivates)
	sc.Step(`^the component activates against the new provider$`, stepActivatesAgainstNewProvider)
	sc.Step(`^the component makes no transition$`, stepNoTransition)
	sc.Step(`^a component that provides "([^"]*)" and declares "([^"]*)"$`, stepProvidesDeclares)
	sc.Step(`^the second component is inserted$`, stepSecondComponentInserted)
	sc.Step(`^the insertion fails with a dependency cycle error$`, stepCycleError)
	sc.Step(`^the registry is unchanged$`, stepRegistryUnchanged)
}

// dependentComponent builds a component that injects key, records its
// transitions, and reads the key through its committed view during teardown.
func (w *world) dependentComponent(key spc.Key[string]) rt.Component {
	return &testComponent{
		decls: rt.Declarations{Inject: []spc.AnyKey{key}},
		activate: func(inst *rt.Instance, _ any) error {
			w.mu.Lock()
			w.inst = inst
			w.mu.Unlock()
			w.bumpTransitions()
			w.appendEvent("dependent-activated")
			return inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					v, ok := rt.Get(inst, key)
					w.appendEvent(fmt.Sprintf("dependent-undo read=%v ok=%v", v, ok))
					w.appendEvent("dependent-unloaded")
					w.bumpTransitions()
					return nil
				}, nil
			})
		},
	}
}

// providerComponent builds a component that provides key and records the
// dependent's state when its inverses run.
func (w *world) providerComponent(key spc.Key[string], value string) rt.Component {
	return &testComponent{
		decls: rt.Declarations{Provide: []spc.AnyKey{key}},
		activate: func(inst *rt.Instance, _ any) error {
			w.mu.Lock()
			w.providerInst = inst
			w.mu.Unlock()
			if err := rt.Bind(inst, key, value); err != nil {
				return err
			}
			w.recordSet(key.Name(), value)
			return inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					state := "gone"
					if info, ok := w.sched.Inspect(w.dependentFiber()); ok {
						state = info.State.String()
					}
					w.appendEvent("provider-undo dependent-state=" + state)
					w.recordDel(key.Name())
					return nil
				}, nil
			})
		},
	}
}

func (w *world) dependentFiber() spc.FiberID {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dependent
}

func (w *world) startProvider(key spc.Key[string], value string) error {
	if err := w.insert("", w.providerComponent(key, value)); err != nil {
		return err
	}
	w.mu.Lock()
	w.provider = w.fiber
	w.mu.Unlock()
	return w.waitState(w.provider, rt.StateActive)
}

func (w *world) rememberKey(name string) {
	w.mu.Lock()
	w.lastKey = name
	w.mu.Unlock()
}

func stepDeclaresKey(ctx context.Context, name string) error {
	w := worldFrom(ctx)
	w.rememberKey(name)
	w.pending = w.dependentComponent(w.key(name))
	return nil
}

func stepDeclaresLoadedInactive(ctx context.Context, name string) error {
	w := worldFrom(ctx)
	w.rememberKey(name)
	w.pending = w.dependentComponent(w.key(name))
	if err := w.insert("", w.pending); err != nil {
		return err
	}
	w.dependent = w.fiber
	return w.waitState(w.dependent, rt.StateInactive)
}

func stepDeclaresActive(ctx context.Context, name string) error {
	w := worldFrom(ctx)
	w.rememberKey(name)
	key := w.key(name)

	// An active component implies its declared key already has a provider.
	if err := w.startProvider(key, name); err != nil {
		return err
	}

	w.pending = w.dependentComponent(key)
	if err := w.insert("", w.pending); err != nil {
		return err
	}
	w.dependent = w.fiber
	return w.waitState(w.dependent, rt.StateActive)
}

func stepNoActiveProvider(ctx context.Context, name string) error {
	w := worldFrom(ctx)
	key := w.key(name).ID()
	for _, f := range w.sched.Snapshot().Fibers {
		if f.State != rt.StateActive {
			continue
		}
		for _, provided := range f.Provided {
			if provided == key {
				return fmt.Errorf("fiber %d actively provides %q", f.ID, name)
			}
		}
	}
	return nil
}

func stepComponentLoaded(ctx context.Context) error {
	w := worldFrom(ctx)
	if w.pending == nil {
		return errors.New("no component has been declared")
	}
	if err := w.insert("", w.pending); err != nil {
		return err
	}
	w.dependent = w.fiber
	return w.waitSettled(w.dependent)
}

func stepProviderActivates(ctx context.Context, name string) error {
	w := worldFrom(ctx)
	w.mu.Lock()
	w.transitionsBefore = w.transitions
	w.mu.Unlock()
	return w.startProvider(w.key(name), name)
}

func stepComponentActivates(ctx context.Context) error {
	w := worldFrom(ctx)
	return w.waitState(w.dependent, rt.StateActive)
}

func stepProviderStartsUnloading(ctx context.Context, name string) error {
	w := worldFrom(ctx)
	return w.sched.Retire(w.provider)
}

func stepDeactivatesBeforeProvider(ctx context.Context) error {
	w := worldFrom(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		dependent := w.eventIndex("dependent-unloaded")
		provider := w.eventIndex("provider-undo")
		if dependent >= 0 && provider >= 0 {
			if dependent < provider {
				return nil
			}
			return fmt.Errorf("provider inverses ran before the dependent deactivated")
		}
		time.Sleep(time.Millisecond)
	}
	return fmt.Errorf("teardown events never completed: %v", w.eventsSnapshot())
}

func stepReadsDuringTeardown(ctx context.Context, name string) error {
	w := worldFrom(ctx)
	prefix := fmt.Sprintf("dependent-undo read=%s ok=true", name)
	return w.waitEventPrefix(prefix)
}

func stepProviderInversesAfterInactive(ctx context.Context) error {
	w := worldFrom(ctx)
	return w.waitEventPrefix("provider-undo dependent-state=inactive")
}

func stepOverwriteInPlace(ctx context.Context) error {
	w := worldFrom(ctx)
	w.mu.Lock()
	inst := w.providerInst
	key := w.key(w.lastKey)
	w.mu.Unlock()
	if inst == nil {
		return errors.New("no provider instance is available")
	}
	return rt.Bind(inst, key, "v2")
}

func stepStaysActive(ctx context.Context) error {
	w := worldFrom(ctx)
	time.Sleep(20 * time.Millisecond)
	state, ok := w.stateOf(w.dependent)
	if !ok || state != rt.StateActive {
		return fmt.Errorf("component state = %v (ok=%v), want active", state, ok)
	}
	return nil
}

func stepDifferentProvider(ctx context.Context, name string) error {
	w := worldFrom(ctx)
	w.mu.Lock()
	old := w.provider
	w.mu.Unlock()
	if err := w.sched.Remove(old); err != nil {
		return err
	}
	if err := w.waitGone(old); err != nil {
		return err
	}
	return w.startProvider(w.key(name), "second")
}

func stepComponentDeactivates(ctx context.Context) error {
	w := worldFrom(ctx)
	return w.waitEventPrefix("dependent-unloaded")
}

func stepActivatesAgainstNewProvider(ctx context.Context) error {
	w := worldFrom(ctx)
	if err := w.waitState(w.dependent, rt.StateActive); err != nil {
		return err
	}
	w.mu.Lock()
	key, provider := w.key(w.lastKey), w.provider
	w.mu.Unlock()
	info, ok := w.sched.Inspect(w.dependent)
	if !ok {
		return errors.New("the component has no fiber")
	}
	if got := info.CommittedView[key.ID()]; got != provider {
		return fmt.Errorf("component committed to %v, want the new provider %v", got, provider)
	}
	return nil
}

func stepNoTransition(ctx context.Context) error {
	w := worldFrom(ctx)
	time.Sleep(20 * time.Millisecond)
	w.mu.Lock()
	before := w.transitionsBefore
	w.mu.Unlock()
	if got := w.transitionCount(); got != before {
		return fmt.Errorf("component transitioned %d times, want it undisturbed", got-before)
	}
	return nil
}

func stepProvidesDeclares(ctx context.Context, provide, declare string) error {
	w := worldFrom(ctx)
	provided, injected := w.key(provide), w.key(declare)
	comp := &testComponent{
		decls: rt.Declarations{Provide: []spc.AnyKey{provided}, Inject: []spc.AnyKey{injected}},
		activate: func(inst *rt.Instance, _ any) error {
			if err := rt.Bind(inst, provided, provide); err != nil {
				return err
			}
			w.recordSet(provide, provide)
			return nil
		},
	}

	w.mu.Lock()
	first := w.cycleFirst == 0
	w.mu.Unlock()
	if first {
		if err := w.insert("", comp); err != nil {
			return err
		}
		w.mu.Lock()
		w.cycleFirst = w.fiber
		w.mu.Unlock()
		return w.waitSettled(w.fiber)
	}
	w.pending = comp
	return nil
}

func stepSecondComponentInserted(ctx context.Context) error {
	w := worldFrom(ctx)
	if w.pending == nil {
		return errors.New("no second component has been declared")
	}
	w.fiberCountBefore = len(w.sched.Snapshot().Fibers)
	_, w.lastErr = w.sched.Insert(w.pending, nil)
	return nil
}

func stepCycleError(ctx context.Context) error {
	w := worldFrom(ctx)
	if w.lastErr == nil {
		return errors.New("the cycle-closing insertion succeeded")
	}
	if !strings.Contains(w.lastErr.Error(), "dependency cycle") {
		return fmt.Errorf("insertion error = %v, want a dependency cycle error", w.lastErr)
	}
	return nil
}

func stepRegistryUnchanged(ctx context.Context) error {
	w := worldFrom(ctx)
	if got := len(w.sched.Snapshot().Fibers); got != w.fiberCountBefore {
		return fmt.Errorf("registry holds %d fibers, want the unchanged %d", got, w.fiberCountBefore)
	}
	return nil
}
