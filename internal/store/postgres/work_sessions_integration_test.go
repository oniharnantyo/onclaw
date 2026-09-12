//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// wsiSeedChannel mirrors the fake's seeding: workspace + provider + user
// member + agent, the agent joined as the channel's facilitator.
func wsiSeedChannel(t *testing.T, ctx context.Context, s store.Store, slug string) (workspaceID, channelID, userID, agentID string) {
	t.Helper()
	ws := chSeedChannelWorkspace(t, ctx, s, slug)
	user := chSeedUser(t, ctx, s, slug+"-sarah@example.com", "Sarah Chen")
	chSeedWorkspaceMember(t, ctx, s, ws, user.ID)
	agent := chSeedChannelAgent(t, ctx, s, ws, "atlas-"+slug)
	channel := chSeedChannel(t, ctx, s, ws, "Ops "+slug, "ops")
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

func wsiSeedMessage(t *testing.T, ctx context.Context, s store.Store, workspaceID, channelID, userID, body string) *domain.ChannelMessage {
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

func TestIntegration_WorkSessionStore_Lifecycle(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, chID, userID, _ := wsiSeedChannel(t, ctx, s, "ws-life")
	root := wsiSeedMessage(t, ctx, s, wsID, chID, userID, "add dark mode")

	ws := s.WorkSessions()

	active, err := ws.ActiveWorkSession(ctx, wsID, chID)
	if err != nil || active != nil {
		t.Fatalf("ActiveWorkSession = %+v, %v; want nil, nil", active, err)
	}

	session := &domain.WorkSession{WorkspaceID: wsID, ChannelID: chID, RootMessageID: root.ID, Goal: "add dark mode"}
	if err := ws.CreateWorkSession(ctx, wsID, session); err != nil {
		t.Fatalf("create: %v", err)
	}
	if session.ID == "" || session.CreatedAt.IsZero() || session.UpdatedAt.IsZero() {
		t.Fatalf("create did not fill id/timestamps: %+v", session)
	}
	if session.Status != domain.WorkSessionOpen || session.Budget != domain.DefaultWorkSessionBudget {
		t.Fatalf("defaults = status %q budget %d", session.Status, session.Budget)
	}

	// A second active session conflicts (the store-level guard).
	if err := ws.CreateWorkSession(ctx, wsID, &domain.WorkSession{WorkspaceID: wsID, ChannelID: chID, RootMessageID: root.ID, Goal: "another"}); !errors.Is(err, domain.ErrWorkSessionActive) {
		t.Fatalf("second create = %v, want ErrWorkSessionActive", err)
	}

	active, err = ws.ActiveWorkSession(ctx, wsID, chID)
	if err != nil || active == nil || active.ID != session.ID {
		t.Fatalf("ActiveWorkSession = %+v, %v", active, err)
	}

	updated, err := ws.ConsumeWorkSessionHop(ctx, wsID, session.ID)
	if err != nil || updated.HopsUsed != 1 {
		t.Fatalf("consume hop = %+v, %v; want hops_used 1", updated, err)
	}

	paused, err := ws.PauseWorkSession(ctx, wsID, session.ID, domain.WorkSessionPauseAwaitingHuman)
	if err != nil || paused.Status != domain.WorkSessionPaused || paused.PauseReason != domain.WorkSessionPauseAwaitingHuman {
		t.Fatalf("pause = %+v, %v", paused, err)
	}
	if _, err := ws.PauseWorkSession(ctx, wsID, session.ID, domain.WorkSessionPauseBudgetExhausted); !errors.Is(err, domain.ErrWorkSessionNotOpen) {
		t.Fatalf("double pause = %v, want ErrWorkSessionNotOpen", err)
	}
	if _, err := ws.ConsumeWorkSessionHop(ctx, wsID, session.ID); !errors.Is(err, domain.ErrWorkSessionHopUnavailable) {
		t.Fatalf("consume while paused = %v, want ErrWorkSessionHopUnavailable", err)
	}
	resumed, err := ws.ResumeWorkSession(ctx, wsID, session.ID)
	if err != nil || resumed.Status != domain.WorkSessionOpen || resumed.PauseReason != "" {
		t.Fatalf("resume = %+v, %v", resumed, err)
	}
	// Resume on an already-open session is the documented accepted no-op.
	if again, err := ws.ResumeWorkSession(ctx, wsID, session.ID); err != nil || again.ID != session.ID {
		t.Fatalf("idempotent resume = %+v, %v; want the open row", again, err)
	}

	closed, err := ws.CloseWorkSession(ctx, wsID, session.ID, "shipped")
	if err != nil || closed.Status != domain.WorkSessionClosed || closed.Summary != "shipped" || closed.ClosedAt == nil {
		t.Fatalf("close = %+v, %v", closed, err)
	}
	if closed.PauseReason != "" {
		t.Fatalf("closed session carries pause_reason %q, want cleared", closed.PauseReason)
	}
	if _, err := ws.CloseWorkSession(ctx, wsID, session.ID, "again"); !errors.Is(err, domain.ErrWorkSessionClosed) {
		t.Fatalf("double close = %v, want ErrWorkSessionClosed", err)
	}
	active, err = ws.ActiveWorkSession(ctx, wsID, chID)
	if err != nil || active != nil {
		t.Fatalf("post-close ActiveWorkSession = %+v, %v; want nil, nil", active, err)
	}

	// Newest-first listing + tenant isolation.
	list, err := ws.ListWorkSessions(ctx, wsID, chID)
	if err != nil || len(list) != 1 || list[0].ID != session.ID {
		t.Fatalf("ListWorkSessions = %+v, %v", list, err)
	}
	ws2ID, _, _, _ := wsiSeedChannel(t, ctx, s, "ws-other")
	if _, err := ws.GetWorkSession(ctx, ws2ID, session.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-workspace get = %v, want ErrNotFound", err)
	}
}

func TestIntegration_WorkSessionStore_HopBudgetStopsConsumption(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, chID, userID, _ := wsiSeedChannel(t, ctx, s, "ws-budget")
	root := wsiSeedMessage(t, ctx, s, wsID, chID, userID, "goal")

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
		t.Fatalf("hops_used = %d, want exactly 3 (the guarded UPDATE never overshoots)", reloaded.HopsUsed)
	}
}

func TestIntegration_WorkSessionStore_ConcurrentHopAndPause(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, chID, userID, _ := wsiSeedChannel(t, ctx, s, "ws-race")
	root := wsiSeedMessage(t, ctx, s, wsID, chID, userID, "goal")

	ws := s.WorkSessions()
	session := &domain.WorkSession{WorkspaceID: wsID, ChannelID: chID, RootMessageID: root.ID, Goal: "goal", Budget: 2}
	if err := ws.CreateWorkSession(ctx, wsID, session); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Two concurrent hop consumers on a budget of 2: exactly two wins.
	var wg sync.WaitGroup
	hopResults := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, hopResults[i] = ws.ConsumeWorkSessionHop(ctx, wsID, session.ID)
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, err := range hopResults {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, domain.ErrWorkSessionHopUnavailable):
		default:
			t.Fatalf("unexpected hop error: %v", err)
		}
	}
	if wins != 2 {
		t.Fatalf("hop wins = %d, want 2 (budget 2)", wins)
	}
	if _, err := ws.ConsumeWorkSessionHop(ctx, wsID, session.ID); !errors.Is(err, domain.ErrWorkSessionHopUnavailable) {
		t.Fatalf("third hop = %v, want ErrWorkSessionHopUnavailable", err)
	}

	// Two concurrent pauses on an open session: exactly one wins.
	channel2 := chSeedChannel(t, ctx, s, mustWorkspace(t, ctx, s, wsID), "Ops 2", "ops-2")
	root2 := wsiSeedMessage(t, ctx, s, wsID, channel2.ID, userID, "goal 2")
	session2 := &domain.WorkSession{WorkspaceID: wsID, ChannelID: channel2.ID, RootMessageID: root2.ID, Goal: "goal 2"}
	if err := ws.CreateWorkSession(ctx, wsID, session2); err != nil {
		t.Fatalf("create 2: %v", err)
	}

	pauseErrs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, pauseErrs[i] = ws.PauseWorkSession(ctx, wsID, session2.ID, domain.WorkSessionPauseBudgetExhausted)
		}(i)
	}
	wg.Wait()
	pauseWins := 0
	for _, err := range pauseErrs {
		switch {
		case err == nil:
			pauseWins++
		case errors.Is(err, domain.ErrWorkSessionNotOpen):
		default:
			t.Fatalf("unexpected pause error: %v", err)
		}
	}
	if pauseWins != 1 {
		t.Fatalf("pause wins = %d, want exactly one", pauseWins)
	}
}

