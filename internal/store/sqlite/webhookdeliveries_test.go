package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestWebhookDeliveries_AppendAndList covers the delivery-log contract: the
// store assigns the id, stamps zero timestamps, and List reads the endpoint's
// log newest-first with every column round-tripped.
func TestWebhookDeliveries_AppendAndList(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	hooks := s.Webhooks()

	ep := &store.WebhookEndpoint{Name: "gitlab", AgentID: "a1", Prompt: "p"}
	require.NoError(t, hooks.SaveEndpoint(ctx, ep))

	t.Run("append on absent endpoint is ErrNotFound", func(t *testing.T) {
		err := hooks.AppendDelivery(ctx, &store.WebhookDelivery{EndpointID: "absent"})
		require.True(t, errors.Is(err, store.ErrNotFound), "want ErrNotFound, got %v", err)
	})
	t.Run("list on absent endpoint is ErrNotFound", func(t *testing.T) {
		_, err := hooks.ListDeliveries(ctx, "absent")
		require.True(t, errors.Is(err, store.ErrNotFound), "want ErrNotFound, got %v", err)
	})

	t.Run("append stamps and list returns newest-first", func(t *testing.T) {
		first := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
		second := time.Date(2026, 11, 1, 9, 1, 0, 0, time.UTC)
		require.NoError(t, hooks.AppendDelivery(ctx, &store.WebhookDelivery{
			EndpointID: ep.ID, AttemptedAt: first, Status: 202,
			Outcome: store.WebhookOutcomeAccepted, SessionID: "sess-1",
			Via: "GitLab", SenderDeliveryID: "msg_1",
		}))
		require.NoError(t, hooks.AppendDelivery(ctx, &store.WebhookDelivery{
			EndpointID: ep.ID, AttemptedAt: second, Status: 200,
			Outcome: store.WebhookOutcomeDuplicate, SenderDeliveryID: "msg_1",
		}))

		out, err := hooks.ListDeliveries(ctx, ep.ID)
		require.NoError(t, err)
		require.Len(t, out, 2)
		require.True(t, out[0].AttemptedAt.Equal(second), "newest first")
		require.Equal(t, 200, out[0].Status)
		require.Equal(t, store.WebhookOutcomeDuplicate, out[0].Outcome)
		require.True(t, out[1].AttemptedAt.Equal(first))
		require.Equal(t, "sess-1", out[1].SessionID)
		require.Equal(t, "GitLab", out[1].Via)
		require.Equal(t, "msg_1", out[1].SenderDeliveryID)
		require.Equal(t, "", out[1].Note, "an accepted delivery carries no note")
		require.False(t, out[1].CreatedAt.IsZero(), "the store stamps CreatedAt")
		require.NotEmpty(t, out[1].ID, "the store assigns the id")
	})

	t.Run("failed delivery round-trips the human-readable note", func(t *testing.T) {
		at := time.Date(2026, 11, 1, 9, 2, 0, 0, time.UTC)
		require.NoError(t, hooks.AppendDelivery(ctx, &store.WebhookDelivery{
			EndpointID: ep.ID, AttemptedAt: at, Status: 404,
			Outcome: store.WebhookOutcomeFailed,
			Note:    "agent deleted since resolve",
		}))

		out, err := hooks.ListDeliveries(ctx, ep.ID)
		require.NoError(t, err)
		require.Len(t, out, 3)
		require.Equal(t, store.WebhookOutcomeFailed, out[0].Outcome, "newest first")
		require.Equal(t, "agent deleted since resolve", out[0].Note)
		require.Equal(t, "", out[1].Note, "rows without a note read back empty")
	})
}

