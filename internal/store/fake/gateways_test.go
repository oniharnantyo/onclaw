package fake_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// gatewayFixture seeds a workspace, provider, agent, and member user —
// everything a gateway row can reference.
type gatewayFixture struct {
	Store       store.Store
	WorkspaceID string
	AgentID     string
	UserID      string
}

func seedGatewayFixture(t *testing.T) gatewayFixture {
	t.Helper()
	ctx := context.Background()
	s := fake.New()

	u := &domain.User{Email: "gateway-admin@example.com", Name: "Admin"}
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ws := &domain.Workspace{Slug: "gateway-ws", Name: "Gateway WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Name: "main", Type: "openai"}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	a := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  p.ID,
		Model:       "gpt",
	}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return gatewayFixture{Store: s, WorkspaceID: ws.ID, AgentID: a.ID, UserID: u.ID}
}

func newGatewayConfig(f gatewayFixture) *domain.GatewayConfig {
	return &domain.GatewayConfig{
		WorkspaceID:        f.WorkspaceID,
		Platform:           domain.GatewayPlatformTelegram,
		Identity:           "@onclaw_bot",
		AgentID:            f.AgentID,
		BotTokenCiphertext: "v1:bm9uY2U=:Y2lwaGVydGV4dA==",
		BotUsername:        "onclaw_bot",
		Enabled:            true,
		Transport:          domain.GatewayTransportLongPolling,
	}
}

func newPairingToken(f gatewayFixture) *domain.PairingToken {
	return &domain.PairingToken{
		Token:       strings.Repeat("a", 43),
		WorkspaceID: f.WorkspaceID,
		UserID:      f.UserID,
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	}
}

