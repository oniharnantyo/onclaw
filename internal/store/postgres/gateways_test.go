//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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

func gwNewConfig(ws *domain.Workspace, agent *domain.Agent) *domain.GatewayConfig {
	return &domain.GatewayConfig{
		WorkspaceID:        ws.ID,
		Platform:           domain.GatewayPlatformTelegram,
		Identity:           "@onclaw_bot",
		AgentID:            agent.ID,
		BotTokenCiphertext: "v1:bm9uY2U=:Y2lwaGVydGV4dA==",
		BotUsername:        "onclaw_bot",
		Enabled:            true,
		Transport:          domain.GatewayTransportLongPolling,
	}
}

func TestIntegration_GatewayStore_CRUDAndMultiBot(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := gwSeedWorkspace(t, ctx, s, "gw-crud")
	agent := gwSeedAgent(t, ctx, s, ws, "atlas")
	st := s.Gateways()

	// 1. Unknown workspace fails as NotFound (FK parity).
	ghost := gwNewConfig(ws, agent)
	if err := st.CreateGateway(ctx, "00000000-0000-0000-0000-000000000000", ghost); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}

	// 2. Cross-workspace agent is refused (workspace-scoped FK parity).
	crossWs := gwSeedWorkspace(t, ctx, s, "gw-crud-b")
	crossAgent := gwSeedAgent(t, ctx, s, crossWs, "beacon")
	badAgentCfg := gwNewConfig(ws, crossAgent)
	if err := st.CreateGateway(ctx, ws.ID, badAgentCfg); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-workspace agent, got %v", err)
	}

	// 3. Create: id and timestamps assigned; ciphertext round-trips opaquely.
	g1 := gwNewConfig(ws, agent)
	if err := st.CreateGateway(ctx, ws.ID, g1); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if g1.ID == "" || g1.CreatedAt.IsZero() {
		t.Fatalf("expected id/created_at assigned, got %+v", g1)
	}
	got, err := st.GetGateway(ctx, ws.ID, g1.ID)
	if err != nil || got == nil {
		t.Fatalf("unexpected get result: (%v, %v)", got, err)
	}
	if got.BotTokenCiphertext != g1.BotTokenCiphertext || !got.Enabled || got.BotUsername != "onclaw_bot" || got.Identity != "@onclaw_bot" || got.AgentID != agent.ID {
		t.Fatalf("expected round-tripped config, got %+v", got)
	}

	// 4. Absent id and foreign workspace read as (nil, nil) — no leak.
	if none, err := st.GetGateway(ctx, ws.ID, "00000000-0000-0000-0000-000000000099"); err != nil || none != nil {
		t.Fatalf("expected (nil, nil) for absent id, got (%v, %v)", none, err)
	}
	foreign, err := st.GetGateway(ctx, "00000000-0000-0000-0000-000000000002", g1.ID)
	if err != nil || foreign != nil {
		t.Fatalf("expected (nil, nil) for foreign workspace, got (%v, %v)", foreign, err)
	}

	// 5. Same workspace, same platform, same identity -> Conflict!
	dup := gwNewConfig(ws, agent)
	if err := st.CreateGateway(ctx, ws.ID, dup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate identity in same workspace+platform, got %v", err)
	}

	// 6. Same workspace, same platform, different identity (multi-bot) -> Success!
	agent2 := gwSeedAgent(t, ctx, s, ws, "beacon")
	g2 := gwNewConfig(ws, agent2)
	g2.Identity = "@second_bot"
	g2.BotUsername = "second_bot"
	if err := st.CreateGateway(ctx, ws.ID, g2); err != nil {
		t.Fatalf("unexpected create second bot error: %v", err)
	}

	// 7. Two workspaces, same bot name/identity -> Success!
	gWs2 := &domain.GatewayConfig{
		WorkspaceID:        crossWs.ID,
		Platform:           domain.GatewayPlatformTelegram,
		Identity:           "@onclaw_bot", // same identity as g1 in ws
		AgentID:            crossAgent.ID,
		BotTokenCiphertext: "v1:bm9uY2U=:Y2lwaGVydGV4dA==",
		BotUsername:        "onclaw_bot",
		Enabled:            true,
		Transport:          domain.GatewayTransportLongPolling,
	}
	if err := st.CreateGateway(ctx, crossWs.ID, gWs2); err != nil {
		t.Fatalf("expected cross-workspace same identity allowed, got %v", err)
	}

	// 8. ListGateways and ListGatewaysByPlatform
	list, err := st.ListGateways(ctx, ws.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("expected 2 gateways in ws, got %d (%v)", len(list), err)
	}
	tgList, err := st.ListGatewaysByPlatform(ctx, ws.ID, domain.GatewayPlatformTelegram)
	if err != nil || len(tgList) != 2 {
		t.Fatalf("expected 2 telegram gateways in ws, got %d (%v)", len(tgList), err)
	}
	waList, err := st.ListGatewaysByPlatform(ctx, ws.ID, domain.GatewayPlatformWhatsApp)
	if err != nil || len(waList) != 0 {
		t.Fatalf("expected 0 whatsapp gateways in ws, got %d (%v)", len(waList), err)
	}

	// 9. UpdateGateway
	g1.BotTokenCiphertext = "v1:bm9uY2Uy:Y2lwaGVydGV4dDI="
	g1.BotUsername = "onclaw_bot_v2"
	g1.Identity = "@onclaw_bot_v2"
	g1.Enabled = false
	g1.AgentID = agent2.ID
	if err := st.UpdateGateway(ctx, ws.ID, g1); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	reloaded, _ := st.GetGateway(ctx, ws.ID, g1.ID)
	if reloaded.ID != g1.ID || !reloaded.CreatedAt.Equal(g1.CreatedAt) {
		t.Fatalf("expected id/created_at preserved, got %+v vs %+v", reloaded, g1)
	}
	if reloaded.BotTokenCiphertext != g1.BotTokenCiphertext || reloaded.BotUsername != "onclaw_bot_v2" || reloaded.Identity != "@onclaw_bot_v2" {
		t.Fatalf("expected connection fields rewritten, got %+v", reloaded)
	}
	if reloaded.AgentID != agent2.ID {
		t.Fatalf("expected agent rewritten, got %+v", reloaded.AgentID)
	}
	if reloaded.Enabled {
		t.Fatal("expected stored enabled flag to be false")
	}

	// 10. Update identity collision with g2
	g1.Identity = "@second_bot"
	if err := st.UpdateGateway(ctx, ws.ID, g1); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on update identity collision, got %v", err)
	}

	// 11. Enable toggle; absent rows are ErrNotFound.
	if err := st.SetGatewayEnabled(ctx, ws.ID, g1.ID, true); err != nil {
		t.Fatalf("unexpected enable error: %v", err)
	}
	reloaded, _ = st.GetGateway(ctx, ws.ID, g1.ID)
	if !reloaded.Enabled {
		t.Fatal("expected gateway enabled")
	}
	if err := st.SetGatewayEnabled(ctx, ws.ID, "00000000-0000-0000-0000-000000000099", true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for absent gateway, got %v", err)
	}

	// 12. Delete; the row is gone afterwards.
	if err := st.DeleteGateway(ctx, ws.ID, g1.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if err := st.DeleteGateway(ctx, ws.ID, g1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound re-deleting, got %v", err)
	}
}

