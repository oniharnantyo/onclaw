//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// pgWWSeedWorkspace creates a workspace for workspace-webhook store tests
// (integration twin of the fake helper).
func pgWWSeedWorkspace(t *testing.T, ctx context.Context, s store.Store, slug string) *domain.Workspace {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "PG WW WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}
	return ws
}

func pgWWValidWebhook(ws *domain.Workspace) *domain.WorkspaceWebhook {
	return &domain.WorkspaceWebhook{
		WorkspaceID:      ws.ID,
		Name:             "gitlab-mr-review",
		Scheme:           domain.WorkspaceWebhookSchemeTimestampedHmac,
		SecretCiphertext: "v1:bm9uY2U:Y2lwaGVydGV4dA",
		SecretHint:       "bG9s",
		EventSource:      domain.WorkspaceWebhookEventSource{Kind: domain.WorkspaceWebhookEventSourceHeader, Name: "X-Gitlab-Event"},
		DeliveryIDSource: "X-Gitlab-Event-UUID",
		FilterExpression: `event == "merge_request"`,
		TurnTemplate:     "MR on {project.path_with_namespace}",
		SamplePayload:    `{"project":{"path_with_namespace":"team/api"}}`,
		TargetAgentID:    uuid.NewString(),
		TargetKind:       domain.ConnectionWebhookTargetChannel,
		TargetID:         "chan-code-review",
		Enabled:          true,
	}
}

