package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestWebhookEndpoints_RoundTrip covers the endpoint contract the webhook
// service relies on: the store assigns the id and stamps the timestamps on
// insert, a present-id save rewrites the definition in place keeping the
// original CreatedAt, reads come back with the stored verifier list, and
// delete removes the row with the not-found path surfaced after.
func TestWebhookEndpoints_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	hooks := s.Webhooks()

	var createdAtZero bool
	var id string
	t.Run("save assigns id and stamps timestamps", func(t *testing.T) {
		ep := &store.WebhookEndpoint{
			Name:    "gitlab",
			AgentID: "agent-1",
			Prompt:  "Summarize: {{payload}}",
			Enabled: true,
		}
		require.NoError(t, hooks.SaveEndpoint(ctx, ep))
		require.NotEmpty(t, ep.ID, "the store assigns the id")
		require.False(t, ep.CreatedAt.IsZero(), "the store stamps CreatedAt")
		require.False(t, ep.UpdatedAt.IsZero(), "the store stamps UpdatedAt")
		id = ep.ID
		createdAtZero = false
	})
	require.False(t, createdAtZero)

	t.Run("get returns the saved endpoint", func(t *testing.T) {
		ep, err := hooks.GetEndpoint(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "gitlab", ep.Name)
		require.Equal(t, "agent-1", ep.AgentID)
		require.Equal(t, "", ep.GroupID, "the unset target reads back empty")
		require.True(t, ep.Enabled)
	})

	t.Run("update keeps CreatedAt and rewrites the definition", func(t *testing.T) {
		before, err := hooks.GetEndpoint(ctx, id)
		require.NoError(t, err)

		ep := &store.WebhookEndpoint{
			ID:      id,
			Name:    "gitlab",
			GroupID: "group-9",
			Prompt:  "changed",
			Enabled: false,
		}
		require.NoError(t, hooks.SaveEndpoint(ctx, ep))
		got, err := hooks.GetEndpoint(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "group-9", got.GroupID, "the target may move between kinds")
		require.Equal(t, "", got.AgentID)
		require.False(t, got.Enabled)
		require.True(t, got.CreatedAt.Equal(before.CreatedAt), "CreatedAt survives the update")
		require.True(t, got.UpdatedAt.After(before.UpdatedAt), "UpdatedAt re-stamps")
	})

	t.Run("list orders by name case-insensitively", func(t *testing.T) {
		require.NoError(t, hooks.SaveEndpoint(ctx, &store.WebhookEndpoint{
			Name: "Alerts", GroupID: "group-1", Prompt: "p",
		}))
		out, err := hooks.ListEndpoints(ctx)
		require.NoError(t, err)
		require.Len(t, out, 2)
		require.Equal(t, "Alerts", out[0].Name, "NOCASE: alerts < gitlab")
		require.Equal(t, "gitlab", out[1].Name)
	})

	t.Run("delete removes the row", func(t *testing.T) {
		require.NoError(t, hooks.DeleteEndpoint(ctx, id))
		_, err := hooks.GetEndpoint(ctx, id)
		require.True(t, errors.Is(err, store.ErrNotFound), "want ErrNotFound, got %v", err)
		_, err = hooks.GetEndpoint(ctx, "absent")
		require.True(t, errors.Is(err, store.ErrNotFound), "want ErrNotFound, got %v", err)
	})
}

