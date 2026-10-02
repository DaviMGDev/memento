package loader

import (
	"errors"
	"fmt"
)

// Entry is one node of a declarative configuration.
//
// An entry declares a stable id — the reconciliation key — the component it
// instantiates (a registry reference), its configuration payload, and its
// enabled flag. Entries are declarative: they describe the desired
// composition, not the operations to reach it. Children nest entries, so a
// configuration is a tree.
type Entry struct {
	ID        string
	Component string
	Payload   any
	Enabled   bool
	Children  []Entry
}

// Tree is a validated configuration tree.
type Tree struct {
	roots []Entry
}

// NewTree validates entries and returns the configuration tree. Ids must be
// non-empty and unique across the whole tree, and every entry must name the
// component it instantiates.
func NewTree(entries ...Entry) (*Tree, error) {
	seen := make(map[string]bool)
	var walk func(e Entry) error
	walk = func(e Entry) error {
		if e.ID == "" {
			return errors.New("loader: entry with an empty id")
		}
		if seen[e.ID] {
			return fmt.Errorf("loader: duplicate entry id %q", e.ID)
		}
		seen[e.ID] = true
		if e.Component == "" {
			return fmt.Errorf("loader: entry %q has no component reference", e.ID)
		}
		for _, child := range e.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, e := range entries {
		if err := walk(e); err != nil {
			return nil, err
		}
	}
	return &Tree{roots: cloneEntries(entries)}, nil
}

// Roots returns a deep copy of the top-level entries.
func (t *Tree) Roots() []Entry {
	if t == nil {
		return nil
	}
	return cloneEntries(t.roots)
}

func cloneEntries(entries []Entry) []Entry {
	if entries == nil {
		return nil
	}
	out := make([]Entry, len(entries))
	for i, e := range entries {
		out[i] = e
		out[i].Children = cloneEntries(e.Children)
	}
	return out
}

// Walk calls fn for every entry in depth-first pre-order.
func (t *Tree) Walk(fn func(Entry)) {
	if t == nil {
		return
	}
	var walk func(e Entry)
	walk = func(e Entry) {
		fn(e)
		for _, child := range e.Children {
			walk(child)
		}
	}
	for _, e := range t.roots {
		walk(e)
	}
}
