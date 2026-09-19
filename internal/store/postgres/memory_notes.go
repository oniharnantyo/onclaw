package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// memoryEventStore implements storeport.MemoryEventStore for PostgreSQL over
// memory_events (integrate-agent-zero-memory D3). Every read carries the
// workspace partition and the structural visibility scope in its WHERE
// clause; writes are single statements validated through the domain layer.
type memoryEventStore struct {
	db Executor
}

// NewMemoryEventStore creates a new MemoryEventStore with the given database executor.
func NewMemoryEventStore(db Executor) storeport.MemoryEventStore {
	return &memoryEventStore{db: db}
}

// memoryNoteStore implements storeport.MemoryNoteStore for PostgreSQL over
// memory_notes (integrate-agent-zero-memory D4/D5/D6). Supersede is one
// statement — guard CTE, insert, and pointer stamp share a snapshot — so no
// read-modify-write race can produce two current notes for one fact.
type memoryNoteStore struct {
	db Executor
}

// NewMemoryNoteStore creates a new MemoryNoteStore with the given database executor.
func NewMemoryNoteStore(db Executor) storeport.MemoryNoteStore {
	return &memoryNoteStore{db: db}
}

const memoryEventColumns = `id, workspace_id, agent_id, session_id, turn_id, visibility, user_id,
	origin, event_time, learned_at, source_event_id, description, outcome, participants, tombstoned_at`

const memoryNoteColumns = `id, workspace_id, visibility, user_id, agent_id, origin,
	event_time, learned_at, source_event_id, content, importance, pinned, topic, conflict_flag,
	supersedes, superseded_by, promoted_by, promoted_at, tombstoned_at`

// memoryScopeClause is the structural visibility filter shared by every read
// on both tables (integrate-agent-zero-memory D4/D8): shared rows, the
// viewer's own user rows, and the serving agent's rows. NULLIF maps an empty
// identity (a pipeline read with no human context, say) to NULL so it
// contributes no rows of that tier instead of failing uuid coercion.
// Cross-member exclusion happens here, never by post-filtering.
func memoryScopeClause(userParam, agentParam int) string {
	return fmt.Sprintf(
		`(visibility = 'shared' OR (visibility = 'user' AND user_id = NULLIF($%d::text, '')::uuid) OR (visibility = 'agent' AND agent_id = NULLIF($%d::text, '')::uuid))`,
		userParam, agentParam)
}

func unmarshalMemoryParticipants(data []byte) ([]domain.MemoryParticipant, error) {
	if len(data) == 0 {
		return []domain.MemoryParticipant{}, nil
	}
	var ps []domain.MemoryParticipant
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("%w: invalid participants jsonb: %v", domain.ErrInvalid, err)
	}
	if ps == nil {
		ps = []domain.MemoryParticipant{}
	}
	return ps, nil
}

func marshalMemoryParticipants(ps []domain.MemoryParticipant) ([]byte, error) {
	if ps == nil {
		ps = []domain.MemoryParticipant{}
	}
	data, err := json.Marshal(ps)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid participants jsonb: %v", domain.ErrInvalid, err)
	}
	return data, nil
}

func scanMemoryEvent(row pgx.Row) (*domain.MemoryEvent, error) {
	var e domain.MemoryEvent
	var visibility, origin string
	var participants []byte
	err := row.Scan(&e.ID, &e.WorkspaceID, &e.AgentID, &e.SessionID, &e.TurnID,
		&visibility, &e.UserID, &origin, &e.EventTime, &e.LearnedAt,
		&e.SourceEventID, &e.Description, &e.Outcome, &participants, &e.TombstonedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, convertError(err)
	}
	e.Visibility = domain.MemoryVisibility(visibility)
	e.Origin = domain.MemoryOrigin(origin)
	if e.Participants, err = unmarshalMemoryParticipants(participants); err != nil {
		return nil, err
	}
	return &e, nil
}

func scanMemoryNote(row pgx.Row) (*domain.MemoryNote, error) {
	var n domain.MemoryNote
	var visibility, origin string
	err := row.Scan(&n.ID, &n.WorkspaceID, &visibility, &n.UserID, &n.AgentID, &origin,
		&n.EventTime, &n.LearnedAt, &n.SourceEventID, &n.Content, &n.Importance,
		&n.Pinned, &n.Topic, &n.ConflictFlag, &n.Supersedes, &n.SupersededBy,
		&n.PromotedBy, &n.PromotedAt, &n.TombstonedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, convertError(err)
	}
	n.Visibility = domain.MemoryVisibility(visibility)
	n.Origin = domain.MemoryOrigin(origin)
	return &n, nil
}

