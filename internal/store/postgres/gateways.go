package postgres

import (
	"context"
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

// Gateway stores (integrate-telegram-gateway 1.4). Every query is
// workspace-scoped; the exceptions are ClaimDue and PruneDelivered on the
// outbox — intentionally unscoped maintenance operations serving the
// gateway's delivery worker (the ClaimDueSchedulers precedent), each guarded
// by status predicates and, for claims, FOR UPDATE SKIP LOCKED. The bot
// token rides as an AES-256-GCM secrets envelope (the web-search provider
// credential pattern): the caller encrypts with the workspace id as AAD, the
// store persists the ciphertext opaquely, and no read ever returns it
// alongside a decryption path.

// isGatewayChatBindingViolation reports whether the error is a unique
// violation on uq_gateway_chat_bindings_platform_chat — the global
// one-agent-per-platform-chat rule (design D2) — as opposed to a primary-key
// collision.
func isGatewayChatBindingViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgerrcode.UniqueViolation &&
			pgErr.ConstraintName == "uq_gateway_chat_bindings_platform_chat"
	}
	return false
}

// -------------------------------------------------------------------------
// GatewayStore (configuration)
// -------------------------------------------------------------------------

type gatewayStore struct {
	db Executor
}

// NewGatewayStore creates a new GatewayStore with the given database executor.
func NewGatewayStore(db Executor) storeport.GatewayStore {
	return &gatewayStore{db: db}
}

const gatewayColumns = `
	id, workspace_id, platform, bot_token_ciphertext, bot_username, enabled,
	transport, webhook_url, default_agent_id, created_at, updated_at
`

func scanGateway(row pgx.Row) (*domain.GatewayConfig, error) {
	var g domain.GatewayConfig
	var defaultAgentID *string
	err := row.Scan(
		&g.ID,
		&g.WorkspaceID,
		&g.Platform,
		&g.BotTokenCiphertext,
		&g.BotUsername,
		&g.Enabled,
		&g.Transport,
		&g.WebhookURL,
		&defaultAgentID,
		&g.CreatedAt,
		&g.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	g.DefaultAgentID = defaultAgentID
	return &g, nil
}

// UpsertGateway inserts the configuration; on conflict (workspace_id,
// platform) the connection fields are rewritten while the row's id,
// created_at, and enabled flag survive (re-saving a bot token never
// disables a live gateway). The RETURNING clause keeps the caller's struct
// faithful on both paths.
func (gs *gatewayStore) UpsertGateway(ctx context.Context, workspaceID string, g *domain.GatewayConfig) error {
	if g == nil {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	g.WorkspaceID = workspaceID
	if err := domain.ValidateGatewayConfig(g); err != nil {
		return err
	}

	// FK parity: the workspace must exist, and a set default agent must
	// belong to this workspace (the plain agent FK alone would admit
	// cross-workspace references).
	var one bool
	err := gs.db.QueryRow(ctx, `SELECT true FROM workspaces WHERE id = $1`, workspaceID).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
		}
		return convertError(err)
	}
	if g.DefaultAgentID != nil && *g.DefaultAgentID != "" {
		err = gs.db.QueryRow(ctx,
			`SELECT true FROM agents WHERE workspace_id = $1 AND id = $2`,
			workspaceID, *g.DefaultAgentID,
		).Scan(&one)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
			}
			return convertError(err)
		}
	}

	query := `
		INSERT INTO workspace_gateways (
			workspace_id, platform, bot_token_ciphertext, bot_username,
			enabled, transport, webhook_url, default_agent_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (workspace_id, platform) DO UPDATE SET
			bot_token_ciphertext = EXCLUDED.bot_token_ciphertext,
			bot_username = EXCLUDED.bot_username,
			transport = EXCLUDED.transport,
			webhook_url = EXCLUDED.webhook_url,
			default_agent_id = EXCLUDED.default_agent_id,
			updated_at = $11
		RETURNING id, created_at, updated_at, enabled
	`
	err = gs.db.QueryRow(ctx, query,
		g.WorkspaceID,
		g.Platform,
		g.BotTokenCiphertext,
		g.BotUsername,
		g.Enabled,
		g.Transport,
		g.WebhookURL,
		g.DefaultAgentID,
		now,
		now,
		now,
	).Scan(&g.ID, &g.CreatedAt, &g.UpdatedAt, &g.Enabled)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (gs *gatewayStore) GetGateway(ctx context.Context, workspaceID, platform string) (*domain.GatewayConfig, error) {
	if workspaceID == "" || platform == "" {
		return nil, nil
	}

	query := `
		SELECT ` + gatewayColumns + `
		FROM workspace_gateways
		WHERE workspace_id = $1 AND platform = $2
	`
	g, err := scanGateway(gs.db.QueryRow(ctx, query, workspaceID, platform))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return g, nil
}

