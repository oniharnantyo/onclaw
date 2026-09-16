package gateways

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// -------------------------------------------------------------------------
// Shared fixture: a workspace with two agents, a paired member, a gateway,
// and the router/service wired over the fake store.
// -------------------------------------------------------------------------

type gwFixture struct {
	t       *testing.T
	st      store.Store
	ctx     context.Context
	ws      *domain.Workspace
	user    *domain.User
	atlas   *domain.Agent
	beacon  *domain.Agent
	gateway *domain.GatewayConfig
	pairing *PairingService
	router  *Router
}

func newFixture(t *testing.T) *gwFixture {
	t.Helper()
	ctx := context.Background()
	st := fake.New()

	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	user := &domain.User{Email: "oni@acme.test", Name: "Oni"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleMember, BuiltIn: true}
	if err := st.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	provider := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI"}
	if err := st.Providers().Create(ctx, provider); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	newAgent := func(name, slug string) *domain.Agent {
		a := &domain.Agent{WorkspaceID: ws.ID, Name: name, Slug: slug, ProviderID: provider.ID, Model: "gpt-test"}
		if err := st.Agents().Create(ctx, a); err != nil {
			t.Fatalf("create agent %s: %v", name, err)
		}
		return a
	}
	atlas := newAgent("Atlas", "atlas")
	beacon := newAgent("Beacon", "beacon")

		gateway := &domain.GatewayConfig{
			WorkspaceID:        ws.ID,
			Platform:           domain.GatewayPlatformTelegram,
			Identity:           "onclaw_bot",
			AgentID:            atlas.ID,
			BotTokenCiphertext: "v1:bm9uY2U6MTIzNDU2Nzg:Y2lwaGVydGV4dA",
			BotUsername:        "onclaw_bot",
			Enabled:            true,
			Transport:          domain.GatewayTransportLongPolling,
		}
		if err := st.Gateways().CreateGateway(ctx, ws.ID, gateway); err != nil {
			t.Fatalf("create gateway: %v", err)
		}

	f := &gwFixture{
		t:       t,
		st:      st,
		ctx:     ctx,
		ws:      ws,
		user:    user,
		atlas:   atlas,
		beacon:  beacon,
		gateway: gateway,
		pairing: NewPairingService(st.GatewayLinks()),
		router: NewRouter(
			st.Gateways(), st.GatewayBindings(), st.GatewayLinks(), st.Agents(),
			st.Members(), st.Users(), nil, fakeUsageReader{},
		),
	}
	// The router needs the pairing service, which is built after the
	// constructor above (same stores) — wire it directly.
	f.router.pairing = f.pairing
	return f
}

// pairUser links a Telegram identity to the fixture's member.
func (f *gwFixture) pairUser(platformUserID, username string) *domain.UserLink {
	f.t.Helper()
	token, err := f.pairing.MintToken(f.ctx, f.ws.ID, f.user.ID)
	if err != nil {
		f.t.Fatalf("mint token: %v", err)
	}
	link, err := f.pairing.Pair(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, platformUserID, username, token.Token)
	if err != nil {
		f.t.Fatalf("pair: %v", err)
	}
	return link
}

// pairUserWhatsApp links a WhatsApp identity (bare phone digits,
// add-whatsapp-gateway design D7) to the fixture's member.
func (f *gwFixture) pairUserWhatsApp(platformUserID, username string) *domain.UserLink {
	f.t.Helper()
	token, err := f.pairing.MintToken(f.ctx, f.ws.ID, f.user.ID)
	if err != nil {
		f.t.Fatalf("mint token: %v", err)
	}
	link, err := f.pairing.Pair(f.ctx, f.ws.ID, domain.GatewayPlatformWhatsApp, platformUserID, username, token.Token)
	if err != nil {
		f.t.Fatalf("pair: %v", err)
	}
	return link
}

// fakeUsageReader satisfies SessionUsageReader deterministically.
type fakeUsageReader struct {
	usage *agents.UsagePayload
	err   error
}

func (r fakeUsageReader) LatestUsage(context.Context, string, string) (*agents.UsagePayload, error) {
	return r.usage, r.err
}

// fakeRunSubmitter records ExecRequests and scripts responses.
type fakeRunSubmitter struct {
	mu    sync.Mutex
	reqs  []agents.ExecRequest
	next  func(call int, req agents.ExecRequest) (*agents.EventStream, error)
	calls int
}

func (s *fakeRunSubmitter) Run(_ context.Context, req agents.ExecRequest) (*agents.EventStream, error) {
	s.mu.Lock()
	call := s.calls
	s.calls++
	s.reqs = append(s.reqs, req)
	next := s.next
	s.mu.Unlock()
	if next == nil {
		stream := agents.NewEventStream(8)
		stream.Close()
		return stream, nil
	}
	return next(call, req)
}

