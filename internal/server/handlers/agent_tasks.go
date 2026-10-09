package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/adk"
	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// taskCompletedSessionEventKind is the durable session-event kind a task's
// terminal notice persists under (agents.sessionEventKindTaskCompleted —
// unexported there, so the read side matches the literal the runner writes;
// tasklist.go's doc comment pins the same value).
const taskCompletedSessionEventKind = "x.task_completed"

// taskEventSessionKinds are the only session-event kinds the merged task list
// and the output replay's persisted evidence read: the eino task-created
// event and the task-completed chip. The kinds filter keeps the load scoped
// no matter how long the session's transcript is.
func taskEventSessionKinds() []string {
	return []string{
		string(bgtask.SessionEventTaskCreated),
		taskCompletedSessionEventKind,
	}
}

// ListSessionTasks lists a session's background tasks — the live run's task
// space merged with the session's persisted task-created and task-completed
// session events (add-subagent-runs-surface D3), so tasks from earlier turns
// and from ended runs appear with their persisted outcome. Read rides the
// session-events access shape: agents.read at the route, workspace scoping
// through the resolved agent and the workspace-scoped stores. An unknown
// session (no live run AND no persisted events) is not-found, mirroring
// CancelRun's existence probe — a live run proves existence even with no
// persisted events.
func (h *agentHandlers) ListSessionTasks(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return
	}
	sessionID := c.Param("session")

	live := h.taskSurface.LiveTasks(ws.ID, agent.ID, sessionID)
	_, liveRun := h.taskSurface.TaskSpace(ws.ID, agent.ID, sessionID)

	created, completed, err := h.persistedTaskRecords(c.Request.Context(), ws.ID, sessionID)
	if err != nil {
		RespondError(c, err)
		return
	}

	if !liveRun && len(created) == 0 && len(completed) == 0 {
		// No task evidence anywhere: the session-existence probe decides
		// unknown-session vs. a known session with no tasks (the CancelRun
		// probe — a session with no persisted events is indistinguishable
		// from an unknown one; both are not-found).
		if err := h.probeSessionExists(c.Request.Context(), ws.ID, sessionID); err != nil {
			RespondError(c, err)
			return
		}
	}

	RespondOK(c, gin.H{"tasks": agents.MergeSessionTasks(live, created, completed)})
}

// taskOutputRecord is one trajectory row of the output response: the pinned
// TaskProgressRecord shape plus its per-task sequence. Embedding flattens the
// record's fields so the wire keys stay sequence,type,text?,name?,args?,result?.
type taskOutputRecord struct {
	agents.TaskProgressRecord
	Sequence int `json:"sequence"`
}

// taskOutputResponse is the task trajectory read's wire shape (pinned): the
// addressed task, its records in append order, and the terminal notice —
// null while the task has not reached a final status.
type taskOutputResponse struct {
	TaskID   string                       `json:"task_id"`
	Records  []taskOutputRecord           `json:"records"`
	Terminal *agents.TaskCompletedPayload `json:"terminal"`
}

// GetSessionTaskOutput reads one task's trajectory (add-subagent-runs-surface,
// task trajectory read). A running task returns its live progress records so
// far (the optional `after` sequence cursor); an ended task replays its
// persisted output file, one record per decoded line/chunk. When no live
// trajectory and no replayable file exist the endpoint returns a clear
// not-found naming the task — never an empty trajectory masquerading (an
// unknown task, a restart-lost created event, and a pruned output file all
// read as not-found).
func (h *agentHandlers) GetSessionTaskOutput(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return
	}
	sessionID := c.Param("session")
	taskID := c.Param("task")

	after := 0
	if s := c.Query("after"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			RespondError(c, fmt.Errorf("%w: after must be a non-negative integer", domain.ErrInvalid))
			return
		}
		after = n
	}

	// Live first: a task known to the live run's store reads from the task
	// space (read-your-writes past the cursor). The terminal frame rides the
	// frames when the task went terminal in the live space.
	frames, ok, err := h.taskSurface.TaskProgressRecords(c.Request.Context(), ws.ID, agent.ID, sessionID, taskID, after)
	if err != nil {
		RespondError(c, err)
		return
	}
	if ok {
		RespondOK(c, taskOutputFromFrames(taskID, frames))
		return
	}

	// No live trajectory: the persisted evidence decides. The completion chip
	// carries the output location the replay reads; the created event proves
	// the task existed even when it never went terminal (restart-lost).
	created, completed, err := h.persistedTaskRecords(c.Request.Context(), ws.ID, sessionID)
	if err != nil {
		RespondError(c, err)
		return
	}

	known := false
	var terminal *agents.TaskCompletedPayload
	for _, rec := range created {
		if rec.TaskID == taskID {
			known = true
			break
		}
	}
	for i := range completed {
		if completed[i].TaskID == taskID {
			known = true
			payload := completed[i]
			terminal = &payload
			break
		}
	}
	if !known {
		if err := h.probeSessionExists(c.Request.Context(), ws.ID, sessionID); err != nil {
			RespondError(c, err)
			return
		}
		RespondError(c, fmt.Errorf("%w: task %q not found in session", domain.ErrNotFound, taskID))
		return
	}
	if terminal == nil || terminal.OutputPath == "" {
		// The task existed but never recorded an output location (a
		// created-without-completion event, or a lane that reserved no file):
		// there is no trajectory to replay, and an empty one would lie.
		RespondError(c, fmt.Errorf("%w: task %q has no recorded output trajectory", domain.ErrNotFound, taskID))
		return
	}

	data, err := os.ReadFile(h.taskOutputFilePath(ws, agent, terminal.OutputPath))
	if err != nil {
		// Pruned/missing file (design risk "Ended tasks' files may be pruned
		// while session events persist"): a clear not-found naming the task.
		// The list still shows the task with its persisted outcome.
		RespondError(c, fmt.Errorf("%w: task %q output file is no longer available", domain.ErrNotFound, taskID))
		return
	}

	RespondOK(c, taskOutputResponse{
		TaskID:   taskID,
		Records:  replayTaskOutputRecords(terminal.Kind, data),
		Terminal: terminal,
	})
}

