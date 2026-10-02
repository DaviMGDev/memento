package loader_test

import (
	"sync/atomic"
	"testing"

	"github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/loader"
	"github.com/DaviMGDev/memento/runtime"
)

// payloadComponent binds its key to the string payload it is activated with
// and counts how often it activated.
type payloadComponent struct {
	key         context.Key[string]
	activations *int32
}

func (c *payloadComponent) Declarations() runtime.Declarations {
	return runtime.Declarations{Provide: []context.AnyKey{c.key}}
}

func (c *payloadComponent) Activate(inst *runtime.Instance, payload any) error {
	atomic.AddInt32(c.activations, 1)
	return runtime.Bind(inst, c.key, payload.(string))
}

// payloadAwareComponent additionally decides whether a payload change is
// material.
type payloadAwareComponent struct {
	payloadComponent
	material func(oldPayload, newPayload any) bool
}

func (c *payloadAwareComponent) Material(oldPayload, newPayload any) bool {
	return c.material(oldPayload, newPayload)
}

func TestPayloadChangeReloadsOnlyItsEntry(t *testing.T) {
	sched := runtime.New()
	defer sched.Close()

	reg := loader.NewRegistry()
	dbKey := context.NewKey[string]("db")
	consoleKey := context.NewKey[string]("console")

	var dbActivations, consoleActivations int32
	if err := reg.Register("database", func(payload any) (runtime.Component, error) {
		return &payloadComponent{key: dbKey, activations: &dbActivations}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("console", func(payload any) (runtime.Component, error) {
		return &payloadComponent{key: consoleKey, activations: &consoleActivations}, nil
	}); err != nil {
		t.Fatal(err)
	}
	l := loader.New(sched, reg)

	if err := l.Reconcile(mustTree(t,
		loader.Entry{ID: "db", Component: "database", Payload: "v1", Enabled: true},
		loader.Entry{ID: "console", Component: "console", Payload: "console", Enabled: true},
	)); err != nil {
		t.Fatalf("Reconcile() = %v", err)
	}
	db1, _ := l.Fiber("db")
	console1, _ := l.Fiber("console")
	waitActive(t, sched, db1)
	waitActive(t, sched, console1)

	// Only the payload of db changes.
	if err := l.Reconcile(mustTree(t,
		loader.Entry{ID: "db", Component: "database", Payload: "v2", Enabled: true},
		loader.Entry{ID: "console", Component: "console", Payload: "console", Enabled: true},
	)); err != nil {
		t.Fatalf("Reconcile(payload change) = %v", err)
	}

	db2, _ := l.Fiber("db")
	if db2 != db1 {
		t.Fatalf("payload change replaced the fiber: %v -> %v", db1, db2)
	}
	if got := atomic.LoadInt32(&dbActivations); got != 2 {
		t.Fatalf("db activations = %d, want 2 (initial + reload)", got)
	}
	waitActive(t, sched, db2)

	if console2, _ := l.Fiber("console"); console2 != console1 {
		t.Fatal("a payload change on db touched console's fiber")
	}
	if got := atomic.LoadInt32(&consoleActivations); got != 1 {
		t.Fatalf("console activations = %d, want 1 (untouched)", got)
	}
}

func TestImmaterialPayloadChangeDoesNotReload(t *testing.T) {
	sched := runtime.New()
	defer sched.Close()

	reg := loader.NewRegistry()
	dbKey := context.NewKey[string]("db")

	var activations int32
	material := func(oldPayload, newPayload any) bool { return false }
	if err := reg.Register("database", func(payload any) (runtime.Component, error) {
		return &payloadAwareComponent{
			payloadComponent: payloadComponent{key: dbKey, activations: &activations},
			material:         func(oldPayload, newPayload any) bool { return material(oldPayload, newPayload) },
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	l := loader.New(sched, reg)

	if err := l.Reconcile(mustTree(t,
		loader.Entry{ID: "db", Component: "database", Payload: "v1", Enabled: true},
	)); err != nil {
		t.Fatalf("Reconcile() = %v", err)
	}
	db1, _ := l.Fiber("db")
	waitActive(t, sched, db1)

	// The component declares the change immaterial: no reload happens.
	if err := l.Reconcile(mustTree(t,
		loader.Entry{ID: "db", Component: "database", Payload: "v2", Enabled: true},
	)); err != nil {
		t.Fatalf("Reconcile(immaterial) = %v", err)
	}
	if got := atomic.LoadInt32(&activations); got != 1 {
		t.Fatalf("activations = %d, want 1: an immaterial change must not reload", got)
	}
	if db2, _ := l.Fiber("db"); db2 != db1 {
		t.Fatal("an immaterial payload change replaced the fiber")
	}
	if info, _ := sched.Inspect(db1); info.State != runtime.StateActive {
		t.Fatalf("state = %v, want active", info.State)
	}

	// A material change still reloads the same fiber.
	material = func(oldPayload, newPayload any) bool { return newPayload == "v3" }
	if err := l.Reconcile(mustTree(t,
		loader.Entry{ID: "db", Component: "database", Payload: "v3", Enabled: true},
	)); err != nil {
		t.Fatalf("Reconcile(material) = %v", err)
	}
	if got := atomic.LoadInt32(&activations); got != 2 {
		t.Fatalf("activations = %d, want 2 after a material change", got)
	}
	if db3, _ := l.Fiber("db"); db3 != db1 {
		t.Fatal("a material payload change replaced the fiber instead of reloading it")
	}
}
