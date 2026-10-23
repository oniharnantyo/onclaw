package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// ChatService is the bound service for chat sessions: session lifecycle and
// transcript reads over the session service, turn dispatch over the agent
// runtime, live turn events forwarded to the frontend through the emitter
// (design D7: SendMessage returns immediately; progress flows as
// session-scoped events), the capability-gated attachment store (design D5),
// and the jailed workspace reader (design D8).
type ChatService struct {
	turns    agents.Manager
	sessions sessions.Service
	emitter  Emitter
	// store resolves the agent rows the workspace reader jails against;
	// attachments and workspaces back the attachment and preview surfaces.
	store       store.Store
	attachments *attachments.Store
	workspaces  *workspaces.Manager
	// browserDir is the browser tool's home directory the screenshot read
	// serves from (design D6): screenshots live at <browserDir>/screenshots.
	browserDir string

	mu   sync.Mutex
	subs map[string]agents.Subscription
}

// NewChatService returns the chat service over the runtime and session
// service, forwarding runtime events through emitter. The store, attachment
// store, and workspace manager back the attachment and workspace-preview
// surfaces; all dependencies are required. Options carry the optional
// surfaces (the browser-screenshot read's directory, design D6).
func NewChatService(turns agents.Manager, sessions sessions.Service, emitter Emitter, store store.Store, atts *attachments.Store, ws *workspaces.Manager, opts ...ChatOption) *ChatService {
	s := &ChatService{
		turns:       turns,
		sessions:    sessions,
		emitter:     emitter,
		store:       store,
		attachments: atts,
		workspaces:  ws,
		subs:        make(map[string]agents.Subscription),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CreateSession registers a new, untitled session for the agent;
// store.ErrNotFound when the agent does not exist. The runtime titles the
// session from its first message.
func (s *ChatService) CreateSession(ctx context.Context, agentID string) (store.Session, error) {
	session, err := s.turns.CreateSession(ctx, agentID)
	if err != nil {
		return store.Session{}, fmt.Errorf("create session: %w", err)
	}
	return session, nil
}

// Sessions returns the agent's sessions, most recent activity first. The
// result is never nil.
func (s *ChatService) Sessions(ctx context.Context, agentID string) ([]store.Session, error) {
	sessions, err := s.sessions.SessionsByAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return sessions, nil
}

// Transcript replays the session's persisted events in log order. Live-only
// events (text and reasoning deltas) are absent; the durable forms
// (assistant message, reasoning) stand in for them.
func (s *ChatService) Transcript(ctx context.Context, sessionID string) ([]sessions.Event, error) {
	events, err := s.sessions.Events(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load transcript: %w", err)
	}
	return events, nil
}

// TranscriptPage replays one window of the session's transcript (design D6,
// spec chat-view "Transcript windowing"): the newest `limit` events — or,
// when beforeSeq is set, the newest events strictly before that sequence —
// in log order, so the chat view loads the most recent history first and
// pages backward with "load earlier". A non-positive limit takes the
// sessions package's default page size.
func (s *ChatService) TranscriptPage(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]sessions.Event, error) {
	events, err := s.sessions.EventsPage(ctx, sessionID, beforeSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("load transcript page: %w", err)
	}
	return events, nil
}

// DeleteSession removes the session and its transcript (spec chat-view
// "Sessions organized per agent"): the session row, its event log, and its
// compaction checkpoint. The agent's other sessions are untouched, and
// deleting an unknown session is a safe no-op. A session with a turn in
// flight (running or parked on an approval) is refused with
// agents.ErrTurnInProgress — its transcript is being written; cancel first.
func (s *ChatService) DeleteSession(ctx context.Context, sessionID string) error {
	if s.turns.Busy(ctx, sessionID) {
		return fmt.Errorf("delete session %s: %w", sessionID, agents.ErrTurnInProgress)
	}
	if err := s.sessions.Delete(ctx, sessionID); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// SaveAttachment stores one uploaded file (design D5, spec chat-attachments
// "Capability-gated storage"): the bytes land under the attachments
// directory named by an unguessable capability key and the transcript
// metadata is registered. data is the base64-encoded file content (explicit
// on the bridge so the encoding is identical on both hosts). An over-cap or
// unknown-type file is rejected with the reason (attachments caps: at most
// 5 MB images, 20 MB PDFs, 1 MB text; the per-message count cap rides the
// send path).
func (s *ChatService) SaveAttachment(ctx context.Context, name, mime, data string) (sessions.Attachment, error) {
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return sessions.Attachment{}, fmt.Errorf("save attachment: decode payload: %w", err)
	}
	att, err := s.attachments.Save(ctx, name, mime, raw)
	if err != nil {
		return sessions.Attachment{}, fmt.Errorf("save attachment: %w", err)
	}
	return att, nil
}

// AttachmentContent is one attachment's served content: the transcript
// metadata plus the base64-encoded bytes (design D5 — the frontend renders
// previews and downloads from this, never from a raw path).
type AttachmentContent struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
	// Data is the base64-encoded file content.
	Data string `json:"data"`
}