// TestWebhookEndpoints_UniqueName covers the NOCASE unique index: a
// duplicate among other endpoints is the ErrDuplicate sentinel; keeping the
// endpoint's own name never conflicts.
func TestWebhookEndpoints_UniqueName(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	hooks := s.Webhooks()

	first := &store.WebhookEndpoint{Name: "hooks", GroupID: "g1", Prompt: "p"}
	require.NoError(t, hooks.SaveEndpoint(ctx, first))

	t.Run("same name other endpoint rejected", func(t *testing.T) {
		second := &store.WebhookEndpoint{Name: "hooks", AgentID: "a1", Prompt: "p"}
		err := hooks.SaveEndpoint(ctx, second)
		require.True(t, errors.Is(err, store.ErrDuplicate), "want ErrDuplicate, got %v", err)
	})

	t.Run("case-insensitive collision rejected", func(t *testing.T) {
		second := &store.WebhookEndpoint{Name: "HOOKS", AgentID: "a1", Prompt: "p"}
		err := hooks.SaveEndpoint(ctx, second)
		require.True(t, errors.Is(err, store.ErrDuplicate), "want ErrDuplicate, got %v", err)
	})

	t.Run("same name same endpoint kept", func(t *testing.T) {
		first.Prompt = "still mine"
		require.NoError(t, hooks.SaveEndpoint(ctx, first), "keeping its own name must not conflict")
		got, err := hooks.GetEndpoint(ctx, first.ID)
		require.NoError(t, err)
		require.Equal(t, "still mine", got.Prompt)
	})
}

// TestWebhookEndpoints_TargetXor covers the exactly-one-target rule: rows
// setting neither or both agent id and group id are rejected.
func TestWebhookEndpoints_TargetXor(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	hooks := s.Webhooks()

	t.Run("neither target set", func(t *testing.T) {
		err := hooks.SaveEndpoint(ctx, &store.WebhookEndpoint{Name: "x", Prompt: "p"})
		require.Error(t, err)
	})
	t.Run("both targets set", func(t *testing.T) {
		err := hooks.SaveEndpoint(ctx, &store.WebhookEndpoint{
			Name: "y", AgentID: "a", GroupID: "g", Prompt: "p",
		})
		require.Error(t, err)
	})
	t.Run("update path rejected too", func(t *testing.T) {
		ep := &store.WebhookEndpoint{Name: "ok", AgentID: "a", Prompt: "p"}
		require.NoError(t, hooks.SaveEndpoint(ctx, ep))
		ep.GroupID = "g"
		require.Error(t, hooks.SaveEndpoint(ctx, ep))
	})
}

// TestWebhookVerifiers_SaveAndRead covers the ordered child list: the save
// replaces the whole list with store-assigned ids and slice positions, and
// Get/List read them back in order.
func TestWebhookVerifiers_SaveAndRead(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	hooks := s.Webhooks()

	ep := &store.WebhookEndpoint{Name: "stripe", AgentID: "a1", Prompt: "p"}
	require.NoError(t, hooks.SaveEndpoint(ctx, ep))

	t.Run("save on absent endpoint is ErrNotFound", func(t *testing.T) {
		err := hooks.SaveVerifiers(ctx, "absent", []store.WebhookVerifier{
			{Kind: store.WebhookVerifierPath, Secret: "tok"},
		})
		require.True(t, errors.Is(err, store.ErrNotFound), "want ErrNotFound, got %v", err)
	})

	t.Run("two verifiers stored in slice order", func(t *testing.T) {
		require.NoError(t, hooks.SaveVerifiers(ctx, ep.ID, []store.WebhookVerifier{
			{
				Kind:       store.WebhookVerifierTsHMAC,
				Preset:     "Stripe",
				Scheme:     "stripe",
				HeaderName: "stripe-signature",
				Algo:       "sha256",
				Encoding:   "hex",
				Secret:     "whsec_12345678",
			},
			{
				Kind:   store.WebhookVerifierPath,
				Secret: "pat-token",
			},
		}))
		got, err := hooks.GetEndpoint(ctx, ep.ID)
		require.NoError(t, err)
		require.Len(t, got.Verifiers, 2)
		require.Equal(t, store.WebhookVerifierTsHMAC, got.Verifiers[0].Kind)
		require.Equal(t, 0, got.Verifiers[0].Position)
		require.Equal(t, "Stripe", got.Verifiers[0].Preset)
		require.Equal(t, "stripe-signature", got.Verifiers[0].HeaderName)
		require.Equal(t, "whsec_12345678", got.Verifiers[0].Secret, "the store keeps secret material")
		require.NotEmpty(t, got.Verifiers[0].ID, "the store assigns verifier ids")
		require.Equal(t, ep.ID, got.Verifiers[0].EndpointID)
		require.False(t, got.Verifiers[0].CreatedAt.IsZero())
		require.Equal(t, store.WebhookVerifierPath, got.Verifiers[1].Kind)
		require.Equal(t, 1, got.Verifiers[1].Position)
		require.Equal(t, "pat-token", got.Verifiers[1].Secret)
	})

	t.Run("re-save replaces the list wholesale", func(t *testing.T) {
		require.NoError(t, hooks.SaveVerifiers(ctx, ep.ID, []store.WebhookVerifier{
			{Kind: store.WebhookVerifierHeader, HeaderName: "x-api-key", Secret: "static"},
		}))
		got, err := hooks.GetEndpoint(ctx, ep.ID)
		require.NoError(t, err)
		require.Len(t, got.Verifiers, 1, "earlier rows — secrets included — are dropped")
		require.Equal(t, store.WebhookVerifierHeader, got.Verifiers[0].Kind)
	})

	t.Run("empty list clears", func(t *testing.T) {
		require.NoError(t, hooks.SaveVerifiers(ctx, ep.ID, nil))
		got, err := hooks.GetEndpoint(ctx, ep.ID)
		require.NoError(t, err)
		require.Empty(t, got.Verifiers)
	})

	t.Run("list carries the verifiers too", func(t *testing.T) {
		require.NoError(t, hooks.SaveVerifiers(ctx, ep.ID, []store.WebhookVerifier{
			{Kind: store.WebhookVerifierRawHMAC, Preset: "GitHub", Prefix: "sha256="},
		}))
		out, err := hooks.ListEndpoints(ctx)
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Len(t, out[0].Verifiers, 1)
		require.Equal(t, "GitHub", out[0].Verifiers[0].Preset)
	})
}

