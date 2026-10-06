#!/usr/bin/env bash
# purge-memory.sh — wipe a workspace's learned memory before a bench cycle.
#
# WHY THIS EXISTS: the harness has THREE persistent memory channels, and every
# one of them survives across cycles and is injected into agent context:
#   1. the note pipeline (memory_notes/events/embeddings/entities/reports),
#   2. the workspace memory column (workspaces.memory — the WORKSPACE.md
#      virtual doc, composed into every turn, "reloaded every execution"),
#   3. user memories (user_memories — the USER.md virtual doc; the fixture
#      actors accumulated 28KB of task residue).
# Task residue in any channel makes cycles incomparable — the suite-v2 cycle
# measured identical prompts flipping 3/3-pass to 0/3-fail against the purged
# postfix cycle from accumulated memory alone, and the first v3 attempt had to
# be killed 15 runs in when vault-echo went 0/3 on residue the table purge
# missed (the workspace/user memory columns). Purge before EVERY cycle; never
# chat manually with the bench agent while a cycle runs, for the same reason.
#
# Also clears task-artifact files from the agent's jailed workspace (every
# prior cycle's orders.md / vault.md / winner.md etc.), keeping only the
# provisioned IDENTITY.md and SOUL.md — within a cycle, tasks rewrite the
# files they need.
#
# Usage: purge-memory.sh [workspace-slug] [database-url]
#   workspace-slug  default: bench-eval
#   database-url    default: $DATABASE_URL, then postgres://postgres:password@localhost:5432/onclaw
#
# The bench runner stays black-box HTTP; this is instance-level ops, run
# against the same DB and data dir the server on --base-url uses.

set -euo pipefail

SLUG="${1:-bench-eval}"
DSN="${2:-${DATABASE_URL:-postgres://postgres:password@localhost:5432/onclaw}}"
AGENT_SLUG="${ONCLAW_BENCH_AGENT:-bench-agent}"
DATA_DIR="${ONCLAW_DATA_DIR:-$HOME/.onclaw}"

PSQL="$(command -v psql || true)"
if [ -z "$PSQL" ] && [ -x /Applications/Postgres.app/Contents/Versions/latest/bin/psql ]; then
  PSQL=/Applications/Postgres.app/Contents/Versions/latest/bin/psql
fi
if [ -z "$PSQL" ]; then
  echo "error: psql not on PATH and no Postgres.app installation found" >&2
  exit 1
fi

echo "purging memory for workspace '$SLUG' in ${DSN%%\?*}"
$PSQL "$DSN" -v ON_ERROR_STOP=1 -At <<SQL
BEGIN;
DELETE FROM memory_embeddings WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = '$SLUG');
DELETE FROM memory_events     WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = '$SLUG');
DELETE FROM memory_notes      WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = '$SLUG');
DELETE FROM memory_entities   WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = '$SLUG');
DELETE FROM memory_reports    WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = '$SLUG');
DELETE FROM user_memories     WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = '$SLUG');
UPDATE workspaces SET memory = '', updated_at = now() WHERE slug = '$SLUG';
COMMIT;
SQL

echo "remaining state (must all be 0):"
$PSQL "$DSN" -At <<SQL
SELECT '  notes            ' || count(*) FROM memory_notes      WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = '$SLUG');
SELECT '  events           ' || count(*) FROM memory_events     WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = '$SLUG');
SELECT '  embeddings       ' || count(*) FROM memory_embeddings WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = '$SLUG');
SELECT '  entities         ' || count(*) FROM memory_entities   WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = '$SLUG');
SELECT '  user memories    ' || count(*) FROM user_memories     WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = '$SLUG');
SELECT '  workspace memory ' || coalesce(length(memory), 0) || ' chars' FROM workspaces WHERE slug = '$SLUG';
SQL

AGENT_DIR="$DATA_DIR/workspaces/$SLUG/agents/$AGENT_SLUG"
if [ -d "$AGENT_DIR" ]; then
  echo "cleaning task artifacts under $AGENT_DIR (keeping IDENTITY.md, SOUL.md)"
  find "$AGENT_DIR" -mindepth 1 -maxdepth 1 ! -name IDENTITY.md ! -name SOUL.md -exec rm -rf {} +
else
  echo "note: agent dir $AGENT_DIR not found, skipping file cleanup"
fi
