import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import { ModelCombobox } from './ModelCombobox';
import { api } from '../../lib/api';

describe('components/ui/ModelCombobox', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders live source badge and lists models from workspace provider endpoint', async () => {
    vi.spyOn(api.providers, 'models').mockResolvedValue({
      source: 'live',
      models: [
        { id: 'claude-3-7-sonnet', name: 'Claude 3.7 Sonnet', efforts: ['low', 'medium', 'high'] },
        { id: 'claude-3-5-haiku', name: 'Claude 3.5 Haiku' },
      ],
    });

    const onModelChange = vi.fn();
    const onEffortChange = vi.fn();

    render(
      <ModelCombobox
        workspaceId="acme"
        providerId="prov-1"
        model="claude-3-7-sonnet"
        onModelChange={onModelChange}
        effort="medium"
        onEffortChange={onEffortChange}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('badge-source-live')).not.toBeNull();
    });

    expect(screen.getByTestId('badge-source-live').textContent).toBe('live');
    expect(screen.getByTestId('select-model')).not.toBeNull();

    // Reasoning effort dropdown is shown because claude-3-7-sonnet has efforts
    expect(screen.getByTestId('select-effort')).not.toBeNull();
    const effortSelect = screen.getByTestId('select-effort') as HTMLSelectElement;
    expect(effortSelect.value).toBe('medium');

    // Change effort
    fireEvent.change(effortSelect, { target: { value: 'high' } });
    expect(onEffortChange).toHaveBeenCalledWith('high');
  });

  it('hides effort dropdown when selected model has no reasoning efforts', async () => {
    vi.spyOn(api.providers, 'models').mockResolvedValue({
      source: 'catalog',
      models: [
        { id: 'claude-3-5-haiku', name: 'Claude 3.5 Haiku' }, // no efforts
      ],
    });

    render(
      <ModelCombobox
        workspaceId="acme"
        providerId="prov-1"
        model="claude-3-5-haiku"
        onModelChange={vi.fn()}
        onEffortChange={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('badge-source-catalog')).not.toBeNull();
    });

    expect(screen.queryByTestId('select-effort')).toBeNull();
  });

  it('renders free-text input and none badge when source is none', async () => {
    vi.spyOn(api.providers, 'models').mockResolvedValue({
      source: 'none',
      models: [],
    });

    const onModelChange = vi.fn();

    render(
      <ModelCombobox
        workspaceId="acme"
        providerId="prov-custom"
        model=""
        onModelChange={onModelChange}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('badge-source-none')).not.toBeNull();
    });

    const input = screen.getByTestId('input-custom-model');
    expect(input).not.toBeNull();

    fireEvent.change(input, { target: { value: 'meta-llama/llama-3.3-70b' } });
    expect(onModelChange).toHaveBeenCalledWith('meta-llama/llama-3.3-70b');
  });

  it('fetches from models-preview when previewCreds are provided', async () => {
    vi.spyOn(api.providers, 'modelsPreview').mockResolvedValue({
      source: 'live',
      models: [{ id: 'gpt-4o', name: 'GPT-4o' }],
    });

    render(
      <ModelCombobox
        previewCreds={{
          type: 'openai',
          key: 'sk-test',
        }}
        model="gpt-4o"
        onModelChange={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(api.providers.modelsPreview).toHaveBeenCalledWith({
        type: 'openai',
        base_url: undefined,
        key: 'sk-test',
      });
    });

    expect(screen.getByTestId('badge-source-live')).not.toBeNull();
  });

  it('threads the catalog-mapping hint through models-preview', async () => {
    const previewSpy = vi.spyOn(api.providers, 'modelsPreview').mockResolvedValue({
      source: 'live',
      models: [{ id: 'glm-5.3-flash', name: 'GLM 5.3 Flash' }],
    });

    render(
      <ModelCombobox
        previewCreds={{
          type: 'openai-compatible',
          base_url: 'https://api.example.com/v1',
          key: 'sk-test',
          catalog_provider: 'zai-coding-plan',
        }}
        model="glm-5.3-flash"
        onModelChange={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(previewSpy).toHaveBeenCalledWith(
        expect.objectContaining({ catalog_provider: 'zai-coding-plan' })
      );
    });
  });

  it('renders capability icons only on dropdown rows of affirmatively capable models', async () => {
    vi.spyOn(api.providers, 'models').mockResolvedValue({
      source: 'catalog',
      models: [
        {
          id: 'glm-5.3-flash',
          name: 'GLM 5.3 Flash',
          image_input: true,
          pdf_input: true,
          reasoning: true,
          tool_call: true,
        },
        // Same family, text-only: reasoning + tools only.
        { id: 'glm-5.3', name: 'GLM 5.3', reasoning: true, tool_call: true },
        // No catalog entry: no capability flags at all.
        { id: 'unknown-model', name: 'Unknown Model' },
      ],
    });

    render(
      <ModelCombobox
        workspaceId="acme"
        providerId="prov-1"
        model="glm-5.3-flash"
        onModelChange={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('badge-source-catalog')).not.toBeNull();
    });

    // Closed combobox stays quiet — icons live in the dropdown rows only.
    expect(screen.queryByTitle('Accepts image input')).toBeNull();
    expect(screen.queryByTitle('Accepts PDF input')).toBeNull();
    expect(screen.queryByTitle('Supports reasoning')).toBeNull();
    expect(screen.queryByTitle('Supports tool calling')).toBeNull();

    fireEvent.click(screen.getByTestId('select-model-trigger'));

    const capableRow = screen.getByTestId('combobox-option-glm-5.3-flash');
    expect(within(capableRow).getByTitle('Accepts image input')).not.toBeNull();
    expect(within(capableRow).getByTitle('Accepts PDF input')).not.toBeNull();
    expect(within(capableRow).getByTitle('Supports reasoning')).not.toBeNull();
    expect(within(capableRow).getByTitle('Supports tool calling')).not.toBeNull();

    // Show-if-capable only: no struck-through or placeholder markers.
    const partialRow = screen.getByTestId('combobox-option-glm-5.3');
    expect(within(partialRow).queryByTitle('Accepts image input')).toBeNull();
    expect(within(partialRow).queryByTitle('Accepts PDF input')).toBeNull();
    expect(within(partialRow).getByTitle('Supports reasoning')).not.toBeNull();
    expect(within(partialRow).getByTitle('Supports tool calling')).not.toBeNull();

    const unknownRow = screen.getByTestId('combobox-option-unknown-model');
    expect(within(unknownRow).queryByTitle('Accepts image input')).toBeNull();
    expect(within(unknownRow).queryByTitle('Accepts PDF input')).toBeNull();
    expect(within(unknownRow).queryByTitle('Supports reasoning')).toBeNull();
    expect(within(unknownRow).queryByTitle('Supports tool calling')).toBeNull();
  });
});
