/**
 * @vitest-environment jsdom
 */
// Schedules screen on the live API (integrate-scheduler 7.2/7.5): table
// rendering from the workspace wire rows, the empty state, the pause toggle's
// optimistic flip + rollback, and run-now's 409 conflict path.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, cleanup, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

const toggleMock = vi.fn().mockResolvedValue({ scheduler: {} });
const runMock = vi.fn();
const listMock = vi.fn().mockResolvedValue({ schedulers: [] });

vi.mock('../lib/schedulers', () => ({
  schedulers: {
    list: (...a: unknown[]) => listMock(...a),
    update: (...a: unknown[]) => toggleMock(...a),
    run: (...a: unknown[]) => runMock(...a),
  },
}));
vi.mock('../lib/api', () => ({
  api: {
    request: vi.fn(),
    onUnauthorized: vi.fn(),
  },
  pollAgentPromptsStatus: vi.fn(),
  formatApiError: (e: unknown, m: string) => m,
  listAgentSessions: vi.fn().mockResolvedValue({ sessions: [] }),
  deleteAgentSession: vi.fn().mockResolvedValue(undefined),
  ApiError: class ApiError extends Error {
    status: number;
    constructor(status: number, code: string, message: string) { super(message); this.status = status; }
  },
}));

import { SchedulesView } from './SchedulesView';
import { useStore } from '../store';
import { ApiError } from '../lib/api';
import type { Scheduler } from '../lib/schedulers';

const navigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigate };
});

const row = (over: Partial<Scheduler>): Scheduler => ({
  id: 'sch-1', workspace_id: 'ws', agent_id: 'a-atlas', created_by: null,
  name: 'Morning ops digest', prompt: 'Digest', kind: 'recurring',
  expr: '0 7 * * 1-5', run_at: null, delivery: { type: 'thread' },
  enabled: true, next_run_at: '2026-09-11T14:00:00Z', last_run: null,
  human_label: '07:00 · Mon–Fri', created_at: '', updated_at: '',
  ...over,
});

const sched1 = row({});
const sched2 = row({
  id: 'sch-2', name: 'Inbox triage', agent_id: 'a-scout', expr: '*/30 * * * *',
  human_label: 'Every 30 minutes', next_run_at: null, enabled: false,
  last_run: {
    status: 'failed', trigger: 'scheduler', started_at: new Date(Date.now() - 3600_000).toISOString(),
    duration_ms: 8000, tokens_used: 2100, session_id: 'sched_sch-2_1', delivery_status: '',
  },
});

const tenant = {
  id: 't1', sub: 't1', name: 'Acme Corp', tz: 'UTC',
  agents: [{ id: 'a-atlas', name: 'Atlas' }, { id: 'a-scout', name: 'Scout' }],
  schedules: [sched1, sched2],
  runs: [],
};

const toast = vi.fn();

function seedStore() {
  useStore.setState({
    pos: { tenantId: 't1', view: 'schedules', chatId: '', showContext: false, railExpanded: false },
    db: { t1: JSON.parse(JSON.stringify({ ...tenant })) },
  } as any);
}

const view = () =>
  render(<MemoryRouter><SchedulesView tenant={tenant} onEdit={vi.fn()} onNew={vi.fn()} onToast={toast} /></MemoryRouter>);

// Rows and cells carry the house data-od-id convention (not testids).
const odId = (id: string) => document.querySelector('[data-od-id="' + id + '"]') as HTMLElement;
const findByOdId = (id: string) =>
  waitFor(() => {
    const el = odId(id);
    if (!el) throw new Error('element not found: ' + id);
    return el;
  });

beforeEach(() => {
  vi.clearAllMocks();
  listMock.mockResolvedValue({ schedulers: [sched1, sched2] });
  toggleMock.mockResolvedValue({ scheduler: {} });
  runMock.mockResolvedValue({ run: {} });
  seedStore();
});

afterEach(cleanup);

