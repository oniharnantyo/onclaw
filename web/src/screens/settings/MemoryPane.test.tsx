import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryPane } from './MemoryPane';
import { api, type ApiMemoryNoteList, type ApiMemoryNote } from '../../lib/api';

const mockTenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

const doc = (content: string) => ({ content, max_chars: 32000, updated_at: '2026-09-16T00:00:00Z' });

const noteRow = (overrides: Partial<ApiMemoryNote> = {}): ApiMemoryNote => ({
  id: 'note-1',
  workspace_id: 'ws-1',
  visibility: 'user',
  user_id: 'u1',
  agent_id: null,
  origin: 'dialogue',
  event_time: '2026-09-16T01:00:00Z',
  learned_at: '2026-09-16T01:00:05Z',
  source_event_id: 'abc12345-6789-0abc-def0-1234567890ab',
  content: 'Member prefers Go for tooling.',
  importance: 5,
  pinned: false,
  topic: 'tooling',
  conflict_flag: null,
  supersedes: null,
  superseded_by: null,
  promoted_by: null,
  promoted_at: null,
  tombstoned_at: null,
  ...overrides,
});

const notesPage = (notes: ApiMemoryNote[]): ApiMemoryNoteList => ({
  notes,
  counts: { shared: 1, user: 2, agent: 0 },
  viewer_user_id: 'u1',
});

const defaultSettings = {
  settings: {
    visibility_posture: 'narrow' as const,
    ingestion_enabled: true,
    gate_budget_ms: 4000,
    side_call_model: null,
    embedding: null,
  },
};

function mockBase(overrides: {
  notes?: ApiMemoryNoteList;
  settings?: { settings: any };
  providers?: { providers: any[] };
  models?: any;
} = {}) {
  vi.spyOn(api.memory, 'notes').mockResolvedValue(
    overrides.notes || notesPage([noteRow()])
  );
  vi.spyOn(api.memory, 'report').mockResolvedValue({
    report: {
      generated_at: '2026-09-16T02:00:00Z',
      conflicts: [{ note_id: 'note-1', document: 'WORKSPACE.md', excerpt: 'Uses Fridays', flagged_at: '2026-09-16T02:00:00Z' }],
      merges: [{ canonical_id: 'note-1', merged_ids: ['note-2', 'note-3'] }],
      extraction_failures: 2,
    },
  });
  vi.spyOn(api.memory, 'getSettings').mockResolvedValue(overrides.settings || defaultSettings);
  vi.spyOn(api.providers, 'list').mockResolvedValue(
    overrides.providers !== undefined ? overrides.providers : { providers: [{ id: 'prov-1', name: 'OpenAI', type: 'openai' }] } as any
  );
  if (overrides.models !== undefined) {
    vi.spyOn(api.providers, 'models').mockResolvedValue(overrides.models);
  }
}

function switchTab(testid: string) {
  fireEvent.click(screen.getByTestId(testid));
}

