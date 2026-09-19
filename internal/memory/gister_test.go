package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

const gistResponse = `{"description":"The deploy window was set to Tuesdays.","outcome":"Deploy window confirmed for Tuesdays."}`

func gistWindowEvents(t *testing.T) []domain.SessionEvent {
	t.Helper()
	base := time.Now().UTC().Add(-time.Hour)
	return []domain.SessionEvent{
		chatEvent(t, "e1", "turn-1", 1, base, schema.AgenticRoleTypeUser, "Reminder: the deploy window is Tuesdays."),
		chatEvent(t, "e2", "turn-1", 2, base.Add(time.Minute), schema.AgenticRoleTypeAssistant, "Noted, Tuesdays it is."),
	}
}

// TestGisterParticipantRule (D4): no human → agent-visibility, exactly one
// → user-visibility owned by that user, two or more → shared.
func TestGisterParticipantRule(t *testing.T) {
	cases := []struct {
		name       string
		humans     int
		origin     string
		wantVis    domain.MemoryVisibility
		wantUserID bool // owner = the job's user
	}{
		{"scheduled run has no human", 0, originScheduler, domain.MemoryVisibilityAgent, false},
		{"direct chat has one human", 1, originUser, domain.MemoryVisibilityUser, true},
		{"telegram DM has one human", 1, originTelegram, domain.MemoryVisibilityUser, true},
		{"channel has two humans", 2, originChannel, domain.MemoryVisibilityShared, false},
		{"channel with three humans", 3, originChannel, domain.MemoryVisibilityShared, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := seedWorld(t)
			events := gistWindowEvents(t)
			appendEvents(t, s, events...)
			gister := newTestGister(s, &scriptedModel{responses: []string{gistResponse}})

			job := testJob()
			job.Origin = tc.origin
			job.HumanParticipants = tc.humans

			win, err := gister.Window(context.Background(), job)
			if err != nil {
				t.Fatalf("window: %v", err)
			}
			gist, err := gister.Gist(context.Background(), job, win)
			if err != nil {
				t.Fatalf("gist: %v", err)
			}
			if gist.Visibility != tc.wantVis {
				t.Fatalf("participant rule: got %q want %q", gist.Visibility, tc.wantVis)
			}
			if tc.wantVis == domain.MemoryVisibilityUser {
				if gist.UserID == nil || *gist.UserID != testUserID {
					t.Fatalf("user-visibility gist must be owned by the human, got %+v", gist.UserID)
				}
			} else if gist.UserID != nil {
				t.Fatalf("%s gist must not carry a user owner, got %+v", tc.wantVis, gist.UserID)
			}
			if gist.AgentID != testAgentID {
				t.Fatalf("gist must name its producing agent, got %q", gist.AgentID)
			}
		})
	}
}

// TestGisterProvenanceAtBirth (D5): the full birth tuple, with
// origin=manual forbidden for pipeline writes.
func TestGisterProvenanceAtBirth(t *testing.T) {
	s := seedWorld(t)
	events := gistWindowEvents(t)
	appendEvents(t, s, events...)
	gister := newTestGister(s, &scriptedModel{responses: []string{gistResponse}})

	win, err := gister.Window(context.Background(), testJob())
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	gist, err := gister.Gist(context.Background(), testJob(), win)
	if err != nil {
		t.Fatalf("gist: %v", err)
	}

	if gist.Origin == domain.MemoryOriginManual {
		t.Fatal("origin=manual is forbidden for pipeline writes")
	}
	if !domain.ValidMemoryOrigin(gist.Origin) {
		t.Fatalf("origin must be one of the four tiers, got %q", gist.Origin)
	}
	if !gist.EventTime.Equal(events[0].OccurredAt) {
		t.Fatalf("event_time must be the window start, got %v want %v", gist.EventTime, events[0].OccurredAt)
	}
	if gist.LearnedAt.IsZero() {
		t.Fatal("learned_at must be set")
	}
	if gist.SourceEventID != "e2" {
		t.Fatalf("source_event_id must be the window's end event id, got %q", gist.SourceEventID)
	}
	if gist.Description == "" || gist.Outcome == "" {
		t.Fatalf("gist must carry description and outcome, got %+v", gist)
	}
	if len(gist.Participants) == 0 {
		t.Fatal("gist must carry its participants")
	}
}

