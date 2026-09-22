//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// pgCWSeedConnection creates a workspace plus a github connection for webhook
// store tests (integration twin of the fake helper).
func pgCWSeedConnection(t *testing.T, ctx context.Context, s store.Store, slug string) (*domain.Workspace, *domain.Connection) {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "PG CW WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}
	c := &domain.Connection{WorkspaceID: ws.ID, Service: "github", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := s.Connections().Create(ctx, c); err != nil {
		t.Fatalf("unexpected create connection error: %v", err)
	}
	return ws, c
}

func pgCWValidState(ws *domain.Workspace, c *domain.Connection) *domain.ConnectionWebhook {
	return &domain.ConnectionWebhook{
		WorkspaceID:      ws.ID,
		ConnectionID:     c.ID,
		Enabled:          true,
		SecretCiphertext: "v1:bm9uY2U:Y2lwaGVydGV4dA",
		SecretHint:       "bGd9",
		TargetAgentID:    uuid.NewString(),
		TargetKind:       domain.ConnectionWebhookTargetChannel,
		TargetID:         "chan-incidents",
		Events:           []string{"pull_request.opened", "push"},
	}
}

// Webhook state on the connection (add-connection-webhooks tasks.md 1.4):
// inert default read, full-state round trip, updated_at bump, disable-keeps-
// state, and the scope guards — mirroring the fake's semantics.
func TestIntegration_ConnectionWebhookStore_State(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws, c := pgCWSeedConnection(t, ctx, s, "pg-cw-state")
	ws2, _ := pgCWSeedConnection(t, ctx, s, "pg-cw-state-other")

	// 1. Fresh connection reads the inert default (columns' defaults).
	got, err := s.ConnectionWebhooks().GetState(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get-state error: %v", err)
	}
	if got.Enabled || got.SecretCiphertext != "" || got.TargetKind != "" || got.TargetID != "" {
		t.Fatalf("expected the inert default, got %+v", got)
	}
	if got.WorkspaceID != ws.ID || got.ConnectionID != c.ID || got.Events == nil || len(got.Events) != 0 {
		t.Fatalf("expected scope + empty event array, got %+v", got)
	}

	// 2. UpdateState persists the full state and bumps the connection's
	// updated_at.
	before, err := s.Connections().Get(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	state := pgCWValidState(ws, c)
	if err := s.ConnectionWebhooks().UpdateState(ctx, state); err != nil {
		t.Fatalf("unexpected update-state error: %v", err)
	}
	after, err := s.Connections().Get(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("expected updated_at bump, before=%v after=%v", before.UpdatedAt, after.UpdatedAt)
	}

	got, err = s.ConnectionWebhooks().GetState(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get-state error: %v", err)
	}
	if !got.Enabled || got.SecretCiphertext != state.SecretCiphertext || got.SecretHint != state.SecretHint {
		t.Fatalf("expected the enabled state to roundtrip, got %+v", got)
	}
	if got.TargetAgentID != state.TargetAgentID || got.TargetKind != state.TargetKind || got.TargetID != state.TargetID {
		t.Fatalf("expected the binding to roundtrip, got %+v", got)
	}
	if !slices.Equal(got.Events, state.Events) {
		t.Fatalf("expected the selection to roundtrip, got %v", got.Events)
	}

	// 3. A cleared target binding roundtrips to the empty shape (NULL agent
	// id, empty kind and target).
	cleared := got
	cleared.TargetKind = ""
	cleared.TargetAgentID = ""
	cleared.TargetID = ""
	cleared.Enabled = false
	if err := s.ConnectionWebhooks().UpdateState(ctx, cleared); err != nil {
		t.Fatalf("unexpected clear error: %v", err)
	}
	got, err = s.ConnectionWebhooks().GetState(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get-state error: %v", err)
	}
	if got.TargetAgentID != "" || got.TargetKind != "" || got.TargetID != "" {
		t.Fatalf("expected the cleared binding to read back empty, got %+v", got)
	}

	// 3b. The last-error residue (migration 000065, task 2.4) round-trips
	// through the nullable column: NULL reads as the empty string and a set
	// residue survives the round trip.
	if got.LastError != "" {
		t.Fatalf("expected the inert residue to read back empty, got %q", got.LastError)
	}
	got.LastError = `{"event":"push","error":"render failed","at":"2026-09-23T10:30:00Z"}`
	if err := s.ConnectionWebhooks().UpdateState(ctx, got); err != nil {
		t.Fatalf("unexpected last-error update error: %v", err)
	}
	got, err = s.ConnectionWebhooks().GetState(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get-state error: %v", err)
	}
	if got.LastError == "" || !strings.Contains(got.LastError, "render failed") {
		t.Fatalf("expected the residue to round-trip, got %q", got.LastError)
	}
	got.LastError = ""
	if err := s.ConnectionWebhooks().UpdateState(ctx, got); err != nil {
		t.Fatalf("unexpected last-error clear error: %v", err)
	}
	got, err = s.ConnectionWebhooks().GetState(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get-state error: %v", err)
	}
	if got.LastError != "" {
		t.Fatalf("expected the cleared residue to store as NULL and read back empty, got %q", got.LastError)
	}

	// 4. Scope and validation guards mirror the fake.
	if _, err := s.ConnectionWebhooks().GetState(ctx, ws.ID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown connection, got %v", err)
	}
	if _, err := s.ConnectionWebhooks().GetState(ctx, ws2.ID, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant get-state, got %v", err)
	}
	if _, err := s.ConnectionWebhooks().GetState(ctx, "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty scope, got %v", err)
	}
	foreign := pgCWValidState(ws2, c)
	if err := s.ConnectionWebhooks().UpdateState(ctx, foreign); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant update, got %v", err)
	}
	invalid := pgCWValidState(ws, c)
	invalid.TargetKind = "dm"
	if err := s.ConnectionWebhooks().UpdateState(ctx, invalid); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for out-of-catalog target kind, got %v", err)
	}
	if err := s.ConnectionWebhooks().UpdateState(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil state, got %v", err)
	}
}

