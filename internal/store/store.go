// Package store defines the data access ports, driver registry, and contract invariants.
package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Invariant Documentation:
//
// 1. Sentinels Across Boundary:
// Store implementations MUST return domain sentinel errors (e.g. domain.ErrNotFound,
// domain.ErrConflict, domain.ErrInvalid, domain.ErrLastOwnerProtected) and NEVER leak
// driver- or database-specific error types (such as pgx or pq error types) across the interface boundary.
//
// 2. Email Normalization:
// Store implementations MUST ensure all email addresses are normalized (lowercased and trimmed
// using domain.NormalizeEmail) at the boundary before storing or querying users.
//
// 3. Stateless Tenant Scoping:
// Workspace-scoped operations must be partitioned by workspace ID / slug.
// WorkspaceStore.ListAll is the only unscoped workspace query and is reserved strictly
// for instance-level administration paths guarded by master-tenant Superadmin permissions.
//
// 4. App-Managed Timestamps:
// Entity creation and update timestamps (CreatedAt, UpdatedAt) are managed by the application/store
// layer to maintain consistent behavior across adapters.

// Store is the root interface for data persistence, providing access to sub-stores and transaction management.
type Store interface {
	Users() UserStore
	Workspaces() WorkspaceStore
	Roles() RoleStore
	Members() MemberStore
	Providers() ProviderStore
	Agents() AgentStore
	Memories() MemoryStore
	MemoryEvents() MemoryEventStore
	MemoryNotes() MemoryNoteStore
	SessionEvents() SessionEventStore
	SessionCheckpoints() SessionCheckpointStore
	APIKeys() WorkspaceAPIKeyStore
	WorkspaceSkills() WorkspaceSkillStore
	ToolSettings() ToolSettingsStore
	WorkspaceMCPServers() WorkspaceMCPServers
	AgentMCPServers() AgentMCPServers
	Connections() Connections
	Hooks() HookStore
	Channels() ChannelStore
	WorkSessions() WorkSessionStore
	AgentSessions() AgentSessionStore
	Attachments() AttachmentStore
	WorkspaceStorage() WorkspaceStorageStore
	Schedulers() SchedulerStore
	Heartbeats() HeartbeatStore
	MemoryReports() MemoryReportStore
	MemoryEmbeddings() MemoryEmbeddingStore
	MemoryEntities() MemoryEntityStore
	Todos() TodoStore
	Gateways() GatewayStore
	GatewayBindings() GatewayBindings
	GatewayLinks() GatewayLinks
	GatewayOutbox() GatewayOutbox
	WithTx(ctx context.Context, fn func(Store) error) error
	Close() error
}

// WorkspaceAPIKeyStore manages workspace-scoped API keys used to authenticate
// /v1 (OpenResponses) requests. Management reads are workspace-scoped; the
// hash lookup is global because it performs authentication.
type WorkspaceAPIKeyStore interface {
	Create(ctx context.Context, key *domain.WorkspaceAPIKey) error
	List(ctx context.Context, workspaceID string) ([]domain.WorkspaceAPIKey, error)
	// Revoke sets revoked_at on the key. Revoking an already-revoked key or a
	// key belonging to another workspace returns domain.ErrNotFound.
	Revoke(ctx context.Context, workspaceID, id string) error
	// LookupByHash finds a key by its SHA-256 hex hash (global, for authn).
	// Unknown hashes return domain.ErrNotFound.
	LookupByHash(ctx context.Context, keyHash string) (*domain.WorkspaceAPIKey, error)
}

// WorkspaceSkillStore manages the workspace skill registry: one row per
// workspace-level skill. All operations are workspace-scoped; the skill body
// itself lives on disk and is managed by the install service. ListEnabled
// resolves by workspace slug because the runtime reader works from slugs.
type WorkspaceSkillStore interface {
	Create(ctx context.Context, skill *domain.WorkspaceSkill) error
	Get(ctx context.Context, workspaceID, id string) (*domain.WorkspaceSkill, error)
	GetByName(ctx context.Context, workspaceID, name string) (*domain.WorkspaceSkill, error)
	List(ctx context.Context, workspaceID string) ([]domain.WorkspaceSkill, error)
	// ListEnabled returns the names of enabled skills for a workspace slug.
	// Unknown slugs return domain.ErrNotFound.
	ListEnabled(ctx context.Context, workspaceSlug string) ([]string, error)
	Update(ctx context.Context, skill *domain.WorkspaceSkill) error
	SetEnabled(ctx context.Context, workspaceID, id string, enabled bool) error
	Delete(ctx context.Context, workspaceID, id string) error
}

