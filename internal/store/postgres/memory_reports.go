package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// memoryReportStore implements storeport.MemoryReportStore for PostgreSQL
// over memory_reports (integrate-agent-zero-memory D12): the last morning
// report per workspace, replaced on every consolidation pass. Absence is a
// normal state — Get returns (nil, nil) per the MemoryStore convention.
type memoryReportStore struct {
	db Executor
}

// NewMemoryReportStore creates a new MemoryReportStore with the given database executor.
func NewMemoryReportStore(db Executor) storeport.MemoryReportStore {
	return &memoryReportStore{db: db}
}

func (rs *memoryReportStore) Save(ctx context.Context, workspaceID string, report []byte, generatedAt time.Time) error {
	if workspaceID == "" || len(report) == 0 || generatedAt.IsZero() {
		return domain.ErrInvalid
	}

	// One row per workspace: the new report replaces the previous one.
	const query = `
		INSERT INTO memory_reports (workspace_id, report, generated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (workspace_id) DO UPDATE SET
			report = EXCLUDED.report,
			generated_at = EXCLUDED.generated_at
	`
	if _, err := rs.db.Exec(ctx, query, workspaceID, report, generatedAt); err != nil {
		return convertError(err)
	}
	return nil
}

func (rs *memoryReportStore) Get(ctx context.Context, workspaceID string) (*domain.MemoryReport, error) {
	if workspaceID == "" {
		return nil, nil
	}

	const query = `
		SELECT workspace_id, report, generated_at
		FROM memory_reports
		WHERE workspace_id = $1
	`
	var r domain.MemoryReport
	var report []byte
	err := rs.db.QueryRow(ctx, query, workspaceID).Scan(&r.WorkspaceID, &report, &r.GeneratedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, convertError(err)
	}
	r.Report = report
	return &r, nil
}