func (gs *gatewayStore) ListGateways(ctx context.Context, workspaceID string) ([]domain.GatewayConfig, error) {
	if workspaceID == "" {
		return []domain.GatewayConfig{}, nil
	}

	query := `
		SELECT ` + gatewayColumns + `
		FROM workspace_gateways
		WHERE workspace_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := gs.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	gateways := make([]domain.GatewayConfig, 0)
	for rows.Next() {
		g, err := scanGateway(rows)
		if err != nil {
			return nil, err
		}
		gateways = append(gateways, *g)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return gateways, nil
}

func (gs *gatewayStore) SetGatewayEnabled(ctx context.Context, workspaceID, platform string, enabled bool) error {
	if workspaceID == "" || platform == "" {
		return domain.ErrNotFound
	}

	tag, err := gs.db.Exec(ctx, `
		UPDATE workspace_gateways
		SET enabled = $3, updated_at = $4
		WHERE workspace_id = $1 AND platform = $2
	`, workspaceID, platform, enabled, time.Now().UTC())
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (gs *gatewayStore) DeleteGateway(ctx context.Context, workspaceID, platform string) error {
	if workspaceID == "" || platform == "" {
		return domain.ErrNotFound
	}

	tag, err := gs.db.Exec(ctx, `
		DELETE FROM workspace_gateways
		WHERE workspace_id = $1 AND platform = $2
	`, workspaceID, platform)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// -------------------------------------------------------------------------
// GatewayBindings (chat routing)
// -------------------------------------------------------------------------

type gatewayBindingStore struct {
	db Executor
}

// NewGatewayBindingStore creates a new GatewayBindings with the given
// database executor.
func NewGatewayBindingStore(db Executor) storeport.GatewayBindings {
	return &gatewayBindingStore{db: db}
}

const gatewayChatBindingColumns = `
	id, workspace_id, platform, platform_chat_id, agent_id, created_by, created_at
`

func scanChatBinding(row pgx.Row) (*domain.ChatBinding, error) {
	var b domain.ChatBinding
	err := row.Scan(
		&b.ID,
		&b.WorkspaceID,
		&b.Platform,
		&b.PlatformChatID,
		&b.AgentID,
		&b.CreatedBy,
		&b.CreatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &b, nil
}

func (gb *gatewayBindingStore) CreateChatBinding(ctx context.Context, workspaceID string, b *domain.ChatBinding) error {
	if b == nil {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	b.WorkspaceID = workspaceID
	if err := domain.ValidateChatBinding(b); err != nil {
		return err
	}

	// FK parity: workspace, workspace-scoped agent, and optional creator.
	var one bool
	err := gb.db.QueryRow(ctx, `SELECT true FROM workspaces WHERE id = $1`, workspaceID).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
		}
		return convertError(err)
	}
	err = gb.db.QueryRow(ctx,
		`SELECT true FROM agents WHERE workspace_id = $1 AND id = $2`,
		workspaceID, b.AgentID,
	).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
		}
		return convertError(err)
	}
	if b.CreatedBy != nil && *b.CreatedBy != "" {
		err = gb.db.QueryRow(ctx, `SELECT true FROM users WHERE id = $1`, *b.CreatedBy).Scan(&one)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: user not found", domain.ErrNotFound)
			}
			return convertError(err)
		}
	}

	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	query := `
		INSERT INTO gateway_chat_bindings (
			id, workspace_id, platform, platform_chat_id, agent_id, created_by, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = gb.db.Exec(ctx, query,
		b.ID,
		b.WorkspaceID,
		b.Platform,
		b.PlatformChatID,
		b.AgentID,
		b.CreatedBy,
		b.CreatedAt,
	)
	if err != nil {
		if isGatewayChatBindingViolation(err) {
			return fmt.Errorf("%w: %s chat %s is already bound to an agent", domain.ErrGatewayBindingConflict, b.Platform, b.PlatformChatID)
		}
		return convertError(err)
	}
	return nil
}

func (gb *gatewayBindingStore) GetChatBinding(ctx context.Context, workspaceID, platform, platformChatID string) (*domain.ChatBinding, error) {
	if workspaceID == "" || platform == "" || platformChatID == "" {
		return nil, nil
	}

	query := `
		SELECT ` + gatewayChatBindingColumns + `
		FROM gateway_chat_bindings
		WHERE workspace_id = $1 AND platform = $2 AND platform_chat_id = $3
	`
	b, err := scanChatBinding(gb.db.QueryRow(ctx, query, workspaceID, platform, platformChatID))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return b, nil
}

func (gb *gatewayBindingStore) ListChatBindings(ctx context.Context, workspaceID string) ([]domain.ChatBinding, error) {
	if workspaceID == "" {
		return []domain.ChatBinding{}, nil
	}

	query := `
		SELECT ` + gatewayChatBindingColumns + `
		FROM gateway_chat_bindings
		WHERE workspace_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := gb.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	bindings := make([]domain.ChatBinding, 0)
	for rows.Next() {
		b, err := scanChatBinding(rows)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, *b)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return bindings, nil
}

// RemapBindingChat rewrites a binding's platform_chat_id (design D10
// migrate_to_chat_id): old chat-id session keys are derived, never stored,
// so they are abandoned rather than rewritten. A target chat already bound
// (in any workspace) is a conflict.
func (gb *gatewayBindingStore) RemapBindingChat(ctx context.Context, workspaceID, platform, oldChatID, newChatID string) error {
	if workspaceID == "" || platform == "" || oldChatID == "" || newChatID == "" {
		return domain.ErrNotFound
	}

	tag, err := gb.db.Exec(ctx, `
		UPDATE gateway_chat_bindings
		SET platform_chat_id = $4
		WHERE workspace_id = $1 AND platform = $2 AND platform_chat_id = $3
	`, workspaceID, platform, oldChatID, newChatID)
	if err != nil {
		if isGatewayChatBindingViolation(err) {
			return fmt.Errorf("%w: %s chat %s is already bound to an agent", domain.ErrGatewayBindingConflict, platform, newChatID)
		}
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (gb *gatewayBindingStore) DeleteChatBinding(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	tag, err := gb.db.Exec(ctx, `
		DELETE FROM gateway_chat_bindings
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ActiveSessionSuffix reads the deterministic session-key cursor (000048,
// design D3). A missing row means the base key (suffix 0) is active — the
// gateway mints the cursor row on the first /new, never on ordinary turns.
// Note the read is not workspace-scoped: the (platform, platform_chat_id,
// agent_id) triple is the cursor's primary key and agent ids are
// workspace-unique already, so the caller's agent resolution carries the
// tenant scope.
func (gb *gatewayBindingStore) ActiveSessionSuffix(ctx context.Context, platform, platformChatID, agentID string) (int64, error) {
	if platform == "" || platformChatID == "" || agentID == "" {
		return 0, nil
	}

	var suffix int64
	err := gb.db.QueryRow(ctx, `
		SELECT suffix
		FROM gateway_active_sessions
		WHERE platform = $1 AND platform_chat_id = $2 AND agent_id = $3
	`, platform, platformChatID, agentID).Scan(&suffix)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, convertError(err)
	}
	return suffix, nil
}

// BumpActiveSessionSuffix atomically mints the next session suffix: the
// INSERT ... ON CONFLICT upsert bumps under the row lock, so concurrent /new
// commands can never mint the same key. The FK to agents double-serves as
// the existence check; an unknown agent surfaces domain.ErrNotFound via the
// FK violation conversion.
func (gb *gatewayBindingStore) BumpActiveSessionSuffix(ctx context.Context, platform, platformChatID, agentID string) (int64, error) {
	if platform == "" || platformChatID == "" || agentID == "" {
		return 0, fmt.Errorf("%w: platform, chat id, and agent are required", domain.ErrInvalid)
	}

	var suffix int64
	err := gb.db.QueryRow(ctx, `
		INSERT INTO gateway_active_sessions (platform, platform_chat_id, agent_id, workspace_id, suffix, updated_at)
		VALUES ($1, $2, $3, (SELECT workspace_id FROM agents WHERE id = $3), 1, now())
		ON CONFLICT (platform, platform_chat_id, agent_id)
		DO UPDATE SET suffix = gateway_active_sessions.suffix + 1, updated_at = now()
		RETURNING suffix
	`, platform, platformChatID, agentID).Scan(&suffix)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The agent subselect returned NULL — unknown agent.
			return 0, fmt.Errorf("%w: agent not found", domain.ErrNotFound)
		}
		return 0, convertError(err)
	}
	return suffix, nil
}

