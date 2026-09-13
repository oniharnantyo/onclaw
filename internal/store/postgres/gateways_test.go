//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// gwSeedWorkspace creates a workspace for gateway tests.
func gwSeedWorkspace(t *testing.T, ctx context.Context, s store.Store, slug string) *domain.Workspace {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "Gateway WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}
	return ws
}

// gwSeedAgent creates a workspace-scoped agent for gateway bindings.
func gwSeedAgent(t *testing.T, ctx context.Context, s store.Store, ws *domain.Workspace, slug string) *domain.Agent {
	t.Helper()
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI " + slug, Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("unexpected create provider error: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: slug, Name: "Agent " + slug, ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("unexpected create agent error: %v", err)
	}
	return a
}

func gwNewConfig(ws *domain.Workspace) *domain.GatewayConfig {
	return &domain.GatewayConfig{
		WorkspaceID:        ws.ID,
		Platform:           domain.GatewayPlatformTelegram,
		BotTokenCiphertext: "v1:bm9uY2U=:Y2lwaGVydGV4dA==",
		BotUsername:        "onclaw_bot",
		Enabled:            true,
		Transport:          domain.GatewayTransportLongPolling,
	}
}

func TestIntegration_GatewayStore_CRUD(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := gwSeedWorkspace(t, ctx, s, "gw-crud")
	agent := gwSeedAgent(t, ctx, s, ws, "atlas")
	st := s.Gateways()

	// 1. Unknown workspace fails as NotFound (FK parity).
	ghost := gwNewConfig(ws)
	if err := st.UpsertGateway(ctx, "00000000-0000-0000-0000-000000000000", ghost); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}

	// 2. Cross-workspace default agent is refused (workspace-scoped FK parity).
	crossWs := gwSeedWorkspace(t, ctx, s, "gw-crud-b")
	crossAgent := gwSeedAgent(t, ctx, s, crossWs, "beacon")
	foreignAgent := crossAgent.ID
	badAgent := gwNewConfig(ws)
	badAgent.DefaultAgentID = &foreignAgent
	if err := st.UpsertGateway(ctx, ws.ID, badAgent); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-workspace agent, got %v", err)
	}

	// 3. Create: id and timestamps assigned; ciphertext round-trips opaquely.
	g := gwNewConfig(ws)
	if err := st.UpsertGateway(ctx, ws.ID, g); err != nil {
		t.Fatalf("unexpected upsert error: %v", err)
	}
	if g.ID == "" || g.CreatedAt.IsZero() {
		t.Fatalf("expected id/created_at assigned, got %+v", g)
	}
	got, err := st.GetGateway(ctx, ws.ID, domain.GatewayPlatformTelegram)
	if err != nil || got == nil {
		t.Fatalf("unexpected get result: (%v, %v)", got, err)
	}
	if got.BotTokenCiphertext != g.BotTokenCiphertext || !got.Enabled || got.BotUsername != "onclaw_bot" {
		t.Fatalf("expected round-tripped config, got %+v", got)
	}

	// 4. Absent platform and foreign workspace read as (nil, nil) — no leak.
	if none, err := st.GetGateway(ctx, ws.ID, "slack"); err != nil || none != nil {
		t.Fatalf("expected (nil, nil) for absent platform, got (%v, %v)", none, err)
	}
	foreign, err := st.GetGateway(ctx, "00000000-0000-0000-0000-000000000002", domain.GatewayPlatformTelegram)
	if err != nil || foreign != nil {
		t.Fatalf("expected (nil, nil) for foreign workspace, got (%v, %v)", foreign, err)
	}

	// 5. Upsert conflict rewrites connection fields; id, created_at, and the
	// enabled flag survive.
	reconnect := gwNewConfig(ws)
	reconnect.BotTokenCiphertext = "v1:bm9uY2Uy:Y2lwaGVydGV4dDI="
	reconnect.BotUsername = "onclaw_bot_v2"
	reconnect.Enabled = false // must NOT clobber the stored flag
	reconnect.DefaultAgentID = &agent.ID
	if err := st.UpsertGateway(ctx, ws.ID, reconnect); err != nil {
		t.Fatalf("unexpected reconnect error: %v", err)
	}
	reloaded, _ := st.GetGateway(ctx, ws.ID, domain.GatewayPlatformTelegram)
	if reloaded.ID != g.ID || !reloaded.CreatedAt.Equal(g.CreatedAt) {
		t.Fatalf("expected id/created_at preserved, got %+v vs %+v", reloaded, g)
	}
	if reloaded.BotTokenCiphertext != reconnect.BotTokenCiphertext || reloaded.BotUsername != "onclaw_bot_v2" {
		t.Fatalf("expected connection fields rewritten, got %+v", reloaded)
	}
	if reloaded.DefaultAgentID == nil || *reloaded.DefaultAgentID != agent.ID {
		t.Fatalf("expected default agent rewritten, got %+v", reloaded.DefaultAgentID)
	}
	if !reloaded.Enabled {
		t.Fatal("expected stored enabled flag to survive the upsert")
	}

	// 6. Enable toggle; absent rows are ErrNotFound.
	if err := st.SetGatewayEnabled(ctx, ws.ID, domain.GatewayPlatformTelegram, false); err != nil {
		t.Fatalf("unexpected disable error: %v", err)
	}
	reloaded, _ = st.GetGateway(ctx, ws.ID, domain.GatewayPlatformTelegram)
	if reloaded.Enabled {
		t.Fatal("expected gateway disabled")
	}
	if err := st.SetGatewayEnabled(ctx, ws.ID, "slack", true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for absent platform, got %v", err)
	}

	// 7. List is workspace-scoped.
	list, err := st.ListGateways(ctx, ws.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 gateway, got %d (%v)", len(list), err)
	}

	// 8. Delete; the row is gone afterwards.
	if err := st.DeleteGateway(ctx, ws.ID, domain.GatewayPlatformTelegram); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if err := st.DeleteGateway(ctx, ws.ID, domain.GatewayPlatformTelegram); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound re-deleting, got %v", err)
	}
}