// TestIntegration_GatewayStore_LaneRoundTrip covers the lane column
// (add-whatsapp-gateway design D1, composition wave A): WhatsApp rows carry
// cloud_api / multi_device through insert, read, and update, while a
// Telegram row keeps lane empty.
func TestIntegration_GatewayStore_LaneRoundTrip(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := gwSeedWorkspace(t, ctx, s, "gw-lane")
	agent := gwSeedAgent(t, ctx, s, ws, "atlas")
	st := s.Gateways()

	cloud := &domain.GatewayConfig{
		WorkspaceID:        ws.ID,
		Platform:           domain.GatewayPlatformWhatsApp,
		Lane:               domain.GatewayLaneCloudAPI,
		Identity:           "Smoke Cloud",
		AgentID:            agent.ID,
		BotTokenCiphertext: "v1:bm9uY2U=:Y2lwaGVydGV4dA==",
		BotUsername:        "Smoke Cloud",
		Enabled:            true,
		Transport:          domain.GatewayTransportWebhook,
		WebhookURL:         "https://cloud.example.com/api/v1/webhooks/whatsapp/" + ws.ID,
	}
	if err := st.CreateGateway(ctx, ws.ID, cloud); err != nil {
		t.Fatalf("unexpected cloud create error: %v", err)
	}
	got, err := st.GetGateway(ctx, ws.ID, cloud.ID)
	if err != nil || got == nil {
		t.Fatalf("unexpected cloud get result: (%v, %v)", got, err)
	}
	if got.Lane != domain.GatewayLaneCloudAPI || got.Transport != domain.GatewayTransportWebhook {
		t.Fatalf("expected cloud lane round-trip, got %+v", got)
	}

	// Lane switch on the row: the update rewrites
	// the discriminator along with the connection fields.
	cloud.Lane = domain.GatewayLaneMultiDevice
	cloud.Identity = "15551234567"
	cloud.BotUsername = "15551234567"
	cloud.Transport = domain.GatewayTransportLongPolling
	cloud.WebhookURL = ""
	cloud.BotTokenCiphertext = ""
	if err := st.UpdateGateway(ctx, ws.ID, cloud); err != nil {
		t.Fatalf("unexpected multi-device update error: %v", err)
	}
	got, err = st.GetGateway(ctx, ws.ID, cloud.ID)
	if err != nil || got == nil {
		t.Fatalf("unexpected multi-device get result: (%v, %v)", got, err)
	}
	if got.Lane != domain.GatewayLaneMultiDevice {
		t.Fatalf("expected lane rewritten to multi_device, got %+v", got)
	}
	if got.BotTokenCiphertext != "" {
		t.Fatalf("expected credential cleared on the multi-device lane, got %+v", got)
	}

	// A Telegram row keeps lane NULL/empty (read back as the empty string).
	tg := gwNewConfig(ws, agent)
	if err := st.CreateGateway(ctx, ws.ID, tg); err != nil {
		t.Fatalf("unexpected telegram create error: %v", err)
	}
	got, err = st.GetGateway(ctx, ws.ID, tg.ID)
	if err != nil || got == nil {
		t.Fatalf("unexpected telegram get result: (%v, %v)", got, err)
	}
	if got.Lane != "" {
		t.Fatalf("expected telegram lane empty, got %q", got.Lane)
	}
}

