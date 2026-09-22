package fake

import (
	"context"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// connectionWebhookStore implements store.ConnectionWebhookStore in memory:
// the webhook columns of the connection row live in the shared connection
// webhook map, the dedupe window in a per-connection delivery map. Every
// mutation runs under the store lock — the in-memory analogue of the postgres
// adapter's single statement.
type connectionWebhookStore struct {
	s *fakeStore
}

// connectionExistsLocked reports whether the connection id exists in the
// given workspace. Callers hold the store lock.
func (cs *connectionWebhookStore) connectionExistsLocked(workspaceID, connectionID string) bool {
	c, exists := cs.s.connections[connectionID]
	return exists && c.WorkspaceID == workspaceID
}

// GetState reads the connection's webhook state. A connection that never
// enabled webhooks reads back as the inert default — the shape the columns'
// defaults produce (tasks.md 1.4).
func (cs *connectionWebhookStore) GetState(ctx context.Context, workspaceID, connectionID string) (*domain.ConnectionWebhook, error) {
	if workspaceID == "" || connectionID == "" {
		return nil, domain.ErrNotFound
	}

	cs.s.mu.RLock()
	defer cs.s.mu.RUnlock()

	if !cs.connectionExistsLocked(workspaceID, connectionID) {
		return nil, domain.ErrNotFound
	}
	if stored, exists := cs.s.connectionWebhooks[connectionID]; exists {
		return cloneConnectionWebhook(stored), nil
	}
	return &domain.ConnectionWebhook{
		WorkspaceID:  workspaceID,
		ConnectionID: connectionID,
		Events:       []string{},
	}, nil
}

// UpdateState writes the full webhook state and bumps the connection's
// updated_at — the in-memory analogue of the postgres adapter's single
// UPDATE ... WHERE workspace_id = $1 AND id = $2.
func (cs *connectionWebhookStore) UpdateState(ctx context.Context, state *domain.ConnectionWebhook) error {
	if state == nil {
		return domain.ErrInvalid
	}
	if err := state.Validate(); err != nil {
		return err
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	if !cs.connectionExistsLocked(state.WorkspaceID, state.ConnectionID) {
		return domain.ErrNotFound
	}

	cs.s.connectionWebhooks[state.ConnectionID] = cloneConnectionWebhook(state)
	if c, exists := cs.s.connections[state.ConnectionID]; exists {
		c.UpdatedAt = time.Now().UTC()
	}
	return nil
}

// RecordDelivery inserts the delivery id: accepted the first time, replay
// (accepted=false) afterwards — the ack-after-persist semantics design.md D3
// pins.
func (cs *connectionWebhookStore) RecordDelivery(ctx context.Context, workspaceID, connectionID, deliveryID string, now time.Time) (bool, error) {
	if workspaceID == "" || connectionID == "" {
		return false, domain.ErrNotFound
	}
	if deliveryID == "" {
		return false, domain.ErrInvalid
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	if !cs.connectionExistsLocked(workspaceID, connectionID) {
		return false, domain.ErrNotFound
	}

	deliveries, exists := cs.s.connectionDeliveries[connectionID]
	if !exists {
		deliveries = make(map[string]time.Time)
		cs.s.connectionDeliveries[connectionID] = deliveries
	}
	if _, seen := deliveries[deliveryID]; seen {
		return false, nil
	}
	deliveries[deliveryID] = now
	return true, nil
}

// PruneDeliveries deletes the delivery rows recorded strictly before the
// window start and reports the removal count.
func (cs *connectionWebhookStore) PruneDeliveries(ctx context.Context, before time.Time) (int64, error) {
	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	var pruned int64
	for _, deliveries := range cs.s.connectionDeliveries {
		for id, createdAt := range deliveries {
			if createdAt.Before(before) {
				delete(deliveries, id)
				pruned++
			}
		}
	}
	return pruned, nil
}

func cloneConnectionWebhook(w *domain.ConnectionWebhook) *domain.ConnectionWebhook {
	if w == nil {
		return nil
	}
	cp := *w
	// Events always leaves the store as an array, never null (the
	// served-JSON normalization the connection stores apply too).
	if cp.Events == nil {
		cp.Events = []string{}
	} else {
		cp.Events = append([]string(nil), w.Events...)
	}
	return &cp
}
