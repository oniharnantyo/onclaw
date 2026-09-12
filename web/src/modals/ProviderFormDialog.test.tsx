import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ProviderFormDialog } from './ProviderFormDialog';
import { api, type ApiProviderConfig } from '../lib/api';

const compatibleProvider = (over: Partial<ApiProviderConfig> = {}): ApiProviderConfig => ({
  id: 'prov-gw',
  workspace_id: 'acme',
  type: 'openai-compatible',
  name: 'Z.ai Gateway',
  base_url: 'https://api.example.com/v1',
  key_set: true,
  key_hint: '····abcd',
  enabled: true,
  created_at: '',
  updated_at: '',
  ...over,
});

describe('modals/ProviderFormDialog — catalog mapping (fix-image-attachment-lane 3.4)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  function renderDialog(props: Partial<Parameters<typeof ProviderFormDialog>[0]> = {}) {
    const onClose = vi.fn();
    const onSaved = vi.fn();
    const onToast = vi.fn();
    render(
      <ProviderFormDialog
        workspaceId="acme"
        provider={null}
        onClose={onClose}
        onSaved={onSaved}
        onToast={onToast}
        {...props}
      />
    );
    return { onClose, onSaved, onToast };
  }

  const catalogSelect = () => screen.getByLabelText('Catalog mapping') as HTMLSelectElement;

  it('hides the select entirely for the four canonical provider types and shows it for compatible ones', () => {
    renderDialog();

    for (const canonical of ['openai', 'anthropic', 'gemini', 'openrouter']) {
      fireEvent.change(screen.getByLabelText('Provider type'), { target: { value: canonical } });
      expect(screen.queryByLabelText('Catalog mapping')).toBeNull();
    }

    fireEvent.change(screen.getByLabelText('Provider type'), { target: { value: 'openai-compatible' } });
    expect(catalogSelect()).not.toBeNull();

    fireEvent.change(screen.getByLabelText('Provider type'), { target: { value: 'anthropic-compatible' } });
    expect(catalogSelect()).not.toBeNull();
  });

  it('offers auto-detect plus curated real catalog ids, defaulting to auto', () => {
    renderDialog();
    fireEvent.change(screen.getByLabelText('Provider type'), { target: { value: 'openai-compatible' } });

    const values = Array.from(catalogSelect().options).map((o) => o.value);
    // Every id verified against the community catalog cache (models.dev.json).
    expect(values).toEqual([
      '',
      'zai-coding-plan',
      'zhipuai-coding-plan',
      'zhipuai',
      'openrouter',
      'deepseek',
      'mistral',
      'groq',
      'fireworks-ai',
    ]);
    expect(catalogSelect().value).toBe('');
    expect(screen.getByText('Auto-detect from host')).not.toBeNull();
  });

  it('persists an explicit selection and an empty string for untouched auto', async () => {
    const createSpy = vi.spyOn(api.providers, 'create').mockResolvedValue({
      provider: compatibleProvider(),
    });
    renderDialog();
    fireEvent.change(screen.getByLabelText('Provider type'), { target: { value: 'openai-compatible' } });
    // Compatible gateways require a base URL before the form validates.
    fireEvent.change(screen.getByLabelText('Base URL'), { target: { value: 'https://api.example.com/v1' } });
    fireEvent.change(screen.getByLabelText('Provider name'), { target: { value: 'Gateway' } });

    // Untouched → auto semantics persist EMPTY catalog_provider (host
    // auto-detect keeps working server-side).
    fireEvent.click(screen.getByTestId('btn-provider-create-confirm'));
    await waitFor(() => {
      expect(createSpy).toHaveBeenCalledWith('acme', expect.objectContaining({ catalog_provider: '' }));
    });

    // Explicit selection persists that id.
    createSpy.mockClear();
    fireEvent.change(screen.getByLabelText('Provider type'), { target: { value: 'anthropic-compatible' } });
    fireEvent.change(catalogSelect(), { target: { value: 'zhipuai' } });
    fireEvent.click(screen.getByTestId('btn-provider-create-confirm'));
    await waitFor(() => {
      expect(createSpy).toHaveBeenCalledWith(
        'acme',
        expect.objectContaining({ type: 'anthropic-compatible', catalog_provider: 'zhipuai' })
      );
    });
  });

  it('pre-fills the stored hint on edit and keeps it on save', async () => {
    const patchSpy = vi.spyOn(api.providers, 'patch').mockResolvedValue({
      provider: compatibleProvider({ catalog_provider: 'deepseek' }),
    });
    renderDialog({ provider: compatibleProvider({ catalog_provider: 'deepseek' }) });

    expect(catalogSelect().value).toBe('deepseek');

    fireEvent.click(screen.getByTestId('btn-save-edit-prov-gw'));
    await waitFor(() => {
      expect(patchSpy).toHaveBeenCalledWith(
        'acme',
        'prov-gw',
        expect.objectContaining({ catalog_provider: 'deepseek' })
      );
    });
  });

  it('displays the host suggestion but persists empty until the user makes an explicit choice', async () => {
    const patchSpy = vi.spyOn(api.providers, 'patch').mockResolvedValue({
      provider: compatibleProvider(),
    });
    renderDialog({
      provider: compatibleProvider({ suggested_catalog_provider: 'zai-coding-plan' }),
    });

    // Stored value empty + suggestion present → the suggestion is DISPLAYED…
    expect(catalogSelect().value).toBe('zai-coding-plan');

    // …but saving untouched keeps auto semantics (empty stored catalog_provider).
    fireEvent.click(screen.getByTestId('btn-save-edit-prov-gw'));
    await waitFor(() => {
      expect(patchSpy).toHaveBeenCalledWith(
        'acme',
        'prov-gw',
        expect.objectContaining({ catalog_provider: '' })
      );
    });

    // An explicit user selection wins and persists that id.
    patchSpy.mockClear();
    fireEvent.change(catalogSelect(), { target: { value: 'openrouter' } });
    fireEvent.click(screen.getByTestId('btn-save-edit-prov-gw'));
    await waitFor(() => {
      expect(patchSpy).toHaveBeenCalledWith(
        'acme',
        'prov-gw',
        expect.objectContaining({ catalog_provider: 'openrouter' })
      );
    });
  });
});