func TestIntegration_GatewayBindings(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := gwSeedWorkspace(t, ctx, s, "gw-bind")
	agent := gwSeedAgent(t, ctx, s, ws, "atlas")
	st := s.GatewayBindings()

	// 1. Unknown agent fails as NotFound (workspace-scoped FK parity).
	orphan := &domain.ChatBinding{Platform: domain.GatewayPlatformTelegram, PlatformChatID: "-100", AgentID: "00000000-0000-0000-0000-000000000009"}
	if err := st.CreateChatBinding(ctx, ws.ID, orphan); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown agent, got %v", err)
	}

	binding := &domain.ChatBinding{Platform: domain.GatewayPlatformTelegram, PlatformChatID: "-1001234567890", AgentID: agent.ID}
	if err := st.CreateChatBinding(ctx, ws.ID, binding); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if binding.ID == "" || binding.CreatedAt.IsZero() {
		t.Fatalf("expected id/created_at assigned, got %+v", binding)
	}

	// 2. The same chat cannot bind twice — the unique constraint is global,
	// so a second workspace binding the same chat also conflicts.
	dup := &domain.ChatBinding{Platform: domain.GatewayPlatformTelegram, PlatformChatID: "-1001234567890", AgentID: agent.ID}
	if err := st.CreateChatBinding(ctx, ws.ID, dup); !errors.Is(err, domain.ErrGatewayBindingConflict) {
		t.Fatalf("expected ErrGatewayBindingConflict, got %v", err)
	}

	// 3. Routing lookup; unbound chats read (nil, nil).
	got, err := st.GetChatBinding(ctx, ws.ID, domain.GatewayPlatformTelegram, "-1001234567890")
	if err != nil || got == nil || got.AgentID != agent.ID {
		t.Fatalf("unexpected binding lookup: (%v, %v)", got, err)
	}
	if none, err := st.GetChatBinding(ctx, ws.ID, domain.GatewayPlatformTelegram, "-1999999999999"); err != nil || none != nil {
		t.Fatalf("expected (nil, nil) for unbound chat, got (%v, %v)", none, err)
	}

	// 4. Remap (migrate_to_chat_id): lookup follows, old chat id unbound.
	if err := st.RemapBindingChat(ctx, ws.ID, domain.GatewayPlatformTelegram, "-1001234567890", "-1098765432100"); err != nil {
		t.Fatalf("unexpected remap error: %v", err)
	}
	moved, err := st.GetChatBinding(ctx, ws.ID, domain.GatewayPlatformTelegram, "-1098765432100")
	if err != nil || moved == nil || moved.ID != binding.ID {
		t.Fatalf("expected remapped binding, got (%v, %v)", moved, err)
	}
	if old, err := st.GetChatBinding(ctx, ws.ID, domain.GatewayPlatformTelegram, "-1001234567890"); err != nil || old != nil {
		t.Fatalf("expected old chat id unbound, got (%v, %v)", old, err)
	}
	if err := st.RemapBindingChat(ctx, ws.ID, domain.GatewayPlatformTelegram, "-1001234567890", "-1098765432100"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound remapping unknown chat, got %v", err)
	}

	// 5. List is workspace-scoped; delete by id.
	list, err := st.ListChatBindings(ctx, ws.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 binding, got %d (%v)", len(list), err)
	}
	if err := st.DeleteChatBinding(ctx, ws.ID, binding.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if err := st.DeleteChatBinding(ctx, ws.ID, binding.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound re-deleting, got %v", err)
	}
}

