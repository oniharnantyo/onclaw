package channels

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestHub_SubscribeBroadcastCancel(t *testing.T) {
	hub := NewHub()

	ch1, cancel1 := hub.Subscribe("ch1")
	ch2, cancel2 := hub.Subscribe("ch1")
	chOther, _ := hub.Subscribe("ch2")
	defer cancel1()
	defer cancel2()

	hub.Broadcast("ch1", Event{Type: EventMessagePosted, Seq: 7})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Type != EventMessagePosted || ev.Seq != 7 {
				t.Fatalf("got event %+v, want message_posted seq 7", ev)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber did not receive the broadcast")
		}
	}
	select {
	case ev := <-chOther:
		t.Fatalf("other-channel subscriber received %+v", ev)
	default:
	}
}

func TestHub_CancelUnsubscribesAndCloses(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe("ch1")

	cancel()
	cancel() // idempotent: second call must not panic on double close

	if _, open := <-ch; open {
		t.Fatal("cancel must close the subscriber channel")
	}

	hub.Broadcast("ch1", Event{Type: EventRunStarted}) // no subscribers: no panic
}

func TestHub_BroadcastNeverBlocksOnSlowSubscriber(t *testing.T) {
	hub := NewHub()
	_, cancel := hub.Subscribe("ch1")
	defer cancel()

	// Fill the buffer past capacity; broadcast must return immediately.
	done := make(chan struct{})
	go func() {
		for i := 0; i < hubSubscriberBuffer+10; i++ {
			hub.Broadcast("ch1", Event{Type: EventRunStarted, Seq: int64(i)})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("broadcast blocked on a slow subscriber")
	}
}

func TestEventPayloadShapes(t *testing.T) {
	// The SSE wire shapes are a pinned contract with the web client — assert
	// the marshaled frames field by field.

	messagePosted := newMessagePostedEvent(domain.ChannelMessage{
		WorkspaceID:   "ws1",
		ChannelID:     "ch1",
		Seq:           42,
		AuthorType:    domain.ChannelMemberTypeAgent,
		AuthorAgentID: "agent-1",
		Body:          "hello",
		Mentions:      []domain.Mention{},
	})
	data, err := json.Marshal(messagePosted)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Type    string `json:"type"`
		Seq     int64  `json:"seq"`
		Payload struct {
			Message domain.ChannelMessage `json:"message"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Type != EventMessagePosted || wire.Seq != 42 {
		t.Fatalf("message_posted frame = %s", data)
	}
	if wire.Payload.Message.Seq != 42 || wire.Payload.Message.Body != "hello" || wire.Payload.Message.AuthorAgentID != "agent-1" {
		t.Fatalf("message_posted payload message = %+v", wire.Payload.Message)
	}

	considering := newSummonConsideringEvent("ch1", "agent-1")
	var consideringShape struct {
		ChannelID string `json:"channel_id"`
		AgentID   string `json:"agent_id"`
	}
	if err := json.Unmarshal(considering.Payload, &consideringShape); err != nil {
		t.Fatal(err)
	}
	if consideringShape.ChannelID != "ch1" || consideringShape.AgentID != "agent-1" {
		t.Fatalf("considering payload = %+v", consideringShape)
	}
	if considering.Seq != 0 || considering.Type != EventSummonConsidering {
		t.Fatalf("considering frame header = %+v", considering)
	}

	decided := newSummonDecidedEvent("ch1", "agent-1", false, "not my domain")
	var decidedShape struct {
		ChannelID string `json:"channel_id"`
		AgentID   string `json:"agent_id"`
		Engage    bool   `json:"engage"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(decided.Payload, &decidedShape); err != nil {
		t.Fatal(err)
	}
	if decidedShape.Engage || decidedShape.Reason != "not my domain" {
		t.Fatalf("decided payload = %+v", decidedShape)
	}

	started := newRunStatusEvent(EventRunStarted, "ch1", "agent-1", "chan_ch1_agent-1", RunStatusRunning)
	var runShape struct {
		ChannelID string `json:"channel_id"`
		AgentID   string `json:"agent_id"`
		SessionID string `json:"session_id"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(started.Payload, &runShape); err != nil {
		t.Fatal(err)
	}
	if runShape.ChannelID != "ch1" || runShape.AgentID != "agent-1" ||
		runShape.SessionID != "chan_ch1_agent-1" || runShape.Status != RunStatusRunning {
		t.Fatalf("run payload = %+v", runShape)
	}
}
