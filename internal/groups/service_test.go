package groups

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// Service construction and CRUD tests (add-agent-groups tasks 3.1, spec
// "Group entity and membership"): the constructor binds the group turn
// source, member threads are real sessions created through the same path
// ordinary sessions use and seeded into the acceptance cursors, and the
// group lifecycle keeps the store invariants.

func TestService_NewBindsGroupTurnSource(t *testing.T) {
	h := newHarness(t)
	ref := agents.NewGroupTurnRef()
	svc := New(Deps{Store: h.st, Sessions: sessions.NewService(h.st), Manager: h.rt, Ref: ref, Events: h.ev})
	require.True(t, ref.Source() == agents.GroupTurnSource(svc), "the constructor binds the service as the runtime's group turn source")
}

func TestService_CreateGroup_MemberThreadsAreRealSessions(t *testing.T) {
	h := newHarness(t)
	atlas := h.addAgent("Atlas", "coordinates")
	scout := h.addAgent("Scout", "searches")

	g, err := h.svc.CreateGroup(h.ctx, "crew", "The Crew", "work together", []string{atlas.ID, scout.ID})
	require.NoError(t, err)

	// Each member's session was created through the manager — the same
	// path ordinary sessions take (design D2).
	require.Equal(t, []string{atlas.ID, scout.ID}, h.rt.creates)
	for _, agentID := range []string{atlas.ID, scout.ID} {
		sessID := h.rt.sessionFor(agentID)
		require.NotEmpty(t, sessID)
		row, err := h.st.Sessions().ByID(h.ctx, sessID)
		require.NoError(t, err, "the member session is a real session row")
		require.Equal(t, agentID, row.AgentID, "bound to the member agent")

		// The acceptance cursor is seeded onto the session.
		cur, err := h.st.Groups().GetCursor(h.ctx, g.ID, agentID)
		require.NoError(t, err)
		require.Equal(t, sessID, cur.SessionID)
		require.Equal(t, int64(0), cur.ThroughSeq)
	}
}

func TestService_CreateGroup_DuplicateMemberIDsCollapse(t *testing.T) {
	h := newHarness(t)
	atlas := h.addAgent("Atlas", "coordinates")
	g, err := h.svc.CreateGroup(h.ctx, "crew", "", "", []string{atlas.ID, atlas.ID})
	require.NoError(t, err)
	members, err := h.svc.Members(h.ctx, g.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
}

func TestService_CreateGroup_UnknownAgentFails(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.CreateGroup(h.ctx, "crew", "", "", []string{"ghost"})
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestService_CreateGroup_NameRequired(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.CreateGroup(h.ctx, "", "", "", nil)
	require.Error(t, err)
}

func TestService_AddMember_DuplicateFails(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	err := h.svc.AddMember(h.ctx, g.ID, atlas.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrDuplicate)
}

func TestService_AddMember_WritesChangeLineAndSeedsCursor(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	newcomer := h.addAgent("Newt", "joins late")
	before := len(h.transcript(g.ID))
	require.NoError(t, h.svc.AddMember(h.ctx, g.ID, newcomer.ID))

	rows := h.transcript(g.ID)
	require.Len(t, rows, before+1)
	require.Equal(t, store.GroupMessageKindChange, rows[len(rows)-1].Kind)
	require.Equal(t, "added Newt", payloadText(rows[len(rows)-1]))
	require.Equal(t, store.GroupAuthorKindCoordinator, rows[len(rows)-1].AuthorKind)

	cur, err := h.st.Groups().GetCursor(h.ctx, g.ID, newcomer.ID)
	require.NoError(t, err)
	require.Equal(t, h.rt.sessionFor(newcomer.ID), cur.SessionID)
}

func TestService_UpdateGroup_EditsFields(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	updated, err := h.svc.UpdateGroup(h.ctx, g.ID, "renamed", "new title", "new instructions")
	require.NoError(t, err)
	require.Equal(t, "renamed", updated.Name)
	require.Equal(t, "new title", updated.Title)
	require.Equal(t, "new instructions", updated.Instructions)

	// Subsequent packets carry the edited instructions (spec: edit group
	// fields).
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))
	require.Equal(t, 1, h.rt.submitCount())
	require.Equal(t, "new instructions", h.rt.submitAt(0).Packet.Instructions)
}