// AttachmentContent resolves one capability key to its metadata and content.
// An unknown key is refused; the on-disk path is never exposed.
func (s *ChatService) AttachmentContent(ctx context.Context, key string) (AttachmentContent, error) {
	content, err := s.attachments.Read(ctx, key)
	if err != nil {
		return AttachmentContent{}, fmt.Errorf("attachment content: %w", err)
	}
	return AttachmentContent{
		Key:  content.Meta.Key,
		Name: content.Meta.Name,
		Mime: content.Meta.Mime,
		Size: content.Meta.Size,
		Data: base64.StdEncoding.EncodeToString(content.Data),
	}, nil
}

// Send submits one user message — with optional attachment capability keys
// from prior SaveAttachment uploads (design D5) — and returns the turn's ID
// promptly, while the turn proceeds in the background. Attachment-only sends
// (no typed text) are valid; an empty message without attachments is
// rejected, as is more than four keys or an unknown key. Turn progress flows
// as events named SessionEventName(sessionID); the turn's terminal outcome
// surfaces as a turn_completed, turn_error, or turn_cancelled event. Fails
// with agents.ErrTurnInProgress when the session is busy.
func (s *ChatService) Send(ctx context.Context, sessionID, message string, attachmentKeys []string) (string, error) {
	turn, err := s.turns.Send(ctx, sessionID, message, attachmentKeys...)
	if err != nil {
		return "", fmt.Errorf("send message: %w", err)
	}
	return turn.TurnID(), nil
}

// Regenerate re-runs a turn's prompt as a new alternative answer (design D7,
// spec chat-view "Regenerate and branch variants"): a NEW turn executes the
// original user text (and attachments, re-sent from stored content) and its
// reply events carry the variant_of marker; the duplicate prompt is not
// re-persisted. Regeneration is a real turn — it consumes model capacity and
// streams live; Fails with agents.ErrTurnInProgress when the session is busy
// or when the turn has no user message to re-run.
func (s *ChatService) Regenerate(ctx context.Context, sessionID, turnID string) (string, error) {
	turn, err := s.turns.Regenerate(ctx, sessionID, turnID)
	if err != nil {
		return "", fmt.Errorf("regenerate: %w", err)
	}
	return turn.TurnID(), nil
}

// Cancel stops the session's in-flight turn (spec agent-runtime "Turn
// cancellation"): streaming and tool activity stop, a terminal
// turn_cancelled event lands with the partial output retained, and the
// session accepts a new send. A turn parked on a pending approval is
// discarded and its approval cleared. Cancelling with no turn in flight is a
// safe no-op.
func (s *ChatService) Cancel(ctx context.Context, sessionID string) error {
	if err := s.turns.Cancel(ctx, sessionID); err != nil {
		return fmt.Errorf("cancel turn: %w", err)
	}
	return nil
}

// ResolveApproval submits the user's decision for the session's pending
// approval (spec chat-approvals "Decision side-channel"): approving resumes
// the interrupted turn so the command executes and the turn runs to
// completion; denying feeds the refusal back to the model and the turn
// continues. A stale or already-resolved interrupt id fails with
// agents.ErrApprovalNotPending and changes nothing.
func (s *ChatService) ResolveApproval(ctx context.Context, sessionID, interruptID string, approved bool) error {
	if err := s.turns.ResolveApproval(ctx, sessionID, interruptID, approved); err != nil {
		return fmt.Errorf("resolve approval: %w", err)
	}
	return nil
}

// SetSessionOverrides rewrites the session's model and reasoning-effort
// overrides (design D1); an empty value clears that override back to
// inherit. The next Send resolves the turn's effective configuration from
// the row, so a rejected pair surfaces as that turn's turn_error event.
func (s *ChatService) SetSessionOverrides(ctx context.Context, sessionID, model, effort string) error {
	if err := s.sessions.SetOverrides(ctx, sessionID, model, effort); err != nil {
		return fmt.Errorf("set session overrides: %w", err)
	}
	return nil
}