// mustWorkspace re-resolves the workspace by id (the seed helpers return the
// struct, but the race test re-reads it to keep the channel seeding local).
func mustWorkspace(t *testing.T, ctx context.Context, s store.Store, id string) *domain.Workspace {
	t.Helper()
	ws, err := s.Workspaces().ByID(ctx, id)
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	return ws
}

func TestIntegration_ChannelStore_FacilitatorUniqueness(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, chID, _, agentID := wsiSeedChannel(t, ctx, s, "ws-fac")

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
		}
	}
	if agentMemberID == "" {
		t.Fatal("agent member missing from roster")
	}

	// A second agent joining as facilitator conflicts (store pre-check and
	// the DB partial unique index agree); joining as member succeeds, then
	// promotion conflicts.
	agent2 := chSeedChannelAgent(t, ctx, s, mustWorkspace(t, ctx, s, wsID), "beacon-fac")
	second := &domain.ChannelMember{WorkspaceID: wsID, ChannelID: chID, MemberType: domain.ChannelMemberTypeAgent, AgentID: agent2.ID, Role: domain.ChannelMemberRoleFacilitator}
	if err := channels.AddChannelMember(ctx, second); !errors.Is(err, domain.ErrChannelFacilitatorExists) {
		t.Fatalf("second facilitator add = %v, want ErrChannelFacilitatorExists", err)
	}
	second.Role = domain.ChannelMemberRoleMember
	if err := channels.AddChannelMember(ctx, second); err != nil {
		t.Fatalf("member add: %v", err)
	}
	if err := channels.UpdateChannelMemberRole(ctx, wsID, chID, second.ID, domain.ChannelMemberRoleFacilitator); !errors.Is(err, domain.ErrChannelFacilitatorExists) {
		t.Fatalf("second facilitator update = %v, want ErrChannelFacilitatorExists", err)
	}
	if err := channels.UpdateChannelMemberRole(ctx, wsID, chID, agentMemberID, domain.ChannelMemberRoleFacilitator); err != nil {
		t.Fatalf("re-set incumbent = %v, want nil", err)
	}
	if err := channels.UpdateChannelMemberRole(ctx, wsID, chID, agentMemberID, domain.ChannelMemberRoleMember); err != nil {
		t.Fatalf("demote = %v, want nil", err)
	}
	if err := channels.UpdateChannelMemberRole(ctx, wsID, chID, second.ID, domain.ChannelMemberRoleFacilitator); err != nil {
		t.Fatalf("promote after demote = %v, want nil", err)
	}
	if err := channels.UpdateChannelMemberRole(ctx, wsID, chID, agentID, domain.ChannelMemberRoleFacilitator); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("agent ref id as member id = %v, want ErrNotFound", err)
	}
}

