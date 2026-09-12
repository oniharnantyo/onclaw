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

// channelStore implements storeport.ChannelStore for PostgreSQL.
type channelStore struct {
	db Executor
}

// NewChannelStore creates a new ChannelStore with the given database executor.
func NewChannelStore(db Executor) storeport.ChannelStore {
	return &channelStore{db: db}
}

// marshalChannelMentions encodes resolved mentions as the jsonb list
// [{"type","id","handle"}]; nil and empty slices persist as the empty array so
// the column's NOT NULL default shape is preserved.
func marshalChannelMentions(mentions []domain.Mention) ([]byte, error) {
	if len(mentions) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(mentions)
}

// unmarshalChannelMentions decodes the mentions jsonb list; a nil result is
// normalized to the empty slice.
func unmarshalChannelMentions(data []byte) ([]domain.Mention, error) {
	var mentions []domain.Mention
	if len(data) == 0 {
		return []domain.Mention{}, nil
	}
	if err := json.Unmarshal(data, &mentions); err != nil {
		return nil, fmt.Errorf("%w: invalid mentions jsonb: %v", domain.ErrInvalid, err)
	}
	if mentions == nil {
		mentions = []domain.Mention{}
	}
	return mentions, nil
}

// marshalRunSummary encodes the run footprint jsonb; nil persists as SQL NULL
// (the column is nullable — absence of a summary is a normal state).
func marshalRunSummary(summary *domain.RunSummary) ([]byte, error) {
	if summary == nil {
		return nil, nil
	}
	return json.Marshal(summary)
}

// unmarshalRunSummary decodes the run footprint jsonb; NULL reads back as nil.
func unmarshalRunSummary(data []byte) (*domain.RunSummary, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var summary domain.RunSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, fmt.Errorf("%w: invalid run_summary jsonb: %v", domain.ErrInvalid, err)
	}
	return &summary, nil
}

// isChannelSlugViolation reports whether the error is a unique violation on
// uq_channels_workspace_id_slug, as opposed to the primary key.
func isChannelSlugViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgerrcode.UniqueViolation &&
			pgErr.ConstraintName == "uq_channels_workspace_id_slug"
	}
	return false
}

// isChannelMemberDuplicateViolation reports whether the error is a unique
// violation on one of the per-channel roster constraints
// (uq_channel_members_channel_user / uq_channel_members_channel_agent).
func isChannelMemberDuplicateViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgerrcode.UniqueViolation &&
			(pgErr.ConstraintName == "uq_channel_members_channel_user" ||
				pgErr.ConstraintName == "uq_channel_members_channel_agent")
	}
	return false
}

// isFacilitatorViolation reports whether the error is a unique violation on
// uq_channel_members_channel_facilitator (channel-teams D2: one facilitator
// per channel — the DB backstop behind the store-level pre-check).
func isFacilitatorViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgerrcode.UniqueViolation &&
			pgErr.ConstraintName == "uq_channel_members_channel_facilitator"
	}
	return false
}

// channelExistsInWorkspace mirrors the fake's guard: the channel must exist
// within the caller's workspace scope.
func channelExistsInWorkspace(ctx context.Context, db Executor, workspaceID, channelID string) (bool, error) {
	var one bool
	err := db.QueryRow(ctx,
		`SELECT true FROM channels WHERE workspace_id = $1 AND id = $2`,
		workspaceID, channelID,
	).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, convertError(err)
	}
	return true, nil
}

// userIsWorkspaceMember reports whether the user holds a membership in the
// workspace — the tenant guard the plain user_id FK cannot express.
func userIsWorkspaceMember(ctx context.Context, db Executor, workspaceID, userID string) (bool, error) {
	var one bool
	err := db.QueryRow(ctx,
		`SELECT true FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`,
		workspaceID, userID,
	).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, convertError(err)
	}
	return true, nil
}

