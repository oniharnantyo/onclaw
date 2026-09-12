package fake_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// wsSeedChannel builds a workspace + channel + two members (a human owner and
// an agent) and returns the ids the work-session tests need.
func wsSeedChannel(t *testing.T, ctx context.Context, s store.Store, slug string) (workspaceID, channelID, userID, agentID string) {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := s.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	user := &domain.User{Email: slug + "-sarah@example.com", Name: "Sarah Chen"}
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "owner-" + slug}
	if err := s.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := s.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	agent := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas-" + slug, Name: "Atlas", ProviderID: prov.ID, Model: "gpt-4o", Autonomy: domain.AutonomyFull}
	if err := s.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	channel := &domain.Channel{WorkspaceID: ws.ID, Name: "Ops " + slug, Slug: "ops"}
	if err := s.Channels().CreateChannel(ctx, channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	for _, m := range []*domain.ChannelMember{
		{WorkspaceID: ws.ID, ChannelID: channel.ID, MemberType: domain.ChannelMemberTypeUser, UserID: user.ID},
		{WorkspaceID: ws.ID, ChannelID: channel.ID, MemberType: domain.ChannelMemberTypeAgent, AgentID: agent.ID, Role: domain.ChannelMemberRoleFacilitator},
	} {
		if err := s.Channels().AddChannelMember(ctx, m); err != nil {
			t.Fatalf("add channel member: %v", err)
		}
	}
	return ws.ID, channel.ID, user.ID, agent.ID
}

// wsSeedMessage posts one human message to the channel (a kickoff root).
func wsSeedMessage(t *testing.T, ctx context.Context, s store.Store, workspaceID, channelID, userID, body string) *domain.ChannelMessage {
	t.Helper()
	msg := &domain.ChannelMessage{
		WorkspaceID:  workspaceID,
		ChannelID:    channelID,
		AuthorType:   domain.ChannelMemberTypeUser,
		AuthorUserID: userID,
		Body:         body,
		Mentions:     []domain.Mention{},
	}
	if err := s.Channels().InsertChannelMessage(ctx, msg); err != nil {
		t.Fatalf("insert message: %v", err)
	}
	return msg
}

