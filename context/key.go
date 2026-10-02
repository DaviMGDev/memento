package context

import "fmt"

// KeyID is the identity of a typed key.
//
// KeyID values are comparable with ==, and two keys denote the same binding
// slot exactly when their IDs compare equal. KeyID is opaque to callers: it
// is produced by Key.ID and consumed by the runtime when it maintains
// heterogeneous tables keyed by key identity.
type KeyID struct {
	core *keyCore
}

// IsZero reports whether id identifies no key at all.
func (id KeyID) IsZero() bool { return id.core == nil }

// Name returns the key's diagnostic name; the zero KeyID returns "".
func (id KeyID) Name() string {
	if id.core == nil {
		return ""
	}
	return id.core.name
}

// String implements fmt.Stringer for diagnostics.
func (id KeyID) String() string {
	if id.core == nil {
		return "<zero-key>"
	}
	return "key(" + id.core.name + ")"
}

// AnyKey is the type-erased view of a Key.
//
// A component's declarations are heterogeneous sets of keys that may carry
// different value types; AnyKey is what such a set holds. Typed access
// always goes through Key[T], never through AnyKey.
type AnyKey interface {
	ID() KeyID
	Name() string
	IsZero() bool
	String() string
}

// keyCore is the shared identity cell of a key. Every copy of a Key shares
// its *keyCore; every call to NewKey allocates a distinct cell, which is
// what makes same-typed keys impossible to collide.
type keyCore struct {
	name string
}

// Key is a statically typed, package-level handle to a binding slot.
//
// The type parameter T is the value type carried by the key, so reads and
// writes through a Key[T] are checked at compile time. Identity lives in
// the shared *keyCore, not in T: two keys instantiated with the same T are
// still distinct keys.
type Key[T any] struct {
	core *keyCore
}

// NewKey returns a fresh key with the given diagnostic name.
//
// The name is diagnostic only, not an identity: two keys created with the
// same name are still distinct. Keys are meant to be package-level values,
// created once and shared; talking about "the storage key" means sharing
// one Key value, not agreeing on a name.
func NewKey[T any](name string) Key[T] {
	if name == "" {
		name = "unnamed"
	}
	return Key[T]{core: &keyCore{name: name}}
}

// IsZero reports whether k is the zero Key, which never identifies a
// binding. A zero Key's ID is the zero KeyID.
func (k Key[T]) IsZero() bool { return k.core == nil }

// ID returns the runtime identity of the key.
func (k Key[T]) ID() KeyID { return KeyID{core: k.core} }

// Name returns the key's diagnostic name; the zero Key returns "".
func (k Key[T]) Name() string {
	if k.core == nil {
		return ""
	}
	return k.core.name
}

// String implements fmt.Stringer for diagnostics.
func (k Key[T]) String() string {
	if k.core == nil {
		return "<zero-key>"
	}
	return fmt.Sprintf("key(%s)", k.core.name)
}

// Interface assertions: Key[T] must satisfy AnyKey for every T.
var _ AnyKey = Key[struct{}]{}