func TestFakeGatewayStore_CRUDAndMultiBot(t *testing.T) {
	f := seedGatewayFixture(t)
	ctx := context.Background()
	st := f.Store.Gateways()

	// 1. Unknown workspace fails as NotFound (FK parity).
	ghost := newGatewayConfig(f)
	if err := st.CreateGateway(ctx, "00000000-0000-0000-0000-000000000000", ghost); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}

	// 2. Unknown or foreign agent fails as NotFound (FK parity).
	foreignAgentCfg := newGatewayConfig(f)
	foreignAgentCfg.AgentID = "00000000-0000-0000-0000-000000000001"
	if err := st.CreateGateway(ctx, f.WorkspaceID, foreignAgentCfg); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown agent, got %v", err)
	}

	// 3. Create: id and timestamps assigned, read back intact.
	g1 := newGatewayConfig(f)
	if err := st.CreateGateway(ctx, f.WorkspaceID, g1); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if g1.ID == "" || g1.CreatedAt.IsZero() || g1.UpdatedAt.IsZero() {
		t.Fatalf("expected id/timestamps assigned, got %+v", g1)
	}
	got, err := st.GetGateway(ctx, f.WorkspaceID, g1.ID)
	if err != nil || got == nil {
		t.Fatalf("unexpected get result: (%v, %v)", got, err)
	}
	if got.BotTokenCiphertext != g1.BotTokenCiphertext || got.BotUsername != "onclaw_bot" || got.Identity != "@onclaw_bot" || !got.Enabled || got.AgentID != f.AgentID {
		t.Fatalf("expected round-tripped config, got %+v", got)
	}

	// 4. Same workspace, same platform, same identity -> Conflict!
	dup := newGatewayConfig(f)
	if err := st.CreateGateway(ctx, f.WorkspaceID, dup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate identity in same workspace+platform, got %v", err)
	}

	// 5. Same workspace, same platform, different identity (multi-bot) -> Success!
	g2 := newGatewayConfig(f)
	g2.Identity = "@second_bot"
	g2.BotUsername = "second_bot"
	if err := st.CreateGateway(ctx, f.WorkspaceID, g2); err != nil {
		t.Fatalf("unexpected create second bot error: %v", err)
	}

	// 6. Two workspaces, same bot name/identity -> Success!
	ws2 := &domain.Workspace{Slug: "gw-ws-2", Name: "Gateway WS 2"}
	if err := f.Store.Workspaces().Create(ctx, ws2); err != nil {
		t.Fatalf("seed ws2: %v", err)
	}
	p2 := &domain.ProviderConfig{WorkspaceID: ws2.ID, Name: "main2", Type: "openai"}
	if err := f.Store.Providers().Create(ctx, p2); err != nil {
		t.Fatalf("seed provider2: %v", err)
	}
	a2 := &domain.Agent{WorkspaceID: ws2.ID, Slug: "agent2", Name: "Agent 2", ProviderID: p2.ID, Model: "gpt"}
	if err := f.Store.Agents().Create(ctx, a2); err != nil {
		t.Fatalf("seed agent2: %v", err)
	}
	gWs2 := &domain.GatewayConfig{
		WorkspaceID:        ws2.ID,
		Platform:           domain.GatewayPlatformTelegram,
		Identity:           "@onclaw_bot", // same identity as g1 in ws1
		AgentID:            a2.ID,
		BotTokenCiphertext: "v1:bm9uY2U=:Y2lwaGVydGV4dA==",
		BotUsername:        "onclaw_bot",
		Enabled:            true,
		Transport:          domain.GatewayTransportLongPolling,
	}
	if err := st.CreateGateway(ctx, ws2.ID, gWs2); err != nil {
		t.Fatalf("expected cross-workspace same identity allowed, got %v", err)
	}

	// 7. ListGateways and ListGatewaysByPlatform
	list, err := st.ListGateways(ctx, f.WorkspaceID)
	if err != nil || len(list) != 2 {
		t.Fatalf("expected 2 gateways in ws1, got %d (%v)", len(list), err)
	}
	tgList, err := st.ListGatewaysByPlatform(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram)
	if err != nil || len(tgList) != 2 {
		t.Fatalf("expected 2 telegram gateways in ws1, got %d (%v)", len(tgList), err)
	}
	waList, err := st.ListGatewaysByPlatform(ctx, f.WorkspaceID, domain.GatewayPlatformWhatsApp)
	if err != nil || len(waList) != 0 {
		t.Fatalf("expected 0 whatsapp gateways in ws1, got %d (%v)", len(waList), err)
	}

	// 8. Update gateway
	g1.BotUsername = "onclaw_bot_renamed"
	g1.Identity = "@onclaw_bot_renamed"
	if err := st.UpdateGateway(ctx, f.WorkspaceID, g1); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	reloaded, _ := st.GetGateway(ctx, f.WorkspaceID, g1.ID)
	if reloaded.Identity != "@onclaw_bot_renamed" || reloaded.BotUsername != "onclaw_bot_renamed" {
		t.Fatalf("expected updated identity, got %+v", reloaded)
	}

	// 9. Update identity collision with g2
	g1.Identity = "@second_bot"
	if err := st.UpdateGateway(ctx, f.WorkspaceID, g1); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on update identity collision, got %v", err)
	}

	// 10. Enable toggle
	if err := st.SetGatewayEnabled(ctx, f.WorkspaceID, g1.ID, false); err != nil {
		t.Fatalf("unexpected disable error: %v", err)
	}
	reloaded, _ = st.GetGateway(ctx, f.WorkspaceID, g1.ID)
	if reloaded.Enabled {
		t.Fatal("expected gateway disabled")
	}

	// 11. Delete
	if err := st.DeleteGateway(ctx, f.WorkspaceID, g1.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if err := st.DeleteGateway(ctx, f.WorkspaceID, g1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound re-deleting, got %v", err)
	}
	if none, err := st.GetGateway(ctx, f.WorkspaceID, g1.ID); err != nil || none != nil {
		t.Fatalf("expected (nil, nil) after delete, got (%v, %v)", none, err)
	}
}

