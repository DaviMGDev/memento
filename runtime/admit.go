package runtime

import (
	"fmt"
	"sort"

	"github.com/DaviMGDev/memento/context"
)

// checkDeclarations rejects malformed declaration sets.
func checkDeclarations(d Declarations) error {
	seen := make(map[context.KeyID]string)
	for _, k := range d.Provide {
		if k == nil || k.IsZero() {
			return fmt.Errorf("runtime: zero key in the provide set")
		}
		if where, ok := seen[k.ID()]; ok {
			return fmt.Errorf("runtime: key %q declared twice (%s and provide)", k.Name(), where)
		}
		seen[k.ID()] = "provide"
	}
	for _, k := range d.Inject {
		if k == nil || k.IsZero() {
			return fmt.Errorf("runtime: zero key in the inject set")
		}
		if seen[k.ID()] == "provide" {
			return fmt.Errorf("runtime: key %q is both injected and provided by one component", k.Name())
		}
	}
	return nil
}

// admit enforces the two structural admission rules: a key has one possible
// provider (provisions are disjoint), and precedence stays acyclic.
func (s *Scheduler) admit(d Declarations) error {
	for _, k := range d.Provide {
		if owner, ok := s.claims[k.ID()]; ok {
			return fmt.Errorf("runtime: key %q is already provided by fiber %d", k.Name(), owner)
		}
	}
	return s.checkCycle(d)
}

type declaredNode struct {
	label    string
	provides map[context.KeyID]string
	injects  map[context.KeyID]string
}

// checkCycle refuses a declaration set whose precedence would close a
// cycle. Precedence edges run from a provider to a fiber that injects one
// of its keys.
func (s *Scheduler) checkCycle(d Declarations) error {
	nodes := make(map[context.FiberID]declaredNode, len(s.fibers)+1)
	for id, f := range s.fibers {
		nodes[id] = declaredNode{
			label:    fmt.Sprintf("fiber %d", id),
			provides: keyNames(f.decls.Provide),
			injects:  keyNames(f.decls.Inject),
		}
	}
	const newID = context.FiberID(0)
	nodes[newID] = declaredNode{
		label:    "the new component",
		provides: keyNames(d.Provide),
		injects:  keyNames(d.Inject),
	}

	adj := make(map[context.FiberID][]context.FiberID, len(nodes))
	for a, da := range nodes {
		for b, db := range nodes {
			if !shareKey(da.provides, db.injects) {
				continue
			}
			if a == b {
				return fmt.Errorf("runtime: dependency cycle refused: %s provides a key it injects", da.label)
			}
			adj[a] = append(adj[a], b)
		}
	}

	state := make(map[context.FiberID]int, len(nodes)) // 0 new, 1 on stack, 2 done
	var stack []context.FiberID
	var visit func(id context.FiberID) error
	visit = func(id context.FiberID) error {
		state[id] = 1
		stack = append(stack, id)
		for _, next := range adj[id] {
			switch state[next] {
			case 1:
				return fmt.Errorf("runtime: dependency cycle refused: %s", cyclePath(nodes, stack, next))
			case 0:
				if err := visit(next); err != nil {
					return err
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = 2
		return nil
	}

	order := make([]context.FiberID, 0, len(nodes))
	for id := range nodes {
		order = append(order, id)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	for _, id := range order {
		if state[id] == 0 {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}

func cyclePath(nodes map[context.FiberID]declaredNode, stack []context.FiberID, closed context.FiberID) string {
	start := 0
	for i, id := range stack {
		if id == closed {
			start = i
			break
		}
	}
	path := ""
	for _, id := range stack[start:] {
		path += nodes[id].label + " → "
	}
	return path + nodes[closed].label
}

func keyNames(keys []context.AnyKey) map[context.KeyID]string {
	out := make(map[context.KeyID]string, len(keys))
	for _, k := range keys {
		out[k.ID()] = k.Name()
	}
	return out
}

func shareKey(a, b map[context.KeyID]string) bool {
	small, large := a, b
	if len(large) < len(small) {
		small, large = large, small
	}
	for k := range small {
		if _, ok := large[k]; ok {
			return true
		}
	}
	return false
}
