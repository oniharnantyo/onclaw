package gateways

import (
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// routerFixture reuses the service fixture's stores with a deterministic
// pairing service.
func TestRouterDMRoutesToGatewayAgent(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")

	res, err := f.router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "hey"))
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if res.Turn == nil {
		t.Fatalf("expected a turn, got %+v", res)
	}
	if res.Turn.AgentID != f.atlas.ID {
		t.Fatalf("expected gateway agent Atlas, got %s", res.Turn.AgentID)
	}
	if res.Turn.SessionID != GatewayDMSessionKey("593821092", f.atlas.ID, 0) {
		t.Fatalf("unexpected session key %q", res.Turn.SessionID)
	}
}

func TestRouterUnpairedSenderRefused(t *testing.T) {
	f := newFixture(t)

	res, err := f.router.Route(f.ctx, f.gateway, f.dmMsg("999", "hey"))
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if res.Turn != nil {
		t.Fatalf("unpaired sender minted a run")
	}
	if res.Reply == nil || !strings.Contains(res.Reply.Text, "/start") {
		t.Fatalf("expected pairing hint, got %+v", res.Reply)
	}
}

func TestRouterStalePairingRefused(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")
	// The member was removed from the workspace after pairing.
	if err := f.st.Members().Remove(f.ctx, f.ws.ID, f.user.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}

	res, err := f.router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "hey"))
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if res.Turn != nil {
		t.Fatalf("stale pairing minted a run")
	}
	if res.Reply == nil {
		t.Fatalf("expected refusal hint")
	}
}

func TestRouterGroupSecondBotSilentlyIgnored(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")

	if err := f.st.GatewayBindings().CreateChatBinding(f.ctx, f.ws.ID, &domain.ChatBinding{
		WorkspaceID:    f.ws.ID,
		GatewayID:      "other-gateway-id",
		Platform:       domain.GatewayPlatformTelegram,
		PlatformChatID: "-100123",
		AgentID:        f.atlas.ID,
	}); err != nil {
		t.Fatalf("bind group: %v", err)
	}

	msg := InboundMessage{
		Platform: domain.GatewayPlatformTelegram, ChatID: "-100123", Kind: InboundGroup,
		MessageID: "5", FromUserID: "593821092", FromUsername: "onih",
		Text: "check the dashboard",
	}
	res, err := f.router.Route(f.ctx, f.gateway, msg)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if res.Turn != nil || res.Reply == nil || res.Reply.Text != "" {
		t.Fatalf("expected silent drop for second bot in group, got %+v", res)
	}
}

func TestRouterGroupBindsToBoundAgentWithAttribution(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")

	if err := f.st.GatewayBindings().CreateChatBinding(f.ctx, f.ws.ID, &domain.ChatBinding{
		WorkspaceID:    f.ws.ID,
		Platform:       domain.GatewayPlatformTelegram,
		PlatformChatID: "-100123",
		AgentID:        f.atlas.ID,
	}); err != nil {
		t.Fatalf("bind group: %v", err)
	}

	msg := InboundMessage{
		Platform: domain.GatewayPlatformTelegram, ChatID: "-100123", Kind: InboundGroup,
		MessageID: "5", FromUserID: "593821092", FromUsername: "onih",
		Text: "check the dashboard",
	}
	res, err := f.router.Route(f.ctx, f.gateway, msg)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if res.Turn == nil {
		t.Fatalf("expected a turn, got %+v", res)
	}
	if res.Turn.AgentID != f.atlas.ID {
		t.Fatalf("expected bound agent, got %s", res.Turn.AgentID)
	}
	if res.Turn.SessionID != GatewayGroupSessionKey("-100123", f.atlas.ID, 0) {
		t.Fatalf("unexpected group session key %q", res.Turn.SessionID)
	}
	if !strings.HasPrefix(res.Turn.Input, "[Oni (@onih)]: ") {
		t.Fatalf("expected attribution prefix, got %q", res.Turn.Input)
	}
}

