package loader_test

import (
	"testing"
	"time"

	"github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/loader"
	"github.com/DaviMGDev/memento/runtime"
)

type testComponent struct {
	decls    runtime.Declarations
	activate func(*runtime.Instance, any) error
}

func (c *testComponent) Declarations() runtime.Declarations { return c.decls }

func (c *testComponent) Activate(inst *runtime.Instance, payload any) error {
	if c.activate == nil {
		return nil
	}
	return c.activate(inst, payload)
}

func stringProvider(key context.Key[string], value string) loader.Factory {
	return func(payload any) (runtime.Component, error) {
		return &testComponent{
			decls: runtime.Declarations{Provide: []context.AnyKey{key}},
			activate: func(inst *runtime.Instance, payload any) error {
				return runtime.Bind(inst, key, value)
			},
		}, nil
	}
}

func mustTree(t *testing.T, entries ...loader.Entry) *loader.Tree {
	t.Helper()
	tree, err := loader.NewTree(entries...)
	if err != nil {
		t.Fatalf("NewTree() = %v", err)
	}
	return tree
}

func waitActive(t *testing.T, s *runtime.Scheduler, id context.FiberID) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := s.Inspect(id); ok && info.State == runtime.StateActive {
			return
		}
		time.Sleep(time.Millisecond)
	}
	info, ok := s.Inspect(id)
	t.Fatalf("fiber %d never became active: %+v (ok=%v)", id, info, ok)
}

func TestReconcileLoadsRebuildsAndDisablesEntries(t *testing.T) {
	sched := runtime.New()
	defer sched.Close()

	reg := loader.NewRegistry()
	dbKey := context.NewKey[string]("db")
	consoleKey := context.NewKey[string]("console")
	if err := reg.Register("database", stringProvider(dbKey, "db")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("cache", stringProvider(dbKey, "cache")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("console", stringProvider(consoleKey, "console")); err != nil {
		t.Fatal(err)
	}
	l := loader.New(sched, reg)

	// Apply a configuration with two enabled entries.
	if err := l.Reconcile(mustTree(t,
		loader.Entry{ID: "db", Component: "database", Enabled: true},
		loader.Entry{ID: "console", Component: "console", Enabled: true},
	)); err != nil {
		t.Fatalf("Reconcile() = %v", err)
	}
	db1, ok := l.Fiber("db")
	if !ok {
		t.Fatal("db has no fiber after applying its entry")
	}
	console1, ok := l.Fiber("console")
	if !ok {
		t.Fatal("console has no fiber after applying its entry")
	}
	waitActive(t, sched, db1)
	waitActive(t, sched, console1)

	// A component change rebuilds only that entry; the sibling is untouched.
	if err := l.Reconcile(mustTree(t,
		loader.Entry{ID: "db", Component: "cache", Enabled: true},
		loader.Entry{ID: "console", Component: "console", Enabled: true},
	)); err != nil {
		t.Fatalf("Reconcile(component change) = %v", err)
	}
	db2, ok := l.Fiber("db")
	if !ok || db2 == db1 {
		t.Fatalf("db fiber after rebuild = (%v, %v), want a fresh fiber", db2, ok)
	}
	if _, alive := sched.Inspect(db1); alive {
		t.Fatal("the replaced entry's old fiber survived")
	}
	waitActive(t, sched, db2)

	console2, ok := l.Fiber("console")
	if !ok || console2 != console1 {
		t.Fatalf("console fiber = (%v, %v), want the untouched %v", console2, ok, console1)
	}
	if info, _ := sched.Inspect(console1); info.State != runtime.StateActive {
		t.Fatalf("console state = %v, want active", info.State)
	}

	// Disabling an entry unloads its fiber and leaves the rest alone.
	if err := l.Reconcile(mustTree(t,
		loader.Entry{ID: "db", Component: "cache", Enabled: false},
		loader.Entry{ID: "console", Component: "console", Enabled: true},
	)); err != nil {
		t.Fatalf("Reconcile(disable) = %v", err)
	}
	if _, ok := l.Fiber("db"); ok {
		t.Fatal("disabled db still has a fiber")
	}
	if _, alive := sched.Inspect(db2); alive {
		t.Fatal("disabled db's fiber survived")
	}
	if console3, _ := l.Fiber("console"); console3 != console1 {
		t.Fatal("disabling db touched console")
	}

	// Re-enabling starts a fresh instance.
	if err := l.Reconcile(mustTree(t,
		loader.Entry{ID: "db", Component: "database", Enabled: true},
		loader.Entry{ID: "console", Component: "console", Enabled: true},
	)); err != nil {
		t.Fatalf("Reconcile(re-enable) = %v", err)
	}
	db3, ok := l.Fiber("db")
	if !ok || db3 == db1 || db3 == db2 {
		t.Fatalf("db fiber after re-enable = (%v, %v), want a new instance", db3, ok)
	}
	waitActive(t, sched, db3)

	// Removing an entry unloads it.
	if err := l.Reconcile(mustTree(t,
		loader.Entry{ID: "db", Component: "database", Enabled: true},
	)); err != nil {
		t.Fatalf("Reconcile(remove) = %v", err)
	}
	if _, ok := l.Fiber("console"); ok {
		t.Fatal("removed console still has a fiber")
	}
	if _, alive := sched.Inspect(console1); alive {
		t.Fatal("removed console's fiber survived")
	}
}

func TestReapplyingTheSameTreeChangesNothing(t *testing.T) {
	sched := runtime.New()
	defer sched.Close()

	reg := loader.NewRegistry()
	dbKey := context.NewKey[string]("db")
	if err := reg.Register("database", stringProvider(dbKey, "db")); err != nil {
		t.Fatal(err)
	}
	l := loader.New(sched, reg)

	tree := mustTree(t, loader.Entry{ID: "db", Component: "database", Enabled: true})
	if err := l.Reconcile(tree); err != nil {
		t.Fatalf("Reconcile() = %v", err)
	}
	before := sched.Snapshot()

	if err := l.Reconcile(mustTree(t, loader.Entry{ID: "db", Component: "database", Enabled: true})); err != nil {
		t.Fatalf("Reconcile(same tree) = %v", err)
	}
	after := sched.Snapshot()

	if len(before.Fibers) != len(after.Fibers) {
		t.Fatalf("fiber count changed from %d to %d", len(before.Fibers), len(after.Fibers))
	}
	for i := range before.Fibers {
		if before.Fibers[i].ID != after.Fibers[i].ID || before.Fibers[i].State != after.Fibers[i].State {
			t.Fatalf("re-applying an unchanged tree transitioned a fiber: %+v -> %+v", before.Fibers[i], after.Fibers[i])
		}
	}
}