// -------------------------------------------------------------------------
// GatewayLinks (identity pairing + pairing tokens)
// -------------------------------------------------------------------------

type gatewayLinkStore struct {
	db Executor
}

// NewGatewayLinkStore creates a new GatewayLinks with the given database
// executor.
func NewGatewayLinkStore(db Executor) storeport.GatewayLinks {
	return &gatewayLinkStore{db: db}
}

const gatewayUserLinkColumns = `
	platform, platform_user_id, workspace_id, user_id, platform_username, default_agent_id, created_at
`

func scanUserLink(row pgx.Row) (*domain.UserLink, error) {
	var l domain.UserLink
	err := row.Scan(
		&l.Platform,
		&l.PlatformUserID,
		&l.WorkspaceID,
		&l.UserID,
		&l.PlatformUsername,
		&l.DefaultAgentID,
		&l.CreatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &l, nil
}

const gatewayPairingTokenColumns = `
	token, workspace_id, user_id, expires_at, consumed_at, created_at
`

func scanPairingToken(row pgx.Row) (*domain.PairingToken, error) {
	var t domain.PairingToken
	err := row.Scan(
		&t.Token,
		&t.WorkspaceID,
		&t.UserID,
		&t.ExpiresAt,
		&t.ConsumedAt,
		&t.CreatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &t, nil
}

func (gl *gatewayLinkStore) CreateUserLink(ctx context.Context, workspaceID string, l *domain.UserLink) error {
	if l == nil {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	l.WorkspaceID = workspaceID
	if err := domain.ValidateUserLink(l); err != nil {
		return err
	}

	// FK parity: workspace and member must exist.
	var one bool
	err := gl.db.QueryRow(ctx, `SELECT true FROM workspaces WHERE id = $1`, workspaceID).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
		}
		return convertError(err)
	}
	err = gl.db.QueryRow(ctx, `SELECT true FROM users WHERE id = $1`, l.UserID).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: user not found", domain.ErrNotFound)
		}
		return convertError(err)
	}

	if l.CreatedAt.IsZero() {
		l.CreatedAt = now
	}
	query := `
		INSERT INTO gateway_user_links (
			platform, platform_user_id, workspace_id, user_id, platform_username, default_agent_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = gl.db.Exec(ctx, query,
		l.Platform,
		l.PlatformUserID,
		l.WorkspaceID,
		l.UserID,
		l.PlatformUsername,
		l.DefaultAgentID,
		l.CreatedAt,
	)
	if err != nil {
		// The PK (platform, platform_user_id, workspace_id) is the only
		// unique constraint on the table.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return fmt.Errorf("%w: platform identity %s is already paired in this workspace", domain.ErrConflict, l.PlatformUserID)
		}
		return convertError(err)
	}
	return nil
}

func (gl *gatewayLinkStore) GetUserLink(ctx context.Context, workspaceID, platform, platformUserID string) (*domain.UserLink, error) {
	if workspaceID == "" || platform == "" || platformUserID == "" {
		return nil, nil
	}

	query := `
		SELECT ` + gatewayUserLinkColumns + `
		FROM gateway_user_links
		WHERE workspace_id = $1 AND platform = $2 AND platform_user_id = $3
	`
	l, err := scanUserLink(gl.db.QueryRow(ctx, query, workspaceID, platform, platformUserID))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return l, nil
}

func (gl *gatewayLinkStore) ListUserLinks(ctx context.Context, workspaceID string) ([]domain.UserLink, error) {
	if workspaceID == "" {
		return []domain.UserLink{}, nil
	}

	query := `
		SELECT ` + gatewayUserLinkColumns + `
		FROM gateway_user_links
		WHERE workspace_id = $1
		ORDER BY created_at ASC, platform_user_id ASC
	`
	return gl.listUserLinksQuery(ctx, query, workspaceID)
}

func (gl *gatewayLinkStore) ListUserLinksForMember(ctx context.Context, workspaceID, userID string) ([]domain.UserLink, error) {
	if workspaceID == "" || userID == "" {
		return []domain.UserLink{}, nil
	}

	query := `
		SELECT ` + gatewayUserLinkColumns + `
		FROM gateway_user_links
		WHERE workspace_id = $1 AND user_id = $2
		ORDER BY created_at ASC, platform_user_id ASC
	`
	return gl.listUserLinksQuery(ctx, query, workspaceID, userID)
}

func (gl *gatewayLinkStore) listUserLinksQuery(ctx context.Context, query string, args ...any) ([]domain.UserLink, error) {
	rows, err := gl.db.Query(ctx, query, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	links := make([]domain.UserLink, 0)
	for rows.Next() {
		l, err := scanUserLink(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, *l)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return links, nil
}

func (gl *gatewayLinkStore) DeleteUserLink(ctx context.Context, workspaceID, platform, platformUserID string) error {
	if workspaceID == "" || platform == "" || platformUserID == "" {
		return domain.ErrNotFound
	}

	tag, err := gl.db.Exec(ctx, `
		DELETE FROM gateway_user_links
		WHERE workspace_id = $1 AND platform = $2 AND platform_user_id = $3
	`, workspaceID, platform, platformUserID)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SetUserLinkDefaultAgent writes the per-user default agent choice
// (000048, design D3). FK parity: a non-nil agent must exist in the
// workspace (the column's FK is ON DELETE SET NULL, so a stale id never
// blocks the write — but a never-existed one should not silently no-op).
func (gl *gatewayLinkStore) SetUserLinkDefaultAgent(ctx context.Context, workspaceID, platform, platformUserID string, agentID *string) error {
	if workspaceID == "" || platform == "" || platformUserID == "" {
		return domain.ErrNotFound
	}
	if agentID != nil && *agentID == "" {
		agentID = nil
	}
	if agentID != nil {
		var one bool
		err := gl.db.QueryRow(ctx, `SELECT true FROM agents WHERE id = $1 AND workspace_id = $2`, *agentID, workspaceID).Scan(&one)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: agent not found", domain.ErrNotFound)
			}
			return convertError(err)
		}
	}

	tag, err := gl.db.Exec(ctx, `
		UPDATE gateway_user_links
		SET default_agent_id = $4
		WHERE workspace_id = $1 AND platform = $2 AND platform_user_id = $3
	`, workspaceID, platform, platformUserID, agentID)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (gl *gatewayLinkStore) CreatePairingToken(ctx context.Context, t *domain.PairingToken) error {
	if t == nil {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	if err := domain.ValidatePairingToken(t, now, true); err != nil {
		return err
	}

	// FK parity: workspace and member must exist.
	var one bool
	err := gl.db.QueryRow(ctx, `SELECT true FROM workspaces WHERE id = $1`, t.WorkspaceID).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
		}
		return convertError(err)
	}
	err = gl.db.QueryRow(ctx, `SELECT true FROM users WHERE id = $1`, t.UserID).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: user not found", domain.ErrNotFound)
		}
		return convertError(err)
	}

	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	query := `
		INSERT INTO gateway_pairing_tokens (
			token, workspace_id, user_id, expires_at, consumed_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err = gl.db.Exec(ctx, query,
		t.Token,
		t.WorkspaceID,
		t.UserID,
		t.ExpiresAt,
		t.ConsumedAt,
		t.CreatedAt,
	)
	if err != nil {
		// The token itself is the only unique constraint; a crypto-random
		// collision is the only path here.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return fmt.Errorf("%w: pairing token already exists", domain.ErrConflict)
		}
		return convertError(err)
	}
	return nil
}

