package conformance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cucumber/godog"

	spc "github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/loader"
	rt "github.com/DaviMGDev/memento/runtime"
)

func registerLifecycleSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a component whose declared keys are satisfied$`, stepSatisfiedComponent)
	sc.Step(`^the component passes through "([^"]*)"$`, stepPassesThrough)
	sc.Step(`^the component settles at "([^"]*)"$`, stepSettlesAt)
	sc.Step(`^an active component$`, stepActiveComponent)
	sc.Step(`^the component is retired$`, stepComponentRetired)
	sc.Step(`^its dependency is withdrawn while one of its effects is still installing$`, stepDependencyWithdrawn)
	sc.Step(`^its dependency is withdrawn before activation finishes$`, stepDependencyWithdrawn)
	sc.Step(`^the current activation completes$`, stepActivationCompletes)
	sc.Step(`^the activation runs to completion$`, stepActivationCompletes)
	sc.Step(`^the component then unloads$`, stepComponentThenUnloads)
	sc.Step(`^the component deactivates without a further trigger$`, stepDeactivatesWithoutTrigger)
	sc.Step(`^a component is "([^"]*)"$`, stepComponentIs)
	sc.Step(`^its dependency is provided again before unload finishes$`, stepDependencyProvidedAgain)
	sc.Step(`^the unload runs to completion$`, stepUnloadRunsToCompletion)
	sc.Step(`^the component activates again$`, stepComponentActivatesAgain)
	sc.Step(`^a component whose activation fails$`, stepActivationFails)
	sc.Step(`^the same component is reloaded against the same environment$`, stepReloadSameEnvironment)
	sc.Step(`^the component state remains "([^"]*)"$`, stepStateRemains)
	sc.Step(`^a failed component is disabled$`, stepFailedComponentDisabled)
	sc.Step(`^the entry is enabled again$`, stepEntryEnabledAgain)
	sc.Step(`^a new fiber is instantiated$`, stepNewFiberInstantiated)
	sc.Step(`^the failure is not carried over$`, stepFailureNotCarriedOver)
	sc.Step(`^two active components that declare the same key$`, stepTwoActiveComponents)
	sc.Step(`^one is unloaded$`, stepOneIsUnloaded)
	sc.Step(`^the other stays "([^"]*)"$`, stepOtherStays)
	sc.Step(`^the shared provider is unaffected$`, stepSharedProviderUnaffected)
}

func parseState(name string) (rt.State, error) {
	switch name {
	case "inactive":
		return rt.StateInactive, nil
	case "loading":
		return rt.StateLoading, nil
	case "active":
		return rt.StateActive, nil
	case "unloading":
		return rt.StateUnloading, nil
	case "failed":
		return rt.StateFailed, nil
	}
	return 0, fmt.Errorf("unknown state %q", name)
}

// lifecycleComponent installs one effect, parks mid-activation, and parks
// again inside a second effect's inverse, so the scenario can observe the
// LOADING and UNLOADING states and release them deliberately.
func (w *world) lifecycleComponent(key *spc.Key[string]) rt.Component {
	decls := rt.Declarations{}
	if key != nil {
		decls.Inject = []spc.AnyKey{*key}
	}
	return &testComponent{
		decls: decls,
		activate: func(inst *rt.Instance, _ any) error {
			w.mu.Lock()
			w.inst = inst
			w.mu.Unlock()
			if err := inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error { return nil }, nil
			}); err != nil {
				return err
			}
			w.markActivationStarted()
			<-w.activationRelease
			w.markActivationDone()
			return inst.Context().RegisterEffect(func() (func() error, error) {
				return func() error {
					<-w.unloadRelease
					w.markUnloadDone()
					return nil
				}, nil
			})
		},
	}
}

func (w *world) setupProviderAndDependent() error {
	key := w.key("storage")
	if err := w.startProvider(key, "storage"); err != nil {
		return err
	}
	w.pending = w.lifecycleComponent(&key)
	if err := w.insert("", w.pending); err != nil {
		return err
	}
	w.dependent = w.fiber
	return nil
}

func stepSatisfiedComponent(ctx context.Context) error {
	w := worldFrom(ctx)
	w.pending = w.lifecycleComponent(nil)
	return nil
}

