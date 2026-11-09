package computeruse

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// The dispatch lease tests (add-computer-use tasks 2.2, design D3, spec
// computer-use "Single-dispatcher arbitration"): contention answers the
// fixed busy refusal verbatim, a released lease admits the next caller, and
// concurrent contenders produce exactly one winner per generation.

// TestDispatchConcurrentAcquireRefused pins the fixed busy refusal: while a
// lease is held, a second caller receives exactly the spec's text — no
// holder identity, no queue — and the released lock admits the next caller.
func TestDispatchConcurrentAcquireRefused(t *testing.T) {
	require.NoError(t, acquireDispatch())
	t.Cleanup(releaseDispatch)

	err := acquireDispatch()
	require.Error(t, err)
	require.Equal(t, BusyRefusal, err.Error(), "the busy refusal is fixed verbatim (design D3)")

	// Release admits the next caller, and the lease is exclusive again.
	releaseDispatch()
	require.NoError(t, acquireDispatch())
	err = acquireDispatch()
	require.Error(t, err)
	require.Equal(t, BusyRefusal, err.Error())
	releaseDispatch()

	// Double release is safe and leaves the lease free.
	releaseDispatch()
	releaseDispatch()
	require.NoError(t, acquireDispatch())
	releaseDispatch()
}

// TestDispatchConcurrentContentionOneWinner drives real goroutines at the
// lease: the generation's winner holds it until every contender has tried,
// so exactly one caller wins per generation, every loser sees the fixed
// text, and the release admits the next generation.
func TestDispatchConcurrentContentionOneWinner(t *testing.T) {
	const contenders = 16
	for generation := 0; generation < 3; generation++ {
		var mu sync.Mutex
		won, refusals := 0, 0
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < contenders; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if err := acquireDispatch(); err != nil {
					require.Equal(t, BusyRefusal, err.Error())
					mu.Lock()
					refusals++
					mu.Unlock()
					return
				}
				mu.Lock()
				won++
				mu.Unlock()
				// The winner holds the lease until the generation ends.
			}()
		}
		close(start)
		wg.Wait()
		require.Equal(t, 1, won, "exactly one contender holds the lease per generation")
		require.Equal(t, contenders-1, refusals)
		releaseDispatch()
	}
}
