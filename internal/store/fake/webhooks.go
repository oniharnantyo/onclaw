package fake

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/oniharnantyo/buddies/internal/store"
)

type webhookStore struct{ s *Store }

// SaveEndpoint upserts one endpoint: an absent id inserts (id assigned,
// timestamps stamped), a present id rewrites the definition columns keeping
// the original CreatedAt. The name is unique among endpoints
// case-insensitively (mirroring the NOCASE unique index); keeping the
// endpoint's own name is fine. Verifiers are not written here.
func (f webhookStore) SaveEndpoint(_ context.Context, ep *store.WebhookEndpoint) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if (ep.AgentID == "") == (ep.GroupID == "") {
		return fmt.Errorf("save webhook endpoint: exactly one of agent id or group id must be set")
	}
	for id, other := range f.s.webhookEndpoints {
		if id != ep.ID && strings.EqualFold(other.Name, ep.Name) {
			return fmt.Errorf("save webhook endpoint %s: %w", ep.ID, store.ErrDuplicate)
		}
	}
	if ep.ID == "" {
		ep.ID = nextID()
	}
	if stored, ok := f.s.webhookEndpoints[ep.ID]; ok {
		ep.CreatedAt = stored.CreatedAt
	} else if ep.CreatedAt.IsZero() {
		ep.CreatedAt = nowUTC()
	}
	if ep.UpdatedAt.IsZero() {
		ep.UpdatedAt = nowUTC()
	}
	f.s.webhookEndpoints[ep.ID] = *ep
	return nil
}

// GetEndpoint returns one endpoint with its verifiers ordered by position;
// ErrNotFound when absent.
func (f webhookStore) GetEndpoint(_ context.Context, id string) (*store.WebhookEndpoint, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	ep, ok := f.s.webhookEndpoints[id]
	if !ok {
		return nil, fmt.Errorf("get webhook endpoint %s: %w", id, store.ErrNotFound)
	}
	ep.Verifiers = cloneWebhookVerifiers(f.s.webhookVerifiers[id])
	return &ep, nil
}

// ListEndpoints returns every endpoint with its verifiers, ordered by name
// case-insensitively. The result is never nil.
func (f webhookStore) ListEndpoints(_ context.Context) ([]store.WebhookEndpoint, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	out := make([]store.WebhookEndpoint, 0, len(f.s.webhookEndpoints))
	for _, ep := range f.s.webhookEndpoints {
		ep.Verifiers = cloneWebhookVerifiers(f.s.webhookVerifiers[ep.ID])
		out = append(out, ep)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		// Case-insensitive ties fall back to the raw name, mirroring the
		// NOCASE sort's stable secondary behavior.
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// DeleteEndpoint removes one endpoint; its verifiers and delivery log go
// with it (mirroring the FK cascade). ErrNotFound when absent.
func (f webhookStore) DeleteEndpoint(_ context.Context, id string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if _, ok := f.s.webhookEndpoints[id]; !ok {
		return fmt.Errorf("delete webhook endpoint %s: %w", id, store.ErrNotFound)
	}
	delete(f.s.webhookEndpoints, id)
	delete(f.s.webhookVerifiers, id)
	delete(f.s.webhookDeliveries, id)
	return nil
}

// SaveVerifiers replaces one endpoint's whole verifier list in slice order;
// every earlier row (secrets included) is dropped; an empty list clears.
// ErrNotFound when the endpoint is absent.
func (f webhookStore) SaveVerifiers(_ context.Context, endpointID string, verifiers []store.WebhookVerifier) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if _, ok := f.s.webhookEndpoints[endpointID]; !ok {
		return fmt.Errorf("save webhook verifiers %s: %w", endpointID, store.ErrNotFound)
	}
	stored := make([]store.WebhookVerifier, 0, len(verifiers))
	for i := range verifiers {
		v := verifiers[i]
		if v.ID == "" {
			v.ID = nextID()
		}
		v.EndpointID = endpointID
		v.Position = i
		if v.CreatedAt.IsZero() {
			v.CreatedAt = nowUTC()
		}
		if v.UpdatedAt.IsZero() {
			v.UpdatedAt = nowUTC()
		}
		stored = append(stored, v)
	}
	f.s.webhookVerifiers[endpointID] = stored
	return nil
}

// AppendDelivery records one delivery attempt and enforces the per-endpoint
// retention bound: rows beyond store.WebhookDeliveryRetention are evicted
// oldest-first (mirroring the sqlite append-path eviction).
func (f webhookStore) AppendDelivery(_ context.Context, d *store.WebhookDelivery) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if _, ok := f.s.webhookEndpoints[d.EndpointID]; !ok {
		return fmt.Errorf("append webhook delivery %s: %w", d.EndpointID, store.ErrNotFound)
	}
	if d.ID == "" {
		d.ID = nextID()
	}
	if d.AttemptedAt.IsZero() {
		d.AttemptedAt = nowUTC()
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = nowUTC()
	}

	log := append(f.s.webhookDeliveries[d.EndpointID], *d)
	sort.Slice(log, func(i, j int) bool {
		if !log[i].AttemptedAt.Equal(log[j].AttemptedAt) {
			return log[i].AttemptedAt.After(log[j].AttemptedAt)
		}
		return log[i].CreatedAt.After(log[j].CreatedAt)
	})
	if len(log) > store.WebhookDeliveryRetention {
		log = log[:store.WebhookDeliveryRetention]
	}
	f.s.webhookDeliveries[d.EndpointID] = log
	return nil
}

// ListDeliveries returns one endpoint's delivery log newest-first. The
// result is never nil; ErrNotFound when the endpoint is absent.
func (f webhookStore) ListDeliveries(_ context.Context, endpointID string) ([]store.WebhookDelivery, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if _, ok := f.s.webhookEndpoints[endpointID]; !ok {
		return nil, fmt.Errorf("list webhook deliveries %s: %w", endpointID, store.ErrNotFound)
	}
	out := make([]store.WebhookDelivery, len(f.s.webhookDeliveries[endpointID]))
	copy(out, f.s.webhookDeliveries[endpointID])
	return out, nil
}

// cloneWebhookVerifiers copies a stored verifier slice so callers never
// alias the fake's internal state.
func cloneWebhookVerifiers(in []store.WebhookVerifier) []store.WebhookVerifier {
	out := make([]store.WebhookVerifier, len(in))
	copy(out, in)
	return out
}
