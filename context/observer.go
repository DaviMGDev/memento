package context

// Observer receives notifications of binding installs and withdrawals made
// through a context that carries it, directly or through derivation. The
// runtime implements Observer to maintain its provider index; the context
// package itself never interprets binding changes.
type Observer interface {
	// BindingChanged reports that the binding for id changed. On install,
	// current is the installing fiber and previous is the owner of the
	// binding it replaced (RootFiber when there was none). On withdrawal,
	// previous is the withdrawing owner and current is RootFiber.
	BindingChanged(id KeyID, previous, current FiberID, installed bool)
}

// SetObserver attaches obs to c, where it receives binding-change
// notifications. A notification walks from the changed context toward the
// root and stops at the nearest observer, so derived contexts inherit the
// nearest ancestor's observer.
func (c *Context) SetObserver(obs Observer) {
	if c == nil {
		return
	}
	c.observer = obs
}

func (c *Context) notifyBinding(id KeyID, previous, current FiberID, installed bool) {
	for cur := c; cur != nil; cur = cur.parent {
		if cur.observer != nil {
			cur.observer.BindingChanged(id, previous, current, installed)
			return
		}
	}
}