// ToolSettingsStore manages workspace-scoped per-tool settings: the global
// enable toggle plus the tool's structured configuration. Absence of a row
// means the tool is enabled with its default configuration — Get returns
// domain.ErrNotFound for that case and callers apply defaults.
type ToolSettingsStore interface {
	Get(ctx context.Context, workspaceID, toolKey string) (*domain.WorkspaceToolSetting, error)
	Upsert(ctx context.Context, setting *domain.WorkspaceToolSetting) error
	List(ctx context.Context, workspaceID string) ([]domain.WorkspaceToolSetting, error)
}

// AgentStore manages workspace-scoped agents.
type AgentStore interface {
	Create(ctx context.Context, agent *domain.Agent) error
	ByID(ctx context.Context, workspaceID, id string) (*domain.Agent, error)
	BySlug(ctx context.Context, workspaceID, slug string) (*domain.Agent, error)
	ListForWorkspace(ctx context.Context, workspaceID string) ([]domain.Agent, error)
	Update(ctx context.Context, agent *domain.Agent) error
	Delete(ctx context.Context, workspaceID, id string) error
	CountByProvider(ctx context.Context, workspaceID, providerID string) (int, error)
	// CountInheriting counts the workspace's agents that carry the empty
	// provider/model pair (provider_id IS NULL) — the workspace-default-model
	// inheritors. The workspace clear-default guard reads it to refuse the
	// clear with the inheriting count.
	CountInheriting(ctx context.Context, workspaceID string) (int, error)
	SetPromptState(ctx context.Context, workspaceID, id string, status domain.PromptsStatus, promptsErr *string) error
	SweepGenerating(ctx context.Context, errMsg string) (int64, error)
}

// MemoryStore manages the two agent-memory documents: user memory (USER.md —
// one doc per workspace+user) and shared workspace memory (WORKSPACE.md — one
// doc per workspace). All operations are workspace-scoped by their arguments.
//
// Get methods return (nil, nil) when no memory is stored — absence is a normal
// state, not an error. Upsert* REPLACES the stored content (HTTP PUT
// semantics). Append* concatenates atomically (single statement in the
// postgres adapter — no read-modify-write race) and enforces the shared
// domain.MaxMemoryContentChars cap on the resulting document, returning a
// wrapped domain.ErrMemoryCapExceeded when blocked.
type MemoryStore interface {
	UserMemory(ctx context.Context, workspaceID, userID string) (*domain.Memory, error)
	UpsertUserMemory(ctx context.Context, workspaceID, userID, content string) error
	AppendUserMemory(ctx context.Context, workspaceID, userID, content string) error
	WorkspaceMemory(ctx context.Context, workspaceID string) (*domain.Memory, error)
	UpsertWorkspaceMemory(ctx context.Context, workspaceID, content string) error
	AppendWorkspaceMemory(ctx context.Context, workspaceID, content string) error
}

// MemoryTimeWindow bounds a memory read to rows whose event_time — when the
// fact was true — falls inside [From, To). Zero fields are open ends.
type MemoryTimeWindow struct {
	From time.Time
	To   time.Time
}

// MemoryEventFilters narrows episodic-gist reads. Zero-value fields mean "no
// filter"; Limit <= 0 means "no limit" (the LoadSessionEventsParams
// convention). Tombstoned events are never returned, with or without filters.
type MemoryEventFilters struct {
	SessionID  string
	AgentID    string
	Visibility domain.MemoryVisibility
	TimeWindow *MemoryTimeWindow
	Limit      int
}

// MemoryNoteFilters narrows curated-note reads. Zero-value fields mean "no
// filter"; Limit <= 0 means "no limit". History widens a listing past the
// current-state view to include superseded rows — the what-changed view
// (integrate-agent-zero-memory D6); tombstoned rows are hidden from every
// read path and no flag overrides that.
type MemoryNoteFilters struct {
	Visibility domain.MemoryVisibility
	Topic      string
	TimeWindow *MemoryTimeWindow
	History    bool
	Limit      int
}