func TestFakeGatewayBindings_ConflictGetRemapDelete(t *testing.T) {
	f := seedGatewayFixture(t)
	ctx := context.Background()
	st := f.Store.GatewayBindings()

	// 1. Unknown agent reference fails as NotFound (FK parity, workspace-scoped).
	orphan := &domain.ChatBinding{Platform: domain.GatewayPlatformTelegram, PlatformChatID: "-100", AgentID: "00000000-0000-0000-0000-000000000009"}
	if err := st.CreateChatBinding(ctx, f.WorkspaceID, orphan); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown agent, got %v", err)
	}

	binding := &domain.ChatBinding{Platform: domain.GatewayPlatformTelegram, PlatformChatID: "-1001234567890", AgentID: f.AgentID, CreatedBy: &f.UserID}
	if err := st.CreateChatBinding(ctx, f.WorkspaceID, binding); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if binding.ID == "" {
		t.Fatal("expected binding id assigned")
	}

	// 2. Second binding for the same chat is a conflict — even from a
	// different workspace (the global one-agent-per-chat rule).
	dup := &domain.ChatBinding{Platform: domain.GatewayPlatformTelegram, PlatformChatID: "-1001234567890", AgentID: f.AgentID}
	if err := st.CreateChatBinding(ctx, f.WorkspaceID, dup); !errors.Is(err, domain.ErrGatewayBindingConflict) {
		t.Fatalf("expected ErrGatewayBindingConflict, got %v", err)
	}

	// 3. Routing lookup by (platform, chat); unbound chats read (nil, nil).
	got, err := st.GetChatBinding(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram, "-1001234567890")
	if err != nil || got == nil || got.AgentID != f.AgentID {
		t.Fatalf("unexpected binding lookup: (%v, %v)", got, err)
	}
	if none, err := st.GetChatBinding(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram, "-1999999999999"); err != nil || none != nil {
		t.Fatalf("expected (nil, nil) for unbound chat, got (%v, %v)", none, err)
	}

	// 4. Remap (migrate_to_chat_id): rewrites the chat id, lookup follows.
	if err := st.RemapBindingChat(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram, "-1001234567890", "-1098765432100"); err != nil {
		t.Fatalf("unexpected remap error: %v", err)
	}
	moved, err := st.GetChatBinding(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram, "-1098765432100")
	if err != nil || moved == nil || moved.ID != binding.ID {
		t.Fatalf("expected remapped binding, got (%v, %v)", moved, err)
	}
	if old, err := st.GetChatBinding(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram, "-1001234567890"); err != nil || old != nil {
		t.Fatalf("expected old chat id unbound, got (%v, %v)", old, err)
	}
	if err := st.RemapBindingChat(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram, "-1001234567890", "-1098765432100"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound remapping unknown chat, got %v", err)
	}

	// 5. Delete; foreign workspace and unknown ids are ErrNotFound.
	if err := st.DeleteChatBinding(ctx, "00000000-0000-0000-0000-000000000003", binding.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for foreign workspace, got %v", err)
	}
	if err := st.DeleteChatBinding(ctx, f.WorkspaceID, binding.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if err := st.DeleteChatBinding(ctx, f.WorkspaceID, binding.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound re-deleting, got %v", err)
	}
}