// CancelSessionTask cancels one running background task through the run's
// task space (add-subagent-runs-surface, task cancel) — the same control
// surface the model's task_stop tool uses. The session-ownership rule mirrors
// CancelRun exactly (fix-role-permission-audit D3): the session's owning
// member or an agents.write holder; everyone else is 403 — checked before the
// task-space probe. Only a running task accepts the request; anything else
// (unknown id, ended run, already-terminal task) is the 409 conflict. The
// cancel is an acceptance: the task reaches the canceled outcome through the
// normal completion path afterwards.
func (h *agentHandlers) CancelSessionTask(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return
	}
	sessionID := c.Param("session")
	taskID := c.Param("task")
	if sessionID == "" || taskID == "" {
		RespondError(c, fmt.Errorf("%w: session and task path segments are required", domain.ErrInvalid))
		return
	}

	allowed, _, err := h.sessionManageAllowed(c, ws, agent.ID, sessionID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if !allowed {
		AbortForbidden(c, "cancelling another member's task requires the "+domain.AgentsWrite+" permission")
		return
	}

	if _, err := h.taskSurface.CancelTask(c.Request.Context(), ws.ID, agent.ID, sessionID, taskID); err != nil {
		if errors.Is(err, agents.ErrTaskNotRunning) {
			RespondError(c, fmt.Errorf("%w: task %q is not running", domain.ErrConflict, taskID))
			return
		}
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"cancelled": true})
}

// StreamSessionTasks serves the session tasks progress stream
// (add-subagent-runs-surface D2): task-keyed TaskProgressFrame data events for
// the session's running tasks, one terminal frame per task, ending when the
// run ends (the frame channel closes). The auth/stream semantics mirror the
// session-events live stream exactly — same SSE response headers, same
// client-disconnect handling, [DONE] sentinel on every end. The endpoint is
// live-only and reads ONLY the task-progress channel: it never touches the
// transcript tap, so a session's transcript events and stream are untouched
// (spec: a client that never opens this stream observes no behavioral
// difference). With no live run the stream ends immediately after the
// existence probe — a known session gets a clean empty stream, an unknown
// session is not-found before any SSE byte is written.
func (h *agentHandlers) StreamSessionTasks(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return
	}
	sessionID := c.Param("session")

	// Subscribe before the existence probe (the ListSessionEvents ordering):
	// a run going live in between is caught by the subscription, not missed.
	subID, frames, subscribed := h.taskSurface.SubscribeTaskProgress(ws.ID, agent.ID, sessionID)
	if subscribed {
		// Disconnect hygiene: release the subscription on every exit path so
		// abandoned connections never leak subscribers.
		defer h.taskSurface.UnsubscribeTaskProgress(ws.ID, agent.ID, sessionID, subID)
	} else if err := h.probeSessionExists(c.Request.Context(), ws.ID, sessionID); err != nil {
		RespondError(c, err)
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	// Ask reverse proxies not to buffer frames (nginx honors this).
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	writeFrame := func(frame agents.TaskProgressFrame) {
		data, err := json.Marshal(frame)
		if err != nil {
			return
		}
		_, _ = c.Writer.Write([]byte("data: " + string(data) + "\n\n"))
		c.Writer.Flush()
	}
	writeDone := func() {
		_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
		c.Writer.Flush()
	}

	if !subscribed {
		// No run in flight: nothing is producing frames — the stream ends
		// cleanly so a reconnecting client can retry later.
		writeDone()
		return
	}

	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				// The run ended and closed the task fan-out: every buffered
				// frame (terminal ones included) has been drained.
				writeDone()
				return
			}
			writeFrame(frame)
		case <-c.Request.Context().Done():
			// The browser went away (navigation, refresh, abort) — stop
			// pumping into the dead socket.
			writeDone()
			return
		}
	}
}