// MemoryEventStore manages the episodic gist timeline (D3): one row per
// processed window of a session, written by the background gister.
//
// Every read is scope-filtered twice: by workspaceID (the tenant partition —
// no query runs without it) and by caller identity (viewerUserID +
// servingAgentID) computing visible = shared rows ∪ the viewer's own
// user-visibility rows ∪ the serving agent's agent-visibility rows
// structurally in the query, never by post-filtering (D4, D8). An empty
// viewerUserID or servingAgentID simply contributes no rows of that tier.
//
// Absence conventions mirror MemoryStore: LatestEventForSession returns
// (nil, nil) when the session has no gist yet, and mutations on rows that are
// absent or already tombstoned return domain.ErrNotFound (a tombstoned row is
// indistinguishable from an absent one — no existence leak). Writes validate
// the provenance birth tuple and owner shape via the domain layer (D5) and
// are rejected with wrapped sentinels.
type MemoryEventStore interface {
	// InsertEvent appends one gist row, assigning ID when empty.
	InsertEvent(ctx context.Context, event *domain.MemoryEvent) error
	// LatestEventForSession returns the newest non-tombstoned gist for the
	// gister's incremental window cursor; (nil, nil) when none.
	LatestEventForSession(ctx context.Context, workspaceID, sessionID string) (*domain.MemoryEvent, error)
	// ListEventsForUI returns the caller-visible timeline, newest first.
	ListEventsForUI(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, filters MemoryEventFilters) ([]domain.MemoryEvent, error)
	// SearchEvents runs the hybrid lexical search (tsvector OR trigram, D9)
	// over description + outcome within the caller's visible set. An empty
	// query is invalid input.
	SearchEvents(ctx context.Context, workspaceID, viewerUserID, servingAgentID, query string, filters MemoryEventFilters) ([]domain.MemoryEvent, error)
	// GetEventsByIDs returns the caller-visible live events with the given
	// ids, newest first. The structural visible-set predicate applies exactly
	// as on every other read: missing, foreign-workspace, tombstoned, and
	// invisible ids are simply absent from the result, never an error.
	// Duplicate ids collapse; an empty id list returns an empty slice. The
	// batch read the entity traversal's depth-1 expansion rides
	// (wave3-memory-vectors-and-graph D8).
	GetEventsByIDs(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, ids []string) ([]domain.MemoryEvent, error)
	// TombstoneEvent sets tombstoned_at, hiding the gist from every read
	// path (D6). Absent or already-tombstoned rows return domain.ErrNotFound.
	TombstoneEvent(ctx context.Context, workspaceID, id string) error
	// CountByVisibility returns the non-tombstoned per-turn gist counts by
	// visibility tier — the chip's breakdown, never content. The map is
	// empty when nothing was stored for the turn.
	CountByVisibility(ctx context.Context, workspaceID, sessionID, turnID string) (map[domain.MemoryVisibility]int, error)
}

