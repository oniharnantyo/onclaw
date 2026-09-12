package channels

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// SSE + hub event types (integrate-agent-channels D11). These are wire
// contract names — the web client's SSE consumer codes against them verbatim.
const (
	EventMessagePosted     = "message_posted"
	EventSummonConsidering = "summon_considering"
	EventSummonDecided     = "summon_decided"
	EventRunStarted        = "run_started"
	EventRunFinished       = "run_finished"
	// EventSessionUpdated carries a work session lifecycle transition
	// (channel-teams D1): kickoff, pause (awaiting-human / budget-exhausted),
	// resume, close. Seq is always 0 — the session is not a feed row.
	EventSessionUpdated = "session_updated"
)

// Run statuses carried on run_started / run_finished payloads.
const (
	RunStatusRunning   = "running"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
	RunStatusCancelled = "cancelled"
)

// Event is one broadcast frame on a channel's SSE stream. Seq mirrors the
// feed seq for message_posted (0 otherwise) so clients can dedup stream
// frames against the REST feed load.
type Event struct {
	Type    string          `json:"type"`
	Seq     int64           `json:"seq"`     // feed seq for message_posted, 0 otherwise
	Payload json.RawMessage `json:"payload"` // snake_case JSON, shapes below
}

// Wire payload shapes (snake_case — a parallel web worker codes against
// these verbatim):
//
//	message_posted:     {"seq":N,"message":{...domain.ChannelMessage as JSON...}}
//	summon_considering: {"channel_id":"…","agent_id":"…"}
//	summon_decided:     {"channel_id":"…","agent_id":"…","engage":true,"reason":"…"}
//	run_started/finished: {"channel_id":"…","agent_id":"…","session_id":"…","status":"…"}
//	session_updated:    {"session":{...domain.WorkSession as JSON...}}

type messagePostedPayload struct {
	Message domain.ChannelMessage `json:"message"`
}

type summonConsideringPayload struct {
	ChannelID string `json:"channel_id"`
	AgentID   string `json:"agent_id"`
}

type summonDecidedPayload struct {
	ChannelID string `json:"channel_id"`
	AgentID   string `json:"agent_id"`
	Engage    bool   `json:"engage"`
	Reason    string `json:"reason"`
}

type runStatusPayload struct {
	ChannelID string `json:"channel_id"`
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

type sessionUpdatedPayload struct {
	Session domain.WorkSession `json:"session"`
}

// hubSubscriberBuffer bounds each subscriber's queue. Broadcast uses
// drop-new semantics (the EventStream precedent): a slow or stalled SSE
// consumer must never stall the message pipeline — the client recovers the
// gap by refetching the feed and deduping on seq.
const hubSubscriberBuffer = 64

// Hub fans pipeline events out to per-channel SSE subscribers. Subscribers
// are anonymous buffered channels; the returned cancel func unsubscribes and
// closes the channel so the HTTP stream handler's range loop terminates.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

// NewHub creates an empty hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan Event]struct{})}
}

// Subscribe registers a subscriber for one channel's events and returns its
// receive side plus a cancel func. Cancel is idempotent; after it runs, the
// channel is closed and drained.
func (h *Hub) Subscribe(channelID string) (<-chan Event, func()) {
	ch := make(chan Event, hubSubscriberBuffer)

	h.mu.Lock()
	if h.subs[channelID] == nil {
		h.subs[channelID] = make(map[chan Event]struct{})
	}
	h.subs[channelID][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			if set, exists := h.subs[channelID]; exists {
				delete(set, ch)
				if len(set) == 0 {
					delete(h.subs, channelID)
				}
			}
			h.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Broadcast delivers one event to every current subscriber of the channel,
// never blocking: a subscriber whose buffer is full misses the frame (the
// REST load plus seq dedup is the recovery path).
func (h *Hub) Broadcast(channelID string, ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[channelID] {
		select {
		case ch <- ev:
		default:
		}
	}
}

// ----- payload builders (used by the chokepoint / fan-out) -----

func newMessagePostedEvent(msg domain.ChannelMessage) Event {
	return Event{
		Type:    EventMessagePosted,
		Seq:     msg.Seq,
		Payload: mustPayload(messagePostedPayload{Message: msg}),
	}
}

func newSummonConsideringEvent(channelID, agentID string) Event {
	return Event{
		Type:    EventSummonConsidering,
		Payload: mustPayload(summonConsideringPayload{ChannelID: channelID, AgentID: agentID}),
	}
}

func newSummonDecidedEvent(channelID, agentID string, engage bool, reason string) Event {
	return Event{
		Type:    EventSummonDecided,
		Payload: mustPayload(summonDecidedPayload{ChannelID: channelID, AgentID: agentID, Engage: engage, Reason: reason}),
	}
}

func newRunStatusEvent(eventType, channelID, agentID, sessionID, status string) Event {
	return Event{
		Type: eventType,
		Payload: mustPayload(runStatusPayload{
			ChannelID: channelID,
			AgentID:   agentID,
			SessionID: sessionID,
			Status:    status,
		}),
	}
}

func newSessionUpdatedEvent(session domain.WorkSession) Event {
	return Event{
		Type:    EventSessionUpdated,
		Seq:     0,
		Payload: mustPayload(sessionUpdatedPayload{Session: session}),
	}
}

// mustPayload marshals a static payload struct. The shapes are structs over
// JSON-native fields, so marshaling cannot fail at runtime; a panic here is
// a programming error, not a runtime path.
func mustPayload(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("channels: marshal event payload: %v", err))
	}
	return data
}
