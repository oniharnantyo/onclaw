import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { SkillsPane } from './SkillsPane';
import { api, type ApiSkillCandidate, type ApiWorkspaceSkill } from '../../lib/api';

const mockTenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

const rows = (): { skills: ApiWorkspaceSkill[] } => ({
  skills: [
    {
      id: 'sk-sweeper', workspace_id: 'acme', tier: 'workspace', name: 'changelog-sweeper',
      version: '0.1.0', source: 'authored', enabled: true,
      description: 'Sweeps commit logs for changelog entries.', created_at: '', updated_at: '',
    },
    {
      id: 'sys-web-research', tier: 'system', name: 'web-research', version: '2.4.1', source: 'system',
      locked: true, enabled: true, description: 'Research briefs.', created_at: '', updated_at: '',
    },
  ],
});

describe('screens/settings/SkillsPane', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders library rows plus the locked System section', async () => {
    vi.spyOn(api.skills, 'list').mockResolvedValue(rows());

    render(<SkillsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('skill-changelog-sweeper')).not.toBeNull();
    });
    expect(screen.getByText('v0.1.0')).not.toBeNull();
    expect(screen.getByText('authored')).not.toBeNull();

    // System tier: locked, always-on, no toggle or uninstall — fork only
    expect(screen.getByTestId('system-skills-section')).not.toBeNull();
    expect(screen.getByTestId('skill-web-research')).not.toBeNull();
    expect(screen.getByText('always on')).not.toBeNull();
    expect(screen.queryByTestId('btn-uninstall-web-research')).toBeNull();
    expect(screen.getByTestId('btn-fork-web-research')).not.toBeNull();
  });

  it('enable/disable drives PATCH and toasts the everywhere effect', async () => {
    vi.spyOn(api.skills, 'list').mockResolvedValue(rows());
    const setEnabled = vi.spyOn(api.skills, 'setEnabled').mockResolvedValue({
      skill: { ...rows().skills[0], enabled: false },
    });
    const onToast = vi.fn();

    render(<SkillsPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('skill-changelog-sweeper')).not.toBeNull();
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Enable changelog-sweeper' }));

    await waitFor(() => {
      expect(setEnabled).toHaveBeenCalledWith('acme', 'changelog-sweeper', false);
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('changelog-sweeper disabled — removed from every agent');
    });
  });

  it('renders an unmet dependency warning chip on the row', async () => {
    const withDeps = rows();
    (withDeps.skills[0] as any).dependency_status = [
      { kind: 'binaries', name: 'pdftotext', status: 'missing', install_hint: 'brew install poppler' },
      { kind: 'tools', name: 'web.search', status: 'missing' },
    ];
    vi.spyOn(api.skills, 'list').mockResolvedValue(withDeps);

    render(<SkillsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('skill-dep-warning-changelog-sweeper')).not.toBeNull();
    });
    expect(screen.getByTestId('skill-dep-warning-changelog-sweeper').textContent).toContain('2 unmet');
  });

  it('uninstalls through a confirm dialog', async () => {
    vi.spyOn(api.skills, 'list').mockResolvedValue(rows());
    const uninstall = vi.spyOn(api.skills, 'uninstall').mockResolvedValue(undefined);
    const onToast = vi.fn();

    render(<SkillsPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-uninstall-changelog-sweeper')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-uninstall-changelog-sweeper'));
    expect(screen.getByTestId('modal-skill-uninstall')).not.toBeNull();
    fireEvent.click(screen.getByTestId('btn-uninstall-confirm'));

    await waitFor(() => {
      expect(uninstall).toHaveBeenCalledWith('acme', 'changelog-sweeper');
    });
    await waitFor(() => {
      expect(screen.queryByTestId('skill-changelog-sweeper')).toBeNull();
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('changelog-sweeper uninstalled');
    });
  });

  it('forks a system skill into the workspace and opens it for editing', async () => {
    vi.spyOn(api.skills, 'list').mockResolvedValue(rows());
    const create = vi.spyOn(api.skills, 'create').mockResolvedValue({
      skill: {
        id: 'sk-forked', workspace_id: 'acme', tier: 'workspace', name: 'web-research',
        version: '2.4.1', source: 'fork', enabled: true, body: '# forked',
        description: 'Research briefs.', created_at: '', updated_at: '',
      },
    });
    const onToast = vi.fn();

    render(<SkillsPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-fork-web-research')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-fork-web-research'));

    await waitFor(() => {
      expect(create).toHaveBeenCalledWith('acme', { source: 'fork', system_skill: 'web-research' });
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('web-research forked to this workspace — opened for editing');
    });
    // The fork lands in the library and opens for editing
    await waitFor(() => {
      expect(screen.getByTestId('modal-skill-edit')).not.toBeNull();
    });
  });

  it('renders the same lists read-only for Members (skills.read only)', async () => {
    vi.spyOn(api.skills, 'list').mockResolvedValue(rows());

    render(<SkillsPane tenant={mockTenant} canWrite={false} />);

    await waitFor(() => {
      expect(screen.getByTestId('skill-changelog-sweeper')).not.toBeNull();
    });
    expect(screen.queryByTestId('btn-skill-add')).toBeNull();
    expect(screen.queryByRole('switch', { name: 'Enable changelog-sweeper' })).toBeNull();
    expect(screen.queryByTestId('btn-edit-changelog-sweeper')).toBeNull();
    expect(screen.queryByTestId('btn-uninstall-changelog-sweeper')).toBeNull();
    expect(screen.queryByTestId('btn-fork-web-research')).toBeNull();
    expect(screen.getByTestId('system-skills-section')).not.toBeNull();
  });

  // ---------------------------------------------------------------------------
  // Two-tab shell (add-skill-curation-from-traces 9.1): the library stays the
  // default view with identical behavior; Curation carries the pending badge.
  // ---------------------------------------------------------------------------

  const curatedCandidate = (overrides: Partial<ApiSkillCandidate> = {}): ApiSkillCandidate => ({
    id: 'cand-1', workspace_id: 'acme', agent_id: 'ag-1', cluster_id: 'cl-1', skill_name: 'deploy-rollout',
    status: 'pending', proposed_content: '# Deploy rollout', is_edit: false,
    evidence_event_ids: [], cited_pattern_refs: [], helpful_count: 0, harmful_count: 0, use_count: 0,
    proposed_at: '', updated_at: '',
    ...overrides,
  });

  it('presents the two-tab shell with the library as the default view', async () => {
    vi.spyOn(api.skills, 'list').mockResolvedValue(rows());
    vi.spyOn(api.curation, 'listCandidates').mockResolvedValue({ candidates: [], count: 0 });

    render(
      <MemoryRouter>
        <SkillsPane tenant={mockTenant} />
      </MemoryRouter>
    );

    // Both tabs exist; the library renders without switching.
    await waitFor(() => {
      expect(screen.getByTestId('skills-tab-library')).not.toBeNull();
    });
    expect(screen.getByTestId('skills-tab-curation')).not.toBeNull();
    expect(screen.getByTestId('skill-changelog-sweeper')).not.toBeNull();
    expect(screen.getByTestId('btn-skill-add')).not.toBeNull();

    // With an empty queue the Curation tab renders no badge.
    fireEvent.click(screen.getByTestId('skills-tab-curation'));
    await waitFor(() => {
      expect(screen.getByTestId('pane-curation')).not.toBeNull();
    });
    expect(screen.queryByTestId('curation-pending-badge')).toBeNull();

    // Back on Skills, the library is byte-identical: same rows, same controls.
    fireEvent.click(screen.getByTestId('skills-tab-library'));
    expect(screen.getByTestId('skill-changelog-sweeper')).not.toBeNull();
    expect(screen.getByTestId('btn-uninstall-changelog-sweeper')).not.toBeNull();
    expect(screen.getByTestId('system-skills-section')).not.toBeNull();
  });

  it('badges the Curation tab with the pending count and clears it when the queue empties', async () => {
    vi.spyOn(api.skills, 'list').mockResolvedValue(rows());
    let pending = 2;
    vi.spyOn(api.curation, 'listCandidates').mockImplementation(async (_ws: string, status?: string) => {
      if (status === 'pending') {
        return {
          candidates: [curatedCandidate(), curatedCandidate({ id: 'cand-2', skill_name: 'log-sweep' })],
          count: pending,
        };
      }
      return { candidates: [curatedCandidate()], count: 1 };
    });
    vi.spyOn(api.curation, 'cycleStatus').mockResolvedValue({
      status: {
        workspace_id: 'acme', state: 'idle', started_at: '', finished_at: '', stages: [],
        counters: {
          clusters_considered: 0, clusters_processed: 0, proposals_drafted: 0, patterns_changed: 0,
          pattern_count: 0, candidates_pending: 2, probation_graduated: 0, probation_disabled: 0,
          qualifying_runs: 0, cluster_runs: 0, approved_curated_skills: 0, rejected_proposals: 0,
        },
      },
    });
    vi.spyOn(api.curation, 'getCandidate').mockResolvedValue({
      candidate: curatedCandidate(),
      evidence: { event_ids: [], cited_patterns: [] },
    });
    const approve = vi.spyOn(api.curation, 'approve').mockResolvedValue({
      candidate: curatedCandidate({ status: 'provisional' }),
    });

    render(
      <MemoryRouter>
        <SkillsPane tenant={mockTenant} />
      </MemoryRouter>
    );

    // Two candidates await review → the badge reads 2.
    await waitFor(() => {
      expect(screen.getByTestId('curation-pending-badge')).not.toBeNull();
    });
    expect(screen.getByTestId('curation-pending-badge').textContent).toBe('2');

    // Approving the row empties the queue → the badge clears.
    fireEvent.click(screen.getByTestId('skills-tab-curation'));
    await waitFor(() => {
      expect(screen.getByTestId('candidate-deploy-rollout')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('candidate-deploy-rollout'));
    await waitFor(() => {
      expect(screen.getByTestId('btn-approve')).not.toBeNull();
    });
    pending = 0;
    fireEvent.click(screen.getByTestId('btn-approve'));

    await waitFor(() => {
      expect(approve).toHaveBeenCalledWith('acme', 'cand-1');
    });
    await waitFor(() => {
      expect(screen.queryByTestId('curation-pending-badge')).toBeNull();
    });
  });
});
