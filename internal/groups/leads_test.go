package groups

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Lead rule tests (add-agent-groups design D5, spec "Single lead"): exactly
// one lead, the first member leads, SetLead reassigns, removal and agent
// deletion fall the lead back, a member-less group has no lead, and lead
// changes never move owned tasks.

func TestLeads_FirstMemberBecomesLead(t *testing.T) {
	h := newHarness(t)
	atlas := h.addAgent("Atlas", "coordinates")
	scout := h.addAgent("Scout", "searches")

	g, err := h.svc.CreateGroup(h.ctx, "crew", "", "", []string{atlas.ID, scout.ID})
	require.NoError(t, err)
	row, err := h.st.Groups().GroupByID(h.ctx, g.ID)
	require.NoError(t, err)
	require.NotNil(t, row.LeadAgentID)
	require.Equal(t, atlas.ID, *row.LeadAgentID)
}

func TestLeads_FreshGroupHasNoLead(t *testing.T) {
	h := newHarness(t)
	g, err := h.svc.CreateGroup(h.ctx, "empty", "", "", nil)
	require.NoError(t, err)
	row, err := h.st.Groups().GroupByID(h.ctx, g.ID)
	require.NoError(t, err)
	require.Nil(t, row.LeadAgentID)
}

func TestLeads_SetLeadReassigns(t *testing.T) {
	h, g, atlas, scout, jono := rosteredHarness(t)

	// Seed an owned task: a mention to Scout creates and dispatches it; the
	// task completes through the result tool so no open task captures the
	// next untargeted message (the sole-open-task tier).
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Scout dig"}))
	require.Equal(t, 1, h.rt.submitCount())
	resultThroughTool(t, h, g.ID, scout.ID, "dug")
	h.rt.finishTurn(0, nil)

	// Reassign the lead to Jono.
	require.NoError(t, h.svc.SetLead(h.ctx, g.ID, jono.ID))
	row, err := h.st.Groups().GroupByID(h.ctx, g.ID)
	require.NoError(t, err)
	require.NotNil(t, row.LeadAgentID)
	require.Equal(t, jono.ID, *row.LeadAgentID, "that member is the only lead")

	// The next untargeted message routes to the NEW lead.
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "status?"}))
	require.Equal(t, 2, h.rt.submitCount())
	require.Equal(t, jono.ID, h.submitAgentID(1))

	// Tasks that already have owners keep them.
	tasks := h.groupTasks(g.ID)
	require.Len(t, tasks, 2)
	var scoutTask *store.AgentGroupTask
	for i := range tasks {
		if tasks[i].OwnerAgentID != nil && *tasks[i].OwnerAgentID == scout.ID {
			scoutTask = &tasks[i]
		}
	}
	require.NotNil(t, scoutTask)
	require.Equal(t, store.GroupTaskStateCompleted, h.taskByID(scoutTask.TaskID).State)
	require.Equal(t, scout.ID, deref(h.taskByID(scoutTask.TaskID).OwnerAgentID), "lead change does not move owned tasks")
	_ = atlas
}

func TestLeads_SetLeadRequiresRosterMember(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	err := h.svc.SetLead(h.ctx, g.ID, "not-a-member")
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestLeads_RemovingLeadFallsBack(t *testing.T) {
	h, g, atlas, scout, jono := rosteredHarness(t)
	require.NoError(t, h.svc.RemoveMember(h.ctx, g.ID, atlas.ID))

	row, err := h.st.Groups().GroupByID(h.ctx, g.ID)
	require.NoError(t, err)
	require.NotNil(t, row.LeadAgentID)
	require.Equal(t, scout.ID, *row.LeadAgentID, "the first remaining member becomes the lead")

	// The next untargeted message routes to the new lead.
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))
	require.Equal(t, 1, h.rt.submitCount())
	require.Equal(t, scout.ID, h.submitAgentID(0))
	_ = jono
}

func TestLeads_MemberLessGroupHasNoLead(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.RemoveMember(h.ctx, g.ID, atlas.ID))
	require.NoError(t, h.svc.RemoveMember(h.ctx, g.ID, h.memberID(g.ID, "Scout")))
	require.NoError(t, h.svc.RemoveMember(h.ctx, g.ID, h.memberID(g.ID, "Jono")))

	row, err := h.st.Groups().GroupByID(h.ctx, g.ID)
	require.NoError(t, err)
	require.Nil(t, row.LeadAgentID, "a group with no members has no lead")
}

func TestLeads_RemovingMemberKeepsPastContributions(t *testing.T) {
	h, g, _, scout, _ := rosteredHarness(t)
	// Scout contributes, then leaves.
	agentID := scout.ID
	require.NoError(t, h.st.Groups().AppendMessage(h.ctx, &store.AgentGroupMessage{
		GroupID:       g.ID,
		MessageID:     "scout-note",
		Kind:          store.GroupMessageKindMessage,
		AuthorKind:    store.GroupAuthorKindAgent,
		AuthorAgentID: &agentID,
		Payload:       mustPayload(t, "my findings"),
	}))
	require.NoError(t, h.svc.RemoveMember(h.ctx, g.ID, scout.ID))

	// The transcript keeps the past contribution; the other members are
	// unchanged.
	rows := h.transcript(g.ID)
	var found bool
	for _, r := range rows {
		if r.MessageID == "scout-note" {
			found = true
			require.Equal(t, scout.ID, deref(r.AuthorAgentID))
		}
	}
	require.True(t, found, "the removed member's past messages remain in the transcript")
	members, err := h.svc.Members(h.ctx, g.ID)
	require.NoError(t, err)
	require.Len(t, members, 2, "the other members are unchanged")
	// The member's session survives: removing a member does not delete it.
	require.NotEmpty(t, h.rt.sessionFor(scout.ID))
}

// memberID finds a roster member's agent id by name.
func (h *harness) memberID(groupID, name string) string {
	h.t.Helper()
	members, err := h.svc.Members(h.ctx, groupID)
	require.NoError(h.t, err)
	for _, m := range members {
		agent, err := h.st.Agents().ByID(h.ctx, m.AgentID)
		require.NoError(h.t, err)
		if agent.Name == name {
			return agent.ID
		}
	}
	h.t.Fatalf("no member named %s", name)
	return ""
}
