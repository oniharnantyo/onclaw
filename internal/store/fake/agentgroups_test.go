package fake_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// fakeGroupFixture creates an agent and a group led by it.
func fakeGroupFixture(t *testing.T, ctx context.Context, s *fake.Store, name string) (*store.Agent, *store.AgentGroup) {
	t.Helper()
	agent := &store.Agent{Name: name + "-agent"}
	require.NoError(t, s.Agents().Create(ctx, agent))
	g := &store.AgentGroup{Name: name, Title: name + " title", Instructions: "be helpful"}
	require.NoError(t, s.Groups().CreateGroup(ctx, g))
	return agent, g
}

// fakeStrPtr is a nullable-string literal for test seeds.
func fakeStrPtr(s string) *string { return &s }

// TestAgentGroups_Fake_CRUDAndConformance mirrors the sqlite adapter's
// agent-group lifecycle over the fake: group CRUD, roster uniqueness,
// per-group seq allocation with the message-id dedupe, cursor upsert, the
// revision-guarded task worklist, and the versioned summary.
func TestAgentGroups_Fake_CRUDAndConformance(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	t.Run("group CRUD and copy-on-read", func(t *testing.T) {
		agent, g := fakeGroupFixture(t, ctx, s, "atlas-crew")
		require.NotEmpty(t, g.ID)
		require.Nil(t, g.LeadAgentID, "a fresh group has no lead")

		got, err := s.Groups().GroupByID(ctx, g.ID)
		require.NoError(t, err)
		require.Equal(t, "atlas-crew", got.Name)

		got.Title = "renamed"
		got.Archived = true
		got.LeadAgentID = fakeStrPtr(agent.ID)
		got.Revision = 3
		require.NoError(t, s.Groups().UpdateGroup(ctx, got))

		again, err := s.Groups().GroupByID(ctx, g.ID)
		require.NoError(t, err)
		require.Equal(t, "renamed", again.Title)
		require.True(t, again.Archived)
		require.Equal(t, agent.ID, *again.LeadAgentID)
		require.True(t, again.CreatedAt.Equal(g.CreatedAt), "update must keep CreatedAt")

		// Mutating a returned group must not alias the fake's state.
		*again.LeadAgentID = "mutated"
		third, err := s.Groups().GroupByID(ctx, g.ID)
		require.NoError(t, err)
		require.Equal(t, agent.ID, *third.LeadAgentID, "returned groups must be copies")

		require.ErrorIs(t, s.Groups().UpdateGroup(ctx, &store.AgentGroup{ID: "missing"}), store.ErrNotFound)
		require.ErrorIs(t, s.Groups().DeleteGroup(ctx, "missing"), store.ErrNotFound)
	})

	t.Run("members reject duplicates", func(t *testing.T) {
		_, g := fakeGroupFixture(t, ctx, s, "roster-crew")

		require.NoError(t, s.Groups().AddMember(ctx, store.AgentGroupMember{GroupID: g.ID, AgentID: "a2"}))
		require.NoError(t, s.Groups().AddMember(ctx, store.AgentGroupMember{GroupID: g.ID, AgentID: "a1"}))
		require.ErrorIs(t, s.Groups().AddMember(ctx, store.AgentGroupMember{GroupID: g.ID, AgentID: "a2"}), store.ErrDuplicate)

		members, err := s.Groups().ListMembers(ctx, g.ID)
		require.NoError(t, err)
		require.Len(t, members, 2)
		require.Equal(t, "a2", members[0].AgentID, "first added comes first")

		require.NoError(t, s.Groups().RemoveMember(ctx, g.ID, "a2"))
		require.ErrorIs(t, s.Groups().RemoveMember(ctx, g.ID, "a2"), store.ErrNotFound)
	})

	t.Run("messages: per-group seq, dedupe, rewrite, paging", func(t *testing.T) {
		_, g1 := fakeGroupFixture(t, ctx, s, "chat-crew")
		_, g2 := fakeGroupFixture(t, ctx, s, "other-crew")

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

		m1 := appendMsg(g1.ID, "u1")
		m2 := appendMsg(g1.ID, "u2")
		other := appendMsg(g2.ID, "u1")
		require.EqualValues(t, 1, m1.Seq)
		require.EqualValues(t, 2, m2.Seq)
		require.EqualValues(t, 1, other.Seq, "each group's seq starts at 1")

		seq, err := s.Groups().LastSeq(ctx, g1.ID)
		require.NoError(t, err)
		require.EqualValues(t, 2, seq)

		err = s.Groups().AppendMessage(ctx, &store.AgentGroupMessage{
			GroupID: g1.ID, MessageID: "u1",
			Kind: store.GroupMessageKindMessage, AuthorKind: store.GroupAuthorKindUser,
		})
		require.ErrorIs(t, err, store.ErrDuplicate, "UNIQUE(group_id, message_id) must reject a re-append")

		m2.Kind = store.GroupMessageKindResult
		m2.Payload = `{"text":"redone"}`
		taskID := "t-1"
		m2.TaskID = &taskID
		require.NoError(t, s.Groups().UpdateMessage(ctx, m2))
		require.EqualValues(t, 2, m2.Seq, "regenerate keeps the original seq")

		page, err := s.Groups().ListMessages(ctx, g1.ID, 0, 1)
		require.NoError(t, err)
		require.Len(t, page, 1)
		require.Equal(t, "u2", page[0].MessageID)
		require.Equal(t, `{"text":"redone"}`, page[0].Payload)

		// Mutating a returned message's task pointer must not alias the fake
		// (payload strings are immutable; pointer fields prove the copies).
		*page[0].TaskID = "X"
		fresh, err := s.Groups().ListMessages(ctx, g1.ID, 0, 1)
		require.NoError(t, err)
		require.Equal(t, `{"text":"redone"}`, fresh[0].Payload, "returned messages must be copies")
		require.Equal(t, "t-1", *fresh[0].TaskID, "returned messages must be copies")

		require.ErrorIs(t, s.Groups().UpdateMessage(ctx, &store.AgentGroupMessage{
			GroupID: g1.ID, MessageID: "missing",
			Kind: store.GroupMessageKindMessage, AuthorKind: store.GroupAuthorKindUser,
		}), store.ErrNotFound)
	})

	t.Run("cursors upsert", func(t *testing.T) {
		_, g := fakeGroupFixture(t, ctx, s, "cursor-crew")

		_, err := s.Groups().GetCursor(ctx, g.ID, "a1")
		require.ErrorIs(t, err, store.ErrNotFound)

		require.NoError(t, s.Groups().SetCursor(ctx, store.AgentGroupContext{
			GroupID: g.ID, AgentID: "a1", SessionID: "sess-1", ThroughSeq: 5, SummaryVersion: 1,
		}))
		require.NoError(t, s.Groups().SetCursor(ctx, store.AgentGroupContext{
			GroupID: g.ID, AgentID: "a1", SessionID: "sess-1", ThroughSeq: 9, SummaryVersion: 2,
		}))
		got, err := s.Groups().GetCursor(ctx, g.ID, "a1")
		require.NoError(t, err)
		require.EqualValues(t, 9, got.ThroughSeq, "the upsert rewrites in place")
		require.EqualValues(t, 2, got.SummaryVersion)
	})

	t.Run("tasks: round-trip, open worklist, not-found", func(t *testing.T) {
		agent, g := fakeGroupFixture(t, ctx, s, "task-crew")

		task := &store.AgentGroupTask{
			GroupID: g.ID, RootTaskID: g.ID, RequestMessageID: "m1",
			Instruction: "scout the docs", ExpectedResult: "a summary",
			State: store.GroupTaskStateQueued,
		}
		require.NoError(t, s.Groups().CreateTask(ctx, task))
		require.NotEmpty(t, task.TaskID)

		task.State = store.GroupTaskStateRunning
		task.OwnerAgentID = fakeStrPtr(agent.ID)
		task.Revision = 2
		task.AssignmentCount = 1
		require.NoError(t, s.Groups().UpdateTask(ctx, task))

		got, err := s.Groups().TaskByID(ctx, task.TaskID)
		require.NoError(t, err)
		require.Equal(t, store.GroupTaskStateRunning, got.State)
		require.Equal(t, agent.ID, *got.OwnerAgentID)
		require.EqualValues(t, 2, got.Revision)

		require.NoError(t, s.Groups().CreateTask(ctx, &store.AgentGroupTask{
			GroupID: g.ID, RootTaskID: g.ID, State: store.GroupTaskStateCompleted,
		}))

		open, err := s.Groups().ListOpenTasks(ctx)
		require.NoError(t, err)
		require.Len(t, open, 1, "only the non-terminal task is open")
		require.Equal(t, task.TaskID, open[0].TaskID)

		tasks, err := s.Groups().ListTasksByGroup(ctx, g.ID)
		require.NoError(t, err)
		require.Len(t, tasks, 2)

		_, err = s.Groups().TaskByID(ctx, "missing")
		require.ErrorIs(t, err, store.ErrNotFound)
		require.ErrorIs(t, s.Groups().UpdateTask(ctx, &store.AgentGroupTask{
			TaskID: "missing", State: store.GroupTaskStateQueued,
		}), store.ErrNotFound)
	})

	t.Run("summaries version in place", func(t *testing.T) {
		_, g := fakeGroupFixture(t, ctx, s, "summary-crew")

		_, err := s.Groups().GetSummary(ctx, g.ID)
		require.ErrorIs(t, err, store.ErrNotFound)

		require.NoError(t, s.Groups().SaveSummary(ctx, &store.AgentGroupSummary{
			GroupID: g.ID, Version: 1, ThroughSeq: 4, Text: "early chatter",
		}))
		require.NoError(t, s.Groups().SaveSummary(ctx, &store.AgentGroupSummary{
			GroupID: g.ID, Version: 2, ThroughSeq: 12, Text: "the whole story",
		}))
		got, err := s.Groups().GetSummary(ctx, g.ID)
		require.NoError(t, err)
		require.EqualValues(t, 2, got.Version)
		require.EqualValues(t, 12, got.ThroughSeq)
		require.Equal(t, "the whole story", got.Text)
	})
}
