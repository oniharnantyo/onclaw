package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/oniharnantyo/buddies/internal/store"
)

type agentGroupStore struct{ e executor }

const agentGroupColumns = `id, name, title, instructions, lead_agent_id, archived, revision, created_at, updated_at`

const agentGroupMemberColumns = `group_id, agent_id, added_at`

const agentGroupMessageColumns = `group_id, message_id, seq, kind, author_kind, author_agent_id, task_id, payload, occurred_at`

const agentGroupContextColumns = `group_id, agent_id, session_id, through_seq, summary_version`

const agentGroupTaskColumns = `task_id, group_id, parent_task_id, root_task_id, owner_agent_id, request_message_id, instruction, expected_result, state, stop_reason, revision, assignment_count, error`

const agentGroupSummaryColumns = `group_id, version, through_seq, text`

// appendMessageSQL inserts one transcript row. The sequence is allocated
// inside the statement (MAX(seq)+1 for the group): under SQLite's
// single-writer rule two concurrent appends serialize, so sequences are
// gapless per group and strictly monotonic. ?NNN placeholders let group_id
// appear once in the column list and again in the seq subquery. A duplicate
// (group_id, message_id) violates the UNIQUE key and surfaces as
// store.ErrDuplicate.
const appendMessageSQL = `
INSERT INTO agent_group_messages (group_id, message_id, seq, kind, author_kind, author_agent_id, task_id, payload, occurred_at)
VALUES (?1, ?2,
        (SELECT COALESCE(MAX(seq), 0) + 1 FROM agent_group_messages WHERE group_id = ?1),
        ?3, ?4, ?5, ?6, ?7, ?8)`

// Group CRUD

// CreateGroup stores a new group, assigning ID and stamping the timestamps
// when empty.
func (s *agentGroupStore) CreateGroup(ctx context.Context, g *store.AgentGroup) error {
	if g.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("create group: %w", err)
		}
		g.ID = id
	}
	stampCreated(&g.CreatedAt, &g.UpdatedAt)

	_, err := s.e.ExecContext(ctx, `
INSERT INTO agent_groups (`+agentGroupColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.ID, g.Name, g.Title, g.Instructions, nullAgentID(g.LeadAgentID),
		boolInt(g.Archived), g.Revision, formatTime(g.CreatedAt), formatTime(g.UpdatedAt),
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("create group %s: %w", g.ID, store.ErrDuplicate)
		}
		return fmt.Errorf("create group %s: %w", g.ID, err)
	}
	return nil
}

// GroupByID returns the group row with the given ID.
func (s *agentGroupStore) GroupByID(ctx context.Context, id string) (*store.AgentGroup, error) {
	row := s.e.QueryRowContext(ctx, `SELECT `+agentGroupColumns+` FROM agent_groups WHERE id = ?`, id)
	g, err := scanAgentGroup(row)
	if err != nil {
		return nil, readErr("get group", err)
	}
	return g, nil
}

