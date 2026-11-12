package fake_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestWebhooks_CRUDAndRetention mirrors the sqlite webhook tests against the
// in-memory fake: endpoint CRUD with the unique-name and exactly-one-target
// rules, the wholesale verifier replace, and append-path retention eviction.
func TestWebhooks_CRUDAndRetention(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	hooks := s.Webhooks()

	t.Run("save assigns id and get round-trips", func(t *testing.T) {
		ep := &store.WebhookEndpoint{Name: "gitlab", AgentID: "a1", Prompt: "p", Enabled: true}
		require.NoError(t, hooks.SaveEndpoint(ctx, ep))
		require.NotEmpty(t, ep.ID)
		require.False(t, ep.CreatedAt.IsZero())

		got, err := hooks.GetEndpoint(ctx, ep.ID)
		require.NoError(t, err)
		require.Equal(t, "gitlab", got.Name)
		require.Equal(t, "a1", got.AgentID)
		require.True(t, got.Enabled)
	})

	t.Run("absent reads are ErrNotFound", func(t *testing.T) {
		_, err := hooks.GetEndpoint(ctx, "absent")
		require.True(t, errors.Is(err, store.ErrNotFound), "want ErrNotFound, got %v", err)
		err = hooks.DeleteEndpoint(ctx, "absent")
		require.True(t, errors.Is(err, store.ErrNotFound), "want ErrNotFound, got %v", err)
		err = hooks.SaveVerifiers(ctx, "absent", nil)
		require.True(t, errors.Is(err, store.ErrNotFound), "want ErrNotFound, got %v", err)
		err = hooks.AppendDelivery(ctx, &store.WebhookDelivery{EndpointID: "absent"})
		require.True(t, errors.Is(err, store.ErrNotFound), "want ErrNotFound, got %v", err)
		_, err = hooks.ListDeliveries(ctx, "absent")
		require.True(t, errors.Is(err, store.ErrNotFound), "want ErrNotFound, got %v", err)
	})

	t.Run("duplicate name among other endpoints is ErrDuplicate", func(t *testing.T) {
		second := &store.WebhookEndpoint{Name: "gitlab", GroupID: "g1", Prompt: "p"}
		err := hooks.SaveEndpoint(ctx, second)
		require.True(t, errors.Is(err, store.ErrDuplicate), "want ErrDuplicate, got %v", err)
		caseVar := &store.WebhookEndpoint{Name: "GITLAB", GroupID: "g1", Prompt: "p"}
		err = hooks.SaveEndpoint(ctx, caseVar)
		require.True(t, errors.Is(err, store.ErrDuplicate), "case-insensitive: want ErrDuplicate, got %v", err)
	})

	t.Run("same name same endpoint kept", func(t *testing.T) {
		out, err := hooks.ListEndpoints(ctx)
		require.NoError(t, err)
		own := out[0]
		own.Prompt = "still mine"
		require.NoError(t, hooks.SaveEndpoint(ctx, &own))
		got, err := hooks.GetEndpoint(ctx, own.ID)
		require.NoError(t, err)
		require.Equal(t, "still mine", got.Prompt)
	})

	t.Run("target xor enforced", func(t *testing.T) {
		require.Error(t, hooks.SaveEndpoint(ctx, &store.WebhookEndpoint{Name: "x", Prompt: "p"}))
		require.Error(t, hooks.SaveEndpoint(ctx, &store.WebhookEndpoint{
			Name: "y", AgentID: "a", GroupID: "g", Prompt: "p",
		}))
	})

	t.Run("verifier replace stores in slice order", func(t *testing.T) {
		out, err := hooks.ListEndpoints(ctx)
		require.NoError(t, err)
		id := out[0].ID
		require.NoError(t, hooks.SaveVerifiers(ctx, id, []store.WebhookVerifier{
			{Kind: store.WebhookVerifierTsHMAC, Preset: "Stripe", Secret: "whsec_1"},
			{Kind: store.WebhookVerifierPath, Secret: "tok"},
		}))
		got, err := hooks.GetEndpoint(ctx, id)
		require.NoError(t, err)
		require.Len(t, got.Verifiers, 2)
		require.Equal(t, 0, got.Verifiers[0].Position)
		require.Equal(t, "Stripe", got.Verifiers[0].Preset)
		require.Equal(t, 1, got.Verifiers[1].Position)
		require.Equal(t, "tok", got.Verifiers[1].Secret)

		// The fake must never alias internal state.
		got.Verifiers[0].Secret = "tampered"
		again, err := hooks.GetEndpoint(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "whsec_1", again.Verifiers[0].Secret)
	})

	t.Run("delivery retention evicts oldest and stays bounded", func(t *testing.T) {
		out, err := hooks.ListEndpoints(ctx)
		require.NoError(t, err)
		id := out[0].ID

		total := store.WebhookDeliveryRetention + 5
		for i := 0; i < total; i++ {
			at := time.Date(2026, 11, 2, 8, 0, i, 0, time.UTC)
			require.NoError(t, hooks.AppendDelivery(ctx, &store.WebhookDelivery{
				EndpointID: id, AttemptedAt: at, Status: 202,
				Outcome: store.WebhookOutcomeAccepted,
			}))
		}
		log, err := hooks.ListDeliveries(ctx, id)
		require.NoError(t, err)
		require.Len(t, log, store.WebhookDeliveryRetention, "the log stays bounded")
		require.True(t, log[0].AttemptedAt.Equal(time.Date(2026, 11, 2, 8, 0, total-1, 0, time.UTC)),
			"newest first")
		require.True(t, log[len(log)-1].AttemptedAt.Equal(
			time.Date(2026, 11, 2, 8, 0, total-store.WebhookDeliveryRetention, 0, time.UTC)),
			"oldest evicted first")
	})

	t.Run("delivery note round-trips", func(t *testing.T) {
		out, err := hooks.ListEndpoints(ctx)
		require.NoError(t, err)
		id := out[0].ID

		require.NoError(t, hooks.AppendDelivery(ctx, &store.WebhookDelivery{
			EndpointID: id, AttemptedAt: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC),
			Status: 404, Outcome: store.WebhookOutcomeFailed,
			Note: "agent deleted since resolve",
		}))
		log, err := hooks.ListDeliveries(ctx, id)
		require.NoError(t, err)
		require.Equal(t, store.WebhookOutcomeFailed, log[0].Outcome, "newest first")
		require.Equal(t, "agent deleted since resolve", log[0].Note)
		require.Equal(t, 404, log[0].Status)
	})

	t.Run("delete cascades verifiers and deliveries", func(t *testing.T) {
		out, err := hooks.ListEndpoints(ctx)
		require.NoError(t, err)
		id := out[0].ID
		require.NoError(t, hooks.DeleteEndpoint(ctx, id))
		_, err = hooks.GetEndpoint(ctx, id)
		require.True(t, errors.Is(err, store.ErrNotFound))
	})
}

// TestWebhooks_WebhookAddrRidesGatewaySettings covers the fake side of the
// listener-address setting: it round-trips through the existing
// GatewaySettingsStore like every other gateway column.
func TestWebhooks_WebhookAddrRidesGatewaySettings(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	settings, err := s.GatewaySettings().Get(ctx)
	require.NoError(t, err)
	require.Nil(t, settings)

	require.NoError(t, s.GatewaySettings().Save(ctx, &store.GatewaySettings{
		Enabled:     true,
		WebhookAddr: "127.0.0.1:8787",
	}))
	got, err := s.GatewaySettings().Get(ctx)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "127.0.0.1:8787", got.WebhookAddr)
}
