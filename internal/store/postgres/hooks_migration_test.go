//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
)

// TestIntegration_HooksSchema_TablesExist covers migration 000026 (design.md
// D16): the four hook tables exist and instance_hooks carries the comment
// marking it as the one deliberately workspace-unscoped table.
func TestIntegration_HooksSchema_TablesExist(t *testing.T) {
	_, schemaDSN, ctx := setupTestSchema(t)

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer conn.Close(ctx)

	for _, table := range []string{"instance_hooks", "workspace_hooks", "agent_hooks", "hook_executions"} {
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatalf("failed to probe table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %s to exist after migrations", table)
		}
	}

	var comment *string
	if err := conn.QueryRow(ctx,
		`SELECT obj_description($1::regclass)`, "instance_hooks",
	).Scan(&comment); err != nil {
		t.Fatalf("failed to read instance_hooks comment: %v", err)
	}
	if comment == nil {
		t.Error("expected instance_hooks to carry a comment documenting its workspace-unscoping")
	}
}

// TestIntegration_HooksPermissionBackfill covers migration 000027 (design.md
// D17): the backfill grants hooks.read and hooks.write to built-in
// Superadmin/Owner/Admin roles idempotently, without duplicating entries.
func TestIntegration_HooksPermissionBackfill(t *testing.T) {
	s, schemaDSN, ctx := setupTestSchema(t)

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer conn.Close(ctx)

	// Seed the built-in administrator roles with a pre-000027 permission
	// snapshot (no hooks permissions), as roles existing before the upgrade
	// would look.
	ws := &domain.Workspace{Slug: "hooks-backfill", Name: "Hooks Backfill"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected Create workspace error: %v", err)
	}
	for _, name := range []string{domain.RoleSuperadmin, domain.RoleOwner, domain.RoleAdmin} {
		r := &domain.Role{
			WorkspaceID: ws.ID,
			Name:        name,
			Permissions: []string{domain.WorkspaceRead},
			BuiltIn:     true,
		}
		if err := s.Roles().Create(ctx, r); err != nil {
			t.Fatalf("unexpected Create role %s error: %v", name, err)
		}
	}

	strip := func() {
		t.Helper()
		if _, err := conn.Exec(ctx,
			`UPDATE roles
			 SET permissions = array_remove(array_remove(permissions, 'hooks.read'), 'hooks.write')
			 WHERE built_in = true`,
		); err != nil {
			t.Fatalf("failed to strip hooks permissions: %v", err)
		}
	}

	assertBackfilled := func(stage string) {
		t.Helper()
		rows, err := conn.Query(ctx,
			`SELECT name, permissions FROM roles WHERE built_in = true AND name IN ('Superadmin', 'Owner', 'Admin')`,
		)
		if err != nil {
			t.Fatalf("[%s] failed to query roles: %v", stage, err)
		}
		found := map[string][]string{}
		for rows.Next() {
			var name string
			var perms []string
			if err := rows.Scan(&name, &perms); err != nil {
				t.Fatalf("[%s] failed to scan role: %v", stage, err)
			}
			found[name] = perms
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("[%s] rows error: %v", stage, err)
		}

		for _, name := range []string{domain.RoleSuperadmin, domain.RoleOwner, domain.RoleAdmin} {
			perms, ok := found[name]
			if !ok {
				t.Fatalf("[%s] built-in role %s not found", stage, name)
			}
			readCount, writeCount := 0, 0
			for _, p := range perms {
				switch p {
				case domain.HooksRead:
					readCount++
				case domain.HooksWrite:
					writeCount++
				}
			}
			if readCount != 1 || writeCount != 1 {
				t.Errorf("[%s] %s: expected exactly one hooks.read and one hooks.write, got read=%d write=%d perms=%v",
					stage, name, readCount, writeCount, perms)
			}
		}
	}

	// The backfill already ran during setup on an empty roles table; re-run
	// the real migration (down to the pre-backfill world at version 26, then
	// up) against the seeded rows. MigrateToVersion targets the version
	// directly, so appended migrations AND the intentional 000032–000039
	// numbering gap above 000027 do not desync the step arithmetic.
	mig := postgres.NewMigrator(schemaDSN)

	rerun := func(stage string) {
		t.Helper()
		before, dirty, err := mig.Status()
		if err != nil || dirty {
			t.Fatalf("[%s] failed to read version before down: v=%d dirty=%v err=%v", stage, before, dirty, err)
		}
		if before < 26 {
			t.Fatalf("[%s] expected version >= 26 before down, got %d", stage, before)
		}
		if err := mig.MigrateToVersion(26); err != nil {
			t.Fatalf("[%s] failed to migrate down to 000026: %v", stage, err)
		}
		if v, dirty, err := mig.Status(); err != nil || v != 26 || dirty {
			t.Fatalf("[%s] expected clean version 26 after down, got v=%d dirty=%v err=%v", stage, v, dirty, err)
		}
		if err := mig.Up(); err != nil {
			t.Fatalf("[%s] failed to re-run migrations above 000026: %v", stage, err)
		}
		if v, dirty, err := mig.Status(); err != nil || v != before || dirty {
			t.Fatalf("[%s] expected clean version %d after up, got v=%d dirty=%v err=%v", stage, before, v, dirty, err)
		}
		assertBackfilled(stage)
	}

	// Idempotent: strip and re-run once more; no duplicates may appear.
	strip()
	rerun("first run")

	strip()
	rerun("second run")
}

