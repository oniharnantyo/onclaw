package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
	"github.com/stretchr/testify/require"
)

// legacySeed is one fixture agent of the pre-change database: the old
// schema's system_prompt column no longer exists on store.Agent.
type legacySeed struct {
	id           string
	name         string
	systemPrompt string
}

// seedLegacyStore builds a pre-change database fixture: only migration 000001
// is applied (the v1 schema, system_prompt included, no prompt-document
// columns), then rows are inserted the way the old build left them. The
// fixture store is closed; the path is ready for a full Open.
func seedLegacyStore(t *testing.T, path string, seeds []legacySeed) {
	t.Helper()
	ctx := context.Background()

	st, err := openWith(ctx, path, mustEmbeddedMigrations(t)[:1])
	require.NoError(t, err, "seeding the v1-schema fixture must succeed")
	require.NoError(t, st.Close())

	db := openRaw(t, path)
	for i, s := range seeds {
		stamped := formatTime(time.Date(2025, 1, 1, 0, i, 0, 0, time.UTC))
		_, err := db.ExecContext(ctx, `
INSERT INTO agents (id, name, system_prompt, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)`,
			s.id, s.name, s.systemPrompt, stamped, stamped,
		)
		require.NoError(t, err, "seed agent %s", s.name)
	}
}

// TestOpen_BackfillsLegacySystemPrompts covers task 1.3 end to end: a
// pre-change database (agents with system_prompt, no slug) migrates on open
// so that every legacy agent emerges with a deduped slug, its old prompt
// seeded into the workspace IDENTITY.md, prompts_status ready — and the
// system_prompt column gone. Agents without a legacy prompt are left alone.
// A second open is a no-op.
func TestOpen_BackfillsLegacySystemPrompts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "buddies.db")

	seedLegacyStore(t, path, []legacySeed{
		{id: "a-1", name: "Scout", systemPrompt: "You are a scout."},
		{id: "a-2", name: "Scout", systemPrompt: "You are the second scout."},
		{id: "a-3", name: "Atlas 9000!", systemPrompt: "Carry the world."},
		{id: "a-4", name: "Promptless", systemPrompt: ""},
	})

	st, err := Open(ctx, path)
	require.NoError(t, err, "the migrated database must open cleanly")
	t.Cleanup(func() { _ = st.Close() })

	root := filepath.Join(filepath.Dir(path), "workspaces")
	ws := workspaces.NewManager(root)

	agents, err := st.Agents().List(ctx)
	require.NoError(t, err)
	require.Len(t, agents, 4)

	byID := make(map[string]store.Agent, len(agents))
	for _, a := range agents {
		byID[a.ID] = a
	}

	// Both "Scout" rows are carried over with deduped slugs.
	scout := byID["a-1"]
	require.Equal(t, "scout", scout.Slug)
	require.Equal(t, store.PromptsStatusReady, scout.PromptsStatus)
	scout2 := byID["a-2"]
	require.Equal(t, "scout-2", scout2.Slug)
	require.Equal(t, store.PromptsStatusReady, scout2.PromptsStatus)

	// Slugification folds punctuation out of the name.
	atlas := byID["a-3"]
	require.Equal(t, "atlas-9000", atlas.Slug)
	require.Equal(t, store.PromptsStatusReady, atlas.PromptsStatus)

	// The promptless agent is untouched: no slug and no workspace
	// directory (its status rides the migration's 'ready' default).
	promptless := byID["a-4"]
	require.Empty(t, promptless.Slug)
	require.Equal(t, store.PromptsStatusReady, promptless.PromptsStatus)
	promptlessDir, err := ws.Dir("promptless")
	require.NoError(t, err)
	_, err = os.Stat(promptlessDir)
	require.True(t, os.IsNotExist(err), "a skipped agent must not get a workspace")

	// IDENTITY.md holds the legacy prompt under the seed heading, in each
	// carried-over agent's own workspace.
	for id, want := range map[string]string{
		"a-1": "You are a scout.",
		"a-2": "You are the second scout.",
		"a-3": "Carry the world.",
	} {
		dir, err := ws.Dir(byID[id].Slug)
		require.NoError(t, err)
		content, exists, err := ws.ReadDocument(dir, workspaces.DocIdentity)
		require.NoError(t, err)
		require.True(t, exists, "agent %s must have an IDENTITY.md", id)
		require.Equal(t, seedHeading+"\n\n"+want+"\n", content, "agent %s IDENTITY.md", id)
	}

	// The legacy column is gone: the schema is past 000005.
	db := openRaw(t, path)
	legacy, err := columnExists(ctx, db, "agents", "system_prompt")
	require.NoError(t, err)
	require.False(t, legacy, "system_prompt must be dropped after the backfill")
	var version int
	require.NoError(t, db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version))
	require.Equal(t, 18, version)

	// Second open is a no-op. IDENTITY.md is made read-only first: a
	// rewrite attempt would fail the open, so success proves the backfill
	// did not touch anything.
	for _, slug := range []string{"scout", "scout-2", "atlas-9000"} {
		dir, err := ws.Dir(slug)
		require.NoError(t, err)
		require.NoError(t, os.Chmod(filepath.Join(dir, workspaces.DocIdentity), 0444))
	}
	st2, err := Open(ctx, path)
	require.NoError(t, err)
	again, err := st2.Agents().List(ctx)
	require.NoError(t, err)
	require.NoError(t, st2.Close())
	require.Len(t, again, 4)
}