// MemoryNoteStore manages the curated semantic fact store: gate-extracted
// facts with provenance stamped at birth, supersede chains, tombstones, and
// audited human promotion.
//
// Scope-filtering follows MemoryEventStore: workspaceID partition plus the
// structural visible = shared ∪ own-user ∪ serving-agent predicate on every
// read. Absence conventions also match: GetNote returns (nil, nil) for
// unknown, foreign-visible, or tombstoned notes, and single-row mutations on
// such notes return domain.ErrNotFound. All writes validate the provenance
// birth tuple via the domain layer (D5); note writes additionally enforce the
// caller-supplied visibility ceiling (D4) — the store is the last line of
// defense behind the gate's own op validation.
type MemoryNoteStore interface {
	// InsertNote stores one fact, assigning ID when empty. The ceiling must
	// dominate the note's proposed visibility (wrapped
	// domain.ErrMemoryVisibilityExceeded otherwise).
	InsertNote(ctx context.Context, note *domain.MemoryNote, ceiling domain.MemoryVisibility) error
	// SupersedeNote atomically inserts note as the correction of oldID and
	// stamps oldID.superseded_by with the new row's ID in one statement
	// (D6 — nothing is overwritten, history stays answerable). The old note
	// must exist, be visible-scope-clean (not tombstoned), and not already
	// superseded: the first two cases return domain.ErrNotFound, the last
	// domain.ErrConflict. The returned note carries Supersedes = oldID.
	SupersedeNote(ctx context.Context, workspaceID, oldID string, note *domain.MemoryNote, ceiling domain.MemoryVisibility) error
	// TombstoneNote sets tombstoned_at, hiding the note from every read path
	// (D6). Absent or already-tombstoned notes return domain.ErrNotFound.
	TombstoneNote(ctx context.Context, workspaceID, id string) error
	// PromoteNote widens a note to shared — the only widening path, human
	// initiated and audited via promoted_by/promoted_at (D4). The promoting
	// user must be named. Absent, tombstoned, or superseded notes return
	// domain.ErrNotFound; already-shared notes return domain.ErrConflict.
	PromoteNote(ctx context.Context, workspaceID, id, promotedByUserID string) error
	// GetNote returns the note by ID within the caller's visible set;
	// (nil, nil) when no such visible non-tombstoned note exists. Superseded
	// notes are returned — history stays queryable (D6).
	GetNote(ctx context.Context, workspaceID, viewerUserID, servingAgentID, id string) (*domain.MemoryNote, error)
	// GetNotesByIDs returns the caller-visible CURRENT notes (live, not
	// superseded — the retrieval contract SearchNotes follows) with the given
	// ids, newest-learned first. The structural visible-set predicate applies
	// exactly as on every other read: missing, foreign-workspace, tombstoned,
	// superseded, and invisible ids are simply absent from the result, never
	// an error. Duplicate ids collapse; an empty id list returns an empty
	// slice. The batch read the entity traversal's depth-1 expansion rides
	// (wave3-memory-vectors-and-graph D8).
	GetNotesByIDs(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, ids []string) ([]domain.MemoryNote, error)
	// ListNotesForUI returns the caller-visible notes, pinned first then
	// newest-learned. Default lists carry the current state only; History
	// adds superseded rows. Tombstoned rows never appear.
	ListNotesForUI(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, filters MemoryNoteFilters) ([]domain.MemoryNote, error)
	// SearchNotes runs the hybrid lexical search (tsvector OR trigram, D9)
	// over content within the caller's visible set, current-state only
	// (superseded notes are dead for retrieval). An empty query is invalid
	// input.
	SearchNotes(ctx context.Context, workspaceID, viewerUserID, servingAgentID, query string, filters MemoryNoteFilters) ([]domain.MemoryNote, error)
	// CountSimilar returns how many current, caller-visible notes exceed the
	// trigram-similarity threshold (0..1) against content — the dedupe
	// candidate count the gate decides ADD vs SUPERSEDE against.
	CountSimilar(ctx context.Context, workspaceID, viewerUserID, servingAgentID, content string, threshold float64) (int, error)
	// CountByVisibility returns the workspace's non-tombstoned note counts
	// by visibility tier — aggregate counters for the memory UI, never
	// content. The map is empty when no notes exist.
	CountByVisibility(ctx context.Context, workspaceID string) (map[domain.MemoryVisibility]int, error)
	// SupersedeInto stamps an existing note as superseded BY another
	// existing current note (D6): the canonical survivor of a consolidation
	// cluster stays exactly as it is and every folded duplicate gains
	// superseded_by = intoID in one statement — nothing is inserted,
	// overwritten, or deleted, so history stays answerable. Both notes must
	// belong to the workspace; old == into is domain.ErrInvalid; a missing,
	// tombstoned, or already-superseded survivor returns domain.ErrNotFound;
	// a duplicate that is missing, tombstoned, or already superseded returns
	// domain.ErrNotFound / domain.ErrConflict like SupersedeNote.
	SupersedeInto(ctx context.Context, workspaceID, oldID, intoID string) error
	// SetNoteTopic labels one current note for the topic fold (D12). The
	// note must exist in the workspace and be live (not tombstoned, not
	// superseded) — otherwise domain.ErrNotFound; an empty or blank topic is
	// domain.ErrInvalid.
	SetNoteTopic(ctx context.Context, workspaceID, noteID, topic string) error
	// AddNoteEvidence links additional raw-event evidence on a note (D12's
	// multi-evidence): one memory_note_evidence row per source event id,
	// idempotent per (note, source event) — re-adding an existing link is a
	// no-op, and added_at keeps the first link's timestamp. Empty ids are
	// skipped; an absent or tombstoned note returns domain.ErrNotFound.
	AddNoteEvidence(ctx context.Context, workspaceID, noteID string, sourceEventIDs []string) error
	// ListNoteEvidence returns the note's multi-evidence links, oldest link
	// first. Absent or tombstoned notes return an empty slice — the caller
	// decides whether a missing note is an error; unknown links are simply
	// absent. The workspace partition scopes the read.
	ListNoteEvidence(ctx context.Context, workspaceID, noteID string) ([]domain.MemoryNoteEvidence, error)
}

// MemoryReportStore manages the per-workspace last morning report (D12):
// the consolidator saves one report per pass and the Memory pane reads the
// latest back. One row per workspace — Save replaces the previous report.
// Like MemoryStore, absence is a normal state: Get returns (nil, nil) when
// the workspace has no report yet.
type MemoryReportStore interface {
	// Save stores report (the marshaled MorningReport JSON, persisted
	// verbatim) as the workspace's last report, replacing any previous one.
	Save(ctx context.Context, workspaceID string, report []byte, generatedAt time.Time) error
	// Get returns the workspace's last report; (nil, nil) when none.
	Get(ctx context.Context, workspaceID string) (*domain.MemoryReport, error)
}

// MemoryEmbeddingFilters narrows vector-search reads. Zero-value fields mean
// "no filter"; Limit <= 0 means "no limit" (the LoadSessionEventsParams
// convention) — callers in practice always carry the shared prefetch budget.
type MemoryEmbeddingFilters struct {
	// Visibility restricts results to one tier ("" = any visible tier).
	Visibility domain.MemoryVisibility
}

