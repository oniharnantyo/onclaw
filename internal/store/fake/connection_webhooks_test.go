package fake_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// cwSeedConnection creates a workspace plus a github connection for webhook
// store tests.
func cwSeedConnection(t *testing.T, ctx context.Context, s store.Store, slug string) (*domain.Workspace, *domain.Connection) {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "CW WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}
	c := &domain.Connection{WorkspaceID: ws.ID, Service: "github", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := s.Connections().Create(ctx, c); err != nil {
		t.Fatalf("unexpected create connection error: %v", err)
	}
	return ws, c
}

func cwValidState(ws *domain.Workspace, c *domain.Connection) *domain.ConnectionWebhook {
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

func TestConnectionWebhookStore_StateRoundTrip(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	ws, c := cwSeedConnection(t, ctx, s, "cw-roundtrip")
	otherWS, _ := cwSeedConnection(t, ctx, s, "cw-roundtrip-other")

	// 1. A fresh connection reads back the inert default — the shape the
	// columns' defaults produce (tasks.md 1.4).
	got, err := s.ConnectionWebhooks().GetState(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get-state error: %v", err)
	}
	if got.Enabled || got.SecretCiphertext != "" || got.SecretHint != "" || got.TargetKind != "" || got.TargetID != "" {
		t.Fatalf("expected the inert default, got %+v", got)
	}
	if got.WorkspaceID != ws.ID || got.ConnectionID != c.ID {
		t.Fatalf("expected the scope to be filled in, got %+v", got)
	}
	if got.Events == nil || len(got.Events) != 0 {
		t.Fatalf("expected events to read back as an empty array, got %#v", got.Events)
	}

	// 2. UpdateState persists the full state and bumps the connection's
	// updated_at.
	before, err := s.Connections().Get(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	state := cwValidState(ws, c)
	if err := s.ConnectionWebhooks().UpdateState(ctx, state); err != nil {
		t.Fatalf("unexpected update-state error: %v", err)
	}
	after, err := s.Connections().Get(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("expected the connection's updated_at to be bumped, before=%v after=%v", before.UpdatedAt, after.UpdatedAt)
	}

	// 3. The round trip returns exactly what was stored (ciphertext
	// included — reads through the store are trusted; serialization strips
	// it).
	got, err = s.ConnectionWebhooks().GetState(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get-state error: %v", err)
	}
	if !got.Enabled || got.SecretCiphertext != state.SecretCiphertext || got.SecretHint != state.SecretHint {
		t.Fatalf("expected the enabled state to roundtrip, got %+v", got)
	}
	if got.TargetAgentID != state.TargetAgentID || got.TargetKind != state.TargetKind || got.TargetID != state.TargetID {
		t.Fatalf("expected the target binding to roundtrip, got %+v", got)
	}
	if len(got.Events) != 2 || got.Events[0] != "pull_request.opened" || got.Events[1] != "push" {
		t.Fatalf("expected the selection to roundtrip in order, got %v", got.Events)
	}

	// 4. Disabling keeps the rest of the state intact (spec: disabling stops
	// ingestion and leaves the connection otherwise intact).
	disabled := got
	disabled.Enabled = false
	if err := s.ConnectionWebhooks().UpdateState(ctx, disabled); err != nil {
		t.Fatalf("unexpected disable error: %v", err)
	}
	got, err = s.ConnectionWebhooks().GetState(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get-state error: %v", err)
	}
	if got.Enabled || got.TargetKind == "" || len(got.Events) == 0 {
		t.Fatalf("expected the disabled state to keep binding and selection, got %+v", got)
	}

	// 4b. The last-error residue rides the full-state write path both ways
	// (the ingress's render-drop surface, task 2.4): the inert default reads
	// empty, a set residue round-trips, and clearing stores the clear shape.
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
		t.Fatalf("expected the cleared residue to read back empty, got %q", got.LastError)
	}

	// 5. Scope guards: unknown connection, cross-tenant, empty scope, nil,
	// and invalid states.
	if _, err := s.ConnectionWebhooks().GetState(ctx, ws.ID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown connection, got %v", err)
	}
	if _, err := s.ConnectionWebhooks().GetState(ctx, otherWS.ID, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant get-state, got %v", err)
	}
	if _, err := s.ConnectionWebhooks().GetState(ctx, "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty scope, got %v", err)
	}
	foreign := cwValidState(otherWS, c) // state claims another workspace
	if err := s.ConnectionWebhooks().UpdateState(ctx, foreign); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant update, got %v", err)
	}
	invalid := cwValidState(ws, c)
	invalid.TargetKind = "dm"
	if err := s.ConnectionWebhooks().UpdateState(ctx, invalid); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for out-of-catalog target kind, got %v", err)
	}
	if err := s.ConnectionWebhooks().UpdateState(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil state, got %v", err)
	}

	// 6. Transactions snapshot the webhook state like every other store:
	// a rollback leaves the stored state untouched.
	err = s.WithTx(ctx, func(tx store.Store) error {
		st := cwValidState(ws, c)
		st.Enabled = true
		if err := tx.ConnectionWebhooks().UpdateState(ctx, st); err != nil {
			return err
		}
		return errForceRollback
	})
	if !errors.Is(err, errForceRollback) {
		t.Fatalf("expected the forced rollback error, got %v", err)
	}
	got, err = s.ConnectionWebhooks().GetState(ctx, ws.ID, c.ID)
	if err != nil {
		t.Fatalf("unexpected get-state error: %v", err)
	}
	if got.Enabled {
		t.Fatalf("expected the rolled-back enablement to be absent, got %+v", got)
	}
}

