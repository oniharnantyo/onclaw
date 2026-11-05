package groups

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The member turn's context packet (add-agent-groups design D3): the packet
// is the member turn's whole model input — the member session's log is never
// replayed into a group turn (design D2) — so the source builds it from the
// group transcript alone and commits the acceptance cursor after the turn.

// Packet shaping constants (design D3's bounded packet).
const (
	// recentWindow is the packet's bounded recent window: the last N
	// already-delivered transcript rows ride every delta packet for
	// immediate context (unread rows never duplicate them).
	recentWindow = 20
	// defaultHistoryLimit is the group_history tool's page size.
	defaultHistoryLimit = 50
	// maxHistoryLimit caps one group_history page.
	maxHistoryLimit = 200
	// transcriptScanPages bounds the backward transcript scans (the
	// dependent-result collection): 10 pages of transcriptReadPage rows.
	transcriptScanPages = 10
)

// checkpointFingerprint identifies one member session's compaction state
// (design D3's full-packet signal): a changed fingerprint — or one seen
// without a recorded baseline, as after a restart — rebuilds the full packet
// and resets the cursor. Group turns never replay the session log, so the
// signal is conservative belt-and-braces: a full packet guarantees the
// member holds the complete transcript context in its (possibly compacted)
// session.
type checkpointFingerprint struct {
	UpToSeq       int64
	CreatedAtNano int64
}

// transcriptMessage converts a store row to the packet's transcript shape:
// the display text decodes from the row's payload JSON.
func transcriptMessage(m store.AgentGroupMessage) agents.GroupTranscriptMessage {
	return agents.GroupTranscriptMessage{
		ID:            m.MessageID,
		Seq:           m.Seq,
		Kind:          m.Kind,
		AuthorKind:    m.AuthorKind,
		AuthorAgentID: deref(m.AuthorAgentID),
		Text:          payloadText(m),
	}
}

// transcriptMessages converts a row window.
func transcriptMessages(rows []store.AgentGroupMessage) []agents.GroupTranscriptMessage {
	out := make([]agents.GroupTranscriptMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, transcriptMessage(r))
	}
	return out
}

