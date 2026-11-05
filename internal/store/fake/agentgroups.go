package fake

import (
	"context"
	"fmt"
	"sort"

	"github.com/oniharnantyo/buddies/internal/store"
)

// agentGroupPair is the composite identity of a membership or acceptance
// cursor row: (group id, agent id).
type agentGroupPair struct {
	GroupID string
	AgentID string
}

type agentGroupStore struct{ s *Store }

// Group CRUD

// CreateGroup stores a new group, assigning ID and stamping the timestamps
// when empty.
func (f agentGroupStore) CreateGroup(_ context.Context, g *store.AgentGroup) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if g.ID == "" {
		g.ID = nextID()
	}
	if _, ok := f.s.agentGroups[g.ID]; ok {
		return fmt.Errorf("create group %s: %w", g.ID, store.ErrDuplicate)
	}
	stamp(&g.CreatedAt, &g.UpdatedAt)
	f.s.agentGroups[g.ID] = *g
	return nil
}

// GroupByID returns the group with the given ID; ErrNotFound when absent.
func (f agentGroupStore) GroupByID(_ context.Context, id string) (*store.AgentGroup, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	g, ok := f.s.agentGroups[id]
	if !ok {
		return nil, fmt.Errorf("get group %s: %w", id, store.ErrNotFound)
	}
	out := cloneAgentGroup(g)
	return &out, nil
}

// UpdateGroup rewrites the group's editable columns, keeping the original
// CreatedAt; ErrNotFound when absent.
func (f agentGroupStore) UpdateGroup(_ context.Context, g *store.AgentGroup) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	stored, ok := f.s.agentGroups[g.ID]
	if !ok {
		return fmt.Errorf("update group %s: %w", g.ID, store.ErrNotFound)
	}
	g.CreatedAt = stored.CreatedAt
	if g.UpdatedAt.IsZero() {
		g.UpdatedAt = nowUTC()
	}
	f.s.agentGroups[g.ID] = *g
	return nil
}

// ListGroups returns every group ordered by creation time, then id. The
// result is never nil.
func (f agentGroupStore) ListGroups(_ context.Context) ([]store.AgentGroup, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	out := make([]store.AgentGroup, 0, len(f.s.agentGroups))
	for _, g := range f.s.agentGroups {
		out = append(out, cloneAgentGroup(g))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// DeleteGroup removes the group and its members, transcript, cursors,
// tasks, and summary; ErrNotFound when absent.
func (f agentGroupStore) DeleteGroup(_ context.Context, id string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if _, ok := f.s.agentGroups[id]; !ok {
		return fmt.Errorf("delete group %s: %w", id, store.ErrNotFound)
	}
	delete(f.s.agentGroups, id)
	for pair := range f.s.agentGroupMembers {
		if pair.GroupID == id {
			delete(f.s.agentGroupMembers, pair)
		}
	}
	delete(f.s.agentGroupMessages, id)
	for pair := range f.s.agentGroupContexts {
		if pair.GroupID == id {
			delete(f.s.agentGroupContexts, pair)
		}
	}
	for taskID, t := range f.s.agentGroupTasks {
		if t.GroupID == id {
			delete(f.s.agentGroupTasks, taskID)
		}
	}
	delete(f.s.agentGroupSummaries, id)
	return nil
}

// Members

// AddMember adds one roster row; a duplicate (group_id, agent_id) is the
// ErrDuplicate sentinel.
func (f agentGroupStore) AddMember(_ context.Context, m store.AgentGroupMember) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	key := agentGroupPair{GroupID: m.GroupID, AgentID: m.AgentID}
	if _, ok := f.s.agentGroupMembers[key]; ok {
		return fmt.Errorf("add group member %s/%s: %w", m.GroupID, m.AgentID, store.ErrDuplicate)
	}
	if m.AddedAt.IsZero() {
		m.AddedAt = nowUTC()
	}
	f.s.agentGroupMembers[key] = m
	return nil
}

// RemoveMember drops one roster row; ErrNotFound when absent.
func (f agentGroupStore) RemoveMember(_ context.Context, groupID, agentID string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	key := agentGroupPair{GroupID: groupID, AgentID: agentID}
	if _, ok := f.s.agentGroupMembers[key]; !ok {
		return fmt.Errorf("remove group member %s/%s: %w", groupID, agentID, store.ErrNotFound)
	}
	delete(f.s.agentGroupMembers, key)
	return nil
}

// ListMembers returns the group's roster ordered by added time, then agent
// id. The result is never nil.
func (f agentGroupStore) ListMembers(_ context.Context, groupID string) ([]store.AgentGroupMember, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	out := make([]store.AgentGroupMember, 0)
	for pair, m := range f.s.agentGroupMembers {
		if pair.GroupID == groupID {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].AddedAt.Equal(out[j].AddedAt) {
			return out[i].AddedAt.Before(out[j].AddedAt)
		}
		return out[i].AgentID < out[j].AgentID
	})
	return out, nil
}