// TestIntegration_GatewayStore_NullLaneReadsEmpty is the upgraded-deployment
// regression for migration 000051: the lane column is added nullable with no
// backfill, so every pre-migration row carries SQL NULL. The scan must
// tolerate it (Lane reads "") instead of failing the whole load.
func TestIntegration_GatewayStore_NullLaneReadsEmpty(t *testing.T) {
	s, schemaDSN, ctx := setupTestSchema(t)
	ws := gwSeedWorkspace(t, ctx, s, "gw-null-lane")
	agent := gwSeedAgent(t, ctx, s, ws, "atlas")
	st := s.Gateways()

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer conn.Close(ctx)

	// Raw pre-migration-shaped row: lane omitted → SQL NULL, everything else
	// exactly what a gateway row holds.
	var id string
	if err := conn.QueryRow(ctx, `
		INSERT INTO workspace_gateways (
			workspace_id, platform, identity, agent_id, bot_token_ciphertext, bot_username, enabled,
			transport, webhook_url
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text
	`, ws.ID, domain.GatewayPlatformTelegram, "legacy_bot", agent.ID, "v1:bm9uY2U=:Y2lwaGVydGV4dA==", "legacy_bot", true,
		domain.GatewayTransportLongPolling, "").Scan(&id); err != nil {
		t.Fatalf("failed to insert raw NULL-lane gateway row: %v", err)
	}

	// The column is genuinely NULL (guard the test's own premise).
	var lane *string
	if err := conn.QueryRow(ctx,
		`SELECT lane FROM workspace_gateways WHERE workspace_id = $1 AND id = $2`,
		ws.ID, id,
	).Scan(&lane); err != nil {
		t.Fatalf("failed to probe raw lane column: %v", err)
	}
	if lane != nil {
		t.Fatalf("test premise broken: expected SQL NULL lane, got %q", *lane)
	}

	got, err := st.GetGateway(ctx, ws.ID, id)
	if err != nil {
		t.Fatalf("expected NULL-lane row to load, got error: %v", err)
	}
	if got == nil {
		t.Fatal("expected NULL-lane row to load, got nil")
	}
	if got.Lane != "" {
		t.Fatalf("expected NULL lane to read as empty string, got %q", got.Lane)
	}

	// ListGateways runs the same scan — the manager's boot path lists rows.
	list, err := st.ListGateways(ctx, ws.ID)
	if err != nil {
		t.Fatalf("expected NULL-lane row to list, got error: %v", err)
	}
	if len(list) != 1 || list[0].Lane != "" {
		t.Fatalf("expected one NULL-lane row listed with empty lane, got %+v", list)
	}
}

