package agents

import (
	"context"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/scheduler"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// SendRequest is the full send shape: one turn dispatch's session, user text,
// attachment capability keys, and the run's origin. Send is the (sessionID,
// message) shorthand over it; Submit takes the whole shape so non-UI callers
// (the scheduler, design D6) can name who started the run.
type SendRequest struct {
	// SessionID is the chat session the turn runs in.
	SessionID string
	// Message is the user text (empty for an attachment-only send).
	Message string
	// AttachmentKeys names uploaded files riding the message (spec
	// chat-attachments); empty for a plain text send.
	AttachmentKeys []string
	// Origin names who started the run (design D6) — hooks.OriginUser when
	// empty — and is stamped on every hook event the run raises.
	Origin string
}

// Manager is the agent runtime's public surface: it creates chat sessions
// for stored agents, executes turns asynchronously, exposes per-session
// live subscriptions, and reports context-window usage. It speaks no eino
// types (see doc.go).
//
// Concurrency: Send serializes turns per session — a second Send on a
// session with an in-flight turn fails with ErrTurnInProgress; senders queue
// and resend after the running Turn completes. Distinct sessions run
// concurrently.
type Manager interface {
	// CreateSession registers a new, untitled chat session for the agent;
	// store.ErrNotFound when the agent does not exist. The runtime titles
	// the session from its first message.
	CreateSession(ctx context.Context, agentID string) (sessions.Session, error)
	// Send submits one user message (with optional attachment capability
	// keys, design D5) and returns promptly, while the turn proceeds in the
	// background. The turn persists its events regardless of any subscriber;
	// the returned Turn carries the terminal outcome. Fails with
	// store.ErrNotFound for an unknown session, ErrTurnInProgress when the
	// session is busy, and a rejection error for more than
	// attachments.MaxPerMessage keys or a key that was never uploaded.
	// Message-only sends keep the (sessionID, message) call shape; an empty
	// message with attachment keys is the attachment-only send (spec
	// chat-attachments). Model and provider failures do NOT fail Send: they
	// surface as the turn's terminal error and a persisted turn_error event.
	Send(ctx context.Context, sessionID, message string, attachmentKeys ...string) (*Turn, error)
	// Submit dispatches one turn from the full request shape (design D6,
	// spec add-scheduler "Scheduled runs are distinguishable to hooks") and
	// returns promptly, while the turn proceeds in the background — the same
	// path Send takes, with the run's origin made explicit: req.Origin names
	// who started the run (hooks.OriginUser when empty) and is stamped on
	// every hook event the run raises, so a hook can treat an unattended run
	// differently. The turn persists its events regardless of any
	// subscriber; the returned Turn carries the terminal outcome. Fails
	// exactly like Send: store.ErrNotFound for an unknown session,
	// ErrTurnInProgress when the session is busy, and a rejection error for
	// bad attachments. Model and provider failures do NOT fail Submit: they
	// surface as the turn's terminal error and a persisted turn_error event.
	Submit(ctx context.Context, req SendRequest) (*Turn, error)
	// Regenerate re-runs a turn's prompt as a new alternative answer (design
	// D7, spec chat-view "Regenerate and branch variants"): the original
	// turn's user text (and attachments, re-sent from disk — never
	// re-uploaded) drives a NEW turn whose reply events carry the
	// variant_of marker; the original prompt is not re-persisted. Fails with
	// store.ErrNotFound for an unknown session, ErrTurnInProgress when the
	// session is busy, and an error when the turn has no persisted user
	// message to re-run.
	Regenerate(ctx context.Context, sessionID, turnID string) (*Turn, error)
	// Subscribe registers a live subscription for the session's events;
	// store.ErrNotFound when the session does not exist. Events emitted
	// before Subscribe (or dropped under backpressure) are recovered by
	// replaying the session's events from the store.
	Subscribe(ctx context.Context, sessionID string) (Subscription, error)
	// ContextUsage reports the session's context-window fill state against
	// the agent's resolved limit (its override when set, the model catalog
	// window otherwise); store.ErrNotFound for an unknown session.
	ContextUsage(ctx context.Context, sessionID string) (ContextUsage, error)
	// Cancel stops the session's in-flight turn: the run is cancelled (the
	// runtime's safe-point cancel first, an escalation to the turn's context
	// after the grace period), a terminal turn_cancelled event is persisted
	// with any partial output retained, and the session is released so the
	// next send dispatches normally. Cancelling a turn parked on a pending
	// approval discards the run and clears the pending approval. Cancelling
	// with no turn in flight is a safe no-op (spec agent-runtime "Turn
	// cancellation").
	Cancel(ctx context.Context, sessionID string) error
	// ResolveApproval submits the user's decision for the session's pending
	// approval (spec chat-approvals "Decision side-channel"). Approving
	// resumes the interrupted turn from its checkpoint so the command
	// executes and the turn runs to completion; denying feeds a refusal back
	// to the model in place of the execution and the turn continues. A
	// decision for an interrupt that is not pending fails with
	// ErrApprovalNotPending and changes nothing.
	ResolveApproval(ctx context.Context, sessionID, interruptID string, approved bool) error
	// Busy reports whether the session has a turn in flight — running or
	// parked on a pending approval (spec chat-view: the running indicator,
	// and the delete refusal for a session with a turn in flight).
	Busy(ctx context.Context, sessionID string) bool
}

// Option configures the agent runtime at construction.
type Option func(*manager)

// WithPromptDocs supplies the persona-document source turns compose from
// (the agent's identity, soul and bootstrap documents, read fresh each
// turn). Unset — the default — turns run without persona documents
// (null-object behavior: agents without documents still chat).
func WithPromptDocs(d PromptDocs) Option {
	return func(m *manager) { m.prompts = d }
}

// WithWorkspaces supplies the workspace source turns root the jailed tools
// at (filesystem, shell, delete_file, task plan tools — resolved per agent slug, see
// gate.go). Unset — the default — no workspace-rooted tool is ever offered:
// agents keep chatting with the workspace-free tools only.
func WithWorkspaces(w *workspaces.Manager) Option {
	return func(m *manager) { m.workspaces = w }
}

// WithMCPManager supplies the MCP connection cache turns draw the agent's
// opted-in servers' tools from through the mcp.ToolSource seam (design D7,
// see mcp.go). Unset — the default — a no-op source contributes zero MCP
// tools: agents keep chatting without MCP.
func WithMCPManager(source mcp.ToolSource) Option {
	return func(m *manager) {
		if source != nil {
			m.mcpManager = source
		}
	}
}

// WithSkills supplies the global skills library directory turns draw skills
// from (design D6): each turn resolves a two-tier backend over this
// directory plus the agent's workspace skills directory (agent tier wins on
// a name conflict), gated by the store's registry enable flags and the
// agent's skill allowlist (see skills.go). Unset — the default — turns
// carry no skill metadata: agents keep chatting without skills.
func WithSkills(globalDir string) Option {
	return func(m *manager) {
		if globalDir != "" {
			m.skillsDir = globalDir
		}
	}
}

// WithAttachments supplies the attachment store turns read uploaded content
// from (design D5): a turn whose message carries attachment keys resolves
// their metadata against it and reads the bytes for the multi-part model
// input. Unset — the default — turns reject attachment keys: agents keep
// chatting without attachments.
func WithAttachments(dir string) Option {
	return func(m *manager) {
		if dir != "" {
			m.attachments = attachments.NewStore(dir, m.store.Attachments())
		}
	}
}

// WithBrowserDir supplies the browser tool's home directory (spec
// add-browser-tool D10): profile user-data directories root at
// <browserDir>/profiles and screenshots at <browserDir>/screenshots. Unset —
// the default — the browser tool keeps its own fallback location.
func WithBrowserDir(dir string) Option {
	return func(m *manager) {
		if dir != "" {
			m.browserDir = dir
		}
	}
}

// WithScheduler supplies the schedule service the scheduler tool drives
// (spec add-scheduler): the turn-scoped tool created in buildDirectTool is a
// thin client over it (add-scheduler design D1/D8). Unset — the default —
// the scheduler tool is withheld from the catalog entirely: agents keep
// chatting without scheduling.
func WithScheduler(s *scheduler.Service) Option {
	return func(m *manager) {
		if s != nil {
			m.scheduler = s
		}
	}
}

// WithHooks supplies the hook dispatcher turns resolve their lifecycle-hook
// chain from (design D4, spec agent-hooks; see hooks.go). Unset — the
// default — a no-op dispatcher resolves an empty chain: turns run without
// hooks, and nothing about the turn path changes.
func WithHooks(d *hooks.Dispatcher) Option {
	return func(m *manager) {
		if d != nil {
			m.hooks = d
		}
	}
}

// NewManager wires the agent runtime onto the store: sessions persist
// through the sessions service and models build through the provider
// registry over the store's provider configurations. Options apply
// construction-time configuration (WithPromptDocs, WithWorkspaces, WithHooks).
func NewManager(s store.Store, opts ...Option) Manager {
	return newManager(s, sessions.NewService(s), newModelRegistry(s), opts...)
}

// newManager is the injection seam for the store, session service, and
// model registry. It exists so tests can substitute a fake model registry;
// production callers use NewManager.
func newManager(s store.Store, svc sessions.Service, registry modelRegistry, opts ...Option) *manager {
	m := &manager{
		store:        s,
		sessions:     svc,
		registry:     registry,
		resolver:     NewCapabilityResolver(s),
		mcpManager:   noopMCPServer{},
		hooks:        hooks.NewNoopDispatcher(),
		baseCtx:      context.Background(),
		subs:         make(map[string]map[int]*streamSub),
		running:      make(map[string]*runningTurn),
		resolving:    make(map[string]bool),
		sessionTasks: make(map[string]*taskRegistry),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}