// buildPacket builds one member dispatch's context packet (design D3): the
// group's standing context, the member's responsibility line, the request,
// the unread transcript rows past the acceptance cursor (the full range when
// the member's session is new, compacted, or lost its cursor — the cursor is
// rebuilt), the dependent task results of the task's root chain, the bounded
// recent window of already-delivered rows, and the versioned summary. Also
// returns the member's session id (the dispatch target). The caller must
// hold the group's write lock.
func (s *Service) buildPacket(ctx context.Context, task *store.AgentGroupTask, agent store.Agent, requestText string, attachments []sessions.Attachment) (agents.GroupPacket, string, error) {
	g, err := s.deps.Store.Groups().GroupByID(ctx, task.GroupID)
	if err != nil {
		return agents.GroupPacket{}, "", fmt.Errorf("build packet: %w", err)
	}
	key := groupKey{GroupID: g.ID, AgentID: agent.ID}

	// The acceptance cursor. A missing row (or one without a session) means
	// the member never accepted anything: the full packet rebuilds it.
	full := false
	cursor, err := s.deps.Store.Groups().GetCursor(ctx, g.ID, agent.ID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return agents.GroupPacket{}, "", fmt.Errorf("build packet: %w", err)
		}
		cursor = &store.AgentGroupContext{}
		full = true
	}
	if cursor.SessionID == "" {
		full = true
	}

	// A cursor bound to a session that no longer exists — the member thread
	// was deleted from the rail while the group kept its acceptance row —
	// heals here instead of poisoning dispatch: SubmitGroup loads the
	// session and would fail forever, leaving the task queued and warning
	// on every boot. Re-mint the member session exactly like AddMember does
	// and reseed the cursor, so the packet, the copy-back pump, and the
	// turn all see a live session. The full packet rebuilds the member's
	// context on the new thread.
	if cursor.SessionID != "" {
		if _, err := s.deps.Sessions.Session(ctx, cursor.SessionID); err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return agents.GroupPacket{}, "", fmt.Errorf("build packet: check member session: %w", err)
			}
			sess, cerr := s.deps.Manager.CreateSession(ctx, agent.ID)
			if cerr != nil {
				return agents.GroupPacket{}, "", fmt.Errorf("build packet: re-mint member session: %w", cerr)
			}
			cursor = &store.AgentGroupContext{
				GroupID:   g.ID,
				AgentID:   agent.ID,
				SessionID: sess.ID,
			}
			if cerr := s.deps.Store.Groups().SetCursor(ctx, *cursor); cerr != nil {
				return agents.GroupPacket{}, "", fmt.Errorf("build packet: reseed member cursor: %w", cerr)
			}
			full = true
			slog.Warn("groups: member session missing; re-minted",
				"group", g.ID, "agent", agent.ID, "session", sess.ID)
		}
	}

	// The compaction signal (design D3): a checkpoint whose fingerprint
	// differs from the recorded one — or exists with no baseline recorded,
	// as after a restart — resets the cursor. The fingerprint records at
	// packet-build and cursor-commit time.
	if cursor.SessionID != "" {
		if cp, ok, err := s.deps.Sessions.Checkpoint(ctx, cursor.SessionID); err == nil && ok {
			fp := checkpointFingerprint{UpToSeq: cp.UpToSeq, CreatedAtNano: cp.CreatedAt.UnixNano()}
			s.mu.Lock()
			prev, seen := s.checkpoints[key]
			s.checkpoints[key] = fp
			s.mu.Unlock()
			if !seen || fp != prev {
				full = true
			}
		} else if err == nil && !ok {
			s.mu.Lock()
			delete(s.checkpoints, key)
			s.mu.Unlock()
		}
	}

	throughSeq, err := s.deps.Store.Groups().LastSeq(ctx, g.ID)
	if err != nil {
		return agents.GroupPacket{}, "", fmt.Errorf("build packet: %w", err)
	}

	// Unread rows: everything past the (possibly reset) cursor, oldest
	// first. A full packet covers the transcript from the beginning.
	fromSeq := int64(0)
	if !full {
		fromSeq = cursor.ThroughSeq
	}
	unread, err := s.readTranscriptRange(ctx, g.ID, fromSeq, throughSeq)
	if err != nil {
		return agents.GroupPacket{}, "", err
	}

	// The bounded recent window: already-delivered rows only, so the packet
	// never duplicates unread rows.
	var recent []store.AgentGroupMessage
	if !full && cursor.ThroughSeq > 0 {
		recent, err = s.deps.Store.Groups().ListMessages(ctx, g.ID, cursor.ThroughSeq+1, recentWindow)
		if err != nil {
			return agents.GroupPacket{}, "", fmt.Errorf("build packet: %w", err)
		}
	}

	results, err := s.collectTaskResults(ctx, g.ID, task.RootTaskID, throughSeq)
	if err != nil {
		return agents.GroupPacket{}, "", err
	}

	summary := &store.AgentGroupSummary{}
	if sum, err := s.deps.Store.Groups().GetSummary(ctx, g.ID); err == nil {
		summary = sum
	} else if !errors.Is(err, store.ErrNotFound) {
		return agents.GroupPacket{}, "", fmt.Errorf("build packet: %w", err)
	}

	title := g.Title
	if title == "" {
		title = g.Name
	}
	packet := agents.GroupPacket{
		GroupID:        g.ID,
		GroupTitle:     title,
		Instructions:   g.Instructions,
		Responsibility: strings.TrimSpace(agent.Description),
		Request:        requestText,
		Attachments:    attachments,
		Unread:         transcriptMessages(unread),
		TaskResults:    results,
		Recent:         transcriptMessages(recent),
		Summary:        summary.Text,
		SummaryVersion: int(summary.Version),
		ThroughSeq:     throughSeq,
		FullPacket:     full,
	}
	return packet, cursor.SessionID, nil
}

