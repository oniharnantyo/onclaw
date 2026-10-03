import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { CandidateTakeover, REJECT_REASONS } from './CandidateTakeover';
import { ApiError, api, type ApiSkillCandidate } from '../../../lib/api';

const candidate = (overrides: Partial<ApiSkillCandidate> = {}): ApiSkillCandidate => ({
  id: 'cand-1',
  workspace_id: 'acme',
  agent_id: 'ag-radar',
  cluster_id: 'cl-deploy',
  skill_name: 'deploy-rollout',
  status: 'pending',
  proposed_content: '# Deploy rollout\n\nRun the canary, then promote.',
  is_edit: false,
  evidence_event_ids: ['ev-aaa', 'ev-bbb'],
  cited_pattern_refs: ['deploy-rollout'],
  helpful_count: 0,
  harmful_count: 0,
  use_count: 0,
  proposed_at: '2026-09-30T00:00:00Z',
  updated_at: '2026-09-30T00:00:00Z',
  ...overrides,
});

const renderTakeover = (props: Partial<Parameters<typeof CandidateTakeover>[0]> = {}) => {
  const merged = {
    wsSlug: 'acme',
    candidateId: 'cand-1',
    canWrite: true,
    onClose: vi.fn(),
    onDecided: vi.fn(),
    onToast: vi.fn(),
    ...props,
  };
  render(
    <MemoryRouter>
      <CandidateTakeover {...merged} />
    </MemoryRouter>
  );
  return merged;
};