func TestIntegration_GatewayBindings(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := gwSeedWorkspace(t, ctx, s, "gw-bind")
	agent := gwSeedAgent(t, ctx, s, ws, "atlas")
	gw := gwNewConfig(ws, agent)
	if err := s.Gateways().CreateGateway(ctx, ws.ID, gw); err != nil {
		t.Fatalf("unexpected create gateway error: %v", err)
	}
	st := s.GatewayBindings()

	// 1. Unknown agent fails as NotFound (workspace-scoped FK parity).
	orphan := &domain.ChatBinding{GatewayID: gw.ID, Platform: domain.GatewayPlatformTelegram, PlatformChatID: "-100", AgentID: "00000000-0000-0000-0000-000000000009"}
	if err := st.CreateChatBinding(ctx, ws.ID, orphan); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown agent, got %v", err)
	}

	binding := &domain.ChatBinding{GatewayID: gw.ID, Platform: domain.GatewayPlatformTelegram, PlatformChatID: "-1001234567890", AgentID: agent.ID}
	if err := st.CreateChatBinding(ctx, ws.ID, binding); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if binding.ID == "" || binding.CreatedAt.IsZero() {
		t.Fatalf("expected id/created_at assigned, got %+v", binding)
	}

	// 2. The same chat cannot bind twice — the unique constraint is global,
	// so a second workspace binding the same chat also conflicts.
	dup := &domain.ChatBinding{GatewayID: gw.ID, Platform: domain.GatewayPlatformTelegram, PlatformChatID: "-1001234567890", AgentID: agent.ID}
	if err := st.CreateChatBinding(ctx, ws.ID, dup); !errors.Is(err, domain.ErrGatewayBindingConflict) {
		t.Fatalf("expected ErrGatewayBindingConflict, got %v", err)
	}

	// 3. Routing lookup; unbound chats read (nil, nil).
	got, err := st.GetChatBinding(ctx, ws.ID, domain.GatewayPlatformTelegram, "-1001234567890")
	if err != nil || got == nil || got.AgentID != agent.ID || got.GatewayID != gw.ID {
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
	if err != nil || moved == nil || moved.ID != binding.ID || moved.GatewayID != gw.ID {
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
	if err != nil || len(list) != 1 || list[0].GatewayID != gw.ID {
		t.Fatalf("expected 1 binding with gateway_id, got %d (%v)", len(list), err)
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

// TestIntegration_GatewayOutbox_CountDead covers the health probe's
// dead-delivery signal (add-whatsapp-gateway design D4): the count filters
// on status AND the payload's gateway_id — the only place the generating
// gateway is recorded.
func TestIntegration_GatewayOutbox_CountDead(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := gwSeedWorkspace(t, ctx, s, "gw-outbox-dead")
	other := gwSeedWorkspace(t, ctx, s, "gw-outbox-dead-b")
	st := s.GatewayOutbox()

	deadA := &domain.OutboxEntry{WorkspaceID: ws.ID, SessionID: "wa_dm_111", Payload: []byte(`{"gateway_id":"gw-a","chat_id":"111"}`)}
	deadA2 := &domain.OutboxEntry{WorkspaceID: ws.ID, SessionID: "wa_dm_112", Payload: []byte(`{"gateway_id":"gw-a","chat_id":"112"}`)}
	deadB := &domain.OutboxEntry{WorkspaceID: ws.ID, SessionID: "wa_dm_121", Payload: []byte(`{"gateway_id":"gw-b","chat_id":"121"}`)}
	pendingA := &domain.OutboxEntry{WorkspaceID: ws.ID, SessionID: "wa_dm_113", Payload: []byte(`{"gateway_id":"gw-a","chat_id":"113"}`)}
	foreign := &domain.OutboxEntry{WorkspaceID: other.ID, SessionID: "wa_dm_211", Payload: []byte(`{"gateway_id":"gw-a","chat_id":"211"}`)}
	for _, e := range []*domain.OutboxEntry{deadA, deadA2, deadB, pendingA, foreign} {
		if err := st.Enqueue(ctx, e); err != nil {
			t.Fatalf("unexpected enqueue error: %v", err)
		}
	}
	for _, e := range []*domain.OutboxEntry{deadA, deadA2, deadB, foreign} {
		if err := st.MarkDead(ctx, e.WorkspaceID, e.ID); err != nil {
			t.Fatalf("unexpected dead error: %v", err)
		}
	}

	// The gateway's own dead entries only: other gateways' entries, pending
	// entries, and other workspaces' entries never count.
	n, err := st.CountDead(ctx, ws.ID, "gw-a")
	if err != nil || n != 2 {
		t.Fatalf("expected 2 dead entries for gw-a, got %d (%v)", n, err)
	}
	n, err = st.CountDead(ctx, ws.ID, "gw-b")
	if err != nil || n != 1 {
		t.Fatalf("expected 1 dead entry for gw-b, got %d (%v)", n, err)
	}
	n, err = st.CountDead(ctx, ws.ID, "gw-unknown")
	if err != nil || n != 0 {
		t.Fatalf("expected 0 dead entries for an unknown gateway, got %d (%v)", n, err)
	}
	n, err = st.CountDead(ctx, other.ID, "gw-a")
	if err != nil || n != 1 {
		t.Fatalf("expected 1 dead entry in the foreign workspace, got %d (%v)", n, err)
	}
	// Empty arguments read as "no question asked" (0), matching the
	// workspace-scoped read convention.
	n, err = st.CountDead(ctx, "", "gw-a")
	if err != nil || n != 0 {
		t.Fatalf("expected 0 for empty workspace, got %d (%v)", n, err)
	}
}