// Messages

// AppendMessage appends one transcript row, allocating the next per-group
// Seq and stamping OccurredAt when empty; MessageID is assigned when empty.
// A MessageID already stored for the group is ErrDuplicate.
func (f agentGroupStore) AppendMessage(_ context.Context, m *store.AgentGroupMessage) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if m.MessageID == "" {
		m.MessageID = nextID()
	}
	for i := range f.s.agentGroupMessages[m.GroupID] {
		if f.s.agentGroupMessages[m.GroupID][i].MessageID == m.MessageID {
			return fmt.Errorf("append group message %s/%s: %w", m.GroupID, m.MessageID, store.ErrDuplicate)
		}
	}
	var maxSeq int64
	if n := len(f.s.agentGroupMessages[m.GroupID]); n > 0 {
		maxSeq = f.s.agentGroupMessages[m.GroupID][n-1].Seq
	}
	m.Seq = maxSeq + 1
	if m.OccurredAt.IsZero() {
		m.OccurredAt = nowUTC()
	}
	f.s.agentGroupMessages[m.GroupID] = append(f.s.agentGroupMessages[m.GroupID], cloneAgentGroupMessage(*m))
	return nil
}

// UpdateMessage rewrites one transcript row in place, keeping its Seq —
// the regenerate path (design D7); ErrNotFound when absent.
func (f agentGroupStore) UpdateMessage(_ context.Context, m *store.AgentGroupMessage) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	msgs := f.s.agentGroupMessages[m.GroupID]
	for i := range msgs {
		if msgs[i].MessageID != m.MessageID {
			continue
		}
		m.Seq = msgs[i].Seq
		if m.OccurredAt.IsZero() {
			m.OccurredAt = nowUTC()
		}
		msgs[i] = cloneAgentGroupMessage(*m)
		return nil
	}
	return fmt.Errorf("update group message %s/%s: %w", m.GroupID, m.MessageID, store.ErrNotFound)
}

// ListMessages returns one window of the group's transcript: the newest
// limit messages with Seq strictly below beforeSeq — or the newest limit
// messages overall when beforeSeq is 0 — ordered by Seq ascending. The
// result is empty (not nil) when the window is empty.
func (f agentGroupStore) ListMessages(_ context.Context, groupID string, beforeSeq int64, limit int) ([]store.AgentGroupMessage, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	msgs := f.s.agentGroupMessages[groupID]
	out := make([]store.AgentGroupMessage, 0, len(msgs))
	for i := range msgs {
		if beforeSeq > 0 && msgs[i].Seq >= beforeSeq {
			continue
		}
		out = append(out, cloneAgentGroupMessage(msgs[i]))
	}
	// Newest window first, replayed in ascending order.
	sort.Slice(out, func(i, j int) bool { return out[i].Seq > out[j].Seq })
	if len(out) > limit {
		out = out[:limit]
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// LastSeq returns the group's highest allocated transcript Seq — 0 for a
// group without messages.
func (f agentGroupStore) LastSeq(_ context.Context, groupID string) (int64, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	msgs := f.s.agentGroupMessages[groupID]
	if len(msgs) == 0 {
		return 0, nil
	}
	return msgs[len(msgs)-1].Seq, nil
}

// Cursors

// GetCursor returns one member's acceptance cursor; ErrNotFound when the
// member has none.
func (f agentGroupStore) GetCursor(_ context.Context, groupID, agentID string) (*store.AgentGroupContext, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	c, ok := f.s.agentGroupContexts[agentGroupPair{GroupID: groupID, AgentID: agentID}]
	if !ok {
		return nil, fmt.Errorf("get group cursor %s/%s: %w", groupID, agentID, store.ErrNotFound)
	}
	return &c, nil
}

// SetCursor upserts one member's acceptance cursor.
func (f agentGroupStore) SetCursor(_ context.Context, c store.AgentGroupContext) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	f.s.agentGroupContexts[agentGroupPair{GroupID: c.GroupID, AgentID: c.AgentID}] = c
	return nil
}

// Tasks

// CreateTask stores a new task, assigning TaskID when empty; a TaskID
// already stored is ErrDuplicate.
func (f agentGroupStore) CreateTask(_ context.Context, t *store.AgentGroupTask) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if t.TaskID == "" {
		t.TaskID = nextID()
	}
	if _, ok := f.s.agentGroupTasks[t.TaskID]; ok {
		return fmt.Errorf("create group task %s: %w", t.TaskID, store.ErrDuplicate)
	}
	f.s.agentGroupTasks[t.TaskID] = cloneAgentGroupTask(*t)
	return nil
}

