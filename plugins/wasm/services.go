package wasm

import (
	"github.com/DaviMGDev/memento/runtime"
)

// Host import result codes. Every host import returns 0 on success or a
// non-zero code; the job, publish, and cancellation imports share the table.
const (
	// FailureCode is the generic non-zero failure code.
	FailureCode uint32 = 1
	// CanceledCode is returned to a guest whose calling job was killed: as
	// the failure code of any host import reached afterwards, and as the
	// positive answer of cancel_poll.
	CanceledCode uint32 = 2
)

// HostServices is the host-side contract behind the job, publish, and
// cancellation imports (SPEC.md, "Host Services" and D11-D13). The embedding
// host supplies one with WithHostServices; the loader treats every job
// document as opaque bytes and owns nothing about tools, topics, or job
// records.
//
// Job calls receive the calling instance so the host can answer for the
// caller's own subtree only; the returned document is stashed for the guest
// to read back with job_result_len/job_result. Cancelled reports whether the
// calling job was killed; the host answers for the caller only.
type HostServices interface {
	// StartJob starts a job from a request document and returns the result
	// document to stash. It must not wait for the job to finish.
	StartJob(caller *runtime.Instance, req []byte) ([]byte, error)
	// PeepJob reads one job's status from a request document and returns
	// the result document to stash.
	PeepJob(caller *runtime.Instance, req []byte) ([]byte, error)
	// KillJob kills one job from a request document and returns the result
	// document to stash.
	KillJob(caller *runtime.Instance, req []byte) ([]byte, error)
	// Publish delivers one event to the host bus.
	Publish(topic string, payload []byte) error
	// Cancelled reports whether the calling job was killed.
	Cancelled(caller *runtime.Instance) bool
}

// canceled reports whether the calling job of this instance was killed. With
// no host services configured, or outside an activation instance, nothing is
// canceled.
func (s *execState) canceled() bool {
	if s == nil || s.services == nil || s.instance == nil {
		return false
	}
	return s.services.Cancelled(s.instance)
}
