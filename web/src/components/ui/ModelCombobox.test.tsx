import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
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
});
