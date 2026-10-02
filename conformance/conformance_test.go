package conformance

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cucumber/godog"

	spc "github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/loader"
	rt "github.com/DaviMGDev/memento/runtime"
)

// TestFeatures runs the behavioral contract in specs/features against the
// implementation. Every scenario is a Gherkin scenario; the step
// definitions bind them to the runtime and loader APIs.
func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Name:                "conformance",
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../specs/features/effects.feature"},
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("conformance scenarios failed")
	}
}

// InitializeScenario wires a fresh world into every scenario.
func InitializeScenario(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, worldKey{}, newWorld()), nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		worldFrom(ctx).close()
		return ctx, nil
	})
	registerEffectSteps(sc)
}

// handle is the value type of the key whose comparator the effects feature
// exercises.
type handle struct {
	ID   int
	Name string
}

// testComponent is a component built by a step definition.
type testComponent struct {
	decls    rt.Declarations
	activate func(*rt.Instance, any) error
}

func (c *testComponent) Declarations() rt.Declarations { return c.decls }

func (c *testComponent) Activate(inst *rt.Instance, payload any) error {
	if c.activate == nil {
		return nil
	}
	return c.activate(inst, payload)
}

type worldKey struct{}

// world is the per-scenario state: one runtime, one loader, the named keys
// and components a scenario mentions, and the observations its steps assert
// on.
type world struct {
	mu sync.Mutex

	sched *rt.Scheduler
	reg   *loader.Registry
	ld    *loader.Loader

	stringKeys map[string]spc.Key[string]
	handleKeys map[string]spc.Key[handle]

	fiber   spc.FiberID
	inst    *rt.Instance
	fibers  map[string]spc.FiberID
	lastKey string

	before  map[string]string
	record  map[string]string
	order   []string
	events  []string
	reverts map[string]int
	emitted map[string][]string

	original handle
	current  handle
}

func newWorld() *world {
	w := &world{
		sched:      rt.New(),
		reg:        loader.NewRegistry(),
		stringKeys: make(map[string]spc.Key[string]),
		handleKeys: make(map[string]spc.Key[handle]),
		fibers:     make(map[string]spc.FiberID),
		record:     make(map[string]string),
		reverts:    make(map[string]int),
		emitted:    make(map[string][]string),
	}
	w.ld = loader.New(w.sched, w.reg)
	return w
}

func (w *world) close() { _ = w.sched.Close() }

func worldFrom(ctx context.Context) *world {
	w, _ := ctx.Value(worldKey{}).(*world)
	return w
}

func (w *world) key(name string) spc.Key[string] {
	if k, ok := w.stringKeys[name]; ok {
		return k
	}
	k := spc.NewKey[string](name)
	w.stringKeys[name] = k
	return k
}

func (w *world) instance() *rt.Instance {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.inst
}

func (w *world) insert(name string, comp rt.Component) error {
	id, err := w.sched.Insert(comp, nil)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.fiber = id
	if name != "" {
		w.fibers[name] = id
	}
	w.mu.Unlock()
	return nil
}

func (w *world) waitState(id spc.FiberID, want rt.State) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := w.sched.Inspect(id); ok && info.State == want {
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	info, ok := w.sched.Inspect(id)
	return fmt.Errorf("fiber %d never reached %s (last %+v, ok=%v)", id, want, info, ok)
}

func (w *world) waitSettled(id spc.FiberID) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := w.sched.Inspect(id); ok && info.State != rt.StateLoading && info.State != rt.StateUnloading {
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	info, ok := w.sched.Inspect(id)
	return fmt.Errorf("fiber %d never settled (last %+v, ok=%v)", id, info, ok)
}

func (w *world) waitGone(id spc.FiberID) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := w.sched.Inspect(id); !ok {
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	return fmt.Errorf("fiber %d is still registered", id)
}

func (w *world) stateOf(id spc.FiberID) (rt.State, bool) {
	info, ok := w.sched.Inspect(id)
	return info.State, ok
}

func (w *world) recordSet(name, value string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.record[name] = value
}

func (w *world) recordDel(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.record, name)
}

func (w *world) recordHas(name string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.record[name]
	return ok
}

func (w *world) recordSnapshot() map[string]string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]string, len(w.record))
	for k, v := range w.record {
		out[k] = v
	}
	return out
}

func (w *world) appendOrder(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.order = append(w.order, name)
}

func (w *world) orderSnapshot() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.order...)
}

func (w *world) appendEvent(event string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, event)
}

func (w *world) hasEvent(event string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, e := range w.events {
		if e == event {
			return true
		}
	}
	return false
}

func (w *world) incReverts(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.reverts[name]++
}

func (w *world) revertCount(name string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reverts[name]
}

func (w *world) emit(peer, message string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.emitted[peer] = append(w.emitted[peer], message)
}

func (w *world) emissions(peer string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.emitted[peer]...)
}

func equalState(a, b map[string]string) bool { return reflect.DeepEqual(a, b) }
