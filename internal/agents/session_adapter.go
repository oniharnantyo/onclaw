package agents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// eventSerializer serializes typed ADK session events for durable storage.
// It uses Eino's HumanReadableSerializer (not plain encoding/json) so that
// registered concrete types stored behind interface{} fields — session
// extension payloads (Extension.Data) and envelope Extra values — survive a
// write/read round-trip as their original types instead of degrading to
// map[string]any. This matches the serializer contract Eino's own session
// stores honor, and is what lets the adk/session conformance suite compare
// reloaded events with reflect.DeepEqual.
var eventSerializer = &schema.HumanReadableSerializer{}

// ADKSessionAdapter adapts the domain store's session event and checkpoint
// stores to the Eino ADK session interface. It is Eino-type-aware but the
// domain store layer stays Eino-free (opaque serialized payloads).
type ADKSessionAdapter struct {
	eventStore  storeport.SessionEventStore
	cpStore     storeport.SessionCheckpointStore
	workspaceID string
}

// NewADKSessionAdapter creates a new adapter that backs Eino session operations
// against the given domain store ports.
func NewADKSessionAdapter(
	eventStore storeport.SessionEventStore,
	cpStore storeport.SessionCheckpointStore,
	workspaceID string,
) *ADKSessionAdapter {
	return &ADKSessionAdapter{
		eventStore:  eventStore,
		cpStore:     cpStore,
		workspaceID: workspaceID,
	}
}

// AppendEvents serializes typed ADK session events into the domain store.
// Duplicate EventIDs (cross-call) return adk.ErrDuplicateEventID.
// Empty EventIDs return adk.ErrInvalidEventID.
func (a *ADKSessionAdapter) AppendEvents(ctx context.Context, sessionID string, events []*adk.SessionEvent[*schema.AgenticMessage]) error {
	if len(events) == 0 {
		return nil
	}

	// Validate: reject empty EventIDs and within-batch duplicates.
	seenIDs := make(map[string]struct{}, len(events))
	for _, e := range events {
		if e == nil || e.EventID == "" {
			return adk.ErrInvalidEventID
		}
		if _, dup := seenIDs[e.EventID]; dup {
			return adk.ErrDuplicateEventID
		}
		seenIDs[e.EventID] = struct{}{}
	}

	// Load existing event IDs for duplicate detection against the store.
	existing, err := a.eventStore.LoadEvents(ctx, storeport.LoadSessionEventsParams{
		WorkspaceID: a.workspaceID,
		SessionID:   sessionID,
		Limit:       0, // all
	})
	if err != nil {
		return fmt.Errorf("ADKSessionAdapter.AppendEvents: load existing: %w", err)
	}
	existingIDs := make(map[string]struct{}, len(existing))
	for _, ex := range existing {
		existingIDs[ex.EventID] = struct{}{}
	}

	// Determine next seq (domain store seqs are append positions).
	nextSeq := int64(len(existing))

	var domainEvents []domain.SessionEvent
	for _, e := range events {
		if _, dup := existingIDs[e.EventID]; dup {
			return adk.ErrDuplicateEventID
		}

		if err := adk.NormalizeSessionEventKind(e); err != nil {
			return fmt.Errorf("ADKSessionAdapter.AppendEvents: normalize kind: %w", err)
		}

		payload, err := eventSerializer.Marshal(e)
		if err != nil {
			return fmt.Errorf("ADKSessionAdapter.AppendEvents: marshal event %q: %w", e.EventID, err)
		}

		occurredAt := e.Timestamp
		if occurredAt.IsZero() {
			occurredAt = time.Now().UTC()
		}

		domainEvents = append(domainEvents, domain.SessionEvent{
			SessionID:   sessionID,
			EventID:     e.EventID,
			TurnID:      e.TurnID,
			Seq:         nextSeq,
			Kind:        string(e.Kind),
			Payload:     payload,
			OccurredAt:  occurredAt,
			WorkspaceID: a.workspaceID,
		})
		nextSeq++
	}

	return a.eventStore.AppendEvents(ctx, a.workspaceID, domainEvents)
}

// LoadEvents deserializes domain store rows back into typed ADK session events.
func (a *ADKSessionAdapter) LoadEvents(ctx context.Context, sessionID string, req *adk.LoadSessionEventsRequest) (*adk.LoadSessionEventsResult[*schema.AgenticMessage], error) {
	if req == nil {
		req = &adk.LoadSessionEventsRequest{}
	}

	// Map kind filter.
	var kinds []string
	for _, k := range req.Kinds {
		kinds = append(kinds, string(k))
	}

	params := storeport.LoadSessionEventsParams{
		WorkspaceID: a.workspaceID,
		SessionID:   sessionID,
		Limit:       req.Limit,
		Reverse:     req.Reverse,
		Kinds:       kinds,
	}

	// Resolve cursor (After = event_id, not seq).
	if req.After != "" {
		// We pass AfterEventID directly; the store resolves the seq boundary.
		params.AfterEventID = req.After
	}

	rows, err := a.eventStore.LoadEvents(ctx, params)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// Cursor event ID not found → ErrEventIDOutOfRange.
			return nil, adk.ErrEventIDOutOfRange
		}
		return nil, fmt.Errorf("ADKSessionAdapter.LoadEvents: %w", err)
	}

	// Deserialize.
	events := make([]*adk.SessionEvent[*schema.AgenticMessage], 0, len(rows))
	for _, row := range rows {
		var se adk.SessionEvent[*schema.AgenticMessage]
		if err := eventSerializer.Unmarshal(row.Payload, &se); err != nil {
			return nil, fmt.Errorf("ADKSessionAdapter.LoadEvents: unmarshal event %q: %w", row.EventID, err)
		}
		events = append(events, &se)
	}

	// Compute next cursor: the EventID of the last item in the page.
	var next string
	if req.Limit > 0 && len(events) >= req.Limit {
		// There may be more; the next cursor is the last event's ID.
		next = events[len(events)-1].EventID
	}

	return &adk.LoadSessionEventsResult[*schema.AgenticMessage]{
		Events: events,
		Next:   next,
	}, nil
}

// Get implements adk.CheckPointStore for interrupt checkpoints.
func (a *ADKSessionAdapter) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	return a.cpStore.Get(ctx, checkPointID)
}

// Set implements adk.CheckPointStore for interrupt checkpoints.
func (a *ADKSessionAdapter) Set(ctx context.Context, checkPointID string, data []byte) error {
	return a.cpStore.Set(ctx, checkPointID, data)
}

// Delete implements adk.CheckPointDeleter for post-run cleanup. The ADK
// runner deletes checkpoints unconditionally after completion, so a missing
// checkpoint is a no-op rather than an error.
func (a *ADKSessionAdapter) Delete(ctx context.Context, checkPointID string) error {
	if err := a.cpStore.Delete(ctx, checkPointID); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	return nil
}
