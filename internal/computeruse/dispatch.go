package computeruse

import (
	"errors"
	"sync"
)

// The single-dispatcher arbitration (add-computer-use design D3, spec
// computer-use "Single-dispatcher arbitration"): the daemon enforces no
// cross-client serialization and the adapter is the only process that talks
// to it, so one process-wide dispatch mutex here IS the controller lease.
// Every tool acquires it for the full duration of its driver call — mutating
// and read-only alike, because observation must not race an in-flight
// action's settle window. A second concurrent caller receives the fixed busy
// refusal; nothing queues and nothing interleaves. The lock is in-process
// only: an adapter crash drops it, and the respawn clears it (design risk
// note) while the daemon-side revoke at boot cleans stranded driver state.

// dispatch is the process-wide lease. A plain mutex-guarded flag (not a
// channel) keeps TryAcquire refusal-side: contention answers immediately
// with the fixed busy text instead of parking the caller.
var dispatch struct {
	mu   sync.Mutex
	held bool
}

// acquireDispatch takes the controller lease for one tool call. Contention
// returns the fixed busy refusal — the exact string the spec names — as a
// non-retryable error the handler renders verbatim.
func acquireDispatch() error {
	dispatch.mu.Lock()
	defer dispatch.mu.Unlock()
	if dispatch.held {
		return errors.New(BusyRefusal)
	}
	dispatch.held = true
	return nil
}

// releaseDispatch returns the lease. Safe against double release: an
// unlocked release is a no-op on the flag.
func releaseDispatch() {
	dispatch.mu.Lock()
	defer dispatch.mu.Unlock()
	dispatch.held = false
}
