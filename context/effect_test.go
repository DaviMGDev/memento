package context

import (
	"errors"
	"slices"
	"testing"
)

func TestExplicitEffectIsAccumulatedAndReverted(t *testing.T) {
	root := NewContext(1)
	child := root.Derive(2)

	released := false
	err := child.RegisterEffect(func() (func() error, error) {
		return func() error {
			released = true
			return nil
		}, nil
	})
	if err != nil {
		t.Fatalf("RegisterEffect() = %v, want nil", err)
	}
	if released {
		t.Fatal("the inverse ran during registration instead of at unload")
	}
	if got := child.Effects().Pending(); got != 1 {
		t.Fatalf("Pending() = %d, want 1", got)
	}
	if err := child.Effects().Revert(); err != nil {
		t.Fatalf("Revert() = %v, want nil", err)
	}
	if !released {
		t.Fatal("the custom resource was not reclaimed on unload")
	}
}

func TestExplicitEffectSharesTheBuiltInOrder(t *testing.T) {
	root := NewContext(1)
	child := root.Derive(2)
	var order []string

	// A built-in effect registers directly on the same accumulator; Bind
	// will do exactly this once bindings land.
	child.Effects().Push(func() error {
		order = append(order, "builtin")
		return nil
	})
	err := child.RegisterEffect(func() (func() error, error) {
		return func() error {
			order = append(order, "custom")
			return nil
		}, nil
	})
	if err != nil {
		t.Fatalf("RegisterEffect() = %v, want nil", err)
	}

	if err := child.Effects().Revert(); err != nil {
		t.Fatalf("Revert() = %v, want nil", err)
	}
	if want := []string{"custom", "builtin"}; !slices.Equal(order, want) {
		t.Fatalf("revert order = %v, want %v", order, want)
	}
}

func TestExplicitEffectInstallErrorPushesNothing(t *testing.T) {
	root := NewContext(1)
	child := root.Derive(2)

	boom := errors.New("cannot install")
	err := child.RegisterEffect(func() (func() error, error) {
		return nil, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("RegisterEffect() = %v, want the install error", err)
	}
	if got := child.Effects().Pending(); got != 0 {
		t.Fatalf("Pending() = %d, want 0: a failed install must push no inverse", got)
	}
}

func TestExplicitEffectWithNilInverse(t *testing.T) {
	root := NewContext(1)
	child := root.Derive(2)

	if err := child.RegisterEffect(func() (func() error, error) { return nil, nil }); err != nil {
		t.Fatalf("RegisterEffect() = %v, want nil", err)
	}
	if got := child.Effects().Pending(); got != 0 {
		t.Fatalf("Pending() = %d, want 0 for an effect with nothing to reclaim", got)
	}
}

func TestExplicitEffectRequiresAFiber(t *testing.T) {
	root := NewContext(1)
	install := func() (func() error, error) { return nil, nil }

	if err := root.RegisterEffect(install); !errors.Is(err, ErrNoFiber) {
		t.Fatalf("root RegisterEffect() = %v, want ErrNoFiber", err)
	}
	if err := (*Context)(nil).RegisterEffect(install); !errors.Is(err, ErrNoFiber) {
		t.Fatalf("nil-context RegisterEffect() = %v, want ErrNoFiber", err)
	}
	if err := root.Derive(2).RegisterEffect(nil); err == nil {
		t.Fatal("RegisterEffect(nil) = nil error, want a misuse error")
	}
}