func TestFakeGatewayLinks_PairingLifecycle(t *testing.T) {
	f := seedGatewayFixture(t)
	ctx := context.Background()
	st := f.Store.GatewayLinks()

	// 1. Pairing token mint: shape validated, workspace + member pre-checked.
	token := newPairingToken(f)
	if err := st.CreatePairingToken(ctx, token); err != nil {
		t.Fatalf("unexpected mint error: %v", err)
	}
	badShape := newPairingToken(f)
	badShape.Token = "short"
	if err := st.CreatePairingToken(ctx, badShape); !errors.Is(err, domain.ErrInvalidPairingToken) {
		t.Fatalf("expected ErrInvalidPairingToken, got %v", err)
	}
	ghostUser := newPairingToken(f)
	ghostUser.UserID = "00000000-0000-0000-0000-000000000009"
	if err := st.CreatePairingToken(ctx, ghostUser); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown user, got %v", err)
	}

	// 2. Consume: wins once; second use and expired tokens are refused.
	now := time.Now().UTC()
	consumed, err := st.ConsumePairingToken(ctx, f.WorkspaceID, token.Token, now)
	if err != nil || consumed == nil || consumed.ConsumedAt == nil {
		t.Fatalf("expected consumed token, got (%v, %v)", consumed, err)
	}
	if _, err := st.ConsumePairingToken(ctx, f.WorkspaceID, token.Token, now); !errors.Is(err, domain.ErrPairingTokenExpired) {
		t.Fatalf("expected ErrPairingTokenExpired on second use, got %v", err)
	}

	expired := newPairingToken(f)
	expired.Token = strings.Repeat("b", 43)
	expired.ExpiresAt = now.Add(-time.Minute)
	if err := st.CreatePairingToken(ctx, expired); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid minting an already-expired token, got %v", err)
	}
	expired.ExpiresAt = now.Add(time.Hour)
	if err := st.CreatePairingToken(ctx, expired); err != nil {
		t.Fatalf("unexpected mint error: %v", err)
	}
	if _, err := st.ConsumePairingToken(ctx, f.WorkspaceID, expired.Token, now.Add(2*time.Hour)); !errors.Is(err, domain.ErrPairingTokenExpired) {
		t.Fatalf("expected ErrPairingTokenExpired for expired token, got %v", err)
	}

	// Unknown tokens are ErrNotFound, not expired.
	if _, err := st.ConsumePairingToken(ctx, f.WorkspaceID, strings.Repeat("c", 43), now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown token, got %v", err)
	}

	// 3. Identity link: duplicate platform identity is a conflict.
	link := &domain.UserLink{Platform: domain.GatewayPlatformTelegram, PlatformUserID: "593821092", WorkspaceID: f.WorkspaceID, UserID: f.UserID, PlatformUsername: "onih"}
	if err := st.CreateUserLink(ctx, f.WorkspaceID, link); err != nil {
		t.Fatalf("unexpected link error: %v", err)
	}
	if err := st.CreateUserLink(ctx, f.WorkspaceID, link); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate identity, got %v", err)
	}

	// 4. Routing lookup + per-member listing + unpair.
	got, err := st.GetUserLink(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram, "593821092")
	if err != nil || got == nil || got.UserID != f.UserID {
		t.Fatalf("expected paired member, got (%v, %v)", got, err)
	}
	memberLinks, err := st.ListUserLinksForMember(ctx, f.WorkspaceID, f.UserID)
	if err != nil || len(memberLinks) != 1 {
		t.Fatalf("expected 1 member link, got %d (%v)", len(memberLinks), err)
	}
	if err := st.DeleteUserLink(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram, "593821092"); err != nil {
		t.Fatalf("unexpected unpair error: %v", err)
	}
	if none, err := st.GetUserLink(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram, "593821092"); err != nil || none != nil {
		t.Fatalf("expected (nil, nil) after unpair, got (%v, %v)", none, err)
	}

	// 5. Revocation deletes an unconsumed token.
	revocable := newPairingToken(f)
	revocable.Token = strings.Repeat("d", 43)
	if err := st.CreatePairingToken(ctx, revocable); err != nil {
		t.Fatalf("unexpected mint error: %v", err)
	}
	if err := st.RevokePairingToken(ctx, f.WorkspaceID, revocable.Token); err != nil {
		t.Fatalf("unexpected revoke error: %v", err)
	}
	if _, err := st.ConsumePairingToken(ctx, f.WorkspaceID, revocable.Token, now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound consuming revoked token, got %v", err)
	}
}

