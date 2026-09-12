-- Schedulers (integrate-scheduler design D12): a named standing order firing
-- one workspace agent on a recurrence (standard 5-field cron interpreted in
-- the workspace timezone) or at a one-shot instant, with a task prompt and a
-- delivery target. Validation lives in the domain layer; the schema only
-- pins the shape: kind/expr/run_at exclusivity, per-workspace+agent name
-- uniqueness, and the partial index the due-claim scan rides on.

CREATE TABLE schedulers (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id     uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    created_by   uuid REFERENCES users(id) ON DELETE SET NULL,
    name         text NOT NULL,
    prompt       text NOT NULL,
    kind         text NOT NULL CHECK (kind IN ('recurring', 'once')),
    expr         text,
    run_at       timestamptz,
    delivery     jsonb NOT NULL,
    enabled      boolean NOT NULL DEFAULT true,
    next_run_at  timestamptz,
    last_run     jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_schedulers_workspace_id_agent_id_name UNIQUE (workspace_id, agent_id, name),
    CONSTRAINT chk_schedulers_kind_shape CHECK (
        (kind = 'recurring' AND expr IS NOT NULL AND run_at IS NULL) OR
        (kind = 'once' AND run_at IS NOT NULL AND expr IS NULL)
    )
);

-- The claim query's access path: enabled schedulers with a pending
-- occurrence. Uniqueness is not indexable beyond the table constraint —
-- the domain validator owns expression validity, never a DB constraint.
CREATE INDEX idx_schedulers_due
    ON schedulers(next_run_at)
    WHERE enabled AND next_run_at IS NOT NULL;

-- Run records (integrate-scheduler design D7 "lightweight run-records
-- view"): one row per execution of a scheduler, carrying status, trigger,
-- token usage and delivery outcome. The transcript itself persists as
-- session events addressable via session_id ("sched_<schedulerID>_<ts>");
-- workspace_id is denormalized (house tenant rule) without its own FK —
-- the cascading scheduler FK already pins the row to one workspace.
CREATE TABLE scheduler_runs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    uuid NOT NULL,
    scheduler_id    uuid NOT NULL REFERENCES schedulers(id) ON DELETE CASCADE,
    session_id      text NOT NULL,
    trigger         text NOT NULL CHECK (trigger IN ('scheduler', 'manual')),
    status          text NOT NULL CHECK (status IN ('running', 'completed', 'failed', 'cancelled', 'blocked', 'missed')),
    started_at      timestamptz NOT NULL,
    duration_ms     bigint NOT NULL DEFAULT 0,
    tokens_used     integer NOT NULL DEFAULT 0,
    delivery_status text NOT NULL DEFAULT '',
    error           text NOT NULL DEFAULT ''
);

-- Runs listing: newest first per scheduler.
CREATE INDEX idx_scheduler_runs_scheduler_started
    ON scheduler_runs(scheduler_id, started_at DESC);

-- Backfill scheduler.read and scheduler.write to existing built-in roles that
-- grant them since the scheduler feature (000044). Roles created before it
-- keep the older permission snapshot and cannot access workspace schedulers.
-- scheduler.read reaches every built-in role (screens are visible to
-- Members); scheduler.write stops at Owner and Admin.

-- 1. Add to Superadmin
UPDATE roles
SET permissions = array_append(permissions, 'scheduler.read')
WHERE built_in = true AND name = 'Superadmin'
  AND NOT ('scheduler.read' = ANY(permissions));

UPDATE roles
SET permissions = array_append(permissions, 'scheduler.write')
WHERE built_in = true AND name = 'Superadmin'
  AND NOT ('scheduler.write' = ANY(permissions));

-- 2. Add to Owner
UPDATE roles
SET permissions = array_append(permissions, 'scheduler.read')
WHERE built_in = true AND name = 'Owner'
  AND NOT ('scheduler.read' = ANY(permissions));

UPDATE roles
SET permissions = array_append(permissions, 'scheduler.write')
WHERE built_in = true AND name = 'Owner'
  AND NOT ('scheduler.write' = ANY(permissions));

-- 3. Add to Admin
UPDATE roles
SET permissions = array_append(permissions, 'scheduler.read')
WHERE built_in = true AND name = 'Admin'
  AND NOT ('scheduler.read' = ANY(permissions));

UPDATE roles
SET permissions = array_append(permissions, 'scheduler.write')
WHERE built_in = true AND name = 'Admin'
  AND NOT ('scheduler.write' = ANY(permissions));

-- 4. Add to Member (read only)
UPDATE roles
SET permissions = array_append(permissions, 'scheduler.read')
WHERE built_in = true AND name = 'Member'
  AND NOT ('scheduler.read' = ANY(permissions));