// ConsumePairingToken atomically consumes a single-use token: the
// conditional UPDATE is the whole decision — exactly one caller's WHERE
// guard (unconsumed AND unexpired) can match, concurrent consumers see the
// row disappear under the other's lock, and no read-modify-write race
// exists.
func (gl *gatewayLinkStore) ConsumePairingToken(ctx context.Context, workspaceID, token string, now time.Time) (*domain.PairingToken, error) {
	if workspaceID == "" || token == "" {
		return nil, domain.ErrNotFound
	}

	tag, err := gl.db.Exec(ctx, `
		UPDATE gateway_pairing_tokens
		SET consumed_at = $3
		WHERE workspace_id = $1 AND token = $2
		  AND consumed_at IS NULL AND expires_at > $3
	`, workspaceID, token, now)
	if err != nil {
		return nil, convertError(err)
	}
	if tag.RowsAffected() == 1 {
		t, scanErr := scanPairingToken(gl.db.QueryRow(ctx, `
			SELECT `+gatewayPairingTokenColumns+`
			FROM gateway_pairing_tokens
			WHERE workspace_id = $1 AND token = $2
		`, workspaceID, token))
		if scanErr != nil {
			return nil, scanErr
		}
		return t, nil
	}

	// No row matched: classify unknown vs expired/used for the caller.
	t, classifyErr := scanPairingToken(gl.db.QueryRow(ctx, `
		SELECT `+gatewayPairingTokenColumns+`
		FROM gateway_pairing_tokens
		WHERE workspace_id = $1 AND token = $2
	`, workspaceID, token))
	if classifyErr != nil {
		if errors.Is(classifyErr, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, classifyErr
	}
	return nil, fmt.Errorf("%w: token expired at %s or already consumed", domain.ErrPairingTokenExpired, t.ExpiresAt.Format(time.RFC3339))
}

func (gl *gatewayLinkStore) RevokePairingToken(ctx context.Context, workspaceID, token string) error {
	if workspaceID == "" || token == "" {
		return domain.ErrNotFound
	}

	tag, err := gl.db.Exec(ctx, `
		DELETE FROM gateway_pairing_tokens
		WHERE workspace_id = $1 AND token = $2
	`, workspaceID, token)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// -------------------------------------------------------------------------
// GatewayOutbox (delivery reliability)
// -------------------------------------------------------------------------

type gatewayOutboxStore struct {
	db Executor
}

// NewGatewayOutboxStore creates a new GatewayOutbox with the given database
// executor.
func NewGatewayOutboxStore(db Executor) storeport.GatewayOutbox {
	return &gatewayOutboxStore{db: db}
}

const gatewayOutboxColumns = `
	id, workspace_id, session_id, payload, status, attempts, deliver_after, created_at
`

func scanOutboxEntry(row pgx.Row) (*domain.OutboxEntry, error) {
	var e domain.OutboxEntry
	err := row.Scan(
		&e.ID,
		&e.WorkspaceID,
		&e.SessionID,
		&e.Payload,
		&e.Status,
		&e.Attempts,
		&e.DeliverAfter,
		&e.CreatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &e, nil
}

func (goStore *gatewayOutboxStore) Enqueue(ctx context.Context, e *domain.OutboxEntry) error {
	if e == nil {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	if err := domain.ValidateOutboxEntry(e); err != nil {
		return err
	}

	// FK parity: the workspace must exist (workspace_id carries no FK on
	// the table — the scheduler_runs denormalization precedent).
	var one bool
	err := goStore.db.QueryRow(ctx, `SELECT true FROM workspaces WHERE id = $1`, e.WorkspaceID).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
		}
		return convertError(err)
	}

	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	e.Attempts = 0
	if e.DeliverAfter.IsZero() {
		e.DeliverAfter = now
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	query := `
		INSERT INTO gateway_outbox (
			id, workspace_id, session_id, payload, status, attempts, deliver_after, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err = goStore.db.Exec(ctx, query,
		e.ID,
		e.WorkspaceID,
		e.SessionID,
		e.Payload,
		e.Status,
		e.Attempts,
		e.DeliverAfter,
		e.CreatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

// ClaimDue atomically claims due pending entries: the subquery locks the
// candidates FOR UPDATE SKIP LOCKED (a second claimer skips them instead of
// blocking, so an entry is claimed exactly once) and the UPDATE increments
// each one's attempts — one statement, one lock decision, no explicit
// transaction needed. Intentionally workspace-unscoped (the
// ClaimDueSchedulers precedent).
func (goStore *gatewayOutboxStore) ClaimDue(ctx context.Context, now time.Time, limit int) ([]domain.OutboxEntry, error) {
	if limit <= 0 {
		return []domain.OutboxEntry{}, nil
	}

	rows, err := goStore.db.Query(ctx, `
		UPDATE gateway_outbox SET attempts = attempts + 1
		WHERE id IN (
			SELECT id FROM gateway_outbox
			WHERE status = 'pending' AND deliver_after <= $1
			ORDER BY deliver_after ASC, id ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+gatewayOutboxColumns+`
	`, now, limit)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	claimed := make([]domain.OutboxEntry, 0, limit)
	for rows.Next() {
		e, err := scanOutboxEntry(rows)
		if err != nil {
			return nil, err
		}
		claimed = append(claimed, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return claimed, nil
}

func (goStore *gatewayOutboxStore) MarkDelivered(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	tag, err := goStore.db.Exec(ctx, `
		UPDATE gateway_outbox SET status = $3
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id, domain.GatewayOutboxStatusDelivered)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (goStore *gatewayOutboxStore) Reschedule(ctx context.Context, workspaceID, id string, deliverAfter time.Time) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	tag, err := goStore.db.Exec(ctx, `
		UPDATE gateway_outbox SET status = $3, deliver_after = $4
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id, domain.GatewayOutboxStatusPending, deliverAfter)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (goStore *gatewayOutboxStore) MarkDead(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	tag, err := goStore.db.Exec(ctx, `
		UPDATE gateway_outbox SET status = $3
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id, domain.GatewayOutboxStatusDead)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (goStore *gatewayOutboxStore) PruneDelivered(ctx context.Context, before time.Time) (int64, error) {
	tag, err := goStore.db.Exec(ctx, `
		DELETE FROM gateway_outbox
		WHERE status = 'delivered' AND created_at < $1
	`, before)
	if err != nil {
		return 0, convertError(err)
	}
	return tag.RowsAffected(), nil
}
