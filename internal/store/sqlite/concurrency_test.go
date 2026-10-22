package sqlite

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestConcurrent_SessionEventsAndConfigChange covers task 3.5: session event
// appends (background service side) racing concurrent config changes (UI
// side) all complete successfully, and the replayed transcript stays intact
// — contiguous, monotonic sequences with no lost writes. Run under -race.
func TestConcurrent_SessionEventsAndConfigChange(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	const (
		eventWriters    = 6
		configWriters   = 6
		eventsPerWriter = 20
	)

	var wg sync.WaitGroup
	errs := make(chan error, eventWriters+configWriters)

	for w := 0; w < eventWriters; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			ids := make([]string, 0, eventsPerWriter)
			for i := 0; i < eventsPerWriter; i++ {
				ids = append(ids, fmt.Sprintf("w%d-e%d", w, i))
			}
			if err := s.SessionEvents().AppendEvents(ctx, "race-sess", eventBatch(ids...)); err != nil {
				errs <- fmt.Errorf("event writer %d: %w", w, err)
			}
		}(w)
	}

	for w := 0; w < configWriters; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			err := s.WithTx(ctx, func(tx store.Store) error {
				if err := tx.GatewaySettings().Save(ctx, &store.GatewaySettings{
					Enabled:        true,
					DefaultAgentID: fmt.Sprintf("agent-%d", w),
				}); err != nil {
					return err
				}
				return tx.Providers().Create(ctx, &store.Provider{Name: fmt.Sprintf("provider-%d", w)})
			})
			if err != nil {
				errs <- fmt.Errorf("config writer %d: %w", w, err)
			}
		}(w)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent write failed: %v", err)
	}

	// Every session event landed, sequenced contiguously 1..N.
	events, err := s.SessionEvents().LoadEvents(ctx, "race-sess")
	require.NoError(t, err)
	require.Len(t, events, eventWriters*eventsPerWriter, "no session event may be lost")
	for i, ev := range events {
		require.Equal(t, int64(i+1), ev.Seq, "replayed sequences must be contiguous and monotonic")
	}

	// Every config change landed too.
	providers, err := s.Providers().List(ctx)
	require.NoError(t, err)
	require.Len(t, providers, configWriters, "no config write may be lost")

	settings, err := s.GatewaySettings().Get(ctx)
	require.NoError(t, err)
	require.NotNil(t, settings)
	require.True(t, settings.Enabled)
}

// TestConcurrent_WithTxWritersSerialize covers writer-vs-writer contention:
// several transactions at once all commit, none are lost.
func TestConcurrent_WithTxWritersSerialize(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	const writers = 8
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			err := s.WithTx(ctx, func(tx store.Store) error {
				return tx.Agents().Create(ctx, &store.Agent{Name: fmt.Sprintf("agent-%d", w)})
			})
			require.NoError(t, err)
		}(w)
	}
	wg.Wait()

	agents, err := s.Agents().List(ctx)
	require.NoError(t, err)
	require.Len(t, agents, writers)
}
