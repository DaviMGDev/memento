package loader_test

import (
	"strings"
	"testing"

	"github.com/DaviMGDev/memento/loader"
	"github.com/DaviMGDev/memento/runtime"
)

type stubComponent struct {
	decls runtime.Declarations
}

func (s *stubComponent) Declarations() runtime.Declarations { return s.decls }

func (s *stubComponent) Activate(*runtime.Instance, any) error { return nil }

func TestRegistryResolvesFactories(t *testing.T) {
	reg := loader.NewRegistry()
	want := &stubComponent{decls: runtime.Declarations{}}

	if err := reg.Register("database", func(payload any) (runtime.Component, error) {
		return want, nil
	}); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	if !reg.Has("database") {
		t.Fatal("Has(database) = false after registration")
	}

	factory, err := reg.Resolve("database")
	if err != nil {
		t.Fatalf("Resolve(database) = %v", err)
	}
	comp, err := factory(nil)
	if err != nil {
		t.Fatalf("factory() = %v", err)
	}
	if comp != runtime.Component(want) {
		t.Fatal("Resolve returned a different factory than the one registered")
	}

	if _, err := reg.Resolve("missing"); err == nil {
		t.Fatal("Resolve(missing) = nil, want an unknown-reference error")
	} else if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Resolve(missing) error = %v, want it to name the reference", err)
	}
}

func TestRegistryRejectsInvalidRegistrations(t *testing.T) {
	reg := loader.NewRegistry()
	factory := func(payload any) (runtime.Component, error) { return &stubComponent{}, nil }

	if err := reg.Register("", factory); err == nil {
		t.Fatal("Register with an empty reference succeeded")
	}
	if err := reg.Register("x", nil); err == nil {
		t.Fatal("Register with a nil factory succeeded")
	}
	if err := reg.Register("x", factory); err != nil {
		t.Fatalf("Register(x) = %v", err)
	}
	if err := reg.Register("x", factory); err == nil {
		t.Fatal("duplicate Register(x) succeeded")
	}
}