func TestIntegration_GatewayLinks_PairingLifecycle(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := gwSeedWorkspace(t, ctx, s, "gw-links")
	user := &domain.User{Email: "gw-links@example.com", Name: "Member"}
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatalf("unexpected create user error: %v", err)
	}
	st := s.GatewayLinks()
	now := time.Now().UTC()

	// 1. Mint: shape validated, member pre-checked, expiry must be future.
	token := &domain.PairingToken{Token: strings.Repeat("a", 43), WorkspaceID: ws.ID, UserID: user.ID, ExpiresAt: now.Add(time.Hour)}
	if err := st.CreatePairingToken(ctx, token); err != nil {
		t.Fatalf("unexpected mint error: %v", err)
	}
	badShape := &domain.PairingToken{Token: "short", WorkspaceID: ws.ID, UserID: user.ID, ExpiresAt: now.Add(time.Hour)}
	if err := st.CreatePairingToken(ctx, badShape); !errors.Is(err, domain.ErrInvalidPairingToken) {
		t.Fatalf("expected ErrInvalidPairingToken, got %v", err)
	}
	pastExpiry := &domain.PairingToken{Token: strings.Repeat("e", 43), WorkspaceID: ws.ID, UserID: user.ID, ExpiresAt: now.Add(-time.Minute)}
	if err := st.CreatePairingToken(ctx, pastExpiry); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid minting an expired token, got %v", err)
	}

	// 2. Consume: wins exactly once; second use is refused.
	consumed, err := st.ConsumePairingToken(ctx, ws.ID, token.Token, now)
	if err != nil || consumed == nil || consumed.ConsumedAt == nil {
		t.Fatalf("expected consumed token, got (%v, %v)", consumed, err)
	}
	if _, err := st.ConsumePairingToken(ctx, ws.ID, token.Token, now.Add(time.Minute)); !errors.Is(err, domain.ErrPairingTokenExpired) {
		t.Fatalf("expected ErrPairingTokenExpired on second use, got %v", err)
	}

	// 3. Expired tokens are refused with the same sentinel; unknown tokens
	// are ErrNotFound. Validation refuses minting with past expiry, so the
	// expiry arrives by the clock passing the horizon.
	later := &domain.PairingToken{Token: strings.Repeat("c", 43), WorkspaceID: ws.ID, UserID: user.ID, ExpiresAt: now.Add(time.Hour)}
	if err := st.CreatePairingToken(ctx, later); err != nil {
		t.Fatalf("unexpected mint error: %v", err)
	}
	if _, err := st.ConsumePairingToken(ctx, ws.ID, later.Token, now.Add(2*time.Hour)); !errors.Is(err, domain.ErrPairingTokenExpired) {
		t.Fatalf("expected ErrPairingTokenExpired for expired token, got %v", err)
	}
	if _, err := st.ConsumePairingToken(ctx, ws.ID, strings.Repeat("d", 43), now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown token, got %v", err)
	}

	// 4. Identity link: duplicate platform identity is a conflict.
	link := &domain.UserLink{Platform: domain.GatewayPlatformTelegram, PlatformUserID: "593821092", WorkspaceID: ws.ID, UserID: user.ID, PlatformUsername: "onih"}
	if err := st.CreateUserLink(ctx, ws.ID, link); err != nil {
		t.Fatalf("unexpected link error: %v", err)
	}
	if err := st.CreateUserLink(ctx, ws.ID, link); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate identity, got %v", err)
	}
	got, err := st.GetUserLink(ctx, ws.ID, domain.GatewayPlatformTelegram, "593821092")
	if err != nil || got == nil || got.UserID != user.ID {
		t.Fatalf("expected paired member, got (%v, %v)", got, err)
	}
	memberLinks, err := st.ListUserLinksForMember(ctx, ws.ID, user.ID)
	if err != nil || len(memberLinks) != 1 {
		t.Fatalf("expected 1 member link, got %d (%v)", len(memberLinks), err)
	}
	if err := st.DeleteUserLink(ctx, ws.ID, domain.GatewayPlatformTelegram, "593821092"); err != nil {
		t.Fatalf("unexpected unpair error: %v", err)
	}
	if none, err := st.GetUserLink(ctx, ws.ID, domain.GatewayPlatformTelegram, "593821092"); err != nil || none != nil {
		t.Fatalf("expected (nil, nil) after unpair, got (%v, %v)", none, err)
	}

	// 5. Revocation deletes an unconsumed token.
	revocable := &domain.PairingToken{Token: strings.Repeat("f", 43), WorkspaceID: ws.ID, UserID: user.ID, ExpiresAt: now.Add(time.Hour)}
	if err := st.CreatePairingToken(ctx, revocable); err != nil {
		t.Fatalf("unexpected mint error: %v", err)
	}
	if err := st.RevokePairingToken(ctx, ws.ID, revocable.Token); err != nil {
		t.Fatalf("unexpected revoke error: %v", err)
	}
	if _, err := st.ConsumePairingToken(ctx, ws.ID, revocable.Token, now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound consuming revoked token, got %v", err)
	}
}