// TaskByID returns the task with the given ID; ErrNotFound when absent.
func (f agentGroupStore) TaskByID(_ context.Context, taskID string) (*store.AgentGroupTask, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	t, ok := f.s.agentGroupTasks[taskID]
	if !ok {
		return nil, fmt.Errorf("get group task %s: %w", taskID, store.ErrNotFound)
	}
	out := cloneAgentGroupTask(t)
	return &out, nil
}

// UpdateTask rewrites the task's mutable columns, keeping the chain fields
// (parent, root, request message); ErrNotFound when absent.
func (f agentGroupStore) UpdateTask(_ context.Context, t *store.AgentGroupTask) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	stored, ok := f.s.agentGroupTasks[t.TaskID]
	if !ok {
		return fmt.Errorf("update group task %s: %w", t.TaskID, store.ErrNotFound)
	}
	updated := cloneAgentGroupTask(*t)
	updated.GroupID = stored.GroupID
	updated.ParentTaskID = stored.ParentTaskID
	updated.RootTaskID = stored.RootTaskID
	updated.RequestMessageID = stored.RequestMessageID
	f.s.agentGroupTasks[t.TaskID] = updated
	return nil
}

// ListTasksByGroup returns the group's tasks ordered by task id. The result
// is never nil.
func (f agentGroupStore) ListTasksByGroup(_ context.Context, groupID string) ([]store.AgentGroupTask, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	out := make([]store.AgentGroupTask, 0)
	for _, t := range f.s.agentGroupTasks {
		if t.GroupID == groupID {
			out = append(out, cloneAgentGroupTask(t))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out, nil
}

// ListOpenTasks returns every non-terminal task across all groups — the
// restart drain's worklist — ordered by task id. The result is never nil.
func (f agentGroupStore) ListOpenTasks(_ context.Context) ([]store.AgentGroupTask, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	open := make(map[string]bool, len(store.GroupTaskOpenStates))
	for _, state := range store.GroupTaskOpenStates {
		open[state] = true
	}
	out := make([]store.AgentGroupTask, 0)
	for _, t := range f.s.agentGroupTasks {
		if open[t.State] {
			out = append(out, cloneAgentGroupTask(t))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out, nil
}

// Summaries

// GetSummary returns the group's transcript summary; ErrNotFound when none
// exists yet.
func (f agentGroupStore) GetSummary(_ context.Context, groupID string) (*store.AgentGroupSummary, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	sum, ok := f.s.agentGroupSummaries[groupID]
	if !ok {
		return nil, fmt.Errorf("get group summary %s: %w", groupID, store.ErrNotFound)
	}
	return &sum, nil
}

// SaveSummary upserts the group's transcript summary; the caller owns the
// versioning.
func (f agentGroupStore) SaveSummary(_ context.Context, sum *store.AgentGroupSummary) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	f.s.agentGroupSummaries[sum.GroupID] = *sum
	return nil
}

// clone helpers: payload strings are immutable, task pointers are nullable
// strings — every value crossing the fake's boundary is copied.

func cloneAgentGroupMessage(in store.AgentGroupMessage) store.AgentGroupMessage {
	in.AuthorAgentID = cloneStringPtr(in.AuthorAgentID)
	in.TaskID = cloneStringPtr(in.TaskID)
	return in
}

func cloneAgentGroupTask(in store.AgentGroupTask) store.AgentGroupTask {
	in.ParentTaskID = cloneStringPtr(in.ParentTaskID)
	in.OwnerAgentID = cloneStringPtr(in.OwnerAgentID)
	in.StopReason = cloneStringPtr(in.StopReason)
	in.Error = cloneStringPtr(in.Error)
	return in
}

func cloneAgentGroup(in store.AgentGroup) store.AgentGroup {
	in.LeadAgentID = cloneStringPtr(in.LeadAgentID)
	return in
}

func cloneStringPtr(in *string) *string {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
