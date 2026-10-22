package sessions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Append persists events to the session's log and advances its last
// activity, atomically where the store supports it. Event IDs and occurrence
// times are assigned here in place: the live stream can carry the same
// identity the log records.
func (s *service) Append(ctx context.Context, sessionID string, events []Event) error {
	if len(events) == 0 {
		return nil
	}

	now := time.Now().UTC()
	rows := make([]store.SessionEvent, 0, len(events))
	for i := range events {
		ev := &events[i]
		// The running tool state is live-only (design D10): persisting it
		// would corrupt replay with a spinner that never resolves, so Append
		// rejects it instead of silently storing a half-record.
		if ev.Kind == KindToolActivity && !ev.Tool.Terminal() {
			return fmt.Errorf("append session events: %s: tool call %s is still running; persist the terminal record only",
				ev.Kind, ev.Tool.CallID)
		}
		// Delegated tasks follow the same rule: a running task card is
		// live-only, its terminal record is what the transcript holds.
		if ev.Kind == KindTaskActivity && !ev.Task.Terminal() {
			return fmt.Errorf("append session events: %s: task %s is still running; persist the terminal record only",
				ev.Kind, ev.Task.TaskID)
		}
		if ev.ID == "" {
			id, err := newEventID()
			if err != nil {
				return fmt.Errorf("append session events: %w", err)
			}
			ev.ID = id
		}
		if ev.OccurredAt.IsZero() {
			ev.OccurredAt = now
		}
		payload, err := json.Marshal(eventPayload{
			Text:        ev.Text,
			Attachments: ev.Attachments,
			VariantOf:   ev.VariantOf,
			Tool:        ev.Tool,
			Task:        ev.Task,
			Usage:       ev.Usage,
			Error:       ev.Error,
			Compaction:  ev.Compaction,
			Approval:    ev.Approval,
		})
		if err != nil {
			return fmt.Errorf("append session events: marshal %s: %w", ev.Kind, err)
		}
		rows = append(rows, store.SessionEvent{
			EventID:    ev.ID,
			TurnID:     ev.TurnID,
			Kind:       string(ev.Kind),
			Payload:    payload,
			OccurredAt: ev.OccurredAt,
		})
	}

	err := s.store.WithTx(ctx, func(tx store.Store) error {
		if err := tx.SessionEvents().AppendEvents(ctx, sessionID, rows); err != nil {
			return err
		}
		return tx.Sessions().Touch(ctx, sessionID, now)
	})
	if err != nil {
		return fmt.Errorf("append session events: %w", err)
	}
	return nil
}

// Events replays the session's log in order, decoded. Events written by a
// newer version with unknown kinds are skipped rather than failing the whole
// transcript.
func (s *service) Events(ctx context.Context, sessionID string) ([]Event, error) {
	rows, err := s.store.SessionEvents().LoadEvents(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session events: %w", err)
	}
	return decodeEvents(rows)
}

// EventsPage replays one window of the session's log (design D6): the newest
// limit decoded events with Seq strictly below beforeSeq — or the newest page
// when beforeSeq is 0 — in log order. A non-positive limit takes
// DefaultPageLimit; absurd limits are clamped to MaxPageLimit.
func (s *service) EventsPage(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = DefaultPageLimit
	}
	if limit > MaxPageLimit {
		limit = MaxPageLimit
	}
	rows, err := s.store.SessionEvents().LoadEventsPage(ctx, sessionID, beforeSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("load session events page: %w", err)
	}
	return decodeEvents(rows)
}

// decodeEvents maps raw log rows onto decoded events, skipping kinds a newer
// version introduced (the transcript never fails on forward-compatible
// unknowns).
func decodeEvents(rows []store.SessionEvent) ([]Event, error) {
	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		kind := Kind(row.Kind)
		if !knownKind(kind) {
			continue
		}
		var payload eventPayload
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			return nil, fmt.Errorf("load session events: decode %s: %w", row.EventID, err)
		}
		events = append(events, Event{
			ID:          row.EventID,
			Seq:         row.Seq,
			TurnID:      row.TurnID,
			Kind:        kind,
			OccurredAt:  row.OccurredAt,
			Text:        payload.Text,
			Attachments: payload.Attachments,
			VariantOf:   payload.VariantOf,
			Tool:        payload.Tool,
			Task:        payload.Task,
			Usage:       payload.Usage,
			Error:       payload.Error,
			Compaction:  payload.Compaction,
			Approval:    payload.Approval,
		})
	}
	return events, nil
}

// eventPayload is the JSON body of the store row's payload column: the
// kind-specific fields of an Event, without the identity the row columns
// already carry.
type eventPayload struct {
	Text        string        `json:"text,omitempty"`
	Attachments []Attachment  `json:"attachments,omitempty"`
	VariantOf   string        `json:"variant_of,omitempty"`
	Tool        *ToolActivity `json:"tool,omitempty"`
	Task        *TaskActivity `json:"task,omitempty"`
	Usage       *Usage        `json:"usage,omitempty"`
	Error       string        `json:"error,omitempty"`
	Compaction  *Compaction   `json:"compaction,omitempty"`
	Approval    *Approval     `json:"approval,omitempty"`
}

func knownKind(k Kind) bool {
	switch k {
	case KindUserMessage, KindTextDelta, KindReasoningDelta, KindAssistantMessage,
		KindReasoning, KindToolActivity, KindTaskActivity, KindTurnCompleted, KindTurnError,
		KindTurnCancelled, KindApprovalRequired, KindApprovalResolved, KindCompaction:
		return true
	default:
		return false
	}
}

// Page-sizing constants for the transcript window (design D6, spec
// chat-view "Transcript windowing"): the default page the chat view opens a
// session with, and the clamp bounding a caller-supplied limit.
const (
	// DefaultPageLimit is the newest-page size TranscriptPage serves when
	// the caller passes no explicit limit.
	DefaultPageLimit = 60
	// MaxPageLimit bounds a caller-supplied page size.
	MaxPageLimit = 200
)

// newEventID returns a random 128-bit hex id, mirroring the store's own
// id convention (the event log is keyed by (session, event id)).
func newEventID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate event id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