// TestIntegration_HookExecutions_SurviveHookDeletion covers the audit
// durability requirement (design.md D15/D16): deleting a hook row keeps its
// hook_executions records with hook_id set to NULL and the denormalized
// hook_name intact.
func TestIntegration_HookExecutions_SurviveHookDeletion(t *testing.T) {
	_, schemaDSN, ctx := setupTestSchema(t)

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer conn.Close(ctx)

	var workspaceID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO workspaces (slug, name) VALUES ('hooks-audit-ws', 'Hooks Audit WS') RETURNING id`,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("failed to insert workspace: %v", err)
	}

	var hookID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO workspace_hooks (workspace_id, name, event, matcher, handler_type, config, timeout_ms)
		 VALUES ($1, 'policy-gate', 'pre_tool_use', '', 'http', '{}'::jsonb, 5000)
		 RETURNING id`, workspaceID,
	).Scan(&hookID); err != nil {
		t.Fatalf("failed to insert workspace hook: %v", err)
	}

	var executionID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO hook_executions (hook_id, hook_name, hook_level, workspace_id, event, decision, duration_ms, detail, origin)
		 VALUES ($1, 'policy-gate', 'workspace', $2, 'pre_tool_use', 'block', 42, 'denied by policy', 'user')
		 RETURNING id`, hookID, workspaceID,
	).Scan(&executionID); err != nil {
		t.Fatalf("failed to insert hook execution: %v", err)
	}

	if _, err := conn.Exec(ctx, `DELETE FROM workspace_hooks WHERE id = $1`, hookID); err != nil {
		t.Fatalf("failed to delete hook: %v", err)
	}

	var afterHookID *string
	var hookName, hookLevel string
	if err := conn.QueryRow(ctx,
		`SELECT hook_id, hook_name, hook_level FROM hook_executions WHERE id = $1`, executionID,
	).Scan(&afterHookID, &hookName, &hookLevel); err != nil {
		t.Fatalf("failed to re-query hook execution: %v", err)
	}

	if afterHookID != nil {
		t.Errorf("expected hook_id to be NULL after hook deletion, got %s", *afterHookID)
	}
	if hookName != "policy-gate" {
		t.Errorf("expected denormalized hook_name to survive deletion, got %q", hookName)
	}
	if hookLevel != "workspace" {
		t.Errorf("expected hook_level to survive deletion, got %q", hookLevel)
	}
}

// TestIntegration_HooksSchema_MatcherTextConversion covers migration 000029
// (design.md D19): the matcher JSONB tagged union (000026) collapses to one
// plain text string — only-lists join with '|', regex keeps its pattern, and
// except/all/null degrade to the empty match-all string — while 000028's
// if_rule column defaults to the empty string.
func TestIntegration_HooksSchema_MatcherTextConversion(t *testing.T) {
	ctx := context.Background()
	baseDSN := getTestBaseDSN(t)

	conn, err := pgx.Connect(ctx, baseDSN)
	if err != nil {
		t.Skipf("skipping integration test: database connection failed: %v", err)
	}
	defer conn.Close(ctx)

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	schemaName := fmt.Sprintf("test_matcherconv_%s", hex.EncodeToString(b))

	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupConn, err := pgx.Connect(context.Background(), baseDSN)
		if err == nil {
			_, _ = cleanupConn.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
			_ = cleanupConn.Close(context.Background())
		}
	})

	separator := "?"
	if strings.Contains(baseDSN, "?") {
		separator = "&"
	}
	schemaDSN := fmt.Sprintf("%s%ssearch_path=%s", baseDSN, separator, schemaName)

	schemaConn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer schemaConn.Close(ctx)

	mig := postgres.NewMigrator(schemaDSN)

	// Stage pre-000029 rows at version 28: if_rule already exists (000028),
	// matcher is still the JSONB tagged union.
	if err := mig.MigrateToVersion(28); err != nil {
		t.Fatalf("failed to migrate to version 28: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != 28 || dirty {
		t.Fatalf("expected clean version 28 before fixtures, got v=%d dirty=%v err=%v", v, dirty, err)
	}

	if _, err := schemaConn.Exec(ctx,
		`INSERT INTO workspaces (slug, name) VALUES ('matcher-conv-ws', 'Matcher Conv WS')`,
	); err != nil {
		t.Fatalf("failed to insert workspace: %v", err)
	}

	fixtures := []struct {
		name string
		json string
		want string
	}{
		{"conv-only", `{"type":"tools","mode":"only","tools":["web.fetch","cron"]}`, "web.fetch|cron"},
		{"conv-except", `{"type":"tools","mode":"except","tools":["email.send"]}`, ""},
		{"conv-regex", `{"type":"regex","pattern":"^web\\."}`, `^web\.`},
		{"conv-all", `{"type":"tools","mode":"all"}`, ""},
		{"conv-null", `null`, ""},
	}
	for _, f := range fixtures {
		if _, err := schemaConn.Exec(ctx,
			`INSERT INTO workspace_hooks (workspace_id, name, event, matcher, handler_type, config, timeout_ms)
			 SELECT id, $2, 'pre_tool_use', $3::jsonb, 'http', '{}'::jsonb, 5000 FROM workspaces WHERE slug = $1`,
			"matcher-conv-ws", f.name, f.json,
		); err != nil {
			t.Fatalf("failed to insert workspace hook %s: %v", f.name, err)
		}
	}

	// instance_hooks carries no FK, so it converts without extra fixtures.
	if _, err := schemaConn.Exec(ctx,
		`INSERT INTO instance_hooks (key, source, version, name, event, matcher, handler_type, timeout_ms)
		 VALUES ('conv-only', 'managed', 1, 'conv only', 'pre_tool_use',
		         '{"type":"tools","mode":"only","tools":["a","b"]}'::jsonb, 'http', 5000)`,
	); err != nil {
		t.Fatalf("failed to insert instance hook: %v", err)
	}

	// Apply 000029 (and anything stacked above it).
	if err := mig.Up(); err != nil {
		t.Fatalf("failed to migrate up: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v < 29 || dirty {
		t.Fatalf("expected clean version >= 29 after up, got v=%d dirty=%v err=%v", v, dirty, err)
	}

	for _, f := range fixtures {
		var matcher, ifRule string
		if err := schemaConn.QueryRow(ctx,
			`SELECT matcher, if_rule FROM workspace_hooks WHERE name = $1`, f.name,
		).Scan(&matcher, &ifRule); err != nil {
			t.Fatalf("failed to read converted matcher for %s: %v", f.name, err)
		}
		if matcher != f.want {
			t.Errorf("matcher conversion for %s: got %q want %q", f.name, matcher, f.want)
		}
		if ifRule != "" {
			t.Errorf("if_rule for %s: got %q want '' (column default)", f.name, ifRule)
		}
	}

	var instanceMatcher string
	if err := schemaConn.QueryRow(ctx,
		`SELECT matcher FROM instance_hooks WHERE key = 'conv-only'`,
	).Scan(&instanceMatcher); err != nil {
		t.Fatalf("failed to read converted instance matcher: %v", err)
	}
	if instanceMatcher != "a|b" {
		t.Errorf("instance matcher conversion: got %q want %q", instanceMatcher, "a|b")
	}
}