// memoryEventWhere builds the shared read predicate: tenant partition, live
// rows, structural scope, then the optional filters. Args are returned in
// placeholder order.
func memoryEventWhere(workspaceID, viewerUserID, servingAgentID string, filters storeport.MemoryEventFilters) (string, []any) {
	args := []any{workspaceID, viewerUserID, servingAgentID}
	clause := ` WHERE workspace_id = $1 AND tombstoned_at IS NULL AND ` + memoryScopeClause(2, 3)
	if filters.SessionID != "" {
		args = append(args, filters.SessionID)
		clause += fmt.Sprintf(` AND session_id = $%d`, len(args))
	}
	if filters.AgentID != "" {
		args = append(args, filters.AgentID)
		clause += fmt.Sprintf(` AND agent_id = $%d`, len(args))
	}
	if filters.Visibility != "" {
		args = append(args, string(filters.Visibility))
		clause += fmt.Sprintf(` AND visibility = $%d`, len(args))
	}
	clause, args = appendMemoryTimeWindow(clause, args, filters.TimeWindow)
	return clause, args
}

// appendMemoryTimeWindow appends the event_time range predicates and their
// args in lockstep so placeholder numbers never desync (D9 filters: the
// window bounds when the fact was true). Zero fields are open ends.
func appendMemoryTimeWindow(clause string, args []any, window *storeport.MemoryTimeWindow) (string, []any) {
	if window == nil {
		return clause, args
	}
	if !window.From.IsZero() {
		args = append(args, window.From)
		clause += fmt.Sprintf(` AND event_time >= $%d`, len(args))
	}
	if !window.To.IsZero() {
		args = append(args, window.To)
		clause += fmt.Sprintf(` AND event_time < $%d`, len(args))
	}
	return clause, args
}