func (s *fakeRunSubmitter) Resume(_ context.Context, req agents.ExecRequest, _ agents.ApprovalPayload, _ bool) (*agents.EventStream, error) {
	s.mu.Lock()
	s.calls++
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()
	stream := agents.NewEventStream(8)
	stream.Close()
	return stream, nil
}

func (s *fakeRunSubmitter) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// fakeChatAdapter is a PlatformAdapter that records sends and serves the
// service tests without network. Its capability matrix is
// constructor-parameterized (add-whatsapp-gateway design D2); the zero
// value is the Telegram cell (both capabilities on).
type fakeChatAdapter struct {
	mu        sync.Mutex
	canEdit   bool
	canButton bool
	sent      []fakeChatSend
	edits     []fakeChatEdit
	typing    []string
	cards     []agents.ApprovalPayload
}

type fakeChatSend struct {
	ChatID string
	HTML   string
	Flavor string
}

type fakeChatEdit struct {
	ChatID    string
	MessageID string
	HTML      string
	Flavor    string
}

// newFakeChatAdapterWithCaps builds the double for one capability cell.
func newFakeChatAdapterWithCaps(canEdit, canButton bool) *fakeChatAdapter {
	return &fakeChatAdapter{canEdit: canEdit, canButton: canButton}
}

func (a *fakeChatAdapter) Start(context.Context) error { return nil }
func (a *fakeChatAdapter) Stop(context.Context) error  { return nil }

func (a *fakeChatAdapter) Capabilities() AdapterCapabilities {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AdapterCapabilities{CanEdit: a.canEdit, CanButton: a.canButton}
}

func (a *fakeChatAdapter) SendMessage(_ context.Context, chatID, body, flavor string, _ SendOptions) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, fakeChatSend{ChatID: chatID, HTML: body, Flavor: flavor})
	return "msg-" + string(rune('0'+len(a.sent))), nil
}

func (a *fakeChatAdapter) EditMessage(_ context.Context, chatID, messageID, body, flavor string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.edits = append(a.edits, fakeChatEdit{ChatID: chatID, MessageID: messageID, HTML: body, Flavor: flavor})
	return nil
}

func (a *fakeChatAdapter) SendTyping(_ context.Context, chatID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.typing = append(a.typing, chatID)
	return nil
}

func (a *fakeChatAdapter) SendApprovalCard(_ context.Context, chatID string, interrupt agents.ApprovalPayload) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cards = append(a.cards, interrupt)
	a.sent = append(a.sent, fakeChatSend{ChatID: chatID, HTML: "card:" + interrupt.InterruptID})
	return "card-" + interrupt.InterruptID, nil
}

func (a *fakeChatAdapter) DownloadFile(context.Context, string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (a *fakeChatAdapter) sends() []fakeChatSend {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]fakeChatSend(nil), a.sent...)
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (f *gwFixture) dmMsg(fromUserID, text string) InboundMessage {
	return InboundMessage{
		Platform:     domain.GatewayPlatformTelegram,
		ChatID:       fromUserID,
		Kind:         InboundDM,
		MessageID:    "10",
		FromUserID:   fromUserID,
		FromUsername: "onih",
		Text:         text,
	}
}

// -------------------------------------------------------------------------
// Service orchestration tests
// -------------------------------------------------------------------------

func newServiceFixture(t *testing.T) (*gwFixture, *Service, *fakeRunSubmitter, *fakeChatAdapter) {
	f := newFixture(t)
	submitter := &fakeRunSubmitter{}
	adapter := &fakeChatAdapter{}
	bridge := NewApprovalBridge(submitter, adapter, f.st.GatewayLinks())
	// The outbox resolves send seams through the service's adapter registry
	// (assigned below, before any delivery can run).
	var svc *Service
	outbox := NewOutbox(f.st.GatewayOutbox(), func(gatewayID string) (MessageSender, bool) {
		return svc.Adapter(gatewayID)
	})
	svc = NewService(f.router, submitter, bridge, f.st.Gateways(), outbox)
	svc.AttachGateway(f.gateway.ID, f.ws.ID, f.gateway.Platform, adapter)
	return f, svc, submitter, adapter
}

