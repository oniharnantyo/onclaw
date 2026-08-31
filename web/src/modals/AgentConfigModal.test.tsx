import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { AgentConfigModal } from './AgentConfigModal';
import { api } from '../lib/api';

describe('modals/AgentConfigModal', () => {
  const mockTenant = {
    id: 'acme',
    sub: 'acme',
    name: 'Acme Corp',
    providers: [
      {
        id: 'prov_anthropic',
        workspace_id: 'acme',
        type: 'anthropic',
        name: 'Anthropic Prod',
        base_url: '',
        key_set: true,
        key_hint: '7f3a',
        enabled: true,
        created_at: '',
        updated_at: '',
      },
      {
        id: 'prov_openai',
        workspace_id: 'acme',
        type: 'openai',
        name: 'OpenAI Prod',
        base_url: '',
        key_set: true,
        key_hint: '9k2b',
        enabled: true,
        created_at: '',
        updated_at: '',
      },
    ],
  };

  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api.providers, 'list').mockResolvedValue({
      providers: mockTenant.providers,
    });
  });

  it('renders configured providers and unconfigured types as disabled entries', async () => {
    const mockApiProviders = [
      {
        id: 'prov_api_only',
        workspace_id: 'acme',
        type: 'gemini',
        name: 'Gemini Custom API',
        base_url: '',
        key_set: true,
        key_hint: '1234',
        enabled: true,
        created_at: '',
        updated_at: '',
      },
    ];
    vi.spyOn(api.providers, 'list').mockResolvedValue({
      providers: mockApiProviders,
    });

    render(
      <AgentConfigModal
        tenant={mockTenant}
        onClose={vi.fn()}
        onSave={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(screen.getByLabelText(/provider/i)).not.toBeNull();
    });

    expect(api.providers.list).toHaveBeenCalledWith('acme');

    const providerSelect = screen.getByLabelText(/provider/i);
    const options = Array.from(providerSelect.querySelectorAll('option'));

    // Configured options from API (NOT mockTenant)
    expect(options.some((o) => o.textContent?.includes('Gemini Custom API (Gemini)'))).toBe(true);
    // Anthropic and OpenAI from seed should NOT be configured options, they should be disabled
    expect(options.some((o) => o.textContent?.includes('Anthropic Prod'))).toBe(false);

    // Unconfigured option (e.g. Anthropic)
    const anthropicOption = options.find((o) => o.textContent?.includes('Anthropic (Configure in Settings → Providers)'));
    expect(anthropicOption).toBeDefined();
    expect(anthropicOption?.disabled).toBe(true);
  });

  it('cascades model list on provider change', async () => {
    render(
      <AgentConfigModal
        tenant={mockTenant}
        onClose={vi.fn()}
        onSave={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(screen.getByLabelText(/provider/i)).not.toBeNull();
    });

    const providerSelect = screen.getByLabelText(/provider/i);
    const modelSelect = screen.getByLabelText(/model/i) as HTMLSelectElement;

    // Initial provider is Anthropic (first provider), model is claude-sonnet-5
    expect(modelSelect.value).toBe('claude-sonnet-5');

    // Switch to OpenAI Prod
    fireEvent.change(providerSelect, { target: { value: 'prov_openai' } });

    // Model should cascade to OpenAI's first model (gpt-4o)
    expect(modelSelect.value).toBe('gpt-4o');
  });

  it('allows custom model ID entry via free-text input', async () => {
    const onSave = vi.fn();

    render(
      <AgentConfigModal
        tenant={mockTenant}
        onClose={vi.fn()}
        onSave={onSave}
      />
    );

    await waitFor(() => {
      expect(screen.getByLabelText(/agent name/i)).not.toBeNull();
    });

    const nameInput = screen.getByLabelText(/agent name/i);
    fireEvent.change(nameInput, { target: { value: 'CustomAgent' } });

    const modelSelect = screen.getByLabelText(/model/i);
    fireEvent.change(modelSelect, { target: { value: '__custom__' } });

    const customModelInput = screen.getByLabelText(/custom model id/i);
    expect(customModelInput).not.toBeNull();

    fireEvent.change(customModelInput, { target: { value: 'my-custom-model:v1' } });

    fireEvent.click(screen.getByTestId('btn-agent-save-modal'));

    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'CustomAgent',
        model: 'my-custom-model:v1',
      })
    );
  });

  it('disables deploy/save button until name is longer than 1 character', async () => {
    render(
      <AgentConfigModal
        tenant={mockTenant}
        onClose={vi.fn()}
        onSave={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('btn-agent-save-modal')).not.toBeNull();
    });

    const saveBtn = screen.getByTestId('btn-agent-save-modal') as HTMLButtonElement;
    const nameInput = screen.getByLabelText(/agent name/i);

    expect(saveBtn.disabled).toBe(true);

    fireEvent.change(nameInput, { target: { value: 'A' } });
    expect(saveBtn.disabled).toBe(true);

    fireEvent.change(nameInput, { target: { value: 'Radar' } });
    expect(saveBtn.disabled).toBe(false);
  });
});
