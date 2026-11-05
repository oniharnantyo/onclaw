package groups

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Agent-deletion cleanup tests (add-agent-groups spec "Agent deletion
// cleanup"): deleting an agent removes its memberships from every group,
// falls the lead back where it was the lead, and pauses its open tasks with
// a visible reason; the groups, transcripts, and other members survive.

func TestCleanupAgent_RemovesMembershipsAndPausesTasks(t *testing.T) {
	h, g, atlas, scout, _ := rosteredHarness(t)
	// Scout owns an open task.
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Scout dig"}))
	require.Equal(t, 1, h.rt.submitCount())
	task := h.groupTasks(g.ID)[0]
	require.Equal(t, scout.ID, deref(task.OwnerAgentID))
	before := len(h.transcript(g.ID))

	require.NoError(t, h.svc.CleanupAgent(h.ctx, scout.ID))

	// The membership is gone; the group, transcript, and lead survive.
	members, err := h.svc.Members(h.ctx, g.ID)
	require.NoError(t, err)
	require.Len(t, members, 2)
	row, err := h.st.Groups().GroupByID(h.ctx, g.ID)
	require.NoError(t, err)
	require.NotNil(t, row.LeadAgentID)
	require.Equal(t, atlas.ID, *row.LeadAgentID, "a non-lead member's cleanup leaves the lead alone")

	// The open task paused with a visible reason; the turn was interrupted.
	updated := h.taskByID(task.TaskID)
	require.Equal(t, store.GroupTaskStatePaused, updated.State)
	require.Equal(t, cleanupStopReason, deref(updated.StopReason))
	require.Equal(t, 1, h.rt.cancelCount())

	// The transcript is intact, with a change line for the removal.
	require.GreaterOrEqual(t, len(h.transcript(g.ID)), before)
	var found bool
	for _, r := range h.messagesOf(g.ID, store.GroupMessageKindChange) {
		if r.MessageID != "" && payloadText(r) == "removed Scout" {
			found = true
		}
	}
	require.True(t, found)

	// The other members are unchanged and still receive work.
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))
	require.Equal(t, atlas.ID, h.submitAgentID(1), "untargeted still routes to the lead")
}

func TestCleanupAgent_LeadFallsBack(t *testing.T) {
	h, g, atlas, scout, _ := rosteredHarness(t)
	require.NoError(t, h.svc.CleanupAgent(h.ctx, atlas.ID))

	row, err := h.st.Groups().GroupByID(h.ctx, g.ID)
	require.NoError(t, err)
	require.NotNil(t, row.LeadAgentID)
	require.Equal(t, scout.ID, *row.LeadAgentID, "the lead falls to the first remaining member")

	// Untargeted messages route to the new lead.
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))
	require.Equal(t, 1, h.rt.submitCount())
	require.Equal(t, scout.ID, h.submitAgentID(0))
}

func TestCleanupAgent_SpansEveryGroup(t *testing.T) {
	h := newHarness(t)
	a1 := h.addAgent("One", "")
	a2 := h.addAgent("Two", "")
	shared := h.addAgent("Shared", "in both")
	g1, err := h.svc.CreateGroup(h.ctx, "one", "", "", []string{a1.ID, shared.ID})
	require.NoError(t, err)
	g2, err := h.svc.CreateGroup(h.ctx, "two", "", "", []string{a2.ID, shared.ID})
	require.NoError(t, err)

	require.NoError(t, h.svc.CleanupAgent(h.ctx, shared.ID))

	for _, groupID := range []string{g1.ID, g2.ID} {
		members, err := h.svc.Members(h.ctx, groupID)
		require.NoError(t, err)
		require.Len(t, members, 1)
		// The lead fell back where the deleted agent was lead.
		row, err := h.st.Groups().GroupByID(h.ctx, groupID)
		require.NoError(t, err)
		require.NotNil(t, row.LeadAgentID)
	}
}

func TestCleanupAgent_UnknownAgentIsNoOp(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.CleanupAgent(h.ctx, "nobody"))
	members, err := h.svc.Members(h.ctx, g.ID)
	require.NoError(t, err)
	require.Len(t, members, 3)
}