func TestServiceRoutesDMRunUnderPairedIdentity(t *testing.T) {
	f, svc, submitter, _ := newServiceFixture(t)
	f.pairUser("593821092", "onih")

	svc.HandleMessage(f.ctx, f.gateway.ID, f.dmMsg("593821092", "hello Atlas"))

	waitFor(t, "run submission", func() bool { return submitter.requestCount() == 1 })
	submitter.mu.Lock()
	req := submitter.reqs[0]
	submitter.mu.Unlock()

	if req.WorkspaceID != f.ws.ID || req.AgentID != f.atlas.ID || req.UserID != f.user.ID {
		t.Fatalf("unexpected exec request coordinates: %+v", req)
	}
	if req.SessionID != GatewayDMSessionKey("593821092", f.atlas.ID, 0) {
		t.Fatalf("unexpected session key %q", req.SessionID)
	}
	if req.Input != "hello Atlas" {
		t.Fatalf("unexpected input %q", req.Input)
	}
	if req.Origin != OriginTelegram {
		t.Fatalf("unexpected origin %q", req.Origin)
	}
	// No channel artifacts (spec).
	if req.ChannelID != "" || req.RootMessageID != "" || req.WorkSessionID != "" || req.ChainDepth != 0 {
		t.Fatalf("channel coordinates must be zero: %+v", req)
	}
}

func TestServiceDisabledGatewayIsInert(t *testing.T) {
	f, svc, submitter, adapter := newServiceFixture(t)
	f.pairUser("593821092", "onih")
	if err := f.st.Gateways().SetGatewayEnabled(f.ctx, f.ws.ID, f.gateway.ID, false); err != nil {
		t.Fatalf("disable gateway: %v", err)
	}

	svc.HandleMessage(f.ctx, f.gateway.ID, f.dmMsg("593821092", "hello"))

	if submitter.requestCount() != 0 {
		t.Fatalf("disabled gateway minted a run")
	}
	if len(adapter.sends()) != 0 {
		t.Fatalf("disabled gateway replied: %v", adapter.sends())
	}
}

func TestServiceUnpairedSenderGetsHint(t *testing.T) {
	f, svc, submitter, adapter := newServiceFixture(t)

	svc.HandleMessage(f.ctx, f.gateway.ID, f.dmMsg("999", "hello"))

	if submitter.requestCount() != 0 {
		t.Fatalf("unpaired sender minted a run")
	}
	sends := adapter.sends()
	if len(sends) != 1 || !strings.Contains(sends[0].HTML, "/start") {
		t.Fatalf("expected pairing hint, got %v", sends)
	}
}

func TestServiceQueuesOnBusyAndResubmitsOnTerminal(t *testing.T) {
	f, svc, submitter, adapter := newServiceFixture(t)
	f.pairUser("593821092", "onih")

	submitter.mu.Lock()
	submitter.next = func(call int, _ agents.ExecRequest) (*agents.EventStream, error) {
		if call == 0 {
			return nil, domain.ErrConflict
		}
		stream := agents.NewEventStream(8)
		stream.Close()
		return stream, nil
	}
	submitter.mu.Unlock()

	svc.HandleMessage(f.ctx, f.gateway.ID, f.dmMsg("593821092", "first"))

	// First submission hit the busy guard: ack sent, nothing resubmitted yet.
	waitFor(t, "queued ack", func() bool {
		for _, s := range adapter.sends() {
			if strings.Contains(s.HTML, "Queued") {
				return true
			}
		}
		return false
	})
	if submitter.requestCount() != 1 {
		t.Fatalf("expected 1 submission while busy, got %d", submitter.requestCount())
	}
	if got := svc.queue.Depth(GatewayDMSessionKey("593821092", f.atlas.ID, 0)); got != 1 {
		t.Fatalf("expected queue depth 1, got %d", got)
	}

	// The drain of the active (foreign) run ends: queue releases the turn.
	svc.NotifyRunFinished(context.Background(), GatewayDMSessionKey("593821092", f.atlas.ID, 0))

	waitFor(t, "resubmission", func() bool { return submitter.requestCount() == 2 })
	if got := svc.queue.Depth(GatewayDMSessionKey("593821092", f.atlas.ID, 0)); got != 0 {
		t.Fatalf("expected queue drained, depth %d", got)
	}
}

func TestServiceRefusesTurnsWhileApprovalPending(t *testing.T) {
	f, svc, submitter, adapter := newServiceFixture(t)
	f.pairUser("593821092", "onih")

	bridge := svc.bridge
	err := bridge.Present(f.ctx, StreamSession{
		GatewayID: f.gateway.ID, WorkspaceID: f.ws.ID,
		SessionID: GatewayDMSessionKey("593821092", f.atlas.ID, 0), ChatID: "593821092",
	}, agents.ExecRequest{}, agents.ApprovalPayload{InterruptID: "int-1", Command: "rm -rf /"})
	if err != nil {
		t.Fatalf("present approval: %v", err)
	}

	svc.HandleMessage(f.ctx, f.gateway.ID, f.dmMsg("593821092", "another task"))

	if submitter.requestCount() != 0 {
		t.Fatalf("pending approval did not block the turn")
	}
	sends := adapter.sends()
	if len(sends) == 0 || !strings.Contains(sends[len(sends)-1].HTML, "approval is pending") {
		t.Fatalf("expected pending-approval notice, got %v", sends)
	}
}