func (es *memoryEventStore) InsertEvent(ctx context.Context, event *domain.MemoryEvent) error {
	if event == nil || event.WorkspaceID == "" || event.AgentID == "" || event.SessionID == "" || event.TurnID == "" {
		return domain.ErrInvalid
	}
	if !domain.ValidMemoryVisibility(event.Visibility) {
		return fmt.Errorf("%w: unknown memory visibility %q", domain.ErrInvalid, event.Visibility)
	}
	if err := domain.ValidateMemoryEventOwner(event.Visibility, event.UserID); err != nil {
		return err
	}
	// Provenance at birth (D5): no row without the complete tuple.
	if err := domain.ValidateMemoryProvenance(event.Origin, event.EventTime, event.LearnedAt, event.SourceEventID); err != nil {
		return err
	}
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	participants, err := marshalMemoryParticipants(event.Participants)
	if err != nil {
		return err
	}

	const query = `
		INSERT INTO memory_events (
			id, workspace_id, agent_id, session_id, turn_id, visibility, user_id,
			origin, event_time, learned_at, source_event_id, description, outcome,
			participants
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	if _, err := es.db.Exec(ctx, query,
		event.ID, event.WorkspaceID, event.AgentID, event.SessionID, event.TurnID,
		string(event.Visibility), event.UserID, string(event.Origin),
		event.EventTime, event.LearnedAt, event.SourceEventID,
		event.Description, event.Outcome, participants); err != nil {
		return convertError(err)
	}
	return nil
}

func (es *memoryEventStore) LatestEventForSession(ctx context.Context, workspaceID, sessionID string) (*domain.MemoryEvent, error) {
	if workspaceID == "" || sessionID == "" {
		return nil, nil
	}

	const query = `
		SELECT ` + memoryEventColumns + `
		FROM memory_events
		WHERE workspace_id = $1 AND session_id = $2 AND tombstoned_at IS NULL
		ORDER BY event_time DESC, learned_at DESC, id DESC
		LIMIT 1
	`
	e, err := scanMemoryEvent(es.db.QueryRow(ctx, query, workspaceID, sessionID))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

func (es *memoryEventStore) ListEventsForUI(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, filters storeport.MemoryEventFilters) ([]domain.MemoryEvent, error) {
	return es.queryEvents(ctx, workspaceID, viewerUserID, servingAgentID, filters, "")
}

func (es *memoryEventStore) SearchEvents(ctx context.Context, workspaceID, viewerUserID, servingAgentID, query string, filters storeport.MemoryEventFilters) ([]domain.MemoryEvent, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: search query is empty", domain.ErrInvalid)
	}
	return es.queryEvents(ctx, workspaceID, viewerUserID, servingAgentID, filters, query)
}

// queryEvents runs the shared read: filters, then the lexical predicate
// (any-term tsvector OR full-string ILIKE — must keep the exact indexed
// to_tsvector expression, D9), best match first (fix-memory-prefetch-matching
// D5: same any-term/rank construction as the notes store).
func (es *memoryEventStore) queryEvents(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, filters storeport.MemoryEventFilters, queryText string) ([]domain.MemoryEvent, error) {
	if workspaceID == "" {
		return []domain.MemoryEvent{}, nil
	}
	clause, args := memoryEventWhere(workspaceID, viewerUserID, servingAgentID, filters)
	var shaped memoryLexicalQuery
	if queryText != "" {
		shaped = shapeMemoryLexicalQuery(queryText)
		args = append(args, queryText)
		clause += fmt.Sprintf(` AND (description ILIKE '%%' || $%d || '%%'
		       OR outcome ILIKE '%%' || $%d || '%%'`, len(args), len(args))
		if shaped.termCount > 0 {
			args = append(args, shaped.tsquery)
			clause += fmt.Sprintf(` OR to_tsvector('english', description || ' ' || outcome) @@ to_tsquery('english', $%d)`, len(args))
		}
		clause += `)`
	}
	if shaped.termCount > 0 {
		args = append(args, shaped.tsquery)
		clause += fmt.Sprintf(` ORDER BY ts_rank(to_tsvector('english', description || ' ' || outcome), to_tsquery('english', $%d)) DESC, event_time DESC, learned_at DESC, id DESC`, len(args))
	} else {
		clause += ` ORDER BY event_time DESC, learned_at DESC, id DESC`
	}
	if filters.Limit > 0 {
		args = append(args, filters.Limit)
		clause += fmt.Sprintf(` LIMIT $%d`, len(args))
	}

	rows, err := es.db.Query(ctx, `SELECT `+memoryEventColumns+` FROM memory_events`+clause, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	events := make([]domain.MemoryEvent, 0)
	for rows.Next() {
		e, err := scanMemoryEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return events, nil
}

func (es *memoryEventStore) TombstoneEvent(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrInvalid
	}

	// Deletion is a recorded tombstone (D6); an already-tombstoned row is
	// indistinguishable from an absent one — no existence leak.
	const query = `
		UPDATE memory_events
		SET tombstoned_at = now()
		WHERE id = $2 AND workspace_id = $1 AND tombstoned_at IS NULL
	`
	tag, err := es.db.Exec(ctx, query, workspaceID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (es *memoryEventStore) CountByVisibility(ctx context.Context, workspaceID, sessionID, turnID string) (map[domain.MemoryVisibility]int, error) {
	counts := make(map[domain.MemoryVisibility]int)
	if workspaceID == "" || sessionID == "" || turnID == "" {
		return counts, nil
	}

	const query = `
		SELECT visibility, count(*)
		FROM memory_events
		WHERE workspace_id = $1 AND session_id = $2 AND turn_id = $3 AND tombstoned_at IS NULL
		GROUP BY visibility
	`
	rows, err := es.db.Query(ctx, query, workspaceID, sessionID, turnID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	for rows.Next() {
		var visibility string
		var n int
		if err := rows.Scan(&visibility, &n); err != nil {
			return nil, convertError(err)
		}
		counts[domain.MemoryVisibility(visibility)] = n
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return counts, nil
}

// validateMemoryNoteForWrite runs the shared write-path validation: scope,
// tier validity, owner shape, ceiling dominance (D4), and the provenance
// birth tuple (D5) — the same rules the SQL CHECK constraints pin.
func validateMemoryNoteForWrite(note *domain.MemoryNote, ceiling domain.MemoryVisibility) error {
	if note == nil || note.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if !domain.ValidMemoryVisibility(note.Visibility) {
		return fmt.Errorf("%w: unknown memory visibility %q", domain.ErrInvalid, note.Visibility)
	}
	if err := domain.ValidateMemoryNoteOwner(note.Visibility, note.UserID, note.AgentID); err != nil {
		return err
	}
	if err := domain.ValidateMemoryVisibilityWithin(ceiling, note.Visibility); err != nil {
		return err
	}
	return domain.ValidateMemoryProvenance(note.Origin, note.EventTime, note.LearnedAt, note.SourceEventID)
}

func (ns *memoryNoteStore) InsertNote(ctx context.Context, note *domain.MemoryNote, ceiling domain.MemoryVisibility) error {
	if err := validateMemoryNoteForWrite(note, ceiling); err != nil {
		return err
	}
	if note.ID == "" {
		note.ID = uuid.NewString()
	}

	const query = `
		INSERT INTO memory_notes (
			id, workspace_id, visibility, user_id, agent_id, origin,
			event_time, learned_at, source_event_id, content, importance,
			pinned, topic, conflict_flag
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	if _, err := ns.db.Exec(ctx, query,
		note.ID, note.WorkspaceID, string(note.Visibility), note.UserID, note.AgentID,
		string(note.Origin), note.EventTime, note.LearnedAt, note.SourceEventID,
		note.Content, note.Importance, note.Pinned, note.Topic, note.ConflictFlag); err != nil {
		return convertError(err)
	}
	return nil
}

func (ns *memoryNoteStore) SupersedeNote(ctx context.Context, workspaceID, oldID string, note *domain.MemoryNote, ceiling domain.MemoryVisibility) error {
	if workspaceID == "" || oldID == "" {
		return domain.ErrInvalid
	}
	if err := validateMemoryNoteForWrite(note, ceiling); err != nil {
		return err
	}
	if note.ID == "" {
		note.ID = uuid.NewString()
	}

	// Single statement (D6): the old row is locked and re-checked, the
	// correction inserts only while the guard holds, and the superseded_by
	// pointer is stamped in the same snapshot — a failed guard writes zero
	// rows, and concurrent supersedes serialize on the row lock.
	const query = `
		WITH old_note AS (
			SELECT id FROM memory_notes
			WHERE id = $1 AND workspace_id = $2
			  AND tombstoned_at IS NULL AND superseded_by IS NULL
			FOR UPDATE
		), inserted AS (
			INSERT INTO memory_notes (
				id, workspace_id, visibility, user_id, agent_id, origin,
				event_time, learned_at, source_event_id, content, importance,
				pinned, topic, conflict_flag, supersedes
			)
			SELECT $3, $2, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $1
			FROM old_note
			RETURNING id
		)
		UPDATE memory_notes old
		SET superseded_by = (SELECT id FROM inserted)
		WHERE old.id = $1 AND EXISTS (SELECT 1 FROM inserted)
		RETURNING old.id
	`
	tag, err := ns.db.Exec(ctx, query,
		oldID, workspaceID, note.ID, string(note.Visibility), note.UserID, note.AgentID,
		string(note.Origin), note.EventTime, note.LearnedAt, note.SourceEventID,
		note.Content, note.Importance, note.Pinned, note.Topic, note.ConflictFlag)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		// Disambiguate the blocked guard: unknown and tombstoned are
		// ErrNotFound (hidden rows leak nothing), already-superseded is a
		// conflict — a correction already landed.
		var tombstoned, superseded bool
		scanErr := ns.db.QueryRow(ctx,
			`SELECT tombstoned_at IS NOT NULL, superseded_by IS NOT NULL
			 FROM memory_notes WHERE id = $2 AND workspace_id = $1`,
			workspaceID, oldID).Scan(&tombstoned, &superseded)
		if scanErr != nil {
			return convertError(scanErr)
		}
		if tombstoned {
			return domain.ErrNotFound
		}
		return fmt.Errorf("%w: note %s already superseded", domain.ErrConflict, oldID)
	}

	note.Supersedes = &oldID
	return nil
}