func TestIntegration_GatewayOutbox(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := gwSeedWorkspace(t, ctx, s, "gw-outbox")
	st := s.GatewayOutbox()
	now := time.Now().UTC()

	// 1. Enqueue (write-before-send): born pending, attempts 0.
	entry := &domain.OutboxEntry{WorkspaceID: ws.ID, SessionID: "tg_dm_593821092_atlas", Payload: []byte(`{"text":"hi"}`)}
	if err := st.Enqueue(ctx, entry); err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}
	if entry.Status != domain.GatewayOutboxStatusPending || entry.Attempts != 0 {
		t.Fatalf("expected pending entry with 0 attempts, got %+v", entry)
	}

	// 2. Claim: the UPDATE ... FOR UPDATE SKIP LOCKED statement increments
	// attempts and returns the row with its payload intact.
	claimed, err := st.ClaimDue(ctx, now.Add(time.Minute), 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("expected 1 claimed entry, got %d (%v)", len(claimed), err)
	}
	if claimed[0].ID != entry.ID || claimed[0].Attempts != 1 {
		t.Fatalf("expected the enqueued entry with attempts=1, got %+v", claimed[0])
	}
	// jsonb normalizes whitespace, so compare the payload semantically: the
	// opaque JSON object must survive the roundtrip field-for-field.
	var payload map[string]string
	if err := json.Unmarshal(claimed[0].Payload, &payload); err != nil {
		t.Fatalf("expected jsonb object payload, got %s: %v", claimed[0].Payload, err)
	}
	if payload["text"] != "hi" {
		t.Fatalf("expected payload text %q, got %q", "hi", payload["text"])
	}

	// 3. Deliver; future entries are unclaimable until due.
	if err := st.MarkDelivered(ctx, ws.ID, entry.ID); err != nil {
		t.Fatalf("unexpected deliver error: %v", err)
	}
	future := &domain.OutboxEntry{WorkspaceID: ws.ID, SessionID: "tg_group_-100_atlas", Payload: []byte(`{"text":"later"}`), DeliverAfter: now.Add(time.Hour)}
	if err := st.Enqueue(ctx, future); err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}
	claimed, err = st.ClaimDue(ctx, now.Add(time.Minute), 10)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("expected no claimable entries, got %d (%v)", len(claimed), err)
	}

	// 4. Dead entries are never claimed again; foreign-workspace marks are
	// ErrNotFound (tenant scoping on the mark paths).
	if err := st.MarkDead(ctx, ws.ID, future.ID); err != nil {
		t.Fatalf("unexpected dead error: %v", err)
	}
	claimed, _ = st.ClaimDue(ctx, now.Add(2*time.Hour), 10)
	if len(claimed) != 0 {
		t.Fatalf("expected dead entry to be unclaimable, got %d", len(claimed))
	}
	if err := st.MarkDelivered(ctx, "00000000-0000-0000-0000-000000000009", entry.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for foreign workspace, got %v", err)
	}

	// 5. Reschedule returns an entry to pending; prune removes delivered
	// rows past the horizon.
	if err := st.Reschedule(ctx, ws.ID, future.ID, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("unexpected reschedule error: %v", err)
	}
	reloaded, err := st.ClaimDue(ctx, now.Add(3*time.Hour), 10)
	if err != nil || len(reloaded) != 1 || reloaded[0].ID != future.ID {
		t.Fatalf("expected rescheduled entry claimable, got %d (%v)", len(reloaded), err)
	}
	pruned, err := st.PruneDelivered(ctx, now.Add(24*time.Hour))
	if err != nil || pruned != 1 {
		t.Fatalf("expected 1 pruned delivered row, got %d (%v)", pruned, err)
	}
}