func stepPassesThrough(ctx context.Context, want string) error {
	w := worldFrom(ctx)
	switch want {
	case "loading":
		if err := w.waitActivationStarted(); err != nil {
			return err
		}
		return w.waitState(w.dependent, rt.StateLoading)
	case "unloading":
		// The in-flight activation must complete before the unload starts.
		w.releaseActivation()
		return w.waitState(w.dependent, rt.StateUnloading)
	}
	return fmt.Errorf("unsupported transient state %q", want)
}

func stepSettlesAt(ctx context.Context, want string) error {
	w := worldFrom(ctx)
	state, err := parseState(want)
	if err != nil {
		return err
	}
	w.releaseActivation()
	w.releaseUnload()
	return w.waitState(w.dependent, state)
}

func stepActiveComponent(ctx context.Context) error {
	w := worldFrom(ctx)
	return w.setupProviderAndDependent()
}

func stepComponentRetired(ctx context.Context) error {
	w := worldFrom(ctx)
	return w.sched.Retire(w.dependent)
}

func stepDependencyWithdrawn(ctx context.Context) error {
	w := worldFrom(ctx)
	// The dependent is parked mid-activation; withdrawing the provider
	// changes its target while one of its effects is already installed.
	return w.sched.Retire(w.provider)
}

func stepActivationCompletes(ctx context.Context) error {
	w := worldFrom(ctx)
	w.releaseActivation()
	return w.waitActivationDone()
}

func stepComponentThenUnloads(ctx context.Context) error {
	w := worldFrom(ctx)
	return w.waitState(w.dependent, rt.StateUnloading)
}

func stepDeactivatesWithoutTrigger(ctx context.Context) error {
	w := worldFrom(ctx)
	w.releaseUnload()
	return w.waitState(w.dependent, rt.StateInactive)
}

func stepComponentIs(ctx context.Context, want string) error {
	w := worldFrom(ctx)
	switch want {
	case "loading":
		if err := w.setupProviderAndDependent(); err != nil {
			return err
		}
		if err := w.waitActivationStarted(); err != nil {
			return err
		}
		return w.waitState(w.dependent, rt.StateLoading)
	case "unloading":
		if err := w.setupProviderAndDependent(); err != nil {
			return err
		}
		w.releaseActivation()
		if err := w.waitActivationDone(); err != nil {
			return err
		}
		if err := w.waitState(w.dependent, rt.StateActive); err != nil {
			return err
		}
		// The dependency is withdrawn (the provider reloads), which starts
		// the dependent's own unload.
		if err := w.sched.Reload(w.provider, "second"); err != nil {
			return err
		}
		return w.waitState(w.dependent, rt.StateUnloading)
	}
	return fmt.Errorf("unsupported state %q", want)
}

func stepDependencyProvidedAgain(ctx context.Context) error {
	w := worldFrom(ctx)
	return w.sched.Reload(w.provider, "second")
}

func stepUnloadRunsToCompletion(ctx context.Context) error {
	w := worldFrom(ctx)
	w.releaseUnload()
	return w.waitUnloadDone()
}

func stepComponentActivatesAgain(ctx context.Context) error {
	w := worldFrom(ctx)
	return w.waitState(w.dependent, rt.StateActive)
}

func stepActivationFails(ctx context.Context) error {
	w := worldFrom(ctx)
	key := w.key("failure")
	comp := &testComponent{
		decls: rt.Declarations{Provide: []spc.AnyKey{key}},
		activate: func(*rt.Instance, any) error {
			return errors.New("activation failed")
		},
	}
	if err := w.insert("", comp); err != nil {
		return err
	}
	w.dependent = w.fiber
	return w.waitState(w.dependent, rt.StateFailed)
}

func stepReloadSameEnvironment(ctx context.Context) error {
	w := worldFrom(ctx)
	if err := w.sched.Reload(w.dependent, nil); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	return nil
}

func stepStateRemains(ctx context.Context, want string) error {
	w := worldFrom(ctx)
	state, ok := w.stateOf(w.dependent)
	if !ok {
		return errors.New("the component has no fiber")
	}
	if state.String() != want {
		return fmt.Errorf("component state = %q, want it to remain %q", state, want)
	}
	return nil
}