// Endpoint CRUD and the workspace scope (add-workspace-webhooks tasks.md
// 1.3): store-assigned id, full-state round trip through the jsonb event
// source and the nullable filter/template/sample columns, immutable
// created_at, and the hard tenant partition — mirroring the fake's
// semantics.
func TestIntegration_WorkspaceWebhookStore_CRUD(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := pgWWSeedWorkspace(t, ctx, s, "pg-ww-crud")
	otherWS := pgWWSeedWorkspace(t, ctx, s, "pg-ww-crud-other")

	// 1. Create assigns the id and stamps the timestamps.
	wh := pgWWValidWebhook(ws)
	if err := s.WorkspaceWebhooks().Create(ctx, wh); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if wh.ID == "" || wh.CreatedAt.IsZero() || wh.UpdatedAt.IsZero() {
		t.Fatalf("expected store-assigned id and timestamps, got %+v", wh)
	}

	// 2. An unknown workspace fails the FK as ErrNotFound.
	orphan := pgWWValidWebhook(&domain.Workspace{ID: uuid.NewString()})
	if err := s.WorkspaceWebhooks().Create(ctx, orphan); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}

	// 3. The full state round-trips.
	got, err := s.WorkspaceWebhooks().ByID(ctx, ws.ID, wh.ID)
	if err != nil {
		t.Fatalf("unexpected by-id error: %v", err)
	}
	if got.Scheme != wh.Scheme || got.SecretCiphertext != wh.SecretCiphertext || got.SecretHint != wh.SecretHint {
		t.Fatalf("expected credential fields to roundtrip, got %+v", got)
	}
	if got.EventSource != wh.EventSource || got.DeliveryIDSource != wh.DeliveryIDSource {
		t.Fatalf("expected event source to roundtrip, got %+v", got)
	}
	if got.FilterExpression != wh.FilterExpression || got.TurnTemplate != wh.TurnTemplate || got.SamplePayload != wh.SamplePayload {
		t.Fatalf("expected filter/template/sample to roundtrip, got %+v", got)
	}
	if got.TargetAgentID != wh.TargetAgentID || got.TargetKind != wh.TargetKind || got.TargetID != wh.TargetID || !got.Enabled {
		t.Fatalf("expected binding to roundtrip, got %+v", got)
	}
	if !got.CreatedAt.Equal(wh.CreatedAt.Truncate(time.Microsecond)) {
		t.Fatalf("expected created_at to roundtrip, got %v want %v", got.CreatedAt, wh.CreatedAt)
	}

	// 4. Update is full-state: rotate, pause, and clear the config in one
	// write; created_at is immutable.
	first := got.CreatedAt
	wh.SecretCiphertext = "v1:bmV3:ZW52ZWxvcGU"
	wh.SecretHint = "cGcw"
	wh.Enabled = false
	wh.FilterExpression = ""
	wh.TurnTemplate = ""
	wh.SamplePayload = ""
	if err := s.WorkspaceWebhooks().Update(ctx, wh); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	got, err = s.WorkspaceWebhooks().ByID(ctx, ws.ID, wh.ID)
	if err != nil {
		t.Fatalf("unexpected by-id error: %v", err)
	}
	if got.Enabled || got.SecretCiphertext != wh.SecretCiphertext || got.FilterExpression != "" || got.TurnTemplate != "" || got.SamplePayload != "" {
		t.Fatalf("expected the full-state update to persist, got %+v", got)
	}
	if !got.CreatedAt.Equal(first) {
		t.Fatalf("expected created_at immutable, before=%v after=%v", first, got.CreatedAt)
	}

	// 5. List is oldest first and workspace-partitioned.
	second := pgWWValidWebhook(ws)
	second.Name = "second-endpoint"
	if err := s.WorkspaceWebhooks().Create(ctx, second); err != nil {
		t.Fatalf("unexpected second create error: %v", err)
	}
	list, err := s.WorkspaceWebhooks().List(ctx, ws.ID)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(list) != 2 || list[0].ID != wh.ID || list[1].ID != second.ID {
		t.Fatalf("expected two endpoints oldest first, got %+v", list)
	}
	foreignList, err := s.WorkspaceWebhooks().List(ctx, otherWS.ID)
	if err != nil || len(foreignList) != 0 {
		t.Fatalf("expected an empty list for the foreign workspace, got %v err=%v", foreignList, err)
	}

	// 6. Scope and validation guards mirror the fake.
	if _, err := s.WorkspaceWebhooks().ByID(ctx, ws.ID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown id, got %v", err)
	}
	if _, err := s.WorkspaceWebhooks().ByID(ctx, otherWS.ID, wh.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant read, got %v", err)
	}
	foreign := pgWWValidWebhook(otherWS)
	foreign.ID = wh.ID
	if err := s.WorkspaceWebhooks().Update(ctx, foreign); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant update, got %v", err)
	}
	invalid := pgWWValidWebhook(ws)
	invalid.Scheme = "hmac_md5"
	if err := s.WorkspaceWebhooks().Create(ctx, invalid); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for bad scheme at create, got %v", err)
	}
	if err := s.WorkspaceWebhooks().Create(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil create, got %v", err)
	}

	// 7. Delete removes the endpoint; a foreign-workspace delete is
	// ErrNotFound.
	if err := s.WorkspaceWebhooks().Delete(ctx, otherWS.ID, wh.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant delete, got %v", err)
	}
	if err := s.WorkspaceWebhooks().Delete(ctx, ws.ID, wh.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if _, err := s.WorkspaceWebhooks().ByID(ctx, ws.ID, wh.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

// Delivery dedupe and the pruned window (design.md D4): ack-after-persist —
// first sighting accepted, replay rejected without error, uniqueness per
// webhook, and the timestamped "sig:" replay key rides the same store.
func TestIntegration_WorkspaceWebhookStore_Dedupe(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := pgWWSeedWorkspace(t, ctx, s, "pg-ww-dedupe")
	wh := pgWWValidWebhook(ws)
	if err := s.WorkspaceWebhooks().Create(ctx, wh); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	wh2 := pgWWValidWebhook(ws)
	wh2.Name = "second-endpoint"
	if err := s.WorkspaceWebhooks().Create(ctx, wh2); err != nil {
		t.Fatalf("unexpected second create error: %v", err)
	}
	foreignWS := pgWWSeedWorkspace(t, ctx, s, "pg-ww-dedupe-foreign")
	now := time.Now().UTC().Truncate(time.Microsecond)

	// 1. Accept then replay-reject.
	accepted, err := s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh.ID, "delivery-1", now)
	if err != nil || !accepted {
		t.Fatalf("expected delivery-1 accepted, got accepted=%v err=%v", accepted, err)
	}
	accepted, err = s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh.ID, "delivery-1", now)
	if err != nil || accepted {
		t.Fatalf("expected delivery-1 replay rejected, got accepted=%v err=%v", accepted, err)
	}

	// 2. Uniqueness is per webhook.
	accepted, err = s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh2.ID, "delivery-1", now)
	if err != nil || !accepted {
		t.Fatalf("expected per-webhook uniqueness, got accepted=%v err=%v", accepted, err)
	}

	// 3. The timestamped-signature replay key rides the same store.
	sigKey := "sig:9f86d081884c7d65"
	accepted, err = s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh.ID, sigKey, now)
	if err != nil || !accepted {
		t.Fatalf("expected signature key accepted, got accepted=%v err=%v", accepted, err)
	}
	accepted, err = s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh.ID, sigKey, now)
	if err != nil || accepted {
		t.Fatalf("expected signature key replay rejected, got accepted=%v err=%v", accepted, err)
	}

	// 4. Guards.
	if _, err := s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh.ID, "", now); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty dedupe key, got %v", err)
	}
	if _, err := s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, uuid.NewString(), "d", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown webhook, got %v", err)
	}
	if _, err := s.WorkspaceWebhooks().RecordDelivery(ctx, foreignWS.ID, wh.ID, "d", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant record, got %v", err)
	}

	// 5. Prune: old rows die, new rows survive, pruned keys are first
	// sightings again.
	old := now.Add(-domain.WorkspaceWebhookDeliveryWindow - time.Hour)
	accepted, err = s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh.ID, "delivery-old", old)
	if err != nil || !accepted {
		t.Fatalf("expected delivery-old accepted, got accepted=%v err=%v", accepted, err)
	}
	if err := s.WorkspaceWebhooks().PruneDeliveries(ctx, now.Add(-domain.WorkspaceWebhookDeliveryWindow)); err != nil {
		t.Fatalf("unexpected prune error: %v", err)
	}
	accepted, err = s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh.ID, "delivery-1", now)
	if err != nil || accepted {
		t.Fatalf("expected delivery-1 to survive the prune and stay deduped, got accepted=%v err=%v", accepted, err)
	}
	accepted, err = s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh.ID, "delivery-old", now)
	if err != nil || !accepted {
		t.Fatalf("expected the pruned key to be accepted again, got accepted=%v err=%v", accepted, err)
	}
}