// TestWebhookDeliveries_Retention covers task 1.3: appending past the
// per-endpoint bound (store.WebhookDeliveryRetention) evicts the oldest rows
// first, and the bound is per endpoint — other endpoints' logs are untouched.
func TestWebhookDeliveries_Retention(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	hooks := s.Webhooks()

	epA := &store.WebhookEndpoint{Name: "a", AgentID: "a1", Prompt: "p"}
	epB := &store.WebhookEndpoint{Name: "b", AgentID: "a1", Prompt: "p"}
	require.NoError(t, hooks.SaveEndpoint(ctx, epA))
	require.NoError(t, hooks.SaveEndpoint(ctx, epB))

	// Insert past the bound with strictly increasing attempt times.
	total := store.WebhookDeliveryRetention + 10
	for i := 0; i < total; i++ {
		at := time.Date(2026, 11, 1, 8, 0, i, 0, time.UTC)
		require.NoError(t, hooks.AppendDelivery(ctx, &store.WebhookDelivery{
			EndpointID: epA.ID, AttemptedAt: at, Status: 202,
			Outcome: store.WebhookOutcomeAccepted,
		}))
	}
	// A concurrent-looking burst with identical attempt times must not
	// wedge the bound either.
	for i := 0; i < 3; i++ {
		at := time.Date(2026, 11, 1, 8, 0, total-1, 0, time.UTC)
		require.NoError(t, hooks.AppendDelivery(ctx, &store.WebhookDelivery{
			EndpointID: epA.ID, AttemptedAt: at, Status: 200,
			Outcome: store.WebhookOutcomeDuplicate,
		}))
	}
	require.NoError(t, hooks.AppendDelivery(ctx, &store.WebhookDelivery{
		EndpointID: epB.ID, AttemptedAt: time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC),
		Status: 202, Outcome: store.WebhookOutcomeAccepted,
	}))

	t.Run("bounded count", func(t *testing.T) {
		require.Equal(t, store.WebhookDeliveryRetention, countRows(t, s.db, "webhook_deliveries")-1,
			"endpoint A's log stays at the bound (endpoint B holds one row)")
	})

	t.Run("eviction order oldest-first", func(t *testing.T) {
		out, err := hooks.ListDeliveries(ctx, epA.ID)
		require.NoError(t, err)
		require.Len(t, out, store.WebhookDeliveryRetention)

		// total + 3 appends (0-indexed 0..total+2), bound R: the last R
		// inserts survive. The oldest kept insert is index total+3-R, whose
		// attempt slot is the same number.
		keptFrom := total + 3 - store.WebhookDeliveryRetention
		oldestKept := time.Date(2026, 11, 1, 8, 0, keptFrom, 0, time.UTC)
		require.True(t, out[len(out)-1].AttemptedAt.Equal(oldestKept),
			"the oldest rows are evicted first: oldest kept = slot %d", keptFrom)
		newestKept := time.Date(2026, 11, 1, 8, 0, total-1, 0, time.UTC)
		require.True(t, out[0].AttemptedAt.Equal(newestKept), "the newest rows survive")
		require.Equal(t, store.WebhookOutcomeDuplicate, out[0].Outcome,
			"same-slot later inserts count as newer (created_at breaks ties)")
	})

	t.Run("bound is per endpoint", func(t *testing.T) {
		out, err := hooks.ListDeliveries(ctx, epB.ID)
		require.NoError(t, err)
		require.Len(t, out, 1, "endpoint B's log is untouched by A's evictions")
	})
}

// TestWebhookDeliveries_AtBound covers the boundary: exactly the bound rows
// evict nothing; the next append evicts exactly one.
func TestWebhookDeliveries_AtBound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	hooks := s.Webhooks()

	ep := &store.WebhookEndpoint{Name: "gitlab", AgentID: "a1", Prompt: "p"}
	require.NoError(t, hooks.SaveEndpoint(ctx, ep))

	for i := 0; i < store.WebhookDeliveryRetention; i++ {
		at := time.Date(2026, 11, 1, 8, 0, i, 0, time.UTC)
		require.NoError(t, hooks.AppendDelivery(ctx, &store.WebhookDelivery{
			EndpointID: ep.ID, AttemptedAt: at, Status: 202,
			Outcome: store.WebhookOutcomeAccepted,
		}))
	}
	require.Equal(t, store.WebhookDeliveryRetention, countRows(t, s.db, "webhook_deliveries"),
		"at the bound nothing is evicted")

	require.NoError(t, hooks.AppendDelivery(ctx, &store.WebhookDelivery{
		EndpointID: ep.ID, AttemptedAt: time.Date(2026, 11, 1, 8, 0, 99, 0, time.UTC),
		Status: 202, Outcome: store.WebhookOutcomeAccepted,
	}))
	require.Equal(t, store.WebhookDeliveryRetention, countRows(t, s.db, "webhook_deliveries"),
		"one append past the bound evicts exactly one row")
}
