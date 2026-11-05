package groups

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Routing tests pin the full deterministic D4 tier order (add-agent-groups
// design D4, spec "Deterministic routing with a lead default"): pure
// routeMessage decisions table-driven, then Send-level tests asserting the
// transcript contents each route produces (assigned lines appear ONLY for
// task-targeted routes).

func routingRosterOf(lead string, members ...memberRef) routingRoster {
	return routingRoster{Members: members, LeadAgentID: lead}
}

func TestRouteMessage_TierOrder(t *testing.T) {
	scout := memberRef{AgentID: "scout", Name: "Scout"}
	jono := memberRef{AgentID: "jono", Name: "Jono"}
	joAnn := memberRef{AgentID: "joann", Name: "Jo Ann"}
	lead := memberRef{AgentID: "lead", Name: "Atlas"}
	roster3 := routingRosterOf("lead", lead, scout, jono)

	openTask := taskRef{TaskID: "task-1", OwnerAgentID: "scout"}

	tests := []struct {
		name string
		in   routingInput
		want routingDecision
	}{
		{
			name: "leading mention routes to the named member",
			in:   routingInput{Text: "@Scout summarize the corpus", Roster: roster3},
			want: routingDecision{Recipients: []string{"scout"}},
		},
		{
			name: "mention match is case-insensitive",
			in:   routingInput{Text: "@scout do it", Roster: roster3},
			want: routingDecision{Recipients: []string{"scout"}},
		},
		{
			name: "longest name wins",
			in:   routingInput{Text: "@Jo Ann please", Roster: routingRosterOf("lead", lead, joAnn, memberRef{AgentID: "jo", Name: "Jo"})},
			want: routingDecision{Recipients: []string{"joann"}},
		},
		{
			name: "mention with trailing punctuation still matches",
			in:   routingInput{Text: "@Scout, please", Roster: roster3},
			want: routingDecision{Recipients: []string{"scout"}},
		},
		{
			name: "multiple mentions route to each named member in mention order",
			in:   routingInput{Text: "@Jono @Scout pair up", Roster: roster3},
			want: routingDecision{Recipients: []string{"jono", "scout"}},
		},
		{
			name: "duplicate mentions collapse",
			in:   routingInput{Text: "@Scout @scout go", Roster: roster3},
			want: routingDecision{Recipients: []string{"scout"}},
		},
		{
			name: "mid-sentence mention does not route",
			in:   routingInput{Text: "please tell @Scout hello", Roster: roster3},
			want: routingDecision{Recipients: []string{"lead"}},
		},
		{
			name: "everyone mention fans out to every member in roster order",
			in:   routingInput{Text: "@everyone give your view", Roster: roster3},
			want: routingDecision{Recipients: []string{"lead", "scout", "jono"}},
		},
		{
			name: "explicit recipient behaves like a mention",
			in:   routingInput{Text: "please take this", Roster: roster3, ExplicitRecipient: "jono"},
			want: routingDecision{Recipients: []string{"jono"}},
		},
		{
			name: "explicit recipient outside the roster falls to the lead",
			in:   routingInput{Text: "please take this", Roster: roster3, ExplicitRecipient: "ghost"},
			want: routingDecision{Recipients: []string{"lead"}},
		},
		{
			name: "reply to a task message continues the task with its owner and an assigned line",
			in: routingInput{
				Text:   "also add charts",
				Roster: roster3,
				Reply:  &replyTarget{Task: &openTask},
			},
			want: routingDecision{Recipients: []string{"scout"}, FollowUpTaskID: "task-1", AssignedLine: true},
		},
		{
			name: "reply without a task to a member's message returns to that member, no line",
			in: routingInput{
				Text:   "thanks!",
				Roster: roster3,
				Reply:  &replyTarget{AuthorAgentID: "jono"},
			},
			want: routingDecision{Recipients: []string{"jono"}},
		},
		{
			name: "reply to a user message falls through the tiers",
			in: routingInput{
				Text:   "one more thing",
				Roster: roster3,
				Reply:  &replyTarget{},
			},
			want: routingDecision{Recipients: []string{"lead"}},
		},
		{
			name: "sole open task follow-up routes to its owner with an assigned line",
			in: routingInput{
				Text:      "hurry up",
				Roster:    roster3,
				OpenTasks: []taskRef{openTask},
			},
			want: routingDecision{Recipients: []string{"scout"}, FollowUpTaskID: "task-1", AssignedLine: true},
		},
		{
			name: "a completed sole task holds no open slot — the follow-up falls to the lead (align design D8)",
			in:   routingInput{Text: "hurry up", Roster: roster3, OpenTasks: nil},
			want: routingDecision{Recipients: []string{"lead"}},
		},
		{
			name: "two open tasks are not sole — falls to the lead",
			in: routingInput{
				Text:   "status?",
				Roster: roster3,
				OpenTasks: []taskRef{
					openTask,
					{TaskID: "task-2", OwnerAgentID: "jono"},
				},
			},
			want: routingDecision{Recipients: []string{"lead"}},
		},
		{
			name: "single-member group routes to that member, no line",
			in: routingInput{
				Text:   "hello",
				Roster: routingRosterOf("solo", memberRef{AgentID: "solo", Name: "Solo"}),
			},
			want: routingDecision{Recipients: []string{"solo"}},
		},
		{
			name: "untargeted message goes to the lead, no line",
			in:   routingInput{Text: "what's the plan?", Roster: roster3},
			want: routingDecision{Recipients: []string{"lead"}},
		},
		{
			name: "mention of a non-roster member routes to the lead",
			in:   routingInput{Text: "@Ghost do it", Roster: roster3},
			want: routingDecision{Recipients: []string{"lead"}},
		},
		{
			name: "mention of a non-roster member among valid mentions keeps the valid one",
			in:   routingInput{Text: "@Ghost @Scout do it", Roster: roster3},
			want: routingDecision{Recipients: []string{"scout"}},
		},
		{
			name: "reply to a task whose owner left the roster falls through",
			in: routingInput{
				Text:   "also add charts",
				Roster: roster3,
				Reply:  &replyTarget{Task: &taskRef{TaskID: "task-9", OwnerAgentID: "ghost"}},
			},
			want: routingDecision{Recipients: []string{"lead"}},
		},
		{
			name: "sole open task owned by a non-roster member falls to the lead",
			in: routingInput{
				Text:      "hurry up",
				Roster:    roster3,
				OpenTasks: []taskRef{{TaskID: "task-9", OwnerAgentID: "ghost"}},
			},
			want: routingDecision{Recipients: []string{"lead"}},
		},
		{
			name: "member-less group routes nowhere",
			in:   routingInput{Text: "hello", Roster: routingRosterOf("")},
			want: routingDecision{},
		},
		{
			name: "everyone in a single-member group routes to the member",
			in:   routingInput{Text: "@everyone", Roster: routingRosterOf("solo", memberRef{AgentID: "solo", Name: "Solo"})},
			want: routingDecision{Recipients: []string{"solo"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := routeMessage(tt.in)
			require.Equal(t, tt.want, got)
		})
	}
}

// rosteredHarness wires three agents: Atlas (lead), Scout, Jono.
func rosteredHarness(t *testing.T) (*harness, *store.AgentGroup, *store.Agent, *store.Agent, *store.Agent) {
	h := newHarness(t)
	atlas := h.addAgent("Atlas", "coordinates the crew")
	scout := h.addAgent("Scout", "searches things")
	jono := h.addAgent("Jono", "writes things")
	g := h.newGroup("research", atlas, scout, jono)
	return h, g, atlas, scout, jono
}

// submitAgentID resolves which agent the i-th dispatch targeted.
func (h *harness) submitAgentID(i int) string {
	h.t.Helper()
	req := h.rt.submitAt(i)
	sess, err := h.st.Sessions().ByID(h.ctx, req.SessionID)
	require.NoError(h.t, err)
	return sess.AgentID
}

// TestSend_RoutingTranscript pins the routed outcomes at the Send level: the
// dispatch lands on the right member's session, tasks are created (or
// continued), and the coordinator assigned lines appear only for
// task-targeted routes.
func TestSend_RoutingTranscript(t *testing.T) {
	t.Run("mention routes directly with no assigned line", func(t *testing.T) {
		h, g, _, scout, _ := rosteredHarness(t)
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Scout summarize the corpus"}))

		require.Equal(t, 1, h.rt.submitCount())
		require.Equal(t, scout.ID, h.submitAgentID(0))
		require.Len(t, h.messagesOf(g.ID, store.GroupMessageKindAssigned), 0, "mention-routed messages add no coordinator line")
		tasks := h.groupTasks(g.ID)
		require.Len(t, tasks, 1)
		require.Equal(t, scout.ID, deref(tasks[0].OwnerAgentID))
		require.Equal(t, store.GroupTaskStateRunning, tasks[0].State, "the dispatch persists running before submit")
	})

	t.Run("everyone fans out to every member", func(t *testing.T) {
		h, g, atlas, scout, jono := rosteredHarness(t)
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@everyone give your view"}))

		// Each member receives the message as its own request (its own root
		// task); the group budget dispatches the first two and queues the
		// third.
		require.Len(t, h.groupTasks(g.ID), 3)
		require.Equal(t, 2, h.rt.submitCount())
		got := map[string]bool{}
		for i := 0; i < 2; i++ {
			got[h.submitAgentID(i)] = true
		}
		require.Equal(t, map[string]bool{atlas.ID: true, scout.ID: true}, got)
		tasks := h.groupTasks(g.ID)
		var queued int
		for _, task := range tasks {
			if task.State == store.GroupTaskStateQueued {
				queued++
			}
		}
		require.Equal(t, 1, queued, "the third assignment waits in the queued state")
		require.Len(t, h.messagesOf(g.ID, store.GroupMessageKindAssigned), 0)
		_ = jono
	})

	t.Run("explicit recipient from the mention picker behaves like a mention", func(t *testing.T) {
		h, g, _, _, jono := rosteredHarness(t)
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "please take this", RecipientAgentID: jono.ID}))

		require.Equal(t, 1, h.rt.submitCount())
		require.Equal(t, jono.ID, h.submitAgentID(0))
		require.Len(t, h.messagesOf(g.ID, store.GroupMessageKindAssigned), 0)
	})

	t.Run("untargeted message goes to the lead with no assigned line", func(t *testing.T) {
		h, g, atlas, _, _ := rosteredHarness(t)
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "what's the plan?"}))

		require.Equal(t, 1, h.rt.submitCount())
		require.Equal(t, atlas.ID, h.submitAgentID(0))
		require.Len(t, h.messagesOf(g.ID, store.GroupMessageKindAssigned), 0)
	})

	t.Run("reply to a task message continues the task with an assigned line", func(t *testing.T) {
		h, g, _, scout, _ := rosteredHarness(t)
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Scout summarize"}))
		// The task stays open (stopped mid-run): a stopped task still
		// catches the task-targeted reply tier.
		task := h.groupTasks(g.ID)[0]
		require.NoError(t, h.svc.StopTask(h.ctx, task.TaskID, "hold"))

		var userMsgID string
		for _, r := range h.transcript(g.ID) {
			if r.AuthorKind == store.GroupAuthorKindUser {
				userMsgID = r.MessageID
				break
			}
		}
		require.NotEmpty(t, userMsgID)
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "also add charts", ReplyToMessageID: userMsgID}))

		assigned := h.messagesOf(g.ID, store.GroupMessageKindAssigned)
		require.Len(t, assigned, 1, "the task-targeted reply writes one assigned line")
		require.Equal(t, "Assigned to Scout", payloadText(assigned[0]))
		require.NotNil(t, assigned[0].TaskID)
		require.Equal(t, task.TaskID, *assigned[0].TaskID)
		require.Len(t, h.groupTasks(g.ID), 1, "no new task: the reply continued the existing one")
		updated := h.taskByID(task.TaskID)
		require.Greater(t, updated.Revision, task.Revision)
		require.Equal(t, scout.ID, h.submitAgentID(1))
	})

	t.Run("reply to a member message without a task returns to that member", func(t *testing.T) {
		h, g, _, _, jono := rosteredHarness(t)
		// Seed Jono's authored transcript row with no task link.
		agentID := jono.ID
		require.NoError(t, h.st.Groups().AppendMessage(h.ctx, &store.AgentGroupMessage{
			GroupID:       g.ID,
			MessageID:     "member-row",
			Kind:          store.GroupMessageKindMessage,
			AuthorKind:    store.GroupAuthorKindAgent,
			AuthorAgentID: &agentID,
			Payload:       mustPayload(t, "here is my note"),
		}))
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "thanks!", ReplyToMessageID: "member-row"}))

		require.Equal(t, 1, h.rt.submitCount())
		require.Equal(t, jono.ID, h.submitAgentID(0))
		require.Len(t, h.messagesOf(g.ID, store.GroupMessageKindAssigned), 0, "the member-targeted reply adds no assigned line")
		require.Len(t, h.groupTasks(g.ID), 1, "the reply seeds a fresh root task")
	})

	t.Run("member message with task link routes as a task follow-up", func(t *testing.T) {
		h, g, _, scout, _ := rosteredHarness(t)
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Scout summarize"}))
		// The task stays open (stopped mid-run): its linked rows still
		// resolve to the task follow-up tier.
		task := h.groupTasks(g.ID)[0]
		require.NoError(t, h.svc.StopTask(h.ctx, task.TaskID, "hold"))

		// The member's copied-back row links the task; a reply to it is a
		// task follow-up (tier 3) with an assigned line.
		agentID := scout.ID
		require.NoError(t, h.st.Groups().AppendMessage(h.ctx, &store.AgentGroupMessage{
			GroupID:       g.ID,
			MessageID:     "scout-row",
			Kind:          store.GroupMessageKindMessage,
			AuthorKind:    store.GroupAuthorKindAgent,
			AuthorAgentID: &agentID,
			TaskID:        &task.TaskID,
			Payload:       mustPayload(t, "done summarizing"),
		}))
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "more detail", ReplyToMessageID: "scout-row"}))

		require.Len(t, h.messagesOf(g.ID, store.GroupMessageKindAssigned), 1)
		require.Len(t, h.groupTasks(g.ID), 1)
		require.Equal(t, scout.ID, h.submitAgentID(1))
	})

	t.Run("sole open task follow-up routes to its owner with an assigned line", func(t *testing.T) {
		h, g, _, scout, _ := rosteredHarness(t)
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Scout summarize"}))
		// The turn is still in flight: the task is open.

		// No reply context: the sole open task catches the follow-up.
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hurry up"}))
		require.Len(t, h.messagesOf(g.ID, store.GroupMessageKindAssigned), 1)
		require.Len(t, h.groupTasks(g.ID), 1, "no new task: the follow-up continued the existing one")
		require.Equal(t, scout.ID, h.submitAgentID(1))
	})

	t.Run("a completed sole task routes the follow-up to the lead (align design D8)", func(t *testing.T) {
		h, g, atlas, _, _ := rosteredHarness(t)
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Scout summarize"}))
		h.rt.finishTurn(0, nil)
		requireEventually(t, "task completes", func() bool {
			tasks := h.groupTasks(g.ID)
			return len(tasks) == 1 && tasks[0].State == store.GroupTaskStateCompleted
		})

		// No open task remains: the untargeted follow-up falls past the
		// sole-open-task tier to the lead, as a fresh root task.
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hurry up"}))
		require.Len(t, h.messagesOf(g.ID, store.GroupMessageKindAssigned), 0, "the lead route adds no assigned line")
		require.Len(t, h.groupTasks(g.ID), 2, "the follow-up seeds a fresh root for the lead")
		require.Equal(t, atlas.ID, h.submitAgentID(1))
	})

	t.Run("member-less group accepts the message without routing", func(t *testing.T) {
		h := newHarness(t)
		g, err := h.svc.CreateGroup(h.ctx, "empty", "", "", nil)
		require.NoError(t, err)
		require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "anyone there?"}))
		require.Equal(t, 0, h.rt.submitCount())
		require.Len(t, h.groupTasks(g.ID), 0)
		require.Len(t, h.transcript(g.ID), 1)
	})
}

// mustPayload builds a row payload for test seeds.
func mustPayload(t *testing.T, text string) string {
	t.Helper()
	b, err := makePayload(text, nil)
	require.NoError(t, err)
	return b
}