// agentExistsInWorkspaceByRef reports whether the agent belongs to the
// workspace — the tenant guard the plain agent_id FK cannot express.
func agentExistsInWorkspaceByRef(ctx context.Context, db Executor, workspaceID, agentID string) (bool, error) {
	var one bool
	err := db.QueryRow(ctx,
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

// channelRefExists resolves the workspace/tenant guard for a channel member or
// message reference: users must hold a workspace membership, agents must
// belong to the workspace.
func channelRefExists(ctx context.Context, db Executor, workspaceID string, memberType domain.ChannelMemberType, refID string) (bool, error) {
	switch memberType {
	case domain.ChannelMemberTypeUser:
		return userIsWorkspaceMember(ctx, db, workspaceID, refID)
	case domain.ChannelMemberTypeAgent:
		return agentExistsInWorkspaceByRef(ctx, db, workspaceID, refID)
	default:
		return false, nil
	}
}

// rootExistsInChannel checks that a chain root is an existing message of the
// same channel (the FK alone would admit cross-channel roots).
func rootExistsInChannel(ctx context.Context, db Executor, workspaceID, channelID, rootMessageID string) (bool, error) {
	var one bool
	err := db.QueryRow(ctx,
		`SELECT true FROM channel_messages WHERE workspace_id = $1 AND channel_id = $2 AND id = $3`,
		workspaceID, channelID, rootMessageID,
	).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, convertError(err)
	}
	return true, nil
}

const channelColumns = `
	id, workspace_id, name, slug, purpose, conventions, created_by, created_at, updated_at
`

func scanChannel(row pgx.Row) (*domain.Channel, error) {
	var c domain.Channel
	err := row.Scan(
		&c.ID,
		&c.WorkspaceID,
		&c.Name,
		&c.Slug,
		&c.Purpose,
		&c.Conventions,
		&c.CreatedBy,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &c, nil
}

func (cs *channelStore) CreateChannel(ctx context.Context, channel *domain.Channel) error {
	if channel == nil {
		return domain.ErrInvalid
	}
	if err := channel.Validate(); err != nil {
		return err
	}

	if channel.ID == "" {
		channel.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if channel.CreatedAt.IsZero() {
		channel.CreatedAt = now
	}
	if channel.UpdatedAt.IsZero() {
		channel.UpdatedAt = now
	}

	// Case-insensitive per-workspace slug uniqueness (the schema constraint is
	// exact-only; see migration 000030) — pre-check like the fake.
	var exists bool
	err := cs.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM channels WHERE workspace_id = $1 AND lower(slug) = lower($2))`,
		channel.WorkspaceID, channel.Slug,
	).Scan(&exists)
	if err != nil {
		return convertError(err)
	}
	if exists {
		return fmt.Errorf("%w: channel slug %q already taken in workspace", domain.ErrChannelSlugConflict, channel.Slug)
	}

	query := `
		INSERT INTO channels (
			id, workspace_id, name, slug, purpose, conventions, created_by, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
	`
	_, err = cs.db.Exec(ctx, query,
		channel.ID,
		channel.WorkspaceID,
		channel.Name,
		channel.Slug,
		channel.Purpose,
		channel.Conventions,
		channel.CreatedBy,
		channel.CreatedAt,
		channel.UpdatedAt,
	)
	if err != nil {
		if isChannelSlugViolation(err) {
			return fmt.Errorf("%w: channel slug %q already taken in workspace", domain.ErrChannelSlugConflict, channel.Slug)
		}
		return convertError(err)
	}
	return nil
}

func (cs *channelStore) ChannelByID(ctx context.Context, workspaceID, id string) (*domain.Channel, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT ` + channelColumns + `
		FROM channels
		WHERE workspace_id = $1 AND id = $2
	`
	return scanChannel(cs.db.QueryRow(ctx, query, workspaceID, id))
}

func (cs *channelStore) ChannelBySlug(ctx context.Context, workspaceID, slug string) (*domain.Channel, error) {
	if workspaceID == "" || slug == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT ` + channelColumns + `
		FROM channels
		WHERE workspace_id = $1 AND lower(slug) = lower($2)
	`
	return scanChannel(cs.db.QueryRow(ctx, query, workspaceID, slug))
}

func (cs *channelStore) ListChannels(ctx context.Context, workspaceID string) ([]domain.Channel, error) {
	if workspaceID == "" {
		return []domain.Channel{}, nil
	}

	query := `
		SELECT ` + channelColumns + `
		FROM channels
		WHERE workspace_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := cs.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	channels := make([]domain.Channel, 0)
	for rows.Next() {
		channel, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, *channel)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return channels, nil
}

func (cs *channelStore) UpdateChannel(ctx context.Context, channel *domain.Channel) error {
	if channel == nil || channel.ID == "" || channel.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if err := channel.Validate(); err != nil {
		return err
	}

	// Case-insensitive slug uniqueness within the workspace, excluding this row.
	var exists bool
	err := cs.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM channels WHERE workspace_id = $1 AND lower(slug) = lower($2) AND id <> $3)`,
		channel.WorkspaceID, channel.Slug, channel.ID,
	).Scan(&exists)
	if err != nil {
		return convertError(err)
	}
	if exists {
		return fmt.Errorf("%w: channel slug %q already taken in workspace", domain.ErrChannelSlugConflict, channel.Slug)
	}

	// Editable fields only: created_by and created_at are immutable.
	query := `
		UPDATE channels
		SET name = $3,
		    slug = $4,
		    purpose = $5,
		    conventions = $6,
		    updated_at = $7
		WHERE workspace_id = $1 AND id = $2
		RETURNING created_at, created_by, updated_at
	`
	err = cs.db.QueryRow(ctx, query,
		channel.WorkspaceID,
		channel.ID,
		channel.Name,
		channel.Slug,
		channel.Purpose,
		channel.Conventions,
		time.Now().UTC(),
	).Scan(&channel.CreatedAt, &channel.CreatedBy, &channel.UpdatedAt)
	if err != nil {
		if isChannelSlugViolation(err) {
			return fmt.Errorf("%w: channel slug %q already taken in workspace", domain.ErrChannelSlugConflict, channel.Slug)
		}
		return convertError(err)
	}
	return nil
}

func (cs *channelStore) DeleteChannel(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `
		DELETE FROM channels
		WHERE workspace_id = $1 AND id = $2
	`
	tag, err := cs.db.Exec(ctx, query, workspaceID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// -------------------------------------------------------------------------
// Membership implementation
// -------------------------------------------------------------------------

const channelMemberColumns = `
	id, workspace_id, channel_id, member_type, user_id, agent_id, specialization, role, added_at
`

func scanChannelMember(row pgx.Row) (*domain.ChannelMember, error) {
	var m domain.ChannelMember
	// Nullable uuid columns scan through pointers: NULL cannot scan into a
	// plain string.
	var userID, agentID *string
	err := row.Scan(
		&m.ID,
		&m.WorkspaceID,
		&m.ChannelID,
		&m.MemberType,
		&userID,
		&agentID,
		&m.Specialization,
		&m.Role,
		&m.AddedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if userID != nil {
		m.UserID = *userID
	}
	if agentID != nil {
		m.AgentID = *agentID
	}
	return &m, nil
}

func (cs *channelStore) AddChannelMember(ctx context.Context, member *domain.ChannelMember) error {
	if member == nil {
		return domain.ErrInvalid
	}
	if err := member.Validate(); err != nil {
		return err
	}

	// The channel must exist within the member's workspace.
	found, err := channelExistsInWorkspace(ctx, cs.db, member.WorkspaceID, member.ChannelID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: channel not found in workspace", domain.ErrNotFound)
	}

	// The reference must exist inside the workspace (fake parity; the plain
	// user/agent FK alone would admit cross-workspace references).
	found, err = channelRefExists(ctx, cs.db, member.WorkspaceID, member.MemberType, member.RefID())
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: %s %q not found in workspace", domain.ErrNotFound, member.MemberType, member.RefID())
	}

	// One roster row per (channel, member type, reference) — pre-check like
	// the fake; the partial unique indexes are the backstop.
	dupQuery := `
		SELECT EXISTS (
			SELECT 1 FROM channel_members
			WHERE channel_id = $1 AND member_type = $2 AND
			      (CASE WHEN member_type = 'user' THEN user_id ELSE agent_id END) = $3::uuid
		)
	`
	var exists bool
	err = cs.db.QueryRow(ctx, dupQuery, member.ChannelID, member.MemberType, member.RefID()).Scan(&exists)
	if err != nil {
		return convertError(err)
	}
	if exists {
		return fmt.Errorf("%w: %s %q already a member of channel", domain.ErrDuplicateChannelMember, member.MemberType, member.RefID())
	}

	// Facilitator uniqueness (channel-teams D2): at most one per channel —
	// pre-check like the fake; uq_channel_members_channel_facilitator is the
	// backstop (mapped below).
	if member.Role == domain.ChannelMemberRoleFacilitator {
		var facExists bool
		err = cs.db.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM channel_members WHERE channel_id = $1 AND role = 'facilitator')`,
			member.ChannelID,
		).Scan(&facExists)
		if err != nil {
			return convertError(err)
		}
		if facExists {
			return fmt.Errorf("%w: channel %q already has a facilitator", domain.ErrChannelFacilitatorExists, member.ChannelID)
		}
	}

	if member.ID == "" {
		member.ID = uuid.NewString()
	}
	if member.AddedAt.IsZero() {
		member.AddedAt = time.Now().UTC()
	}

	// Nullable uuid columns: an empty Go string binds as '' (invalid uuid),
	// not NULL — bind pointers so the absent reference persists as NULL.
	userID, agentID := nullableRef(member.UserID), nullableRef(member.AgentID)

	query := `
		INSERT INTO channel_members (
			id, workspace_id, channel_id, member_type, user_id, agent_id, specialization, role, added_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
	`
	_, err = cs.db.Exec(ctx, query,
		member.ID,
		member.WorkspaceID,
		member.ChannelID,
		member.MemberType,
		userID,
		agentID,
		member.Specialization,
		member.Role,
		member.AddedAt,
	)
	if err != nil {
		if isChannelMemberDuplicateViolation(err) {
			return fmt.Errorf("%w: %s %q already a member of channel", domain.ErrDuplicateChannelMember, member.MemberType, member.RefID())
		}
		if isFacilitatorViolation(err) {
			return fmt.Errorf("%w: channel %q already has a facilitator", domain.ErrChannelFacilitatorExists, member.ChannelID)
		}
		return convertError(err)
	}
	return nil
}

