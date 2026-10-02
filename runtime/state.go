package runtime

// State is the lifecycle state of a fiber.
//
// Only StateActive contributes bindings to resolution. StateLoading and
// StateUnloading are transitional: a fiber in either provides nothing — its
// withdrawal is decided before its inverses run — but still resolves its
// own declared keys through its committed view. StateFailed is terminal
// until a revision re-inserts the component as a fresh instance.
type State uint8

const (
	// StateInactive: the fiber exists but is not contributing bindings and
	// is not activating.
	StateInactive State = iota
	// StateLoading: activation is in flight.
	StateLoading
	// StateActive: activation completed; the fiber's bindings resolve.
	StateActive
	// StateUnloading: deactivation is in flight.
	StateUnloading
	// StateFailed: activation failed; no automatic retry.
	StateFailed
)

// String returns the state's lowercase name as it appears in the behavioral
// contract (specs/features/lifecycle.feature).
func (s State) String() string {
	switch s {
	case StateInactive:
		return "inactive"
	case StateLoading:
		return "loading"
	case StateActive:
		return "active"
	case StateUnloading:
		return "unloading"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}
