package store

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// HookStore manages agent lifecycle hooks across the three governance levels
// (design.md D13) plus the shared execution audit log (D16):
//
//   - Instance hooks reach every workspace and agent. They are the ONE
//     deliberately workspace-unscoped entity and are reachable only through
//     the instance-admin surface (D13/D15). Builtin-sourced rows are owned by
//     the release sync pipeline (UpsertBuiltinHook / DeleteMissingBuiltinHooks)
//     and are read-only through the CRUD methods.
//   - Workspace hooks are always-on for every agent in their workspace.
//   - Agent hooks are private to one agent.
//
// Tenant isolation: every workspace- and agent-level method carries a
// workspaceID predicate — no query runs without it — and a hook belonging to
// another workspace is indistinguishable from an unknown id.
// ListHookExecutions refuses to run without a workspace scope.
//
// Getters follow the Get-with-nil precedent (MemoryStore): absence — including
// unknown and malformed ids — returns (nil, nil), not an error sentinel.
// Deletes return domain.ErrNotFound when the row is absent or out of scope.
//
// Ordering follows D14: the list order IS the execution order. Create appends
// to the end of its level's list (position = previous max + 1); Reposition*
// sets position = slice index and is the only way to reorder; Update never
// touches position. List methods order by position, then created_at/id.
type HookStore interface {
	// ----- Instance level (workspace-unscoped by design; D13/D15) -----

	ListInstanceHooks(ctx context.Context) ([]domain.InstanceHook, error)
	GetInstanceHook(ctx context.Context, id string) (*domain.InstanceHook, error)
	// CreateInstanceHook persists a managed instance hook. source MUST be
	// 'managed' — builtin rows are created only by UpsertBuiltinHook (D15).
	CreateInstanceHook(ctx context.Context, hook *domain.InstanceHook) error
	// UpdateInstanceHook replaces the hook's definition and health fields
	// (name, event, matcher, handler type, config, timeout, on_failure,
	// enabled, status, status_error). position, created_at, key, source, and
	// version are owned by other methods and are not written. Builtin rows are
	// read-only: updating one returns domain.ErrInvalid.
	UpdateInstanceHook(ctx context.Context, hook *domain.InstanceHook) error
	// DeleteInstanceHook removes a managed instance hook. Builtin rows are
	// read-only; they are removed via DeleteMissingBuiltinHooks (D15).
	DeleteInstanceHook(ctx context.Context, id string) error
	// RepositionInstanceHooks sets position = slice index for each listed id;
	// ids not present are ignored.
	RepositionInstanceHooks(ctx context.Context, ids []string) error
	// UpsertBuiltinHook inserts or version-guard-updates one instance hook by
	// (source, key) per D15: an update applies only when the stored version is
	// older than the incoming one. Re-running the same version is an idempotent
	// no-op, a newer stored row is NEVER downgraded, and a lost race with an
	// older version is a silent no-op, not an error. On return the hook
	// reflects the stored row.
	UpsertBuiltinHook(ctx context.Context, hook *domain.InstanceHook) error
	// DeleteMissingBuiltinHooks deletes source='builtin' rows whose key is not
	// in keepKeys (an empty keepKeys list removes every builtin row). Audit
	// history survives the deletion (D15/D16).
	DeleteMissingBuiltinHooks(ctx context.Context, keepKeys []string) error

	// ----- Workspace level -----

	ListWorkspaceHooks(ctx context.Context, workspaceID string) ([]domain.WorkspaceHook, error)
	GetWorkspaceHook(ctx context.Context, workspaceID, id string) (*domain.WorkspaceHook, error)
	CreateWorkspaceHook(ctx context.Context, hook *domain.WorkspaceHook) error
	UpdateWorkspaceHook(ctx context.Context, hook *domain.WorkspaceHook) error
	DeleteWorkspaceHook(ctx context.Context, workspaceID, id string) error
	// RepositionWorkspaceHooks sets position = slice index for each listed id
	// within the workspace; ids not present in the workspace are ignored.
	RepositionWorkspaceHooks(ctx context.Context, workspaceID string, ids []string) error

	// ----- Agent level -----

	ListAgentHooks(ctx context.Context, workspaceID, agentID string) ([]domain.AgentHook, error)
	GetAgentHook(ctx context.Context, workspaceID, agentID, id string) (*domain.AgentHook, error)
	CreateAgentHook(ctx context.Context, hook *domain.AgentHook) error
	UpdateAgentHook(ctx context.Context, hook *domain.AgentHook) error
	DeleteAgentHook(ctx context.Context, workspaceID, agentID, id string) error
	// RepositionAgentHooks sets position = slice index for each listed id
	// within the workspace+agent scope; ids not present are ignored.
	RepositionAgentHooks(ctx context.Context, workspaceID, agentID string, ids []string) error

	// ----- Health (per-delivery status; D16/D7) -----

	// SetHookDeliveryStatus updates one hook's health fields (status,
	// status_error, updated_at) after a delivery attempt, addressed by
	// governance level + hook id — the level selects the table, mirroring how
	// the dispatcher resolved the hook. Callers only pass ids read from that
	// same level's workspace-scoped list in the same request, so id-only
	// addressing never crosses tenants; unknown ids (hook deleted mid-run) are
	// an idempotent no-op.
	SetHookDeliveryStatus(ctx context.Context, level domain.HookLevel, hookID string, status domain.HookStatus, statusError string) error

	// ----- Executions (audit log; D16) -----

	// RecordHookExecution appends one audit record. Detail is truncated to 256
	// characters at write time.
	RecordHookExecution(ctx context.Context, exec *domain.HookExecution) error
	// ListHookExecutions returns the workspace's execution history, newest
	// first (created_at DESC with a stable tiebreak). hookID nil selects every
	// hook in the workspace; limit <= 0 returns all matching records. An empty
	// workspaceID returns an empty result — the query never runs unscoped.
	ListHookExecutions(ctx context.Context, workspaceID string, hookID *string, limit int) ([]domain.HookExecution, error)
}