func TestRouterGroupWithoutBindingDropped(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")

	res, err := f.router.Route(f.ctx, f.gateway, InboundMessage{
		Platform: domain.GatewayPlatformTelegram, ChatID: "-100123", Kind: InboundGroup,
		FromUserID: "593821092", Text: "check the dashboard",
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if res.Turn != nil || res.Reply == nil || res.Reply.Text != "" {
		t.Fatalf("expected silent drop, got %+v", res)
	}
}

func TestRouterNewCommandBumpsSuffix(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")

	// Two /new commands mint suffixes 1 and 2.
	for want := int64(1); want <= 2; want++ {
		res, err := f.router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "/new"))
		if err != nil {
			t.Fatalf("route /new: %v", err)
		}
		if res.Turn != nil || res.Reply == nil {
			t.Fatalf("expected a confirmation reply, got %+v", res)
		}
		if !strings.Contains(res.Reply.Text, GatewayDMSessionKey("593821092", f.atlas.ID, want)) {
			t.Fatalf("expected session key with suffix %d in reply %q", want, res.Reply.Text)
		}
	}

	// The next ordinary message runs on the highest suffix.
	res, err := f.router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "hello again"))
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if res.Turn == nil || res.Turn.SessionID != GatewayDMSessionKey("593821092", f.atlas.ID, 2) {
		t.Fatalf("expected active key with suffix 2, got %+v", res.Turn)
	}
}

func TestRouterCompactRunsAsCommandTurn(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")

	res, err := f.router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "/compact"))
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if res.Turn == nil || res.Turn.Command != "compact" {
		t.Fatalf("expected compact command turn, got %+v", res.Turn)
	}
	if res.Turn.SessionID != GatewayDMSessionKey("593821092", f.atlas.ID, 0) {
		t.Fatalf("compact must not bump the suffix, got %q", res.Turn.SessionID)
	}
}

func TestRouterCompactInBoundGroup(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")
	if err := f.st.GatewayBindings().CreateChatBinding(f.ctx, f.ws.ID, &domain.ChatBinding{
		WorkspaceID: f.ws.ID, Platform: domain.GatewayPlatformTelegram,
		PlatformChatID: "-100123", AgentID: f.atlas.ID,
	}); err != nil {
		t.Fatalf("bind group: %v", err)
	}

	res, err := f.router.Route(f.ctx, f.gateway, InboundMessage{
		Platform: domain.GatewayPlatformTelegram, ChatID: "-100123", Kind: InboundGroup,
		FromUserID: "593821092", FromUsername: "onih", Text: "/new",
	})
	if err != nil {
		t.Fatalf("route /new in group: %v", err)
	}
	if res.Reply == nil || !strings.Contains(res.Reply.Text, GatewayGroupSessionKey("-100123", f.atlas.ID, 1)) {
		t.Fatalf("expected group suffix bump, got %+v", res)
	}
}

func TestRouterUsageCommand(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")

	// No usage yet.
	res, err := f.router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "/usage"))
	if err != nil {
		t.Fatalf("route /usage: %v", err)
	}
	if res.Reply == nil || !strings.Contains(res.Reply.Text, "No usage recorded") {
		t.Fatalf("expected no-usage reply, got %+v", res.Reply)
	}

	// With usage numbers.
	usageFixture := agents.UsagePayload{InputTokens: 300, OutputTokens: 200, TotalTokens: 500, FinalInputTokens: 900}
	f.router.usage = fakeUsageReader{usage: &usageFixture}
	res, err = f.router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "/usage"))
	if err != nil {
		t.Fatalf("route /usage: %v", err)
	}
	if res.Reply == nil || !strings.Contains(res.Reply.Text, "Context at last call: 900 tokens.") {
		t.Fatalf("unexpected usage reply: %+v", res.Reply)
	}
}

func TestRouterAgentCommandRetiredHint(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")

	res, err := f.router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "/agent beacon"))
	if err != nil {
		t.Fatalf("route /agent: %v", err)
	}
	if res.Reply == nil || !strings.Contains(res.Reply.Text, "The bot you message selects the agent") {
		t.Fatalf("unexpected /agent reply: %+v", res.Reply)
	}
}