func TestWorkSessionStore_Lifecycle(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID, chID, userID, _ := wsSeedChannel(t, ctx, s, "lifecycle")
	root := wsSeedMessage(t, ctx, s, wsID, chID, userID, "add dark mode")

	ws := s.WorkSessions()

	// Session-less channel reads as (nil, nil).
	active, err := ws.ActiveWorkSession(ctx, wsID, chID)
	if err != nil || active != nil {
		t.Fatalf("ActiveWorkSession = %+v, %v; want nil, nil", active, err)
	}

	// Create: fills ID/timestamps, defaults status/budget.
	session := &domain.WorkSession{
		WorkspaceID:   wsID,
		ChannelID:     chID,
		RootMessageID: root.ID,
		Goal:          "add dark mode",
	}
	if err := ws.CreateWorkSession(ctx, wsID, session); err != nil {
		t.Fatalf("create: %v", err)
	}
	if session.ID == "" || session.CreatedAt.IsZero() || session.UpdatedAt.IsZero() {
		t.Fatalf("create did not fill id/timestamps: %+v", session)
	}
	if session.Status != domain.WorkSessionOpen || session.Budget != domain.DefaultWorkSessionBudget || session.HopsUsed != 0 {
		t.Fatalf("defaults = status %q budget %d hops %d", session.Status, session.Budget, session.HopsUsed)
	}

	// A second active session conflicts.
	if err := ws.CreateWorkSession(ctx, wsID, &domain.WorkSession{WorkspaceID: wsID, ChannelID: chID, RootMessageID: root.ID, Goal: "another"}); !errors.Is(err, domain.ErrWorkSessionActive) {
		t.Fatalf("second create = %v, want ErrWorkSessionActive", err)
	}

	// Active read + hop consumption.
	active, err = ws.ActiveWorkSession(ctx, wsID, chID)
	if err != nil || active == nil || active.ID != session.ID {
		t.Fatalf("ActiveWorkSession = %+v, %v", active, err)
	}
	updated, err := ws.ConsumeWorkSessionHop(ctx, wsID, session.ID)
	if err != nil || updated.HopsUsed != 1 {
		t.Fatalf("consume hop = %+v, %v; want hops_used 1", updated, err)
	}

	// Pause (awaiting-human), resume clears the reason.
	paused, err := ws.PauseWorkSession(ctx, wsID, session.ID, domain.WorkSessionPauseAwaitingHuman)
	if err != nil || paused.Status != domain.WorkSessionPaused || paused.PauseReason != domain.WorkSessionPauseAwaitingHuman {
		t.Fatalf("pause = %+v, %v", paused, err)
	}
	if _, err := ws.PauseWorkSession(ctx, wsID, session.ID, domain.WorkSessionPauseBudgetExhausted); !errors.Is(err, domain.ErrWorkSessionNotOpen) {
		t.Fatalf("double pause = %v, want ErrWorkSessionNotOpen", err)
	}
	// Hops cannot be consumed while paused.
	if _, err := ws.ConsumeWorkSessionHop(ctx, wsID, session.ID); !errors.Is(err, domain.ErrWorkSessionHopUnavailable) {
		t.Fatalf("consume while paused = %v, want ErrWorkSessionHopUnavailable", err)
	}
	resumed, err := ws.ResumeWorkSession(ctx, wsID, session.ID)
	if err != nil || resumed.Status != domain.WorkSessionOpen || resumed.PauseReason != "" {
		t.Fatalf("resume = %+v, %v", resumed, err)
	}

	// Close stores summary + closed_at; closed is terminal.
	closed, err := ws.CloseWorkSession(ctx, wsID, session.ID, "shipped")
	if err != nil || closed.Status != domain.WorkSessionClosed || closed.Summary != "shipped" || closed.ClosedAt == nil {
		t.Fatalf("close = %+v, %v", closed, err)
	}
	if _, err := ws.CloseWorkSession(ctx, wsID, session.ID, "again"); !errors.Is(err, domain.ErrWorkSessionClosed) {
		t.Fatalf("double close = %v, want ErrWorkSessionClosed", err)
	}
	if _, err := ws.ResumeWorkSession(ctx, wsID, session.ID); !errors.Is(err, domain.ErrWorkSessionClosed) {
		t.Fatalf("resume closed = %v, want ErrWorkSessionClosed", err)
	}
	// The channel is session-less again.
	active, err = ws.ActiveWorkSession(ctx, wsID, chID)
	if err != nil || active != nil {
		t.Fatalf("post-close ActiveWorkSession = %+v, %v; want nil, nil", active, err)
	}

	// Newest-first listing shows the closed session.
	list, err := ws.ListWorkSessions(ctx, wsID, chID)
	if err != nil || len(list) != 1 || list[0].ID != session.ID {
		t.Fatalf("ListWorkSessions = %+v, %v", list, err)
	}

	// Tenant isolation: another workspace cannot see the session.
	ws2ID, _, _, _ := wsSeedChannel(t, ctx, s, "other")
	if _, err := ws.GetWorkSession(ctx, ws2ID, session.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-workspace get = %v, want ErrNotFound", err)
	}
}

func TestWorkSessionStore_HopBudgetStopsConsumption(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID, chID, userID, _ := wsSeedChannel(t, ctx, s, "budget")
	root := wsSeedMessage(t, ctx, s, wsID, chID, userID, "goal")

	ws := s.WorkSessions()
	session := &domain.WorkSession{WorkspaceID: wsID, ChannelID: chID, RootMessageID: root.ID, Goal: "goal", Budget: 3}
	if err := ws.CreateWorkSession(ctx, wsID, session); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 1; i <= 3; i++ {
		updated, err := ws.ConsumeWorkSessionHop(ctx, wsID, session.ID)
		if err != nil || updated.HopsUsed != i {
			t.Fatalf("hop %d = %+v, %v", i, updated, err)
		}
	}
	if _, err := ws.ConsumeWorkSessionHop(ctx, wsID, session.ID); !errors.Is(err, domain.ErrWorkSessionHopUnavailable) {
		t.Fatalf("over-budget consume = %v, want ErrWorkSessionHopUnavailable", err)
	}
	reloaded, _ := ws.GetWorkSession(ctx, wsID, session.ID)
	if reloaded.HopsUsed != 3 {
		t.Fatalf("hops_used = %d, want exactly 3 (the guard never overshoots)", reloaded.HopsUsed)
	}
}

