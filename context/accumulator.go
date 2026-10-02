package context

import (
	"errors"
	"fmt"
)

// Accumulator collects the inverses of the effects a fiber installs.
//
// Inverses run serially in last-in-first-out order, each exactly once:
// Revert drains the accumulator, so a repeated Revert — a redundant unload
// request — is a no-op. An error from one inverse does not stop the
// remaining ones; the errors are joined and returned. A panicking inverse
// is contained and reported as an error, so teardown keeps making progress.
//
// An Accumulator is owned by one fiber: pushes happen on the fiber's
// activation path and Revert runs on its deactivation path, and the runtime
// serializes those transitions, so the type needs no internal locking.
type Accumulator struct {
	inverses []func() error
}

// Push appends inv as the most recent effect of the fiber. A nil inverse is
// ignored.
func (a *Accumulator) Push(inv func() error) {
	if inv == nil {
		return
	}
	a.inverses = append(a.inverses, inv)
}

// Pending reports how many effects have not been reverted yet.
func (a *Accumulator) Pending() int { return len(a.inverses) }

// Revert runs every pending inverse in LIFO order and drains the
// accumulator. The first call performs the work; later calls do nothing and
// return nil. Exactly-once is structural: drained inverses cannot run again.
func (a *Accumulator) Revert() error {
	var errs []error
	for i := len(a.inverses) - 1; i >= 0; i-- {
		if err := runInverse(a.inverses[i]); err != nil {
			errs = append(errs, err)
		}
	}
	a.inverses = nil
	return errors.Join(errs...)
}

func runInverse(inv func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("context: effect inverse panicked: %v", r)
		}
	}()
	return inv()
}
