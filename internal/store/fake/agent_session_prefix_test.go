package fake_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// TestFakeAgentSessions_BindingPrefixRules covers the session-store binding
// validators (integrate-telegram-gateway design D3, channel-session-leak
// fix): unknown "<word>_"-prefixed session ids are refused on upsert, and
// the per-user listing indexes chat sessions (web + gateway DM) while
// excluding channel, scheduler, heartbeat, and gateway group artifacts.
func TestFakeAgentSessions_BindingPrefixRules(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	u := &domain.User{Email: "prefixes@example.com", Name: "Member"}
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ws := &domain.Workspace{Slug: "prefixes", Name: "Prefixes"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Name: "main", Type: "openai"}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: p.ID, Model: "gpt"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	st := s.AgentSessions()

	// 1. Registered prefixes are accepted, including the new gateway ones
	// and the heartbeat tick session (add-agent-heartbeat D2).
	registered := []string{
		"sess_d2b1f0a2-6a63-4e4e-9f2f-9e4a6d1f7b33",
		"chan_ab12cd34-1111-2222-3333-444455556666_" + a.ID,
		"sched_" + a.ID + "_1726142400",
		"hb_" + a.ID,
		"tg_dm_593821092_" + a.ID,
		"tg_group_-1001234567890_" + a.ID,
	}
	for _, id := range registered {
		if err := st.UpsertAgentSession(ctx, ws.ID, a.ID, u.ID, domain.AgentSessionUpsert{SessionID: id, Title: "T"}); err != nil {
			t.Fatalf("expected registered prefix accepted for %q, got %v", id, err)
		}
	}

	// 2. Unknown binding prefixes are refused.
	for _, id := range []string{"unknown_prefix_1", "tg_voice_593821092_" + a.ID, "tg_"} {
		if err := st.UpsertAgentSession(ctx, ws.ID, a.ID, u.ID, domain.AgentSessionUpsert{SessionID: id, Title: "T"}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("expected ErrInvalid for unregistered prefix %q, got %v", id, err)
		}
	}

	// 3. The per-user listing shows web and gateway DM sessions only.
	list, err := st.ListAgentSessions(ctx, ws.ID, a.ID, u.ID)
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
	if !ids["tg_dm_593821092_"+a.ID] {
		t.Fatal("expected gateway DM session listed under the paired member")
	}
	for _, hidden := range []string{
		"chan_ab12cd34-1111-2222-3333-444455556666_" + a.ID,
		"sched_" + a.ID + "_1726142400",
		"hb_" + a.ID,
		"tg_group_-1001234567890_" + a.ID,
	} {
		if ids[hidden] {
			t.Fatalf("expected %q excluded from the per-user listing", hidden)
		}
	}
	if len(list) != 2 {
		t.Fatalf("expected exactly 2 listed sessions, got %d", len(list))
	}
}