func (ns *memoryNoteStore) TombstoneNote(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrInvalid
	}

	// Same tombstone semantics as TombstoneEvent (D6).
	const query = `
		UPDATE memory_notes
		SET tombstoned_at = now()
		WHERE id = $2 AND workspace_id = $1 AND tombstoned_at IS NULL
	`
	tag, err := ns.db.Exec(ctx, query, workspaceID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (ns *memoryNoteStore) PromoteNote(ctx context.Context, workspaceID, id, promotedByUserID string) error {
	if workspaceID == "" || id == "" || promotedByUserID == "" {
		return domain.ErrInvalid
	}

	// The only widening path (D4): a human promotes, the audit lands in the
	// same statement, and the owner columns are cleared because shared rows
	// carry no owner (the CHECK pins that shape).
	const query = `
		UPDATE memory_notes
		SET visibility = 'shared',
		    user_id = NULL,
		    agent_id = NULL,
		    promoted_by = $3,
		    promoted_at = now()
		WHERE id = $2 AND workspace_id = $1
		  AND tombstoned_at IS NULL AND superseded_by IS NULL
		  AND visibility <> 'shared'
	`
	tag, err := ns.db.Exec(ctx, query, workspaceID, id, promotedByUserID)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		var visibility string
		var tombstoned, superseded bool
		scanErr := ns.db.QueryRow(ctx,
			`SELECT visibility, tombstoned_at IS NOT NULL, superseded_by IS NOT NULL
			 FROM memory_notes WHERE id = $2 AND workspace_id = $1`,
			workspaceID, id).Scan(&visibility, &tombstoned, &superseded)
		if scanErr != nil {
			return convertError(scanErr)
		}
		if tombstoned || superseded {
			return domain.ErrNotFound
		}
		return fmt.Errorf("%w: note is already shared", domain.ErrConflict)
	}
	return nil
}

