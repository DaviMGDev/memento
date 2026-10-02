// Package context implements the typed context tree, bindings, and
// revertible effects of the spatiotemporal composability runtime.
//
// Every component runs against a context derived from its parent's; a
// context carries a reference to its parent, its fiber, its own bindings,
// and views over inherited ones. Mutating operations register the inverse
// that reverts them with the calling fiber.
package context