// The bounded delivery log (design.md D8): append with store-stamped
// id/scope/timestamp, newest-first listing with limit, outcome validated,
// and the prune keeping the newest 100 per webhook.
func TestIntegration_WorkspaceWebhookStore_DeliveryLog(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := pgWWSeedWorkspace(t, ctx, s, "pg-ww-log")
	wh := pgWWValidWebhook(ws)
	if err := s.WorkspaceWebhooks().Create(ctx, wh); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	otherWS := pgWWSeedWorkspace(t, ctx, s, "pg-ww-log-other")

	// 1. Append stores the entry with store-stamped id, webhook scope, and
	// timestamp.
	entry := domain.WebhookDeliveryLogEntry{
		Event:   "merge_request",
		Outcome: domain.WorkspaceWebhookDeliveryOutcomeSubmitted,
		RunID:   "run-123",
	}
	if err := s.WorkspaceWebhooks().AppendDeliveryLog(ctx, ws.ID, wh.ID, entry); err != nil {
		t.Fatalf("unexpected append error: %v", err)
	}

	// 2. Guards.
	bad := domain.WebhookDeliveryLogEntry{Outcome: "exploded"}
	if err := s.WorkspaceWebhooks().AppendDeliveryLog(ctx, ws.ID, wh.ID, bad); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for unknown outcome, got %v", err)
	}
	if err := s.WorkspaceWebhooks().AppendDeliveryLog(ctx, ws.ID, uuid.NewString(), entry); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown webhook, got %v", err)
	}
	if err := s.WorkspaceWebhooks().AppendDeliveryLog(ctx, otherWS.ID, wh.ID, entry); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant append, got %v", err)
	}
	if _, err := s.WorkspaceWebhooks().ListDeliveryLog(ctx, otherWS.ID, wh.ID, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant list, got %v", err)
	}

	// 3. Listing is newest first; limit bounds the newest window.
	for i := 0; i < 3; i++ {
		e := domain.WebhookDeliveryLogEntry{
			Event:   "push",
			Outcome: domain.WorkspaceWebhookDeliveryOutcomeFiltered,
			Detail:  "filter did not match",
		}
		if err := s.WorkspaceWebhooks().AppendDeliveryLog(ctx, ws.ID, wh.ID, e); err != nil {
			t.Fatalf("unexpected append error: %v", err)
		}
	}
	got, err := s.WorkspaceWebhooks().ListDeliveryLog(ctx, ws.ID, wh.ID, 0)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(got))
	}
	if got[0].Event != "push" || got[3].Event != "merge_request" {
		t.Fatalf("expected newest first, got events %q..%q", got[0].Event, got[3].Event)
	}
	if got[3].RunID != "run-123" || got[3].WebhookID != wh.ID || got[3].ID == "" || got[3].CreatedAt.IsZero() {
		t.Fatalf("expected the first entry store-stamped and run-linked, got %+v", got[3])
	}
	limited, err := s.WorkspaceWebhooks().ListDeliveryLog(ctx, ws.ID, wh.ID, 2)
	if err != nil || len(limited) != 2 {
		t.Fatalf("expected the limit to bound the list to 2, got %d err=%v", len(limited), err)
	}

	// 4. Prune keeps the newest domain.WorkspaceWebhookDeliveryLogKeep rows
	// per webhook: 102 total, the two oldest drop.
	for i := 0; i < 98; i++ {
		e := domain.WebhookDeliveryLogEntry{Event: "bulk", Outcome: domain.WorkspaceWebhookDeliveryOutcomeDuplicate}
		if err := s.WorkspaceWebhooks().AppendDeliveryLog(ctx, ws.ID, wh.ID, e); err != nil {
			t.Fatalf("unexpected bulk append error: %v", err)
		}
	}
	if err := s.WorkspaceWebhooks().PruneDeliveryLog(ctx, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("unexpected prune error: %v", err)
	}
	got, err = s.WorkspaceWebhooks().ListDeliveryLog(ctx, ws.ID, wh.ID, 0)
	if err != nil || len(got) != domain.WorkspaceWebhookDeliveryLogKeep {
		t.Fatalf("expected prune to keep %d, got %d err=%v", domain.WorkspaceWebhookDeliveryLogKeep, len(got), err)
	}
	if got[len(got)-1].Event != "push" || got[len(got)-1].Detail != "filter did not match" {
		t.Fatalf("expected prune to drop the oldest and keep the newer filtered entry, got %+v", got[len(got)-1])
	}
	if got[0].Event != "bulk" {
		t.Fatalf("expected the newest bulk entry to survive, got %+v", got[0])
	}

	// 5. Rows stamped at or after `now` are never pruned, even beyond the
	// cap.
	future := domain.WebhookDeliveryLogEntry{Event: "future", Outcome: domain.WorkspaceWebhookDeliveryOutcomeRejected, Detail: "bad signature"}
	if err := s.WorkspaceWebhooks().AppendDeliveryLog(ctx, ws.ID, wh.ID, future); err != nil {
		t.Fatalf("unexpected future append error: %v", err)
	}
	if err := s.WorkspaceWebhooks().PruneDeliveryLog(ctx, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatalf("unexpected past-now prune error: %v", err)
	}
	got, err = s.WorkspaceWebhooks().ListDeliveryLog(ctx, ws.ID, wh.ID, 1)
	if err != nil || len(got) != 1 || got[0].Event != "future" {
		t.Fatalf("expected the future-stamped row to survive a past-now prune, got %v err=%v", got, err)
	}
}