// MemoryEmbeddingStore manages the polymorphic vector evidence index
// (wave3-memory-vectors-and-graph D1): embeddings of extracted notes,
// episodic events, and raw turn text in one table, with a per-row dimension
// so an embedding-model change never migrates data — the vector channel
// filters to one dimension and stale-dimension rows simply stay
// lexical-retrievable (D5).
//
// Scope-filtering follows MemoryNoteStore: workspaceID partition plus the
// structural visible = shared ∪ own-user ∪ serving-agent predicate computed
// in the query, never by post-filtering. For note/event targets the owners
// come from the LIVE linked row (promotion widens through the row, never
// through a snapshot); raw targets carry their own visibility snapshot and
// owner columns and filter the same way. Note/event embeddings whose target
// row is missing or tombstoned are dead at read time — tombstone semantics
// are modeled by exclusion in the read, not by triggers (raw rows have no
// tombstone of their own). Search results carry each row's scope fields and
// target pointer but never the vector itself.
type MemoryEmbeddingStore interface {
	// InsertEmbeddings writes one ingestion job's batch in one statement.
	// Each row is validated (target kind, dimension/vector length, owner
	// shape, evidence stamp — wrapped domain sentinels name the fault), and
	// the write is an upsert per (workspace, target, dimension): a re-embed
	// at the same dimension replaces the vector while created_at and id stay
	// as stored. An empty batch is a no-op.
	InsertEmbeddings(ctx context.Context, embeddings []domain.MemoryEmbedding) error
	// DeleteByTarget drops every embedding of one target — the primitive the
	// pipeline uses when a note is superseded or tombstoned. Returns the
	// number of rows removed.
	DeleteByTarget(ctx context.Context, workspaceID string, targetType domain.MemoryTargetType, targetID string) (int64, error)
	// SearchByVector orders the workspace's embeddings of the given target
	// kinds (nil = all kinds) at the given dimension by cosine distance to
	// query, nearest first, within the caller's visible set. dimension must
	// be positive and query must match it in length (wrapped domain.ErrInvalid
	// otherwise) — the D5 exclusion is the caller naming the workspace's
	// current dimension. Results carry TargetType, TargetID, the visibility
	// snapshot, SourceEventID (the raw citation pointer), and timestamps.
	SearchByVector(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, targetTypes []domain.MemoryTargetType, dimension int, query []float32, filters MemoryEmbeddingFilters, limit int) ([]domain.MemoryEmbedding, error)
}

// MemoryEntityStore manages the associative graph's two halves (wave3
// D7/D8/D10): tenant-scoped entity labels — pointers, not content, with NO
// visibility tier; all gating lives on edges and linked rows — and the
// bipartite edges connecting them to extracted notes and events.
//
// Entity identity is (workspaceID, NormalizeEntityLabel(label)): resolving
// the same label twice yields one row. Edge visibility is stamped at birth
// as the narrowest endpoint and never widened; edge reads take scope from
// the LIVE linked row through the same structural predicate as direct reads,
// so traversal reaches exactly as far as the direct read path does. Rows are
// never deleted: a fold re-points edges onto the surviving entity and leaves
// the folded duplicate in place (copy-out, D10).
type MemoryEntityStore interface {
	// ResolveEntity inserts entity when its normalized label is new to the
	// workspace and loads the stored row when it is not — idempotent per
	// (workspace, normalized label). On return the struct carries the stored
	// row: the FIRST label's spelling wins at birth, and identity fields are
	// never rewritten. Write-shape validation runs through the domain layer
	// (label/normalization parity, provenance stamp).
	ResolveEntity(ctx context.Context, entity *domain.MemoryEntity) error
	// GetEntityByLabel resolves the exact normalized label; (nil, nil) when
	// the workspace has no such entity.
	GetEntityByLabel(ctx context.Context, workspaceID, normalizedLabel string) (*domain.MemoryEntity, error)
	// FindEntitiesByLabelPrefix is the D8 seed fallback: exact normalized
	// match first, then normalized-label prefix, then trigram similarity on
	// the label — best candidates first. limit <= 0 means no limit.
	FindEntitiesByLabelPrefix(ctx context.Context, workspaceID, label string, limit int) ([]domain.MemoryEntity, error)
	// ListEntities lists the workspace's entities ordered by normalized label
	// then id — the consolidator's hygiene pass (wave3 D10) reads it to group
	// duplicate normalized labels before folding. limit <= 0 means no limit.
	ListEntities(ctx context.Context, workspaceID string, limit int) ([]domain.MemoryEntity, error)
	// AddEdges inserts one ingestion job's edge batch in one statement.
	// Edges referencing an entity absent from the workspace are dropped
	// (the pipeline always resolves entities first; the count reports what
	// landed), and the write is idempotent per (entity, target): re-mentioning
	// the same row links nothing twice. Each edge is validated through the
	// domain layer (note/event targets only, tier validity, provenance).
	AddEdges(ctx context.Context, workspaceID string, edges []domain.MemoryEntityEdge) (int64, error)
	// ListEdgesForEntities returns the caller-visible live edges of the given
	// entities: each edge is scope-filtered through its linked note/event row
	// (shared ∪ own-user ∪ serving-agent, computed in the query) and dies
	// with that row — edges to missing or tombstoned targets are excluded.
	// Empty entityIDs return an empty slice.
	ListEdgesForEntities(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, entityIDs []string) ([]domain.MemoryEntityEdge, error)
	// FoldEntity re-points every edge of duplicateID onto intoID (D10
	// copy-out): edge rows move, keeping their visibility, origin, and
	// source stamps; an edge the survivor already holds is left on the
	// duplicate rather than duplicated or deleted; the duplicate entity row
	// itself stays in place — no edge or provenance row is ever deleted.
	// Returns the number of edges re-pointed. Unknown or foreign entities
	// return domain.ErrNotFound; duplicate == into is domain.ErrInvalid.
	FoldEntity(ctx context.Context, workspaceID, duplicateID, intoID string) (int64, error)
}

