import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
import { ProviderFormDialog } from './ProviderFormDialog';
import { api, type ApiProviderConfig } from '../lib/api';const compatibleProvider = (over: Partial<ApiProviderConfig> = {}): ApiProviderConfig => ({
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

describe('modals/ProviderFormDialog — draft verify connection (refactor-workspace-settings)', () => {
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

  const verifyButton = () => screen.getByTestId('btn-provider-verify-draft') as HTMLButtonElement;

  it('verifies the typed key from the create dialog: busy state, success strip, dialog stays open', async () => {
    let resolveVerify: (val: { ok: boolean }) => void = () => {};
    const verifyDraft = vi.spyOn(api.providers, 'verifyDraft').mockImplementation(
      () => new Promise((resolve) => { resolveVerify = resolve; }) as any
    );
    const { onClose } = renderDialog();

    // Create mode with no typed key — no credential is expressible yet.
    expect(verifyButton().disabled).toBe(true);

    fireEvent.change(screen.getByLabelText('Provider name'), { target: { value: 'Acme Prod' } });
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-draft-key' } });
    expect(verifyButton().disabled).toBe(false);

    fireEvent.click(verifyButton());

    // Busy strip + in-flight body: typed key, no provider_id on create.
    expect(screen.getByTestId('provider-verify-busy')).not.toBeNull();
    expect(verifyDraft).toHaveBeenCalledWith(
      'acme',
      expect.objectContaining({ type: 'openai', key: 'sk-draft-key' })
    );
    const body = verifyDraft.mock.calls[0][1];
    expect(body.provider_id).toBeUndefined();

    resolveVerify({ ok: true });
    await waitFor(() => {
      expect(screen.getByTestId('provider-verify-result').textContent).toBe(
        'Connection verified successfully'
      );
    });
    // The dialog stays open with the entered values.
    expect(screen.getByTestId('modal-provider')).not.toBeNull();
    expect((screen.getByLabelText('Provider name') as HTMLInputElement).value).toBe('Acme Prod');
    expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('sk-draft-key');
    expect(onClose).not.toHaveBeenCalled();
  });

  it('renders the provider error inline when verification fails', async () => {
    vi.spyOn(api.providers, 'verifyDraft').mockResolvedValue({ ok: false, error: 'Invalid API key' });
    renderDialog();

    fireEvent.change(screen.getByLabelText('Provider name'), { target: { value: 'Acme Prod' } });
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-bad' } });
    fireEvent.click(verifyButton());

    const result = await waitFor(() => screen.getByTestId('provider-verify-result'));
    expect(result.textContent).toBe('Invalid API key');
  });

  it('blank-key edit verifies via the stored key: provider_id rides the body, no key', async () => {
    const verifyDraft = vi
      .spyOn(api.providers, 'verifyDraft')
      .mockResolvedValue({ ok: true });
    renderDialog({ provider: compatibleProvider() });

    // Stored key exists → the credential is expressible with a blank field.
    expect(verifyButton().disabled).toBe(false);

    fireEvent.click(verifyButton());

    await waitFor(() => {
      expect(verifyDraft).toHaveBeenCalledTimes(1);
    });
    const body = verifyDraft.mock.calls[0][1];
    expect(body).toEqual(
      expect.objectContaining({
        type: 'openai-compatible',
        base_url: 'https://api.example.com/v1',
        provider_id: 'prov-gw',
      })
    );
    expect(body.key).toBeUndefined();
    await waitFor(() => {
      expect(screen.getByTestId('provider-verify-result').textContent).toBe(
        'Connection verified successfully'
      );
    });
  });

  it('stays disabled until a credential is expressible: typed key or edit-with-stored-key', async () => {
    renderDialog(); // create mode, no key typed
    expect(verifyButton().disabled).toBe(true);

    // Typing a key expresses a credential.
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-x' } });
    expect(verifyButton().disabled).toBe(false);

    cleanup();
    // Edit mode without a stored key and without typing — still disabled.
    render(
      <ProviderFormDialog
        workspaceId="acme"
        provider={compatibleProvider({ key_set: false, key_hint: '' })}
        onClose={vi.fn()}
        onSaved={vi.fn()}
        onToast={vi.fn()}
      />
    );
    expect(verifyButton().disabled).toBe(true);

    // Editing a config WITH a stored key is expressible with a blank field.
    cleanup();
    render(
      <ProviderFormDialog
        workspaceId="acme"
        provider={compatibleProvider()}
        onClose={vi.fn()}
        onSaved={vi.fn()}
        onToast={vi.fn()}
      />
    );
    expect(verifyButton().disabled).toBe(false);
  });

  it('never gates save: a failed verify leaves the save button enabled and saving still works', async () => {
    vi.spyOn(api.providers, 'verifyDraft').mockResolvedValue({ ok: false, error: 'Connection refused' });
    const createSpy = vi.spyOn(api.providers, 'create').mockResolvedValue({
      provider: compatibleProvider(),
    });
    const { onToast } = renderDialog();

    fireEvent.change(screen.getByLabelText('Provider name'), { target: { value: 'Gateway' } });
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-any' } });

    fireEvent.click(verifyButton());
    await waitFor(() => {
      expect(screen.getByTestId('provider-verify-result').textContent).toBe('Connection refused');
    });

    // The failed test does not lock the save button — verification is not a gate.
    const saveBtn = screen.getByTestId('btn-provider-create-confirm') as HTMLButtonElement;
    expect(saveBtn.disabled).toBe(false);
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(createSpy).toHaveBeenCalledWith('acme', expect.objectContaining({ name: 'Gateway' }));
      // The toast names the server-confirmed row ('Z.ai Gateway').
      expect(onToast).toHaveBeenCalledWith('Provider Z.ai Gateway created');
    });
  });
});