func TestWorkSessionStore_ConcurrentPauseSingleWinner(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID, chID, userID, _ := wsSeedChannel(t, ctx, s, "race")
	root := wsSeedMessage(t, ctx, s, wsID, chID, userID, "goal")

	ws := s.WorkSessions()
	session := &domain.WorkSession{WorkspaceID: wsID, ChannelID: chID, RootMessageID: root.ID, Goal: "goal"}
	if err := ws.CreateWorkSession(ctx, wsID, session); err != nil {
		t.Fatalf("create: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	reasons := []domain.WorkSessionPauseReason{domain.WorkSessionPauseAwaitingHuman, domain.WorkSessionPauseBudgetExhausted}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = ws.PauseWorkSession(ctx, wsID, session.ID, reasons[i])
		}(i)
	}
	wg.Wait()

	wins, losses := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, domain.ErrWorkSessionNotOpen):
			losses++
		default:
			t.Fatalf("unexpected pause error: %v", err)
		}
	}
	if wins != 1 || losses != 1 {
		t.Fatalf("pause outcomes = %d wins / %d losses, want exactly one winner", wins, losses)
	}
	final, _ := ws.ActiveWorkSession(ctx, wsID, chID)
	if final == nil || final.Status != domain.WorkSessionPaused {
		t.Fatalf("final session = %+v, want paused", final)
	}
}

func TestChannelStore_FacilitatorUniqueness(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID, chID, _, agentID := wsSeedChannel(t, ctx, s, "facilitator")

	channels := s.Channels()
	roster, err := channels.ListChannelMembers(ctx, wsID, chID)
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	var agentMemberID string
	for _, m := range roster {
		if m.MemberType == domain.ChannelMemberTypeAgent {
			agentMemberID = m.ID
			if m.Role != domain.ChannelMemberRoleFacilitator {
				t.Fatalf("seeded agent role = %q, want facilitator", m.Role)
			}
		} else if m.Role != domain.ChannelMemberRoleMember {
			t.Fatalf("seeded user role = %q, want member", m.Role)
		}
	}

	// A second facilitator via AddChannelMember conflicts; the same member
	// joins fine once demoted.
	prov, err := s.Providers().ListForWorkspace(ctx, wsID)
	if err != nil || len(prov) == 0 {
		t.Fatalf("providers: %+v, %v", prov, err)
	}
	beacon := &domain.Agent{WorkspaceID: wsID, Slug: "beacon-facilitator", Name: "Beacon", ProviderID: prov[0].ID, Model: "gpt-4o", Autonomy: domain.AutonomyFull}
	if err := s.Agents().Create(ctx, beacon); err != nil {
		t.Fatalf("create beacon: %v", err)
	}
	beaconMember := &domain.ChannelMember{WorkspaceID: wsID, ChannelID: chID, MemberType: domain.ChannelMemberTypeAgent, AgentID: beacon.ID, Role: domain.ChannelMemberRoleFacilitator}
	if err := channels.AddChannelMember(ctx, beaconMember); !errors.Is(err, domain.ErrChannelFacilitatorExists) {
		t.Fatalf("second facilitator add = %v, want ErrChannelFacilitatorExists", err)
	}
	beaconMember.Role = domain.ChannelMemberRoleMember
	if err := channels.AddChannelMember(ctx, beaconMember); err != nil {
		t.Fatalf("member add: %v", err)
	}

	// Promoting a second member conflicts; re-setting the incumbent is a
	// no-op; demoting always succeeds.
	if err := channels.UpdateChannelMemberRole(ctx, wsID, chID, beaconMember.ID, domain.ChannelMemberRoleFacilitator); !errors.Is(err, domain.ErrChannelFacilitatorExists) {
		t.Fatalf("second facilitator = %v, want ErrChannelFacilitatorExists", err)
	}
	if err := channels.UpdateChannelMemberRole(ctx, wsID, chID, agentMemberID, domain.ChannelMemberRoleFacilitator); err != nil {
		t.Fatalf("re-set incumbent = %v, want nil", err)
	}
	if err := channels.UpdateChannelMemberRole(ctx, wsID, chID, agentMemberID, domain.ChannelMemberRoleMember); err != nil {
		t.Fatalf("demote = %v, want nil", err)
	}
	if err := channels.UpdateChannelMemberRole(ctx, wsID, chID, agentMemberID, "architect"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid role = %v, want ErrInvalid", err)
	}
	if err := channels.UpdateChannelMemberRole(ctx, wsID, chID, agentID, domain.ChannelMemberRoleFacilitator); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("agent ref id as member id = %v, want ErrNotFound", err)
	}
}

