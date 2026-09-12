package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

	// Allocate the batch's starting sequence from the store's append
	// position (MAX(seq)+1) instead of reading the whole history.
	//
	// Concurrency assumption (fix-session-event-ordering D5): sequence
	// allocation assumes per-session serialization of appends — turn
	// execution holds a per-run lock, and compaction runs as-a-turn. Two
	// concurrent appends to one session would race this read-modify-write
	// count; that is unreachable today, so this comment records the
	// assumption rather than adding locking machinery.
	nextSeq, err := a.eventStore.NextEventSeq(ctx, a.workspaceID, sessionID)
	if err != nil {
		return fmt.Errorf("ADKSessionAdapter.AppendEvents: next seq: %w", err)
	}

	var domainEvents []domain.SessionEvent
	for _, e := range events {
		// Cross-call duplicate detection without the full pre-read (D4): a
		// single indexed existence probe per candidate event. Batches are
		// small, so per-event probes stay cheap; the store's
		// ON CONFLICT (session_id, event_id) DO NOTHING remains the
		// last-line idempotency guarantee.
		exists, err := a.eventStore.EventExists(ctx, a.workspaceID, sessionID, e.EventID)
		if err != nil {
			return fmt.Errorf("ADKSessionAdapter.AppendEvents: probe event %q: %w", e.EventID, err)
		}
		if exists {
			return adk.ErrDuplicateEventID
		}

		if err := adk.NormalizeSessionEventKind(e); err != nil {
			return fmt.Errorf("ADKSessionAdapter.AppendEvents: normalize kind: %w", err)
		}

		// Attachment bytes never persist (attachments design D6): user
		// messages carrying inline bytes are demoted to URL-only references
		// before serialization. The demotion is a clone — the ADK still holds
		// the caller's event and message for the running turn.
		persisted := demoteAttachmentBytes(e)

		payload, err := eventSerializer.Marshal(persisted)
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

// demoteAttachmentBytes returns a shallow-cloned copy of the event whose user
// message's image and file blocks carry no inline bytes (attachments design
// D6): Base64Data is cleared while the capability URL, Extra identity
// (name/mime/size), and every other block survive untouched. The input event
// and message are never mutated — the ADK still holds them for the running
// turn. Events without byte-carrying user blocks return unchanged (aliased),
// so the zero-attachment path pays a type test and nothing else.
func demoteAttachmentBytes(e *adk.SessionEvent[*schema.AgenticMessage]) *adk.SessionEvent[*schema.AgenticMessage] {
	msg := e.Message
	if msg == nil || strings.ToLower(strings.TrimSpace(string(msg.Role))) != string(schema.AgenticRoleTypeUser) {
		return e
	}

	demoted := false
	blocks := make([]*schema.ContentBlock, len(msg.ContentBlocks))
	for i, block := range msg.ContentBlocks {
		switch {
		case block != nil && block.UserInputImage != nil && block.UserInputImage.Base64Data != "":
			b := *block
			img := *block.UserInputImage
			img.Base64Data = ""
			b.UserInputImage = &img
			blocks[i] = &b
			demoted = true
		case block != nil && block.UserInputFile != nil && block.UserInputFile.Base64Data != "":
			b := *block
			file := *block.UserInputFile
			file.Base64Data = ""
			b.UserInputFile = &file
			blocks[i] = &b
			demoted = true
		default:
			blocks[i] = block
		}
	}
	if !demoted {
		return e
	}

	clone := *e
	m := *msg
	m.ContentBlocks = blocks
	clone.Message = &m
	return &clone
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