func (ns *memoryNoteStore) GetNote(ctx context.Context, workspaceID, viewerUserID, servingAgentID, id string) (*domain.MemoryNote, error) {
	if workspaceID == "" || id == "" {
		return nil, nil
	}

	query := `
		SELECT ` + memoryNoteColumns + `
		FROM memory_notes
		WHERE id = $4 AND workspace_id = $1 AND tombstoned_at IS NULL
		  AND ` + memoryScopeClause(2, 3)
	n, err := scanMemoryNote(ns.db.QueryRow(ctx, query, workspaceID, viewerUserID, servingAgentID, id))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return n, nil
}

func (ns *memoryNoteStore) ListNotesForUI(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, filters storeport.MemoryNoteFilters) ([]domain.MemoryNote, error) {
	return ns.queryNotes(ctx, workspaceID, viewerUserID, servingAgentID, filters, "", filters.History)
}

func (ns *memoryNoteStore) SearchNotes(ctx context.Context, workspaceID, viewerUserID, servingAgentID, query string, filters storeport.MemoryNoteFilters) ([]domain.MemoryNote, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: search query is empty", domain.ErrInvalid)
	}
	// Retrieval is current-state only: a superseded note is dead for search.
	return ns.queryNotes(ctx, workspaceID, viewerUserID, servingAgentID, filters, query, false)
}