// TestOpen_BackfillRecoversCrashAtBoundary covers the crash window inside
// the two-phase migration (design D10): a database stranded at schema v4 —
// 000004 applied, backfill never ran, 000005 pending — still gets its
// legacy prompts carried over on the next open, before the column drops.
func TestOpen_BackfillRecoversCrashAtBoundary(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "buddies.db")

	// Stop the schema exactly at the boundary version.
	st, err := openWith(ctx, path, mustEmbeddedMigrations(t)[:4])
	require.NoError(t, err)
	require.NoError(t, st.Close())

	db := openRaw(t, path)
	stamped := formatTime(time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC))
	_, err = db.ExecContext(ctx, `
INSERT INTO agents (id, name, system_prompt, created_at, updated_at)
VALUES ('a-1', 'Scout', 'You are a scout.', ?, ?)`, stamped, stamped)
	require.NoError(t, err)

	st, err = Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	agent, err := st.Agents().ByID(ctx, "a-1")
	require.NoError(t, err)
	require.Equal(t, "scout", agent.Slug)
	require.Equal(t, store.PromptsStatusReady, agent.PromptsStatus)

	ws := workspaces.NewManager(filepath.Join(filepath.Dir(path), "workspaces"))
	dir, err := ws.Dir("scout")
	require.NoError(t, err)
	content, exists, err := ws.ReadDocument(dir, workspaces.DocIdentity)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, seedHeading+"\n\nYou are a scout.\n", content)

	legacy, err := columnExists(ctx, db, "agents", "system_prompt")
	require.NoError(t, err)
	require.False(t, legacy, "000005 must apply only after the recovered backfill")
}

// TestOpen_BackfillFailureBlocksStartup pins the startup gate: when the
// backfill cannot create the workspace directory the open fails instead of
// silently dropping the legacy prompts. A regular file parked at the
// workspaces root breaks directory creation (ENOTDIR) while leaving the
// database fully accessible.
func TestOpen_BackfillFailureBlocksStartup(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "buddies.db")

	seedLegacyStore(t, path, []legacySeed{
		{id: "a-1", name: "Scout", systemPrompt: "You are a scout."},
	})

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "workspaces"), []byte("not a directory"), 0600))

	_, err := Open(ctx, path)
	require.Error(t, err, "a failing backfill must block startup")
	require.Contains(t, err.Error(), "backfill legacy system prompts")
}

// TestUniqueSlug_DedupeAndCap covers the backfill's slug dedupe: suffixes
// increment until free, and appending never produces an over-long or
// invalid slug even for a full-length base.
func TestUniqueSlug_DedupeAndCap(t *testing.T) {
	taken := map[string]bool{}

	slug, err := uniqueSlug("Scout Bot", taken)
	require.NoError(t, err)
	require.Equal(t, "scout-bot", slug)
	taken[slug] = true

	slug, err = uniqueSlug("Scout", taken)
	require.NoError(t, err)
	require.Equal(t, "scout", slug)
	taken[slug] = true

	slug, err = uniqueSlug("Scout", taken)
	require.NoError(t, err)
	require.Equal(t, "scout-2", slug)
	taken[slug] = true

	slug, err = uniqueSlug("Scout", taken)
	require.NoError(t, err)
	require.Equal(t, "scout-3", slug)

	// A base at the cap truncates so the suffix fits, staying valid.
	long := strings.Repeat("a", maxSlugLen)
	taken[long] = true
	slug, err = uniqueSlug(long, taken)
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("a", maxSlugLen-2)+"-2", slug)
	require.True(t, workspaces.ValidSlug(slug))

	// A name with no alphanumerics yields no slug: an error, not a guess.
	_, err = uniqueSlug("---", taken)
	require.Error(t, err)
}

// TestColumnExists guards the backfill's guard.
func TestColumnExists(t *testing.T) {
	ctx := context.Background()
	db := openRaw(t, filepath.Join(t.TempDir(), "t.db"))

	_, err := db.Exec(`CREATE TABLE things (id TEXT, name TEXT)`)
	require.NoError(t, err)
	has, err := columnExists(ctx, db, "things", "name")
	require.NoError(t, err)
	require.True(t, has)
	has, err = columnExists(ctx, db, "things", "missing")
	require.NoError(t, err)
	require.False(t, has)
}