describe('modals/ProviderFormDialog — keyless declaration checkbox (fix-keyless-provider-verify D4)', () => {
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

  const verifyButton = () => screen.getByTestId('btn-provider-verify-draft') as HTMLButtonElement;
  const keyInput = () => screen.getByLabelText('API key') as HTMLInputElement;
  const noKeyCheckbox = () =>
    screen.getByTestId('prov-no-key-checkbox') as HTMLInputElement;
  const switchType = (type: string) =>
    fireEvent.change(screen.getByLabelText('Provider type'), { target: { value: type } });

  it('renders the checkbox only for keyless-capable types, never for key-requiring ones', () => {
    renderDialog();

    // Default create mode is 'openai' — a key-requiring type.
    expect(screen.queryByTestId('prov-no-key-checkbox')).toBeNull();
    for (const keyRequiring of ['openai', 'anthropic', 'gemini', 'openrouter', 'typesafe']) {
      switchType(keyRequiring);
      expect(screen.queryByTestId('prov-no-key-checkbox')).toBeNull();
    }

    for (const keyless of ['openai-compatible', 'anthropic-compatible']) {
      switchType(keyless);
      expect(screen.queryByTestId('prov-no-key-checkbox')).not.toBeNull();
    }
  });

  it('checking the checkbox disables the key field and enables Verify; unchecking reverses both', () => {
    renderDialog();
    switchType('openai-compatible');

    // No credential expressible yet — Verify disabled, key field enabled.
    expect(verifyButton().disabled).toBe(true);
    expect(keyInput().disabled).toBe(false);

    fireEvent.click(noKeyCheckbox());
    expect(noKeyCheckbox().checked).toBe(true);
    expect(keyInput().disabled).toBe(true);
    expect(verifyButton().disabled).toBe(false);

    fireEvent.click(noKeyCheckbox());
    expect(noKeyCheckbox().checked).toBe(false);
    expect(keyInput().disabled).toBe(false);
    expect(verifyButton().disabled).toBe(true);
  });

  it('verifies a keyless declaration with NO key in the body and an empty key field', async () => {
    const verifyDraft = vi.spyOn(api.providers, 'verifyDraft').mockResolvedValue({ ok: true });
    renderDialog();
    switchType('openai-compatible');

    fireEvent.click(noKeyCheckbox());
    fireEvent.change(screen.getByLabelText('Base URL'), { target: { value: 'https://api.example.com/v1' } });
    expect(verifyButton().disabled).toBe(false);

    fireEvent.click(verifyButton());

    await waitFor(() => {
      expect(verifyDraft).toHaveBeenCalledTimes(1);
    });
    const body = verifyDraft.mock.calls[0][1];
    expect(body).toEqual(
      expect.objectContaining({
        type: 'openai-compatible',
        base_url: 'https://api.example.com/v1',
      })
    );
    expect(body).not.toHaveProperty('key');
    expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('');
    await waitFor(() => {
      expect(screen.getByTestId('provider-verify-result').textContent).toBe(
        'Connection verified successfully'
      );
    });
  });

  it('shows no checkbox for an edit with a stored key and keeps Verify enabled on a blank field', () => {
    renderDialog({ provider: compatibleProvider() });

    expect(screen.queryByTestId('prov-no-key-checkbox')).toBeNull();
    expect(verifyButton().disabled).toBe(false);
    expect((screen.getByLabelText('Edit API key') as HTMLInputElement).disabled).toBe(false);
  });

  it('switching from a keyless type to a key-requiring one drops the declaration: checkbox gone, key field enabled, Verify needs a key again', () => {
    renderDialog();
    switchType('openai-compatible');

    fireEvent.click(noKeyCheckbox());
    expect(verifyButton().disabled).toBe(false);

    switchType('openai');
    expect(screen.queryByTestId('prov-no-key-checkbox')).toBeNull();
    expect(keyInput().disabled).toBe(false);
    expect(keyInput().value).toBe('');
    expect(verifyButton().disabled).toBe(true);
  });

  it('routes keyless users to the checkbox via the disabled-Verify tooltip', () => {
    renderDialog();

    // Key-requiring default type keeps the existing tooltip.
    expect(verifyButton().title).toBe('Enter an API key to verify');

    switchType('openai-compatible');
    expect(verifyButton().disabled).toBe(true);
    expect(verifyButton().title).toBe(
      "No API key needed? Tick 'This endpoint needs no API key'"
    );
  });
});

