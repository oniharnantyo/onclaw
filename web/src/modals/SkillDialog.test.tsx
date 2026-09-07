import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { SkillInstallWizard, SkillEditDialog } from './SkillDialog';
import { api, type ApiWorkspaceSkill } from '../lib/api';

const systemSkills: ApiWorkspaceSkill[] = [
  {
    id: 'sys-web-research', tier: 'system', name: 'web-research', version: '2.4.1', source: 'system',
    locked: true, enabled: true, description: 'Research briefs.', created_at: '', updated_at: '',
  },
];

const installedSkill: ApiWorkspaceSkill = {
  id: 'sk-sweeper', workspace_id: 'acme', tier: 'workspace', name: 'changelog-sweeper',
  version: '0.1.0', source: 'authored', enabled: true, description: 'Sweeps logs.',
  created_at: '', updated_at: '',
};

describe('modals/SkillInstallWizard', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('authors a skill through source → content → dependency review and installs it', async () => {
    const create = vi.spyOn(api.skills, 'create').mockResolvedValue({ skill: installedSkill });
    const onInstalled = vi.fn();
    const onToast = vi.fn();

    render(
      <SkillInstallWizard
        wsSlug="acme"
        systemSkills={systemSkills}
        existingNames={[]}
        onClose={vi.fn()}
        onInstalled={onInstalled}
        onToast={onToast}
      />
    );

    // Step 1: pick Author
    fireEvent.click(screen.getByTestId('wizard-source-author'));
    fireEvent.click(screen.getByTestId('btn-skill-continue'));

    // Step 2: name + SKILL.md body (structured fields — no raw JSON anywhere)
    await waitFor(() => {
      expect(screen.getByTestId('input-skill-name')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('input-skill-name'), { target: { value: 'Changelog sweeper' } });
    fireEvent.change(screen.getByTestId('input-skill-body'), { target: { value: 'Sweep commit logs.' } });
    fireEvent.click(screen.getByTestId('btn-skill-continue'));

    // Step 3: dependency review (none declared) → install
    await waitFor(() => {
      expect(screen.getByTestId('wizard-review-step')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-skill-install'));

    await waitFor(() => {
      expect(create).toHaveBeenCalledWith('acme', expect.objectContaining({
        source: 'authored',
        name: 'changelog-sweeper',
        body: 'Sweep commit logs.',
        enable_everywhere: true,
        provision_python: true,
      }));
    });
    await waitFor(() => {
      expect(onInstalled).toHaveBeenCalledWith(installedSkill);
    });
    expect(onToast).toHaveBeenCalledWith('changelog-sweeper installed — live on all agents');
  });

  it('shows the error state and installs nothing when an upload has no root SKILL.md', async () => {
    vi.spyOn(api.skills, 'inspectUpload').mockRejectedValue(
      Object.assign(new Error('Archive root has no SKILL.md'), { status: 422, code: 'invalid_request' })
    );
    const createUpload = vi.spyOn(api.skills, 'createUpload');

    render(
      <SkillInstallWizard
        wsSlug="acme"
        systemSkills={systemSkills}
        existingNames={[]}
        onClose={vi.fn()}
        onInstalled={vi.fn()}
        onToast={vi.fn()}
      />
    );

    fireEvent.click(screen.getByTestId('wizard-source-upload'));
    fireEvent.click(screen.getByTestId('btn-skill-continue'));

    const file = new File(['zip'], 'skill.zip', { type: 'application/zip' });
    fireEvent.change(screen.getByTestId('input-skill-archive'), { target: { files: [file] } });

    await waitFor(() => {
      expect(screen.getByTestId('upload-inspect-error')).not.toBeNull();
    });
    expect(screen.getByTestId('upload-inspect-error').textContent).toContain('SKILL.md');
    expect(createUpload).not.toHaveBeenCalled();
  });

  it('review step reports missing binaries with hint, copy, and re-check; installs with warning', async () => {
    vi.spyOn(api.skills, 'inspectUpload').mockResolvedValue({
      skill: {
        name: 'pdf-sweep',
        dependencies: { binaries: ['pdftotext'] },
        dependency_status: [
          { kind: 'binaries', name: 'pdftotext', status: 'missing', install_hint: 'brew install poppler' },
        ],
      },
      files: ['SKILL.md', 'scripts/sweep.py'],
    });
    const createUpload = vi.spyOn(api.skills, 'createUpload').mockResolvedValue({
      skill: { ...installedSkill, name: 'pdf-sweep' },
      dependency_status: [{ kind: 'binaries', name: 'pdftotext', status: 'missing' }],
    });
    const onToast = vi.fn();

    render(
      <SkillInstallWizard
        wsSlug="acme"
        systemSkills={systemSkills}
        existingNames={[]}
        onClose={vi.fn()}
        onInstalled={vi.fn()}
        onToast={onToast}
      />
    );

    fireEvent.click(screen.getByTestId('wizard-source-upload'));
    fireEvent.click(screen.getByTestId('btn-skill-continue'));

    const file = new File(['zip'], 'pdf-sweep.zip', { type: 'application/zip' });
    fireEvent.change(screen.getByTestId('input-skill-archive'), { target: { files: [file] } });

    // Tree preview renders before install
    await waitFor(() => {
      expect(screen.getByTestId('upload-tree-preview')).not.toBeNull();
    });
    expect(screen.getByText('scripts/sweep.py')).not.toBeNull();
    fireEvent.click(screen.getByTestId('btn-skill-continue'));

    // Dependency review: binary unmet, hint shown, copy + re-check available
    await waitFor(() => {
      expect(screen.getByTestId('dep-binaries-pdftotext')).not.toBeNull();
    });
    expect(screen.getByText('brew install poppler')).not.toBeNull();
    expect(screen.getByTestId('btn-copy-pdftotext')).not.toBeNull();
    expect(screen.getByTestId('btn-dep-recheck')).not.toBeNull();

    // Enable-everywhere is pre-checked for tool deps
    expect((screen.getByTestId('dep-enable-everywhere').querySelector('input') as HTMLInputElement).checked).toBe(true);

    fireEvent.click(screen.getByTestId('btn-skill-install'));

    await waitFor(() => {
      expect(createUpload).toHaveBeenCalled();
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('pdf-sweep installed with 1 unmet dependency — live on all agents');
    });
  });

  it('offers discovered-skill multi-select for a git URL', async () => {
    vi.spyOn(api.skills, 'inspectGit').mockResolvedValue({
      skills: [
        { name: 'alpha', description: 'First skill' },
        { name: 'beta', description: 'Second skill' },
        { name: 'gamma', description: 'Third skill' },
      ],
    });
    const create = vi.spyOn(api.skills, 'create').mockResolvedValue({ skill: installedSkill });

    render(
      <SkillInstallWizard
        wsSlug="acme"
        systemSkills={systemSkills}
        existingNames={[]}
        onClose={vi.fn()}
        onInstalled={vi.fn()}
        onToast={vi.fn()}
      />
    );

    fireEvent.click(screen.getByTestId('wizard-source-git'));
    fireEvent.click(screen.getByTestId('btn-skill-continue'));

    fireEvent.change(screen.getByTestId('input-skill-git-url'), { target: { value: 'https://github.com/acme/skills' } });
    fireEvent.click(screen.getByTestId('btn-skill-discover'));

    await waitFor(() => {
      expect(screen.getByTestId('git-skill-list')).not.toBeNull();
    });
    // All three discovered skills list with descriptions; uncheck one
    expect(screen.getByText('First skill')).not.toBeNull();
    expect(screen.getByTestId('git-skill-gamma')).not.toBeNull();
    fireEvent.click(screen.getByTestId('git-skill-gamma'));

    fireEvent.click(screen.getByTestId('btn-skill-continue'));
    await waitFor(() => {
      expect(screen.getByTestId('wizard-review-step')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-skill-install'));

    await waitFor(() => {
      expect(create).toHaveBeenCalledWith('acme', expect.objectContaining({
        source: 'git',
        url: 'https://github.com/acme/skills',
        names: ['alpha', 'beta'],
      }));
    });
  });

  it('offers overwrite-with-confirm on a same-name conflict', async () => {
    let call = 0;
    const create = vi
      .spyOn(api.skills, 'create')
      .mockImplementation(async (_ws: string, body: any) => {
        call += 1;
        if (call === 1) {
          throw Object.assign(new Error('A skill named changelog-sweeper already exists'), {
            status: 409,
            code: 'conflict',
          });
        }
        return { skill: installedSkill, ...(body.overwrite ? {} : {}) };
      });

    render(
      <SkillInstallWizard
        wsSlug="acme"
        systemSkills={systemSkills}
        existingNames={['changelog-sweeper']}
        onClose={vi.fn()}
        onInstalled={vi.fn()}
        onToast={vi.fn()}
      />
    );

    fireEvent.click(screen.getByTestId('wizard-source-author'));
    fireEvent.click(screen.getByTestId('btn-skill-continue'));
    fireEvent.change(screen.getByTestId('input-skill-name'), { target: { value: 'Changelog sweeper' } });
    fireEvent.change(screen.getByTestId('input-skill-body'), { target: { value: 'Sweep logs.' } });
    fireEvent.click(screen.getByTestId('btn-skill-continue'));
    fireEvent.click(screen.getByTestId('btn-skill-install'));

    await waitFor(() => {
      expect(screen.getByTestId('skill-conflict')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-skill-overwrite'));

    await waitFor(() => {
      expect(create).toHaveBeenCalledTimes(2);
    });
    expect((create as any).mock.calls[1][1].overwrite).toBe(true);
  });
});

describe('modals/SkillEditDialog', () => {
  it('saves description and body via PUT', async () => {
    const update = vi.spyOn(api.skills, 'update').mockResolvedValue({
      skill: { ...installedSkill, description: 'Updated description' },
    });
    const onSaved = vi.fn();
    const onToast = vi.fn();

    render(
      <SkillEditDialog
        skill={{ ...installedSkill, body: 'Original body' }}
        wsSlug="acme"
        onClose={vi.fn()}
        onSaved={onSaved}
        onToast={onToast}
      />
    );

    fireEvent.change(screen.getByTestId('input-skill-edit-desc'), { target: { value: 'Updated description' } });
    fireEvent.change(screen.getByTestId('input-skill-edit-body'), { target: { value: 'New body' } });
    fireEvent.click(screen.getByTestId('btn-skill-save'));

    await waitFor(() => {
      expect(update).toHaveBeenCalledWith('acme', 'changelog-sweeper', {
        description: 'Updated description',
        body: 'New body',
      });
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('changelog-sweeper updated');
    });
    expect(onSaved).toHaveBeenCalled();
  });
});
