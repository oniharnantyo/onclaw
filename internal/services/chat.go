package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"sync"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/vault"
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
	// saves opens the host's native save dialog for the Save As export
	// (add-artifact-save-as design D1); NoSaveDialog on hosts without one.
	saves SaveDialog
	// store resolves the agent rows the workspace reader jails against;
	// attachments and workspaces back the attachment and preview surfaces,
	// and workspaces also roots the screenshot read (the browser tool writes
	// captures into the calling session's workspace, design D6).
	store       store.Store
	attachments *attachments.Store
	workspaces  *workspaces.Manager
	// vault backs the save-to-vault path of ResolveInput
	// (add-browser-login-handover design D5). Nil until WithChatVault wires
	// it: a save-to-vault request then resolves unsaved, and the sign-in
	// still proceeds (a failed save never blocks the delivery).
	vault *vault.Service

	mu   sync.Mutex
	subs map[string]agents.Subscription
}

// NewChatService returns the chat service over the runtime and session
// service, forwarding runtime events through emitter. The store, attachment
// store, and workspace manager back the attachment and workspace-preview
// surfaces; all dependencies are required. Options carry the optional
// surfaces (the Save As dialog, add-artifact-save-as design D1).
func NewChatService(turns agents.Manager, sessions sessions.Service, emitter Emitter, store store.Store, atts *attachments.Store, ws *workspaces.Manager, opts ...ChatOption) *ChatService {
	s := &ChatService{
		turns:       turns,
		sessions:    sessions,
		emitter:     emitter,
		saves:       NoSaveDialog{},
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

// AllSessions returns every agent's sessions, most recent activity first
// (add-grouped-session-rail design D1: one all-sessions listing replaces the
// per-agent fetch). The result is never nil.
func (s *ChatService) AllSessions(ctx context.Context) ([]store.Session, error) {
	sessions, err := s.sessions.AllSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return sessions, nil
}

// RunningSessions reports the sessions with a turn in flight — running or
// parked on a pending approval (add-grouped-session-rail design D2) — for
// the frontend's boot-time sync. The result is never nil.
func (s *ChatService) RunningSessions(ctx context.Context) ([]string, error) {
	return s.turns.RunningSessions(ctx), nil
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
// metadata is registered. An upload made with sessionID's chat open is
// foldered under the attachments directory's per-session folder
// (attachments/<sessionID>/<key>, schema v19); an empty sessionID — the
// fresh-chat composer, where the send path creates the session — keeps the
// flat legacy layout; the row records which, so reads need only the key.
// data is the base64-encoded file content (explicit on the bridge so the
// encoding is identical on both hosts). An over-cap or unknown-type file is
// rejected with the reason (attachments caps: at most 5 MB images, 20 MB
// PDFs, 1 MB text; the per-message count cap rides the send path).
func (s *ChatService) SaveAttachment(ctx context.Context, sessionID, name, mime, data string) (sessions.Attachment, error) {
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return sessions.Attachment{}, fmt.Errorf("save attachment: decode payload: %w", err)
	}
	att, err := s.attachments.Save(ctx, sessionID, name, mime, raw)
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

// Resync re-fires the session's composite in-flight snapshot to every live
// subscriber (enhance-resumable-streams design D5): the bound call the
// frontend's resync triggers land on — a detected delta gap (the beacon's
// counter comparison, design D6), a reconnect that kept its subscription, or
// a focus regain while the session is busy. Idle or unknown sessions are a
// safe no-op.
func (s *ChatService) Resync(ctx context.Context, sessionID string) error {
	if err := s.turns.Resync(ctx, sessionID); err != nil {
		return fmt.Errorf("resync session: %w", err)
	}
	return nil
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

// ResolveInput submits the user-typed secret — and, when the card collected
// one, the username-or-email identifier — for the session's pending browser
// attention request (add-browser-login-handover design D3/D5). The value is
// never persisted: it is handed to the runtime's delivery channel only, and
// the transcript records a receipt. When saveLogin is set and the pending
// request names a secret with an origin and an account name, the value is
// vaulted first under a name derived from the origin host, addedBy the
// session's agent — a typed identifier overrides the agent-supplied username
// for the vault entry. A failed save is logged and does NOT block the
// sign-in; the runtime learns the save's outcome through
// (savedAs, savedLoginID). A stale or already-resolved interrupt id fails
// with agents.ErrInputNotPending and changes nothing.
func (s *ChatService) ResolveInput(ctx context.Context, sessionID, interruptID, text, identifier string, saveLogin bool) error {
	savedAs, savedLoginID := s.savePendingLogin(ctx, sessionID, interruptID, text, identifier, saveLogin)
	if err := s.turns.ResolveInput(ctx, sessionID, interruptID, text, identifier, savedAs, savedLoginID); err != nil {
		return fmt.Errorf("resolve input: %w", err)
	}
	return nil
}

// CancelInput ends the session's pending browser secret request with the
// user's cancel — the secret card's Cancel button. The parked submit_secret
// call unblocks on the cancelled result (the turn continues; the agent's
// fallback is plain chat or takeover), the child's protection freeze is
// released best-effort, and the receipt records the cancelled outcome only.
// A stale or already-resolved interrupt id fails with agents.ErrInputNotPending
// and changes nothing.
func (s *ChatService) CancelInput(ctx context.Context, sessionID, interruptID string) error {
	if err := s.turns.ResolveAttention(ctx, sessionID, interruptID, sessions.InputCancelled); err != nil {
		return fmt.Errorf("cancel input: %w", err)
	}
	return nil
}

// BeginBrowserTakeover approves the session's pending browser takeover
// request (design D8): the parent signals the child to relaunch that
// profile's browser headed and focused, and the session is held for the
// user. A stale id fails with agents.ErrInputNotPending.
func (s *ChatService) BeginBrowserTakeover(ctx context.Context, sessionID, interruptID string) error {
	if err := s.turns.BeginTakeover(ctx, sessionID, interruptID); err != nil {
		return fmt.Errorf("begin takeover: %w", err)
	}
	return nil
}

// ResolveTakeover ends the session's pending browser takeover with the
// user's outcome — the completed/cancelled vocabulary of the attention
// card (design D8). A stale id fails with agents.ErrInputNotPending.
func (s *ChatService) ResolveTakeover(ctx context.Context, sessionID, interruptID, outcome string) error {
	if err := s.turns.ResolveAttention(ctx, sessionID, interruptID, outcome); err != nil {
		return fmt.Errorf("resolve takeover: %w", err)
	}
	return nil
}

// savePendingLogin vaults the typed value when saveLogin is set and the
// pending secret names an origin and username. Every failure — an unwired
// vault, an unreadable session log, a missing payload, a vault write
// error — is logged and answered with (false, ""): the sign-in proceeds
// unsaved, never blocked by the save (spec settings; design D5).
func (s *ChatService) savePendingLogin(ctx context.Context, sessionID, interruptID, text, identifier string, saveLogin bool) (bool, string) {
	if !saveLogin {
		return false, ""
	}
	if s.vault == nil {
		slog.WarnContext(ctx, "resolve input: save-to-vault requested but the vault is unavailable",
			"session_id", sessionID, "interrupt_id", interruptID)
		return false, ""
	}
	events, err := s.sessions.Events(ctx, sessionID)
	if err != nil {
		slog.WarnContext(ctx, "resolve input: vault save skipped, session log unreadable",
			"session_id", sessionID, "interrupt_id", interruptID, "error", err)
		return false, ""
	}
	var pending *sessions.Input
	for i := range events {
		ev := events[i]
		if ev.Kind == sessions.KindInputRequired && ev.Input != nil && ev.Input.InterruptID == interruptID {
			pending = ev.Input // keep the last pending payload
		}
	}
	if pending == nil || pending.AttentionKind != sessions.AttentionSecret || pending.Origin == "" {
		return false, ""
	}
	// The typed identifier (username or email) wins for the vault entry;
	// the agent-supplied username is the fallback.
	username := identifier
	if username == "" {
		username = pending.Username
	}
	if username == "" {
		return false, ""
	}

	name := originHostName(pending.Origin)
	if u, err := url.Parse(pending.Origin); err == nil && u.Hostname() != "" {
		name = u.Hostname()
	}
	addedBy := s.sessionAgentName(ctx, sessionID)
	view, err := s.vault.SaveLogin(ctx, pending.Origin, username, text, name, addedBy)
	if err != nil {
		// The value must never reach a log line: the error carries vault
		// context only.
		slog.WarnContext(ctx, "resolve input: vault save failed; the sign-in continues unsaved",
			"session_id", sessionID, "interrupt_id", interruptID, "error", err)
		return false, ""
	}
	return true, view.ID
}

// sessionAgentName resolves the session's agent's display name for the
// vault's addedBy record; any resolution failure falls back to the agent id.
func (s *ChatService) sessionAgentName(ctx context.Context, sessionID string) string {
	sess, err := s.sessions.Session(ctx, sessionID)
	if err != nil {
		return sessionID
	}
	agent, err := s.store.Agents().ByID(ctx, sess.AgentID)
	if err != nil || agent.Name == "" {
		return sess.AgentID
	}
	return agent.Name
}

// WithChatVault supplies the vault service the resolve-input save-to-vault
// path uses (add-browser-login-handover design D5). A nil service is
// ignored — the unwired default stands, where save requests resolve
// unsaved (mirrors WithChatBrowserDir).
func WithChatVault(v *vault.Service) ChatOption {
	return func(s *ChatService) {
		if v != nil {
			s.vault = v
		}
	}
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
