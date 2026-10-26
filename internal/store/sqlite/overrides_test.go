package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestOpen_MigratesV7DatabaseForward covers the 000008 rollout on an
// existing database: a schema stopped exactly at v7 — with a session and an
// agent row seeded the way the old build left them — migrates forward-only
// on open, and both rows read back with empty overrides (inherit, the
// columns' DEFAULT ”). A second open is a no-op.
func TestOpen_MigratesV7DatabaseForward(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v7.db")

	// Stop the schema exactly at v7 and seed pre-v8 rows.
	st, err := openWith(ctx, path, mustEmbeddedMigrations(t)[:7])
	require.NoError(t, err, "seeding the v7-schema fixture must succeed")
	require.NoError(t, st.Close())

	db := openRaw(t, path)
	stamped := formatTime(time.Date(2025, 10, 1, 8, 0, 0, 0, time.UTC))
	_, err = db.ExecContext(ctx, `
INSERT INTO agents (id, name, created_at, updated_at)
VALUES ('legacy-agent', 'Legacy', ?, ?)`, stamped, stamped)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
INSERT INTO sessions (id, agent_id, title, created_at, last_activity_at)
VALUES ('legacy-session', 'legacy-agent', '', ?, ?)`, stamped, stamped)
	require.NoError(t, err)

	// The full embedded list migrates the database forward.
	st, err = Open(ctx, path)
	require.NoError(t, err, "a v7 database must migrate forward on open")
	t.Cleanup(func() { _ = st.Close() })

	has, err := columnExists(ctx, db, "sessions", "model")
	require.NoError(t, err)
	require.True(t, has, "000008 must add sessions.model")
	has, err = columnExists(ctx, db, "sessions", "effort")
	require.NoError(t, err)
	require.True(t, has, "000008 must add sessions.effort")
	has, err = columnExists(ctx, db, "agents", "effort")
	require.NoError(t, err)
	require.True(t, has, "000008 must add agents.effort")

	var version int
	require.NoError(t, db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version))
	require.Equal(t, 15, version)

	// The pre-v8 rows emerge with empty overrides: the columns' defaults.
	sess, err := st.Sessions().ByID(ctx, "legacy-session")
	require.NoError(t, err)
	require.Empty(t, sess.Model, "the DEFAULT '' must leave sessions inheriting")
	require.Empty(t, sess.Effort, "the DEFAULT '' must leave sessions inheriting")
	legacy, err := st.Agents().ByID(ctx, "legacy-agent")
	require.NoError(t, err)
	require.Empty(t, legacy.Effort, "the DEFAULT '' must leave agents without an effort default")

	// Second open: idempotent, no re-application.
	st2, err := Open(ctx, path)
	require.NoError(t, err)
	require.NoError(t, st2.Close())
	require.NoError(t, db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version))
	require.Equal(t, 15, version, "re-opening must not re-apply migrations")
}

// TestSessions_OverridesRoundTrip covers the per-session model/effort
// overrides (design D1): seeded values survive create/get, SetOverrides
// rewrites both in one call, an empty value clears back to inherit, and the
// absent session is the store sentinel.
func TestSessions_OverridesRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	session := &store.Session{
		AgentID: "agent-1",
		Model:   "claude-opus-4-1",
		Effort:  "high",
	}
	require.NoError(t, s.Sessions().Create(ctx, session))
	got, err := s.Sessions().ByID(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-1", got.Model)
	require.Equal(t, "high", got.Effort)

	require.NoError(t, s.Sessions().SetOverrides(ctx, session.ID, "gpt-5", "low"))
	got, err = s.Sessions().ByID(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, "gpt-5", got.Model)
	require.Equal(t, "low", got.Effort)

	// Empty clears back to inherit.
	require.NoError(t, s.Sessions().SetOverrides(ctx, session.ID, "", ""))
	got, err = s.Sessions().ByID(ctx, session.ID)
	require.NoError(t, err)
	require.Empty(t, got.Model)
	require.Empty(t, got.Effort)

	require.ErrorIs(t, s.Sessions().SetOverrides(ctx, "missing", "m", "high"), store.ErrNotFound)
}

// TestAgents_EffortRoundTrip covers the agent's default reasoning effort:
// the field round-trips create/get/update and clears through an empty value.
func TestAgents_EffortRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	agent := &store.Agent{Name: "atlas", Slug: "atlas", Effort: "medium"}
	require.NoError(t, s.Agents().Create(ctx, agent))
	got, err := s.Agents().ByID(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, "medium", got.Effort)

	agent.Effort = "high"
	require.NoError(t, s.Agents().Update(ctx, agent))
	got, err = s.Agents().ByID(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, "high", got.Effort)

	agent.Effort = ""
	require.NoError(t, s.Agents().Update(ctx, agent))
	got, err = s.Agents().ByID(ctx, agent.ID)
	require.NoError(t, err)
	require.Empty(t, got.Effort, "an empty effort is stored verbatim (provider default)")
}
