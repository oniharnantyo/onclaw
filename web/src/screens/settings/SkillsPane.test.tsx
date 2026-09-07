import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { SkillsPane } from './SkillsPane';
import { api, type ApiWorkspaceSkill } from '../../lib/api';

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
});
