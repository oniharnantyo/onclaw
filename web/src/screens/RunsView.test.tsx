/**
 * @vitest-environment jsdom
 */
// Runs screen on the live scheduler-runs endpoints (integrate-scheduler
// 7.4/7.5): monospace table rendering, trigger chips, filters (All/Succeeded/
// Failed), the two empty states, per-scheduler filtering via ?scheduler=,
// and the row → transcript handoff.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, cleanup, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

const listRunsMock = vi.fn().mockResolvedValue({ runs: [], total: 0 });
const perSchedulerMock = vi.fn().mockResolvedValue({ runs: [], total: 0 });

vi.mock('../lib/schedulers', () => ({
  schedulers: {
    listRuns: (...a: unknown[]) => listRunsMock(...a),
    runs: (...a: unknown[]) => perSchedulerMock(...a),
  },
}));
vi.mock('../lib/api', () => ({
  api: { request: vi.fn(), onUnauthorized: vi.fn() },
  pollAgentPromptsStatus: vi.fn(),
  formatApiError: (e: unknown, m: string) => m,
  listAgentSessions: vi.fn().mockResolvedValue({ sessions: [] }),
  deleteAgentSession: vi.fn().mockResolvedValue(undefined),
  ApiError: class ApiError extends Error {
    status: number;
    constructor(status: number, code: string, message: string) { super(message); this.status = status; }
  },
}));

import { RunsView } from './RunsView';
import { useStore } from '../store';

const navigate = vi.fn();
const setSearchParams = vi.fn();
let searchParamsValue = new URLSearchParams();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return {
    ...actual,
    useNavigate: () => navigate,
    useSearchParams: () => [searchParamsValue, setSearchParams],
  };
});

const run = (over: any) => ({
  id: 'run-1', scheduler_id: 'sch-1', scheduler_name: 'Morning ops digest',
  agent_id: 'a-atlas', agent_name: 'Atlas', session_id: 'sched_sch-1_1',
  trigger: 'scheduler', status: 'completed',
  started_at: '2026-09-10T14:00:00Z', duration_ms: 42000, tokens_used: 18200,
  delivery_status: 'delivered',
  ...over,
});

const tenant = {
  id: 't1', sub: 't1', name: 'Acme Corp', tz: 'UTC',
  agents: [{ id: 'a-atlas', name: 'Atlas' }],
  schedules: [{ id: 'sch-1', name: 'Morning ops digest' }],
  runs: [],
};

const toast = vi.fn();

const view = () =>
  render(<MemoryRouter><RunsView tenant={tenant} onToast={toast} /></MemoryRouter>);

const odId = (id: string) => document.querySelector('[data-od-id="' + id + '"]') as HTMLElement;
const findByOdId = (id: string) =>
  waitFor(() => {
    const el = odId(id);
    if (!el) throw new Error('element not found: ' + id);
    return el;
  });

beforeEach(() => {
  vi.clearAllMocks();
  searchParamsValue = new URLSearchParams();
  useStore.setState({
    pos: { tenantId: 't1', view: 'runs', chatId: '', showContext: false, railExpanded: false },
    db: { t1: JSON.parse(JSON.stringify({ ...tenant, runs: [] })) },
  } as any);
});

afterEach(cleanup);

describe('screens/RunsView — workspace-wide table', () => {
  it('renders runs with monospace id/times/counts, agent, trigger chip, and status', async () => {
    listRunsMock.mockResolvedValue({
      runs: [
        run({}),
        run({ id: 'run-2', status: 'failed', trigger: 'manual', agent_name: undefined,
          started_at: '2026-09-09T14:00:00Z', duration_ms: 130000, tokens_used: 9400 }),
        run({ id: 'run-3', status: 'running', trigger: 'manual' }),
      ],
      total: 3,
    });
    view();
    await findByOdId('run-row-run-1');
    expect(screen.getAllByText('Atlas').length).toBeGreaterThan(0);
    // run-2 has no agent_name on the wire — resolved from the workspace roster.
    expect(screen.getAllByText('Atlas').length).toBe(3);
    // Trigger chips.
    expect(screen.getAllByText('scheduler').length).toBe(1);
    expect(screen.getAllByText('manual').length).toBe(2);
    // Failed status renders with the danger color class.
    const failedRow = odId('run-row-run-2');
    expect(failedRow.querySelector('.text-danger')).not.toBeNull();
    expect(failedRow.textContent).toMatch(/failed/);
    // Duration formatting: 130000ms → "2m 10s"; tokens → "18.2k"/"9.4k".
    expect(odId('run-row-run-2').textContent).toMatch(/2m 10s/);
    expect(odId('run-row-run-1').textContent).toMatch(/18\.2k/);
  });

  it('narrowing to Failed leaves only failed runs, colored with the danger class', async () => {
    listRunsMock.mockResolvedValue({
      runs: [run({}), run({ id: 'run-2', status: 'failed', trigger: 'manual' })],
      total: 2,
    });
    view();
    await findByOdId('run-row-run-1');
    fireEvent.click(screen.getByRole('button', { name: 'Failed' }));
    expect(odId('run-row-run-1')).toBeNull();
    expect(odId('run-row-run-2')).not.toBeNull();
    expect(odId('run-row-run-2').querySelector('.text-danger')).not.toBeNull();
  });

  it('shows "No runs match this filter." when a filter matches nothing', async () => {
    listRunsMock.mockResolvedValue({ runs: [run({ status: 'completed' })], total: 1 });
    view();
    await findByOdId('run-row-run-1');
    fireEvent.click(screen.getByRole('button', { name: 'Failed' }));
    expect(screen.getByText('No runs match this filter.')).not.toBeNull();
  });

  it('shows the empty state inviting schedule creation when there are no runs', async () => {
    listRunsMock.mockResolvedValue({ runs: [], total: 0 });
    view();
    await findByOdId('runs-empty');
    expect(screen.getByText('No runs yet')).not.toBeNull();
    expect(odId('btn-runs-new-schedule')).not.toBeNull();
  });

  it('mirrors the workspace-wide feed into the store for the sidebar counts', async () => {
    const rows = [run({})];
    listRunsMock.mockResolvedValue({ runs: rows, total: 1 });
    view();
    await findByOdId('run-row-run-1');
    await waitFor(() => {
      expect((useStore.getState().db.t1 as any).runs.length).toBe(1);
    });
  });
});

