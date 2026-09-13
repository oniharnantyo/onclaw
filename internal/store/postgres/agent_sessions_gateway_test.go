//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// TestIntegration_AgentSessions_BindingPrefixRules covers the session-store
// binding validators at the SQL layer (integrate-telegram-gateway design D3,
// channel-session-leak fix): unknown "<word>_"-prefixed session ids are
// refused on upsert, and the per-user listing indexes chat sessions (web +
// gateway DM) while excluding channel, scheduler, and gateway group
// artifacts — mirroring the fake adapter.
func TestIntegration_AgentSessions_BindingPrefixRules(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := gwSeedWorkspace(t, ctx, s, "sess-prefixes")
	agent := gwSeedAgent(t, ctx, s, ws, "atlas")
	user := &domain.User{Email: "sess-prefixes@example.com", Name: "Member"}
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatalf("unexpected create user error: %v", err)
	}

	st := s.AgentSessions()

	// 1. Registered prefixes are accepted, including the new gateway ones.
	registered := []string{
		"sess_d2b1f0a2-6a63-4e4e-9f2f-9e4a6d1f7b33",
		"chan_ab12cd34-1111-2222-3333-444455556666_" + agent.ID,
		"sched_" + agent.ID + "_1726142400",
		"tg_dm_593821092_" + agent.ID,
		"tg_group_-1001234567890_" + agent.ID,
	}
	for _, id := range registered {
		if err := st.UpsertAgentSession(ctx, ws.ID, agent.ID, user.ID, domain.AgentSessionUpsert{SessionID: id, Title: "T"}); err != nil {
			t.Fatalf("expected registered prefix accepted for %q, got %v", id, err)
		}
	}

	// 2. Unknown binding prefixes are refused.
	for _, id := range []string{"unknown_prefix_1", "tg_voice_593821092_" + agent.ID, "tg_"} {
		if err := st.UpsertAgentSession(ctx, ws.ID, agent.ID, user.ID, domain.AgentSessionUpsert{SessionID: id, Title: "T"}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("expected ErrInvalid for unregistered prefix %q, got %v", id, err)
		}
	}

	// 3. The per-user listing shows web and gateway DM sessions only; the
	// excluded rows still exist (soft-delete by direct id remains possible).
	list, err := st.ListAgentSessions(ctx, ws.ID, agent.ID, user.ID)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	ids := make(map[string]bool, len(list))
	for _, row := range list {
		ids[row.SessionID] = true
	}
	if !ids["sess_d2b1f0a2-6a63-4e4e-9f2f-9e4a6d1f7b33"] {
		t.Fatal("expected web session listed")
	}
	if !ids["tg_dm_593821092_"+agent.ID] {
		t.Fatal("expected gateway DM session listed under the paired member")
	}
	for _, hidden := range []string{
		"chan_ab12cd34-1111-2222-3333-444455556666_" + agent.ID,
		"sched_" + agent.ID + "_1726142400",
		"tg_group_-1001234567890_" + agent.ID,
	} {
		if ids[hidden] {
			t.Fatalf("expected %q excluded from the per-user listing", hidden)
		}
	}
	if len(list) != 2 {
		t.Fatalf("expected exactly 2 listed sessions, got %d", len(list))
	}
}
