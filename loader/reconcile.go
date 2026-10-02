package loader

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/runtime"
)

// Runtime is the control-plane surface the loader drives. The runtime
// scheduler implements it.
type Runtime interface {
	Insert(c runtime.Component, payload any) (context.FiberID, error)
	Remove(id context.FiberID) error
	Reload(id context.FiberID, payload any) error
	Snapshot() runtime.Snapshot
}

// PayloadAware is implemented by components that can decide whether a
// configuration payload change is material. A component that does not
// implement it is reloaded on every payload change.
type PayloadAware interface {
	Material(oldPayload, newPayload any) bool
}

// Loader reconciles a desired entry tree against the live fibers.
//
// Reconciliation is incremental: it diffs the desired entries against the
// applied ones and touches only the entries whose fields changed. It
// returns once the runtime is quiescent — every fiber settled and every
// removed fiber gone — so a returned reconciliation is a converged one.
type Loader struct {
	rt       Runtime
	registry *Registry

	live    map[string]*liveEntry
	pending []context.FiberID
}

type liveEntry struct {
	fiber   context.FiberID
	loaded  bool
	comp    runtime.Component
	ref     string
	payload any
	enabled bool
}

// New returns a loader driving rt with components from registry.
func New(rt Runtime, registry *Registry) *Loader {
	return &Loader{
		rt:       rt,
		registry: registry,
		live:     make(map[string]*liveEntry),
	}
}

// Fiber returns the live fiber identity of an applied, enabled entry.
func (l *Loader) Fiber(id string) (context.FiberID, bool) {
	le, ok := l.live[id]
	if !ok || !le.loaded {
		return 0, false
	}
	return le.fiber, true
}

// Reconcile makes the live composition match the desired entry tree.
//
// Per-field dispatch: a component-reference change unloads the old instance
// and instantiates the new one; a payload change is handed to the component,
// which may declare it immaterial; enabling or disabling an entry loads or
// unloads it; unrelated entries are untouched. Errors on one entry do not
// stop the others; they are joined and returned once the runtime settles.
func (l *Loader) Reconcile(t *Tree) error {
	desired := make(map[string]Entry)
	t.Walk(func(e Entry) { desired[e.ID] = e })

	var errs []error

	// Phase 1: withdraw what the new tree no longer wants, what changed its
	// component, and what was disabled. Withdrawing before inserting frees
	// the provision claims the insertions may need.
	for id, le := range l.live {
		want, keep := desired[id]
		if !keep || le.ref != want.Component {
			if err := l.unload(le); err != nil {
				errs = append(errs, fmt.Errorf("loader: entry %q: %w", id, err))
			}
			delete(l.live, id)
			continue
		}
		if !want.Enabled {
			if le.loaded {
				if err := l.unload(le); err != nil {
					errs = append(errs, fmt.Errorf("loader: entry %q: %w", id, err))
				}
			}
			le.loaded = false
			le.fiber = 0
			le.comp = nil
			le.enabled = false
			le.payload = want.Payload
		}
	}

	// Phase 2: removed fibers must be gone before their keys can be claimed
	// again by the insertions that follow.
	if err := l.waitRemoved(); err != nil {
		errs = append(errs, err)
	}

	// Phase 3: load and update.
	for id, want := range desired {
		le, known := l.live[id]
		if !known {
			if !want.Enabled {
				l.live[id] = &liveEntry{ref: want.Component, payload: want.Payload}
				continue
			}
			le = &liveEntry{}
			if err := l.load(id, want, le); err != nil {
				errs = append(errs, err)
				continue
			}
			l.live[id] = le
			continue
		}
		if !want.Enabled {
			continue
		}
		if !le.loaded {
			if err := l.load(id, want, le); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		le.enabled = true
		if reflect.DeepEqual(le.payload, want.Payload) {
			continue
		}
		if pa, ok := le.comp.(PayloadAware); ok && !pa.Material(le.payload, want.Payload) {
			le.payload = want.Payload
			continue
		}
		if err := l.rt.Reload(le.fiber, want.Payload); err != nil {
			errs = append(errs, fmt.Errorf("loader: entry %q: %w", id, err))
			continue
		}
		le.payload = want.Payload
	}

	if err := l.waitQuiescent(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (l *Loader) load(id string, want Entry, le *liveEntry) error {
	factory, err := l.registry.Resolve(want.Component)
	if err != nil {
		return fmt.Errorf("loader: entry %q: %w", id, err)
	}
	comp, err := factory(want.Payload)
	if err != nil {
		return fmt.Errorf("loader: entry %q: %w", id, err)
	}
	fiber, err := l.rt.Insert(comp, want.Payload)
	if err != nil {
		return fmt.Errorf("loader: entry %q: %w", id, err)
	}
	le.fiber = fiber
	le.loaded = true
	le.comp = comp
	le.ref = want.Component
	le.payload = want.Payload
	le.enabled = true
	return nil
}

func (l *Loader) unload(le *liveEntry) error {
	if !le.loaded || le.fiber == 0 {
		return nil
	}
	if err := l.rt.Remove(le.fiber); err != nil {
		return err
	}
	l.pending = append(l.pending, le.fiber)
	return nil
}

func (l *Loader) waitRemoved() error {
	if len(l.pending) == 0 {
		return nil
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		alive := make(map[context.FiberID]bool)
		for _, f := range l.rt.Snapshot().Fibers {
			alive[f.ID] = true
		}
		done := true
		for _, id := range l.pending {
			if alive[id] {
				done = false
				break
			}
		}
		if done {
			l.pending = nil
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("loader: timed out waiting for removed fibers")
		}
		time.Sleep(time.Millisecond)
	}
}

func (l *Loader) waitQuiescent() error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if l.quiescent() {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("loader: reconciliation did not reach quiescence")
		}
		time.Sleep(time.Millisecond)
	}
}

func (l *Loader) quiescent() bool {
	snap := l.rt.Snapshot()
	alive := make(map[context.FiberID]bool, len(snap.Fibers))
	for _, f := range snap.Fibers {
		if f.State == runtime.StateLoading || f.State == runtime.StateUnloading {
			return false
		}
		alive[f.ID] = true
	}
	for _, le := range l.live {
		if le.loaded && !alive[le.fiber] {
			return false
		}
	}
	for _, id := range l.pending {
		if alive[id] {
			return false
		}
	}
	return true
}
