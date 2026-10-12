package fake

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// workspaceWebhookStore implements store.WorkspaceWebhookStore in memory:
// the endpoint rows live in a shared id-keyed map, the dedupe window in a
// per-endpoint delivery map, the delivery log in per-endpoint ordered
// slices. Every mutation runs under the store lock — the in-memory analogue
// of the postgres adapter's single statement.
type workspaceWebhookStore struct {
	s *fakeStore
}

// webhookExistsLocked reports whether the endpoint id exists in the given
// workspace. Callers hold the store lock.
func (ws *workspaceWebhookStore) webhookExistsLocked(workspaceID, webhookID string) bool {
	w, exists := ws.s.workspaceWebhooks[webhookID]
	return exists && w.WorkspaceID == workspaceID
}

// Create inserts a new endpoint row (tasks.md 1.3): validation at the
// boundary, workspace existence, id assignment when empty, conflict on a
// duplicate caller-supplied id, and app-stamped timestamps.
func (ws *workspaceWebhookStore) Create(ctx context.Context, wh *domain.WorkspaceWebhook) error {
	if wh == nil {
		return domain.ErrInvalid
	}
	if err := wh.Validate(); err != nil {
		return err
	}

	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	if _, exists := ws.s.workspaces[wh.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}

	if wh.ID != "" {
		if _, exists := ws.s.workspaceWebhooks[wh.ID]; exists {
			return fmt.Errorf("%w: webhook with id %q already exists", domain.ErrConflict, wh.ID)
		}
	} else {
		wh.ID = uuid.NewString()
	}

	now := ws.s.stampNow()
	if wh.CreatedAt.IsZero() {
		wh.CreatedAt = now
	}
	if wh.UpdatedAt.IsZero() {
		wh.UpdatedAt = now
	}

	ws.s.workspaceWebhooks[wh.ID] = cloneWorkspaceWebhook(wh)
	return nil
}

// ByID reads one endpoint; absent and foreign-workspace ids are
// indistinguishably domain.ErrNotFound.
func (ws *workspaceWebhookStore) ByID(ctx context.Context, workspaceID, id string) (*domain.WorkspaceWebhook, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	ws.s.mu.RLock()
	defer ws.s.mu.RUnlock()

	w, exists := ws.s.workspaceWebhooks[id]
	if !exists || w.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return cloneWorkspaceWebhook(w), nil
}

// List returns the workspace's endpoints, oldest first (created_at, id as
// the determinism tiebreak). The result is never nil.
func (ws *workspaceWebhookStore) List(ctx context.Context, workspaceID string) ([]*domain.WorkspaceWebhook, error) {
	if workspaceID == "" {
		return []*domain.WorkspaceWebhook{}, nil
	}

	ws.s.mu.RLock()
	defer ws.s.mu.RUnlock()

	webhooks := make([]*domain.WorkspaceWebhook, 0)
	for _, w := range ws.s.workspaceWebhooks {
		if w.WorkspaceID == workspaceID {
			webhooks = append(webhooks, cloneWorkspaceWebhook(w))
		}
	}
	sort.Slice(webhooks, func(i, j int) bool {
		if webhooks[i].CreatedAt.Equal(webhooks[j].CreatedAt) {
			return webhooks[i].ID < webhooks[j].ID
		}
		return webhooks[i].CreatedAt.Before(webhooks[j].CreatedAt)
	})
	return webhooks, nil
}

// Update writes the full endpoint state — the config-edit, rotate, and
// enable/pause write path. Identity fields are immutable: CreatedAt stays as
// stored and the stored state is written back into wh so it stays faithful.
func (ws *workspaceWebhookStore) Update(ctx context.Context, wh *domain.WorkspaceWebhook) error {
	if wh == nil {
		return domain.ErrInvalid
	}
	if err := wh.Validate(); err != nil {
		return err
	}

	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	existing, exists := ws.s.workspaceWebhooks[wh.ID]
	if !exists || existing.WorkspaceID != wh.WorkspaceID {
		return domain.ErrNotFound
	}

	wh.CreatedAt = existing.CreatedAt
	wh.UpdatedAt = ws.s.stampNow()
	ws.s.workspaceWebhooks[wh.ID] = cloneWorkspaceWebhook(wh)
	return nil
}

