import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { CurationTab, cycleOutcomeLine } from './CurationTab';
import { ApiError, api, type ApiCycleStatus } from '../../../lib/api';

const idleStatus = (): ApiCycleStatus => ({
  workspace_id: 'acme',
  state: 'idle',
  started_at: '',
  finished_at: '',
  stages: [],
  counters: {
    clusters_considered: 0,
    clusters_processed: 0,
    proposals_drafted: 0,
    patterns_changed: 0,
    pattern_count: 0,
    candidates_pending: 0,
    probation_graduated: 0,
    probation_disabled: 0,
    qualifying_runs: 0,
    cluster_runs: 0,
    approved_curated_skills: 0,
    rejected_proposals: 0,
  },
});

const doneStatus = (): ApiCycleStatus => ({
  ...idleStatus(),
  state: 'succeeded',
  trigger: 'scheduled',
  started_at: '2026-10-01T03:00:00Z',
  finished_at: '2026-10-01T03:04:00Z',
  stages: [
    { stage: 'probation_sweep', ok: true },
    { stage: 'proposals', ok: true },
  ],
  counters: { ...idleStatus().counters, pattern_count: 4, proposals_drafted: 2, candidates_pending: 2 },
});

const renderTab = (props: Partial<Parameters<typeof CurationTab>[0]> = {}) => {
  const merged = {
    tenant: { id: 'acme', sub: 'acme', name: 'Acme Corp' },
    canWrite: true,
    onToast: vi.fn(),
    ...props,
  };
  render(
    <MemoryRouter>
      <CurationTab {...merged} />
    </MemoryRouter>
  );
  return merged;
};

