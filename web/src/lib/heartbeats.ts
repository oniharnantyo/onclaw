// Heartbeat client (change add-agent-heartbeat): one opt-in heartbeat per
// agent, addressed as an agent sub-resource. Wire shapes mirror the server's
// snake_case JSON (domain AgentHeartbeat / HeartbeatRun). Follows the
// schedulers client pattern — thin request() wrappers, no state.
import { request } from './api';

/** One heartbeat. `expr` is the canonical 5-field cron in the workspace
 * timezone. `human_label` is server-derived at read time (like the scheduler
 * label) and never stored from input. `last_tick` is a snapshot of the most
 * recent run (null before the first tick). */
export interface ApiHeartbeat {
  id: string;
  workspace_id: string;
  agent_id: string;
  /** HEARTBEAT checklist prompt; empty means skip-until-edited. */
  prompt: string;
  expr: string;
  human_label: string;
  /** Optional active-hours window (workspace timezone); null when unset. */
  active_start: string | null;
  active_end: string | null;
  delivery: { type: 'creator_dm' | 'channel'; channel_id?: string };
  enabled: boolean;
  /** Absent (null) when paused or disabled. */
  next_tick_at: string | null;
  last_tick: ApiHeartbeatLastTick | null;
  /** Consecutive failed ticks; auto-pauses at 5 (server-side). */
  failure_streak: number;
  created_at: string;
  updated_at: string;
}

/** Read-only snapshot of the most recent tick. */
export interface ApiHeartbeatLastTick {
  status: string;
  trigger: string;
  started_at: string;
  duration_ms: number;
  tokens_used: number;
  session_id: string;
  delivery_status: string;
  error?: string;
}

/** One heartbeat execution (run-now / read view). */
export interface ApiHeartbeatRun {
  id: string;
  heartbeat_id: string;
  session_id: string;
  trigger: string;
  status: string;
  started_at: string;
  duration_ms: number;
  tokens_used: number;
  delivery_status: string;
  error?: string;
  trace_id?: string;
}

export interface HeartbeatPayload {
  enabled: boolean;
  expr: string;
  active_start: string | null;
  active_end: string | null;
  delivery: { type: 'creator_dm' | 'channel'; channel_id?: string };
  prompt: string;
}

export const heartbeats = {
  // The agent's current heartbeat, or `{ heartbeat: null }` when it has none
  // (never-created state). `default_prompt` carries the server's embedded
  // checklist template for the UI's reset-to-default affordance.
  get: (ws: string, agentId: string) =>
    request<{ heartbeat: ApiHeartbeat | null; default_prompt: string }>(
      `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agentId)}/heartbeat`,
      { method: 'GET' }
    ),
  // Create-or-update: the PUT seeds the row on first save. 422 answers carry
  // fielded validation details on the thrown ApiError.
  update: (ws: string, agentId: string, body: HeartbeatPayload) =>
    request<{ heartbeat: ApiHeartbeat }>(
      `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agentId)}/heartbeat`,
      { method: 'PUT', body }
    ),
  // Fire one tick immediately; 409 (conflict) while a tick is already in
  // flight. Unknown agent/heartbeat answers 404.
  runNow: (ws: string, agentId: string) =>
    request<{ run: ApiHeartbeatRun }>(
      `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agentId)}/heartbeat/run-now`,
      { method: 'POST' }
    ),
  // Re-enable an auto-paused heartbeat and recompute `next_tick_at`.
  resume: (ws: string, agentId: string) =>
    request<{ heartbeat: ApiHeartbeat }>(
      `/workspaces/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agentId)}/heartbeat/resume`,
      { method: 'POST' }
    ),
};