// Delete removes the endpoint; the dedupe rows and delivery log die with it
// (the in-memory analogue of ON DELETE CASCADE).
func (ws *workspaceWebhookStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	if !ws.webhookExistsLocked(workspaceID, id) {
		return domain.ErrNotFound
	}

	delete(ws.s.workspaceWebhooks, id)
	delete(ws.s.workspaceWebhookDeliveries, id)
	delete(ws.s.workspaceWebhookDeliveryLog, id)
	return nil
}

// RecordDelivery inserts the dedupe key: accepted the first time, replay
// (accepted=false) afterwards — the ack-after-persist semantics design.md
// D4 pins.
func (ws *workspaceWebhookStore) RecordDelivery(ctx context.Context, workspaceID, webhookID, dedupeKey string, now time.Time) (bool, error) {
	if workspaceID == "" || webhookID == "" {
		return false, domain.ErrNotFound
	}
	if dedupeKey == "" {
		return false, domain.ErrInvalid
	}

	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	if !ws.webhookExistsLocked(workspaceID, webhookID) {
		return false, domain.ErrNotFound
	}

	deliveries, exists := ws.s.workspaceWebhookDeliveries[webhookID]
	if !exists {
		deliveries = make(map[string]time.Time)
		ws.s.workspaceWebhookDeliveries[webhookID] = deliveries
	}
	if _, seen := deliveries[dedupeKey]; seen {
		return false, nil
	}
	deliveries[dedupeKey] = now
	return true, nil
}

// AppendDeliveryLog appends one processed delivery's row: outcome validated
// through the domain, id/webhook scope/timestamp store-stamped when absent.
func (ws *workspaceWebhookStore) AppendDeliveryLog(ctx context.Context, workspaceID, webhookID string, entry domain.WebhookDeliveryLogEntry) error {
	if workspaceID == "" || webhookID == "" {
		return domain.ErrNotFound
	}
	if err := entry.Validate(); err != nil {
		return err
	}

	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	if !ws.webhookExistsLocked(workspaceID, webhookID) {
		return domain.ErrNotFound
	}

	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	entry.WebhookID = webhookID
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = ws.s.stampNow()
	}
	ws.s.workspaceWebhookDeliveryLog[webhookID] = append(ws.s.workspaceWebhookDeliveryLog[webhookID], entry)
	return nil
}

// ListDeliveryLog returns the endpoint's delivery history, newest first;
// limit <= 0 means no limit. The result is never nil.
func (ws *workspaceWebhookStore) ListDeliveryLog(ctx context.Context, workspaceID, webhookID string, limit int) ([]domain.WebhookDeliveryLogEntry, error) {
	if workspaceID == "" || webhookID == "" {
		return nil, domain.ErrNotFound
	}

	ws.s.mu.RLock()
	defer ws.s.mu.RUnlock()

	if !ws.webhookExistsLocked(workspaceID, webhookID) {
		return nil, domain.ErrNotFound
	}

	stored := ws.s.workspaceWebhookDeliveryLog[webhookID]
	entries := make([]domain.WebhookDeliveryLogEntry, len(stored))
	copy(entries, stored)
	// Newest first, id as the determinism tiebreak — the same order the
	// postgres adapter's (webhook_id, created_at DESC) index produces.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			return entries[i].ID > entries[j].ID
		}
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// FinishDeliveryRun records a submitted delivery's run telemetry on its log
// row, keyed by the run id — only submitted rows carry one. A missing or
// pruned row is a no-op: the finish is strictly best-effort.
func (ws *workspaceWebhookStore) FinishDeliveryRun(ctx context.Context, workspaceID, runID, status string, durationMS int64, tokensUsed int) error {
	if workspaceID == "" || runID == "" {
		return domain.ErrInvalid
	}

	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	for webhookID, entries := range ws.s.workspaceWebhookDeliveryLog {
		wh, exists := ws.s.workspaceWebhooks[webhookID]
		if !exists || wh.WorkspaceID != workspaceID {
			continue
		}
		for i := range entries {
			if entries[i].RunID == runID {
				entries[i].Status = status
				entries[i].DurationMS = durationMS
				entries[i].TokensUsed = tokensUsed
			}
		}
	}
	return nil
}