// TestWebhookEndpoints_DeleteCascades covers the FK cascade: deleting the
// endpoint removes its verifiers and delivery log rows.
func TestWebhookEndpoints_DeleteCascades(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	hooks := s.Webhooks()

	ep := &store.WebhookEndpoint{Name: "gitlab", AgentID: "a1", Prompt: "p"}
	require.NoError(t, hooks.SaveEndpoint(ctx, ep))
	require.NoError(t, hooks.SaveVerifiers(ctx, ep.ID, []store.WebhookVerifier{
		{Kind: store.WebhookVerifierStdWeb, Secret: "whsec_x"},
	}))
	require.NoError(t, hooks.AppendDelivery(ctx, &store.WebhookDelivery{
		EndpointID: ep.ID, Status: 202, Outcome: store.WebhookOutcomeAccepted,
	}))

	require.NoError(t, hooks.DeleteEndpoint(ctx, ep.ID))
	require.Zero(t, countRows(t, s.db, "webhook_verifiers"), "verifiers cascade")
	require.Zero(t, countRows(t, s.db, "webhook_deliveries"), "deliveries cascade")

	// The aggregate is usable again immediately after the delete.
	ep2 := &store.WebhookEndpoint{Name: "gitlab", AgentID: "a1", Prompt: "p"}
	require.NoError(t, hooks.SaveEndpoint(ctx, ep2))
	require.NoError(t, hooks.AppendDelivery(ctx, &store.WebhookDelivery{
		EndpointID: ep2.ID, Status: 202, Outcome: store.WebhookOutcomeAccepted,
	}))
}