// UpdateGroup rewrites the group's editable columns, keeping the original
// created_at.
func (s *agentGroupStore) UpdateGroup(ctx context.Context, g *store.AgentGroup) error {
	if g.UpdatedAt.IsZero() {
		g.UpdatedAt = nowUTC()
	}
	res, err := s.e.ExecContext(ctx, `
UPDATE agent_groups SET
name = ?, title = ?, instructions = ?, lead_agent_id = ?, archived = ?,
revision = ?, updated_at = ?
WHERE id = ?`,
		g.Name, g.Title, g.Instructions, nullAgentID(g.LeadAgentID),
		boolInt(g.Archived), g.Revision, formatTime(g.UpdatedAt), g.ID,
	)
	if err != nil {
		return fmt.Errorf("update group %s: %w", g.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update group %s: %w", g.ID, store.ErrNotFound)
	}
	return nil
}

// ListGroups returns every group ordered by creation time, then id.
func (s *agentGroupStore) ListGroups(ctx context.Context) ([]store.AgentGroup, error) {
	rows, err := s.e.QueryContext(ctx, `SELECT `+agentGroupColumns+` FROM agent_groups ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()

	out := make([]store.AgentGroup, 0)
	for rows.Next() {
		g, err := scanAgentGroup(rows)
		if err != nil {
			return nil, fmt.Errorf("list groups: %w", err)
		}
		out = append(out, *g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	return out, nil
}

// DeleteGroup removes the group; the foreign-key cascades remove its
// members, transcript, cursors, tasks, and summary with it.
func (s *agentGroupStore) DeleteGroup(ctx context.Context, id string) error {
	res, err := s.e.ExecContext(ctx, `DELETE FROM agent_groups WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete group %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete group %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// Members

// AddMember adds one roster row; a duplicate (group_id, agent_id) is the
// ErrDuplicate sentinel (PK group_id, agent_id).
func (s *agentGroupStore) AddMember(ctx context.Context, m store.AgentGroupMember) error {
	if m.AddedAt.IsZero() {
		m.AddedAt = nowUTC()
	}
	_, err := s.e.ExecContext(ctx, `
INSERT INTO agent_group_members (`+agentGroupMemberColumns+`)
VALUES (?, ?, ?)`,
		m.GroupID, m.AgentID, formatTime(m.AddedAt),
	)
	if err != nil {
		// (group_id, agent_id) is the PRIMARY KEY: one roster row per
		// member.
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("add group member %s/%s: %w", m.GroupID, m.AgentID, store.ErrDuplicate)
		}
		return fmt.Errorf("add group member %s/%s: %w", m.GroupID, m.AgentID, err)
	}
	return nil
}

// RemoveMember drops one roster row.
func (s *agentGroupStore) RemoveMember(ctx context.Context, groupID, agentID string) error {
	res, err := s.e.ExecContext(ctx,
		`DELETE FROM agent_group_members WHERE group_id = ? AND agent_id = ?`, groupID, agentID)
	if err != nil {
		return fmt.Errorf("remove group member %s/%s: %w", groupID, agentID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("remove group member %s/%s: %w", groupID, agentID, store.ErrNotFound)
	}
	return nil
}

// ListMembers returns the group's roster ordered by added time, then agent
// id.
func (s *agentGroupStore) ListMembers(ctx context.Context, groupID string) ([]store.AgentGroupMember, error) {
	rows, err := s.e.QueryContext(ctx, `
SELECT `+agentGroupMemberColumns+` FROM agent_group_members
WHERE group_id = ? ORDER BY added_at, agent_id`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	defer rows.Close()

	out := make([]store.AgentGroupMember, 0)
	for rows.Next() {
		var m store.AgentGroupMember
		var addedAt string
		if err := rows.Scan(&m.GroupID, &m.AgentID, &addedAt); err != nil {
			return nil, fmt.Errorf("list group members: %w", err)
		}
		if m.AddedAt, err = parseTime(addedAt); err != nil {
			return nil, fmt.Errorf("list group members: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	return out, nil
}

// Messages

// AppendMessage appends one transcript row, allocating the next per-group
// Seq and stamping OccurredAt when empty; MessageID is assigned when empty.
// A MessageID already stored for the group is ErrDuplicate.
func (s *agentGroupStore) AppendMessage(ctx context.Context, m *store.AgentGroupMessage) error {
	if m.MessageID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("append group message: %w", err)
		}
		m.MessageID = id
	}
	if m.OccurredAt.IsZero() {
		m.OccurredAt = nowUTC()
	}
	if _, err := s.e.ExecContext(ctx, appendMessageSQL,
		m.GroupID, m.MessageID, m.Kind, m.AuthorKind, nullAgentID(m.AuthorAgentID),
		nullAgentID(m.TaskID), m.Payload, formatTime(m.OccurredAt),
	); err != nil {
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("append group message %s/%s: %w", m.GroupID, m.MessageID, store.ErrDuplicate)
		}
		return fmt.Errorf("append group message %s/%s: %w", m.GroupID, m.MessageID, err)
	}
	if err := s.e.QueryRowContext(ctx, `
SELECT seq FROM agent_group_messages WHERE group_id = ? AND message_id = ?`,
		m.GroupID, m.MessageID).Scan(&m.Seq); err != nil {
		return fmt.Errorf("append group message %s/%s: %w", m.GroupID, m.MessageID, err)
	}
	return nil
}

// UpdateMessage rewrites one transcript row in place, keeping its Seq —
// the regenerate path (design D7).
func (s *agentGroupStore) UpdateMessage(ctx context.Context, m *store.AgentGroupMessage) error {
	if m.OccurredAt.IsZero() {
		m.OccurredAt = nowUTC()
	}
	res, err := s.e.ExecContext(ctx, `
UPDATE agent_group_messages SET
kind = ?, author_kind = ?, author_agent_id = ?, task_id = ?, payload = ?,
occurred_at = ?
WHERE group_id = ? AND message_id = ?`,
		m.Kind, m.AuthorKind, nullAgentID(m.AuthorAgentID), nullAgentID(m.TaskID),
		m.Payload, formatTime(m.OccurredAt), m.GroupID, m.MessageID,
	)
	if err != nil {
		return fmt.Errorf("update group message %s/%s: %w", m.GroupID, m.MessageID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update group message %s/%s: %w", m.GroupID, m.MessageID, store.ErrNotFound)
	}
	if err := s.e.QueryRowContext(ctx, `
SELECT seq FROM agent_group_messages WHERE group_id = ? AND message_id = ?`,
		m.GroupID, m.MessageID).Scan(&m.Seq); err != nil {
		return fmt.Errorf("update group message %s/%s: %w", m.GroupID, m.MessageID, err)
	}
	return nil
}

// ListMessages returns one window of the group's transcript, replayed in
// ascending Seq order.
func (s *agentGroupStore) ListMessages(ctx context.Context, groupID string, beforeSeq int64, limit int) ([]store.AgentGroupMessage, error) {
	query := `SELECT ` + agentGroupMessageColumns + ` FROM agent_group_messages WHERE group_id = ?`
	args := []any{groupID}
	if beforeSeq > 0 {
		query += ` AND seq < ?`
		args = append(args, beforeSeq)
	}
	// Newest window first, replayed in ascending order.
	query += ` ORDER BY seq DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.e.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list group messages: %w", err)
	}
	defer rows.Close()

	out := make([]store.AgentGroupMessage, 0)
	for rows.Next() {
		m, err := scanAgentGroupMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("list group messages: %w", err)
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list group messages: %w", err)
	}
	// Reverse into replay (ascending) order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// LastSeq returns the group's highest allocated transcript Seq — 0 for a
// group without messages.
func (s *agentGroupStore) LastSeq(ctx context.Context, groupID string) (int64, error) {
	var seq int64
	if err := s.e.QueryRowContext(ctx, `
SELECT COALESCE(MAX(seq), 0) FROM agent_group_messages WHERE group_id = ?`,
		groupID).Scan(&seq); err != nil {
		return 0, fmt.Errorf("last group seq %s: %w", groupID, err)
	}
	return seq, nil
}

// Cursors

// GetCursor returns one member's acceptance cursor.
func (s *agentGroupStore) GetCursor(ctx context.Context, groupID, agentID string) (*store.AgentGroupContext, error) {
	row := s.e.QueryRowContext(ctx, `
SELECT `+agentGroupContextColumns+` FROM agent_group_contexts
WHERE group_id = ? AND agent_id = ?`, groupID, agentID)
	var c store.AgentGroupContext
	if err := row.Scan(&c.GroupID, &c.AgentID, &c.SessionID, &c.ThroughSeq, &c.SummaryVersion); err != nil {
		return nil, readErr("get group cursor", err)
	}
	return &c, nil
}

// SetCursor upserts one member's acceptance cursor.
func (s *agentGroupStore) SetCursor(ctx context.Context, c store.AgentGroupContext) error {
	_, err := s.e.ExecContext(ctx, `
INSERT INTO agent_group_contexts (`+agentGroupContextColumns+`)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(group_id, agent_id) DO UPDATE SET
session_id = excluded.session_id, through_seq = excluded.through_seq,
summary_version = excluded.summary_version`,
		c.GroupID, c.AgentID, c.SessionID, c.ThroughSeq, c.SummaryVersion,
	)
	if err != nil {
		return fmt.Errorf("set group cursor %s/%s: %w", c.GroupID, c.AgentID, err)
	}
	return nil
}

// Tasks

// CreateTask stores a new task, assigning TaskID when empty; a TaskID
// already stored is ErrDuplicate.
func (s *agentGroupStore) CreateTask(ctx context.Context, t *store.AgentGroupTask) error {
	if t.TaskID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("create group task: %w", err)
		}
		t.TaskID = id
	}
	_, err := s.e.ExecContext(ctx, `
INSERT INTO agent_group_tasks (`+agentGroupTaskColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TaskID, t.GroupID, nullAgentID(t.ParentTaskID), t.RootTaskID,
		nullAgentID(t.OwnerAgentID), t.RequestMessageID, t.Instruction,
		t.ExpectedResult, t.State, nullAgentID(t.StopReason), t.Revision,
		t.AssignmentCount, nullAgentID(t.Error),
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("create group task %s: %w", t.TaskID, store.ErrDuplicate)
		}
		return fmt.Errorf("create group task %s: %w", t.TaskID, err)
	}
	return nil
}

// TaskByID returns the task row with the given ID.
func (s *agentGroupStore) TaskByID(ctx context.Context, taskID string) (*store.AgentGroupTask, error) {
	row := s.e.QueryRowContext(ctx, `SELECT `+agentGroupTaskColumns+` FROM agent_group_tasks WHERE task_id = ?`, taskID)
	t, err := scanAgentGroupTask(row)
	if err != nil {
		return nil, readErr("get group task", err)
	}
	return t, nil
}

// UpdateTask rewrites the task's mutable columns, keeping the chain fields
// (parent, root, request message).
func (s *agentGroupStore) UpdateTask(ctx context.Context, t *store.AgentGroupTask) error {
	res, err := s.e.ExecContext(ctx, `
UPDATE agent_group_tasks SET
owner_agent_id = ?, state = ?, stop_reason = ?, revision = ?,
assignment_count = ?, error = ?
WHERE task_id = ?`,
		nullAgentID(t.OwnerAgentID), t.State, nullAgentID(t.StopReason),
		t.Revision, t.AssignmentCount, nullAgentID(t.Error), t.TaskID,
	)
	if err != nil {
		return fmt.Errorf("update group task %s: %w", t.TaskID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update group task %s: %w", t.TaskID, store.ErrNotFound)
	}
	return nil
}

// ListTasksByGroup returns the group's tasks ordered by task id.
func (s *agentGroupStore) ListTasksByGroup(ctx context.Context, groupID string) ([]store.AgentGroupTask, error) {
	rows, err := s.e.QueryContext(ctx, `
SELECT `+agentGroupTaskColumns+` FROM agent_group_tasks
WHERE group_id = ? ORDER BY task_id`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group tasks: %w", err)
	}
	return scanAgentGroupTasks(rows, "list group tasks")
}

// ListOpenTasks returns every non-terminal task across all groups — the
// restart drain's worklist.
func (s *agentGroupStore) ListOpenTasks(ctx context.Context) ([]store.AgentGroupTask, error) {
	placeholders := strings.Repeat("?, ", len(store.GroupTaskOpenStates))
	placeholders = placeholders[:len(placeholders)-2]
	args := make([]any, len(store.GroupTaskOpenStates))
	for i, state := range store.GroupTaskOpenStates {
		args[i] = state
	}
	rows, err := s.e.QueryContext(ctx, `
SELECT `+agentGroupTaskColumns+` FROM agent_group_tasks
WHERE state IN (`+placeholders+`) ORDER BY task_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list open group tasks: %w", err)
	}
	return scanAgentGroupTasks(rows, "list open group tasks")
}

// Summaries

// GetSummary returns the group's transcript summary.
func (s *agentGroupStore) GetSummary(ctx context.Context, groupID string) (*store.AgentGroupSummary, error) {
	row := s.e.QueryRowContext(ctx, `SELECT `+agentGroupSummaryColumns+` FROM agent_group_summaries WHERE group_id = ?`, groupID)
	var sum store.AgentGroupSummary
	if err := row.Scan(&sum.GroupID, &sum.Version, &sum.ThroughSeq, &sum.Text); err != nil {
		return nil, readErr("get group summary", err)
	}
	return &sum, nil
}

// SaveSummary upserts the group's transcript summary; the caller owns the
// versioning.
func (s *agentGroupStore) SaveSummary(ctx context.Context, sum *store.AgentGroupSummary) error {
	_, err := s.e.ExecContext(ctx, `
INSERT INTO agent_group_summaries (`+agentGroupSummaryColumns+`)
VALUES (?, ?, ?, ?)
ON CONFLICT(group_id) DO UPDATE SET
version = excluded.version, through_seq = excluded.through_seq, text = excluded.text`,
		sum.GroupID, sum.Version, sum.ThroughSeq, sum.Text,
	)
	if err != nil {
		return fmt.Errorf("save group summary %s: %w", sum.GroupID, err)
	}
	return nil
}

// scan helpers

// scanAgentGroup scans one agent_groups row from a single- or multi-row
// result.
func scanAgentGroup(row scanner) (*store.AgentGroup, error) {
	var g store.AgentGroup
	var leadAgentID sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&g.ID, &g.Name, &g.Title, &g.Instructions, &leadAgentID,
		&g.Archived, &g.Revision, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if leadAgentID.Valid {
		g.LeadAgentID = &leadAgentID.String
	}
	var err error
	if g.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if g.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &g, nil
}

// scanAgentGroupMessage scans one agent_group_messages row from a single-
// or multi-row result.
func scanAgentGroupMessage(row scanner) (*store.AgentGroupMessage, error) {
	var m store.AgentGroupMessage
	var authorAgentID, taskID sql.NullString
	var occurredAt string
	if err := row.Scan(&m.GroupID, &m.MessageID, &m.Seq, &m.Kind, &m.AuthorKind,
		&authorAgentID, &taskID, &m.Payload, &occurredAt); err != nil {
		return nil, err
	}
	if authorAgentID.Valid {
		m.AuthorAgentID = &authorAgentID.String
	}
	if taskID.Valid {
		m.TaskID = &taskID.String
	}
	var err error
	if m.OccurredAt, err = parseTime(occurredAt); err != nil {
		return nil, err
	}
	return &m, nil
}

// scanAgentGroupTasks drains a task result set, closing it.
func scanAgentGroupTasks(rows *sql.Rows, op string) ([]store.AgentGroupTask, error) {
	defer rows.Close()

	out := make([]store.AgentGroupTask, 0)
	for rows.Next() {
		t, err := scanAgentGroupTask(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return out, nil
}

// scanAgentGroupTask scans one agent_group_tasks row from a single- or
// multi-row result.
func scanAgentGroupTask(row scanner) (*store.AgentGroupTask, error) {
	var t store.AgentGroupTask
	var parentTaskID, ownerAgentID, stopReason, taskErr sql.NullString
	if err := row.Scan(&t.TaskID, &t.GroupID, &parentTaskID, &t.RootTaskID,
		&ownerAgentID, &t.RequestMessageID, &t.Instruction, &t.ExpectedResult,
		&t.State, &stopReason, &t.Revision, &t.AssignmentCount, &taskErr); err != nil {
		return nil, err
	}
	if parentTaskID.Valid {
		t.ParentTaskID = &parentTaskID.String
	}
	if ownerAgentID.Valid {
		t.OwnerAgentID = &ownerAgentID.String
	}
	if stopReason.Valid {
		t.StopReason = &stopReason.String
	}
	if taskErr.Valid {
		t.Error = &taskErr.String
	}
	return &t, nil
}