var errForceRollback = errors.New("force rollback")

func TestConnectionWebhookStore_DedupeAndPrune(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	ws, c := cwSeedConnection(t, ctx, s, "cw-dedupe")
	// A second connection in the SAME workspace — per-connection delivery
	// uniqueness needs two connections sharing the scope.
	c2 := &domain.Connection{WorkspaceID: ws.ID, Service: "gitlab", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := s.Connections().Create(ctx, c2); err != nil {
		t.Fatalf("unexpected create second connection error: %v", err)
	}
	now := time.Now().UTC()

	// 1. First sighting is accepted; the replay is rejected without error
	// (spec: the second delivery is acknowledged without a second turn).
	accepted, err := s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, c.ID, "delivery-1", now)
	if err != nil || !accepted {
		t.Fatalf("expected delivery-1 accepted, got accepted=%v err=%v", accepted, err)
	}
	accepted, err = s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, c.ID, "delivery-1", now)
	if err != nil || accepted {
		t.Fatalf("expected delivery-1 replay rejected, got accepted=%v err=%v", accepted, err)
	}

	// 2. Uniqueness is per connection (tasks.md 1.3): the same delivery id
	// on another connection is a first sighting.
	accepted, err = s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, c2.ID, "delivery-1", now)
	if err != nil || !accepted {
		t.Fatalf("expected per-connection uniqueness, got accepted=%v err=%v", accepted, err)
	}

	// 3. Scope and shape guards.
	if _, err := s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, c.ID, "", now); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty delivery id, got %v", err)
	}
	if _, err := s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, uuid.NewString(), "d", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown connection, got %v", err)
	}

	// Build a genuinely foreign workspace to scope-check against.
	foreignWS := &domain.Workspace{Slug: "cw-dedupe-foreign", Name: "CW Foreign"}
	if err := s.Workspaces().Create(ctx, foreignWS); err != nil {
		t.Fatalf("unexpected create foreign workspace error: %v", err)
	}
	if _, err := s.ConnectionWebhooks().RecordDelivery(ctx, foreignWS.ID, c.ID, "d", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant record, got %v", err)
	}

	// 4. The pruned window (design.md D3): rows recorded before the window
	// start are forgotten, newer rows survive, and a pruned id is a first
	// sighting again.
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

	// A pruned id is a first sighting again: recording delivery-old is now
	// accepted (a redelivery that late is a new event, not a replay).
	accepted, err = s.ConnectionWebhooks().RecordDelivery(ctx, ws.ID, c.ID, "delivery-old", now)
	if err != nil || !accepted {
		t.Fatalf("expected the pruned id to be accepted again, got accepted=%v err=%v", accepted, err)
	}

	// Pruning an empty window removes nothing.
	pruned, err = s.ConnectionWebhooks().PruneDeliveries(ctx, now)
	if err != nil || pruned != 0 {
		t.Fatalf("expected no-op prune, got pruned=%d err=%v", pruned, err)
	}
}
