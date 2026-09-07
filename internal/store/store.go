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
	AgentUserMemories() AgentUserMemoryStore
	SessionEvents() SessionEventStore
	SessionCheckpoints() SessionCheckpointStore
	APIKeys() WorkspaceAPIKeyStore
	WorkspaceSkills() WorkspaceSkillStore
	ToolSettings() ToolSettingsStore
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
	SetPromptState(ctx context.Context, workspaceID, id string, status domain.PromptsStatus, promptsErr *string) error
	SweepGenerating(ctx context.Context, errMsg string) (int64, error)
}

// AgentUserMemoryStore manages per-user persistent memory for agents.
type AgentUserMemoryStore interface {
	Get(ctx context.Context, workspaceID, agentID, userID string) (*domain.AgentUserMemory, error)
	Upsert(ctx context.Context, memory *domain.AgentUserMemory) error
	Delete(ctx context.Context, workspaceID, agentID, userID string) error
}

// LoadSessionEventsParams configures query parameters for loading session events.
type LoadSessionEventsParams struct {
	WorkspaceID  string
	SessionID    string
	AfterEventID string
	Limit        int
	Reverse      bool
	Kinds        []string
}

// SessionEventStore manages the append-only session event log.
type SessionEventStore interface {
	AppendEvents(ctx context.Context, workspaceID string, events []domain.SessionEvent) error
	LoadEvents(ctx context.Context, params LoadSessionEventsParams) ([]domain.SessionEvent, error)
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