// TestIntegration_WorkspaceWebhookSchema covers migration 000075
// (add-workspace-webhooks tasks.md 1.2): the three tables exist — the
// endpoint aggregate with its scheme/target-kind CHECKs, the dedupe store
// unique per webhook with the created_at index the prune rides, and the
// delivery log with its (webhook_id, created_at DESC) index — all cascading
// off their parents.
func TestIntegration_WorkspaceWebhookSchema(t *testing.T) {
	_, schemaDSN, ctx := setupTestSchema(t)

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer conn.Close(ctx)

	// The endpoint table's columns: every text column is NOT NULL DEFAULT ''
	// (the 000064 convention — blank is the unset shape, no NULLs); the
	// binding is NOT NULL because a standalone endpoint is born with it.
	wantColumns := []struct {
		name    string
		notNull bool
	}{
		{"id", true},
		{"workspace_id", true},
		{"name", true},
		{"scheme", true},
		{"secret_ciphertext", true},
		{"secret_hint", true},
		{"event_source", true},
		{"delivery_id_source", true},
		{"filter_expression", true},
		{"turn_template", true},
		{"sample_payload", true},
		{"target_agent_id", true},
		{"target_kind", true},
		{"target_id", true},
		{"enabled", true},
		{"created_at", true},
		{"updated_at", true},
	}
	rows, err := conn.Query(ctx, `
		SELECT attname, attnotnull FROM pg_attribute
		WHERE attrelid = 'workspace_webhooks'::regclass AND attname = ANY($1)
		  AND NOT attisdropped
	`, wantColumnNames(wantColumns))
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
			t.Errorf("expected workspace_webhooks.%s to exist", want.name)
		}
	}

	// The CHECK catalogs: scheme and target kind.
	var checks []string
	crows, err := conn.Query(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid = 'workspace_webhooks'::regclass AND contype = 'c'
	`)
	if err != nil {
		t.Fatalf("failed to read checks: %v", err)
	}
	defer crows.Close()
	for crows.Next() {
		var def string
		if err := crows.Scan(&def); err != nil {
			t.Fatalf("failed to scan check: %v", err)
		}
		checks = append(checks, def)
	}
	if err := crows.Err(); err != nil {
		t.Fatalf("failed to iterate checks: %v", err)
	}
	joined := strings.Join(checks, "\n")
	for _, want := range []string{"plain_token", "hmac_sha256", "timestamped_hmac", "thread", "channel"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected the CHECKs to allow %q, got %s", want, joined)
		}
	}

	// The dedupe table: unique (webhook_id, dedupe_key) primary key, the
	// created_at index, and both cascading FKs.
	assertTableShape(t, ctx, conn, "workspace_webhook_deliveries",
		[]string{"webhook_id", "dedupe_key"},
		"idx_workspace_webhook_deliveries_created_at",
		map[string]string{"workspace_webhooks": "webhook_id", "workspaces": "workspace_id"})

	// The delivery log: uuid primary key, the bounded-list index, and both
	// cascading FKs.
	assertTableShape(t, ctx, conn, "workspace_webhook_delivery_log",
		[]string{"id"},
		"idx_workspace_webhook_delivery_log_webhook_created",
		map[string]string{"workspace_webhooks": "webhook_id", "workspaces": "workspace_id"})

	// The log's (webhook_id, created_at DESC) index shape.
	var indexDef string
	if err := conn.QueryRow(ctx, `
		SELECT indexdef FROM pg_indexes WHERE indexname = 'idx_workspace_webhook_delivery_log_webhook_created'
	`).Scan(&indexDef); err != nil {
		t.Fatalf("failed to read log index: %v", err)
	}
	if !strings.Contains(indexDef, "created_at DESC") {
		t.Fatalf("expected the log index to order created_at DESC, got %s", indexDef)
	}
}

func wantColumnNames(cols []struct {
	name    string
	notNull bool
}) []string {
	names := make([]string, 0, len(cols))
	for _, c := range cols {
		names = append(names, c.name)
	}
	return names
}

// assertTableShape probes one delivery-side table: its existence, primary
// key coverage, the prune index, and its FK targets.
func assertTableShape(t *testing.T, ctx context.Context, conn *pgx.Conn, table string, pkCols []string, indexName string, fks map[string]string) {
	t.Helper()

	var exists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
		t.Fatalf("failed to probe %s: %v", table, err)
	}
	if !exists {
		t.Fatalf("expected %s to exist after migrations", table)
	}

	var pkDefinition string
	if err := conn.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid = $1::regclass AND contype = 'p'
	`, table).Scan(&pkDefinition); err != nil {
		t.Fatalf("failed to read primary key of %s: %v", table, err)
	}
	for _, col := range pkCols {
		if !strings.Contains(pkDefinition, col) {
			t.Fatalf("expected the primary key of %s to cover %s, got %s", table, col, pkDefinition)
		}
	}

	var hasIndex bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes WHERE tablename = $1 AND indexname = $2
		)
	`, table, indexName).Scan(&hasIndex); err != nil {
		t.Fatalf("failed to probe index of %s: %v", table, err)
	}
	if !hasIndex {
		t.Fatalf("expected %s to exist", indexName)
	}

	for refTable, refCol := range fks {
		var fkCount int
		if err := conn.QueryRow(ctx, `
			SELECT COUNT(*) FROM pg_constraint
			WHERE conrelid = $1::regclass
			  AND contype = 'f'
			  AND confrelid = $2::regclass
			  AND pg_get_constraintdef(oid) LIKE '%' || $3 || '%'
		`, table, refTable, refCol).Scan(&fkCount); err != nil {
			t.Fatalf("failed to probe FK of %s: %v", table, err)
		}
		if fkCount != 1 {
			t.Fatalf("expected one FK from %s to %s (%s), got %d", table, refTable, refCol, fkCount)
		}
	}
}

// TestIntegration_WorkspaceWebhookStore_RunTelemetry covers migration
// 000076's columns end to end (add-webhook-channel-visibility 4.2/4.4):
// FinishDeliveryRun finishes the submitted row by run id — a missing row is
// a no-op — and ListRuns lists the workspace's submitted deliveries as runs
// with the endpoint and bound agent projected, legacy blank statuses reading
// completed, other workspaces and non-submitted outcomes excluded.
func TestIntegration_WorkspaceWebhookStore_RunTelemetry(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := pgWWSeedWorkspace(t, ctx, s, "pg-ww-telemetry")
	otherWS := pgWWSeedWorkspace(t, ctx, s, "pg-ww-telemetry-other")

	provider := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI WS", Enabled: true}
	if err := s.Providers().Create(ctx, provider); err != nil {
		t.Fatalf("unexpected create provider error: %v", err)
	}
	agent := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: provider.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("unexpected create agent error: %v", err)
	}

	wh := pgWWValidWebhook(ws)
	wh.TargetAgentID = agent.ID
	if err := s.WorkspaceWebhooks().Create(ctx, wh); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	other := pgWWValidWebhook(otherWS)
	if err := s.WorkspaceWebhooks().Create(ctx, other); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	base := time.Now().UTC().Add(-time.Hour)
	appendEntry := func(wh *domain.WorkspaceWebhook, at time.Time, event, outcome, runID, status string) {
		entry := domain.WebhookDeliveryLogEntry{Event: event, Outcome: outcome, RunID: runID, Status: status, CreatedAt: at}
		if err := s.WorkspaceWebhooks().AppendDeliveryLog(ctx, wh.WorkspaceID, wh.ID, entry); err != nil {
			t.Fatalf("unexpected append error: %v", err)
		}
	}
	appendEntry(wh, base, "merge_request", domain.WorkspaceWebhookDeliveryOutcomeSubmitted, "run-1", domain.WorkspaceWebhookRunStatusRunning)
	appendEntry(wh, base.Add(time.Minute), "push", domain.WorkspaceWebhookDeliveryOutcomeRejected, "", "")
	appendEntry(wh, base.Add(2*time.Minute), "note_hook", domain.WorkspaceWebhookDeliveryOutcomeSubmitted, "run-legacy", "")
	appendEntry(other, base.Add(3*time.Minute), "merge_request", domain.WorkspaceWebhookDeliveryOutcomeSubmitted, "run-other", "")

	if err := s.WorkspaceWebhooks().FinishDeliveryRun(ctx, ws.ID, "run-1", domain.WorkspaceWebhookRunStatusCompleted, 1500, 321); err != nil {
		t.Fatalf("unexpected finish error: %v", err)
	}
	if err := s.WorkspaceWebhooks().FinishDeliveryRun(ctx, ws.ID, "run-absent", domain.WorkspaceWebhookRunStatusFailed, 1, 1); err != nil {
		t.Fatalf("finish on a missing row must be a no-op, got %v", err)
	}

	got, err := s.WorkspaceWebhooks().ListRuns(ctx, ws.ID, 0)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected the two submitted rows only, got %d: %+v", len(got), got)
	}
	if got[0].RunID != "run-legacy" || got[0].Status != domain.WorkspaceWebhookRunStatusCompleted {
		t.Fatalf("expected the legacy row reading completed, got %+v", got[0])
	}
	if got[1].RunID != "run-1" || got[1].Status != domain.WorkspaceWebhookRunStatusCompleted || got[1].DurationMS != 1500 || got[1].TokensUsed != 321 {
		t.Fatalf("expected the finished telemetry carried through, got %+v", got[1])
	}
	if got[1].WebhookName != wh.Name || got[1].AgentID != agent.ID || got[1].AgentName != "Atlas" {
		t.Fatalf("expected the endpoint and bound agent projected, got %+v", got[1])
	}

	capped, err := s.WorkspaceWebhooks().ListRuns(ctx, ws.ID, 1)
	if err != nil || len(capped) != 1 {
		t.Fatalf("expected the limit to bind, got %v err=%v", capped, err)
	}
	if otherRuns, err := s.WorkspaceWebhooks().ListRuns(ctx, otherWS.ID, 0); err != nil || len(otherRuns) != 1 {
		t.Fatalf("expected only the other workspace's row there, got %v err=%v", otherRuns, err)
	}
}
