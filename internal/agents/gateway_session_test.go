package agents

import (
	"context"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Telegram gateway runner integration (integrate-telegram-gateway task 6.3):
// the `telegram` origin rides the existing origin contracts, `tg_dm_`
// sessions index under the paired member like web sessions, and `tg_group_`
// sessions never touch the per-user index (the scheduler-session precedent).

// spyAgentSessions records index upserts while delegating to the real store,
// so the runner-side skip rules are observable independently of the store's
// own IsPrivateIndexSessionID listing filter.
type spyAgentSessions struct {
	store.AgentSessionStore

	mu      sync.Mutex
	upserts []string // recorded session ids, call order
}

func (s *spyAgentSessions) UpsertAgentSession(ctx context.Context, workspaceID, agentID, userID string, upsert domain.AgentSessionUpsert) error {
	s.mu.Lock()
	s.upserts = append(s.upserts, upsert.SessionID)
	s.mu.Unlock()
	return s.AgentSessionStore.UpsertAgentSession(ctx, workspaceID, agentID, userID, upsert)
}

func (s *spyAgentSessions) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.upserts...)
}

// TestNormalizeOriginTelegram: the gateway origin passes through the D1
// normalizer (otherwise gateway turns would be re-tagged user), and unknown
// values still fold to OriginUser.
func TestNormalizeOriginTelegram(t *testing.T) {
	if got := normalizeOrigin(OriginTelegram); got != OriginTelegram {
		t.Fatalf("normalizeOrigin(telegram) = %q, want telegram", got)
	}
	// The pre-existing value set is untouched.
	if got := normalizeOrigin(OriginScheduler); got != OriginScheduler {
		t.Fatalf("normalizeOrigin(scheduler) = %q, want scheduler", got)
	}
	if got := normalizeOrigin(OriginChannel); got != OriginChannel {
		t.Fatalf("normalizeOrigin(channel) = %q, want channel", got)
	}
	if got := normalizeOrigin(""); got != OriginUser {
		t.Fatalf("normalizeOrigin(empty) = %q, want user", got)
	}
	if got := normalizeOrigin("carrier_pigeon"); got != OriginUser {
		t.Fatalf("normalizeOrigin(unknown) = %q, want user", got)
	}
}

// TestNormalizeOriginService: the service-authority origin is first-class in
// the D1 value set (add-connection-webhooks contract §6) — webhook-triggered
// runs keep their honest origin instead of folding back to user, while every
// other origin's mapping is unchanged.
func TestNormalizeOriginService(t *testing.T) {
	if got := normalizeOrigin(OriginService); got != OriginService {
		t.Fatalf("normalizeOrigin(service) = %q, want service", got)
	}
	if got := normalizeOrigin(OriginHeartbeat); got != OriginHeartbeat {
		t.Fatalf("normalizeOrigin(heartbeat) = %q, want heartbeat", got)
	}
	// The literal the ingress client stamps (webhooks.OriginService) must
	// stay the recognized value.
	if got := normalizeOrigin("service"); got != OriginService {
		t.Fatalf("normalizeOrigin(%q) = %q, want service", OriginService, got)
	}
}

// TestHookOriginValuesIncludeTelegram: hook event payloads (and the save-time
// origin matcher match-counts) expose the telegram origin.
func TestHookOriginValuesIncludeTelegram(t *testing.T) {
	found := false
	for _, v := range hooks.OriginValues() {
		if v == OriginTelegram {
			found = true
		}
	}
	if !found {
		t.Fatalf("hooks.OriginValues() %v must include %q", hooks.OriginValues(), OriginTelegram)
	}
}

// TestRun_TelegramDMSessionIndexesUnderPairedMember: a gateway DM turn (a
// tg_dm_ session with OriginTelegram) registers in the paired member's
// private index exactly like a web session, with the input-derived birth
// title (agent-runtime delta spec, "Telegram DM session registers with
// origin").
func TestRun_TelegramDMSessionIndexesUnderPairedMember(t *testing.T) {
	m := &manyDeltaModel{deltas: 1}
	runner, req := setupLifecycleRunner(t, "tg_dm_593821092_atlas", m)
	req.Origin = OriginTelegram
	req.Input = "ping from Telegram"
	ctx := context.Background()

	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run (telegram DM): %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the telegram turn to complete, got %+v", ev)
	}
	waitRunDone(t, runner, runKey(req))

	rows, err := runner.agentSessions.ListAgentSessions(ctx, req.WorkspaceID, req.AgentID, req.UserID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(rows) != 1 || rows[0].SessionID != "tg_dm_593821092_atlas" {
		t.Fatalf("expected the tg_dm_ session indexed under the paired member, got %+v", rows)
	}
	if rows[0].Title != "ping from Telegram" {
		t.Fatalf("expected the input-derived birth title, got %q", rows[0].Title)
	}
}

// TestRun_TelegramGroupSessionNeverIndexes: a gateway turn on a tg_group_
// shared session performs no index upsert at all — the runner-side rule,
// observed through a recording store so the store's listing filter cannot
// mask it (agent-runtime delta spec, "Gateway group session stays out of the
// index").
func TestRun_TelegramGroupSessionNeverIndexes(t *testing.T) {
	m := &manyDeltaModel{deltas: 1}
	runner, req := setupLifecycleRunner(t, "tg_group_-100123_atlas", m)
	spy := &spyAgentSessions{AgentSessionStore: runner.agentSessions}
	runner.agentSessions = spy
	req.Origin = OriginTelegram
	ctx := context.Background()

	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run (telegram group): %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the telegram group turn to complete, got %+v", ev)
	}
	waitRunDone(t, runner, runKey(req))

	for _, id := range spy.recorded() {
		if id == "tg_group_-100123_atlas" {
			t.Fatalf("gateway group session was indexed: %v", spy.recorded())
		}
	}

	// Control: the same runner still indexes ordinary sessions — the skip is
	// the tg_group_ rule, not a broken index.
	req.SessionID = "sess-control"
	req.Origin = ""
	istream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run (control): %v", err)
	}
	if ev := collectStream(t, istream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected the control turn to complete, got %+v", ev)
	}
	waitRunDone(t, runner, runKey(req))

	found := false
	for _, id := range spy.recorded() {
		if id == "sess-control" {
			found = true
		}
	}
	if !found {
		t.Fatalf("control session was not indexed; upserts: %v", spy.recorded())
	}
}
