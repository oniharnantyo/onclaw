package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// hookStore implements storeport.HookStore for PostgreSQL across the four hook
// tables (design.md D13/D15/D16). workspace_hooks and agent_hooks queries are
// ALWAYS workspace-scoped (tenant isolation — agent-level queries carry both
// predicates even though agent_id alone would address the row);
// instance_hooks is the one deliberately workspace-unscoped table and is
// reachable only through the instance surface.
type hookStore struct {
	db Executor
}

// NewHookStore creates a new HookStore with the given database executor.
func NewHookStore(db Executor) storeport.HookStore {
	return &hookStore{db: db}
}

// maxHookExecutionDetailChars caps the audit detail at write time (D16).
const maxHookExecutionDetailChars = 256

const instanceHookColumns = `id, key, source, version, name, event, matcher, if_rule, handler_type, config,
	timeout_ms, on_failure, enabled, position, status, status_error, created_at, updated_at`

const workspaceHookColumns = `id, workspace_id, name, event, matcher, if_rule, handler_type, config,
	timeout_ms, on_failure, enabled, position, status, status_error, created_at, updated_at`

const agentHookColumns = `id, workspace_id, agent_id, name, event, matcher, if_rule, handler_type, config,
	timeout_ms, on_failure, enabled, position, status, status_error, created_at, updated_at`

const hookExecutionColumns = `id, hook_id, hook_name, hook_level, workspace_id, event, decision,
	duration_ms, exit_code, http_status, detail, token_count, origin, created_at`

// normalizeHookBase applies the schema's column defaults (on_failure 'allow',
// status 'ok') so empty enum fields round-trip identically in the fake.
func normalizeHookBase(base *domain.HookBase) {
	if base.OnFailure == "" {
		base.OnFailure = domain.HookFailureAllow
	}
	if base.Status == "" {
		base.Status = domain.HookStatusOK
	}
}

// truncateHookDetail caps the audit detail at write time (rune-safe, matching
// the fake and left(detail, 256) semantics).
func truncateHookDetail(detail string) string {
	runes := []rune(detail)
	if len(runes) <= maxHookExecutionDetailChars {
		return detail
	}
	return string(runes[:maxHookExecutionDetailChars])
}

// hookConfigJSONB normalizes config for the NOT NULL jsonb column: nil/empty
// persists as the empty object.
func hookConfigJSONB(config json.RawMessage) []byte {
	if len(config) == 0 {
		return []byte("{}")
	}
	return []byte(config)
}

// isHookUniqueViolation reports whether the error is a unique violation on one
// of the hook tables' business constraints (as opposed to the primary key).
func isHookUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgerrcode.UniqueViolation &&
			(pgErr.ConstraintName == "uq_instance_hooks_source_key" ||
				pgErr.ConstraintName == "uq_workspace_hooks_workspace_id_name" ||
				pgErr.ConstraintName == "uq_agent_hooks_agent_id_name")
	}
	return false
}