describe('screens/settings/curation/CandidateTakeover', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders a create proposal as a readable preview with evidence and cited patterns', async () => {
    vi.spyOn(api.curation, 'getCandidate').mockResolvedValue({
      candidate: candidate(),
      evidence: { event_ids: ['ev-aaa', 'ev-bbb'], cited_patterns: ['deploy-rollout'] },
    });

    renderTakeover();

    await waitFor(() => {
      expect(screen.getByTestId('takeover-preview')).not.toBeNull();
    });
    expect(screen.getByTestId('takeover-preview').textContent).toContain('Run the canary, then promote.');
    expect(screen.getByTestId('takeover-skill-name').textContent).toBe('deploy-rollout');
    expect(screen.getByTestId('takeover-evidence-ev-aaa')).not.toBeNull();
    expect(screen.getByTestId('takeover-evidence-ev-bbb')).not.toBeNull();
    expect(screen.getByTestId('takeover-cited-pattern-deploy-rollout')).not.toBeNull();
  });

  it('renders an edit proposal as a before/after diff, not a full replacement', async () => {
    vi.spyOn(api.curation, 'getCandidate').mockResolvedValue({
      candidate: candidate({
        is_edit: true,
        supersedes_skill_name: 'deploy-rollout',
        superseded_content: '# Deploy rollout\n\nDeploy directly to prod.',
        proposed_content: '# Deploy rollout\n\nRun the canary, then promote.',
      }),
      evidence: { event_ids: [], cited_patterns: [] },
    });

    renderTakeover();

    await waitFor(() => {
      expect(screen.getByTestId('takeover-diff')).not.toBeNull();
    });
    const diff = screen.getByTestId('takeover-diff').textContent;
    expect(diff).toContain('−');
    expect(diff).toContain('Deploy directly to prod.');
    expect(diff).toContain('+');
    expect(diff).toContain('Run the canary, then promote.');
    // Unchanged context line survives, and no full-preview pane renders.
    expect(diff).toContain('# Deploy rollout');
    expect(screen.queryByTestId('takeover-preview')).toBeNull();
  });

  it('walks the evidence chain: a cited pattern opens its wiki page inside the review', async () => {
    vi.spyOn(api.curation, 'getCandidate').mockResolvedValue({
      candidate: candidate(),
      evidence: { event_ids: ['ev-aaa'], cited_patterns: ['deploy-rollout'] },
    });
    vi.spyOn(api.curation, 'listPatterns').mockResolvedValue({
      patterns: [
        {
          slug: 'deploy-rollout',
          title: 'Deploy rollout',
          status: 'active',
          evidence_runs: ['sess-1'],
          body: 'Canary first, then promote.',
        },
      ],
    });

    renderTakeover();

    await waitFor(() => {
      expect(screen.getByTestId('takeover-cited-pattern-deploy-rollout')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('takeover-cited-pattern-deploy-rollout'));

    await waitFor(() => {
      expect(screen.getByTestId('takeover-pattern')).not.toBeNull();
    });
    expect(screen.getByTestId('takeover-pattern-title').textContent).toContain('Deploy rollout');
    expect(screen.getByTestId('takeover-pattern').textContent).toContain('active');
    expect(screen.getByTestId('takeover-pattern-evidence-sess-1')).not.toBeNull();
    // The takeover never closed — this is a nested view with a way back.
    expect(screen.getByTestId('modal-candidate-takeover')).not.toBeNull();
    fireEvent.click(screen.getByTestId('btn-pattern-back'));
    expect(screen.getByTestId('takeover-evidence-ev-aaa')).not.toBeNull();
  });

  it('reject requires a reason before it can be recorded', async () => {
    const getCandidate = vi.spyOn(api.curation, 'getCandidate').mockResolvedValue({
      candidate: candidate(),
      evidence: { event_ids: [], cited_patterns: [] },
    });
    const reject = vi.spyOn(api.curation, 'reject').mockResolvedValue({ candidate: candidate({ status: 'rejected', reason: 'Already covered' }) });

    const props = renderTakeover();

    await waitFor(() => {
      expect(screen.getByTestId('btn-reject')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-reject'));

    const confirm = screen.getByTestId('btn-reject-confirm') as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);

    // Choosing a canonical reason enables the recording.
    fireEvent.click(screen.getByTestId('reject-reason-already-covered'));
    expect((screen.getByTestId('btn-reject-confirm') as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByTestId('btn-reject-confirm'));

    await waitFor(() => {
      expect(reject).toHaveBeenCalledWith('acme', 'cand-1', 'Already covered');
    });
    await waitFor(() => {
      expect(props.onDecided).toHaveBeenCalled();
    });
    expect(getCandidate).toHaveBeenCalledWith('acme', 'cand-1');
    // All four canonical reasons are offered.
    expect(REJECT_REASONS).toHaveLength(4);
  });

  it('reject with Other requires free text before recording', async () => {
    vi.spyOn(api.curation, 'getCandidate').mockResolvedValue({
      candidate: candidate(),
      evidence: { event_ids: [], cited_patterns: [] },
    });
    const reject = vi.spyOn(api.curation, 'reject').mockResolvedValue({ candidate: candidate({ status: 'rejected' }) });

    renderTakeover();

    await waitFor(() => {
      expect(screen.getByTestId('btn-reject')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-reject'));
    fireEvent.click(screen.getByTestId('reject-reason-other'));

    // "Other" alone is not a recordable reason — free text is required.
    expect((screen.getByTestId('btn-reject-confirm') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId('reject-reason-text'), { target: { value: 'Conflicts with the new deploy policy' } });
    fireEvent.click(screen.getByTestId('btn-reject-confirm'));

    await waitFor(() => {
      expect(reject).toHaveBeenCalledWith('acme', 'cand-1', 'Conflicts with the new deploy policy');
    });
  });

  it('surfaces a 409 conflict message and keeps the review open', async () => {
    vi.spyOn(api.curation, 'getCandidate').mockResolvedValue({
      candidate: candidate(),
      evidence: { event_ids: [], cited_patterns: [] },
    });
    vi.spyOn(api.curation, 'approve').mockRejectedValue(
      new ApiError(409, 'conflict', 'curated catalog for agent ag-radar at capacity (20/20): disable or merge existing curated skills first')
    );

    const props = renderTakeover();

    await waitFor(() => {
      expect(screen.getByTestId('btn-approve')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-approve'));

    await waitFor(() => {
      expect(screen.getByTestId('takeover-conflict')).not.toBeNull();
    });
    expect(screen.getByTestId('takeover-conflict').textContent).toContain('at capacity');
    // The review stays open — the reviewer can reject instead.
    expect(screen.getByTestId('modal-candidate-takeover')).not.toBeNull();
    expect(props.onClose).not.toHaveBeenCalled();
  });

  it('approve records the decision and closes the takeover', async () => {
    vi.spyOn(api.curation, 'getCandidate').mockResolvedValue({
      candidate: candidate(),
      evidence: { event_ids: [], cited_patterns: [] },
    });
    vi.spyOn(api.curation, 'approve').mockResolvedValue({ candidate: candidate({ status: 'provisional' }) });

    const props = renderTakeover();

    await waitFor(() => {
      expect(screen.getByTestId('btn-approve')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-approve'));

    await waitFor(() => {
      expect(props.onDecided).toHaveBeenCalled();
    });
    expect(props.onClose).toHaveBeenCalled();
  });

  it('members see the same content with no action affordances', async () => {
    vi.spyOn(api.curation, 'getCandidate').mockResolvedValue({
      candidate: candidate(),
      evidence: { event_ids: [], cited_patterns: [] },
    });

    renderTakeover({ canWrite: false });

    await waitFor(() => {
      expect(screen.getByTestId('takeover-preview')).not.toBeNull();
    });
    expect(screen.queryByTestId('btn-approve')).toBeNull();
    expect(screen.queryByTestId('btn-reject')).toBeNull();
  });

  it('a non-pending candidate shows no footer actions even for writers', async () => {
    vi.spyOn(api.curation, 'getCandidate').mockResolvedValue({
      candidate: candidate({ status: 'provisional', decided_at: '2026-10-01T00:00:00Z', helpful_count: 2, harmful_count: 0, use_count: 2 }),
      evidence: { event_ids: [], cited_patterns: [] },
    });

    renderTakeover();

    await waitFor(() => {
      expect(screen.getByTestId('takeover-preview')).not.toBeNull();
    });
    expect(screen.queryByTestId('btn-approve')).toBeNull();
    expect(screen.queryByTestId('btn-reject')).toBeNull();
  });

  it('recovers from a failed detail load with retry', async () => {
    const getCandidate = vi.spyOn(api.curation, 'getCandidate').mockRejectedValueOnce(new Error('boom'));

    renderTakeover();

    await waitFor(() => {
      expect(screen.getByTestId('takeover-error')).not.toBeNull();
    });
    getCandidate.mockResolvedValue({
      candidate: candidate(),
      evidence: { event_ids: [], cited_patterns: [] },
    });
    fireEvent.click(screen.getByTestId('btn-takeover-retry'));

    await waitFor(() => {
      expect(screen.getByTestId('takeover-preview')).not.toBeNull();
    });
  });
});
