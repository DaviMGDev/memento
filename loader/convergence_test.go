package loader_test

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/DaviMGDev/memento/context"
	"github.com/DaviMGDev/memento/loader"
	"github.com/DaviMGDev/memento/runtime"
)

// recorder tracks the bindings that are currently active, keyed by name. A
// component records its binding on activation and removes the record in the
// inverse of that effect, so a quiescent recorder holds exactly the active
// bindings of a run.
type recorder struct {
	mu     sync.Mutex
	values map[string]string
}

func newRecorder() *recorder { return &recorder{values: make(map[string]string)} }

func (r *recorder) set(name, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[name] = value
}

func (r *recorder) del(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.values, name)
}

func (r *recorder) snapshot() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.values))
	for k, v := range r.values {
		out[k] = v
	}
	return out
}

func recordingProvider(key context.Key[string], name string, rec *recorder) loader.Factory {
	return func(payload any) (runtime.Component, error) {
		return &testComponent{
			decls: runtime.Declarations{Provide: []context.AnyKey{key}},
			activate: func(inst *runtime.Instance, payload any) error {
				value := payload.(string)
				if err := runtime.Bind(inst, key, value); err != nil {
					return err
				}
				rec.set(name, value)
				return inst.Context().RegisterEffect(func() (func() error, error) {
					return func() error {
						rec.del(name)
						return nil
					}, nil
				})
			},
		}, nil
	}
}

func applyRevisions(t *testing.T, revisions [][]loader.Entry) (*loader.Loader, *runtime.Scheduler, *recorder) {
	t.Helper()
	sched := runtime.New()
	t.Cleanup(func() { sched.Close() })

	rec := newRecorder()
	reg := loader.NewRegistry()
	storage := context.NewKey[string]("storage")
	console := context.NewKey[string]("console")
	for ref, factory := range map[string]loader.Factory{
		"database": recordingProvider(storage, "storage", rec),
		"cache":    recordingProvider(storage, "storage", rec),
		"console":  recordingProvider(console, "console", rec),
	} {
		if err := reg.Register(ref, factory); err != nil {
			t.Fatalf("Register(%s) = %v", ref, err)
		}
	}

	l := loader.New(sched, reg)
	for i, entries := range revisions {
		if err := l.Reconcile(mustTree(t, entries...)); err != nil {
			t.Fatalf("revision %d: Reconcile() = %v", i, err)
		}
	}
	return l, sched, rec
}

func TestRevisionSequenceConvergesToFromScratchState(t *testing.T) {
	revisions := [][]loader.Entry{
		{
			{ID: "db", Component: "database", Payload: "v1", Enabled: true},
			{ID: "console", Component: "console", Payload: "c1", Enabled: true},
		},
		{
			{ID: "db", Component: "database", Payload: "v2", Enabled: true},
			{ID: "console", Component: "console", Payload: "c1", Enabled: true},
		},
		{
			{ID: "db", Component: "database", Payload: "v2", Enabled: true},
			{ID: "console", Component: "console", Payload: "c1", Enabled: false},
		},
		{
			{ID: "db", Component: "cache", Payload: "v2", Enabled: true},
			{ID: "console", Component: "console", Payload: "c2", Enabled: true},
		},
		{
			{ID: "db", Component: "cache", Payload: "v3", Enabled: true},
		},
	}

	incremental, _, recIncremental := applyRevisions(t, revisions)
	fresh, _, recFresh := applyRevisions(t, [][]loader.Entry{revisions[len(revisions)-1]})

	got := recIncremental.snapshot()
	want := recFresh.snapshot()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("incremental state = %v, from-scratch state = %v", got, want)
	}
	if len(want) != 1 || want["storage"] != "v3" {
		t.Fatalf("from-scratch bindings = %v, want {storage:v3}", want)
	}

	if _, ok := incremental.Fiber("db"); !ok {
		t.Fatal("incremental run has no db fiber")
	}
	if _, ok := fresh.Fiber("db"); !ok {
		t.Fatal("from-scratch run has no db fiber")
	}
}

func TestFailingEntryDoesNotBlockOthers(t *testing.T) {
	sched := runtime.New()
	defer sched.Close()

	reg := loader.NewRegistry()
	goodKey := context.NewKey[string]("good")
	badKey := context.NewKey[string]("bad")
	if err := reg.Register("good", stringProvider(goodKey, "ok")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("bad", func(payload any) (runtime.Component, error) {
		return &testComponent{
			decls: runtime.Declarations{Provide: []context.AnyKey{badKey}},
			activate: func(*runtime.Instance, any) error {
				return errors.New("boom")
			},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("bad-fixed", stringProvider(badKey, "fixed")); err != nil {
		t.Fatal(err)
	}
	l := loader.New(sched, reg)

	if err := l.Reconcile(mustTree(t,
		loader.Entry{ID: "good", Component: "good", Enabled: true},
		loader.Entry{ID: "bad", Component: "bad", Enabled: true},
	)); err != nil {
		t.Fatalf("Reconcile() = %v, want the failure confined to its entry", err)
	}

	goodID, ok := l.Fiber("good")
	if !ok {
		t.Fatal("good entry has no fiber")
	}
	waitActive(t, sched, goodID)

	badID, ok := l.Fiber("bad")
	if !ok {
		t.Fatal("bad entry has no fiber")
	}
	if info, _ := sched.Inspect(badID); info.State != runtime.StateFailed {
		t.Fatalf("bad entry state = %v, want failed", info.State)
	}

	// A revision replaces the failed instance with a fresh, working one.
	if err := l.Reconcile(mustTree(t,
		loader.Entry{ID: "good", Component: "good", Enabled: true},
		loader.Entry{ID: "bad", Component: "bad-fixed", Enabled: true},
	)); err != nil {
		t.Fatalf("Reconcile(revision) = %v", err)
	}
	fixedID, ok := l.Fiber("bad")
	if !ok || fixedID == badID {
		t.Fatalf("revision fiber = (%v, %v), want a fresh instance", fixedID, ok)
	}
	waitActive(t, sched, fixedID)
	if _, alive := sched.Inspect(badID); alive {
		t.Fatal("the failed instance survived the revision")
	}
}
