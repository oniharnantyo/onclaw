package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
)

// The retired browser tool's per-agent allowlist migration
// (rebrand-browser-use-system-mcp task 4.2, design D6): the browser
// capability left the built-in catalog and now rides the system MCP server,
// so a stored entry named "browser" in an agent's tools allowlist maps to
// "browser-use" in its mcp_servers allowlist.
//
// Both columns are plain allowlists — empty means every entry, a non-empty
// list names exactly the permitted entries; the repo has no exclusion syntax
// (an entry is excluded by absence). The mapping is therefore pinned→pinned:
// only rows whose tools list actually names "browser" are rewritten — the
// entry is removed from tools and "browser-use" is appended to mcp_servers
// when not already present, preserving the rest of both lists in order. Rows
// without a "browser" entry — including agents whose tools list excluded the
// browser by omission and unmanaged lists — are left untouched. A second run
// finds no "browser" entries and writes nothing: idempotent. It runs once
// after the migrations at open; any failure fails the open — the same
// startup gate as a migration failure.

// browserAllowlistOldKey is the retired catalog tool's allowlist entry.
const browserAllowlistOldKey = "browser"

// browserAllowlistNewKey is the system MCP server's allowlist entry it
// migrates to (mcp.SystemServerID — kept as a literal so the store layer
// stays free of the agents package).
const browserAllowlistNewKey = "browser-use"

// migrateBrowserAllowlists rewrites every agent row whose tools allowlist
// still names the retired browser tool, moving the entry to the mcp_servers
// allowlist under the system server's key.
func migrateBrowserAllowlists(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT id, tools, mcp_servers FROM agents`)
	if err != nil {
		return fmt.Errorf("migrate browser allowlists: query: %w", err)
	}
	type pending struct{ id, tools, mcps string }
	var updates []pending
	for rows.Next() {
		var id, toolsRaw, mcpsRaw string
		if err := rows.Scan(&id, &toolsRaw, &mcpsRaw); err != nil {
			_ = rows.Close()
			return fmt.Errorf("migrate browser allowlists: scan: %w", err)
		}
		var tools, mcps []string
		if err := jsonUnmarshal(toolsRaw, &tools); err != nil {
			_ = rows.Close()
			return fmt.Errorf("migrate browser allowlists: agent %s tools: %w", id, err)
		}
		if err := jsonUnmarshal(mcpsRaw, &mcps); err != nil {
			_ = rows.Close()
			return fmt.Errorf("migrate browser allowlists: agent %s mcp_servers: %w", id, err)
		}
		if !slices.Contains(tools, browserAllowlistOldKey) {
			continue // no retired entry: nothing to move (already-migrated and unrelated rows alike)
		}
		tools = slices.DeleteFunc(tools, func(name string) bool { return name == browserAllowlistOldKey })
		if !slices.Contains(mcps, browserAllowlistNewKey) {
			mcps = append(mcps, browserAllowlistNewKey)
		}
		toolsEnc, err := jsonMarshal(tools)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("migrate browser allowlists: agent %s: %w", id, err)
		}
		mcpsEnc, err := jsonMarshal(mcps)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("migrate browser allowlists: agent %s: %w", id, err)
		}
		updates = append(updates, pending{id: id, tools: toolsEnc, mcps: mcpsEnc})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("migrate browser allowlists: iterate: %w", err)
	}
	rows.Close()
	if len(updates) == 0 {
		return nil // fresh or fully migrated database: nothing to write
	}

	for _, u := range updates {
		if _, err := db.ExecContext(ctx, `
UPDATE agents SET tools = ?, mcp_servers = ?, updated_at = ?
WHERE id = ?`,
			u.tools, u.mcps, formatTime(nowUTC()), u.id,
		); err != nil {
			return fmt.Errorf("migrate browser allowlists: update agent %s: %w", u.id, err)
		}
	}
	return nil
}