// persistedTaskRecords loads and decodes the session's persisted task
// evidence: the x.eino.background_task.created events and the x.task_completed
// chips. Payloads round-trip through eino's HumanReadableSerializer (the
// write path in agents.ADKSessionAdapter, the read path in agents History's
// hydration), so decode mirrors it — the registered concrete types come back
// whole and non-matching payloads are skipped, exactly like the transcript
// projection's kind switch.
func (h *agentHandlers) persistedTaskRecords(ctx context.Context, workspaceID, sessionID string) ([]agents.TaskCreatedRecord, []agents.TaskCompletedPayload, error) {
	rows, err := h.sessionEvents.LoadEvents(ctx, store.LoadSessionEventsParams{
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		Kinds:       taskEventSessionKinds(),
	})
	if err != nil {
		return nil, nil, err
	}

	var (
		created   []agents.TaskCreatedRecord
		completed []agents.TaskCompletedPayload
	)
	serializer := &schema.HumanReadableSerializer{}
	for _, row := range rows {
		var se adk.SessionEvent[*schema.AgenticMessage]
		if err := serializer.Unmarshal(row.Payload, &se); err != nil {
			return nil, nil, fmt.Errorf("session tasks: decode event %q: %w", row.EventID, err)
		}
		if se.Extension == nil {
			continue
		}
		switch data := se.Extension.Data.(type) {
		case *bgtask.TaskCreatedSessionEvent:
			created = append(created, agents.TaskCreatedRecord{TaskID: data.TaskID, Description: data.Description})
		case agents.TaskCompletedPayload:
			completed = append(completed, data)
		}
	}
	return created, completed, nil
}

// probeSessionExists applies CancelRun's existence rule: a session with no
// persisted events is indistinguishable from an unknown one — both are
// not-found. The callers invoke it only when no live run already proved
// existence.
func (h *agentHandlers) probeSessionExists(ctx context.Context, workspaceID, sessionID string) error {
	rows, err := h.sessionEvents.LoadEvents(ctx, store.LoadSessionEventsParams{
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		Limit:       1,
	})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("%w: session not found", domain.ErrNotFound)
	}
	return nil
}

// taskOutputFilePath resolves a persisted output location inside the agent's
// jail. The lanes record jail-relative paths under .tasks (the append opener
// enforces the same confinement on the write side); the read side rejects
// absolute paths and traversal segments before joining so a tampered row can
// never read outside the jail.
func (h *agentHandlers) taskOutputFilePath(ws *domain.Workspace, agent *domain.Agent, outputPath string) string {
	if outputPath == "" || filepath.IsAbs(outputPath) || strings.Contains(outputPath, "..") {
		return ""
	}
	return filepath.Join(domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, agent.Slug), outputPath)
}

// replayTaskOutputRecords decodes a persisted output file into trajectory
// records: one decode per non-empty line/chunk, sequences assigned from a
// 1-based running counter across all decoded records — the exact numbering
// the live drain uses (readTaskProgressFrames counts records, and a shell
// file's one-record-per-line shape makes the counter coincide with the line
// ordinal). File order is preserved.
func replayTaskOutputRecords(kind string, data []byte) []taskOutputRecord {
	records := make([]taskOutputRecord, 0)
	sequence := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		for _, record := range agents.DecodeTaskProgressRecords(kind, line) {
			sequence++
			records = append(records, taskOutputRecord{TaskProgressRecord: record, Sequence: sequence})
		}
	}
	return records
}

// taskOutputFromFrames builds the output response from live trajectory
// frames: records in append order with their sequences, and the terminal
// notice when the frames carry one (null while the task is still running).
func taskOutputFromFrames(taskID string, frames []agents.TaskProgressFrame) taskOutputResponse {
	records := make([]taskOutputRecord, 0, len(frames))
	var terminal *agents.TaskCompletedPayload
	for _, frame := range frames {
		if frame.Record != nil {
			records = append(records, taskOutputRecord{TaskProgressRecord: *frame.Record, Sequence: frame.Sequence})
		}
		if frame.Terminal != nil {
			terminalPayload := *frame.Terminal
			terminal = &terminalPayload
		}
	}
	return taskOutputResponse{
		TaskID:   taskID,
		Records:  records,
		Terminal: terminal,
	}
}
