package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// maxSlugLen mirrors the DNS-label cap of the workspaces package (kept
// local to leave that package's pinned API untouched).
const maxSlugLen = 63

// seedHeading prefixes every backfilled IDENTITY.md so the carried-over
// content is recognizable (design D10).
const seedHeading = "## Carried over from previous version"

// backfillLegacySystemPrompts carries pre-change agents' system_prompt
// content into the workspace layout (design D2 + D10): every agent row with
// a populated legacy prompt and no slug yet gets a slug (slugified name,
// deduped against the existing ones), a workspace directory, an IDENTITY.md
// seeded with the old prompt under the carried-over heading, and
// prompts_status ready. Rows already migrated are skipped, so a second run
// is a no-op. It runs through the migrate hook between migrations 000004
// (columns added, system_prompt kept) and 000005 (column dropped); any
// failure fails the open — the same startup gate as a migration failure.
func backfillLegacySystemPrompts(ctx context.Context, db *sql.DB, workspacesRoot string) error {
	// Guard: only between 000004 and 000005 does the legacy column still
	// exist; afterwards there is nothing to carry over and the scan query
	// would fail on the missing column.
	legacy, err := columnExists(ctx, db, "agents", "system_prompt")
	if err != nil {
		return fmt.Errorf("backfill legacy system prompts: %w", err)
	}
	if !legacy {
		return nil
	}

	rows, err := db.QueryContext(ctx, `
SELECT id, name, system_prompt FROM agents
WHERE system_prompt != '' AND (slug IS NULL OR slug = '')
ORDER BY created_at, id`)
	if err != nil {
		return fmt.Errorf("backfill legacy system prompts: query: %w", err)
	}
	// Collect before writing: SQLite tolerates concurrent readers and
	// writers, but a bounded snapshot keeps the pass deterministic.
	type legacyRow struct{ id, name, prompt string }
	pending := make([]legacyRow, 0)
	for rows.Next() {
		var r legacyRow
		if err := rows.Scan(&r.id, &r.name, &r.prompt); err != nil {
			_ = rows.Close()
			return fmt.Errorf("backfill legacy system prompts: scan: %w", err)
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("backfill legacy system prompts: iterate: %w", err)
	}
	rows.Close()
	if len(pending) == 0 {
		return nil // Fresh or fully migrated database: nothing to do.
	}

	taken, err := existingSlugs(ctx, db)
	if err != nil {
		return fmt.Errorf("backfill legacy system prompts: %w", err)
	}

	ws := workspaces.NewManager(workspacesRoot)
	for _, r := range pending {
		slug, err := uniqueSlug(r.name, taken)
		if err != nil {
			return fmt.Errorf("backfill legacy system prompts: agent %s: %w", r.id, err)
		}
		dir, err := ws.Create(slug)
		if err != nil {
			return fmt.Errorf("backfill legacy system prompts: agent %s: %w", r.id, err)
		}
		// A plain overwrite is safe here: the content is deterministic and
		// the row is only stamped after the file lands, so a crash between
		// the two simply redoes both on the next startup.
		doc := filepath.Join(dir, workspaces.DocIdentity)
		content := seedHeading + "\n\n" + r.prompt + "\n"
		if err := os.WriteFile(doc, []byte(content), 0600); err != nil {
			return fmt.Errorf("backfill legacy system prompts: write %s: %w", doc, err)
		}
		if _, err := db.ExecContext(ctx, `
UPDATE agents SET slug = ?, prompts_status = ?, updated_at = ?
WHERE id = ?`,
			slug, store.PromptsStatusReady, formatTime(nowUTC()), r.id,
		); err != nil {
			return fmt.Errorf("backfill legacy system prompts: update agent %s: %w", r.id, err)
		}
		taken[slug] = true
	}
	return nil
}

// existingSlugs returns the set of slugs already assigned in the agents
// table, for the backfill's dedupe.
func existingSlugs(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT slug FROM agents WHERE slug IS NOT NULL AND slug != ''`)
	if err != nil {
		return nil, fmt.Errorf("query existing slugs: %w", err)
	}
	defer rows.Close()

	taken := make(map[string]bool)
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, fmt.Errorf("scan existing slugs: %w", err)
		}
		taken[slug] = true
	}
	return taken, rows.Err()
}

// uniqueSlug slugifies name and, when taken, appends -2, -3, … until a free
// slug is found — every candidate stays a valid DNS-label slug of at most
// 63 characters.
func uniqueSlug(name string, taken map[string]bool) (string, error) {
	base := workspaces.Slugify(name)
	if !workspaces.ValidSlug(base) {
		return "", fmt.Errorf("name %q yields no valid slug", name)
	}
	if !taken[base] {
		return base, nil
	}
	for n := 2; ; n++ {
		candidate := withSuffix(base, strconv.Itoa(n))
		if !taken[candidate] {
			return candidate, nil
		}
	}
}

// withSuffix appends -<suffix> to base, truncating the base when the result
// would exceed the slug length cap (trimming any dangling dash the cut
// leaves, so the candidate stays a valid slug).
func withSuffix(base, suffix string) string {
	full := "-" + suffix
	if len(base)+len(full) <= maxSlugLen {
		return base + full
	}
	return strings.TrimRight(base[:maxSlugLen-len(full)], "-") + full
}

// columnExists reports whether a column is present on a table.
func columnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("inspect %s.%s: %w", table, column, err)
	}
	return n > 0, nil
}