// readTranscriptRange reads the rows with Seq in (afterSeq, throughSeq],
// oldest first, paging down from the top. The result is empty (not nil) when
// the range is empty.
func (s *Service) readTranscriptRange(ctx context.Context, groupID string, afterSeq, throughSeq int64) ([]store.AgentGroupMessage, error) {
	var pages [][]store.AgentGroupMessage
	beforeSeq := throughSeq + 1
	for {
		page, err := s.deps.Store.Groups().ListMessages(ctx, groupID, beforeSeq, transcriptReadPage)
		if err != nil {
			return nil, fmt.Errorf("build packet: read transcript: %w", err)
		}
		if len(page) == 0 {
			break
		}
		pages = append(pages, page)
		if len(page) < transcriptReadPage || page[0].Seq <= afterSeq {
			break // the transcript's start, or the range is covered
		}
		beforeSeq = page[0].Seq
	}
	out := make([]store.AgentGroupMessage, 0)
	for i := len(pages) - 1; i >= 0; i-- {
		for _, r := range pages[i] {
			if r.Seq > afterSeq {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// collectTaskResults gathers the completed task results of one root chain
// (design D3's shared decisions): each completed task's result row, newest
// first scan bounded by transcriptScanPages. The result is empty (not nil)
// when the chain has none.
func (s *Service) collectTaskResults(ctx context.Context, groupID, rootTaskID string, throughSeq int64) ([]agents.GroupTaskResult, error) {
	tasks, err := s.deps.Store.Groups().ListTasksByGroup(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("build packet: %w", err)
	}
	completed := make(map[string]bool)
	for i := range tasks {
		if tasks[i].RootTaskID == rootTaskID && tasks[i].State == store.GroupTaskStateCompleted {
			completed[tasks[i].TaskID] = true
		}
	}
	if len(completed) == 0 {
		return nil, nil
	}
	out := make([]agents.GroupTaskResult, 0, len(completed))
	beforeSeq := throughSeq + 1
	for page := 0; page < transcriptScanPages; page++ {
		rows, err := s.deps.Store.Groups().ListMessages(ctx, groupID, beforeSeq, transcriptReadPage)
		if err != nil {
			return nil, fmt.Errorf("build packet: read results: %w", err)
		}
		for _, r := range rows {
			if r.Kind == store.GroupMessageKindResult && r.TaskID != nil && completed[*r.TaskID] {
				out = append(out, agents.GroupTaskResult{TaskID: *r.TaskID, Result: payloadText(r)})
				delete(completed, *r.TaskID)
			}
		}
		if len(rows) == 0 || len(completed) == 0 || len(rows) < transcriptReadPage {
			break
		}
		beforeSeq = rows[0].Seq
	}
	return out, nil
}

// recordCheckpointFingerprint records (or clears) the member session's
// compaction baseline after a packet or a cursor commit.
func (s *Service) recordCheckpointFingerprint(ctx context.Context, key groupKey, sessionID string) {
	if sessionID == "" {
		return
	}
	cp, ok, err := s.deps.Sessions.Checkpoint(ctx, sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil || !ok {
		delete(s.checkpoints, key)
		return
	}
	s.checkpoints[key] = checkpointFingerprint{UpToSeq: cp.UpToSeq, CreatedAtNano: cp.CreatedAt.UnixNano()}
}

// CommitCursor persists the member's acceptance cursor advance after a turn's
// terminal outcome (design D3) — the runtime calls it with the packet's
// ThroughSeq. A full packet rebuilds the cursor; a delta advances it; the
// write is the same upsert either way, carrying the group's current summary
// version (every packet incorporates the latest summary). The member session's
// compaction baseline re-records so the next packet reads the post-turn
// state.
func (s *Service) CommitCursor(ctx context.Context, groupID, agentID string, throughSeq int64) error {
	key := groupKey{GroupID: groupID, AgentID: agentID}
	cursor, err := s.deps.Store.Groups().GetCursor(ctx, groupID, agentID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("commit cursor: %w", err)
		}
		cursor = &store.AgentGroupContext{GroupID: groupID, AgentID: agentID}
	}
	if cursor.SessionID == "" {
		// A lost cursor row reconstructs the session id from the live pump.
		s.mu.Lock()
		if p := s.pumps[key]; p != nil {
			cursor.SessionID = p.sessionID
		}
		s.mu.Unlock()
	}
	version := int64(0)
	if sum, err := s.deps.Store.Groups().GetSummary(ctx, groupID); err == nil {
		version = sum.Version
	} else if !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("commit cursor: %w", err)
	}
	cursor.ThroughSeq = throughSeq
	cursor.SummaryVersion = version
	if err := s.deps.Store.Groups().SetCursor(ctx, *cursor); err != nil {
		return fmt.Errorf("commit cursor: %w", err)
	}
	s.recordCheckpointFingerprint(ctx, key, cursor.SessionID)
	return nil
}

// The in-channel group tools (fix-agent-group-member-tools D4, amending
// add-agent-groups design D4): a member's group turn is offered these —
// reading the group transcript, assigning a child task to a named member,
// and reporting a task result — appended BESIDE the turn's ordinary tool
// surface (the runner resolves that surface; see internal/agents/runner.go
// step 6). They replace only the session-history affordance: the packet is
// the turn's history, and group_history is the shared transcript's only
// reader.

// The group tools' names, as the model sees them.
const (
	// ToolGroupHistory reads the group transcript, bounded and paged.
	ToolGroupHistory = "group_history"
	// ToolGroupAssignTask creates a child task owned by a chosen member,
	// chained to the assigning task's root (budget-capped).
	ToolGroupAssignTask = "group_assign_task"
	// ToolGroupTaskResult reports a result that lands in the transcript and
	// completes the task.
	ToolGroupTaskResult = "group_task_result"
)

// GroupTurnTools returns the member turn's in-channel tools (the
// agents.GroupTurnSource seam). The delegation tool's description enumerates
// the roster — the model's only view of who it can assign to, since the
// packet carries no roster section.
func (s *Service) GroupTurnTools(ctx context.Context, groupID, agentID string) ([]tool.BaseTool, error) {
	history, err := s.groupHistoryTool(groupID)
	if err != nil {
		return nil, err
	}
	assign, err := s.groupAssignTaskTool(ctx, groupID, agentID)
	if err != nil {
		return nil, err
	}
	result, err := s.groupTaskResultTool(groupID, agentID)
	if err != nil {
		return nil, err
	}
	return []tool.BaseTool{history, assign, result}, nil
}

// groupHistoryTool builds the group_history invokable tool: a bounded,
// paged read of the group transcript, oldest first per page.
func (s *Service) groupHistoryTool(groupID string) (tool.BaseTool, error) {
	type input struct {
		// BeforeSeq pages backward: rows strictly below this sequence; 0
		// reads the newest page.
		BeforeSeq int64 `json:"before_seq,omitempty"`
		// Limit is the page size; the default is 50 rows, capped at 200.
		Limit int `json:"limit,omitempty"`
	}
	type output struct {
		// Transcript renders one "[seq] [author] text" line per row, oldest
		// first.
		Transcript string `json:"transcript"`
		// FirstSeq is the oldest row's sequence; pass it as before_seq to
		// page further back.
		FirstSeq int64 `json:"first_seq"`
		// HasMore reports older rows beyond the page.
		HasMore bool `json:"has_more"`
	}
	return utils.InferTool(ToolGroupHistory,
		"Read the agent group's shared transcript (every member's messages, coordinator assignment lines, and task results). Pages oldest-first; page backward with before_seq.",
		func(ctx context.Context, in input) (output, error) {
			limit := in.Limit
			if limit <= 0 {
				limit = defaultHistoryLimit
			}
			if limit > maxHistoryLimit {
				limit = maxHistoryLimit
			}
			l := s.groupLock(groupID)
			l.Lock()
			defer l.Unlock()

			rows, err := s.deps.Store.Groups().ListMessages(ctx, groupID, in.BeforeSeq, limit+1)
			if err != nil {
				return output{}, fmt.Errorf("group_history: %w", err)
			}
			// The window arrives ascending (newest limit+1 rows); trimming
			// keeps the NEWEST limit rows for the page.
			hasMore := len(rows) > limit
			if hasMore {
				rows = rows[len(rows)-limit:]
			}
			out := output{Transcript: renderTranscriptRows(rows), HasMore: hasMore}
			if len(rows) > 0 {
				out.FirstSeq = rows[0].Seq
			}
			return out, nil
		})
}

// groupAssignTaskTool builds the group_assign_task invokable tool: creates a
// child task owned by the chosen member, chained to the calling task's root
// (design D4), capped by the per-root child budget, and dispatches it
// (budget-guarded — the output's Queued reports a dispatch held by the group
// budget).
func (s *Service) groupAssignTaskTool(ctx context.Context, groupID, agentID string) (tool.BaseTool, error) {
	type input struct {
		// Member names the assignee by display name or agent id
		// (case-insensitive); it must be a current roster member.
		Member string `json:"member"`
		// Instruction is the work the assignee is asked to do.
		Instruction string `json:"instruction"`
		// ExpectedResult optionally states the acceptance criterion.
		ExpectedResult string `json:"expected_result,omitempty"`
	}
	type output struct {
		TaskID string `json:"task_id"`
		Member string `json:"member"`
		// Queued reports that the group's active-assignment budget holds
		// the dispatch; it starts when a running assignment finishes.
		Queued bool `json:"queued"`
	}

	// The roster rides the tool description: the model's only view of who
	// it can delegate to (the packet carries no roster section).
	desc := "Assign a child task to another member of the agent group. The task chains to your current task's root, the assignee receives it as their own group turn, and their reported result lands in the shared transcript. Members: " + s.rosterSummary(ctx, groupID)
	return utils.InferTool(ToolGroupAssignTask, desc, func(ctx context.Context, in input) (output, error) {
		l := s.groupLock(groupID)
		l.Lock()
		defer l.Unlock()

		calling, err := s.callingTask(ctx, groupID, agentID)
		if err != nil {
			return output{}, fmt.Errorf("group_assign_task: %w", err)
		}
		members, err := s.deps.Store.Groups().ListMembers(ctx, groupID)
		if err != nil {
			return output{}, fmt.Errorf("group_assign_task: %w", err)
		}
		ownerID, ownerName, err := s.resolveMemberRef(ctx, members, in.Member)
		if err != nil {
			return output{}, fmt.Errorf("group_assign_task: %w", err)
		}

		// The per-root child budget (design D6): at most
		// maxAutoChildrenPerRoot automatic assignments per root request.
		tasks, err := s.deps.Store.Groups().ListTasksByGroup(ctx, groupID)
		if err != nil {
			return output{}, fmt.Errorf("group_assign_task: %w", err)
		}
		children := 0
		for i := range tasks {
			if tasks[i].RootTaskID == calling.RootTaskID && tasks[i].TaskID != calling.RootTaskID {
				children++
			}
		}
		if children >= maxAutoChildrenPerRoot {
			return output{}, fmt.Errorf("group_assign_task: the root request already has %d child assignments (budget %d)", children, maxAutoChildrenPerRoot)
		}

		taskID, err := newID()
		if err != nil {
			return output{}, fmt.Errorf("group_assign_task: %w", err)
		}
		child := &store.AgentGroupTask{
			TaskID:           taskID,
			GroupID:          groupID,
			ParentTaskID:     &calling.TaskID,
			RootTaskID:       calling.RootTaskID,
			OwnerAgentID:     &ownerID,
			RequestMessageID: calling.RequestMessageID,
			Instruction:      in.Instruction,
			ExpectedResult:   in.ExpectedResult,
			State:            store.GroupTaskStateQueued,
			Revision:         1,
		}
		if err := s.deps.Store.Groups().CreateTask(ctx, child); err != nil {
			return output{}, fmt.Errorf("group_assign_task: %w", err)
		}
		s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: groupID, TaskID: child.TaskID})
		// Delegation assignments carry the coordinator assigned line
		// (design D4).
		if _, err := s.appendAssignedLine(ctx, groupID, child.TaskID, "Assigned to "+ownerName); err != nil {
			return output{}, err
		}
		dispatched, err := s.dispatchTask(ctx, child, "", nil, false)
		if err != nil {
			return output{}, fmt.Errorf("group_assign_task: %w", err)
		}
		return output{TaskID: child.TaskID, Member: ownerName, Queued: !dispatched}, nil
	})
}

// groupTaskResultTool builds the group_task_result invokable tool: the
// reported result lands in the transcript as a task result row authored by
// the member and completes the task (revision-guarded).
func (s *Service) groupTaskResultTool(groupID, agentID string) (tool.BaseTool, error) {
	type input struct {
		// Result is the work's outcome, reported into the shared transcript.
		Result string `json:"result"`
		// TaskID optionally names which task the result completes; it
		// defaults to the calling task.
		TaskID string `json:"task_id,omitempty"`
	}
	type output struct {
		TaskID    string `json:"task_id"`
		Completed bool   `json:"completed"`
	}
	return utils.InferTool(ToolGroupTaskResult,
		"Report the result of your current group task (or the task named by task_id). The result lands in the shared transcript and completes the task.",
		func(ctx context.Context, in input) (output, error) {
			l := s.groupLock(groupID)
			l.Lock()
			defer l.Unlock()

			targetID := in.TaskID
			if targetID == "" {
				calling, err := s.callingTask(ctx, groupID, agentID)
				if err != nil {
					return output{}, fmt.Errorf("group_task_result: %w", err)
				}
				targetID = calling.TaskID
			}
			t, err := s.deps.Store.Groups().TaskByID(ctx, targetID)
			if err != nil {
				return output{}, fmt.Errorf("group_task_result: %w", err)
			}
			if t.GroupID != groupID {
				return output{}, fmt.Errorf("group_task_result: task %s is not in this group", targetID)
			}
			if !taskOpen(t) {
				return output{}, fmt.Errorf("group_task_result: task %s is already %s", targetID, t.State)
			}
			if _, err := s.appendResultRow(ctx, groupID, t.TaskID, agentID, in.Result); err != nil {
				return output{}, err
			}
			t.State = store.GroupTaskStateCompleted
			t.Revision++
			if err := s.deps.Store.Groups().UpdateTask(ctx, t); err != nil {
				return output{}, fmt.Errorf("group_task_result: %w", err)
			}
			s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: groupID, TaskID: t.TaskID})
			// The reported completion may close its parent's dependency wait
			// (design D3): a waiting parent whose last open child just
			// completed re-queues before the drain dispatches it under the
			// budget.
			s.resumeWaitingParent(ctx, groupID, t.TaskID)
			s.drainGroupLocked(ctx, groupID)
			return output{TaskID: t.TaskID, Completed: true}, nil
		})
}

