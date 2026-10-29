package store

import "time"

// Invariant Documentation:
//
// 1. Sentinels Across Boundary:
// Store implementations return ErrNotFound for absent rows and never leak
// driver- or database-specific error types across the interface boundary.
//
// 2. App-Managed Timestamps:
// CreatedAt and UpdatedAt are stamped by the store layer (UTC) so behavior
// is identical across adapters. Zero timestamps on write are filled in;
// non-zero ones are preserved.
//
// 3. Store-Assigned Identity:
// Entity IDs are assigned by the store when empty (Create/Upsert) and are
// immutable afterwards. Skills are keyed by name instead: the registry
// indexes on-disk skill directories whose natural key is the name.

// Prompt document lifecycle states for Agent.PromptsStatus. The status moves
// generating → ready | failed only; the services layer owns the transitions
// and the startup sweep.
const (
	// PromptsStatusGenerating marks a create/regeneration in flight; a row
	// still in this state at startup is crash-orphaned.
	PromptsStatusGenerating = "generating"
	// PromptsStatusReady marks the workspace documents as usable.
	PromptsStatusReady = "ready"
	// PromptsStatusFailed marks the last generation as failed;
	// PromptsError then carries the reason and the agent stays usable.
	PromptsStatusFailed = "failed"
)

// Agent is one chat agent definition.
type Agent struct {
	ID   string
	Name string
	// Slug is the DNS-label identifier (see the workspaces package) that
	// names the agent's workspace directory. It is unique among agents and
	// immutable after create; empty only on rows not yet backfilled.
	Slug string
	// Description is the optional multi-line card summary.
	Description string
	// Brief is the persona instruction prompt generation runs from.
	Brief string
	// Avatar is the agent's stored procedural-avatar config — the wizard's
	// AvatarPicker output, persisted verbatim as a JSON object (opaque JSON,
	// the Hook.Config pattern). A nil map means "no stored avatar": surfaces
	// derive the face from the agent id instead.
	Avatar map[string]any
	// PromptsStatus is the prompt-document lifecycle state (one of the
	// PromptsStatus* constants); PromptsError carries the last failure's
	// reason and is empty otherwise.
	PromptsStatus string
	PromptsError  string

	ProviderID string
	Model      string
	// Effort is the agent's default reasoning effort ("" = provider default).
	// Its options are the selected model's capability set (see the agents
	// package catalog).
	Effort      string
	Temperature float64
	// MaxTokens is the per-turn output cap; 0 means the provider default.
	MaxTokens int
	// ContextWindow overrides the model catalog's context window when
	// non-zero; 0 means use the catalog value.
	ContextWindow int
	// Tools, Skills, and MCPServers are allowlists of identifiers the agent's
	// turns may use; an empty list permits every enabled one (design D3).
	Tools      []string
	Skills     []string
	MCPServers []string
	// SubagentsEnabled turns the agent tool on for this agent's turns
	// (design D3: a dedicated column, not an allowlist entry). The column
	// defaults on; callers set the flag explicitly.
	SubagentsEnabled bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Provider is one model provider configuration. The API key is stored in the
// clear for the base design (single-user local assumption).
type Provider struct {
	ID        string
	Type      string
	Name      string
	BaseURL   string
	APIKey    string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MCPServer is one MCP server definition. Status and StatusError persist the
// last connection probe outcome and MAY be stale between probes.
type MCPServer struct {
	ID        string
	Name      string
	Transport string
	Command   string
	Args      []string
	Env       map[string]string
	URL       string
	Headers   map[string]string
	// Auth is the server's authentication settings (design D4); the zero
	// value means static headers only.
	Auth MCPServerAuth
	// Service is the optional integration catalog tag; empty for raw MCP
	// servers (add-integrations design D1: an integration is a tagged
	// MCPServer row, not a separate entity).
	Service string
	// CredentialSet and CredentialHint are computed/redacted views for
	// listings (add-integrations design D4): only whether an integration
	// credential exists and its last four characters. Plain row fields —
	// never persisted (no column, never part of the auth JSON document);
	// the store populates them on read.
	CredentialSet  bool
	CredentialHint string
	Enabled        bool
	Status         string
	StatusError    string
	ToolCount      int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// MCP server authentication modes — the MCPServerAuth.Mode vocabulary.
const (
	// MCPAuthModeNone marks a server that authenticates with static
	// configured headers only. An empty Mode means the same.
	MCPAuthModeNone = "none"
	// MCPAuthModeOAuth marks a server that authenticates with OAuth 2.1.
	MCPAuthModeOAuth = "oauth"
)

// Credential header styles — the MCPServerAuth.CredentialHeaderStyle
// vocabulary (add-integrations design D3). Each style declares how the
// integration credential is rendered into a request at dial time.
const (
	// MCPAuthHeaderStyleBearer sends "Authorization: Bearer <credential>".
	// The empty style means the same.
	MCPAuthHeaderStyleBearer = "bearer"
	// MCPAuthHeaderStyleTokenEq sends "Authorization: token <credential>".
	MCPAuthHeaderStyleTokenEq = "token="
	// MCPAuthHeaderStyleBasic sends "Authorization: Basic <base64(credential)>".
	MCPAuthHeaderStyleBasic = "basic"
	// MCPAuthHeaderStyleCustomPrefix marks a "<custom>: <credential>"
	// Authorization scheme; the scheme follows the "custom:" prefix.
	MCPAuthHeaderStyleCustomPrefix = "custom:"
	// MCPAuthHeaderStyleHeaderPrefix marks a named header carrying the
	// credential verbatim; the header name follows the "header:" prefix.
	MCPAuthHeaderStyleHeaderPrefix = "header:"
)

// MCPServerAuth is an MCP server's authentication settings, persisted as
// JSON in mcp_servers.auth (design D4). An empty Mode means none (static
// headers). The registered client survives Disconnect so re-authorizing
// never re-registers.
type MCPServerAuth struct {
	Mode                  string   `json:"mode,omitempty"`
	ClientID              string   `json:"client_id,omitempty"`
	ClientSecret          string   `json:"client_secret,omitempty"`
	Issuer                string   `json:"issuer,omitempty"`
	ResourceMetadataURL   string   `json:"resource_metadata_url,omitempty"`
	AuthServerMetadataURL string   `json:"auth_server_metadata_url,omitempty"`
	RedirectURI           string   `json:"redirect_uri,omitempty"`
	Scopes                []string `json:"scopes,omitempty"`

	// IntegrationCredential is the token/API key entered through the
	// connect sheet (add-integrations design D4). Stored in the auth JSON,
	// never in Headers (credential secrecy).
	IntegrationCredential string `json:"integration_credential,omitempty"`
	// CredentialHeaderStyle declares how to render the credential at dial
	// time: one of the MCPAuthHeaderStyle* values, including the "custom:"
	// and "header:" prefixed forms. Empty means bearer. Catalog-declared
	// per service (design D3).
	CredentialHeaderStyle string `json:"credential_header_style,omitempty"`
	// ExtraAuthParams are extra query parameters appended to the authorize
	// URL by the OAuth flow (add-integrations design D3): Google needs
	// access_type=offline and prompt=consent to hand back a refresh token.
	ExtraAuthParams map[string]string `json:"extra_auth_params,omitempty"`
}

// IsOAuth reports whether the server authenticates with OAuth 2.1.
func (a MCPServerAuth) IsOAuth() bool { return a.Mode == MCPAuthModeOAuth }

// MCPOAuthToken is one server's persisted OAuth token set. Token is the MCP
// client's opaque JSON encoding.
type MCPOAuthToken struct {
	ServerID  string
	Token     []byte
	UpdatedAt time.Time
}

// Schedule run outcomes — the Schedule.LastRunStatus vocabulary. An empty
// status means the schedule has never fired.
const (
	// ScheduleRunStatusRan marks a fire that started a run successfully.
	ScheduleRunStatusRan = "ran"
	// ScheduleRunStatusFailed marks a fire that could not start a run (store
	// error, agent gone); ScheduleFire.Error carries the reason.
	ScheduleRunStatusFailed = "failed"
	// ScheduleRunStatusMissed marks a slot the process could not fire on
	// time (off, asleep, paused) that was skipped rather than run late.
	ScheduleRunStatusMissed = "missed"
)

// Schedule binds an agent and a prompt to a five-field cron recurrence
// (local time). Enabled and NextFireAt together drive the scheduler's
// three-way due state machine (add-scheduler design D3): an enabled schedule
// with NextFireAt at or before now is due.
type Schedule struct {
	ID   string
	Name string
	// AgentID locates the run's agent; deleting the agent deletes the
	// schedule with it (FK cascade).
	AgentID string
	// Prompt is sent to the agent on every fire.
	Prompt string
	// Cron is the five-field cron expression, evaluated in the host's local
	// zone at computation time.
	Cron    string
	Enabled bool
	// NextFireAt is the persisted due slot the scheduler advances on every
	// fire; nil when disabled or not yet computed.
	NextFireAt *time.Time
	// LastRunAt, LastRunStatus, LastRunError, and LastRunSessionID record
	// the most recent fire; LastRunStatus is "" (never fired) or one of the
	// ScheduleRunStatus* constants, and LastRunError carries the failure
	// reason when the status is failed.
	LastRunAt        *time.Time
	LastRunStatus    string
	LastRunError     string
	LastRunSessionID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ScheduleFire is the last-run state RecordFire writes in one UPDATE
// (mark-and-advance before the run starts, so a crash mid-fire can never
// double-fire).
type ScheduleFire struct {
	// At is the fire time stamped into LastRunAt.
	At     time.Time
	Status string
	Error  string
	// SessionID locates the run the fire started, when one exists.
	SessionID string
	// NextFireAt is the advanced slot; nil clears the stored one.
	NextFireAt *time.Time
}

// Hook is one lifecycle hook definition. Matcher and Config are opaque JSON
// documents owned by the settings service; the store persists them verbatim.
type Hook struct {
	ID          string
	Name        string
	Event       string
	Matcher     string
	HandlerType string
	Config      string
	TimeoutMS   int
	OnFailure   string
	Enabled     bool
	Position    int
	Status      string
	StatusError string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// GatewaySettings is the singleton gateway configuration. Absence of a saved
// row is a normal state: Get returns (nil, nil) and callers apply defaults.
type GatewaySettings struct {
	Enabled        bool
	DefaultAgentID string
	UpdatedAt      time.Time
}

// SessionEvent is one record of the append-only session event log. Seq is
// assigned by the store on append and is monotonically increasing per
// session; Payload is opaque JSON owned by the session layer.
type SessionEvent struct {
	SessionID  string
	EventID    string
	TurnID     string
	Seq        int64
	Kind       string
	Payload    []byte
	OccurredAt time.Time
}

// Session is one chat session: the queryable identity of a conversation with
// an agent. The conversation content lives in the session event log; this row
// only carries what the session list renders plus the session's optional
// model/effort overrides (design D1: empty means inherit the agent's
// configuration).
type Session struct {
	ID      string
	AgentID string
	Title   string
	// Model and Effort are the session's per-conversation overrides over the
	// agent's configuration; empty inherits.
	Model string
	// Effort is the session's reasoning-effort override ("" = inherit).
	Effort string
	// LastActivityAt is advanced by the store layer whenever events are
	// appended to the session.
	CreatedAt      time.Time
	LastActivityAt time.Time
}

// Attachment is one stored upload's registry row (design D5). Key is the
// unguessable capability key naming both the row and the file on disk under
// the attachments directory; Name, Mime, and Size are the transcript
// metadata. Raw filesystem paths never live here — the key is the only
// address any surface sees.
type Attachment struct {
	Key  string
	Name string
	Mime string
	Size int64
}

// ToolState is one stored global toggle of a built-in catalog tool. Rows
// exist only for explicitly toggled tools; absence means enabled.
type ToolState struct {
	Name      string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Skill is one registry row indexing an on-disk skill directory. Name is the
// natural key and matches the directory name under ~/.buddies/skills/.
type Skill struct {
	Name        string
	Description string
	Version     string
	Source      string
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// AgentHook is one ordered assignment of a hook to an agent (design D4): a
// link row, not a copy — deleting the agent or the hook removes it. An
// assigned hook leaves the global tier: it applies only on turns of the
// agents it is assigned to.
type AgentHook struct {
	AgentID  string
	HookID   string
	Position int
}

// Hook execution outcomes — the audit values of the hooks decision contract.
const (
	// HookOutcomeAllowed marks an evaluation that did not block.
	HookOutcomeAllowed = "allowed"
	// HookOutcomeBlocked marks an evaluation whose decision blocked the
	// prompt submit or tool call.
	HookOutcomeBlocked = "blocked"
	// HookOutcomeError marks an evaluation whose handler failed or timed
	// out; HookExecution.Error carries the reason.
	HookOutcomeError = "error"
)

// HookExecution is one audit row of a hook evaluation during a turn. The
// trail is append-only and outlives the hook: rows are kept when the hook
// is deleted.
type HookExecution struct {
	ID     string
	HookID string
	// AgentID, SessionID, and TurnID locate the turn the evaluation ran in;
	// empty when there is no session context (e.g. a dry-run).
	AgentID   string
	SessionID string
	TurnID    string
	// Event is the lifecycle point that fired the hook (the Hook.Event
	// value).
	Event string
	// Outcome is one of the HookOutcome* constants (allowed, blocked,
	// error); Error carries the failure reason when the outcome is error.
	Outcome    string
	Error      string
	DurationMS int
	// OccurredAt is stamped by the store (UTC) when empty.
	OccurredAt time.Time
}

// ProviderModel is one model row stored for a provider (design D2).
type ProviderModel struct {
	ID            int64  `json:"id"`
	ProviderID    string `json:"provider_id"`
	ModelID       string `json:"model_id"`
	ContextWindow int    `json:"context_window"`
	ImageInput    bool   `json:"image_input"`
	PDFInput      bool   `json:"pdf_input"`
	VideoInput    bool   `json:"video_input"`
	// Efforts is the row's declared reasoning-effort set in declared order.
	// Empty means undeclared; readers resolve an undeclared row to high.
	Efforts   []string  `json:"efforts"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