describe('screens/settings/MemoryPane', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('opens on Configuration; the facts browser and morning report stay a tab away', async () => {
    const getMine = vi.spyOn(api.memory, 'getMine');
    const getWorkspace = vi.spyOn(api.memory, 'getWorkspace');
    mockBase();
    render(<MemoryPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('pane-memory')).not.toBeNull();
    });
    // The precedence rule is stated in copy (design D7 risk mitigation).
    expect(screen.getByTestId('memory-precedence-copy').textContent).toContain('manually');
    expect(screen.getByTestId('memory-precedence-copy').textContent).toContain('never edits the documents');

    // The pane never touches the documents — their editors live in their own
    // homes (user menu "My memory", Workspace settings).
    expect(getMine).not.toHaveBeenCalled();
    expect(getWorkspace).not.toHaveBeenCalled();
    expect(screen.queryByTestId('memory-user-doc')).toBeNull();
    expect(screen.queryByTestId('memory-workspace-doc')).toBeNull();

    // Default tab: Configuration is in view without any interaction.
    expect(screen.getByTestId('memory-configuration')).not.toBeNull();
    expect(screen.queryByTestId('memory-notes-browser')).toBeNull();
    expect(screen.queryByTestId('memory-report-counts')).toBeNull();

    // Facts is a tab away: the notes browser shows the fact with chip + provenance.
    switchTab('memory-tab-facts');
    expect(screen.getByTestId('memory-notes-browser')).not.toBeNull();
    expect(screen.getByTestId('memory-note-note-1').textContent).toContain('Member prefers Go for tooling.');
    expect(screen.getByTestId('memory-visibility-user')).not.toBeNull();
    expect(screen.getByTestId('memory-note-note-1').textContent).toContain('dialogue');
    expect(screen.getByTestId('memory-note-note-1').textContent).toContain('abc12345');

    // Morning report is its own tab and surfaces conflicts/merges/failures.
    switchTab('memory-tab-report');
    expect(screen.getByTestId('memory-report-counts').textContent).toContain('1 conflict');
    expect(screen.getByTestId('memory-report-counts').textContent).toContain('1 merge');
    expect(screen.getByTestId('memory-report-counts').textContent).toContain('2 extraction failures');
    expect(screen.queryByTestId('memory-notes-browser')).toBeNull();
  });

  it('promotes a note through the confirm modal', async () => {
    const promoted = noteRow({ visibility: 'shared', user_id: null, promoted_by: 'u1', promoted_at: '2026-09-16T03:00:00Z' });
    mockBase();
    const promote = vi.spyOn(api.memory, 'promoteNote').mockResolvedValue({ note: promoted });
    const onToast = vi.fn();
    render(<MemoryPane tenant={mockTenant} canWrite onToast={onToast} />);

    // Tabs render only after the load settles.
    await waitFor(() => {
      expect(screen.getByTestId('memory-tab-facts')).not.toBeNull();
    });
    switchTab('memory-tab-facts');
    await waitFor(() => {
      expect(screen.getByTestId('btn-memory-promote-note-1')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-memory-promote-note-1'));

    // Confirm modal states the audited widening.
    await waitFor(() => {
      expect(screen.getByTestId('modal-memory-promote')).not.toBeNull();
    });
    expect(screen.getByTestId('modal-memory-promote').textContent).toContain('recorded');
    fireEvent.click(screen.getByTestId('btn-memory-promote-confirm'));

    await waitFor(() => {
      expect(promote).toHaveBeenCalledWith('acme', 'note-1');
    });
    await waitFor(() => {
      expect(screen.getByTestId('memory-visibility-shared')).not.toBeNull();
    });
  });

  it('deletes a fact through the tombstone confirm modal', async () => {
    mockBase();
    const del = vi.spyOn(api.memory, 'deleteNote').mockResolvedValue(undefined);
    const onToast = vi.fn();
    render(<MemoryPane tenant={mockTenant} canWrite onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('memory-tab-facts')).not.toBeNull();
    });
    switchTab('memory-tab-facts');
    await waitFor(() => {
      expect(screen.getByTestId('btn-memory-delete-note-1')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-memory-delete-note-1'));
    await waitFor(() => {
      expect(screen.getByTestId('modal-memory-delete')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-memory-delete-confirm'));

    await waitFor(() => {
      expect(del).toHaveBeenCalledWith('acme', 'note-1');
    });
    await waitFor(() => {
      expect(screen.queryByTestId('memory-note-note-1')).toBeNull();
    });
  });

  it('saves settings: posture, ingestion, and the provider-pinned embedding model', async () => {
    mockBase();
    const save = vi.spyOn(api.memory, 'updateSettings').mockResolvedValue({
      settings: {
        visibility_posture: 'org-shared',
        ingestion_enabled: false,
        gate_budget_ms: 4000,
        side_call_model: null,
        embedding: {
          provider_id: 'prov-1',
          model: 'text-embedding-3-small',
          dimension: 1536,
        },
      },
    });
    const onToast = vi.fn();
    render(<MemoryPane tenant={mockTenant} canWrite onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('pane-memory')).not.toBeNull();
    });
    switchTab('memory-tab-config');
    await waitFor(() => {
      expect(screen.getByTestId('memory-embedding-provider')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('memory-posture-org-shared'));
    fireEvent.click(screen.getByRole('switch', { name: 'Enable memory ingestion' }));
    // The endpoint and credential live on the workspace provider — the pane
    // pins provider + model only, and the provider select offers no
    // "no provider" escape.
    expect(screen.queryByTestId('memory-embedding-endpoint')).toBeNull();
    expect(screen.queryByTestId('memory-embedding-key')).toBeNull();
    expect((screen.getByTestId('memory-embedding-provider') as HTMLSelectElement).options[0].value).toBe('');
    expect((screen.getByTestId('memory-embedding-provider') as HTMLSelectElement).options[0].textContent).not.toContain('No provider');
    fireEvent.change(screen.getByTestId('memory-embedding-provider'), { target: { value: 'prov-1' } });
    fireEvent.change(screen.getByTestId('memory-embedding-model'), { target: { value: 'text-embedding-3-small' } });
    fireEvent.click(screen.getByTestId('btn-memory-save-settings'));

    await waitFor(() => {
      expect(save).toHaveBeenCalledWith('acme', {
        visibility_posture: 'org-shared',
        ingestion_enabled: false,
        side_call_model: null,
        embedding: {
          provider_id: 'prov-1',
          model: 'text-embedding-3-small',
        },
      });
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Memory settings saved');
    });
  });

  it('memory side-call model: agent default sends null; specific pins a catalog model', async () => {
    // The side-call model rides the catalog-backed ModelCombobox: picking the
    // provider resolves its catalog and auto-selects the first model.
    mockBase({
      models: {
        source: 'live',
        models: [{ id: 'glm-4.5-air', name: 'GLM 4.5 Air' }],
      },
    });
    const save = vi.spyOn(api.memory, 'updateSettings').mockResolvedValue({ settings: defaultSettings.settings });
    const onToast = vi.fn();
    render(<MemoryPane tenant={mockTenant} canWrite onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('pane-memory')).not.toBeNull();
    });
    switchTab('memory-tab-config');

    // Default is agent default; saving sends the explicit clear. The saved
    // toast fires after the response sync, so waiting for it bars the next
    // scenario from racing save #1's state reset.
    expect(screen.getByTestId('memory-sidecall-agent_default').getAttribute('aria-pressed')).toBe('true');
    fireEvent.click(screen.getByTestId('btn-memory-save-settings'));
    await waitFor(() => {
      expect(save).toHaveBeenLastCalledWith('acme', expect.objectContaining({ side_call_model: null }));
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Memory settings saved');
    });

    // Pinning a specific provider resolves the catalog and pins the model.
    fireEvent.click(screen.getByTestId('memory-sidecall-specific'));
    fireEvent.change(screen.getByTestId('memory-sidecall-provider'), { target: { value: 'prov-1' } });
    // Wait on the combobox TRIGGER (not the container — its option list
    // renders before the auto-selected model commits to the pane state the
    // save reads).
    await waitFor(() => {
      expect(screen.getByTestId('select-model-trigger').textContent).toContain('GLM 4.5 Air');
    });
    fireEvent.click(screen.getByTestId('btn-memory-save-settings'));
    await waitFor(() => {
      expect(save).toHaveBeenLastCalledWith('acme', expect.objectContaining({
        side_call_model: { provider_id: 'prov-1', model: 'glm-4.5-air' },
      }));
    });
  });

  it('falls back to free-text model entry when no embedding-classified models resolve', async () => {
    mockBase({
      models: {
        source: 'catalog',
        models: [
          { id: 'gpt-4o', name: 'GPT-4o' },
          { id: 'weird-custom-model', name: 'Weird' },
        ],
      },
    });
    render(<MemoryPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('pane-memory')).not.toBeNull();
    });
    switchTab('memory-tab-config');
    await waitFor(() => {
      expect(screen.getByTestId('memory-embedding-provider')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('memory-embedding-provider'), { target: { value: 'prov-1' } });

    // Unknown gateway models → free-text entry, never a raw-JSON textarea.
    await waitFor(() => {
      const input = screen.getByTestId('memory-embedding-model') as HTMLInputElement;
      expect(input.tagName).toBe('INPUT');
    });
    expect(screen.getByTestId('memory-configuration').textContent).toContain(
      'connection test discovers its dimension'
    );
  });

  it('offers the embedding dropdown when the catalog classifies embedding models (tri-state)', async () => {
    mockBase({
      models: {
        source: 'catalog',
        models: [
          { id: 'gpt-4o', name: 'GPT-4o' },
          { id: 'text-embedding-3-small', name: 'text-embedding-3-small' },
          // Regression: the suffix-embed families (arctic-embed-l, embed-qa-4)
          // used to classify "unknown" and the dropdown never appeared.
          { id: 'snowflake/arctic-embed-l', name: 'snowflake/arctic-embed-l' },
          { id: 'nvidia/embed-qa-4', name: 'nvidia/embed-qa-4' },
        ],
      },
    });
    render(<MemoryPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('pane-memory')).not.toBeNull();
    });
    switchTab('memory-tab-config');
    await waitFor(() => {
      expect(screen.getByTestId('memory-embedding-provider')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('memory-embedding-provider'), { target: { value: 'prov-1' } });

    await waitFor(() => {
      const select = screen.getByTestId('memory-embedding-model') as HTMLSelectElement;
      expect(select.tagName).toBe('SELECT');
      expect(select.textContent).toContain('text-embedding-3-small');
    });
  });

  it('dimension dropdown offers Auto plus the supported set; a known model preselects its dimension', async () => {
    mockBase({
      models: {
        source: 'catalog',
        models: [
          { id: 'text-embedding-3-small', name: 'text-embedding-3-small' },
          { id: 'weird-custom-embed', name: 'Weird' },
        ],
      },
    });
    render(<MemoryPane tenant={mockTenant} canWrite />);

    // Configuration is the default tab — no interaction to reach it.
    await waitFor(() => {
      expect(screen.getByTestId('memory-configuration')).not.toBeNull();
    });
    await waitFor(() => {
      expect(screen.getByTestId('memory-embedding-provider')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('memory-embedding-provider'), { target: { value: 'prov-1' } });
    await waitFor(() => {
      expect((screen.getByTestId('memory-embedding-model') as HTMLSelectElement).tagName).toBe('SELECT');
    });

    // Exactly "Auto — detect on test" plus OnClaw's five supported dimensions.
    const dimSel = () => screen.getByTestId('memory-embedding-dimension') as HTMLSelectElement;
    expect(Array.from(dimSel().options).map((o) => o.value)).toEqual(['', '768', '1024', '1536', '2048', '3072']);
    expect(dimSel().options[0].textContent).toBe('Auto — detect on test');
    expect(dimSel().value).toBe('');

    // A model the map knows preselects its dimension.
    fireEvent.change(screen.getByTestId('memory-embedding-model'), { target: { value: 'text-embedding-3-small' } });
    expect(dimSel().value).toBe('1536');

    // An unknown model leaves the standing dimension alone.
    fireEvent.change(screen.getByTestId('memory-embedding-model'), { target: { value: 'weird-custom-embed' } });
    expect(dimSel().value).toBe('1536');

    // An explicit choice is never clobbered by a later known model.
    fireEvent.change(dimSel(), { target: { value: '768' } });
    fireEvent.change(screen.getByTestId('memory-embedding-model'), { target: { value: 'text-embedding-3-large' } });
    expect(dimSel().value).toBe('768');
  });

  it('connection test rides the chosen provider: dimension filled on success, structured error on failure', async () => {
    mockBase();
    const test = vi
      .spyOn(api.memory, 'testSettings')
      .mockResolvedValueOnce({ ok: true, dimension: 1536 })
      .mockRejectedValueOnce(new Error('embedding endpoint returned HTTP 401: bad key'));
    render(<MemoryPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('pane-memory')).not.toBeNull();
    });
    switchTab('memory-tab-config');
    await waitFor(() => {
      expect(screen.getByTestId('btn-memory-test')).not.toBeNull();
    });
    // Testing without a provider is blocked client-side: endpoint and
    // credential come from the provider.
    fireEvent.click(screen.getByTestId('btn-memory-test'));
    await waitFor(() => {
      expect(test).not.toHaveBeenCalled();
    });
    fireEvent.change(screen.getByTestId('memory-embedding-provider'), { target: { value: 'prov-1' } });
    fireEvent.change(screen.getByTestId('memory-embedding-model'), { target: { value: 'text-embedding-3-small' } });

    fireEvent.click(screen.getByTestId('btn-memory-test'));
    await waitFor(() => {
      expect(test).toHaveBeenCalledWith('acme', {
        provider_id: 'prov-1',
        model: 'text-embedding-3-small',
      });
    });
    await waitFor(() => {
      expect(screen.getByTestId('memory-test-result').textContent).toContain('dimension 1536');
    });
    // The dimension was left on Auto — the discovered value fills the dropdown.
    expect((screen.getByTestId('memory-embedding-dimension') as HTMLSelectElement).value).toBe('1536');

    fireEvent.click(screen.getByTestId('btn-memory-test'));
    await waitFor(() => {
      expect(screen.getByTestId('memory-test-error').textContent).toContain('HTTP 401');
    });
  });

  it('consolidate-now posts and refreshes the report', async () => {
    mockBase();
    const consolidate = vi.spyOn(api.memory, 'consolidate').mockResolvedValue({
      report: { generated_at: '2026-09-16T05:00:00Z', conflicts: [], merges: [], extraction_failures: 0 },
    });
    const onToast = vi.fn();
    render(<MemoryPane tenant={mockTenant} canWrite onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('pane-memory')).not.toBeNull();
    });
    switchTab('memory-tab-report');
    await waitFor(() => {
      expect(screen.getByTestId('btn-memory-consolidate')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-memory-consolidate'));

    await waitFor(() => {
      expect(consolidate).toHaveBeenCalledWith('acme');
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Consolidation finished — report refreshed');
    });
  });

  it('members see read-only surfaces: no promote/delete buttons, no configuration controls', async () => {
    mockBase();
    // The membership-derived check can't see a member role in this offline
    // test env, so the member path rides the pane's canWrite override.
    render(<MemoryPane tenant={mockTenant} canWrite={false} />);

    await waitFor(() => {
      expect(screen.getByTestId('memory-tab-facts')).not.toBeNull();
    });
    switchTab('memory-tab-facts');
    await waitFor(() => {
      expect(screen.getByTestId('memory-note-note-1')).not.toBeNull();
    });
    expect(screen.queryByTestId('btn-memory-promote-note-1')).toBeNull();
    expect(screen.queryByTestId('btn-memory-delete-note-1')).toBeNull();
    expect(screen.queryByTestId('btn-memory-consolidate')).toBeNull();
    switchTab('memory-tab-config');
    expect(screen.getByTestId('memory-configuration').textContent).toContain(
      'Only Owner and Admin can change memory configuration'
    );
  });
});
