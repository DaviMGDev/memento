package conformance

import (
	"context"
	"errors"
	"fmt"

	"github.com/cucumber/godog"

	spc "github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/loader"
	rt "github.com/DaviMGDev/memento/runtime"
)

type fiberSnapshot struct {
	id    spc.FiberID
	state rt.State
}

func registerConfigurationSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a configuration with entries "([^"]*)" and "([^"]*)"$`, stepConfigWithEntries)
	sc.Step(`^the configuration is applied$`, stepConfigurationApplied)
	sc.Step(`^both entries have active fibers$`, stepBothEntriesActive)
	sc.Step(`^a configuration with an active entry "([^"]*)"$`, stepConfigWithActiveEntry)
	sc.Step(`^a configuration with active entries "([^"]*)" and "([^"]*)"$`, stepConfigWithActiveEntries)
	sc.Step(`^the entry is disabled$`, stepEntryDisabled)
	sc.Step(`^the entry's fiber is unloaded$`, stepEntryFiberUnloaded)
	sc.Step(`^the rest of the configuration is untouched$`, stepRestUntouched)
	sc.Step(`^a configuration is applied$`, stepDefaultConfigApplied)
	sc.Step(`^the same configuration is applied again$`, stepSameConfigAgain)
	sc.Step(`^no fiber transitions$`, stepNoFiberTransitions)
	sc.Step(`^the payload of "([^"]*)" changes$`, stepPayloadChanges)
	sc.Step(`^only "([^"]*)" reloads$`, stepOnlyReloads)
	sc.Step(`^the entry references a different component$`, stepEntryReferencesDifferent)
	sc.Step(`^the old fiber is unloaded$`, stepOldFiberUnloaded)
	sc.Step(`^a fiber of the new component is instantiated$`, stepNewComponentFiber)
	sc.Step(`^the entry is removed$`, stepEntryRemoved)
	sc.Step(`^its fiber is unloaded$`, stepItsFiberUnloaded)
	sc.Step(`^then revised several times$`, stepRevisedSeveralTimes)
	sc.Step(`^reconciliation reaches quiescence$`, stepReconciliationQuiescent)
	sc.Step(`^the resulting state is equivalent to a from-scratch load of the final configuration$`, stepEquivalentFromScratch)
	sc.Step(`^an entry whose component fails to activate during reconciliation$`, stepEntryFailsDuringReconciliation)
	sc.Step(`^reconciliation runs$`, stepReconciliationRuns)
	sc.Step(`^the failing entry is "([^"]*)"$`, stepFailingEntryIs)
	sc.Step(`^every other entry reaches its target state$`, stepEveryOtherEntryReachesTarget)
}