func scanInstanceHook(row pgx.Row) (*domain.InstanceHook, error) {
	var h domain.InstanceHook
	var config []byte
	err := row.Scan(
		&h.ID,
		&h.Key,
		&h.Source,
		&h.Version,
		&h.Name,
		&h.Event,
		&h.Matcher,
		&h.If,
		&h.HandlerType,
		&config,
		&h.TimeoutMS,
		&h.OnFailure,
		&h.Enabled,
		&h.Position,
		&h.Status,
		&h.StatusError,
		&h.CreatedAt,
		&h.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if err := decodeHookConfig(config, &h.HookBase); err != nil {
		return nil, err
	}
	return &h, nil
}

func scanWorkspaceHook(row pgx.Row) (*domain.WorkspaceHook, error) {
	var h domain.WorkspaceHook
	var config []byte
	err := row.Scan(
		&h.ID,
		&h.WorkspaceID,
		&h.Name,
		&h.Event,
		&h.Matcher,
		&h.If,
		&h.HandlerType,
		&config,
		&h.TimeoutMS,
		&h.OnFailure,
		&h.Enabled,
		&h.Position,
		&h.Status,
		&h.StatusError,
		&h.CreatedAt,
		&h.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if err := decodeHookConfig(config, &h.HookBase); err != nil {
		return nil, err
	}
	return &h, nil
}

func scanAgentHook(row pgx.Row) (*domain.AgentHook, error) {
	var h domain.AgentHook
	var config []byte
	err := row.Scan(
		&h.ID,
		&h.WorkspaceID,
		&h.AgentID,
		&h.Name,
		&h.Event,
		&h.Matcher,
		&h.If,
		&h.HandlerType,
		&config,
		&h.TimeoutMS,
		&h.OnFailure,
		&h.Enabled,
		&h.Position,
		&h.Status,
		&h.StatusError,
		&h.CreatedAt,
		&h.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if err := decodeHookConfig(config, &h.HookBase); err != nil {
		return nil, err
	}
	return &h, nil
}

// decodeHookConfig copies the config jsonb verbatim into the raw message.
// matcher and if_rule are plain text columns (migration 000029 / 000028) and
// scan directly into their string fields.
func decodeHookConfig(config []byte, base *domain.HookBase) error {
	if len(config) > 0 {
		base.Config = make(json.RawMessage, len(config))
		copy(base.Config, config)
	} else {
		base.Config = json.RawMessage("{}")
	}
	return nil
}

func scanHookExecution(row pgx.Row) (*domain.HookExecution, error) {
	var e domain.HookExecution
	var hookID, workspaceID *string
	err := row.Scan(
		&e.ID,
		&hookID,
		&e.HookName,
		&e.HookLevel,
		&workspaceID,
		&e.Event,
		&e.Decision,
		&e.DurationMS,
		&e.ExitCode,
		&e.HTTPStatus,
		&e.Detail,
		&e.TokenCount,
		&e.Origin,
		&e.CreatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	e.HookID = hookID
	if workspaceID != nil {
		e.WorkspaceID = *workspaceID
	}
	return &e, nil
}

// getWithNil converts the not-found shape of a getter into the Get-with-nil
// contract (absence — unknown or malformed id — is (nil, nil)).
func getWithNil[T any](h *T, err error) (*T, error) {
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return h, nil
}

// -------------------------------------------------------------------------
// Instance level (workspace-unscoped by design; D13/D15)
// -------------------------------------------------------------------------

func (hs *hookStore) CreateInstanceHook(ctx context.Context, hook *domain.InstanceHook) error {
	if hook == nil {
		return domain.ErrInvalid
	}
	if hook.Source != domain.HookSourceManaged {
		return fmt.Errorf("%w: instance hooks are created with source %q only; builtin rows are owned by the sync pipeline", domain.ErrInvalid, domain.HookSourceManaged)
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	if hook.ID == "" {
		hook.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if hook.CreatedAt.IsZero() {
		hook.CreatedAt = now
	}
	if hook.UpdatedAt.IsZero() {
		hook.UpdatedAt = now
	}
	normalizeHookBase(&hook.HookBase)

	// Create appends to the list order (D14: API create order).
	var nextPos int
	if err := hs.db.QueryRow(ctx, `SELECT COALESCE(MAX(position) + 1, 0) FROM instance_hooks`).Scan(&nextPos); err != nil {
		return convertError(err)
	}
	hook.Position = nextPos

	query := `
		INSERT INTO instance_hooks (
			id, key, source, version, name, event, matcher, if_rule, handler_type, config,
			timeout_ms, on_failure, enabled, position, status, status_error, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		)
	`
	_, err := hs.db.Exec(ctx, query,
		hook.ID,
		hook.Key,
		hook.Source,
		hook.Version,
		hook.Name,
		hook.Event,
		hook.Matcher,
		hook.If,
		hook.HandlerType,
		hookConfigJSONB(hook.Config),
		hook.TimeoutMS,
		hook.OnFailure,
		hook.Enabled,
		hook.Position,
		hook.Status,
		hook.StatusError,
		hook.CreatedAt,
		hook.UpdatedAt,
	)
	if err != nil {
		if isHookUniqueViolation(err) {
			return fmt.Errorf("%w: instance hook with key %q already exists", domain.ErrConflict, hook.Key)
		}
		return convertError(err)
	}
	return nil
}

func (hs *hookStore) ListInstanceHooks(ctx context.Context) ([]domain.InstanceHook, error) {
	query := `
		SELECT ` + instanceHookColumns + `
		FROM instance_hooks
		ORDER BY position ASC, created_at ASC, id ASC
	`
	rows, err := hs.db.Query(ctx, query)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	hooks := make([]domain.InstanceHook, 0)
	for rows.Next() {
		h, err := scanInstanceHook(rows)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, *h)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return hooks, nil
}

func (hs *hookStore) GetInstanceHook(ctx context.Context, id string) (*domain.InstanceHook, error) {
	if id == "" {
		return nil, nil
	}

	query := `
		SELECT ` + instanceHookColumns + `
		FROM instance_hooks
		WHERE id = $1
	`
	return getWithNil(scanInstanceHook(hs.db.QueryRow(ctx, query, id)))
}

func (hs *hookStore) UpdateInstanceHook(ctx context.Context, hook *domain.InstanceHook) error {
	if hook == nil || hook.ID == "" {
		return domain.ErrInvalid
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	// Builtin rows are read-only; managed rows only. The stored source is
	// authoritative (a caller echoing a builtin row as 'managed' must not
	// bypass the guard); source never changes for a row, so the two-statement
	// read has no exploitable window.
	var source domain.HookSource
	err := hs.db.QueryRow(ctx, `SELECT source FROM instance_hooks WHERE id = $1`, hook.ID).Scan(&source)
	if err != nil {
		return convertError(err)
	}
	if source == domain.HookSourceBuiltin {
		return fmt.Errorf("%w: builtin instance hooks are read-only; ship a new version via the sync pipeline", domain.ErrInvalid)
	}

	normalizeHookBase(&hook.HookBase)

	// Editable definition + health fields only: key, source, version, position
	// and created_at are owned by other methods.
	query := `
		UPDATE instance_hooks
		SET name = $2,
		    event = $3,
		    matcher = $4,
		    if_rule = $5,
		    handler_type = $6,
		    config = $7,
		    timeout_ms = $8,
		    on_failure = $9,
		    enabled = $10,
		    status = $11,
		    status_error = $12,
		    updated_at = $13
		WHERE id = $1
		RETURNING created_at, position, key, source, version
	`
	err = hs.db.QueryRow(ctx, query,
		hook.ID,
		hook.Name,
		hook.Event,
		hook.Matcher,
		hook.If,
		hook.HandlerType,
		hookConfigJSONB(hook.Config),
		hook.TimeoutMS,
		hook.OnFailure,
		hook.Enabled,
		hook.Status,
		hook.StatusError,
		time.Now().UTC(),
	).Scan(&hook.CreatedAt, &hook.Position, &hook.Key, &hook.Source, &hook.Version)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (hs *hookStore) DeleteInstanceHook(ctx context.Context, id string) error {
	if id == "" {
		return domain.ErrNotFound
	}

	var source domain.HookSource
	err := hs.db.QueryRow(ctx, `SELECT source FROM instance_hooks WHERE id = $1`, id).Scan(&source)
	if err != nil {
		return convertError(err)
	}
	if source == domain.HookSourceBuiltin {
		return fmt.Errorf("%w: builtin instance hooks are read-only; they are removed via DeleteMissingBuiltinHooks", domain.ErrInvalid)
	}

	tag, err := hs.db.Exec(ctx, `DELETE FROM instance_hooks WHERE id = $1`, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (hs *hookStore) RepositionInstanceHooks(ctx context.Context, ids []string) error {
	now := time.Now().UTC()
	pos := 0
	for _, id := range ids {
		if id == "" {
			continue
		}
		// Ids not present are skipped without leaving gaps: the position
		// counter only advances when a row was actually repositioned.
		tag, err := hs.db.Exec(ctx,
			`UPDATE instance_hooks SET position = $2, updated_at = $3 WHERE id = $1`,
			id, pos, now,
		)
		if err != nil {
			return convertError(err)
		}
		if tag.RowsAffected() > 0 {
			pos++
		}
	}
	return nil
}

// UpsertBuiltinHook implements D15 exactly: INSERT ... ON CONFLICT (source, key)
// DO UPDATE ... WHERE instance_hooks.version < EXCLUDED.version. Idempotent
// per version, race-safe under concurrent server starts, never downgrades a
// newer stored row — a lost race with an older version is a silent no-op.
func (hs *hookStore) UpsertBuiltinHook(ctx context.Context, hook *domain.InstanceHook) error {
	if hook == nil {
		return domain.ErrInvalid
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	if hook.ID == "" {
		hook.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if hook.CreatedAt.IsZero() {
		hook.CreatedAt = now
	}
	if hook.UpdatedAt.IsZero() {
		hook.UpdatedAt = now
	}
	normalizeHookBase(&hook.HookBase)

	// A version bump ships a new definition: its health flag resets (the
	// stored status described a definition that no longer exists). Identity
	// fields (id, key, source, created_at) persist on conflict.
	query := `
		INSERT INTO instance_hooks (
			id, key, source, version, name, event, matcher, if_rule, handler_type, config,
			timeout_ms, on_failure, enabled, position, status, status_error, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		)
		ON CONFLICT (source, key) DO UPDATE SET
			version = EXCLUDED.version,
			name = EXCLUDED.name,
			event = EXCLUDED.event,
			matcher = EXCLUDED.matcher,
			if_rule = EXCLUDED.if_rule,
			handler_type = EXCLUDED.handler_type,
			config = EXCLUDED.config,
			timeout_ms = EXCLUDED.timeout_ms,
			on_failure = EXCLUDED.on_failure,
			enabled = EXCLUDED.enabled,
			position = EXCLUDED.position,
			status = 'ok',
			status_error = '',
			updated_at = EXCLUDED.updated_at
		WHERE instance_hooks.version < EXCLUDED.version
		RETURNING id, created_at, updated_at
	`
	err := hs.db.QueryRow(ctx, query,
		hook.ID,
		hook.Key,
		hook.Source,
		hook.Version,
		hook.Name,
		hook.Event,
		hook.Matcher,
		hook.If,
		hook.HandlerType,
		hookConfigJSONB(hook.Config),
		hook.TimeoutMS,
		hook.OnFailure,
		hook.Enabled,
		hook.Position,
		hook.Status,
		hook.StatusError,
		hook.CreatedAt,
		hook.UpdatedAt,
	).Scan(&hook.ID, &hook.CreatedAt, &hook.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The version guard blocked the write: the stored row is the same
			// or newer version. Surface the stored truth to the caller.
			stored, getErr := hs.getInstanceHookByKey(ctx, hook.Source, hook.Key)
			if getErr != nil {
				return getErr
			}
			if stored == nil {
				return convertError(err)
			}
			*hook = *stored
			return nil
		}
		return convertError(err)
	}
	return nil
}

func (hs *hookStore) getInstanceHookByKey(ctx context.Context, source domain.HookSource, key string) (*domain.InstanceHook, error) {
	query := `
		SELECT ` + instanceHookColumns + `
		FROM instance_hooks
		WHERE source = $1 AND key = $2
	`
	return getWithNil(scanInstanceHook(hs.db.QueryRow(ctx, query, source, key)))
}

func (hs *hookStore) DeleteMissingBuiltinHooks(ctx context.Context, keepKeys []string) error {
	// A nil slice would bind as SQL NULL and make key = ANY(...) NULL, keeping
	// every row; an empty slice deletes all builtin rows (v1 ships none).
	keep := keepKeys
	if keep == nil {
		keep = []string{}
	}
	query := `DELETE FROM instance_hooks WHERE source = 'builtin' AND NOT (key = ANY($1))`
	if _, err := hs.db.Exec(ctx, query, keep); err != nil {
		return convertError(err)
	}
	// hook_executions rows survive via the ON DELETE SET NULL trigger already
	// in the schema (migration 000026).
	return nil
}

// -------------------------------------------------------------------------
// Workspace level
// -------------------------------------------------------------------------

func (hs *hookStore) CreateWorkspaceHook(ctx context.Context, hook *domain.WorkspaceHook) error {
	if hook == nil || hook.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	if hook.ID == "" {
		hook.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if hook.CreatedAt.IsZero() {
		hook.CreatedAt = now
	}
	if hook.UpdatedAt.IsZero() {
		hook.UpdatedAt = now
	}
	normalizeHookBase(&hook.HookBase)

	// Create appends to the workspace's list order (D14: API create order).
	var nextPos int
	if err := hs.db.QueryRow(ctx,
		`SELECT COALESCE(MAX(position) + 1, 0) FROM workspace_hooks WHERE workspace_id = $1`,
		hook.WorkspaceID,
	).Scan(&nextPos); err != nil {
		return convertError(err)
	}
	hook.Position = nextPos

	query := `
		INSERT INTO workspace_hooks (
			id, workspace_id, name, event, matcher, if_rule, handler_type, config,
			timeout_ms, on_failure, enabled, position, status, status_error, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
		)
	`
	_, err := hs.db.Exec(ctx, query,
		hook.ID,
		hook.WorkspaceID,
		hook.Name,
		hook.Event,
		hook.Matcher,
		hook.If,
		hook.HandlerType,
		hookConfigJSONB(hook.Config),
		hook.TimeoutMS,
		hook.OnFailure,
		hook.Enabled,
		hook.Position,
		hook.Status,
		hook.StatusError,
		hook.CreatedAt,
		hook.UpdatedAt,
	)
	if err != nil {
		if isHookUniqueViolation(err) {
			return fmt.Errorf("%w: workspace hook %q already exists in workspace", domain.ErrConflict, hook.Name)
		}
		// Unknown workspace and cross-tenant surprises surface as the
		// workspace-not-found shape (FK violation -> ErrNotFound).
		return convertError(err)
	}
	return nil
}

func (hs *hookStore) ListWorkspaceHooks(ctx context.Context, workspaceID string) ([]domain.WorkspaceHook, error) {
	if workspaceID == "" {
		return []domain.WorkspaceHook{}, nil
	}

	query := `
		SELECT ` + workspaceHookColumns + `
		FROM workspace_hooks
		WHERE workspace_id = $1
		ORDER BY position ASC, created_at ASC, id ASC
	`
	rows, err := hs.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	hooks := make([]domain.WorkspaceHook, 0)
	for rows.Next() {
		h, err := scanWorkspaceHook(rows)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, *h)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return hooks, nil
}

func (hs *hookStore) GetWorkspaceHook(ctx context.Context, workspaceID, id string) (*domain.WorkspaceHook, error) {
	if workspaceID == "" || id == "" {
		return nil, nil
	}

	query := `
		SELECT ` + workspaceHookColumns + `
		FROM workspace_hooks
		WHERE workspace_id = $1 AND id = $2
	`
	return getWithNil(scanWorkspaceHook(hs.db.QueryRow(ctx, query, workspaceID, id)))
}

func (hs *hookStore) UpdateWorkspaceHook(ctx context.Context, hook *domain.WorkspaceHook) error {
	if hook == nil || hook.ID == "" || hook.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	normalizeHookBase(&hook.HookBase)

	// Editable definition + health fields only: position and created_at are
	// owned by other methods.
	query := `
		UPDATE workspace_hooks
		SET name = $3,
		    event = $4,
		    matcher = $5,
		    if_rule = $6,
		    handler_type = $7,
		    config = $8,
		    timeout_ms = $9,
		    on_failure = $10,
		    enabled = $11,
		    status = $12,
		    status_error = $13,
		    updated_at = $14
		WHERE workspace_id = $1 AND id = $2
		RETURNING created_at, position
	`
	err := hs.db.QueryRow(ctx, query,
		hook.WorkspaceID,
		hook.ID,
		hook.Name,
		hook.Event,
		hook.Matcher,
		hook.If,
		hook.HandlerType,
		hookConfigJSONB(hook.Config),
		hook.TimeoutMS,
		hook.OnFailure,
		hook.Enabled,
		hook.Status,
		hook.StatusError,
		time.Now().UTC(),
	).Scan(&hook.CreatedAt, &hook.Position)
	if err != nil {
		if isHookUniqueViolation(err) {
			return fmt.Errorf("%w: workspace hook %q already exists in workspace", domain.ErrConflict, hook.Name)
		}
		return convertError(err)
	}
	return nil
}

func (hs *hookStore) DeleteWorkspaceHook(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `DELETE FROM workspace_hooks WHERE workspace_id = $1 AND id = $2`
	tag, err := hs.db.Exec(ctx, query, workspaceID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (hs *hookStore) RepositionWorkspaceHooks(ctx context.Context, workspaceID string, ids []string) error {
	if workspaceID == "" {
		return nil
	}

	now := time.Now().UTC()
	pos := 0
	for _, id := range ids {
		if id == "" {
			continue
		}
		// Always workspace-scoped: a row from another workspace is untouched
		// and does not consume a position.
		tag, err := hs.db.Exec(ctx,
			`UPDATE workspace_hooks SET position = $3, updated_at = $4 WHERE workspace_id = $1 AND id = $2`,
			workspaceID, id, pos, now,
		)
		if err != nil {
			return convertError(err)
		}
		if tag.RowsAffected() > 0 {
			pos++
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// Agent level
// -------------------------------------------------------------------------

// hookAgentExistsInWorkspace guards the create path: the owning agent must
// exist and belong to the hook's workspace (the plain agent_id FK alone would
// admit cross-workspace agents).
func (hs *hookStore) hookAgentExistsInWorkspace(ctx context.Context, workspaceID, agentID string) (bool, error) {
	var one bool
	err := hs.db.QueryRow(ctx,
		`SELECT true FROM agents WHERE workspace_id = $1 AND id = $2`,
		workspaceID, agentID,
	).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, convertError(err)
	}
	return true, nil
}

func (hs *hookStore) CreateAgentHook(ctx context.Context, hook *domain.AgentHook) error {
	if hook == nil || hook.WorkspaceID == "" || hook.AgentID == "" {
		return domain.ErrInvalid
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	found, err := hs.hookAgentExistsInWorkspace(ctx, hook.WorkspaceID, hook.AgentID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}

	if hook.ID == "" {
		hook.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if hook.CreatedAt.IsZero() {
		hook.CreatedAt = now
	}
	if hook.UpdatedAt.IsZero() {
		hook.UpdatedAt = now
	}
	normalizeHookBase(&hook.HookBase)

	// Create appends to the agent's list order (D14: API create order).
	var nextPos int
	if err := hs.db.QueryRow(ctx,
		`SELECT COALESCE(MAX(position) + 1, 0) FROM agent_hooks WHERE workspace_id = $1 AND agent_id = $2`,
		hook.WorkspaceID, hook.AgentID,
	).Scan(&nextPos); err != nil {
		return convertError(err)
	}
	hook.Position = nextPos

	query := `
		INSERT INTO agent_hooks (
			id, workspace_id, agent_id, name, event, matcher, if_rule, handler_type, config,
			timeout_ms, on_failure, enabled, position, status, status_error, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
		)
	`
	_, err = hs.db.Exec(ctx, query,
		hook.ID,
		hook.WorkspaceID,
		hook.AgentID,
		hook.Name,
		hook.Event,
		hook.Matcher,
		hook.If,
		hook.HandlerType,
		hookConfigJSONB(hook.Config),
		hook.TimeoutMS,
		hook.OnFailure,
		hook.Enabled,
		hook.Position,
		hook.Status,
		hook.StatusError,
		hook.CreatedAt,
		hook.UpdatedAt,
	)
	if err != nil {
		if isHookUniqueViolation(err) {
			return fmt.Errorf("%w: agent hook %q already exists for agent", domain.ErrConflict, hook.Name)
		}
		return convertError(err)
	}
	return nil
}

func (hs *hookStore) ListAgentHooks(ctx context.Context, workspaceID, agentID string) ([]domain.AgentHook, error) {
	if workspaceID == "" || agentID == "" {
		return []domain.AgentHook{}, nil
	}

	// Always workspace-scoped, even though agent_id alone would address rows.
	query := `
		SELECT ` + agentHookColumns + `
		FROM agent_hooks
		WHERE workspace_id = $1 AND agent_id = $2
		ORDER BY position ASC, created_at ASC, id ASC
	`
	rows, err := hs.db.Query(ctx, query, workspaceID, agentID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	hooks := make([]domain.AgentHook, 0)
	for rows.Next() {
		h, err := scanAgentHook(rows)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, *h)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return hooks, nil
}

func (hs *hookStore) GetAgentHook(ctx context.Context, workspaceID, agentID, id string) (*domain.AgentHook, error) {
	if workspaceID == "" || agentID == "" || id == "" {
		return nil, nil
	}

	query := `
		SELECT ` + agentHookColumns + `
		FROM agent_hooks
		WHERE workspace_id = $1 AND agent_id = $2 AND id = $3
	`
	return getWithNil(scanAgentHook(hs.db.QueryRow(ctx, query, workspaceID, agentID, id)))
}

func (hs *hookStore) UpdateAgentHook(ctx context.Context, hook *domain.AgentHook) error {
	if hook == nil || hook.ID == "" || hook.WorkspaceID == "" || hook.AgentID == "" {
		return domain.ErrInvalid
	}
	if err := hook.Validate(nil); err != nil {
		return err
	}

	normalizeHookBase(&hook.HookBase)

	// Editable definition + health fields only: position and created_at are
	// owned by other methods. Workspace-scoped even though agent_id + id would
	// address the row.
	query := `
		UPDATE agent_hooks
		SET name = $4,
		    event = $5,
		    matcher = $6,
		    if_rule = $7,
		    handler_type = $8,
		    config = $9,
		    timeout_ms = $10,
		    on_failure = $11,
		    enabled = $12,
		    status = $13,
		    status_error = $14,
		    updated_at = $15
		WHERE workspace_id = $1 AND agent_id = $2 AND id = $3
		RETURNING created_at, position
	`
	err := hs.db.QueryRow(ctx, query,
		hook.WorkspaceID,
		hook.AgentID,
		hook.ID,
		hook.Name,
		hook.Event,
		hook.Matcher,
		hook.If,
		hook.HandlerType,
		hookConfigJSONB(hook.Config),
		hook.TimeoutMS,
		hook.OnFailure,
		hook.Enabled,
		hook.Status,
		hook.StatusError,
		time.Now().UTC(),
	).Scan(&hook.CreatedAt, &hook.Position)
	if err != nil {
		if isHookUniqueViolation(err) {
			return fmt.Errorf("%w: agent hook %q already exists for agent", domain.ErrConflict, hook.Name)
		}
		return convertError(err)
	}
	return nil
}

func (hs *hookStore) DeleteAgentHook(ctx context.Context, workspaceID, agentID, id string) error {
	if workspaceID == "" || agentID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `DELETE FROM agent_hooks WHERE workspace_id = $1 AND agent_id = $2 AND id = $3`
	tag, err := hs.db.Exec(ctx, query, workspaceID, agentID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (hs *hookStore) RepositionAgentHooks(ctx context.Context, workspaceID, agentID string, ids []string) error {
	if workspaceID == "" || agentID == "" {
		return nil
	}

	now := time.Now().UTC()
	pos := 0
	for _, id := range ids {
		if id == "" {
			continue
		}
		tag, err := hs.db.Exec(ctx,
			`UPDATE agent_hooks SET position = $4, updated_at = $5 WHERE workspace_id = $1 AND agent_id = $2 AND id = $3`,
			workspaceID, agentID, id, pos, now,
		)
		if err != nil {
			return convertError(err)
		}
		if tag.RowsAffected() > 0 {
			pos++
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// Health (per-delivery status; D16/D7)
// -------------------------------------------------------------------------

// SetHookDeliveryStatus updates one hook's health fields. The level selects
// the table (fixed whitelist, never user input), mirroring how the dispatcher
// resolved the hook; callers only pass ids read from that same level's
// workspace-scoped list in the same request, so id-only addressing never
// crosses tenants. Unknown ids (hook deleted mid-run) affect zero rows and are
// an idempotent no-op.
func (hs *hookStore) SetHookDeliveryStatus(ctx context.Context, level domain.HookLevel, hookID string, status domain.HookStatus, statusError string) error {
	if hookID == "" {
		return nil
	}
	var table string
	switch level {
	case domain.HookLevelInstance:
		table = "instance_hooks"
	case domain.HookLevelWorkspace:
		table = "workspace_hooks"
	case domain.HookLevelAgent:
		table = "agent_hooks"
	default:
		return fmt.Errorf("%w: level: unknown level %q", domain.ErrInvalid, level)
	}
	query := `UPDATE ` + table + ` SET status = $2, status_error = $3, updated_at = $4 WHERE id = $1`
	if _, err := hs.db.Exec(ctx, query, hookID, status, statusError, time.Now().UTC()); err != nil {
		return convertError(err)
	}
	return nil
}

// -------------------------------------------------------------------------
// Executions (audit log; D16)
// -------------------------------------------------------------------------

func (hs *hookStore) RecordHookExecution(ctx context.Context, exec *domain.HookExecution) error {
	if exec == nil {
		return domain.ErrInvalid
	}

	if exec.ID == "" {
		exec.ID = uuid.NewString()
	}
	if exec.CreatedAt.IsZero() {
		exec.CreatedAt = time.Now().UTC()
	}
	exec.Detail = truncateHookDetail(exec.Detail)

	// workspace_id is nullable: an execution outside any workspace context
	// stores NULL rather than an invalid empty uuid.
	var workspaceID *string
	if exec.WorkspaceID != "" {
		workspaceID = &exec.WorkspaceID
	}

	query := `
		INSERT INTO hook_executions (
			id, hook_id, hook_name, hook_level, workspace_id, event, decision,
			duration_ms, exit_code, http_status, detail, token_count, origin, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)
	`
	if _, err := hs.db.Exec(ctx, query,
		exec.ID,
		exec.HookID,
		exec.HookName,
		exec.HookLevel,
		workspaceID,
		exec.Event,
		exec.Decision,
		exec.DurationMS,
		exec.ExitCode,
		exec.HTTPStatus,
		exec.Detail,
		exec.TokenCount,
		exec.Origin,
		exec.CreatedAt,
	); err != nil {
		return convertError(err)
	}
	return nil
}

func (hs *hookStore) ListHookExecutions(ctx context.Context, workspaceID string, hookID *string, limit int) ([]domain.HookExecution, error) {
	if workspaceID == "" {
		return []domain.HookExecution{}, nil
	}

	query := `
		SELECT ` + hookExecutionColumns + `
		FROM hook_executions
		WHERE workspace_id = $1
	`
	args := []any{workspaceID}
	if hookID != nil {
		args = append(args, *hookID)
		query += fmt.Sprintf(` AND hook_id = $%d`, len(args))
	}
	query += ` ORDER BY created_at DESC, id DESC`
	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(` LIMIT $%d`, len(args))
	}

	rows, err := hs.db.Query(ctx, query, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	executions := make([]domain.HookExecution, 0)
	for rows.Next() {
		e, err := scanHookExecution(rows)
		if err != nil {
			return nil, err
		}
		executions = append(executions, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return executions, nil
}
