import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryPane } from './MemoryPane';
import { api, type ApiMemoryNoteList, type ApiMemoryNote } from '../../lib/api';
import { useAuthStore } from '../../store/auth';

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

  // ------------------------------------------------------------- Decision
  // backend (add-configurable-decision-backend 5.4): unchecked stores
  // nothing; checked reveals the typesafe-only pair; omission clears.
  const decisionProviders = [
    { id: 'prov-1', name: 'OpenAI', type: 'openai' },
    { id: 'ts-1', name: 'TypeSafe Prod', type: 'typesafe' },
  ];

  function decisionSwitch() {
    return screen.getByRole('switch', { name: 'Use decision backend' }) as HTMLButtonElement;
  }

  it('decision backend defaults off: checkbox unchecked, no decision fields render', async () => {
    mockBase();
    render(<MemoryPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('memory-configuration')).not.toBeNull();
    });
    expect(decisionSwitch().getAttribute('aria-checked')).toBe('false');
    expect(screen.queryByTestId('memory-decision-fields')).toBeNull();
    expect(screen.queryByTestId('memory-decision-provider')).toBeNull();
    expect(screen.queryByTestId('memory-decision-model')).toBeNull();
  });

  it('checking reveals a typesafe-only provider select and a model prefilled jev-latest', async () => {
    mockBase({ providers: { providers: decisionProviders } as any });
    render(<MemoryPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('memory-configuration')).not.toBeNull();
    });
    fireEvent.click(decisionSwitch());

    const fields = screen.getByTestId('memory-decision-fields');
    expect(fields).not.toBeNull();
    const select = screen.getByTestId('memory-decision-provider') as HTMLSelectElement;
    // Decision configs only — the mocked openai config must not appear.
    expect(Array.from(select.options).map((o) => o.textContent)).toEqual([
      'Select a provider…',
      'TypeSafe Prod',
    ]);
    const model = screen.getByTestId('memory-decision-model') as HTMLInputElement;
    expect(model.value).toBe('jev-latest');
  });

  it('saving with the box checked but no provider selected is blocked inline', async () => {
    mockBase({ providers: { providers: decisionProviders } as any });
    const save = vi.spyOn(api.memory, 'updateSettings').mockResolvedValue({ settings: defaultSettings.settings });
    const onToast = vi.fn();
    render(<MemoryPane tenant={mockTenant} canWrite onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('memory-configuration')).not.toBeNull();
    });
    fireEvent.click(decisionSwitch());
    fireEvent.click(screen.getByTestId('btn-memory-save-settings'));

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith(
        expect.stringContaining('decision backend'),
        'danger'
      );
    });
    expect(save).not.toHaveBeenCalled();
  });

  it('a checked save sends both decision keys; unticking a stored pair and saving omits both (the clear)', async () => {
    mockBase({
      providers: { providers: decisionProviders } as any,
      settings: {
        settings: {
          ...defaultSettings.settings,
          decision_provider_id: 'ts-1',
          decision_model: 'jev-latest',
        },
      },
    });
    const storedPair = {
      settings: {
        ...defaultSettings.settings,
        decision_provider_id: 'ts-1',
        decision_model: 'jev-latest',
      },
    };
    // The echo keeps the pair stored — a still-set response leaves the box
    // checked after save #1, so the untick below is a real untick.
    const save = vi.spyOn(api.memory, 'updateSettings').mockResolvedValue(storedPair);
    const onToast = vi.fn();
    render(<MemoryPane tenant={mockTenant} canWrite onToast={onToast} />);

    // A stored pair opens with the box checked and the fields populated.
    await waitFor(() => {
      expect(decisionSwitch().getAttribute('aria-checked')).toBe('true');
    });

    // Saving checked: both flat keys ride the save.
    fireEvent.click(screen.getByTestId('btn-memory-save-settings'));
    await waitFor(() => {
      expect(save).toHaveBeenLastCalledWith(
        'acme',
        expect.objectContaining({ decision_provider_id: 'ts-1', decision_model: 'jev-latest' })
      );
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Memory settings saved');
    });

    // Unticked: the body omits BOTH keys — omission is the server's clear.
    fireEvent.click(decisionSwitch());
    fireEvent.click(screen.getByTestId('btn-memory-save-settings'));
    await waitFor(() => {
      expect(save).toHaveBeenCalledTimes(2);
    });
    const body = save.mock.lastCall![1] as Record<string, unknown>;
    expect(body).not.toHaveProperty('decision_provider_id');
    expect(body).not.toHaveProperty('decision_model');
    // Unrelated keys ride untouched.
    expect(body.visibility_posture).toBe('narrow');
    expect(body.ingestion_enabled).toBe(true);
  });

  it('a stored pair opens with the box checked and the fields populated', async () => {
    mockBase({
      providers: { providers: decisionProviders } as any,
      settings: {
        settings: {
          ...defaultSettings.settings,
          decision_provider_id: 'ts-1',
          decision_model: 'jev-latest',
        },
      },
    });
    render(<MemoryPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('memory-configuration')).not.toBeNull();
    });
    expect(decisionSwitch().getAttribute('aria-checked')).toBe('true');
    expect((screen.getByTestId('memory-decision-provider') as HTMLSelectElement).value).toBe('ts-1');
    expect((screen.getByTestId('memory-decision-model') as HTMLInputElement).value).toBe('jev-latest');
  });

  it('model pickers (side-call and embedding) never list a decision config', async () => {
    mockBase({
      providers: { providers: decisionProviders } as any,
      models: { source: 'live', models: [{ id: 'glm-4.5-air', name: 'GLM 4.5 Air' }] },
    });
    render(<MemoryPane tenant={mockTenant} canWrite />);

    await waitFor(() => {
      expect(screen.getByTestId('memory-configuration')).not.toBeNull();
    });

    // Embedding select: language providers only.
    const embeddingOptions = Array.from(
      (screen.getByTestId('memory-embedding-provider') as HTMLSelectElement).options
    ).map((o) => o.textContent);
    expect(embeddingOptions).toContain('OpenAI');
    expect(embeddingOptions).not.toContain('TypeSafe Prod');

    // Side-call select (specific mode): language providers only.
    fireEvent.click(screen.getByTestId('memory-sidecall-specific'));
    const sidecallOptions = Array.from(
      (screen.getByTestId('memory-sidecall-provider') as HTMLSelectElement).options
    ).map((o) => o.textContent);
    expect(sidecallOptions).toContain('OpenAI');
    expect(sidecallOptions).not.toContain('TypeSafe Prod');
  });
});