describe('screens/SchedulesView — table rendering', () => {
  it('renders schedule rows with agent names, human labels, and next runs', async () => {
    view();
    await findByOdId('schedule-row-sch-1');
    expect(screen.getByText('Morning ops digest')).not.toBeNull();
    expect(screen.getByText('runs Atlas')).not.toBeNull();
    expect(screen.getByText('runs Scout')).not.toBeNull();
    // Human labels; the custom every-30 expression stays visible alongside.
    expect(screen.getByText('07:00 · Mon–Fri')).not.toBeNull();
    expect(screen.getByText('*/30 * * * *')).not.toBeNull();
    // Next run renders for the enabled row.
    expect(odId('schedule-next-sch-1').textContent).toMatch(/:/);
  });

  it('shows — for the next run while paused and a last-run cell with status', async () => {
    view();
    await findByOdId('schedule-row-sch-2');
    expect(odId('schedule-next-sch-2').textContent).toBe('—');
    const lastRun = odId('schedule-lastrun-sch-2');
    // The failed status renders as the danger icon; when/duration as mono text.
    const icon = lastRun.querySelector('svg.text-danger');
    expect(icon).not.toBeNull();
    expect(lastRun.textContent).toMatch(/1h ago/);
    expect(lastRun.textContent).toMatch(/8s/);
  });

  it("links the last-run cell to that scheduler's runs", async () => {
    view();
    await findByOdId('schedule-row-sch-2');
    fireEvent.click(odId('schedule-lastrun-sch-2'));
    expect(navigate).toHaveBeenCalledWith('/runs?scheduler=sch-2');
  });

  it('shows the empty state inviting creation when the workspace has none', async () => {
    listMock.mockResolvedValue({ schedulers: [] });
    seedStore();
    view();
    await findByOdId('schedules-empty');
    expect(screen.getByText('No schedules yet')).not.toBeNull();
    expect(odId('btn-empty-new-schedule')).not.toBeNull();
  });

  it('shows the error state with retry when the load fails', async () => {
    listMock.mockRejectedValue(new Error('offline'));
    view();
    await findByOdId('schedules-error');
    expect(odId('btn-schedules-retry')).not.toBeNull();
  });
});

describe('screens/SchedulesView — toggle and run-now', () => {
  it('pauses via PATCH and toasts; the schedule row survives (nothing corrupted)', async () => {
    view();
    await findByOdId('schedule-row-sch-1');
    // The toggle for sch-1: aria-label 'Pause Morning ops digest'. Mobile and
    // desktop toggles both exist in the DOM (jsdom ignores CSS visibility).
    fireEvent.click(screen.getAllByRole('switch', { name: 'Pause Morning ops digest' })[0]);
    await vi.waitFor(() => {
      expect(toggleMock).toHaveBeenCalledWith('t1', 'sch-1', { enabled: false });
      expect(toast).toHaveBeenCalledWith('Paused “Morning ops digest”');
    });
    // The row is still there — no state corruption.
    expect(screen.getByText('Morning ops digest')).not.toBeNull();
  });

  it('rolls the optimistic toggle back when the PATCH fails', async () => {
    toggleMock.mockRejectedValue(new ApiError(500, 'error', 'boom'));
    view();
    await findByOdId('schedule-row-sch-2');
    fireEvent.click(screen.getAllByRole('switch', { name: 'Resume Inbox triage' })[0]);
    await vi.waitFor(() => {
      expect(toast).toHaveBeenCalledWith('Failed to update the schedule', 'danger');
    });
    // Rolled back: still paused in the store copy.
    const t = useStore.getState().db.t1 as any;
    expect(t.schedules.find((s: Scheduler) => s.id === 'sch-2').enabled).toBe(false);
  });

  it('run-now triggers the run endpoint and toasts', async () => {
    view();
    await findByOdId('schedule-row-sch-1');
    fireEvent.click(odId('schedule-run-sch-1'));
    await vi.waitFor(() => {
      expect(runMock).toHaveBeenCalledWith('t1', 'sch-1');
      expect(toast).toHaveBeenCalledWith('Triggered “Morning ops digest”');
    });
  });

  it('surfaces the 409 in-flight conflict as a notice, not an error state', async () => {
    runMock.mockRejectedValue(new ApiError(409, 'conflict', 'a run is already in flight'));
    view();
    await findByOdId('schedule-row-sch-1');
    fireEvent.click(odId('schedule-run-sch-1'));
    await vi.waitFor(() => {
      expect(toast).toHaveBeenCalledWith('“Morning ops digest” is already running', 'danger');
    });
    // No error banner — the table stays.
    expect(odId('schedules-error')).toBeNull();
    expect(screen.getByText('Morning ops digest')).not.toBeNull();
  });
});