// Delivery dedupe and the pruned window (design.md D3): ack-after-persist —
// first sighting accepted, replay rejected without error, uniqueness per
// connection, prune forgets only the old window.
func TestIntegration_ConnectionWebhookStore_Dedupe(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws, c := pgCWSeedConnection(t, ctx, s, "pg-cw-dedupe")
	c2 := &domain.Connection{WorkspaceID: ws.ID, Service: "gitlab", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := s.Connections().Create(ctx, c2); err != nil {
		t.Fatalf("unexpected create second connection error: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)

	// 1. Accept then replay-reject.
	accepted, err := s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, c.ID, "delivery-1", now)
	if err != nil || !accepted {
		t.Fatalf("expected delivery-1 accepted, got accepted=%v err=%v", accepted, err)
	}
	accepted, err = s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, c.ID, "delivery-1", now)
	if err != nil || accepted {
		t.Fatalf("expected delivery-1 replay rejected, got accepted=%v err=%v", accepted, err)
	}

	// 2. Uniqueness is per connection.
	accepted, err = s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, c2.ID, "delivery-1", now)
	if err != nil || !accepted {
		t.Fatalf("expected per-connection uniqueness, got accepted=%v err=%v", accepted, err)
	}

	// 3. Guards.
	if _, err := s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, c.ID, "", now); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty delivery id, got %v", err)
	}
	if _, err := s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, uuid.NewString(), "d", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown connection, got %v", err)
	}

	// 4. Prune: old rows die, new rows survive, pruned ids are first
	// sightings again.
	old := now.Add(-domain.ConnectionWebhookDeliveryWindow - time.Hour)
	accepted, err = s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, c.ID, "delivery-old", old)
	if err != nil || !accepted {
		t.Fatalf("expected delivery-old accepted, got accepted=%v err=%v", accepted, err)
	}
	pruned, err := s.ConnectionWebhooks().PruneDeliveries(ctx, now.Add(-domain.ConnectionWebhookDeliveryWindow))
	if err != nil {
		t.Fatalf("unexpected prune error: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("expected exactly 1 pruned delivery, got %d", pruned)
	}
	accepted, err = s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, c.ID, "delivery-1", now)
	if err != nil || accepted {
		t.Fatalf("expected delivery-1 to survive the prune and stay deduped, got accepted=%v err=%v", accepted, err)
	}
	accepted, err = s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, c.ID, "delivery-old", now)
	if err != nil || !accepted {
		t.Fatalf("expected the pruned id to be accepted again, got accepted=%v err=%v", accepted, err)
	}
	pruned, err = s.ConnectionWebhooks().PruneDeliveries(ctx, now)
	if err != nil || pruned != 0 {
		t.Fatalf("expected no-op prune, got pruned=%d err=%v", pruned, err)
	}
}

