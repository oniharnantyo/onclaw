package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/oniharnantyo/onclaw/internal/memory"
)

// sqliteEpisodicStore implements memory.EpisodicStore backed by the
// episodic_summaries table.
type sqliteEpisodicStore struct {
	db *sql.DB
}

// NewEpisodicStore creates a new EpisodicStore backed by SQLite.
func NewEpisodicStore(db *sql.DB) memory.EpisodicStore {
	return &sqliteEpisodicStore{db: db}
}

func (s *sqliteEpisodicStore) AppendEpisodic(ctx context.Context, agent string, summary string, l0Abstract string, keyTopics string, sourceID string, expiresAt string) (int64, error) {
	now := time.Now().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO episodic_summaries (agent, summary, l0_abstract, key_topics, source_id, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent, source_id) WHERE source_id != '' DO NOTHING`,
		agent, summary, l0Abstract, keyTopics, sourceID, expiresAt, now,
	)
	if err != nil {
		return 0, fmt.Errorf("append episodic: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	// When ON CONFLICT DO NOTHING suppresses the insert, LastInsertId returns 0.
	// Look up the existing row to return its real ID.
	if id == 0 && sourceID != "" {
		var existingID int64
		err := s.db.QueryRowContext(ctx, `
			SELECT id FROM episodic_summaries WHERE agent = ? AND source_id = ?`,
			agent, sourceID,
		).Scan(&existingID)
		if err == nil {
			return existingID, nil
		}
	}
	return id, nil
}

func (s *sqliteEpisodicStore) ListUnpromoted(ctx context.Context, agent string) ([]*memory.EpisodicSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent, summary, l0_abstract, key_topics, source_id, promoted_at, expires_at, created_at
		FROM episodic_summaries
		WHERE agent = ? AND promoted_at IS NULL
		ORDER BY created_at ASC`,
		agent,
	)
	if err != nil {
		return nil, fmt.Errorf("list unpromoted: %w", err)
	}
	defer rows.Close()

	return scanEpisodicRows(rows)
}

func (s *sqliteEpisodicStore) CountUnpromoted(ctx context.Context, agent string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM episodic_summaries
		WHERE agent = ? AND promoted_at IS NULL`,
		agent,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count unpromoted: %w", err)
	}
	return count, nil
}

func (s *sqliteEpisodicStore) MarkPromoted(ctx context.Context, id int64) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE episodic_summaries
		SET promoted_at = ?
		WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("mark promoted: %w", err)
	}
	return nil
}

func (s *sqliteEpisodicStore) PruneExpired(ctx context.Context) (int64, error) {
	now := time.Now().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM episodic_summaries
		WHERE expires_at < ?`,
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("prune expired: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}

func (s *sqliteEpisodicStore) GetEpisodic(ctx context.Context, id int64) (*memory.EpisodicSummary, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, agent, summary, l0_abstract, key_topics, source_id, promoted_at, expires_at, created_at
		FROM episodic_summaries
		WHERE id = ?`, id)
	var es memory.EpisodicSummary
	var promotedAt sql.NullString
	if err := row.Scan(&es.ID, &es.Agent, &es.Summary, &es.L0Abstract, &es.KeyTopics, &es.SourceID, &promotedAt, &es.ExpiresAt, &es.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get episodic %d: %w", id, err)
	}
	if promotedAt.Valid {
		es.PromotedAt = &promotedAt.String
	}
	return &es, nil
}

func scanEpisodicRows(rows *sql.Rows) ([]*memory.EpisodicSummary, error) {
	var episodes []*memory.EpisodicSummary
	for rows.Next() {
		var es memory.EpisodicSummary
		var promotedAt sql.NullString
		if err := rows.Scan(&es.ID, &es.Agent, &es.Summary, &es.L0Abstract, &es.KeyTopics, &es.SourceID, &promotedAt, &es.ExpiresAt, &es.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan episodic: %w", err)
		}
		if promotedAt.Valid {
			es.PromotedAt = &promotedAt.String
		}
		episodes = append(episodes, &es)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return episodes, nil
}
