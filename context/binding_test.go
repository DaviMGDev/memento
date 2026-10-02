package context

import "testing"

func TestInstallTracksProviderIdentity(t *testing.T) {
	c := NewContext(1)
	key := NewKey[string]("storage")

	if prev := c.install(key.ID(), 10, "first"); prev != nil {
		t.Fatalf("first install reported a previous binding: %+v", prev)
	}
	if b := c.lookup(key.ID()); b == nil || b.owner != 10 || b.value != "first" {
		t.Fatalf("stored binding = %+v, want {value:first owner:10}", b)
	}

	prev := c.install(key.ID(), 10, "second")
	if prev == nil || prev.owner != 10 {
		t.Fatalf("same-provider overwrite prev = %+v, want the owner-10 binding", prev)
	}
	if b := c.lookup(key.ID()); b.owner != 10 || b.value != "second" {
		t.Fatalf("after overwrite binding = %+v, want {value:second owner:10}", b)
	}

	prev = c.install(key.ID(), 20, "third")
	if prev == nil || prev.owner != 10 {
		t.Fatalf("provider replacement prev = %+v, want the owner-10 binding", prev)
	}
	if b := c.lookup(key.ID()); b.owner != 20 || b.value != "third" {
		t.Fatalf("after replacement binding = %+v, want {value:third owner:20}", b)
	}
}

func TestWithdrawIsOwnerChecked(t *testing.T) {
	c := NewContext(1)
	key := NewKey[string]("storage")
	c.install(key.ID(), 10, "value")

	if removed, ok := c.withdraw(key.ID(), 11); ok {
		t.Fatalf("withdraw by a non-owner removed %+v", removed)
	}
	if b := c.lookup(key.ID()); b == nil || b.owner != 10 {
		t.Fatal("rejected withdraw must leave the binding in place")
	}

	removed, ok := c.withdraw(key.ID(), 10)
	if !ok || removed.owner != 10 || removed.value != "value" {
		t.Fatalf("owner withdraw = (%+v, %v), want the owner-10 binding", removed, ok)
	}
	if b := c.lookup(key.ID()); b != nil {
		t.Fatalf("binding survived its owner's withdraw: %+v", b)
	}
}

func TestOverwriteAndWithdrawAcrossScopes(t *testing.T) {
	root := NewContext(1)
	child := root.Derive(2)
	key := NewKey[int]("quota")

	root.install(key.ID(), 1, 100)
	child.install(key.ID(), 2, 200)

	if v, ok := key.Lookup(child); !ok || v != 200 {
		t.Fatalf("child lookup = (%d, %v), want (200, true)", v, ok)
	}
	if _, ok := child.withdraw(key.ID(), 2); !ok {
		t.Fatal("owner withdraw from the child scope failed")
	}
	if v, ok := key.Lookup(child); !ok || v != 100 {
		t.Fatalf("child lookup after withdraw = (%d, %v), want inherited (100, true)", v, ok)
	}
}