// nullableRef maps the empty-string reference to a nil pointer so pgx binds
// SQL NULL instead of an invalid empty uuid.
func nullableRef(ref string) *string {
	if ref == "" {
		return nil
	}
	return &ref
}

// nullableText normalizes an optional text reference: a pointer to the empty
// string is treated as absent.
func nullableText(ref *string) *string {
	if ref != nil && *ref == "" {
		return nil
	}
	return ref
}

func (cs *channelStore) RemoveChannelMember(ctx context.Context, workspaceID, channelID, memberID string) error {
	if workspaceID == "" || channelID == "" || memberID == "" {
		return domain.ErrNotFound
	}

	query := `
		DELETE FROM channel_members
		WHERE workspace_id = $1 AND channel_id = $2 AND id = $3
	`
	tag, err := cs.db.Exec(ctx, query, workspaceID, channelID, memberID)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (cs *channelStore) UpdateChannelMemberSpecialization(ctx context.Context, workspaceID, channelID, memberID, specialization string) error {
	if workspaceID == "" || channelID == "" || memberID == "" {
		return domain.ErrNotFound
	}

	query := `
		UPDATE channel_members
		SET specialization = $4
		WHERE workspace_id = $1 AND channel_id = $2 AND id = $3
	`
	tag, err := cs.db.Exec(ctx, query, workspaceID, channelID, memberID, specialization)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (cs *channelStore) UpdateChannelMemberRole(ctx context.Context, workspaceID, channelID, memberID string, role domain.ChannelMemberRole) error {
	switch role {
	case domain.ChannelMemberRoleMember, domain.ChannelMemberRoleFacilitator:
	default:
		return fmt.Errorf("%w: role %q must be %q or %q", domain.ErrInvalid, role, domain.ChannelMemberRoleMember, domain.ChannelMemberRoleFacilitator)
	}
	if workspaceID == "" || channelID == "" || memberID == "" {
		return domain.ErrNotFound
	}

	// Facilitator pre-check like the fake — and gated on the target row
	// existing, so an unknown member id surfaces ErrNotFound (from the UPDATE
	// below) instead of the conflict. The partial unique index
	// (uq_channel_members_channel_facilitator) is the backstop, mapped below.
	if role == domain.ChannelMemberRoleFacilitator {
		var facExists bool
		err := cs.db.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM channel_members
				WHERE workspace_id = $1 AND channel_id = $2 AND role = 'facilitator' AND id <> $3
			) AND EXISTS (
				SELECT 1 FROM channel_members
				WHERE workspace_id = $1 AND channel_id = $2 AND id = $3
			)
		`, workspaceID, channelID, memberID).Scan(&facExists)
		if err != nil {
			return convertError(err)
		}
		if facExists {
			return fmt.Errorf("%w: channel %q already has a facilitator", domain.ErrChannelFacilitatorExists, channelID)
		}
	}

	query := `
		UPDATE channel_members
		SET role = $4
		WHERE workspace_id = $1 AND channel_id = $2 AND id = $3
	`
	tag, err := cs.db.Exec(ctx, query, workspaceID, channelID, memberID, role)
	if err != nil {
		if isFacilitatorViolation(err) {
			return fmt.Errorf("%w: channel %q already has a facilitator", domain.ErrChannelFacilitatorExists, channelID)
		}
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (cs *channelStore) SetChannelMessageWorkSession(ctx context.Context, workspaceID, channelID, messageID string, workSessionID *string) error {
	if workspaceID == "" || channelID == "" || messageID == "" {
		return domain.ErrNotFound
	}

	// FK parity pre-check: the session must exist in this workspace when
	// linking (the FK backstop would surface as ErrNotFound via convertError,
	// the explicit check keeps the message-missing vs session-missing cases
	// equally addressed).
	if workSessionID != nil && *workSessionID != "" {
		var one bool
		err := cs.db.QueryRow(ctx,
			`SELECT true FROM channel_work_sessions WHERE workspace_id = $1 AND id = $2`,
			workspaceID, *workSessionID,
		).Scan(&one)
		if err != nil {
			return convertError(err)
		}
	}

	query := `
		UPDATE channel_messages
		SET work_session_id = $4
		WHERE workspace_id = $1 AND channel_id = $2 AND id = $3
	`
	tag, err := cs.db.Exec(ctx, query, workspaceID, channelID, messageID, nullableText(workSessionID))
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (cs *channelStore) ListChannelMembers(ctx context.Context, workspaceID, channelID string) ([]domain.ChannelMember, error) {
	if workspaceID == "" || channelID == "" {
		return []domain.ChannelMember{}, nil
	}

	query := `
		SELECT ` + channelMemberColumns + `
		FROM channel_members
		WHERE workspace_id = $1 AND channel_id = $2
		ORDER BY added_at ASC, id ASC
	`
	rows, err := cs.db.Query(ctx, query, workspaceID, channelID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	members := make([]domain.ChannelMember, 0)
	for rows.Next() {
		member, err := scanChannelMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, *member)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return members, nil
}

func (cs *channelStore) ChannelMemberByRef(ctx context.Context, workspaceID, channelID string, memberType domain.ChannelMemberType, refID string) (*domain.ChannelMember, error) {
	switch memberType {
	case domain.ChannelMemberTypeUser, domain.ChannelMemberTypeAgent:
	default:
		return nil, fmt.Errorf("%w: member_type %q must be %q or %q", domain.ErrInvalid, memberType, domain.ChannelMemberTypeUser, domain.ChannelMemberTypeAgent)
	}
	if workspaceID == "" || channelID == "" || refID == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT ` + channelMemberColumns + `
		FROM channel_members
		WHERE workspace_id = $1 AND channel_id = $2 AND member_type = $3 AND
		      (CASE WHEN member_type = 'user' THEN user_id ELSE agent_id END) = $4::uuid
	`
	return scanChannelMember(cs.db.QueryRow(ctx, query, workspaceID, channelID, string(memberType), refID))
}

// -------------------------------------------------------------------------
// Messages implementation
// -------------------------------------------------------------------------

const channelMessageColumns = `
	id, workspace_id, channel_id, seq, author_type, author_user_id, author_agent_id,
	body, mentions, session_id, turn_id, run_summary, root_message_id, chain_depth,
	work_session_id, is_kickoff, created_at
`

func scanChannelMessage(row pgx.Row) (*domain.ChannelMessage, error) {
	var msg domain.ChannelMessage
	var mentions, runSummary []byte
	// Nullable columns scan through pointers: NULL cannot scan into a plain
	// string.
	var authorUserID, authorAgentID *string
	err := row.Scan(
		&msg.ID,
		&msg.WorkspaceID,
		&msg.ChannelID,
		&msg.Seq,
		&msg.AuthorType,
		&authorUserID,
		&authorAgentID,
		&msg.Body,
		&mentions,
		&msg.SessionID,
		&msg.TurnID,
		&runSummary,
		&msg.RootMessageID,
		&msg.ChainDepth,
		&msg.WorkSessionID,
		&msg.IsKickoff,
		&msg.CreatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if authorUserID != nil {
		msg.AuthorUserID = *authorUserID
	}
	if authorAgentID != nil {
		msg.AuthorAgentID = *authorAgentID
	}
	if msg.Mentions, err = unmarshalChannelMentions(mentions); err != nil {
		return nil, err
	}
	if msg.RunSummary, err = unmarshalRunSummary(runSummary); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (cs *channelStore) InsertChannelMessage(ctx context.Context, message *domain.ChannelMessage) error {
	if message == nil {
		return domain.ErrInvalid
	}
	if err := message.Validate(); err != nil {
		return err
	}

	// The channel must exist within the message's workspace.
	found, err := channelExistsInWorkspace(ctx, cs.db, message.WorkspaceID, message.ChannelID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: channel not found in workspace", domain.ErrNotFound)
	}

	// The author must exist inside the workspace (fake parity; the plain
	// author FK alone would admit cross-workspace authors).
	found, err = channelRefExists(ctx, cs.db, message.WorkspaceID, message.AuthorType, messageAuthorRef(message))
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: %s %q not found in workspace", domain.ErrNotFound, message.AuthorType, messageAuthorRef(message))
	}

	// The chain root, when present, must be an existing message of the same
	// channel (the FK alone would admit cross-channel roots).
	if message.RootMessageID != nil && *message.RootMessageID != "" {
		found, err := rootExistsInChannel(ctx, cs.db, message.WorkspaceID, message.ChannelID, *message.RootMessageID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: root message not found in channel", domain.ErrNotFound)
		}
	}

	if message.ID == "" {
		message.ID = uuid.NewString()
	}
	if message.Mentions == nil {
		message.Mentions = []domain.Mention{}
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}
	mentions, err := marshalChannelMentions(message.Mentions)
	if err != nil {
		return convertError(err)
	}
	runSummary, err := marshalRunSummary(message.RunSummary)
	if err != nil {
		return convertError(err)
	}

	// seq is GENERATED ALWAYS AS IDENTITY — the store never writes it; the
	// assigned value is returned to the caller. Nullable uuid columns bind
	// pointers: an empty Go string would bind as '' (invalid uuid), not NULL.
	query := `
		INSERT INTO channel_messages (
			id, workspace_id, channel_id, author_type, author_user_id, author_agent_id,
			body, mentions, session_id, turn_id, run_summary, root_message_id, chain_depth,
			work_session_id, is_kickoff, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
		)
		RETURNING seq
	`
	err = cs.db.QueryRow(ctx, query,
		message.ID,
		message.WorkspaceID,
		message.ChannelID,
		message.AuthorType,
		nullableRef(message.AuthorUserID),
		nullableRef(message.AuthorAgentID),
		message.Body,
		mentions,
		nullableText(message.SessionID),
		nullableText(message.TurnID),
		runSummary,
		nullableText(message.RootMessageID),
		message.ChainDepth,
		nullableText(message.WorkSessionID),
		message.IsKickoff,
		message.CreatedAt,
	).Scan(&message.Seq)
	if err != nil {
		return convertError(err)
	}
	return nil
}

// messageAuthorRef returns the message's single author reference id.
func messageAuthorRef(message *domain.ChannelMessage) string {
	if message.AuthorType == domain.ChannelMemberTypeAgent {
		return message.AuthorAgentID
	}
	return message.AuthorUserID
}

func (cs *channelStore) UpdateChannelMessageRunSummary(ctx context.Context, workspaceID, channelID, sessionID, turnID string, summary domain.RunSummary) error {
	if workspaceID == "" || channelID == "" || sessionID == "" {
		return domain.ErrInvalid
	}

	payload, err := marshalRunSummary(&summary)
	if err != nil {
		return convertError(err)
	}

	// The turn predicate mirrors the fake: an empty turnID matches NULL
	// turn_id rows (posts that raced turn-id discovery on the stream);
	// otherwise the turn must match exactly. No match updates nothing — a
	// run that posted nothing has no rows to annotate.
	query := `
		UPDATE channel_messages
		SET run_summary = $4
		WHERE workspace_id = $1 AND channel_id = $2 AND session_id = $3 AND
		      (($5 = '' AND turn_id IS NULL) OR turn_id = $5)
	`
	if _, err := cs.db.Exec(ctx, query, workspaceID, channelID, sessionID, payload, turnID); err != nil {
		return convertError(err)
	}
	return nil
}

func (cs *channelStore) ListChannelMessages(ctx context.Context, params storeport.ListChannelMessagesParams) ([]domain.ChannelMessage, error) {
	if params.WorkspaceID == "" || params.ChannelID == "" {
		return []domain.ChannelMessage{}, nil
	}

	query := `
		SELECT ` + channelMessageColumns + `
		FROM channel_messages
		WHERE workspace_id = $1 AND channel_id = $2 AND seq > $3
		ORDER BY seq ASC
	`
	args := []any{params.WorkspaceID, params.ChannelID, params.AfterSeq}
	if params.Limit > 0 {
		query += `
		LIMIT $4
	`
		args = append(args, params.Limit)
	}

	rows, err := cs.db.Query(ctx, query, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	messages := make([]domain.ChannelMessage, 0)
	for rows.Next() {
		message, err := scanChannelMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, *message)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return messages, nil
}

func (cs *channelStore) ChannelMessagesByRoot(ctx context.Context, workspaceID, channelID, rootMessageID string) ([]domain.ChannelMessage, error) {
	if workspaceID == "" || channelID == "" || rootMessageID == "" {
		return []domain.ChannelMessage{}, nil
	}
	// A malformed root id matches nothing; mirror that as an empty result
	// rather than a driver cast error (fake parity).
	if _, err := uuid.Parse(rootMessageID); err != nil {
		return []domain.ChannelMessage{}, nil
	}

	query := `
		SELECT ` + channelMessageColumns + `
		FROM channel_messages
		WHERE workspace_id = $1 AND channel_id = $2 AND root_message_id = $3
		ORDER BY seq ASC
	`
	rows, err := cs.db.Query(ctx, query, workspaceID, channelID, rootMessageID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	messages := make([]domain.ChannelMessage, 0)
	for rows.Next() {
		message, err := scanChannelMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, *message)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return messages, nil
}