// TestIntegration_ConnectionWebhookSchema covers migration 000064
// (add-connection-webhooks tasks.md 1.3): the webhook columns exist on
// workspace_connections with their defaults and target-kind CHECK, and the
// delivery-dedupe table is unique per connection with the created_at index
// the prune rides. Down-migration behavior is exercised by the idempotence/
// rollback migration test's Down(1) through 000063.
func TestIntegration_ConnectionWebhookSchema(t *testing.T) {
	_, schemaDSN, ctx := setupTestSchema(t)

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer conn.Close(ctx)

	// The webhook columns. webhook_target_agent_id and webhook_last_error
	// are the nullable columns (unset binding until the first enablement;
	// residue absent until the first fail-closed render drop).
	wantColumns := []struct {
		name    string
		notNull bool
	}{
		{"webhook_enabled", true},
		{"webhook_secret_ciphertext", true},
		{"webhook_secret_hint", true},
		{"webhook_target_agent_id", false},
		{"webhook_target_kind", true},
		{"webhook_target_id", true},
		{"webhook_events", true},
		{"webhook_last_error", false},
	}
	rows, err := conn.Query(ctx, `
		SELECT attname, attnotnull FROM pg_attribute
		WHERE attrelid = 'workspace_connections'::regclass AND attname = ANY($1)
	`, []string{"webhook_enabled", "webhook_secret_ciphertext", "webhook_secret_hint",
		"webhook_target_agent_id", "webhook_target_kind", "webhook_target_id", "webhook_events",
		"webhook_last_error"})
	if err != nil {
		t.Fatalf("failed to read webhook columns: %v", err)
	}
	defer rows.Close()
	seen := make(map[string]bool, len(wantColumns))
	for rows.Next() {
		var name string
		var notNull bool
		if err := rows.Scan(&name, &notNull); err != nil {
			t.Fatalf("failed to scan column: %v", err)
		}
		seen[name] = true
		for _, want := range wantColumns {
			if want.name != name {
				continue
			}
			if notNull != want.notNull {
				t.Errorf("expected %q notNull=%v, got %v", name, want.notNull, notNull)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to iterate columns: %v", err)
	}
	for _, want := range wantColumns {
		if !seen[want.name] {
			t.Errorf("expected workspace_connections.%s to exist", want.name)
		}
	}

	// The target-kind catalog CHECK.
	var check string
	err = conn.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid = 'workspace_connections'::regclass
		  AND contype = 'c' AND pg_get_constraintdef(oid) LIKE '%webhook_target_kind%'
	`).Scan(&check)
	if err != nil {
		t.Fatalf("failed to read target kind check: %v", err)
	}
	for _, v := range []string{"thread", "channel"} {
		if !strings.Contains(check, v) {
			t.Errorf("expected the target kind CHECK to allow %q, got %s", v, check)
		}
	}

	// The dedupe table: unique (connection_id, delivery_id) primary key, the
	// created_at index, and the connection cascade.
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "connection_webhook_deliveries").Scan(&exists); err != nil {
		t.Fatalf("failed to probe connection_webhook_deliveries: %v", err)
	}
	if !exists {
		t.Fatal("expected connection_webhook_deliveries to exist after migrations")
	}

	var pkCols int
	if err := conn.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conrelid = 'connection_webhook_deliveries'::regclass AND contype = 'p'
	`).Scan(&pkCols); err != nil {
		t.Fatalf("failed to read primary key: %v", err)
	}
	if pkCols != 1 {
		t.Fatalf("expected a primary key on connection_webhook_deliveries, got %d", pkCols)
	}

	var pkDefinition string
	if err := conn.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid = 'connection_webhook_deliveries'::regclass AND contype = 'p'
	`).Scan(&pkDefinition); err != nil {
		t.Fatalf("failed to read primary key definition: %v", err)
	}
	for _, col := range []string{"connection_id", "delivery_id"} {
		if !strings.Contains(pkDefinition, col) {
			t.Fatalf("expected the primary key to cover %s, got %s", col, pkDefinition)
		}
	}

	var hasIndex bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE tablename = 'connection_webhook_deliveries'
			  AND indexname = 'idx_connection_webhook_deliveries_created_at'
		)
	`).Scan(&hasIndex); err != nil {
		t.Fatalf("failed to probe created_at index: %v", err)
	}
	if !hasIndex {
		t.Fatal("expected idx_connection_webhook_deliveries_created_at to exist")
	}

	var fkCount int
	if err := conn.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conrelid = 'connection_webhook_deliveries'::regclass
		  AND contype = 'f'
		  AND confrelid = 'workspace_connections'::regclass
	`).Scan(&fkCount); err != nil {
		t.Fatalf("failed to probe connection FK: %v", err)
	}
	if fkCount != 1 {
		t.Fatalf("expected one FK from connection_webhook_deliveries to workspace_connections, got %d", fkCount)
	}
}