func TestRouterStartPairsIdentity(t *testing.T) {
	f := newFixture(t)

	token, err := f.pairing.MintToken(f.ctx, f.ws.ID, f.user.ID)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	res, err := f.router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "/start "+token.Token))
	if err != nil {
		t.Fatalf("route /start: %v", err)
	}
	if res.Reply == nil || !strings.Contains(res.Reply.Text, "Paired") {
		t.Fatalf("expected pairing confirmation, got %+v", res.Reply)
	}

	link, err := f.st.GatewayLinks().GetUserLink(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, "593821092")
	if err != nil || link == nil {
		t.Fatalf("identity not linked: %v %v", link, err)
	}
}

func TestRouterStartTokenExpiryRefused(t *testing.T) {
	f := newFixture(t)
	token, err := f.pairing.MintToken(f.ctx, f.ws.ID, f.user.ID)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	// The router consumes the token well past the TTL.
	f.router.pairing = NewPairingService(f.st.GatewayLinks(), WithPairingClock(func() time.Time {
		return time.Now().Add(2 * PairingTokenTTL)
	}))
	res, err := f.router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "/start "+token.Token))
	if err != nil {
		t.Fatalf("route /start: %v", err)
	}
	if res.Reply == nil || !strings.Contains(res.Reply.Text, "invalid, expired") {
		t.Fatalf("expected refusal for expired token, got %+v", res.Reply)
	}
	link, _ := f.st.GatewayLinks().GetUserLink(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, "593821092")
	if link != nil {
		t.Fatalf("no link must be created for an expired token")
	}
}

func TestRouterMigrateToChatIDRemapsBinding(t *testing.T) {
	f := newFixture(t)
	if err := f.st.GatewayBindings().CreateChatBinding(f.ctx, f.ws.ID, &domain.ChatBinding{
		WorkspaceID: f.ws.ID, Platform: domain.GatewayPlatformTelegram,
		PlatformChatID: "-100old", AgentID: f.atlas.ID,
	}); err != nil {
		t.Fatalf("bind group: %v", err)
	}

	res, err := f.router.Route(f.ctx, f.gateway, InboundMessage{
		Platform: domain.GatewayPlatformTelegram, ChatID: "-100old", Kind: InboundGroup,
		FromUserID: "593821092", MigrateToChatID: "-100new",
	})
	if err != nil {
		t.Fatalf("route migration: %v", err)
	}
	if res.Turn != nil || res.Reply != nil {
		t.Fatalf("migration message must be dropped, got %+v", res)
	}

	binding, err := f.st.GatewayBindings().GetChatBinding(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, "-100new")
	if err != nil || binding == nil {
		t.Fatalf("binding not remapped: %v %v", binding, err)
	}
}

func TestRouterAddressedCommandSuffixStripped(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")

	res, err := f.router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "/new@onclaw_bot"))
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if res.Reply == nil || !strings.Contains(res.Reply.Text, GatewayDMSessionKey("593821092", f.atlas.ID, 1)) {
		t.Fatalf("expected /new@bot to route as /new, got %+v", res)
	}
}

// -------------------------------------------------------------------------
// Text-reply approval interception (add-whatsapp-gateway design D3, task
// 2.4/2.8e): the router synthesizes a Callback from a DM while a card is
// pending on a CanButton=false platform.
// -------------------------------------------------------------------------

// routerWithIntercept builds the fixture's router wired to an approval
// bridge over a capability-matrix adapter.
func routerWithIntercept(t *testing.T, f *gwFixture, canButton bool) (*Router, *ApprovalBridge) {
	t.Helper()
	adapter := newTestPlatformAdapterWithCaps(true, canButton)
	submitter := &testRunSubmitter{resumeStream: agents.NewEventStream(8)}
	bridge := NewApprovalBridge(submitter, adapter, f.st.GatewayLinks())
	router := NewRouter(
		f.st.Gateways(), f.st.GatewayBindings(), f.st.GatewayLinks(), f.st.Agents(),
		f.st.Members(), f.st.Users(), nil, fakeUsageReader{},
		WithApprovalIntercept(bridge),
	)
	router.pairing = f.pairing
	return router, bridge
}

func approvalPendingSession(f *gwFixture) StreamSession {
	return StreamSession{
		GatewayID:   f.gateway.ID,
		WorkspaceID: f.ws.ID,
		SessionID:   GatewayDMSessionKey("593821092", f.atlas.ID, 0),
		ChatID:      "593821092",
	}
}