// TestWebhooks_MigrateForwardFromV22 covers task 1.2's upgrade path: a
// database at the previous schema version (v22) migrates to v23 on open,
// creating the three webhook tables and the gateway_settings.webhook_addr
// column without touching existing rows.
func TestWebhooks_MigrateForwardFromV22(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/v22.db"

	// Build a database at schema v22: every embedded migration but 000023.
	var upToV22 []migration
	for _, m := range mustEmbeddedMigrations(t) {
		if m.Version <= 22 {
			upToV22 = append(upToV22, m)
		}
	}
	require.Len(t, upToV22, 22)

	st22, err := openWith(ctx, path, upToV22)
	require.NoError(t, err)
	agent := &store.Agent{Name: "survivor", Slug: "survivor"}
	require.NoError(t, st22.Agents().Create(ctx, agent))
	require.NoError(t, st22.Close())

	// Seed a pre-existing gateway settings row at the v22 column shape (the
	// wired store accessor already writes the v23 webhook_addr column, which
	// does not exist in this fixture).
	db := openRaw(t, path)
	_, err = db.ExecContext(ctx, `
INSERT INTO gateway_settings (id, enabled, default_agent_id, updated_at)
VALUES (1, 1, ?, ?)`, agent.ID, formatTime(nowUTC()))
	require.NoError(t, err)

	for _, table := range []string{"webhook_endpoints", "webhook_verifiers", "webhook_deliveries"} {
		require.False(t, tableExists(t, db, table), "a v22 database must not hold %s yet", table)
	}
	hasAddr, err := columnExists(ctx, db, "gateway_settings", "webhook_addr")
	require.NoError(t, err)
	require.False(t, hasAddr, "a v22 gateway_settings must not hold webhook_addr yet")

	// Opening with the full embedded list applies 000023 and keeps the data.
	st, err := Open(ctx, path)
	require.NoError(t, err, "a v22 database must migrate on open")
	t.Cleanup(func() { _ = st.Close() })

	var version int
	require.NoError(t, db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version))
	require.Equal(t, 23, version, "migration 000023 must be recorded as applied")

	for _, table := range []string{"webhook_endpoints", "webhook_verifiers", "webhook_deliveries"} {
		require.True(t, tableExists(t, db, table), "table %s must exist after the upgrade", table)
	}
	hasAddr, err = columnExists(ctx, db, "gateway_settings", "webhook_addr")
	require.NoError(t, err)
	require.True(t, hasAddr, "000023 must add gateway_settings.webhook_addr")

	got, err := st.Agents().ByID(ctx, agent.ID)
	require.NoError(t, err, "existing rows must survive the v23 migration")
	require.Equal(t, "survivor", got.Name)
	settings, err := st.GatewaySettings().Get(ctx)
	require.NoError(t, err)
	require.NotNil(t, settings)
	require.True(t, settings.Enabled, "pre-existing gateway settings survive the migration")
	require.Equal(t, agent.ID, settings.DefaultAgentID)
	require.Equal(t, "", settings.WebhookAddr, "the column default reads back empty")

	// The new aggregate is usable right after the upgrade.
	hooks := st.Webhooks()
	ep := &store.WebhookEndpoint{Name: "gitlab", AgentID: agent.ID, Prompt: "p"}
	require.NoError(t, hooks.SaveEndpoint(ctx, ep))
	require.NoError(t, hooks.SaveVerifiers(ctx, ep.ID, []store.WebhookVerifier{
		{Kind: store.WebhookVerifierStdWeb, Preset: "GitLab", Secret: "whsec_s"},
	}))
	gotEP, err := hooks.GetEndpoint(ctx, ep.ID)
	require.NoError(t, err)
	require.Len(t, gotEP.Verifiers, 1)
}

// TestWebhooks_VisibleThroughTx covers the tx-scoped webhook store: writes
// inside WithTx commit with the transaction.
func TestWebhooks_VisibleThroughTx(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	var epID string
	err := s.WithTx(ctx, func(tx store.Store) error {
		ep := &store.WebhookEndpoint{Name: "tx-hook", AgentID: "a1", Prompt: "p"}
		if err := tx.Webhooks().SaveEndpoint(ctx, ep); err != nil {
			return err
		}
		epID = ep.ID
		return tx.Webhooks().SaveVerifiers(ctx, ep.ID, []store.WebhookVerifier{
			{Kind: store.WebhookVerifierPath, Secret: "tok"},
		})
	})
	require.NoError(t, err)

	got, err := s.Webhooks().GetEndpoint(ctx, epID)
	require.NoError(t, err)
	require.Equal(t, "tx-hook", got.Name)
	require.Len(t, got.Verifiers, 1)
}
