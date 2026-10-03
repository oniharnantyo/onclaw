import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { PatternsTab } from './PatternsTab';
import { api, type ApiPatternPage } from '../../../lib/api';

const page = (overrides: Partial<ApiPatternPage> = {}): ApiPatternPage => ({
  slug: 'deploy-rollout',
  title: 'Deploy rollout',
  status: 'active',
  evidence_runs: ['sess-1', 'sess-2'],
  body: 'Canary first, then promote.',
  ...overrides,
});

describe('screens/settings/curation/PatternsTab', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('lists the wiki read-only with status and cited-by skills', async () => {
    vi.spyOn(api.curation, 'listPatterns').mockResolvedValue({
      patterns: [page()],
    });
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({
      candidates: [
        {
          id: 'cand-1', workspace_id: 'acme', agent_id: 'ag', cluster_id: 'cl', skill_name: 'deploy-rollout',
          status: 'provisional', proposed_content: '', is_edit: false, evidence_event_ids: [],
          cited_pattern_refs: ['deploy-rollout'], helpful_count: 0, harmful_count: 0, use_count: 0,
          proposed_at: '', updated_at: '',
        },
      ],
      count: 1,
    });

    render(
      <MemoryRouter>
        <PatternsTab wsSlug="acme" />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('pattern-deploy-rollout')).not.toBeNull();
    });
    const row = screen.getByTestId('pattern-deploy-rollout');
    expect(row.textContent).toContain('Deploy rollout');
    expect(row.textContent).toContain('active');
    expect(row.textContent).toContain('cited by deploy-rollout');
    expect(row.textContent).toContain('2 evidence runs');
    // No editing affordance anywhere.
    expect(screen.queryByTestId('btn-edit-pattern-deploy-rollout')).toBeNull();
  });

  it('keeps a superseded page visible with its successor link', async () => {
    vi.spyOn(api.curation, 'listPatterns').mockResolvedValue({
      patterns: [
        page({ slug: 'deploy-old', title: 'Deploy (old)', status: 'superseded', superseded_by: 'deploy-rollout' }),
        page(),
      ],
    });
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({ candidates: [], count: 0 });

    render(
      <MemoryRouter>
        <PatternsTab wsSlug="acme" />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('pattern-deploy-old')).not.toBeNull();
    });
    const row = screen.getByTestId('pattern-deploy-old');
    expect(row.textContent).toContain('superseded');
    expect(row.textContent).toContain('deploy-rollout');
    // The successor link swaps the viewed page.
    fireEvent.click(screen.getByTestId('btn-successor-deploy-old'));
    await waitFor(() => {
      expect(screen.getByTestId('patterns-page-view')).not.toBeNull();
    });
    expect(screen.getByTestId('pattern-page-title').textContent).toContain('Deploy rollout');
  });

  it('opens the page view with evidence runs and a way back', async () => {
    vi.spyOn(api.curation, 'listPatterns').mockResolvedValue({ patterns: [page()] });
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({ candidates: [], count: 0 });

    render(
      <MemoryRouter>
        <PatternsTab wsSlug="acme" />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('btn-open-pattern-deploy-rollout')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-open-pattern-deploy-rollout'));

    await waitFor(() => {
      expect(screen.getByTestId('pattern-page')).not.toBeNull();
    });
    expect(screen.getByTestId('pattern-page-body').textContent).toContain('Canary first, then promote.');
    expect(screen.getByTestId('pattern-page-evidence-sess-1')).not.toBeNull();
    expect(screen.getByTestId('pattern-page-evidence-sess-2')).not.toBeNull();

    fireEvent.click(screen.getByTestId('btn-patterns-back'));
    expect(screen.getByTestId('patterns-list')).not.toBeNull();
  });

  it('renders the empty state and a failed load with retry', async () => {
    const listPatterns = vi.spyOn(api.curation, 'listPatterns');
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({ candidates: [], count: 0 });

    listPatterns.mockResolvedValue({ patterns: [] });
    const { unmount } = render(
      <MemoryRouter>
        <PatternsTab wsSlug="acme" />
      </MemoryRouter>
    );
    await waitFor(() => {
      expect(screen.getByTestId('patterns-empty')).not.toBeNull();
    });
    expect(screen.getByTestId('patterns-empty').textContent).toContain('No pattern pages yet');
    unmount();

    listPatterns.mockRejectedValue(new Error('boom'));
    render(
      <MemoryRouter>
        <PatternsTab wsSlug="acme" />
      </MemoryRouter>
    );
    await waitFor(() => {
      expect(screen.getByTestId('patterns-error')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-patterns-retry'));
    await waitFor(() => {
      expect(screen.getByTestId('patterns-loading')).not.toBeNull();
    });
  });
});