describe('modals/ProviderFormDialog — type groups & typesafe (add-configurable-decision-backend)', () => {
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

  const typeSelect = () => screen.getByLabelText('Provider type') as HTMLSelectElement;
  const verifyButton = () => screen.getByTestId('btn-provider-verify-draft') as HTMLButtonElement;
  const saveButton = () => screen.getByTestId('btn-provider-create-confirm') as HTMLButtonElement;
  const optgroups = () => Array.from(typeSelect().querySelectorAll('optgroup'));
  const groupValues = (group: HTMLOptGroupElement) =>
    Array.from(group.querySelectorAll('option')).map((o) => o.value);
  const switchType = (type: string) =>
    fireEvent.change(typeSelect(), { target: { value: type } });

  it('groups the type select into Language models and Decision optgroups', () => {
    renderDialog();

    const groups = optgroups();
    expect(groups.map((g) => g.label)).toEqual(['Language models', 'Decision']);
    expect(groupValues(groups[0])).toEqual([
      'openai',
      'anthropic',
      'gemini',
      'openrouter',
      'openai-compatible',
      'anthropic-compatible',
    ]);
    expect(groupValues(groups[1])).toEqual(['typesafe']);
    // The decision option's label is TypeSafe.
    expect(groups[1].querySelector('option')?.textContent).toBe('TypeSafe');
  });

  it('typeGroup "decision" offers only the decision group with TypeSafe preselected', () => {
    renderDialog({ typeGroup: 'decision' });

    const groups = optgroups();
    expect(groups.map((g) => g.label)).toEqual(['Decision']);
    expect(groupValues(groups[0])).toEqual(['typesafe']);
    expect(typeSelect().value).toBe('typesafe');
  });

  it('typeGroup "language" offers only the language group with no typesafe option', () => {
    renderDialog({ typeGroup: 'language' });

    const groups = optgroups();
    expect(groups.map((g) => g.label)).toEqual(['Language models']);
    expect(groupValues(groups[0])).toEqual([
      'openai',
      'anthropic',
      'gemini',
      'openrouter',
      'openai-compatible',
      'anthropic-compatible',
    ]);
    expect(typeSelect().value).toBe('openai');
    expect(screen.queryByRole('option', { name: 'TypeSafe' })).toBeNull();
  });

  it('typesafe form: canonical systemone placeholder, no keyless checkbox, key required to verify', () => {
    renderDialog();
    switchType('typesafe');

    // Canonical origin is the placeholder; an empty field means the default.
    const baseUrl = screen.getByLabelText('Base URL') as HTMLInputElement;
    expect(baseUrl.placeholder).toBe('https://api.typesafe.ai/v1/systemone');

    // The help copy states the endpoint is used as entered — nothing is
    // appended (the language-model "server appends resource paths" copy
    // would steer users into a wrong-path override).
    expect(screen.getByText(/used exactly as entered/)).not.toBeNull();

    // The keyless declaration never renders for the key-requiring decision type.
    expect(screen.queryByTestId('prov-no-key-checkbox')).toBeNull();

    // Base URL stays optional (empty = canonical default, set = override) —
    // save stays gated on the name alone.
    fireEvent.change(screen.getByLabelText('Provider name'), { target: { value: 'Router' } });
    expect(saveButton().disabled).toBe(false);

    // The key IS required for verify: disabled until one is typed.
    expect(verifyButton().disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'ts-key' } });
    expect(verifyButton().disabled).toBe(false);
  });

  it('typesafe create submits type, name and key with no base_url until overridden', async () => {
    const createSpy = vi.spyOn(api.providers, 'create').mockResolvedValue({
      provider: compatibleProvider({ id: 'prov-ts', type: 'typesafe', name: 'Router', base_url: '' }),
    });
    renderDialog();

    switchType('typesafe');
    fireEvent.change(screen.getByLabelText('Provider name'), { target: { value: 'Router' } });
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'ts-key' } });
    fireEvent.click(saveButton());

    await waitFor(() => {
      expect(createSpy).toHaveBeenCalledWith('acme', {
        type: 'typesafe',
        name: 'Router',
        key: 'ts-key',
        enabled: true,
      });
    });
  });

  it('typesafe create with a base URL override rides the payload', async () => {
    const createSpy = vi.spyOn(api.providers, 'create').mockResolvedValue({
      provider: compatibleProvider({
        id: 'prov-ts',
        type: 'typesafe',
        name: 'Router',
        base_url: 'https://staging.typesafe.ai/v1/systemone',
      }),
    });
    renderDialog();

    switchType('typesafe');
    fireEvent.change(screen.getByLabelText('Provider name'), { target: { value: 'Router' } });
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'ts-key' } });
    fireEvent.change(screen.getByLabelText('Base URL'), {
      target: { value: 'https://staging.typesafe.ai/v1/systemone' },
    });
    fireEvent.click(saveButton());

    await waitFor(() => {
      expect(createSpy).toHaveBeenCalledWith(
        'acme',
        expect.objectContaining({
          type: 'typesafe',
          base_url: 'https://staging.typesafe.ai/v1/systemone',
          key: 'ts-key',
        })
      );
    });
  });

  it('typesafe edit with a stored key: no checkbox, verify enabled on a blank field', () => {
    renderDialog({
      provider: compatibleProvider({ id: 'prov-ts', type: 'typesafe', name: 'Router', base_url: '' }),
    });

    expect(typeSelect().value).toBe('typesafe');
    expect((screen.getByLabelText('Edit base URL') as HTMLInputElement).placeholder).toBe(
      'https://api.typesafe.ai/v1/systemone'
    );
    expect(screen.queryByTestId('prov-no-key-checkbox')).toBeNull();
    expect(verifyButton().disabled).toBe(false);
  });

  it('existing language-model types keep their form shape', () => {
    renderDialog();

    // openai: optional-override placeholder, no keyless checkbox.
    expect((screen.getByLabelText('Base URL') as HTMLInputElement).placeholder).toBe(
      'https://api.openai.com/v1 (optional override)'
    );
    expect(screen.queryByTestId('prov-no-key-checkbox')).toBeNull();

    // openai-compatible: required base URL, catalog mapping, keyless checkbox.
    switchType('openai-compatible');
    expect(screen.queryByTestId('prov-no-key-checkbox')).not.toBeNull();
    expect(screen.getByLabelText('Catalog mapping')).not.toBeNull();
    expect(saveButton().disabled).toBe(true); // base URL required
  });
});