// LoadSessionEventsParams configures query parameters for loading session events.
type LoadSessionEventsParams struct {
	WorkspaceID  string
	SessionID    string
	AfterEventID string
	// Limit bounds the returned page. Limit <= 0 means "no limit": every
	// matching event is returned. Only a positive Limit caps the page.
	Limit   int
	Reverse bool
	Kinds   []string
}

// SessionEventStore manages the append-only session event log.
type SessionEventStore interface {
	AppendEvents(ctx context.Context, workspaceID string, events []domain.SessionEvent) error
	LoadEvents(ctx context.Context, params LoadSessionEventsParams) ([]domain.SessionEvent, error)
	// NextEventSeq returns the next append position for the session's event
	// log: MAX(seq)+1 over its rows, or 0 when the log is empty. It lets
	// appenders allocate sequences without reading the whole history.
	NextEventSeq(ctx context.Context, workspaceID, sessionID string) (int64, error)
	// EventExists reports whether an event with the given ID is already
	// stored for the session (indexed existence check — not an error when
	// absent).
	EventExists(ctx context.Context, workspaceID, sessionID, eventID string) (bool, error)
	// EventsByIDs returns the workspace-scoped events with the given ids,
	// ordered deterministically by (session_id, seq). Absent and
	// foreign-workspace ids are simply absent from the result, never an
	// error; duplicate ids collapse; an empty id list returns an empty slice.
	// The read-side hydration primitive the memory searcher uses to resolve a
	// raw-evidence hit's source event id to its turn window
	// (wave3-memory-vectors-and-graph D9: the raw citation pointer IS the
	// source event id).
	EventsByIDs(ctx context.Context, workspaceID string, eventIDs []string) ([]domain.SessionEvent, error)
}

// SessionCheckpointStore manages execution checkpoints.
type SessionCheckpointStore interface {
	Get(ctx context.Context, checkpointID string) ([]byte, bool, error)
	Set(ctx context.Context, checkpointID string, data []byte) error
	Delete(ctx context.Context, checkpointID string) error
}

// ProviderStore manages workspace-scoped provider configurations.
type ProviderStore interface {
	Create(ctx context.Context, p *domain.ProviderConfig) error
	ByID(ctx context.Context, workspaceID, id string) (*domain.ProviderConfig, error)
	ListForWorkspace(ctx context.Context, workspaceID string) ([]domain.ProviderConfig, error)
	Update(ctx context.Context, p *domain.ProviderConfig) error
	Delete(ctx context.Context, workspaceID, id string) error
}

// UserStore manages user identities.
type UserStore interface {
	Create(ctx context.Context, u *domain.User) error
	ByEmail(ctx context.Context, email string) (*domain.User, error)
	ByID(ctx context.Context, id string) (*domain.User, error)
	List(ctx context.Context) ([]domain.User, error)
	SetDisabled(ctx context.Context, id string, at *time.Time) error
	SetPasswordHash(ctx context.Context, id string, hash string) error
	Update(ctx context.Context, u *domain.User) error
}

// WorkspaceStore manages workspaces (tenants).
type WorkspaceStore interface {
	Create(ctx context.Context, ws *domain.Workspace) error
	BySlug(ctx context.Context, slug string) (*domain.Workspace, error)
	ByID(ctx context.Context, id string) (*domain.Workspace, error)
	Update(ctx context.Context, ws *domain.Workspace) error
	ListForUser(ctx context.Context, userID string) ([]domain.Workspace, error)
	ListAll(ctx context.Context) ([]domain.Workspace, error)
}

// RoleStore manages workspace roles.
type RoleStore interface {
	Create(ctx context.Context, r *domain.Role) error
	ByID(ctx context.Context, id string) (*domain.Role, error)
	FindByName(ctx context.Context, workspaceID, name string) (*domain.Role, error)
	ListForWorkspace(ctx context.Context, workspaceID string) ([]domain.Role, error)
	CountMembers(ctx context.Context, roleID string) (int, error)
}

