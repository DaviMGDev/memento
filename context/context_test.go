package context

import "testing"

func TestNearestBindingShadowsAncestors(t *testing.T) {
	root := NewContext(1)
	child := root.Derive(2)
	grandchild := child.Derive(3)

	storage := NewKey[string]("storage")
	root.install(storage.ID(), 1, "root")
	child.install(storage.ID(), 2, "child")

	if got, ok := storage.Lookup(child); !ok || got != "child" {
		t.Fatalf("child lookup = (%q, %v), want (\"child\", true)", got, ok)
	}
	if got, ok := storage.Lookup(grandchild); !ok || got != "child" {
		t.Fatalf("grandchild lookup = (%q, %v), want (\"child\", true)", got, ok)
	}
	if got, ok := storage.Lookup(root); !ok || got != "root" {
		t.Fatalf("root lookup = (%q, %v), want (\"root\", true)", got, ok)
	}
}

func TestSiblingContextsStayBlind(t *testing.T) {
	root := NewContext(1)
	left := root.Derive(2)
	right := root.Derive(3)

	storage := NewKey[string]("storage")
	root.install(storage.ID(), 1, "root")
	left.install(storage.ID(), 2, "left")

	if got, ok := storage.Lookup(right); !ok || got != "root" {
		t.Fatalf("right sibling lookup = (%q, %v), want nearest binding \"root\"", got, ok)
	}

	onlyLeft := NewKey[int]("only-left")
	left.install(onlyLeft.ID(), 2, 42)
	if _, ok := onlyLeft.Lookup(root); ok {
		t.Fatal("a binding installed in a child is visible from the root")
	}
	if _, ok := onlyLeft.Lookup(right); ok {
		t.Fatal("a binding installed in one sibling is visible from the other")
	}
	if v, ok := onlyLeft.Lookup(left); !ok || v != 42 {
		t.Fatalf("left lookup = (%d, %v), want (42, true)", v, ok)
	}
}

func TestWithdrawRestoresInheritedBinding(t *testing.T) {
	root := NewContext(1)
	child := root.Derive(2)

	key := NewKey[string]("key")
	root.install(key.ID(), 1, "root")
	child.install(key.ID(), 2, "child")

	if got, ok := key.Lookup(child); !ok || got != "child" {
		t.Fatalf("lookup after shadowing = (%q, %v), want (\"child\", true)", got, ok)
	}
	removed, ok := child.withdraw(key.ID(), 2)
	if !ok || removed.value != "child" || removed.owner != 2 {
		t.Fatalf("withdraw = (%+v, %v), want the child binding", removed, ok)
	}
	if got, ok := key.Lookup(child); !ok || got != "root" {
		t.Fatalf("lookup after withdraw = (%q, %v), want inherited (\"root\", true)", got, ok)
	}
	if _, ok := child.withdraw(key.ID(), 2); ok {
		t.Fatal("second withdraw reported removing a binding")
	}
}

func TestContextIdentityAndParentage(t *testing.T) {
	root := NewContext(7)
	if root.Parent() != nil {
		t.Fatal("root context has a parent")
	}
	if root.Fiber() != 7 {
		t.Fatalf("root.Fiber() = %v, want 7", root.Fiber())
	}
	child := root.Derive(8)
	if child.Parent() != root {
		t.Fatal("derived context does not point at its parent")
	}
	if child.Fiber() != 8 {
		t.Fatalf("child.Fiber() = %v, want 8", child.Fiber())
	}
}

func TestLookupThroughNilAndZero(t *testing.T) {
	var nilCtx *Context
	key := NewKey[string]("key")
	if _, ok := key.Lookup(nilCtx); ok {
		t.Fatal("lookup through a nil context found a binding")
	}
	var zero Key[string]
	if _, ok := zero.Lookup(NewContext(1)); ok {
		t.Fatal("lookup through the zero key found a binding")
	}
}
