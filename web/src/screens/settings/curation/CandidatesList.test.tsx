import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { CandidatesList, probationDays } from './CandidatesList';
import { api, type ApiSkillCandidate } from '../../../lib/api';

const DAY = 86400000;

const candidate = (overrides: Partial<ApiSkillCandidate> = {}): ApiSkillCandidate => ({
  id: 'cand-1',
  workspace_id: 'acme',
  agent_id: 'ag-radar',
  cluster_id: 'cl-deploy',
  skill_name: 'deploy-rollout',
  status: 'pending',
  proposed_content: '# Deploy rollout',
  is_edit: false,
  evidence_event_ids: ['ev-1', 'ev-2', 'ev-3'],
  cited_pattern_refs: ['deploy-rollout'],
  helpful_count: 0,
  harmful_count: 0,
  use_count: 0,
  proposed_at: new Date(Date.now() - 2 * DAY).toISOString(),
  updated_at: new Date(Date.now() - 2 * DAY).toISOString(),
  ...overrides,
});

describe('screens/settings/curation/CandidatesList', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders review cards with provenance and cited patterns', async () => {
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({
      candidates: [candidate()],
      count: 1,
    });

    render(
      <MemoryRouter>
        <CandidatesList wsSlug="acme" onOpen={vi.fn()} />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('candidate-deploy-rollout')).not.toBeNull();
    });
    const card = screen.getByTestId('candidate-deploy-rollout');
    expect(card.textContent).toContain('deploy-rollout');
    expect(card.textContent).toContain('new skill');
    expect(card.textContent).toContain('pending');
    expect(card.textContent).toContain('agent · ag-radar');
    expect(card.textContent).toContain('cluster · cl-deploy');
    expect(card.textContent).toContain('3 evidence events');
    expect(card.textContent).toContain('pattern · deploy-rollout');
  });

  it('marks an edit proposal as an edit naming the superseded skill', async () => {
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({
      candidates: [
        candidate({
          id: 'cand-2',
          skill_name: 'deploy-rollout',
          is_edit: true,
          supersedes_skill_name: 'deploy-rollout',
          superseded_content: '# old',
          status: 'pending',
        }),
      ],
      count: 1,
    });

    render(
      <MemoryRouter>
        <CandidatesList wsSlug="acme" onOpen={vi.fn()} />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('candidate-deploy-rollout')).not.toBeNull();
    });
    const card = screen.getByTestId('candidate-deploy-rollout');
    expect(card.textContent).toContain('edit · supersedes');
    expect(card.textContent).toContain('deploy-rollout');
  });

  it('shows a provisional card with its probation day count and outcome tally', async () => {
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({
      candidates: [
        candidate({
          status: 'provisional',
          decided_at: new Date(Date.now() - 4 * DAY).toISOString(),
          helpful_count: 3,
          harmful_count: 1,
          use_count: 7,
        }),
      ],
      count: 1,
    });

    render(
      <MemoryRouter>
        <CandidatesList wsSlug="acme" onOpen={vi.fn()} />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('candidate-probation-deploy-rollout')).not.toBeNull();
    });
    expect(screen.getByTestId('candidate-probation-deploy-rollout').textContent).toContain('day 4');
    const tally = screen.getByTestId('candidate-tally-deploy-rollout');
    expect(tally.textContent).toContain('3 helpful');
    expect(tally.textContent).toContain('1 harmful');
    expect(tally.textContent).toContain('7 runs');
  });

  it('names the mechanism in the empty state', async () => {
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({ candidates: [], count: 0 });

    render(
      <MemoryRouter>
        <CandidatesList wsSlug="acme" onOpen={vi.fn()} />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('candidates-empty')).not.toBeNull();
    });
    expect(screen.getByTestId('candidates-empty').textContent).toContain(
      'Candidates appear when agents solve similar problems repeatedly'
    );
  });

  it('survives a failed load with a retry', async () => {
    vi.spyOn(api.curation, 'listCandidates').mockRejectedValue(new Error('boom'));

    render(
      <MemoryRouter>
        <CandidatesList wsSlug="acme" onOpen={vi.fn()} />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('candidates-error')).not.toBeNull();
    });
    expect(screen.getByTestId('btn-candidates-retry')).not.toBeNull();
  });

  it('renders an extraction-failed card with an error chip and a working retry action', async () => {
    const failedRow = candidate({
      id: 'cand-failed',
      skill_name: 'ghost-draft',
      status: 'failed',
      proposed_content: '',
      evidence_event_ids: [],
      cited_pattern_refs: [],
      reason: 'response is not a valid proposal object: no JSON object in response',
    });
    const listSpy = vi
      .spyOn(api.curation, 'listCandidates')
      .mockResolvedValueOnce({ candidates: [failedRow], count: 1 })
      .mockResolvedValue({ candidates: [candidate()], count: 1 });
    const retrySpy = vi.spyOn(api.curation, 'retryCandidate').mockResolvedValue({ candidate: candidate() });

    render(
      <MemoryRouter>
        <CandidatesList wsSlug="acme" onOpen={vi.fn()} />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('candidate-ghost-draft')).not.toBeNull();
    });
    expect(screen.getByTestId('candidate-ghost-draft').textContent).toContain('ghost-draft');
    const footer = screen.getByTestId('candidate-failed-ghost-draft');
    expect(footer.textContent).toContain('extraction failed');
    expect(footer.textContent).toContain('response is not a valid proposal object');

    fireEvent.click(screen.getByTestId('btn-candidate-retry-ghost-draft'));
    await waitFor(() => {
      expect(retrySpy).toHaveBeenCalledWith('acme', 'cand-failed');
    });
    // The queue reloads; the retried row renders as its fresh pending card.
    await waitFor(() => {
      expect(listSpy.mock.calls.length).toBeGreaterThanOrEqual(2);
    });
    await waitFor(() => {
      expect(screen.getByTestId('candidate-deploy-rollout')).not.toBeNull();
    });
  });

  it('keeps a failed retry on the card with the refreshed error message', async () => {
    const failedRow = candidate({
      skill_name: 'ghost-draft',
      status: 'failed',
      reason: 'earlier failure',
    });
    vi.spyOn(api.curation, 'listCandidates')
      .mockResolvedValueOnce({ candidates: [failedRow], count: 1 })
      .mockResolvedValue({
        candidates: [candidate({ skill_name: 'ghost-draft', status: 'failed', reason: 'retry found no extractable draft: no readable windows' })],
        count: 1,
      });
    vi.spyOn(api.curation, 'retryCandidate').mockRejectedValue(new Error('retry found no extractable draft'));

    render(
      <MemoryRouter>
        <CandidatesList wsSlug="acme" onOpen={vi.fn()} />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('btn-candidate-retry-ghost-draft')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-candidate-retry-ghost-draft'));
    await waitFor(() => {
      expect(screen.getByTestId('candidate-failed-ghost-draft').textContent).toContain(
        'retry found no extractable draft'
      );
    });
    expect(screen.getByTestId('candidate-failed-ghost-draft').textContent).toContain('extraction failed');
  });

  it('renders the deleted-source note only where source_available is false', async () => {
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({
      candidates: [
        candidate({ id: 'cand-stale', skill_name: 'stale-evidence', source_available: false }),
        candidate({ id: 'cand-fresh', skill_name: 'fresh-evidence' }),
      ],
      count: 2,
    });

    render(
      <MemoryRouter>
        <CandidatesList wsSlug="acme" onOpen={vi.fn()} />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('candidate-source-missing-stale-evidence')).not.toBeNull();
    });
    expect(screen.getByTestId('candidate-source-missing-stale-evidence').textContent).toContain(
      'source no longer available'
    );
    expect(screen.queryByTestId('candidate-source-missing-fresh-evidence')).toBeNull();
    // The note is an annotation — the card keeps its review affordance.
    expect(screen.getByTestId('candidate-stale-evidence').textContent).toContain('pending');
  });

  it('probationDays counts whole days since the decision and degrades to 0', () => {
    const now = new Date('2026-10-02T12:00:00Z');
    expect(probationDays({ decided_at: '2026-09-30T12:00:00Z' } as ApiSkillCandidate, now)).toBe(2);
    expect(probationDays({ updated_at: '2026-10-02T06:00:00Z' } as ApiSkillCandidate, now)).toBe(0);
    expect(probationDays({ decided_at: 'not-a-date' } as ApiSkillCandidate, now)).toBe(0);
    expect(probationDays({} as ApiSkillCandidate, now)).toBe(0);
  });
});