// ListRuns returns the workspace-wide webhook run listing: submitted
// deliveries of every endpoint in the workspace, projected with the
// endpoint name and the bound agent's id and name (resolved at read time; a
// deleted agent leaves the name blank), newest first. Legacy blank statuses
// read as completed. The result is never nil.
func (ws *workspaceWebhookStore) ListRuns(ctx context.Context, workspaceID string, limit int) ([]domain.WorkspaceWebhookRun, error) {
	if workspaceID == "" {
		return []domain.WorkspaceWebhookRun{}, nil
	}
	if limit <= 0 {
		limit = domain.WorkspaceWebhookRunsLimit
	}

	ws.s.mu.RLock()
	defer ws.s.mu.RUnlock()

	runs := make([]domain.WorkspaceWebhookRun, 0)
	for webhookID, wh := range ws.s.workspaceWebhooks {
		if wh == nil || wh.WorkspaceID != workspaceID {
			continue
		}
		for _, e := range ws.s.workspaceWebhookDeliveryLog[webhookID] {
			if e.Outcome != domain.WorkspaceWebhookDeliveryOutcomeSubmitted || e.RunID == "" {
				continue
			}
			status := e.Status
			if status == "" {
				status = domain.WorkspaceWebhookRunStatusCompleted
			}
			run := domain.WorkspaceWebhookRun{
				ID:          e.ID,
				WebhookID:   wh.ID,
				WebhookName: wh.Name,
				Event:       e.Event,
				RunID:       e.RunID,
				AgentID:     wh.TargetAgentID,
				Status:      status,
				DurationMS:  e.DurationMS,
				TokensUsed:  e.TokensUsed,
				StartedAt:   e.CreatedAt,
			}
			if a, exists := ws.s.agents[wh.TargetAgentID]; exists && a != nil && a.WorkspaceID == workspaceID {
				run.AgentName = a.Name
			}
			runs = append(runs, run)
		}
	}
	// Newest first, log row id as the determinism tiebreak — the postgres
	// adapter's (created_at DESC, id DESC) order.
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].StartedAt.Equal(runs[j].StartedAt) {
			return runs[i].ID > runs[j].ID
		}
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})
	if len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

// PruneDeliveries deletes the dedupe rows recorded strictly before the
// window start — the sweep's dedupe half.
func (ws *workspaceWebhookStore) PruneDeliveries(ctx context.Context, olderThan time.Time) error {
	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	for _, deliveries := range ws.s.workspaceWebhookDeliveries {
		for key, createdAt := range deliveries {
			if createdAt.Before(olderThan) {
				delete(deliveries, key)
			}
		}
	}
	return nil
}

// PruneDeliveryLog caps every endpoint's history at the newest
// domain.WorkspaceWebhookDeliveryLogKeep rows stamped before now — the
// sweep's log half; rows stamped at or after now are never pruned.
func (ws *workspaceWebhookStore) PruneDeliveryLog(ctx context.Context, now time.Time) error {
	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	for webhookID, entries := range ws.s.workspaceWebhookDeliveryLog {
		// Newest first, id as the tiebreak — mirroring the postgres window
		// function's ORDER BY, so both adapters keep the same rows.
		sorted := make([]domain.WebhookDeliveryLogEntry, len(entries))
		copy(sorted, entries)
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
				return sorted[i].ID > sorted[j].ID
			}
			return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
		})
		kept := make([]domain.WebhookDeliveryLogEntry, 0, len(sorted))
		for i, e := range sorted {
			if i < domain.WorkspaceWebhookDeliveryLogKeep || !e.CreatedAt.Before(now) {
				kept = append(kept, e)
			}
		}
		ws.s.workspaceWebhookDeliveryLog[webhookID] = kept
	}
	return nil
}

func cloneWorkspaceWebhook(w *domain.WorkspaceWebhook) *domain.WorkspaceWebhook {
	if w == nil {
		return nil
	}
	cp := *w
	return &cp
}