func TestServiceApprovalCallbackDrainsResumedTurn(t *testing.T) {
	f, svc, submitter, adapter := newServiceFixture(t)
	f.pairUser("593821092", "onih")

	// Simulate a drain that paused on an approval: register the drain
	// coordinates and present the card the way drainTurn does.
	plan := TurnPlan{
		GatewayID: f.gateway.ID, WorkspaceID: f.ws.ID,
		AgentID: f.atlas.ID, SessionID: GatewayDMSessionKey("593821092", f.atlas.ID, 0),
		UserID: f.user.ID, Input: "run the risky thing",
		Kind: InboundDM, ChatID: "593821092",
	}
	err := svc.bridge.Present(f.ctx, StreamSession{
		GatewayID: plan.GatewayID, WorkspaceID: plan.WorkspaceID,
		SessionID: plan.SessionID, ChatID: plan.ChatID,
	}, agents.ExecRequest{}, agents.ApprovalPayload{InterruptID: "int-9", Command: "deploy.sh"})
	if err != nil {
		t.Fatalf("present approval: %v", err)
	}
	svc.approvalMu.Lock()
	svc.approvalChats[approvalDrainKey(plan.ChatID, "int-9")] = plan
	svc.approvalMu.Unlock()

	svc.HandleCallback(f.ctx, f.gateway.ID, Callback{
		Platform:   domain.GatewayPlatformTelegram,
		ChatID:     plan.ChatID,
		FromUserID: "593821092",
		Data:       EncodeApprovalCallback("int-9", true),
	})

	waitFor(t, "resumed run submission", func() bool { return submitter.requestCount() >= 1 })
	if submitter.requestCount() >= 1 && svc.bridge.Pending(plan.SessionID) {
		t.Fatalf("pending approval not cleared after callback")
	}
	_ = adapter
}

// TestServiceTextReplyApprovalDecision drives the full interception path
// (add-whatsapp-gateway design D3, task 2.4): on a CanButton=false platform,
// a DM reading APPROVE resolves the pending card through the same bridge
// path a button press takes.
func TestServiceTextReplyApprovalDecision(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")

	adapter := newFakeChatAdapterWithCaps(false, false) // multi-device-style cell
	submitter := &fakeRunSubmitter{}
	bridge := NewApprovalBridge(submitter, adapter, f.st.GatewayLinks())
	router := NewRouter(
		f.st.Gateways(), f.st.GatewayBindings(), f.st.GatewayLinks(), f.st.Agents(),
		f.st.Members(), f.st.Users(), nil, fakeUsageReader{},
		WithApprovalIntercept(bridge),
	)
	router.pairing = f.pairing
	var svc *Service
	outbox := NewOutbox(f.st.GatewayOutbox(), func(gatewayID string) (MessageSender, bool) {
		return svc.Adapter(gatewayID)
	})
	svc = NewService(router, submitter, bridge, f.st.Gateways(), outbox)
	svc.AttachGateway(f.gateway.ID, f.ws.ID, f.gateway.Platform, adapter)

	sessionID := GatewayDMSessionKey("593821092", f.atlas.ID, 0)
	if err := bridge.Present(f.ctx, StreamSession{
		GatewayID: f.gateway.ID, WorkspaceID: f.ws.ID,
		SessionID: sessionID, ChatID: "593821092",
	}, agents.ExecRequest{}, agents.ApprovalPayload{InterruptID: "int-5", Command: "deploy.sh"}); err != nil {
		t.Fatalf("present approval: %v", err)
	}

	// Non-matching text gets the pending notice and decides nothing.
	svc.HandleMessage(f.ctx, f.gateway.ID, f.dmMsg("593821092", "well... maybe"))
	if submitter.requestCount() != 0 {
		t.Fatalf("non-decision text must not resume the turn")
	}
	sends := adapter.sends()
	if len(sends) == 0 || !strings.Contains(sends[len(sends)-1].HTML, PendingApprovalTextReply) {
		t.Fatalf("expected the text-reply pending notice, got %v", sends)
	}

	// The decision text resolves the card and resumes the run.
	svc.HandleMessage(f.ctx, f.gateway.ID, f.dmMsg("593821092", "approve"))
	waitFor(t, "resumed run after text decision", func() bool { return submitter.requestCount() >= 1 })
	if bridge.Pending(sessionID) {
		t.Fatalf("text decision must clear the pending approval")
	}
	submitter.mu.Lock()
	approved := submitter.calls >= 1
	submitter.mu.Unlock()
	if !approved {
		t.Fatalf("expected the resumed submission")
	}
}
