package fake_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// wwSeedWorkspace creates a workspace for webhook store tests.
func wwSeedWorkspace(t *testing.T, ctx context.Context, s store.Store, slug string) *domain.Workspace {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "WW WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}
	return ws
}

func wwValidWebhook(ws *domain.Workspace) *domain.WorkspaceWebhook {
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

// Endpoint CRUD (tasks.md 1.3): create assigns id/timestamps, the full state
// round-trips, update is full-state, delete cascades the delivery stores,
// and the workspace scope is a hard partition.
func TestWorkspaceWebhookStore_CRUDAndScope(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	ws := wwSeedWorkspace(t, ctx, s, "ww-crud")
	otherWS := wwSeedWorkspace(t, ctx, s, "ww-crud-other")

	// 1. Create assigns the id and stamps the timestamps.
	wh := wwValidWebhook(ws)
	if err := s.WorkspaceWebhooks().Create(ctx, wh); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if wh.ID == "" || wh.CreatedAt.IsZero() || wh.UpdatedAt.IsZero() {
		t.Fatalf("expected store-assigned id and timestamps, got %+v", wh)
	}

	// 2. The full state round-trips (ciphertext included — reads through the
	// store are trusted; serialization strips it).
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

	// 3. Update is full-state: rotate (new envelope + hint), pause, and edit
	// the config in one write. CreatedAt is immutable; UpdatedAt moves.
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
	if got.Enabled || got.SecretCiphertext != wh.SecretCiphertext || got.FilterExpression != "" {
		t.Fatalf("expected the full-state update to persist, got %+v", got)
	}
	if !got.CreatedAt.Equal(first) {
		t.Fatalf("expected created_at immutable, before=%v after=%v", first, got.CreatedAt)
	}
	if !got.UpdatedAt.After(wh.CreatedAt) {
		t.Fatalf("expected updated_at stamped on update, got %+v", got)
	}

	// 4. List returns the workspace's endpoints oldest first, and only its
	// own.
	second := wwValidWebhook(ws)
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

	// 5. Scope guards: unknown id, cross-tenant read/update/delete, and
	// validation at the boundary.
	if _, err := s.WorkspaceWebhooks().ByID(ctx, ws.ID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown id, got %v", err)
	}
	if _, err := s.WorkspaceWebhooks().ByID(ctx, otherWS.ID, wh.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant read, got %v", err)
	}
	foreign := wwValidWebhook(otherWS)
	foreign.ID = wh.ID
	if err := s.WorkspaceWebhooks().Update(ctx, foreign); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant update, got %v", err)
	}
	invalid := wwValidWebhook(ws)
	invalid.Scheme = "hmac_md5"
	if err := s.WorkspaceWebhooks().Create(ctx, invalid); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for bad scheme at create, got %v", err)
	}
	if err := s.WorkspaceWebhooks().Update(ctx, invalid); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for bad scheme at update, got %v", err)
	}
	if err := s.WorkspaceWebhooks().Create(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil create, got %v", err)
	}

	// 6. Delete removes the endpoint (and its delivery stores); a
	// foreign-workspace delete is ErrNotFound.
	if err := s.WorkspaceWebhooks().Delete(ctx, otherWS.ID, wh.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant delete, got %v", err)
	}
	if err := s.WorkspaceWebhooks().Delete(ctx, ws.ID, wh.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if _, err := s.WorkspaceWebhooks().ByID(ctx, ws.ID, wh.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	// 7. Transactions snapshot the endpoint like every other store: a
	// rollback leaves the stored state untouched.
	err = s.WithTx(ctx, func(tx store.Store) error {
		rolled := wwValidWebhook(ws)
		rolled.Name = "rolled-back"
		if err := tx.WorkspaceWebhooks().Create(ctx, rolled); err != nil {
			return err
		}
		return errForceRollback
	})
	if !errors.Is(err, errForceRollback) {
		t.Fatalf("expected the forced rollback error, got %v", err)
	}
	list, err = s.WorkspaceWebhooks().List(ctx, ws.ID)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected the rolled-back create to be absent, got %d endpoints", len(list))
	}
}

