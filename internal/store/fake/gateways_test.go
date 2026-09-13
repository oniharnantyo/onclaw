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

func TestFakeGatewayStore_UpsertGetListEnableDelete(t *testing.T) {
	f := seedGatewayFixture(t)
	ctx := context.Background()
	st := f.Store.Gateways()

	// 1. Unknown workspace fails as NotFound (FK parity).
	ghost := newGatewayConfig(f)
	if err := st.UpsertGateway(ctx, "00000000-0000-0000-0000-000000000000", ghost); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}

	// 2. Create: id and timestamps assigned, read back intact.
	g := newGatewayConfig(f)
	if err := st.UpsertGateway(ctx, f.WorkspaceID, g); err != nil {
		t.Fatalf("unexpected upsert error: %v", err)
	}
	if g.ID == "" || g.CreatedAt.IsZero() || g.UpdatedAt.IsZero() {
		t.Fatalf("expected id/timestamps assigned, got %+v", g)
	}
	got, err := st.GetGateway(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram)
	if err != nil || got == nil {
		t.Fatalf("unexpected get result: (%v, %v)", got, err)
	}
	if got.BotTokenCiphertext != g.BotTokenCiphertext || got.BotUsername != "onclaw_bot" || !got.Enabled {
		t.Fatalf("expected round-tripped config, got %+v", got)
	}

	// 3. Absent platform and foreign workspace read as (nil, nil) — no leak.
	absent, err := st.GetGateway(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram)
	if err != nil || absent == nil {
		t.Fatalf("expected telegram config present, got (%v, %v)", absent, err)
	}
	if none, err := st.GetGateway(ctx, f.WorkspaceID, "slack"); err != nil || none != nil {
		t.Fatalf("expected (nil, nil) for absent platform, got (%v, %v)", none, err)
	}
	foreign, err := st.GetGateway(ctx, "00000000-0000-0000-0000-000000000001", domain.GatewayPlatformTelegram)
	if err != nil || foreign != nil {
		t.Fatalf("expected (nil, nil) for foreign workspace, got (%v, %v)", foreign, err)
	}

	// 4. Upsert conflict rewrites connection fields but keeps id,
	// created_at, and the enabled flag.
	reconnect := newGatewayConfig(f)
	reconnect.BotTokenCiphertext = "v1:bm9uY2Uy:Y2lwaGVydGV4dDI="
	reconnect.BotUsername = "onclaw_bot_v2"
	reconnect.Enabled = false // must NOT clobber the stored flag
	reconnect.DefaultAgentID = &f.AgentID
	if err := st.UpsertGateway(ctx, f.WorkspaceID, reconnect); err != nil {
		t.Fatalf("unexpected reconnect error: %v", err)
	}
	reloaded, _ := st.GetGateway(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram)
	if reloaded.ID != g.ID {
		t.Fatalf("expected stable id across upsert, got %s then %s", g.ID, reloaded.ID)
	}
	if reloaded.BotTokenCiphertext != reconnect.BotTokenCiphertext || reloaded.BotUsername != "onclaw_bot_v2" {
		t.Fatalf("expected connection fields rewritten, got %+v", reloaded)
	}
	if reloaded.DefaultAgentID == nil || *reloaded.DefaultAgentID != f.AgentID {
		t.Fatalf("expected default agent rewritten, got %+v", reloaded.DefaultAgentID)
	}
	if !reloaded.Enabled {
		t.Fatal("expected stored enabled flag to survive the upsert")
	}
	if !reloaded.CreatedAt.Equal(g.CreatedAt) {
		t.Fatalf("expected created_at preserved, got %v then %v", g.CreatedAt, reloaded.CreatedAt)
	}

	// 5. Enable toggle is standalone.
	if err := st.SetGatewayEnabled(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram, false); err != nil {
		t.Fatalf("unexpected disable error: %v", err)
	}
	reloaded, _ = st.GetGateway(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram)
	if reloaded.Enabled {
		t.Fatal("expected gateway disabled")
	}
	if err := st.SetGatewayEnabled(ctx, f.WorkspaceID, "slack", true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for absent platform, got %v", err)
	}

	// 6. List is workspace-scoped.
	list, err := st.ListGateways(ctx, f.WorkspaceID)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 gateway, got %d (%v)", len(list), err)
	}
	foreignList, err := st.ListGateways(ctx, "00000000-0000-0000-0000-000000000002")
	if err != nil || len(foreignList) != 0 {
		t.Fatalf("expected empty foreign list, got %d (%v)", len(foreignList), err)
	}

	// 7. Delete; absent rows afterwards are ErrNotFound.
	if err := st.DeleteGateway(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if err := st.DeleteGateway(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound re-deleting, got %v", err)
	}
	if none, err := st.GetGateway(ctx, f.WorkspaceID, domain.GatewayPlatformTelegram); err != nil || none != nil {
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