func TestIntegration_ChannelStore_KickoffMessageRoundTrip(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, chID, userID, _ := wsiSeedChannel(t, ctx, s, "ws-kick")

	channels := s.Channels()
	ws := s.WorkSessions()

	// Message first (is_kickoff, unlinked), session second, then the stamp —
	// the FKs point both ways.
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
	if err := ws.CreateWorkSession(ctx, wsID, session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := channels.SetChannelMessageWorkSession(ctx, wsID, chID, msg.ID, &session.ID); err != nil {
		t.Fatalf("stamp kickoff: %v", err)
	}

	feed, err := channels.ListChannelMessages(ctx, store.ListChannelMessagesParams{WorkspaceID: wsID, ChannelID: chID})
	if err != nil || len(feed) != 1 {
		t.Fatalf("feed = %+v, %v", feed, err)
	}
	if feed[0].WorkSessionID == nil || *feed[0].WorkSessionID != session.ID || !feed[0].IsKickoff {
		t.Fatalf("kickoff row = work_session_id %v is_kickoff %v", feed[0].WorkSessionID, feed[0].IsKickoff)
	}

	plain := wsiSeedMessage(t, ctx, s, wsID, chID, userID, "casual hello")
	if plain.WorkSessionID != nil || plain.IsKickoff {
		t.Fatalf("plain row = work_session_id %v is_kickoff %v", plain.WorkSessionID, plain.IsKickoff)
	}

	if err := channels.SetChannelMessageWorkSession(ctx, wsID, chID, msg.ID, nil); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	reloaded, err := channels.ListChannelMessages(ctx, store.ListChannelMessagesParams{WorkspaceID: wsID, ChannelID: chID})
	if err != nil || len(reloaded) != 2 {
		t.Fatalf("reload = %+v, %v", reloaded, err)
	}
	if reloaded[0].WorkSessionID != nil {
		t.Fatalf("unlinked row still carries work_session_id %v", reloaded[0].WorkSessionID)
	}
}

func TestIntegration_WorkSessionStore_ListOpen_BootScan(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, chID, userID, _ := wsiSeedChannel(t, ctx, s, "ws-boot")
	root := wsiSeedMessage(t, ctx, s, wsID, chID, userID, "goal")

	ws := s.WorkSessions()
	open := &domain.WorkSession{WorkspaceID: wsID, ChannelID: chID, RootMessageID: root.ID, Goal: "goal"}
	if err := ws.CreateWorkSession(ctx, wsID, open); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := ws.PauseWorkSession(ctx, wsID, open.ID, domain.WorkSessionPauseAwaitingHuman); err != nil {
		t.Fatalf("pause: %v", err)
	}

	channel2 := chSeedChannel(t, ctx, s, mustWorkspace(t, ctx, s, wsID), "Ops 2", "ops-2")
	root2 := wsiSeedMessage(t, ctx, s, wsID, channel2.ID, userID, "goal 2")
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