// Delivery dedupe and the pruned window (design.md D4): ack-after-persist —
// first sighting accepted, replay rejected without error, uniqueness per
// webhook, and the timestamped "sig:" replay key rides the same store.
func TestWorkspaceWebhookStore_Dedupe(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	ws := wwSeedWorkspace(t, ctx, s, "ww-dedupe")
	wh := wwValidWebhook(ws)
	if err := s.WorkspaceWebhooks().Create(ctx, wh); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	wh2 := wwValidWebhook(ws)
	wh2.Name = "second-endpoint"
	if err := s.WorkspaceWebhooks().Create(ctx, wh2); err != nil {
		t.Fatalf("unexpected second create error: %v", err)
	}
	foreignWS := wwSeedWorkspace(t, ctx, s, "ww-dedupe-foreign")
	now := time.Now().UTC()

	// 1. First sighting accepted; the replay rejected without error.
	accepted, err := s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh.ID, "delivery-1", now)
	if err != nil || !accepted {
		t.Fatalf("expected delivery-1 accepted, got accepted=%v err=%v", accepted, err)
	}
	accepted, err = s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh.ID, "delivery-1", now)
	if err != nil || accepted {
		t.Fatalf("expected delivery-1 replay rejected, got accepted=%v err=%v", accepted, err)
	}

	// 2. Uniqueness is per webhook: the same delivery id on another endpoint
	// is a first sighting.
	accepted, err = s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh2.ID, "delivery-1", now)
	if err != nil || !accepted {
		t.Fatalf("expected per-webhook uniqueness, got accepted=%v err=%v", accepted, err)
	}

	// 3. The timestamped-signature replay key rides the same store
	// (design.md D4).
	sigKey := "sig:9f86d081884c7d65"
	accepted, err = s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh.ID, sigKey, now)
	if err != nil || !accepted {
		t.Fatalf("expected signature key accepted, got accepted=%v err=%v", accepted, err)
	}
	accepted, err = s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh.ID, sigKey, now)
	if err != nil || accepted {
		t.Fatalf("expected signature key replay rejected, got accepted=%v err=%v", accepted, err)
	}

	// 4. Guards: empty key, unknown endpoint, cross-tenant.
	if _, err := s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, wh.ID, "", now); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty dedupe key, got %v", err)
	}
	if _, err := s.WorkspaceWebhooks().RecordDelivery(ctx, ws.ID, uuid.NewString(), "d", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown webhook, got %v", err)
	}
	if _, err := s.WorkspaceWebhooks().RecordDelivery(ctx, foreignWS.ID, wh.ID, "d", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant record, got %v", err)
	}

	// 5. Prune: rows recorded before the window start are forgotten, newer
	// rows survive, and a pruned key is a first sighting again.
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
func TestWorkspaceWebhookStore_DeliveryLog(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	ws := wwSeedWorkspace(t, ctx, s, "ww-log")
	wh := wwValidWebhook(ws)
	if err := s.WorkspaceWebhooks().Create(ctx, wh); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	otherWS := wwSeedWorkspace(t, ctx, s, "ww-log-other")

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

	// 2. Guards: unknown outcome, unknown/cross-tenant webhook.
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
	if limited[0].Event != "push" {
		t.Fatalf("expected the limit to keep the newest, got %+v", limited[0])
	}

	// 4. Prune keeps the newest domain.WorkspaceWebhookDeliveryLogKeep rows
	// per webhook. 102 total: the two oldest (the submitted entry and the
	// first filtered one) drop.
	for i := 0; i < 98; i++ {
		e := domain.WebhookDeliveryLogEntry{Event: "bulk", Outcome: domain.WorkspaceWebhookDeliveryOutcomeDuplicate}
		if err := s.WorkspaceWebhooks().AppendDeliveryLog(ctx, ws.ID, wh.ID, e); err != nil {
			t.Fatalf("unexpected bulk append error: %v", err)
		}
	}
	got, err = s.WorkspaceWebhooks().ListDeliveryLog(ctx, ws.ID, wh.ID, 0)
	if err != nil || len(got) != 102 {
		t.Fatalf("expected 102 entries before prune, got %d err=%v", len(got), err)
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

// Run telemetry on the log (add-webhook-channel-visibility 4.2/4.4):
// FinishDeliveryRun finishes the submitted row by run id (best-effort — a
// missing row is a no-op), and ListRuns lists the workspace's submitted
// deliveries as runs with the endpoint and bound agent projected, legacy
// blank statuses reading completed, other workspaces and non-submitted
// outcomes excluded.
func TestWorkspaceWebhookStore_RunTelemetry(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	ws := wwSeedWorkspace(t, ctx, s, "acme")
	otherWS := wwSeedWorkspace(t, ctx, s, "other")

	agent := &domain.Agent{WorkspaceID: ws.ID, Name: "Atlas", Slug: "atlas"}
	if err := s.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	here := wwValidWebhook(ws)
	here.TargetAgentID = agent.ID
	if err := s.WorkspaceWebhooks().Create(ctx, here); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	there := wwValidWebhook(otherWS)
	if err := s.WorkspaceWebhooks().Create(ctx, there); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	base := time.Now().UTC().Add(-time.Hour)
	appendEntry := func(wh *domain.WorkspaceWebhook, at time.Time, event, outcome, runID, status string) {
		entry := domain.WebhookDeliveryLogEntry{Event: event, Outcome: outcome, RunID: runID, Status: status, CreatedAt: at}
		if err := s.WorkspaceWebhooks().AppendDeliveryLog(ctx, wh.WorkspaceID, wh.ID, entry); err != nil {
			t.Fatalf("unexpected append error: %v", err)
		}
	}
	appendEntry(here, base, "merge_request", domain.WorkspaceWebhookDeliveryOutcomeSubmitted, "run-1", domain.WorkspaceWebhookRunStatusRunning)
	appendEntry(here, base.Add(time.Minute), "push", domain.WorkspaceWebhookDeliveryOutcomeRejected, "", "")
	appendEntry(here, base.Add(2*time.Minute), "note_hook", domain.WorkspaceWebhookDeliveryOutcomeSubmitted, "run-legacy", "")
	appendEntry(there, base.Add(3*time.Minute), "merge_request", domain.WorkspaceWebhookDeliveryOutcomeSubmitted, "run-other", "")

	if err := s.WorkspaceWebhooks().FinishDeliveryRun(ctx, ws.ID, "run-1", domain.WorkspaceWebhookRunStatusCompleted, 1500, 321); err != nil {
		t.Fatalf("unexpected finish error: %v", err)
	}
	// A missing run id is a no-op, not an error.
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
	// Newest first; the legacy row reads completed.
	if got[0].RunID != "run-legacy" || got[0].Status != domain.WorkspaceWebhookRunStatusCompleted {
		t.Fatalf("expected the legacy row reading completed, got %+v", got[0])
	}
	if got[1].RunID != "run-1" || got[1].Status != domain.WorkspaceWebhookRunStatusCompleted || got[1].DurationMS != 1500 || got[1].TokensUsed != 321 {
		t.Fatalf("expected the finished telemetry carried through, got %+v", got[1])
	}
	if got[1].WebhookName != here.Name || got[1].AgentID != agent.ID || got[1].AgentName != "Atlas" {
		t.Fatalf("expected the endpoint and bound agent projected, got %+v", got[1])
	}

	// The limit binds; the workspace scope is a hard partition.
	capped, err := s.WorkspaceWebhooks().ListRuns(ctx, ws.ID, 1)
	if err != nil || len(capped) != 1 {
		t.Fatalf("expected the limit to bind, got %v err=%v", capped, err)
	}
	if other, err := s.WorkspaceWebhooks().ListRuns(ctx, otherWS.ID, 0); err != nil || len(other) != 1 {
		t.Fatalf("expected only the other workspace's row there, got %v err=%v", other, err)
	}
}