func TestChannelStore_KickoffMessageRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID, chID, userID, _ := wsSeedChannel(t, ctx, s, "kickoff")

	channels := s.Channels()
	wsStore := s.WorkSessions()

	// The kickoff flow: message first (is_kickoff, no link yet), session
	// second, then the stamp — the FKs point both ways.
	msg := &domain.ChannelMessage{
		WorkspaceID:  wsID,
		ChannelID:    chID,
		AuthorType:   domain.ChannelMemberTypeUser,
		AuthorUserID: userID,
		Body:         "add dark mode to settings",
		Mentions:     []domain.Mention{},
		IsKickoff:    true,
	}
	if err := channels.InsertChannelMessage(ctx, msg); err != nil {
		t.Fatalf("insert kickoff: %v", err)
	}
	session := &domain.WorkSession{WorkspaceID: wsID, ChannelID: chID, RootMessageID: msg.ID, Goal: msg.Body}
	if err := wsStore.CreateWorkSession(ctx, wsID, session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := channels.SetChannelMessageWorkSession(ctx, wsID, chID, msg.ID, &session.ID); err != nil {
		t.Fatalf("stamp kickoff: %v", err)
	}

	// The link round-trips through a feed read.
	feed, err := channels.ListChannelMessages(ctx, store.ListChannelMessagesParams{WorkspaceID: wsID, ChannelID: chID})
	if err != nil || len(feed) != 1 {
		t.Fatalf("feed = %+v, %v", feed, err)
	}
	if feed[0].WorkSessionID == nil || *feed[0].WorkSessionID != session.ID || !feed[0].IsKickoff {
		t.Fatalf("kickoff row = work_session_id %v is_kickoff %v", feed[0].WorkSessionID, feed[0].IsKickoff)
	}

	// A session-less message reads back with no link and no flag.
	plain := wsSeedMessage(t, ctx, s, wsID, chID, userID, "casual hello")
	if plain.WorkSessionID != nil || plain.IsKickoff {
		t.Fatalf("plain row = work_session_id %v is_kickoff %v", plain.WorkSessionID, plain.IsKickoff)
	}

	// Unknown message / session surface ErrNotFound.
	bogus := "not-a-message"
	if err := channels.SetChannelMessageWorkSession(ctx, wsID, chID, bogus, &session.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown message stamp = %v, want ErrNotFound", err)
	}
	unknownSession := "00000000-0000-0000-0000-000000000000"
	if err := channels.SetChannelMessageWorkSession(ctx, wsID, chID, msg.ID, &unknownSession); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown session stamp = %v, want ErrNotFound", err)
	}
}

func TestWorkSessionStore_ListOpen_BootScan(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID, chID, userID, _ := wsSeedChannel(t, ctx, s, "boot")
	root := wsSeedMessage(t, ctx, s, wsID, chID, userID, "goal")

	ws := s.WorkSessions()
	open := &domain.WorkSession{WorkspaceID: wsID, ChannelID: chID, RootMessageID: root.ID, Goal: "goal"}
	if err := ws.CreateWorkSession(ctx, wsID, open); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := ws.PauseWorkSession(ctx, wsID, open.ID, domain.WorkSessionPauseAwaitingHuman); err != nil {
		t.Fatalf("pause: %v", err)
	}

	// A closed session in a second channel stays out of the scan.
	channel2 := &domain.Channel{WorkspaceID: wsID, Name: "Ops 2", Slug: "ops-2"}
	if err := s.Channels().CreateChannel(ctx, channel2); err != nil {
		t.Fatalf("create channel 2: %v", err)
	}
	root2 := wsSeedMessage(t, ctx, s, wsID, channel2.ID, userID, "goal 2")
	done := &domain.WorkSession{WorkspaceID: wsID, ChannelID: channel2.ID, RootMessageID: root2.ID, Goal: "goal 2"}
	if err := ws.CreateWorkSession(ctx, wsID, done); err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if _, err := ws.CloseWorkSession(ctx, wsID, done.ID, "done"); err != nil {
		t.Fatalf("close: %v", err)
	}

	openList, err := ws.ListOpenWorkSessions(ctx)
	if err != nil {
		t.Fatalf("ListOpenWorkSessions: %v", err)
	}
	if len(openList) != 1 || openList[0].ID != open.ID {
		t.Fatalf("open sessions = %+v, want only the paused one", openList)
	}
}
