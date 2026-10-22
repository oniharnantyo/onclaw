package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestWithTx_CommitPersistsAllRows covers the success path of an atomic
// multi-row config write: every row lands together.
func TestWithTx_CommitPersistsAllRows(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.WithTx(ctx, func(tx store.Store) error {
		if err := tx.Providers().Create(ctx, &store.Provider{Name: "anthropic"}); err != nil {
			return err
		}
		return tx.Providers().Create(ctx, &store.Provider{Name: "openai"})
	})
	require.NoError(t, err)

	providers, err := s.Providers().List(ctx)
	require.NoError(t, err)
	require.Len(t, providers, 2)
}

// TestWithTx_ErrorRollsBackAllRows covers task 3.4's atomicity requirement:
// a config write that fails part-way leaves NO partial rows — the earlier
// inserts of the transaction are rolled back, not just the failing one.
func TestWithTx_ErrorRollsBackAllRows(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	sentinel := errors.New("second save exploded")
	err := s.WithTx(ctx, func(tx store.Store) error {
		if err := tx.Providers().Create(ctx, &store.Provider{Name: "partial-a"}); err != nil {
			return err
		}
		if err := tx.Providers().Create(ctx, &store.Provider{Name: "partial-b"}); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	providers, err := s.Providers().List(ctx)
	require.NoError(t, err)
	require.Empty(t, providers, "an aborted config write must leave no partial rows")
}

// TestWithTx_ConstraintFailureRollsBackEarlierRows covers the same guarantee
// when the failure comes from the database itself (unique constraint) rather
// than a returned error: rows inserted before the violation do not survive.
func TestWithTx_ConstraintFailureRollsBackEarlierRows(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.WithTx(ctx, func(tx store.Store) error {
		if err := tx.Providers().Create(ctx, &store.Provider{Name: "dup"}); err != nil {
			return err
		}
		// Same name: violates uq_providers_name mid-transaction.
		return tx.Providers().Create(ctx, &store.Provider{Name: "dup"})
	})
	require.Error(t, err)

	providers, err := s.Providers().List(ctx)
	require.NoError(t, err)
	require.Empty(t, providers, "a constraint violation must roll back earlier inserts")

	// The store stays usable after the rolled-back transaction.
	require.NoError(t, s.Providers().Create(ctx, &store.Provider{Name: "fresh"}))
	providers, err = s.Providers().List(ctx)
	require.NoError(t, err)
	require.Len(t, providers, 1)
}

// TestWithTx_SessionEventsJoinTheTransaction verifies that session event
// appends routed through the transactional store commit and roll back with
// the surrounding config change — one atomic turn.
func TestWithTx_SessionEventsJoinTheTransaction(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	sentinel := errors.New("turn failed")
	err := s.WithTx(ctx, func(tx store.Store) error {
		if err := tx.SessionEvents().AppendEvents(ctx, "sess-tx", eventBatch("t1", "t2")); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	events, err := s.SessionEvents().LoadEvents(ctx, "sess-tx")
	require.NoError(t, err)
	require.Empty(t, events, "aborted turn events must roll back with the transaction")

	require.NoError(t, s.WithTx(ctx, func(tx store.Store) error {
		return tx.SessionEvents().AppendEvents(ctx, "sess-tx", eventBatch("t3"))
	}))
	events, err = s.SessionEvents().LoadEvents(ctx, "sess-tx")
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int64(1), events[0].Seq, "rolled-back sequences must not leak into the next transaction")
}

// TestGatewaySettings_SaveOverridesSingleton covers the config-write path
// used by the settings pane: repeated saves replace the single row.
func TestGatewaySettings_SaveOverridesSingleton(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	settings, err := s.GatewaySettings().Get(ctx)
	require.NoError(t, err)
	require.Nil(t, settings)

	require.NoError(t, s.GatewaySettings().Save(ctx, &store.GatewaySettings{Enabled: true, DefaultAgentID: "a1"}))
	require.NoError(t, s.GatewaySettings().Save(ctx, &store.GatewaySettings{Enabled: false, DefaultAgentID: "a2"}))

	settings, err = s.GatewaySettings().Get(ctx)
	require.NoError(t, err)
	require.NotNil(t, settings)
	require.False(t, settings.Enabled)
	require.Equal(t, "a2", settings.DefaultAgentID)
}