func stepFailedComponentDisabled(ctx context.Context) error {
	w := worldFrom(ctx)
	key := w.key("app")
	attempts := 0
	if err := w.reg.Register("app", func(payload any) (rt.Component, error) {
		attempts++
		fail := attempts == 1
		return &testComponent{
			decls: rt.Declarations{Provide: []spc.AnyKey{key}},
			activate: func(inst *rt.Instance, _ any) error {
				if fail {
					return errors.New("boom")
				}
				return rt.Bind(inst, key, "ok")
			},
		}, nil
	}); err != nil {
		return err
	}

	enabled, err := loader.NewTree(loader.Entry{ID: "app", Component: "app", Enabled: true})
	if err != nil {
		return err
	}
	if err := w.ld.Reconcile(enabled); err != nil {
		return err
	}
	fiber, ok := w.ld.Fiber("app")
	if !ok {
		return errors.New("the failing entry has no fiber")
	}
	w.entryFiber = fiber
	if err := w.waitState(fiber, rt.StateFailed); err != nil {
		return err
	}

	disabled, err := loader.NewTree(loader.Entry{ID: "app", Component: "app", Enabled: false})
	if err != nil {
		return err
	}
	return w.ld.Reconcile(disabled)
}

func stepEntryEnabledAgain(ctx context.Context) error {
	w := worldFrom(ctx)
	enabled, err := loader.NewTree(loader.Entry{ID: "app", Component: "app", Enabled: true})
	if err != nil {
		return err
	}
	if err := w.ld.Reconcile(enabled); err != nil {
		return err
	}
	fiber, ok := w.ld.Fiber("app")
	if !ok {
		return errors.New("the re-enabled entry has no fiber")
	}
	w.entryFiberNew = fiber
	return nil
}

func stepNewFiberInstantiated(ctx context.Context) error {
	w := worldFrom(ctx)
	if w.entryFiberNew == 0 || w.entryFiberNew == w.entryFiber {
		return fmt.Errorf("re-enabled entry reused fiber %v", w.entryFiberNew)
	}
	if _, ok := w.sched.Inspect(w.entryFiberNew); !ok {
		return errors.New("the new fiber is not registered")
	}
	return nil
}

func stepFailureNotCarriedOver(ctx context.Context) error {
	w := worldFrom(ctx)
	return w.waitState(w.entryFiberNew, rt.StateActive)
}

func stepTwoActiveComponents(ctx context.Context) error {
	w := worldFrom(ctx)
	key := w.key("storage")
	if err := w.startProvider(key, "storage"); err != nil {
		return err
	}

	first := w.dependentComponent(key)
	if err := w.insert("", first); err != nil {
		return err
	}
	w.dependent = w.fiber
	if err := w.waitState(w.dependent, rt.StateActive); err != nil {
		return err
	}

	second := w.dependentComponent(key)
	if err := w.insert("", second); err != nil {
		return err
	}
	w.secondDependent = w.fiber
	return w.waitState(w.secondDependent, rt.StateActive)
}

func stepOneIsUnloaded(ctx context.Context) error {
	w := worldFrom(ctx)
	if err := w.sched.Remove(w.secondDependent); err != nil {
		return err
	}
	return w.waitGone(w.secondDependent)
}

func stepOtherStays(ctx context.Context, want string) error {
	w := worldFrom(ctx)
	state, ok := w.stateOf(w.dependent)
	if !ok {
		return errors.New("the surviving component has no fiber")
	}
	if state.String() != want {
		return fmt.Errorf("surviving component state = %q, want %q", state, want)
	}
	return nil
}

func stepSharedProviderUnaffected(ctx context.Context) error {
	w := worldFrom(ctx)
	state, ok := w.stateOf(w.provider)
	if !ok || state != rt.StateActive {
		return fmt.Errorf("shared provider state = %v (ok=%v), want active", state, ok)
	}
	info, _ := w.sched.Inspect(w.provider)
	key := w.key("storage").ID()
	for _, provided := range info.Provided {
		if provided == key {
			return nil
		}
	}
	return errors.New("the shared provider no longer provides the key")
}