func (ns *memoryNoteStore) queryNotes(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, filters storeport.MemoryNoteFilters, queryText string, includeSuperseded bool) ([]domain.MemoryNote, error) {
	if workspaceID == "" {
		return []domain.MemoryNote{}, nil
	}
	args := []any{workspaceID, viewerUserID, servingAgentID}
	clause := ` WHERE workspace_id = $1 AND tombstoned_at IS NULL AND ` + memoryScopeClause(2, 3)
	if !includeSuperseded {
		clause += ` AND superseded_by IS NULL`
	}
	if filters.Visibility != "" {
		args = append(args, string(filters.Visibility))
		clause += fmt.Sprintf(` AND visibility = $%d`, len(args))
	}
	if filters.Topic != "" {
		args = append(args, filters.Topic)
		clause += fmt.Sprintf(` AND topic = $%d`, len(args))
	}
	clause, args = appendMemoryTimeWindow(clause, args, filters.TimeWindow)
	// Lexical match (fix-memory-prefetch-matching D2/D3): any-term semantics —
	// the OR-joined sanitized tsquery no longer starves on a missing term,
	// while the full-string ILIKE fallback still catches verbatim identifiers
	// the tokenizer splits apart. A query with zero surviving terms (pure
	// punctuation) keeps only the ILIKE leg. Rank ordering puts term overlap
	// ahead of recency (ILIKE-only hits carry ts_rank 0 and sort last among
	// equals), so top-k truncation keeps the closest matches.
	var shaped memoryLexicalQuery
	if queryText != "" {
		shaped = shapeMemoryLexicalQuery(queryText)
		args = append(args, queryText)
		clause += fmt.Sprintf(` AND (content ILIKE '%%' || $%d || '%%'`, len(args))
		if shaped.termCount > 0 {
			args = append(args, shaped.tsquery)
			clause += fmt.Sprintf(` OR to_tsvector('english', content) @@ to_tsquery('english', $%d)`, len(args))
		}
		clause += `)`
	}
	if shaped.termCount > 0 {
		args = append(args, shaped.tsquery)
		clause += fmt.Sprintf(` ORDER BY pinned DESC, ts_rank(to_tsvector('english', content), to_tsquery('english', $%d)) DESC, learned_at DESC, id DESC`, len(args))
	} else {
		clause += ` ORDER BY pinned DESC, learned_at DESC, id DESC`
	}
	if filters.Limit > 0 {
		args = append(args, filters.Limit)
		clause += fmt.Sprintf(` LIMIT $%d`, len(args))
	}

	rows, err := ns.db.Query(ctx, `SELECT `+memoryNoteColumns+` FROM memory_notes`+clause, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	notes := make([]domain.MemoryNote, 0)
	for rows.Next() {
		n, err := scanMemoryNote(rows)
		if err != nil {
			return nil, err
		}
		notes = append(notes, *n)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return notes, nil
}

func (ns *memoryNoteStore) CountSimilar(ctx context.Context, workspaceID, viewerUserID, servingAgentID, content string, threshold float64) (int, error) {
	if workspaceID == "" || strings.TrimSpace(content) == "" {
		return 0, nil
	}

	query := `
		SELECT count(*)
		FROM memory_notes
		WHERE workspace_id = $1 AND tombstoned_at IS NULL AND superseded_by IS NULL
		  AND ` + memoryScopeClause(2, 3) + `
		  AND similarity(content, $4) > $5
	`
	var n int
	if err := ns.db.QueryRow(ctx, query, workspaceID, viewerUserID, servingAgentID, content, threshold).Scan(&n); err != nil {
		return 0, convertError(err)
	}
	return n, nil
}

func (ns *memoryNoteStore) CountByVisibility(ctx context.Context, workspaceID string) (map[domain.MemoryVisibility]int, error) {
	counts := make(map[domain.MemoryVisibility]int)
	if workspaceID == "" {
		return counts, nil
	}

	const query = `
		SELECT visibility, count(*)
		FROM memory_notes
		WHERE workspace_id = $1 AND tombstoned_at IS NULL
		GROUP BY visibility
	`
	rows, err := ns.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	for rows.Next() {
		var visibility string
		var n int
		if err := rows.Scan(&visibility, &n); err != nil {
			return nil, convertError(err)
		}
		counts[domain.MemoryVisibility(visibility)] = n
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return counts, nil
}

// SupersedeInto implements the consolidation merge primitive (D12): the
// canonical survivor keeps its row and every folded duplicate is pointed at
// it in one guarded statement — no insert, no rewrite, no delete (D6).
func (ns *memoryNoteStore) SupersedeInto(ctx context.Context, workspaceID, oldID, intoID string) error {
	if workspaceID == "" || oldID == "" || intoID == "" {
		return domain.ErrInvalid
	}
	if oldID == intoID {
		return fmt.Errorf("%w: a note cannot be superseded into itself", domain.ErrInvalid)
	}

	// The survivor is locked and re-checked, then the duplicate's pointer is
	// stamped under the same guard — a failed guard writes zero rows and
	// concurrent folds serialize on the row locks (the SupersedeNote CTE
	// pattern, without the insert leg).
	const query = `
		WITH survivor AS (
			SELECT id FROM memory_notes
			WHERE id = $3 AND workspace_id = $1
			  AND tombstoned_at IS NULL AND superseded_by IS NULL
			FOR UPDATE
		), folded AS (
			UPDATE memory_notes old
			SET superseded_by = (SELECT id FROM survivor)
			WHERE old.id = $2 AND old.workspace_id = $1
			  AND old.tombstoned_at IS NULL AND old.superseded_by IS NULL
			  AND EXISTS (SELECT 1 FROM survivor)
			RETURNING old.id
		)
		SELECT count(*) FROM folded
	`
	var folded int
	if err := ns.db.QueryRow(ctx, query, workspaceID, oldID, intoID).Scan(&folded); err != nil {
		return convertError(err)
	}
	if folded == 0 {
		// Disambiguate the blocked guard the way SupersedeNote does.
		var oldTombstoned, oldSuperseded bool
		scanErr := ns.db.QueryRow(ctx,
			`SELECT tombstoned_at IS NOT NULL, superseded_by IS NOT NULL
			 FROM memory_notes WHERE id = $2 AND workspace_id = $1`, workspaceID, oldID).
			Scan(&oldTombstoned, &oldSuperseded)
		if scanErr != nil {
			// The duplicate itself is absent — the survivor state is unknown
			// but the missing duplicate fails the fold regardless.
			return convertError(scanErr)
		}
		if oldTombstoned {
			return domain.ErrNotFound
		}
		if oldSuperseded {
			return fmt.Errorf("%w: note %s already superseded", domain.ErrConflict, oldID)
		}
		// The duplicate is live, so the survivor guard blocked it: missing,
		// tombstoned, or already-superseded survivors all read as not found.
		return domain.ErrNotFound
	}
	return nil
}

// SetNoteTopic labels one live note for the consolidator's topic fold (D12).
func (ns *memoryNoteStore) SetNoteTopic(ctx context.Context, workspaceID, noteID, topic string) error {
	if workspaceID == "" || noteID == "" || strings.TrimSpace(topic) == "" {
		return domain.ErrInvalid
	}

	// Current-state rows only: a tombstoned or already-superseded note has
	// left the browsable set and must not be relabeled (D6).
	const query = `
		UPDATE memory_notes
		SET topic = $3
		WHERE id = $2 AND workspace_id = $1
		  AND tombstoned_at IS NULL AND superseded_by IS NULL
	`
	tag, err := ns.db.Exec(ctx, query, workspaceID, noteID, strings.TrimSpace(topic))
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// AddNoteEvidence links additional raw-event evidence on a note (D12's
// multi-evidence), idempotent per (note, source event).
func (ns *memoryNoteStore) AddNoteEvidence(ctx context.Context, workspaceID, noteID string, sourceEventIDs []string) error {
	if workspaceID == "" || noteID == "" {
		return domain.ErrInvalid
	}

	// One statement per link: the guard leg proves the note is a live
	// workspace row (zero rows there fail the call with ErrNotFound), the
	// insert leg is idempotent per (note, source event) and keeps the first
	// link's added_at.
	const query = `
		WITH note AS (
			SELECT id FROM memory_notes
			WHERE id = $2 AND workspace_id = $1 AND tombstoned_at IS NULL
		), linked AS (
			INSERT INTO memory_note_evidence (note_id, source_event_id)
			SELECT $2, $3 FROM note
			ON CONFLICT (note_id, source_event_id) DO NOTHING
			RETURNING note_id
		)
		SELECT (SELECT count(*) FROM note)::int, (SELECT count(*) FROM linked)::int
	`
	for _, sourceEventID := range sourceEventIDs {
		if sourceEventID == "" {
			continue
		}
		var noteRows, linkedRows int
		if err := ns.db.QueryRow(ctx, query, workspaceID, noteID, sourceEventID).Scan(&noteRows, &linkedRows); err != nil {
			return convertError(err)
		}
		if noteRows == 0 {
			return domain.ErrNotFound
		}
	}
	return nil
}

// ListNoteEvidence returns the note's multi-evidence links, oldest first,
// workspace-scoped through the memory_notes join.
func (ns *memoryNoteStore) ListNoteEvidence(ctx context.Context, workspaceID, noteID string) ([]domain.MemoryNoteEvidence, error) {
	if workspaceID == "" || noteID == "" {
		return []domain.MemoryNoteEvidence{}, nil
	}

	const query = `
		SELECT e.source_event_id, e.added_at
		FROM memory_note_evidence e
		JOIN memory_notes n ON n.id = e.note_id
		WHERE e.note_id = $2 AND n.workspace_id = $1
		ORDER BY e.added_at ASC, e.source_event_id ASC
	`
	rows, err := ns.db.Query(ctx, query, workspaceID, noteID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	evidence := make([]domain.MemoryNoteEvidence, 0)
	for rows.Next() {
		var ev domain.MemoryNoteEvidence
		if err := rows.Scan(&ev.SourceEventID, &ev.AddedAt); err != nil {
			return nil, convertError(err)
		}
		evidence = append(evidence, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return evidence, nil
}