describe('screens/RunsView — transcript handoff and per-scheduler view', () => {
  it("opens a run's transcript in its agent's chat with the run handoff", async () => {
    listRunsMock.mockResolvedValue({ runs: [run({})], total: 1 });
    view();
    await findByOdId('run-row-run-1');
    fireEvent.click(odId('run-row-run-1'));
    expect(navigate).toHaveBeenCalledWith('/c/a-atlas', {
      state: { openRun: { sessionId: 'sched_sch-1_1', schedulerName: 'Morning ops digest' } },
    });
  });

  it('queries the per-scheduler endpoint and offers the way back to all runs', async () => {
    searchParamsValue = new URLSearchParams('scheduler=sch-1');
    perSchedulerMock.mockResolvedValue({ runs: [run({ id: 'run-9' })], total: 1 });
    view();
    await findByOdId('run-row-run-9');
    expect(perSchedulerMock).toHaveBeenCalledWith('t1', 'sch-1', { limit: 100 });
    expect(listRunsMock).not.toHaveBeenCalled();
    // The back link clears the filter.
    fireEvent.click(screen.getByRole('button', { name: /Show all runs/i }));
    expect(setSearchParams).toHaveBeenCalledWith({});
  });

  it('toasts instead of navigating when the run has no resolvable agent', async () => {
    listRunsMock.mockResolvedValue({
      runs: [run({ agent_id: undefined, agent_name: 'Ghost' })],
      total: 1,
    });
    view();
    await findByOdId('run-row-run-1');
    fireEvent.click(odId('run-row-run-1'));
    expect(toast).toHaveBeenCalledWith("This run's agent is no longer available", 'danger');
    expect(navigate).not.toHaveBeenCalled();
  });
});

describe('screens/RunsView — Langfuse link (integrate-langfuse-tracing 4.1)', () => {
  it('offers "Open in Langfuse" on the row when langfuse_url is set, opening a new tab', async () => {
    listRunsMock.mockResolvedValue({
      runs: [run({ langfuse_url: 'https://langfuse.acme.example.com/trace/tr-123' })],
      total: 1,
    });
    view();
    await findByOdId('run-row-run-1');
    const link = document.querySelector('a[data-od-id="run-langfuse-run-1"]') as HTMLAnchorElement | null;
    expect(link).not.toBeNull();
    expect(link!.getAttribute('href')).toBe('https://langfuse.acme.example.com/trace/tr-123');
    expect(link!.getAttribute('target')).toBe('_blank');
    expect(link!.getAttribute('rel')).toBe('noopener');
    expect(link!.getAttribute('aria-label')).toBe('Open in Langfuse');
  });

  it("clicking the Langfuse action opens the trace, not the run's transcript", async () => {
    listRunsMock.mockResolvedValue({
      runs: [run({ langfuse_url: 'https://langfuse.acme.example.com/trace/tr-123' })],
      total: 1,
    });
    view();
    await findByOdId('run-row-run-1');
    fireEvent.click(document.querySelector('a[data-od-id="run-langfuse-run-1"]') as Element);
    expect(navigate).not.toHaveBeenCalled();
  });

  it('carries the deep link through the transcript handoff for traced runs', async () => {
    listRunsMock.mockResolvedValue({
      runs: [run({ langfuse_url: 'https://langfuse.acme.example.com/trace/tr-123' })],
      total: 1,
    });
    view();
    await findByOdId('run-row-run-1');
    fireEvent.click(odId('run-row-run-1'));
    expect(navigate).toHaveBeenCalledWith('/c/a-atlas', {
      state: { openRun: {
        sessionId: 'sched_sch-1_1',
        schedulerName: 'Morning ops digest',
        langfuseUrl: 'https://langfuse.acme.example.com/trace/tr-123',
      } },
    });
  });

  it('renders no Langfuse action anywhere when langfuse_url is absent or null', async () => {
    listRunsMock.mockResolvedValue({
      runs: [run({}), run({ id: 'run-2', langfuse_url: null })],
      total: 2,
    });
    view();
    await findByOdId('run-row-run-1');
    expect(document.querySelector('[data-od-id="run-langfuse-run-1"]')).toBeNull();
    expect(document.querySelector('[data-od-id="run-langfuse-run-2"]')).toBeNull();
    expect(document.querySelector('a[title="Open in Langfuse"]')).toBeNull();
    // Neither row's transcript handoff carries a link.
    fireEvent.click(odId('run-row-run-1'));
    expect(navigate).toHaveBeenCalledWith('/c/a-atlas', {
      state: { openRun: { sessionId: 'sched_sch-1_1', schedulerName: 'Morning ops digest' } },
    });
  });
});