// MemberStore manages workspace memberships and role bindings.
type MemberStore interface {
	Add(ctx context.Context, m *domain.Member) error
	Get(ctx context.Context, workspaceID, userID string) (*domain.Member, error)
	ListForWorkspace(ctx context.Context, workspaceID string) ([]domain.Member, error)
	ListForUser(ctx context.Context, userID string) ([]domain.MemberView, error)
	UpdateRole(ctx context.Context, workspaceID, userID, roleID string) error
	Remove(ctx context.Context, workspaceID, userID string) error
}

// AgentSessionStore manages the durable per-user agent session index
// (agent-session-index D1): one row per (workspace, agent, session) recording
// that an agent chat session exists, with its birth-derived title and
// last-activity time. Rows are written at run start by the runner (D2) and
// read by the per-user session listing; deletion is soft.
//
// Privacy: the index is private per user. Listing filters on the requesting
// user and SoftDelete is scoped to the owning user, so a foreign user's row
// is indistinguishable from an absent one (domain.ErrNotFound — no existence
// leak). The unique (workspace_id, agent_id, session_id) triple keeps one row
// per session; ownership is fixed at birth and never rewritten.
type AgentSessionStore interface {
	// UpsertAgentSession indexes one persistent run at run start (design D2):
	// the birth turn inserts the row with the input-derived title and
	// created_at = last_active_at = now; a later turn bumps last_active_at,
	// clears deleted_at (a soft-deleted session re-chatted revives — the
	// transcript is still on disk), and applies the birth-only title rule:
	// EXCLUDED.title wins only while the stored title is still '', so
	// empty-title upserts and later turns never retitle. The owning user_id
	// is fixed at birth (the conflict path does not rewrite it).
	UpsertAgentSession(ctx context.Context, workspaceID, agentID, userID string, up domain.AgentSessionUpsert) error
	// ListAgentSessions returns the user's non-deleted sessions for the
	// agent in the workspace, ordered by last activity (newest first, id as
	// the determinism tiebreak). Soft-deleted rows are never returned. An
	// empty result is an empty slice, not nil.
	ListAgentSessions(ctx context.Context, workspaceID, agentID, userID string) ([]domain.AgentSession, error)
	// SoftDeleteAgentSession sets deleted_at = now() on the caller's own row
	// (workspace + agent + user + session scoped). A row owned by another
	// user, or absent entirely, returns domain.ErrNotFound — foreign-owned
	// and unknown are indistinguishable.
	SoftDeleteAgentSession(ctx context.Context, workspaceID, agentID, userID, sessionID string) error
}

// SchedulerClaim is one result of a due-claim batch: Missed marks a once
// scheduler that was overdue beyond the grace window — it was archived
// (enabled=false, next_run_at=nil, last_run status "missed") instead of fired.
type SchedulerClaim struct {
	Scheduler *domain.Scheduler
	Missed    bool
}

// SchedulerStore manages workspace-scoped schedulers (named standing orders
// firing an agent on a recurrence or one-shot instant) and their run records.
// Every method is workspace-scoped — no query runs without a workspace scope —
// except ClaimDueSchedulers, which is intentionally global: it serves the
// ticker, and every claimed row still carries its workspace.
type SchedulerStore interface {
	CreateScheduler(ctx context.Context, workspaceID string, s *domain.Scheduler) error
	GetScheduler(ctx context.Context, workspaceID, id string) (*domain.Scheduler, error) // (nil, nil) when absent
	ListSchedulers(ctx context.Context, workspaceID string) ([]domain.Scheduler, error)
	UpdateScheduler(ctx context.Context, workspaceID string, s *domain.Scheduler) error
	DeleteScheduler(ctx context.Context, workspaceID, id string) error
	// ClaimDueSchedulers atomically claims up to limit enabled schedulers whose
	// next occurrence (recurring next_run_at, once run_at) is due at now, so
	// concurrent claimers never fire the same occurrence: rows are selected
	// FOR UPDATE SKIP LOCKED inside one transaction and each recurring row's
	// next_run_at is advanced to its next future occurrence (computed in the
	// workspace timezone via domain.NextRun) in that same transaction. A once
	// row overdue beyond the missed grace window (1h constant) is instead
	// marked missed (enabled=false, next_run_at=nil, last_run status "missed")
	// and returned with Missed=true — never fired. Uses DB now() semantics
	// anchored at the passed now.
	ClaimDueSchedulers(ctx context.Context, now time.Time, limit int) ([]SchedulerClaim, error)
	StartSchedulerRun(ctx context.Context, run *domain.SchedulerRun) error // inserts with status running
	// FinishSchedulerRun writes the run outcome — including the turn's
	// persisted Langfuse trace id (integrate-langfuse-tracing D3; "" when
	// untraced) — and mirrors it into the scheduler's last_run column
	// atomically.
	FinishSchedulerRun(ctx context.Context, workspaceID, runID string, status string, durationMS int64, tokensUsed int, deliveryStatus string, errMsg string, traceID string) error
	ListSchedulerRuns(ctx context.Context, workspaceID, schedulerID string, limit, offset int) ([]domain.SchedulerRun, int, error) // newest-first + total
}

