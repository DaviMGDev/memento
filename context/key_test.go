package context_test

import (
	"testing"

	"github.com/DaviMGDev/memento/context"
)

func TestSameTypedKeysNeverCollide(t *testing.T) {
	a := context.NewKey[string]("storage")
	b := context.NewKey[string]("storage")

	if a.ID() == b.ID() {
		t.Fatal("two keys of the same value type share one identity")
	}

	c := a
	if a.ID() != c.ID() {
		t.Fatal("copies of a key must share its identity")
	}
}

func TestKeyIdentityAcrossValueTypes(t *testing.T) {
	var decls []context.AnyKey
	decls = append(decls,
		context.NewKey[string]("storage"),
		context.NewKey[int]("storage"),
		context.NewKey[[]byte]("storage"),
	)

	seen := make(map[context.KeyID]bool)
	for _, k := range decls {
		if k.ID().IsZero() {
			t.Fatalf("key %q has the zero identity", k.Name())
		}
		if seen[k.ID()] {
			t.Fatalf("key %q collides with an earlier declaration", k.Name())
		}
		seen[k.ID()] = true
	}
}

func TestKeyNameAndZeroValue(t *testing.T) {
	k := context.NewKey[int]("count")
	if k.Name() != "count" {
		t.Fatalf("Key.Name() = %q, want %q", k.Name(), "count")
	}
	if k.ID().Name() != "count" {
		t.Fatalf("KeyID.Name() = %q, want %q", k.ID().Name(), "count")
	}
	if k.IsZero() {
		t.Fatal("a key from NewKey must not be zero")
	}

	var zero context.Key[string]
	if !zero.IsZero() {
		t.Fatal("the zero Key must report IsZero")
	}
	if !zero.ID().IsZero() {
		t.Fatal("the zero Key's ID must be the zero KeyID")
	}
}

func TestSameNamedKeysRemainDistinct(t *testing.T) {
	a := context.NewKey[string]("")
	b := context.NewKey[string]("")

	if a.Name() == "" || b.Name() == "" {
		t.Fatal("unnamed keys get a diagnostic name")
	}
	if a.ID() == b.ID() {
		t.Fatal("two unnamed keys of the same type collided")
	}
}