func TestService_DeleteGroup_CascadesButMemberSessionsSurvive(t *testing.T) {
	h, g, atlas, scout, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas hi"}))
	require.GreaterOrEqual(t, len(h.transcript(g.ID)), 1)

	atlasSession := h.rt.sessionFor(atlas.ID)
	require.NoError(t, h.svc.DeleteGroup(h.ctx, g.ID))

	_, err := h.st.Groups().GroupByID(h.ctx, g.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = h.st.Groups().GetCursor(h.ctx, g.ID, scout.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	// The member sessions are ordinary sessions and survive the delete.
	row, err := h.st.Sessions().ByID(h.ctx, atlasSession)
	require.NoError(t, err)
	require.Equal(t, atlas.ID, row.AgentID)
}

func TestService_TranscriptPage(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	base := len(h.transcript(g.ID)) // the roster change lines seeded first
	for i := 0; i < 5; i++ {
		require.NoError(t, h.st.Groups().AppendMessage(h.ctx, &store.AgentGroupMessage{
			GroupID:    g.ID,
			Kind:       store.GroupMessageKindMessage,
			AuthorKind: store.GroupAuthorKindUser,
			Payload:    mustPayload(t, "row"),
		}))
	}
	page, err := h.svc.Transcript(h.ctx, g.ID, 0, 3)
	require.NoError(t, err)
	require.Len(t, page, 3)
	require.Equal(t, int64(base+5-2), page[0].Seq, "the newest window, replayed in transcript order")
	require.Equal(t, int64(base+5), page[2].Seq)

	older, err := h.svc.Transcript(h.ctx, g.ID, page[0].Seq, 10)
	require.NoError(t, err)
	require.Len(t, older, base+5-3, "the older page holds everything below the window")
}

func TestService_ListGroups(t *testing.T) {
	h := newHarness(t)
	a := h.addAgent("A", "")
	first, err := h.svc.CreateGroup(h.ctx, "one", "", "", []string{a.ID})
	require.NoError(t, err)
	second, err := h.svc.CreateGroup(h.ctx, "two", "", "", nil)
	require.NoError(t, err)
	groups, err := h.svc.ListGroups(h.ctx)
	require.NoError(t, err)
	require.Len(t, groups, 2)
	require.Equal(t, first.ID, groups[0].ID)
	require.Equal(t, second.ID, groups[1].ID)
}

func TestService_EventsFire(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))
	require.GreaterOrEqual(t, h.ev.eventCount(), 1, "message-appended events ride every append")
	appends := h.ev.eventsOf(GroupEventMessageAppended)
	require.NotEmpty(t, appends)
	require.Equal(t, g.ID, appends[0].GroupID)
	require.NotNil(t, appends[0].Message)

	updates := h.ev.eventsOf(GroupEventTaskUpdated)
	require.NotEmpty(t, updates, "task creation and dispatch push task events")
	require.GreaterOrEqual(t, h.ev.changedCount(), 1, "roster changes push groups:changed")
}

func TestService_SendErrors(t *testing.T) {
	h := newHarness(t)
	err := h.svc.Send(h.ctx, SendRequest{GroupID: "missing", Text: "hi"})
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestService_MemberSessionInPacket demonstrates the member turn's session
// binding end to end: the dispatched turn runs on the member's real session.
func TestService_MemberSessionInPacket(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))
	req := h.rt.submitAt(0)
	require.Equal(t, h.rt.sessionFor(atlas.ID), req.SessionID)
	require.Equal(t, g.ID, req.Packet.GroupID)
}
