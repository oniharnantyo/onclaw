package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// schemaVersionTable tracks applied migration versions. It is created
// outside the per-migration transactions because it must exist before the
// first version is recorded.
const schemaVersionTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
)`

// backfillBoundaryVersion is the schema version the Go backfill of legacy
// system prompts runs against: migration 000004 added the prompt-document
// columns while keeping system_prompt, and 000005 (and anything newer)
// must wait until the backfill has carried the legacy content over
// (design D10).
const backfillBoundaryVersion = 4

// backfillFunc is the Go backfill hook migrate runs against the boundary
// schema, just before the first migration newer than
// backfillBoundaryVersion is applied.
type backfillFunc func(ctx context.Context, db *sql.DB) error

// migrate applies every embedded migration newer than the database's current
// schema version, in order, one transaction per migration: a migration is
// either fully applied and recorded, or not applied at all. When backfill
// is non-nil it runs once against the boundary schema, before the first
// migration above backfillBoundaryVersion — SQLite cannot run Go between
// embedded SQL migrations, so the hook is a runner step. The first failure
// (migration or backfill) aborts the run with a clear, attributed error —
// the caller (Open) refuses to return a store, preventing startup on a
// stale or broken schema.
func migrate(ctx context.Context, db *sql.DB, migs []migration, backfill backfillFunc) error {
	if _, err := db.ExecContext(ctx, schemaVersionTable); err != nil {
		return fmt.Errorf("create schema version table: %w", err)
	}

	current, err := currentVersion(ctx, db)
	if err != nil {
		return err
	}

	backfillDone := false
	for _, m := range migs {
		if m.Version <= current {
			continue
		}
		if backfill != nil && !backfillDone && m.Version > backfillBoundaryVersion {
			if err := backfill(ctx, db); err != nil {
				return fmt.Errorf("backfill after migration %d: %w", backfillBoundaryVersion, err)
			}
			backfillDone = true
		}
		if err := applyMigration(ctx, db, m); err != nil {
			return err
		}
	}
	return nil
}

// currentVersion reads the highest applied schema version (0 for a fresh
// database).
func currentVersion(ctx context.Context, db *sql.DB) (int, error) {
	var current int
	err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return current, nil
}

// applyMigration runs one migration inside a single transaction: the schema
// change and its version record commit together or not at all.
func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migration %03d %s: begin: %w", m.Version, m.Name, err)
	}
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migration %03d %s: %w", m.Version, m.Name, err)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		m.Version, time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migration %03d %s: record version: %w", m.Version, m.Name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migration %03d %s: commit: %w", m.Version, m.Name, err)
	}
	return nil
}