func TestRouterInterceptsApprovalTextReply(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")
	router, bridge := routerWithIntercept(t, f, false)
	if err := bridge.Present(f.ctx, approvalPendingSession(f), testExecRequest(), agents.ApprovalPayload{InterruptID: "int-7", Command: "deploy.sh"}); err != nil {
		t.Fatalf("Present: %v", err)
	}

	cases := []struct {
		text     string
		approved bool
	}{
		{"APPROVE", true},
		{"approve", true},
		{"  Approve  ", true},
		{"DENY", false},
		{"deny", false},
	}
	for _, tc := range cases {
		res, err := router.Route(f.ctx, f.gateway, f.dmMsg("593821092", tc.text))
		if err != nil {
			t.Fatalf("route %q: %v", tc.text, err)
		}
		if res.Callback == nil {
			t.Fatalf("%q must synthesize an approval callback, got %+v", tc.text, res)
		}
		cb := res.Callback
		wantData := EncodeApprovalCallback("int-7", tc.approved)
		if cb.Data != wantData {
			t.Fatalf("%q callback data = %q, want %q", tc.text, cb.Data, wantData)
		}
		if cb.Platform != domain.GatewayPlatformTelegram || cb.ChatID != "593821092" || cb.FromUserID != "593821092" {
			t.Fatalf("callback coordinates wrong: %+v", cb)
		}
		if cb.MessageID != "card-101" {
			t.Fatalf("callback must address the pending card, got message id %q", cb.MessageID)
		}
	}
}

func TestRouterInterceptNonMatchingTextGetsNotice(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")
	router, bridge := routerWithIntercept(t, f, false)
	if err := bridge.Present(f.ctx, approvalPendingSession(f), testExecRequest(), agents.ApprovalPayload{InterruptID: "int-7"}); err != nil {
		t.Fatalf("Present: %v", err)
	}

	res, err := router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "yes please do it"))
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if res.Callback != nil || res.Turn != nil {
		t.Fatalf("non-matching text must neither decide nor mint a turn, got %+v", res)
	}
	if res.Reply == nil || res.Reply.Text != PendingApprovalTextReply {
		t.Fatalf("expected the text-reply pending notice, got %+v", res.Reply)
	}
}

func TestRouterInterceptIdleSessionRoutesNormally(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")
	router, _ := routerWithIntercept(t, f, false)

	res, err := router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "APPROVE"))
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if res.Callback != nil {
		t.Fatalf("nothing is pending: text must ride as an ordinary turn, got %+v", res.Callback)
	}
	if res.Turn == nil {
		t.Fatalf("expected an ordinary turn, got %+v", res)
	}
}

func TestRouterButtonPlatformNeverInterceptsText(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")
	router, bridge := routerWithIntercept(t, f, true)
	if err := bridge.Present(f.ctx, approvalPendingSession(f), testExecRequest(), agents.ApprovalPayload{InterruptID: "int-7"}); err != nil {
		t.Fatalf("Present: %v", err)
	}

	res, err := router.Route(f.ctx, f.gateway, f.dmMsg("593821092", "APPROVE"))
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if res.Callback != nil || res.Reply != nil {
		t.Fatalf("button platforms decide by callback, text rides as a turn: %+v", res)
	}
	if res.Turn == nil {
		t.Fatalf("expected an ordinary turn, got %+v", res)
	}
}

func TestRouterWhatsAppDMUsesWAPrefix(t *testing.T) {
	f := newFixture(t)
	f.pairUserWhatsApp("15551234567", "")

	res, err := f.router.Route(f.ctx, f.gateway, InboundMessage{
		Platform:   domain.GatewayPlatformWhatsApp,
		ChatID:     "15551234567",
		Kind:       InboundDM,
		MessageID:  "wamid.1",
		FromUserID: "15551234567",
		Text:       "hello",
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if res.Turn == nil {
		t.Fatalf("expected a turn, got %+v", res)
	}
	want := "wa_dm_15551234567_" + f.atlas.ID
	if res.Turn.SessionID != want {
		t.Fatalf("expected digits-normalized wa_dm_ session key %q, got %q", want, res.Turn.SessionID)
	}
}
