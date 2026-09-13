// Scheduler client (change integrate-scheduler, design D11): workspace-scoped
// CRUD, run-now, and the run-history reads. Wire shapes mirror the server's
// snake_case JSON (domain.Scheduler / SchedulerRun). Follows the channels
// client pattern in lib/api.ts — thin request() wrappers, no state.
import { request } from './api';

/** One saved scheduler. `expr` is the canonical 5-field cron in the
 * workspace timezone (recurring only; "" for once, which uses `run_at`).
 * `human_label` is server-derived at read time ("09:00 · Mon–Fri"; the raw
 * expression for custom recurrences) and never stored from input. */
export interface Scheduler {
  id: string;
  workspace_id: string;
  agent_id: string;
  created_by: string | null;
  name: string;
  prompt: string;
  kind: 'recurring' | 'once';
  /** 5-field cron; "" for once. */
  expr: string;
  /** ISO instant for once; null for recurring. */
  run_at: string | null;
  delivery: { type: 'thread' | 'channel'; channel_id?: string };
  enabled: boolean;
  /** Absent (null) when paused or archived. */
  next_run_at: string | null;
  last_run: null | {
    status: 'completed' | 'failed' | 'cancelled' | 'blocked' | 'missed';
    trigger: 'scheduler' | 'manual';
    started_at: string;
    duration_ms: number;
    tokens_used: number;
    session_id: string;
    delivery_status: '' | 'delivered' | 'suppressed' | 'failed';
    error?: string;
  };
  human_label: string;
  created_at: string;
  updated_at: string;
}

/** One scheduler execution (read view). `session_id` is the run transcript's
 * session address (`sched_<schedulerID>_<ts>`) — openable like any session. */
export interface SchedulerRun {
  id: string;
  scheduler_id: string;
  scheduler_name?: string;
  agent_id?: string;
  agent_name?: string;
  session_id: string;
  trigger: 'scheduler' | 'manual';
  status: 'running' | 'completed' | 'failed' | 'cancelled' | 'blocked' | 'missed';
  started_at: string;
  duration_ms: number;
  tokens_used: number;
  delivery_status: '' | 'delivered' | 'suppressed' | 'failed';
  error?: string;
  /** Deep link to the run's observability trace (integrate-langfuse-tracing
   * D6): composed server-side from the configured host + persisted trace id.
   * Null/absent when tracing is unconfigured or the run predates tracing —
   * the client learns nothing else about the backend. */
  langfuse_url?: string | null;
}

export interface SchedulerPayload {
  name: string;
  agent_id: string;
  prompt: string;
  kind: 'recurring' | 'once';
  /** Required when kind is recurring; ignored ("" for) once. */
  expr?: string;
  /** ISO instant, required when kind is once. */
  run_at?: string | null;
  delivery: { type: 'thread' | 'channel'; channel_id?: string };
  enabled?: boolean;
}

export type PatchSchedulerPayload = Partial<SchedulerPayload>;

export interface ListRunsOpts {
  limit?: number;
  offset?: number;
}

const qs = (opts?: ListRunsOpts): string => {
  const params = new URLSearchParams();
  if (opts?.limit !== undefined) params.set('limit', String(opts.limit));
  if (opts?.offset !== undefined) params.set('offset', String(opts.offset));
  const q = params.toString();
  return q ? `?${q}` : '';
};

export const schedulers = {
  list: (ws: string) =>
    request<{ schedulers: Scheduler[] }>(`/workspaces/${encodeURIComponent(ws)}/schedulers`, {
      method: 'GET',
    }),
  create: (ws: string, body: SchedulerPayload) =>
    request<{ scheduler: Scheduler }>(`/workspaces/${encodeURIComponent(ws)}/schedulers`, {
      method: 'POST',
      body,
    }),
  get: (ws: string, id: string) =>
    request<{ scheduler: Scheduler }>(
      `/workspaces/${encodeURIComponent(ws)}/schedulers/${encodeURIComponent(id)}`,
      { method: 'GET' }
    ),
  update: (ws: string, id: string, body: PatchSchedulerPayload) =>
    request<{ scheduler: Scheduler }>(
      `/workspaces/${encodeURIComponent(ws)}/schedulers/${encodeURIComponent(id)}`,
      { method: 'PATCH', body }
    ),
  delete: (ws: string, id: string) =>
    request<void>(
      `/workspaces/${encodeURIComponent(ws)}/schedulers/${encodeURIComponent(id)}`,
      { method: 'DELETE' }
    ),
  /** Run now (design D9): direct dispatch recording trigger `manual`; the
   * in-flight map answers 409 (conflict) while a run is already live. */
  run: (ws: string, id: string) =>
    request<{ run: SchedulerRun }>(
      `/workspaces/${encodeURIComponent(ws)}/schedulers/${encodeURIComponent(id)}/run`,
      { method: 'POST' }
    ),
  /** Per-scheduler history, newest-first. */
  runs: (ws: string, id: string, opts?: ListRunsOpts) =>
    request<{ runs: SchedulerRun[]; total: number }>(
      `/workspaces/${encodeURIComponent(ws)}/schedulers/${encodeURIComponent(id)}/runs${qs(opts)}`,
      { method: 'GET' }
    ),
  /** Workspace-wide history, newest-first. */
  listRuns: (ws: string, opts?: ListRunsOpts) =>
    request<{ runs: SchedulerRun[]; total: number }>(
      `/workspaces/${encodeURIComponent(ws)}/scheduler-runs${qs(opts)}`,
      { method: 'GET' }
    ),
};
