package sqlite

import (
	"context"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// newGroupFixture creates an agent (the FK target for lead/owner columns)
// and a group led by it.
func newGroupFixture(t *testing.T, ctx context.Context, s *Store, name string) (*store.Agent, *store.AgentGroup) {
	t.Helper()
	agent := &store.Agent{Name: name + "-agent"}
	require.NoError(t, s.Agents().Create(ctx, agent))
	g := &store.AgentGroup{Name: name, Title: name + " title", Instructions: "be helpful"}
	require.NoError(t, s.Groups().CreateGroup(ctx, g))
	return agent, g
}

// strPtr is a nullable-string literal for test seeds.
func strPtr(s string) *string { return &s }

// TestAgentGroups_GroupCRUD round-trips the group aggregate: create assigns
// ID and stamps, update rewrites keeping CreatedAt, list orders by creation,
// delete cascades the member rows.
func TestAgentGroups_GroupCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	t.Run("create assigns id and stamps", func(t *testing.T) {
		g := &store.AgentGroup{Name: "crew"}
		require.NoError(t, s.Groups().CreateGroup(ctx, g))
		require.NotEmpty(t, g.ID)
		require.False(t, g.CreatedAt.IsZero())
		require.False(t, g.UpdatedAt.IsZero())
		require.Nil(t, g.LeadAgentID, "a fresh group has no lead")
	})

	t.Run("round-trip and update keep created_at", func(t *testing.T) {
		agent, g := newGroupFixture(t, ctx, s, "atlas-crew")

		got, err := s.Groups().GroupByID(ctx, g.ID)
		require.NoError(t, err)
		require.Equal(t, "atlas-crew", got.Name)
		require.Equal(t, "atlas-crew title", got.Title)
		require.Equal(t, "be helpful", got.Instructions)

		got.Title = "renamed"
		got.Archived = true
		got.LeadAgentID = strPtr(agent.ID)
		got.Revision = 3
		require.NoError(t, s.Groups().UpdateGroup(ctx, got))

		again, err := s.Groups().GroupByID(ctx, g.ID)
		require.NoError(t, err)
		require.Equal(t, "renamed", again.Title)
		require.True(t, again.Archived)
		require.NotNil(t, again.LeadAgentID)
		require.Equal(t, agent.ID, *again.LeadAgentID)
		require.EqualValues(t, 3, again.Revision)
		require.True(t, again.CreatedAt.Equal(g.CreatedAt), "update must keep CreatedAt")
	})

	t.Run("update absent row is ErrNotFound", func(t *testing.T) {
		err := s.Groups().UpdateGroup(ctx, &store.AgentGroup{ID: "missing"})
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("list orders by creation", func(t *testing.T) {
		groups, err := s.Groups().ListGroups(ctx)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(groups), 2)
		for i := 1; i < len(groups); i++ {
			require.False(t, groups[i].CreatedAt.Before(groups[i-1].CreatedAt))
		}
	})

	t.Run("delete cascades members", func(t *testing.T) {
		_, g := newGroupFixture(t, ctx, s, "doomed-crew")
		require.NoError(t, s.Groups().AddMember(ctx, store.AgentGroupMember{GroupID: g.ID, AgentID: "a1"}))
		require.NoError(t, s.Groups().AppendMessage(ctx, &store.AgentGroupMessage{
			GroupID: g.ID, MessageID: "m1", Kind: store.GroupMessageKindMessage,
			AuthorKind: store.GroupAuthorKindUser,
		}))

		require.NoError(t, s.Groups().DeleteGroup(ctx, g.ID))
		_, err := s.Groups().GroupByID(ctx, g.ID)
		require.ErrorIs(t, err, store.ErrNotFound)
		members, err := s.Groups().ListMembers(ctx, g.ID)
		require.NoError(t, err)
		require.Empty(t, members)
		msgs, err := s.Groups().ListMessages(ctx, g.ID, 0, 10)
		require.NoError(t, err)
		require.Empty(t, msgs)
	})

	t.Run("delete absent row is ErrNotFound", func(t *testing.T) {
		require.ErrorIs(t, s.Groups().DeleteGroup(ctx, "missing"), store.ErrNotFound)
	})
}

// TestAgentGroups_Members covers the roster: duplicate add is ErrDuplicate
// (PK group_id, agent_id), remove is ErrNotFound when absent, list orders
// by added time.
func TestAgentGroups_Members(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, g := newGroupFixture(t, ctx, s, "roster-crew")

	require.NoError(t, s.Groups().AddMember(ctx, store.AgentGroupMember{GroupID: g.ID, AgentID: "a2"}))
	require.NoError(t, s.Groups().AddMember(ctx, store.AgentGroupMember{GroupID: g.ID, AgentID: "a1"}))
	require.ErrorIs(t, s.Groups().AddMember(ctx, store.AgentGroupMember{GroupID: g.ID, AgentID: "a2"}), store.ErrDuplicate,
		"PK(group_id, agent_id) must reject a duplicate member add")

	members, err := s.Groups().ListMembers(ctx, g.ID)
	require.NoError(t, err)
	require.Len(t, members, 2)
	require.Equal(t, "a2", members[0].AgentID, "first added comes first")
	require.Equal(t, "a1", members[1].AgentID)
	require.False(t, members[0].AddedAt.IsZero())

	require.NoError(t, s.Groups().RemoveMember(ctx, g.ID, "a2"))
	members, err = s.Groups().ListMembers(ctx, g.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, "a1", members[0].AgentID)

	require.ErrorIs(t, s.Groups().RemoveMember(ctx, g.ID, "a2"), store.ErrNotFound)
}

// TestAgentGroups_Messages covers the transcript: per-group gapless seq
// allocation independent across groups, the UNIQUE(group_id, message_id)
// dedupe, in-place rewrite keeping seq, paging windows, and LastSeq.
func TestAgentGroups_Messages(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, g1 := newGroupFixture(t, ctx, s, "chat-crew")
	_, g2 := newGroupFixture(t, ctx, s, "other-crew")

	appendMsg := func(groupID, messageID string) *store.AgentGroupMessage {
		t.Helper()
		m := &store.AgentGroupMessage{
			GroupID: groupID, MessageID: messageID,
			Kind: store.GroupMessageKindMessage, AuthorKind: store.GroupAuthorKindUser,
			Payload: `{"text":"hi"}`,
		}
		require.NoError(t, s.Groups().AppendMessage(ctx, m))
		return m
	}

	t.Run("per-group seq allocation is gapless and group-scoped", func(t *testing.T) {
		m1 := appendMsg(g1.ID, "u1")
		m2 := appendMsg(g1.ID, "u2")
		other := appendMsg(g2.ID, "u1")
		require.EqualValues(t, 1, m1.Seq)
		require.EqualValues(t, 2, m2.Seq)
		require.EqualValues(t, 1, other.Seq, "each group's seq starts at 1")

		seq, err := s.Groups().LastSeq(ctx, g1.ID)
		require.NoError(t, err)
		require.EqualValues(t, 2, seq)
		emptySeq, err := s.Groups().LastSeq(ctx, "no-such-group")
		require.NoError(t, err)
		require.EqualValues(t, 0, emptySeq, "an unknown group has no messages")
	})

	t.Run("duplicate message id is ErrDuplicate", func(t *testing.T) {
		err := s.Groups().AppendMessage(ctx, &store.AgentGroupMessage{
			GroupID: g1.ID, MessageID: "u1",
			Kind: store.GroupMessageKindMessage, AuthorKind: store.GroupAuthorKindUser,
		})
		require.ErrorIs(t, err, store.ErrDuplicate, "UNIQUE(group_id, message_id) must reject a re-append")
	})

	t.Run("update message rewrites in place keeping seq", func(t *testing.T) {
		m := appendMsg(g1.ID, "u3")
		m.Kind = store.GroupMessageKindResult
		m.Payload = `{"text":"redone"}`
		require.NoError(t, s.Groups().UpdateMessage(ctx, m))
		require.EqualValues(t, 3, m.Seq, "regenerate keeps the original seq")

		got, err := s.Groups().ListMessages(ctx, g1.ID, 0, 10)
		require.NoError(t, err)
		require.Len(t, got, 3)
		require.Equal(t, "u3", got[2].MessageID)
		require.Equal(t, store.GroupMessageKindResult, got[2].Kind)
		require.Equal(t, `{"text":"redone"}`, got[2].Payload)
	})

	t.Run("update absent message is ErrNotFound", func(t *testing.T) {
		err := s.Groups().UpdateMessage(ctx, &store.AgentGroupMessage{
			GroupID: g1.ID, MessageID: "missing",
			Kind: store.GroupMessageKindMessage, AuthorKind: store.GroupAuthorKindUser,
		})
		require.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("paging windows replay in ascending order", func(t *testing.T) {
		// g1 currently holds seq 1..3 (u1, u2, u3). Append two more.
		appendMsg(g1.ID, "u4")
		appendMsg(g1.ID, "u5")

		page, err := s.Groups().ListMessages(ctx, g1.ID, 0, 3)
		require.NoError(t, err)
		require.Len(t, page, 3)
		require.Equal(t, []string{"u3", "u4", "u5"}, []string{page[0].MessageID, page[1].MessageID, page[2].MessageID},
			"beforeSeq=0 takes the newest window, replayed ascending")

		page, err = s.Groups().ListMessages(ctx, g1.ID, 3, 3)
		require.NoError(t, err)
		require.Len(t, page, 2)
		require.Equal(t, []string{"u1", "u2"}, []string{page[0].MessageID, page[1].MessageID},
			"seq strictly below the bound")
	})

	t.Run("invalid message kind violates the CHECK", func(t *testing.T) {
		err := s.Groups().AppendMessage(ctx, &store.AgentGroupMessage{
			GroupID: g1.ID, MessageID: "bad-kind", Kind: "shout",
			AuthorKind: store.GroupAuthorKindUser,
		})
		require.Error(t, err)
	})
}

// TestAgentGroups_Cursors covers the acceptance-cursor upsert: absent pairs
// insert, present pairs rewrite in place.
func TestAgentGroups_Cursors(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, g := newGroupFixture(t, ctx, s, "cursor-crew")

	_, err := s.Groups().GetCursor(ctx, g.ID, "a1")
	require.ErrorIs(t, err, store.ErrNotFound, "a member without a cursor reads as absent")

	require.NoError(t, s.Groups().SetCursor(ctx, store.AgentGroupContext{
		GroupID: g.ID, AgentID: "a1", SessionID: "sess-1", ThroughSeq: 5, SummaryVersion: 1,
	}))
	got, err := s.Groups().GetCursor(ctx, g.ID, "a1")
	require.NoError(t, err)
	require.Equal(t, "sess-1", got.SessionID)
	require.EqualValues(t, 5, got.ThroughSeq)
	require.EqualValues(t, 1, got.SummaryVersion)

	require.NoError(t, s.Groups().SetCursor(ctx, store.AgentGroupContext{
		GroupID: g.ID, AgentID: "a1", SessionID: "sess-1", ThroughSeq: 9, SummaryVersion: 2,
	}))
	got, err = s.Groups().GetCursor(ctx, g.ID, "a1")
	require.NoError(t, err)
	require.EqualValues(t, 9, got.ThroughSeq, "the upsert rewrites in place")
	require.EqualValues(t, 2, got.SummaryVersion)
}

// TestAgentGroups_Tasks covers the task system: create/update round-trip,
// the 7-state CHECK, the open-state worklist, and not-found paths.
func TestAgentGroups_Tasks(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	agent, g := newGroupFixture(t, ctx, s, "task-crew")

	t.Run("create and update round-trip", func(t *testing.T) {
		task := &store.AgentGroupTask{
			GroupID: g.ID, RootTaskID: g.ID,
			RequestMessageID: "m1", Instruction: "scout the docs",
			ExpectedResult: "a summary", State: store.GroupTaskStateQueued,
		}
		require.NoError(t, s.Groups().CreateTask(ctx, task))
		require.NotEmpty(t, task.TaskID)

		task.State = store.GroupTaskStateRunning
		task.OwnerAgentID = strPtr(agent.ID)
		task.Revision = 2
		task.AssignmentCount = 1
		require.NoError(t, s.Groups().UpdateTask(ctx, task))

		got, err := s.Groups().TaskByID(ctx, task.TaskID)
		require.NoError(t, err)
		require.Equal(t, g.ID, got.GroupID)
		require.Equal(t, store.GroupTaskStateRunning, got.State)
		require.NotNil(t, got.OwnerAgentID)
		require.Equal(t, agent.ID, *got.OwnerAgentID)
		require.EqualValues(t, 2, got.Revision)
		require.Equal(t, 1, got.AssignmentCount)
		require.Equal(t, "scout the docs", got.Instruction)
	})

	t.Run("invalid state violates the CHECK", func(t *testing.T) {
		err := s.Groups().CreateTask(ctx, &store.AgentGroupTask{
			GroupID: g.ID, RootTaskID: g.ID, State: "teleported",
		})
		require.Error(t, err, "the 7-state CHECK must reject an invalid state")
	})

	t.Run("nullable stop reason round-trips", func(t *testing.T) {
		task := &store.AgentGroupTask{
			GroupID: g.ID, RootTaskID: g.ID, State: store.GroupTaskStatePaused,
			StopReason: strPtr("user stopped it"), Error: strPtr(""),
		}
		require.NoError(t, s.Groups().CreateTask(ctx, task))

		got, err := s.Groups().TaskByID(ctx, task.TaskID)
		require.NoError(t, err)
		require.NotNil(t, got.StopReason)
		require.Equal(t, "user stopped it", *got.StopReason)
	})

	t.Run("list open tasks spans groups and states", func(t *testing.T) {
		require.NoError(t, s.Groups().CreateTask(ctx, &store.AgentGroupTask{
			GroupID: g.ID, RootTaskID: g.ID, State: store.GroupTaskStateWaiting,
		}))
		require.NoError(t, s.Groups().CreateTask(ctx, &store.AgentGroupTask{
			GroupID: g.ID, RootTaskID: g.ID, State: store.GroupTaskStateCompleted,
		}))

		tasks, err := s.Groups().ListTasksByGroup(ctx, g.ID)
		require.NoError(t, err)
		// The running, paused, and completed tasks created by the earlier
		// subtests plus the waiting one created above.
		require.Len(t, tasks, 4)

		open, err := s.Groups().ListOpenTasks(ctx)
		require.NoError(t, err)
		states := make([]string, 0, len(open))
		for _, task := range open {
			states = append(states, task.State)
		}
		require.Contains(t, states, store.GroupTaskStateRunning)
		require.Contains(t, states, store.GroupTaskStateWaiting)
		require.NotContains(t, states, store.GroupTaskStateCompleted)
	})

	t.Run("absent rows are ErrNotFound", func(t *testing.T) {
		_, err := s.Groups().TaskByID(ctx, "missing")
		require.ErrorIs(t, err, store.ErrNotFound)
		err = s.Groups().UpdateTask(ctx, &store.AgentGroupTask{TaskID: "missing", State: store.GroupTaskStateQueued})
		require.ErrorIs(t, err, store.ErrNotFound)
	})
}

// TestAgentGroups_Summaries covers the versioned summary upsert.
func TestAgentGroups_Summaries(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, g := newGroupFixture(t, ctx, s, "summary-crew")

	_, err := s.Groups().GetSummary(ctx, g.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "no summary exists before the first save")

	require.NoError(t, s.Groups().SaveSummary(ctx, &store.AgentGroupSummary{
		GroupID: g.ID, Version: 1, ThroughSeq: 4, Text: "early chatter",
	}))
	got, err := s.Groups().GetSummary(ctx, g.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, got.Version)
	require.EqualValues(t, 4, got.ThroughSeq)
	require.Equal(t, "early chatter", got.Text)

	require.NoError(t, s.Groups().SaveSummary(ctx, &store.AgentGroupSummary{
		GroupID: g.ID, Version: 2, ThroughSeq: 12, Text: "the whole story",
	}))
	got, err = s.Groups().GetSummary(ctx, g.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, got.Version, "the upsert rewrites version in place")
	require.EqualValues(t, 12, got.ThroughSeq)
	require.Equal(t, "the whole story", got.Text)
}