// appendResultRow writes the transcript's result row, authored by the
// reporting member.
func (s *Service) appendResultRow(ctx context.Context, groupID, taskID, agentID, result string) (string, error) {
	payload, err := makePayload(result, nil)
	if err != nil {
		return "", fmt.Errorf("append result row: %w", err)
	}
	msg := &store.AgentGroupMessage{
		GroupID:       groupID,
		Kind:          store.GroupMessageKindResult,
		AuthorKind:    store.GroupAuthorKindAgent,
		AuthorAgentID: &agentID,
		TaskID:        &taskID,
		Payload:       payload,
	}
	if err := s.deps.Store.Groups().AppendMessage(ctx, msg); err != nil {
		return "", fmt.Errorf("append result row: %w", err)
	}
	s.fireGroupEvent(GroupEvent{Kind: GroupEventMessageAppended, GroupID: groupID, MessageID: msg.MessageID, Seq: msg.Seq, Message: msg})
	return msg.MessageID, nil
}

// resolveMemberRef resolves an assign-tool member reference (display name or
// agent id, case-insensitive) against the roster.
func (s *Service) resolveMemberRef(ctx context.Context, members []store.AgentGroupMember, ref string) (agentID, name string, err error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if ref == "" {
		return "", "", fmt.Errorf("member is required")
	}
	for _, m := range members {
		if m.AgentID == ref {
			agent, aerr := s.deps.Store.Agents().ByID(ctx, m.AgentID)
			if aerr != nil {
				return "", "", fmt.Errorf("resolve member: %w", aerr)
			}
			return m.AgentID, agent.Name, nil
		}
	}
	for _, m := range members {
		agent, aerr := s.deps.Store.Agents().ByID(ctx, m.AgentID)
		if aerr != nil {
			return "", "", fmt.Errorf("resolve member: %w", aerr)
		}
		if strings.ToLower(agent.Name) == ref {
			return m.AgentID, agent.Name, nil
		}
	}
	return "", "", fmt.Errorf("no roster member matches %q", ref)
}

// rosterSummary renders the roster for the delegation tool's description:
// "Name (agent id): first description line", comma-separated.
func (s *Service) rosterSummary(ctx context.Context, groupID string) string {
	members, err := s.deps.Store.Groups().ListMembers(ctx, groupID)
	if err != nil {
		return "unavailable"
	}
	parts := make([]string, 0, len(members))
	for _, m := range members {
		agent, err := s.deps.Store.Agents().ByID(ctx, m.AgentID)
		if err != nil {
			continue
		}
		line := agent.Description
		if i := strings.IndexAny(line, "\r\n"); i >= 0 {
			line = line[:i]
		}
		if line = strings.TrimSpace(line); line != "" {
			line = ": " + line
		}
		if len(line) > 200 {
			line = line[:200]
		}
		parts = append(parts, fmt.Sprintf("%s (agent id %s)%s", agent.Name, agent.ID, line))
	}
	if len(parts) == 0 {
		return "unavailable"
	}
	return strings.Join(parts, "; ")
}