// TestGisterIncrementalCursor (D3): the second window covers only the events
// since the first gist — nothing is reprocessed and the first window's text
// never reaches the second model call.
func TestGisterIncrementalCursor(t *testing.T) {
	s := seedWorld(t)
	base := time.Now().UTC().Add(-time.Hour)
	appendEvents(t, s,
		chatEvent(t, "e1", "turn-1", 1, base, schema.AgenticRoleTypeUser, "Reminder: the deploy window is Tuesdays."),
		chatEvent(t, "e2", "turn-1", 2, base.Add(time.Minute), schema.AgenticRoleTypeAssistant, "Noted, Tuesdays it is."),
	)

	model := &scriptedModel{responses: []string{
		`{"description":"Deploy window set","outcome":"Tuesdays confirmed"}`,
		`{"description":"Vendor contact named","outcome":"Budi is the vendor contact"}`,
	}}
	gister := newTestGister(s, model)
	ctx := context.Background()

	// First window: the whole log.
	job := testJob()
	win1, err := gister.Window(ctx, job)
	if err != nil {
		t.Fatalf("window 1: %v", err)
	}
	if len(win1.events) != 2 {
		t.Fatalf("first window must cover the whole log, got %d events", len(win1.events))
	}
	if _, err := gister.Gist(ctx, job, win1); err != nil {
		t.Fatalf("gist 1: %v", err)
	}

	// Second window: only the new turn.
	appendEvents(t, s,
		chatEvent(t, "e3", "turn-2", 3, base.Add(2*time.Minute), schema.AgenticRoleTypeUser, "Also: the vendor contact is Budi."),
		chatEvent(t, "e4", "turn-2", 4, base.Add(3*time.Minute), schema.AgenticRoleTypeAssistant, "Recorded."),
	)
	job2 := testJob()
	job2.TurnID = "turn-2"
	win2, err := gister.Window(ctx, job2)
	if err != nil {
		t.Fatalf("window 2: %v", err)
	}
	if len(win2.events) != 2 {
		t.Fatalf("second window must cover only the new events, got %d", len(win2.events))
	}
	if win2.events[0].EventID != "e3" || win2.events[1].EventID != "e4" {
		t.Fatalf("second window holds the wrong events: %s..%s", win2.events[0].EventID, win2.events[1].EventID)
	}
	if _, err := gister.Gist(ctx, job2, win2); err != nil {
		t.Fatalf("gist 2: %v", err)
	}

	// The second model call never sees the first window's material.
	inputs := model.callInputs()
	if len(inputs) != 2 {
		t.Fatalf("expected exactly two model calls, got %d", len(inputs))
	}
	second := strings.Join(inputs[1], "\n")
	if strings.Contains(second, "deploy window") {
		t.Fatal("the second gist reprocessed the first window's material")
	}
	if !strings.Contains(second, "vendor contact is Budi") {
		t.Fatal("the second gist must carry the new window's material")
	}

	// Two timeline rows, cursor pointers exact.
	gists, err := s.MemoryEvents().ListEventsForUI(ctx, testWorkspaceID, testUserID, testAgentID, store.MemoryEventFilters{SessionID: testSessionID})
	if err != nil {
		t.Fatalf("list gists: %v", err)
	}
	if len(gists) != 2 {
		t.Fatalf("expected two gist rows, got %d", len(gists))
	}
	if gists[0].SourceEventID != "e4" || gists[1].SourceEventID != "e2" {
		t.Fatalf("gist windows must chain through their end-event pointers, got %q then %q", gists[0].SourceEventID, gists[1].SourceEventID)
	}
}

// TestGisterEmptyWindowIsQuiet: nothing new since the last gist is a normal
// state — no model call, no error.
func TestGisterEmptyWindowIsQuiet(t *testing.T) {
	s := seedWorld(t)
	model := &scriptedModel{}
	gister := newTestGister(s, model)
	win, err := gister.Window(context.Background(), testJob())
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	if len(win.events) != 0 {
		t.Fatalf("expected an empty window, got %d events", len(win.events))
	}
	if len(model.callInputs()) != 0 {
		t.Fatalf("an empty window must not spend a model call, got %d", len(model.callInputs()))
	}
}

// TestGisterBadSummaryFailsSoft: an unparseable summary fails the stage
// without writing a row.
func TestGisterBadSummaryFailsSoft(t *testing.T) {
	s := seedWorld(t)
	events := gistWindowEvents(t)
	appendEvents(t, s, events...)
	gister := newTestGister(s, &scriptedModel{responses: []string{"no json here"}})

	win, err := gister.Window(context.Background(), testJob())
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	if _, err := gister.Gist(context.Background(), testJob(), win); err == nil {
		t.Fatal("expected an unparseable summary to error")
	}
	gists, err := s.MemoryEvents().ListEventsForUI(context.Background(), testWorkspaceID, testUserID, testAgentID, store.MemoryEventFilters{SessionID: testSessionID})
	if err != nil {
		t.Fatalf("list gists: %v", err)
	}
	if len(gists) != 0 {
		t.Fatalf("no gist row must be written on failure, got %d", len(gists))
	}
	// The raw evidence stays intact for reprocessing.
	win2, err := gister.Window(context.Background(), testJob())
	if err != nil {
		t.Fatalf("window after failure: %v", err)
	}
	if len(win2.events) != 2 {
		t.Fatalf("raw session events must be untouched, got %d", len(win2.events))
	}
}

// TestSessionCeiling: the session-shape derivation the gate clamps against.
func TestSessionCeiling(t *testing.T) {
	cases := []struct {
		name string
		job  IngestJob
		want domain.MemoryVisibility
	}{
		{"direct chat", IngestJob{Origin: originUser, HumanParticipants: 1}, domain.MemoryVisibilityUser},
		{"telegram DM", IngestJob{Origin: originTelegram, HumanParticipants: 1}, domain.MemoryVisibilityUser},
		{"scheduler", IngestJob{Origin: originScheduler}, domain.MemoryVisibilityAgent},
		{"heartbeat", IngestJob{Origin: originHeartbeat}, domain.MemoryVisibilityAgent},
		{"two-human channel", IngestJob{Origin: originChannel, HumanParticipants: 2}, domain.MemoryVisibilityShared},
		{"one-human channel", IngestJob{Origin: originChannel, HumanParticipants: 1}, domain.MemoryVisibilityUser},
		{"empty origin is a direct chat", IngestJob{}, domain.MemoryVisibilityUser},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionCeiling(tc.job); got != tc.want {
				t.Fatalf("sessionCeiling: got %q want %q", got, tc.want)
			}
		})
	}
}