// ContextUsage reports the session's context-window fill state (used tokens
// against the model's declared limit) for the chat view's context indicator.
func (s *ChatService) ContextUsage(ctx context.Context, sessionID string) (agents.ContextUsage, error) {
	usage, err := s.turns.ContextUsage(ctx, sessionID)
	if err != nil {
		return agents.ContextUsage{}, fmt.Errorf("context usage: %w", err)
	}
	return usage, nil
}

// TurnStateResult is the in-flight turn status of a session (design D1):
// Busy reports whether a turn is running or parked on an approval, and
// TurnID identifies that in-flight turn for re-adoption by the frontend.
type TurnStateResult struct {
	Busy   bool   `json:"busy"`
	TurnID string `json:"turn_id"`
}

// TurnState reports whether the session has a turn in flight and its turn ID.
func (s *ChatService) TurnState(ctx context.Context, sessionID string) (TurnStateResult, error) {
	busy, turnID := s.turns.TurnState(ctx, sessionID)
	return TurnStateResult{Busy: busy, TurnID: turnID}, nil
}

// WorkspaceEntry is one listed workspace child (design D8).
type WorkspaceEntry struct {
	// Name is the entry's base name inside the listed directory.
	Name string `json:"name"`
	// Dir reports whether the entry is a directory.
	Dir bool `json:"dir"`
	// Size is the file's byte size (0 for directories).
	Size int64 `json:"size"`
}

// workspaceAgent resolves the agent row the reader jails against; an unknown
// agent or an agent without a workspace slug is refused.
func (s *ChatService) workspaceAgent(ctx context.Context, agentID string) (store.Agent, error) {
	agent, err := s.store.Agents().ByID(ctx, agentID)
	if err != nil {
		return store.Agent{}, fmt.Errorf("workspace: %w", err)
	}
	if agent.Slug == "" {
		return store.Agent{}, fmt.Errorf("workspace: agent %q has no workspace", agent.Name)
	}
	return *agent, nil
}

// WorkspaceFile reads one file from the agent's workspace (design D8, spec
// chat-panels "Preview sources"): relPath is workspace-relative and jailed —
// traversal, absolute paths, and symlink escapes are refused before the
// open, and nothing outside the agent's workspace is ever served.
func (s *ChatService) WorkspaceFile(ctx context.Context, agentID, relPath string) (string, error) {
	agent, err := s.workspaceAgent(ctx, agentID)
	if err != nil {
		return "", err
	}
	content, err := s.workspaces.ReadFile(agent.Slug, relPath)
	if err != nil {
		return "", fmt.Errorf("workspace file: %w", err)
	}
	return content, nil
}

// ListWorkspace lists one directory of the agent's workspace (design D8),
// jailed like WorkspaceFile. relPath "" or "/" lists the workspace root; the
// result is never nil.
func (s *ChatService) ListWorkspace(ctx context.Context, agentID, relPath string) ([]WorkspaceEntry, error) {
	agent, err := s.workspaceAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	entries, err := s.workspaces.ListDir(agent.Slug, relPath)
	if err != nil {
		return nil, fmt.Errorf("workspace list: %w", err)
	}
	out := make([]WorkspaceEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, WorkspaceEntry{Name: e.Name, Dir: e.Dir, Size: e.Size})
	}
	return out, nil
}

// Subscribe attaches the session's live event stream: runtime events are
// forwarded to the emitter under SessionEventName(sessionID). Subscribing a
// session that is already subscribed replaces its previous subscription.
// Missed events (attached late) are recovered by reading Transcript.
func (s *ChatService) Subscribe(ctx context.Context, sessionID string) error {
	sub, err := s.turns.Subscribe(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("subscribe session: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.subs[sessionID]; ok {
		prev.Close()
	}
	s.subs[sessionID] = sub

	go s.forward(sessionID, sub)
	return nil
}

// Unsubscribe detaches the session's live event stream. It is a no-op when
// the session is not subscribed.
func (s *ChatService) Unsubscribe(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub, ok := s.subs[sessionID]; ok {
		sub.Close()
		delete(s.subs, sessionID)
	}
	return nil
}

// forward pumps one subscription's events to the emitter until the
// subscription closes. The subscription channel is closed by Close
// (replacement or unsubscribe), which ends the loop.
func (s *ChatService) forward(sessionID string, sub agents.Subscription) {
	name := SessionEventName(sessionID)
	for ev := range sub.Events() {
		s.emitter.Emit(name, ev)
	}
}