// ensureConfigFactories registers the factories the configuration feature
// names. Each component provides the key of its entry and binds its payload.
func (w *world) ensureConfigFactories() error {
	refs := map[string]string{
		"database": "database",
		"cache":    "database",
		"console":  "console",
	}
	for ref, keyName := range refs {
		if w.reg.Has(ref) {
			continue
		}
		ref, keyName := ref, keyName
		err := w.reg.Register(ref, func(payload any) (rt.Component, error) {
			w.mu.Lock()
			w.instantiated[ref]++
			w.mu.Unlock()

			key := w.key(keyName)
			return &testComponent{
				decls: rt.Declarations{Provide: []spc.AnyKey{key}},
				activate: func(inst *rt.Instance, payload any) error {
					value := fmt.Sprint(payload)
					if err := rt.Bind(inst, key, value); err != nil {
						return err
					}
					w.recordSet(keyName, value)
					w.mu.Lock()
					w.activations[keyName]++
					w.mu.Unlock()
					return inst.Context().RegisterEffect(func() (func() error, error) {
						return func() error {
							w.recordDel(keyName)
							return nil
						}, nil
					})
				},
			}, nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *world) setEntry(id, ref, payload string, enabled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries[id] = loader.Entry{ID: id, Component: ref, Payload: payload, Enabled: enabled}
}

func (w *world) applyConfig() error {
	w.mu.Lock()
	entries := make([]loader.Entry, 0, len(w.entries))
	for _, e := range w.entries {
		entries = append(entries, e)
	}
	w.mu.Unlock()

	tree, err := loader.NewTree(entries...)
	if err != nil {
		return err
	}
	return w.ld.Reconcile(tree)
}

func (w *world) waitEntryActive(id string) error {
	fiber, ok := w.ld.Fiber(id)
	if !ok {
		return fmt.Errorf("entry %q has no fiber", id)
	}
	w.mu.Lock()
	w.fibers[id] = fiber
	w.mu.Unlock()
	return w.waitState(fiber, rt.StateActive)
}

func (w *world) fiberID(id string) (spc.FiberID, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	fiber, ok := w.fibers[id]
	return fiber, ok
}

func (w *world) activationCount(name string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.activations[name]
}

func (w *world) instantiationCount(ref string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.instantiated[ref]
}

func (w *world) snapshotConfig() map[string]fiberSnapshot {
	w.mu.Lock()
	ids := make([]string, 0, len(w.entries))
	for id := range w.entries {
		ids = append(ids, id)
	}
	w.mu.Unlock()

	out := make(map[string]fiberSnapshot, len(ids))
	for _, id := range ids {
		fiber, ok := w.ld.Fiber(id)
		if !ok {
			continue
		}
		info, ok := w.sched.Inspect(fiber)
		if !ok {
			continue
		}
		out[id] = fiberSnapshot{id: fiber, state: info.State}
	}
	return out
}

func stepConfigWithEntries(ctx context.Context, first, second string) error {
	w := worldFrom(ctx)
	if err := w.ensureConfigFactories(); err != nil {
		return err
	}
	w.setEntry(first, first, "v1", true)
	w.setEntry(second, second, "c1", true)
	return nil
}

func stepConfigurationApplied(ctx context.Context) error {
	w := worldFrom(ctx)
	return w.applyConfig()
}

func stepBothEntriesActive(ctx context.Context) error {
	w := worldFrom(ctx)
	for _, id := range []string{"database", "console"} {
		if err := w.waitEntryActive(id); err != nil {
			return err
		}
	}
	return nil
}

func stepConfigWithActiveEntry(ctx context.Context, id string) error {
	w := worldFrom(ctx)
	if err := w.ensureConfigFactories(); err != nil {
		return err
	}
	w.setEntry("database", "database", "v1", true)
	w.setEntry("console", "console", "c1", true)
	if err := w.applyConfig(); err != nil {
		return err
	}
	if err := w.waitEntryActive("database"); err != nil {
		return err
	}
	if err := w.waitEntryActive("console"); err != nil {
		return err
	}
	if _, ok := w.ld.Fiber(id); !ok {
		return fmt.Errorf("entry %q is not active", id)
	}
	return nil
}

func stepConfigWithActiveEntries(ctx context.Context, first, second string) error {
	return stepConfigWithActiveEntry(ctx, first)
}

func stepEntryDisabled(ctx context.Context) error {
	w := worldFrom(ctx)
	w.setEntry("database", "database", "v1", false)
	return w.applyConfig()
}

func stepEntryFiberUnloaded(ctx context.Context) error {
	w := worldFrom(ctx)
	if _, ok := w.ld.Fiber("database"); ok {
		return errors.New("the disabled entry still has a fiber")
	}
	old, _ := w.fiberID("database")
	if _, alive := w.sched.Inspect(old); alive {
		return errors.New("the disabled entry's fiber survived")
	}
	return nil
}

func stepRestUntouched(ctx context.Context) error {
	w := worldFrom(ctx)
	fiber, ok := w.ld.Fiber("console")
	stored, had := w.fiberID("console")
	if !ok || !had || fiber != stored {
		return errors.New("disabling one entry disturbed its sibling's fiber")
	}
	state, _ := w.stateOf(fiber)
	if state != rt.StateActive {
		return fmt.Errorf("sibling state = %v, want active", state)
	}
	return nil
}

func stepDefaultConfigApplied(ctx context.Context) error {
	w := worldFrom(ctx)
	if err := w.ensureConfigFactories(); err != nil {
		return err
	}
	w.setEntry("database", "database", "v1", true)
	w.setEntry("console", "console", "c1", true)
	if err := w.applyConfig(); err != nil {
		return err
	}
	if err := w.waitEntryActive("database"); err != nil {
		return err
	}
	if err := w.waitEntryActive("console"); err != nil {
		return err
	}
	w.configBefore = w.snapshotConfig()
	return nil
}

func stepSameConfigAgain(ctx context.Context) error {
	w := worldFrom(ctx)
	return w.applyConfig()
}

func stepNoFiberTransitions(ctx context.Context) error {
	w := worldFrom(ctx)
	after := w.snapshotConfig()
	if len(after) != len(w.configBefore) {
		return fmt.Errorf("fiber count changed from %d to %d", len(w.configBefore), len(after))
	}
	for id, before := range w.configBefore {
		got, ok := after[id]
		if !ok || got.id != before.id || got.state != before.state {
			return fmt.Errorf("entry %q transitioned: %+v -> %+v", id, before, got)
		}
	}
	return nil
}

func stepPayloadChanges(ctx context.Context, id string) error {
	w := worldFrom(ctx)
	w.setEntry(id, id, "v2", true)
	return w.applyConfig()
}

func stepOnlyReloads(ctx context.Context, id string) error {
	w := worldFrom(ctx)
	fiber, ok := w.ld.Fiber(id)
	stored, had := w.fiberID(id)
	if !ok || !had || fiber != stored {
		return fmt.Errorf("entry %q was rebuilt instead of reloaded", id)
	}
	if got := w.activationCount(id); got != 2 {
		return fmt.Errorf("entry %q activations = %d, want 2 (initial + reload)", id, got)
	}

	sibling := "console"
	if id == "console" {
		sibling = "database"
	}
	siblingFiber, _ := w.ld.Fiber(sibling)
	siblingStored, _ := w.fiberID(sibling)
	if siblingFiber != siblingStored {
		return fmt.Errorf("reloading %q rebuilt its sibling %q", id, sibling)
	}
	if got := w.activationCount(sibling); got != 1 {
		return fmt.Errorf("sibling %q activations = %d, want 1 (untouched)", sibling, got)
	}
	return nil
}

func stepEntryReferencesDifferent(ctx context.Context) error {
	w := worldFrom(ctx)
	w.setEntry("database", "cache", "v1", true)
	return w.applyConfig()
}

func stepOldFiberUnloaded(ctx context.Context) error {
	w := worldFrom(ctx)
	old, ok := w.fiberID("database")
	if !ok {
		return errors.New("no previous fiber was recorded")
	}
	if _, alive := w.sched.Inspect(old); alive {
		return errors.New("the old fiber survived the component change")
	}
	return nil
}

func stepNewComponentFiber(ctx context.Context) error {
	w := worldFrom(ctx)
	fiber, ok := w.ld.Fiber("database")
	if !ok {
		return errors.New("the rebuilt entry has no fiber")
	}
	if err := w.waitState(fiber, rt.StateActive); err != nil {
		return err
	}
	if w.instantiationCount("cache") == 0 {
		return errors.New("the new component was never instantiated")
	}
	return nil
}

func stepEntryRemoved(ctx context.Context) error {
	w := worldFrom(ctx)
	w.mu.Lock()
	delete(w.entries, "console")
	w.mu.Unlock()
	return w.applyConfig()
}

func stepItsFiberUnloaded(ctx context.Context) error {
	w := worldFrom(ctx)
	old, _ := w.fiberID("console")
	if _, ok := w.ld.Fiber("console"); ok {
		return errors.New("the removed entry still has a fiber")
	}
	if _, alive := w.sched.Inspect(old); alive {
		return errors.New("the removed entry's fiber survived")
	}
	return nil
}

func stepRevisedSeveralTimes(ctx context.Context) error {
	w := worldFrom(ctx)
	// Revision 1: payload change.
	w.setEntry("database", "database", "v2", true)
	if err := w.applyConfig(); err != nil {
		return err
	}
	// Revision 2: disable the sibling.
	w.setEntry("console", "console", "c1", false)
	if err := w.applyConfig(); err != nil {
		return err
	}
	// Revision 3: re-enable the sibling with a new payload and swap the
	// database entry's component.
	w.setEntry("console", "console", "c2", true)
	w.setEntry("database", "cache", "v2", true)
	if err := w.applyConfig(); err != nil {
		return err
	}
	// Revision 4: remove the sibling and change the payload again.
	w.mu.Lock()
	delete(w.entries, "console")
	w.mu.Unlock()
	w.setEntry("database", "cache", "v3", true)
	return w.applyConfig()
}

func stepReconciliationQuiescent(ctx context.Context) error {
	w := worldFrom(ctx)
	for _, f := range w.sched.Snapshot().Fibers {
		if f.State == rt.StateLoading || f.State == rt.StateUnloading {
			return fmt.Errorf("fiber %d is still %s", f.ID, f.State)
		}
	}
	return nil
}

func stepEquivalentFromScratch(ctx context.Context) error {
	w := worldFrom(ctx)

	fresh := newWorld()
	defer fresh.close()
	if err := fresh.ensureConfigFactories(); err != nil {
		return err
	}
	w.mu.Lock()
	for id, entry := range w.entries {
		fresh.entries[id] = entry
	}
	w.mu.Unlock()
	if err := fresh.applyConfig(); err != nil {
		return err
	}

	got, want := w.recordSnapshot(), fresh.recordSnapshot()
	if !equalState(got, want) {
		return fmt.Errorf("incremental bindings = %v, from-scratch bindings = %v", got, want)
	}

	incremental := w.snapshotConfig()
	fromScratch := fresh.snapshotConfig()
	if len(incremental) != len(fromScratch) {
		return fmt.Errorf("incremental has %d entries, from-scratch has %d", len(incremental), len(fromScratch))
	}
	for id, snap := range incremental {
		other, ok := fromScratch[id]
		if !ok || other.state != snap.state {
			return fmt.Errorf("entry %q: incremental %v, from-scratch %v", id, snap.state, other.state)
		}
	}
	return nil
}

func stepEntryFailsDuringReconciliation(ctx context.Context) error {
	w := worldFrom(ctx)
	if err := w.ensureConfigFactories(); err != nil {
		return err
	}
	key := w.key("database")
	if err := w.reg.Register("broken", func(payload any) (rt.Component, error) {
		return &testComponent{
			decls: rt.Declarations{Provide: []spc.AnyKey{key}},
			activate: func(*rt.Instance, any) error {
				return errors.New("boom")
			},
		}, nil
	}); err != nil {
		return err
	}
	w.setEntry("database", "broken", "v1", true)
	w.setEntry("console", "console", "c1", true)
	return nil
}

func stepReconciliationRuns(ctx context.Context) error {
	w := worldFrom(ctx)
	return w.applyConfig()
}

func stepFailingEntryIs(ctx context.Context, want string) error {
	w := worldFrom(ctx)
	fiber, ok := w.ld.Fiber("database")
	if !ok {
		return errors.New("the failing entry has no fiber")
	}
	info, ok := w.sched.Inspect(fiber)
	if !ok || info.State.String() != want {
		return fmt.Errorf("failing entry state = %v (ok=%v), want %q", info.State, ok, want)
	}
	return nil
}

func stepEveryOtherEntryReachesTarget(ctx context.Context) error {
	w := worldFrom(ctx)
	fiber, ok := w.ld.Fiber("console")
	if !ok {
		return errors.New("the healthy entry has no fiber")
	}
	return w.waitState(fiber, rt.StateActive)
}