// HeartbeatClaim is one result of a due-claim batch. A heartbeat has no once
// kind — a catch-up tick fires once and reschedules without replaying — so
// there is no Missed concept (add-agent-heartbeat D14).
type HeartbeatClaim struct {
	Heartbeat *domain.Heartbeat
}

// HeartbeatStore manages workspace-scoped agent heartbeats (exactly one per
// agent, add-agent-heartbeat D1) and their per-tick run records. Every method
// is workspace-scoped — no query runs without a workspace scope — except
// ClaimDueHeartbeats, which is intentionally global: it serves the ticker,
// and every claimed row still carries its workspace.
type HeartbeatStore interface {
	// GetHeartbeat returns the agent's heartbeat; (nil, nil) when the agent
	// has none — absence is a normal state, not an error.
	GetHeartbeat(ctx context.Context, workspaceID, agentID string) (*domain.Heartbeat, error)
	// PutHeartbeat create-or-replaces the agent's single heartbeat
	// (INSERT ... ON CONFLICT (workspace_id, agent_id) DO UPDATE), enforcing
	// the exactly-one-per-agent invariant at the data layer. The store
	// persists the struct as given — enabled, next_tick_at, failure_streak
	// and the last_tick snapshot included — because derivation is
	// caller-side: validation and the next-tick computation run in the
	// domain/service layers, which then persist the recomputed state
	// (add-agent-heartbeat D4: derived fields are never stored from request
	// input). Identity is immutable on replace: id, created_by and
	// created_at stay as stored, and the input struct is written back with
	// them so it stays faithful.
	PutHeartbeat(ctx context.Context, workspaceID, agentID string, hb *domain.Heartbeat) error
	// DeleteHeartbeat removes the agent's heartbeat; its run records die
	// with it (ON DELETE CASCADE). An absent heartbeat returns
	// domain.ErrNotFound, matching DeleteScheduler semantics.
	DeleteHeartbeat(ctx context.Context, workspaceID, agentID string) error
	// ClaimDueHeartbeats atomically claims up to limit enabled heartbeats
	// whose next_tick_at is due at now, so concurrent claimers never fire the
	// same tick twice: rows are selected FOR UPDATE SKIP LOCKED inside one
	// transaction and each row's next_tick_at is advanced to its next future
	// occurrence (computed in the workspace timezone via domain.NextRun) in
	// that same transaction — a missed catch-up fires once and reschedules,
	// never replaying skipped occurrences (add-agent-heartbeat D14). A row
	// whose expression no longer parses fails the batch, mirroring
	// ClaimDueSchedulers. Uses DB now() semantics anchored at the passed now.
	ClaimDueHeartbeats(ctx context.Context, now time.Time, limit int) ([]HeartbeatClaim, error)
	StartHeartbeatRun(ctx context.Context, run *domain.HeartbeatRun) error // inserts with status running
	// FinishHeartbeatRun writes the tick outcome — including the turn's
	// persisted Langfuse trace id (000050 precedent; "" when untraced) — and
	// mirrors it into the heartbeat's last_tick snapshot atomically.
	FinishHeartbeatRun(ctx context.Context, workspaceID, runID string, status string, durationMS int64, tokensUsed int, deliveryStatus string, errMsg string, traceID string) error
	// ApplyHeartbeatOutcome applies failure-streak accounting in one atomic
	// statement (add-agent-heartbeat D12): status completed resets the
	// streak; status failed increments it and, when it reaches five
	// consecutive failures, auto-pauses the heartbeat (enabled=false,
	// next_tick_at=NULL) returning paused=true. Every other status — blocked
	// policy grief, cancelled, skipped guards — leaves the streak untouched.
	ApplyHeartbeatOutcome(ctx context.Context, workspaceID, heartbeatID string, status string) (paused bool, err error)
	ListHeartbeatRuns(ctx context.Context, workspaceID, heartbeatID string, limit, offset int) ([]domain.HeartbeatRun, int, error) // newest-first + total
}

// DSNConfig holds connection parameters for store drivers.
type DSNConfig struct {
	DSN string
}

// DriverOpenFunc creates a Store instance from DSN configuration.
type DriverOpenFunc func(ctx context.Context, cfg DSNConfig) (Store, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]DriverOpenFunc)
)

// Register registers a store driver by name. It panics if a driver with the same name is registered twice.
func Register(name string, open DriverOpenFunc) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("store driver %q already registered", name))
	}
	registry[name] = open
}

// Open opens a store using the named registered driver.
func Open(ctx context.Context, name string, cfg DSNConfig) (Store, error) {
	registryMu.RLock()
	open, exists := registry[name]
	registryMu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("%w: unknown store driver %q", domain.ErrInvalid, name)
	}
	return open(ctx, cfg)
}