describe('screens/settings/curation/CurationTab', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('shows the cycle status line — last run, outcome, pattern and proposal counts', async () => {
    vi.spyOn(api.curation, 'cycleStatus').mockResolvedValue({ status: doneStatus() });

    renderTab();

    await waitFor(() => {
      expect(screen.getByTestId('curation-outcome').textContent).toBe('succeeded');
    });
    expect(screen.getByTestId('curation-last-run').textContent).not.toBe('');
    expect(screen.getByTestId('curation-pattern-count').textContent).toContain('4 patterns');
    expect(screen.getByTestId('curation-proposal-count').textContent).toContain('2 proposals drafted');
    // The cadence pointer names workspace config.
    expect(screen.getByTestId('curation-status-line').textContent).toContain('workspace configuration');
  });

  it('reports the never-run state honestly', async () => {
    vi.spyOn(api.curation, 'cycleStatus').mockResolvedValue({ status: idleStatus() });

    renderTab();

    await waitFor(() => {
      expect(screen.getByTestId('curation-status-line').textContent).toContain('No cycle has run');
    });
    expect(screen.getByTestId('curation-pattern-count').textContent).toContain('0 patterns');
  });

  it('run curation now: running state, status refresh, and the 409 already-running notice', async () => {
    vi.spyOn(api.curation, 'cycleStatus').mockResolvedValue({ status: doneStatus() });
    let resolveRun: (v: { status: ApiCycleStatus }) => void = () => {};
    const runCycle = vi.spyOn(api.curation, 'runCycle').mockImplementation(
      () => new Promise((resolve) => { resolveRun = resolve; })
    );

    const props = renderTab();

    await waitFor(() => {
      expect(screen.getByTestId('btn-run-curation')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-run-curation'));

    // Visible running state while executing.
    expect(screen.getByTestId('btn-run-curation').textContent).toContain('Running');
    expect((screen.getByTestId('btn-run-curation') as HTMLButtonElement).disabled).toBe(true);

    resolveRun({ status: { ...doneStatus(), state: 'running' } });
    await waitFor(() => {
      expect(runCycle).toHaveBeenCalledWith('acme');
    });
    await waitFor(() => {
      expect(screen.getByTestId('curation-outcome').textContent).toBe('running now');
    });
    expect(props.onToast).toHaveBeenCalled();

    // A second trigger colliding in flight maps to the already-running notice.
    vi.spyOn(api.curation, 'runCycle').mockRejectedValueOnce(new ApiError(409, 'conflict', 'cycle already in flight'));
    fireEvent.click(screen.getByTestId('btn-run-curation'));
    await waitFor(() => {
      expect(screen.getByTestId('curation-run-notice')).not.toBeNull();
    });
    expect(screen.getByTestId('curation-run-notice').textContent).toContain('already running');
  });

  it('members get no run-curation-now control', async () => {
    vi.spyOn(api.curation, 'cycleStatus').mockResolvedValue({ status: doneStatus() });

    renderTab({ canWrite: false });

    await waitFor(() => {
      expect(screen.getByTestId('curation-tab-candidates')).not.toBeNull();
    });
    expect(screen.queryByTestId('btn-run-curation')).toBeNull();
  });

  it('switches between Candidates, Patterns, and Audit', async () => {
    vi.spyOn(api.curation, 'cycleStatus').mockResolvedValue({ status: idleStatus() });
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({ candidates: [], count: 0 });
    vi.spyOn(api.curation, 'listPatterns').mockResolvedValue({ patterns: [] });
    vi.spyOn(api.curation, 'listAudit').mockResolvedValue({ entries: [] });

    renderTab();

    await waitFor(() => {
      expect(screen.getByTestId('candidates-empty')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('curation-tab-patterns'));
    await waitFor(() => {
      expect(screen.getByTestId('patterns-empty')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('curation-tab-audit'));
    await waitFor(() => {
      expect(screen.getByTestId('audit-empty')).not.toBeNull();
    });
  });

  it('opens the review takeover from a candidate card', async () => {
    vi.spyOn(api.curation, 'cycleStatus').mockResolvedValue({ status: idleStatus() });
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({
      candidates: [
        {
          id: 'cand-9', workspace_id: 'acme', agent_id: 'ag', cluster_id: 'cl', skill_name: 'deploy-rollout',
          status: 'pending', proposed_content: '# x', is_edit: false, evidence_event_ids: [],
          cited_pattern_refs: [], helpful_count: 0, harmful_count: 0, use_count: 0,
          proposed_at: '', updated_at: '',
        },
      ],
      count: 1,
    });
    vi.spyOn(api.curation, 'getCandidate').mockResolvedValue({
      candidate: {
        id: 'cand-9', workspace_id: 'acme', agent_id: 'ag', cluster_id: 'cl', skill_name: 'deploy-rollout',
        status: 'pending', proposed_content: '# x', is_edit: false, evidence_event_ids: [],
        cited_pattern_refs: [], helpful_count: 0, harmful_count: 0, use_count: 0,
        proposed_at: '', updated_at: '',
      },
      evidence: { event_ids: [], cited_patterns: [] },
    });

    renderTab();

    await waitFor(() => {
      expect(screen.getByTestId('candidate-deploy-rollout')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('candidate-deploy-rollout'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-candidate-takeover')).not.toBeNull();
    });
  });

  it('cycleOutcomeLine summarizes every state', () => {
    expect(cycleOutcomeLine(null)).toBe('not run yet');
    expect(cycleOutcomeLine({ state: 'idle' } as ApiCycleStatus)).toBe('not run yet');
    expect(cycleOutcomeLine({ state: 'running' } as ApiCycleStatus)).toBe('running now');
    expect(cycleOutcomeLine({ state: 'succeeded' } as ApiCycleStatus)).toBe('succeeded');
    expect(
      cycleOutcomeLine({ state: 'failed', stages: [{ stage: 'proposals', ok: false, error: 'x' }] } as ApiCycleStatus)
    ).toBe('failed at proposals');
    expect(cycleOutcomeLine({ state: 'failed', stages: [] } as ApiCycleStatus)).toBe('failed');
  });
});