// fix-role-permission-audit 5.1: the pane derives its gate from
// workspace.write — no longer the tools.write check it shared before the audit.
describe('screens/settings/MemoryPane — derived workspace.write gate', () => {
  const authWith = (permissions: string[]) => {
    useAuthStore.setState({
      user: { id: 'u1', email: 'u1@acme.dev', name: 'U One', created_at: '', updated_at: '' },
      memberships: [
        {
          workspace_id: 'acme',
          workspace_slug: 'acme',
          role_name: 'Custom',
          role: { name: 'Custom', is_owner: false, permissions },
        },
      ] as any,
      status: 'authenticated',
    });
  };

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('a role holding tools.write but not workspace.write sees the surfaces read-only', async () => {
    mockBase();
    authWith(['tools.write', 'workspace.read']);

    render(<MemoryPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('memory-tab-facts')).not.toBeNull();
    });
    switchTab('memory-tab-facts');
    await waitFor(() => {
      expect(screen.getByTestId('memory-note-note-1')).not.toBeNull();
    });
    expect(screen.queryByTestId('btn-memory-promote-note-1')).toBeNull();
    expect(screen.queryByTestId('btn-memory-delete-note-1')).toBeNull();
    switchTab('memory-tab-config');
    expect(screen.getByTestId('memory-configuration').textContent).toContain(
      'Only Owner and Admin can change memory configuration'
    );
  });

  it('a role holding workspace.write but not tools.write gets the mutation affordances', async () => {
    mockBase();
    authWith(['workspace.write']);

    render(<MemoryPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('memory-tab-facts')).not.toBeNull();
    });
    switchTab('memory-tab-facts');
    await waitFor(() => {
      expect(screen.getByTestId('btn-memory-promote-note-1')).not.toBeNull();
    });
    expect(screen.getByTestId('btn-memory-delete-note-1')).not.toBeNull();
  });
});
