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
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
)

// TestIntegration_MemberChannelsBackfill covers migration 000074
// (fix-role-permission-audit tasks 2.2, design D5): pre-migration role rows —
// built-in Member rows carrying the old 7-permission read-only set, built-in
// Owner rows still holding roles.write, and custom rows holding the retired
// permission — converge to the decided sets (Member gains channels.read +
// channels.write exactly once; roles.write is stripped from every row), the
// migration is idempotent, and the down migration reverses both operations.
func TestIntegration_MemberChannelsBackfill(t *testing.T) {
	ctx := context.Background()
	baseDSN := getTestBaseDSN(t)

	conn, err := pgx.Connect(ctx, baseDSN)
	if err != nil {
		t.Skipf("skipping integration test: database connection failed: %v", err)
	}
	defer conn.Close(ctx)

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	schemaName := fmt.Sprintf("test_perm_%s", hex.EncodeToString(b))

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
	schemaDSN := fmt.Sprintf("%s%ssearch_path=%s,public", baseDSN, separator, schemaName)
	schemaConn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer schemaConn.Close(ctx)

	mig := postgres.NewMigrator(schemaDSN)

	// 1. Migrate to the version just before the catalog surgery so the
	// pre-migration role shapes can be seeded with raw inserts — exactly the
	// rows workspaces created before the change carry.
	if err := mig.MigrateToVersion(73); err != nil {
		t.Fatalf("failed to migrate to version 73: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != 73 || dirty {
		t.Fatalf("expected clean version 73 before fixtures, got v=%d dirty=%v err=%v", v, dirty, err)
	}

	var workspaceID string
	if err := schemaConn.QueryRow(ctx,
		`INSERT INTO workspaces (slug, name) VALUES ('perm-ws', 'Perm WS') RETURNING id`,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("failed to insert workspace: %v", err)
	}

	// Pre-migration shapes: the old 7-permission Member set, an Owner row
	// still holding roles.write (old model), a custom role holding it, and a
	// Member row that already carries channels.write (a partially-migrated or
	// hand-edited row — the backfill must not duplicate it).
	oldMemberPerms := []string{"workspace.read", "members.read", "roles.read", "providers.read", "agents.read", "skills.read", "scheduler.read"}
	oldOwnerPerms := append(append([]string{}, oldMemberPerms...), "members.write", "workspace.write", "roles.write")
	seedRole := func(name string, isOwner, builtIn bool, perms []string) string {
		t.Helper()
		var id string
		if err := schemaConn.QueryRow(ctx,
			`INSERT INTO roles (workspace_id, name, is_owner, permissions, built_in)
			 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			workspaceID, name, isOwner, perms, builtIn,
		).Scan(&id); err != nil {
			t.Fatalf("failed to seed role %s: %v", name, err)
		}
		return id
	}
	memberRoleID := seedRole("Member", false, true, oldMemberPerms)
	ownerRoleID := seedRole("Owner", true, true, oldOwnerPerms)
	customRoleID := seedRole("Editors", false, false, []string{"agents.read", "roles.write"})

	// A second workspace whose built-in Member row already carries
	// channels.write (a partially-migrated row) — the backfill must add only
	// channels.read, never duplicate it, and every per-workspace Member row
	// must converge.
	var workspace2ID string
	if err := schemaConn.QueryRow(ctx,
		`INSERT INTO workspaces (slug, name) VALUES ('perm-ws-2', 'Perm WS 2') RETURNING id`,
	).Scan(&workspace2ID); err != nil {
		t.Fatalf("failed to insert second workspace: %v", err)
	}
	var partialMemberRoleID string
	if err := schemaConn.QueryRow(ctx,
		`INSERT INTO roles (workspace_id, name, is_owner, permissions, built_in)
		 VALUES ($1, 'Member', false, $2, true) RETURNING id`,
		workspace2ID, []string{"workspace.read", "channels.write", "roles.write"},
	).Scan(&partialMemberRoleID); err != nil {
		t.Fatalf("failed to seed partial Member role: %v", err)
	}

	loadPerms := func(roleID string) []string {
		t.Helper()
		var perms []string
		if err := schemaConn.QueryRow(ctx,
			`SELECT permissions FROM roles WHERE id = $1`, roleID,
		).Scan(&perms); err != nil {
			t.Fatalf("failed to load role %s: %v", roleID, err)
		}
		return perms
	}

	countPerm := func(perms []string, want string) int {
		t.Helper()
		n := 0
		for _, p := range perms {
			if p == want {
				n++
			}
		}
		return n
	}

	// 2. Apply 000074 (and anything stacked above it).
	if err := mig.Up(); err != nil {
		t.Fatalf("failed to migrate up: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v < 74 || dirty {
		t.Fatalf("expected clean version >= 74 after up, got v=%d dirty=%v err=%v", v, dirty, err)
	}

	// 3. The old Member row converged to the decided set: the original seven
	// reads intact, both channel permissions present exactly once, and no
	// roles.write anywhere.
	memberPerms := loadPerms(memberRoleID)
	for _, p := range oldMemberPerms {
		if countPerm(memberPerms, p) != 1 {
			t.Errorf("Member row lost pre-migration permission %q: %v", p, memberPerms)
		}
	}
	for _, p := range []string{"channels.read", "channels.write"} {
		if got := countPerm(memberPerms, p); got != 1 {
			t.Errorf("Member row holds %q %d times, want exactly 1: %v", p, got, memberPerms)
		}
	}
	if countPerm(memberPerms, "roles.write") != 0 {
		t.Errorf("Member row still holds retired roles.write: %v", memberPerms)
	}

	// The partially-migrated row keeps its existing channels.write (no
	// duplicate) and gains only channels.read.
	partialPerms := loadPerms(partialMemberRoleID)
	if countPerm(partialPerms, "channels.write") != 1 || countPerm(partialPerms, "channels.read") != 1 {
		t.Errorf("partially-migrated Member row not converged idempotently: %v", partialPerms)
	}

	// roles.write is stripped from ALL rows — built-in Owner and custom alike
	// — while everything else stays untouched.
	ownerPerms := loadPerms(ownerRoleID)
	if countPerm(ownerPerms, "roles.write") != 0 {
		t.Errorf("Owner row still holds retired roles.write: %v", ownerPerms)
	}
	if len(ownerPerms) != len(oldOwnerPerms)-1 {
		t.Errorf("Owner row changed beyond the roles.write strip: %v", ownerPerms)
	}
	customPerms := loadPerms(customRoleID)
	if countPerm(customPerms, "roles.write") != 0 {
		t.Errorf("custom role row still holds retired roles.write: %v", customPerms)
	}
	if countPerm(customPerms, "channels.read") != 0 || countPerm(customPerms, "channels.write") != 0 {
		t.Errorf("custom role row must not gain the member channel backfill: %v", customPerms)
	}

	// 4. Re-running the migration is a no-op: capture the converged arrays,
	// re-apply, and compare.
	before := map[string][]string{
		memberRoleID:        loadPerms(memberRoleID),
		ownerRoleID:         loadPerms(ownerRoleID),
		customRoleID:        loadPerms(customRoleID),
		partialMemberRoleID: loadPerms(partialMemberRoleID),
	}
	if err := mig.Up(); err != nil {
		t.Fatalf("expected re-running Up to be a no-op, got %v", err)
	}
	for roleID, want := range before {
		got := loadPerms(roleID)
		if len(got) != len(want) {
			t.Fatalf("re-migration changed permissions for role %s: %v, want %v", roleID, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("re-migration changed permissions for role %s: %v, want %v", roleID, got, want)
			}
		}
	}

	// 5. The down migration reverses both operations: built-in Member rows
	// lose the channel permissions; the built-ins that carried roles.write in
	// the old model (Owner — and Superadmin, not present in this fixture)
	// get it back; the custom row stays stripped (the all-rows strip is not
	// invertible per row — documented on the down migration).
	if err := mig.Down(1); err != nil {
		t.Fatalf("failed to migrate down one step: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != 73 || dirty {
		t.Fatalf("expected clean version 73 after down, got v=%d dirty=%v err=%v", v, dirty, err)
	}
	downMember := loadPerms(memberRoleID)
	for _, p := range []string{"channels.read", "channels.write"} {
		if countPerm(downMember, p) != 0 {
			t.Errorf("down migration left %q on the Member row: %v", p, downMember)
		}
	}
	downOwner := loadPerms(ownerRoleID)
	if countPerm(downOwner, "roles.write") != 1 {
		t.Errorf("down migration did not restore roles.write on the Owner row: %v", downOwner)
	}
	downPartial := loadPerms(partialMemberRoleID)
	if countPerm(downPartial, "channels.read") != 0 || countPerm(downPartial, "channels.write") != 0 {
		t.Errorf("down migration left channel permissions on the partial Member row: %v", downPartial)
	}

	// 6. Re-applying converges again (the down/up pair is stable).
	if err := mig.Up(); err != nil {
		t.Fatalf("failed to re-apply migration after down: %v", err)
	}
	reMember := loadPerms(memberRoleID)
	if countPerm(reMember, "channels.read") != 1 || countPerm(reMember, "channels.write") != 1 || countPerm(reMember, "roles.write") != 0 {
		t.Errorf("re-applied migration did not reconverge the Member row: %v", reMember)
	}
}