func TestFakeGatewayOutbox_WriteClaimDeliverRedeliverPrune(t *testing.T) {
	f := seedGatewayFixture(t)
	ctx := context.Background()
	st := f.Store.GatewayOutbox()
	now := time.Now().UTC()

	// 1. Enqueue (write-before-send): born pending, attempts 0.
	entry := &domain.OutboxEntry{WorkspaceID: f.WorkspaceID, SessionID: "tg_dm_593821092_atlas", Payload: []byte(`{"text":"hi"}`)}
	if err := st.Enqueue(ctx, entry); err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}
	if entry.Status != domain.GatewayOutboxStatusPending || entry.Attempts != 0 {
		t.Fatalf("expected pending entry with 0 attempts, got %+v", entry)
	}

	// 2. Claim: attempts increment, exactly-once per claim. A pending entry
	// with a past deliver_after stays claimable — the attempt budget and
	// backoff are caller policy (Reschedule), the store only counts. The
	// claim instant carries a small margin over the enqueue-time default.
	claimed, err := st.ClaimDue(ctx, now.Add(time.Minute), 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("expected 1 claimed entry, got %d (%v)", len(claimed), err)
	}
	if claimed[0].ID != entry.ID || claimed[0].Attempts != 1 {
		t.Fatalf("expected the enqueued entry with attempts=1, got %+v", claimed[0])
	}
	claimed, err = st.ClaimDue(ctx, now.Add(time.Minute), 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("expected redelivery claim, got %d (%v)", len(claimed), err)
	}
	if claimed[0].Attempts != 2 {
		t.Fatalf("expected attempts=2 on redelivery, got %d", claimed[0].Attempts)
	}

	// 3. Deliver; future deliver_after entries are not claimable.
	if err := st.MarkDelivered(ctx, f.WorkspaceID, entry.ID); err != nil {
		t.Fatalf("unexpected deliver error: %v", err)
	}
	future := &domain.OutboxEntry{WorkspaceID: f.WorkspaceID, SessionID: "tg_group_-100_atlas", Payload: []byte(`{"text":"later"}`), DeliverAfter: now.Add(time.Hour)}
	if err := st.Enqueue(ctx, future); err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}
	claimed, err = st.ClaimDue(ctx, now, 10)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("expected no claimable entries, got %d (%v)", len(claimed), err)
	}

	// 4. Dead entries are never claimed again; workspace scoping holds on
	// the mark paths.
	if err := st.MarkDead(ctx, f.WorkspaceID, future.ID); err != nil {
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
	if err := st.Reschedule(ctx, f.WorkspaceID, future.ID, now.Add(2*time.Hour)); err != nil {
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

// TestFakeGatewayOutbox_CountDead mirrors the postgres adapter's
// dead-delivery count (add-whatsapp-gateway design D4): status AND payload
// gateway_id decide, foreign workspaces and pending entries never count.
func TestFakeGatewayOutbox_CountDead(t *testing.T) {
	f := seedGatewayFixture(t)
	ctx := context.Background()
	st := f.Store.GatewayOutbox()

	deadA := &domain.OutboxEntry{WorkspaceID: f.WorkspaceID, SessionID: "wa_dm_111", Payload: []byte(`{"gateway_id":"gw-a","chat_id":"111"}`)}
	deadA2 := &domain.OutboxEntry{WorkspaceID: f.WorkspaceID, SessionID: "wa_dm_112", Payload: []byte(`{"gateway_id":"gw-a","chat_id":"112"}`)}
	deadB := &domain.OutboxEntry{WorkspaceID: f.WorkspaceID, SessionID: "wa_dm_121", Payload: []byte(`{"gateway_id":"gw-b","chat_id":"121"}`)}
	pendingA := &domain.OutboxEntry{WorkspaceID: f.WorkspaceID, SessionID: "wa_dm_113", Payload: []byte(`{"gateway_id":"gw-a","chat_id":"113"}`)}
	for _, e := range []*domain.OutboxEntry{deadA, deadA2, deadB, pendingA} {
		if err := st.Enqueue(ctx, e); err != nil {
			t.Fatalf("unexpected enqueue error: %v", err)
		}
	}
	for _, e := range []*domain.OutboxEntry{deadA, deadA2, deadB} {
		if err := st.MarkDead(ctx, f.WorkspaceID, e.ID); err != nil {
			t.Fatalf("unexpected dead error: %v", err)
		}
	}

	n, err := st.CountDead(ctx, f.WorkspaceID, "gw-a")
	if err != nil || n != 2 {
		t.Fatalf("expected 2 dead entries for gw-a, got %d (%v)", n, err)
	}
	n, err = st.CountDead(ctx, f.WorkspaceID, "gw-b")
	if err != nil || n != 1 {
		t.Fatalf("expected 1 dead entry for gw-b, got %d (%v)", n, err)
	}
	n, err = st.CountDead(ctx, f.WorkspaceID, "gw-unknown")
	if err != nil || n != 0 {
		t.Fatalf("expected 0 dead entries for an unknown gateway, got %d (%v)", n, err)
	}
	n, err = st.CountDead(ctx, "00000000-0000-0000-0000-000000000009", "gw-a")
	if err != nil || n != 0 {
		t.Fatalf("expected 0 dead entries in a foreign workspace, got %d (%v)", n, err)
	}
}
