package runtime

import (
	"strings"
	"testing"

	"github.com/DaviMGDev/memento/context"
)

func TestCycleClosingInsertionIsRefused(t *testing.T) {
	s := New()
	defer s.Close()

	alpha := context.NewKey[string]("alpha")
	beta := context.NewKey[string]("beta")

	first := &testComponent{
		decls: Declarations{
			Provide: []context.AnyKey{alpha},
			Inject:  []context.AnyKey{beta},
		},
		activate: func(inst *Instance, payload any) error {
			return Bind(inst, alpha, "a")
		},
	}
	firstID, err := s.Insert(first, nil)
	if err != nil {
		t.Fatalf("Insert(first) = %v", err)
	}
	waitForState(t, s, firstID, StateInactive)

	closing := &testComponent{
		decls: Declarations{
			Provide: []context.AnyKey{beta},
			Inject:  []context.AnyKey{alpha},
		},
		activate: func(inst *Instance, payload any) error {
			return Bind(inst, beta, "b")
		},
	}
	if _, err := s.Insert(closing, nil); err == nil {
		t.Fatal("inserting the cycle-closing component succeeded")
	} else if !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("Insert() error = %v, want a dependency cycle error", err)
	} else if !strings.Contains(err.Error(), "fiber 1") || !strings.Contains(err.Error(), "the new component") {
		t.Fatalf("Insert() error = %v, want the cycle to name its members", err)
	}

	// The registry is unchanged: the refused component left no trace and
	// the original fiber still owns its claim.
	if len(s.fibers) != 1 {
		t.Fatalf("registry holds %d fibers, want 1 after a refused insert", len(s.fibers))
	}
	info, ok := s.Inspect(firstID)
	if !ok || info.State != StateInactive {
		t.Fatalf("first fiber = (%+v, %v), want it untouched and inactive", info, ok)
	}
	if owner := s.claims[alpha.ID()]; owner != firstID {
		t.Fatalf("claim on alpha = %v, want %v", owner, firstID)
	}
	if _, claimed := s.claims[beta.ID()]; claimed {
		t.Fatal("the refused component left a claim on beta")
	}
}

func TestSecondProviderOfAKeyIsRefused(t *testing.T) {
	s := New()
	defer s.Close()

	key := context.NewKey[string]("exclusive")
	newProvider := func(value string) *testComponent {
		return &testComponent{
			decls: Declarations{Provide: []context.AnyKey{key}},
			activate: func(inst *Instance, payload any) error {
				return Bind(inst, key, value)
			},
		}
	}

	firstID, err := s.Insert(newProvider("first"), nil)
	if err != nil {
		t.Fatalf("Insert(first) = %v", err)
	}
	waitForState(t, s, firstID, StateActive)

	if _, err := s.Insert(newProvider("second"), nil); err == nil {
		t.Fatal("a second provider of one key was admitted")
	} else if !strings.Contains(err.Error(), "already provided") {
		t.Fatalf("Insert() error = %v, want an already-provided error", err)
	}

	if len(s.fibers) != 1 {
		t.Fatalf("registry holds %d fibers, want 1", len(s.fibers))
	}
	if owner := s.claims[key.ID()]; owner != firstID {
		t.Fatalf("claim = %v, want the original provider %v", owner, firstID)
	}
}

func TestSelfDependenceIsRefused(t *testing.T) {
	s := New()
	defer s.Close()

	key := context.NewKey[string]("loop")
	comp := &testComponent{
		decls: Declarations{
			Provide: []context.AnyKey{key},
			Inject:  []context.AnyKey{key},
		},
	}
	if _, err := s.Insert(comp, nil); err == nil {
		t.Fatal("a component that injects a key it provides was admitted")
	}
}
