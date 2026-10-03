import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ProvidersPane } from './ProvidersPane';
import { api, type ApiProviderConfig } from '../../lib/api';
import { useAuthStore } from '../../store/auth';

const mockTenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

const provider = (over: Partial<ApiProviderConfig> = {}): ApiProviderConfig => ({
  id: 'prov-openai',
  workspace_id: 'acme',
  type: 'openai',
  name: 'Acme OpenAI',
  key_set: true,
  key_hint: '7f3a',
  enabled: true,
  created_at: '',
  updated_at: '',
  ...over,
});

const languageProvider = (over: Partial<ApiProviderConfig> = {}) => provider({ ...over });

const decisionProvider = (over: Partial<ApiProviderConfig> = {}) =>
  provider({ id: 'prov-typesafe', type: 'typesafe', name: 'TypeSafe Router', ...over });

function renderPane(providers: ApiProviderConfig[]) {
  const listSpy = vi.spyOn(api.providers, 'list').mockResolvedValue({ providers });
  render(<ProvidersPane tenant={mockTenant} onToast={vi.fn()} />);
  return { listSpy };
}

const languageTab = () => screen.getByTestId('providers-tab-language');
const decisionTab = () => screen.getByTestId('providers-tab-decision');
const openaiRow = () => screen.queryByTestId('provider-prov-openai');
const typesafeRow = () => screen.queryByTestId('provider-prov-typesafe');

describe('screens/settings/ProvidersPane — Language models / Decision tabs (add-configurable-decision-backend)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('opens on Language models and lists only language-model rows', async () => {
    renderPane([languageProvider(), decisionProvider()]);

    await waitFor(() => {
      expect(openaiRow()).not.toBeNull();
    });
    // Language models is the default tab…
    expect(languageTab().getAttribute('aria-checked')).toBe('true');
    // …listing the openai row…
    expect(screen.getByText('Acme OpenAI')).not.toBeNull();
    // …while the typesafe row stays hidden, and no decision hint shows here.
    expect(typesafeRow()).toBeNull();
    expect(screen.queryByTestId('providers-decision-hint')).toBeNull();
  });

  it('Decision tab lists only typesafe rows, carries the routing hint, and makes no second API call', async () => {
    const { listSpy } = renderPane([languageProvider(), decisionProvider()]);

    await waitFor(() => {
      expect(openaiRow()).not.toBeNull();
    });
    fireEvent.click(decisionTab());

    expect(decisionTab().getAttribute('aria-checked')).toBe('true');
    expect(typesafeRow()).not.toBeNull();
    // Each tab hides the other's rows.
    expect(openaiRow()).toBeNull();
    // The tab is a client-side filter of the already-loaded list.
    expect(listSpy).toHaveBeenCalledTimes(1);
    // The hint states decision providers power routing calls, never chat or
    // agent models.
    expect(screen.getByTestId('providers-decision-hint').textContent).toBe(
      'Decision providers power routing calls — they never serve chat or agent models.'
    );
  });

  it('Decision tab shows an inviting empty state when no typesafe config exists', async () => {
    renderPane([languageProvider()]);

    await waitFor(() => {
      expect(openaiRow()).not.toBeNull();
    });
    fireEvent.click(decisionTab());

    const empty = screen.getByTestId('providers-decision-empty');
    expect(empty).not.toBeNull();
    // The empty state names decision providers as the memory routing backend.
    expect(empty.textContent).toContain('memory routing backend');
    expect(openaiRow()).toBeNull();
  });

  it('Language models tab shows its empty state when only decision configs exist', async () => {
    renderPane([decisionProvider()]);

    await waitFor(() => {
      expect(screen.getByTestId('providers-empty')).not.toBeNull();
    });
    expect(screen.getByText('No providers configured')).not.toBeNull();
    expect(openaiRow()).toBeNull();

    // The typesafe row lives under the Decision tab.
    fireEvent.click(decisionTab());
    expect(typesafeRow()).not.toBeNull();
    expect(screen.queryByTestId('providers-empty')).toBeNull();
  });

  it('both tabs show their empty states when the workspace has no configs', async () => {
    renderPane([]);

    await waitFor(() => {
      expect(screen.getByTestId('providers-empty')).not.toBeNull();
    });
    fireEvent.click(decisionTab());

    const empty = screen.getByTestId('providers-decision-empty');
    expect(empty.textContent).toContain('memory routing backend');
    expect(screen.queryByTestId('providers-empty')).toBeNull();
  });

  it("the add dialog's type select is scoped to the tab it was launched from", async () => {
    renderPane([languageProvider(), decisionProvider()]);

    await waitFor(() => {
      expect(openaiRow()).not.toBeNull();
    });

    // From the Decision tab, only decision types are offered — TypeSafe
    // preselected, no language types in the list.
    fireEvent.click(decisionTab());
    fireEvent.click(screen.getByTestId('btn-provider-add'));
    let typeSelect = screen.getByLabelText('Provider type') as HTMLSelectElement;
    expect(Array.from(typeSelect.options).map((o) => o.value)).toEqual(['typesafe']);
    expect(typeSelect.value).toBe('typesafe');
    fireEvent.click(screen.getByTestId('btn-provider-cancel'));

    // From the Language models tab, the six language types — no TypeSafe.
    fireEvent.click(languageTab());
    fireEvent.click(screen.getByTestId('btn-provider-add'));
    typeSelect = screen.getByLabelText('Provider type') as HTMLSelectElement;
    expect(Array.from(typeSelect.options).map((o) => o.value)).toEqual([
      'openai',
      'anthropic',
      'gemini',
      'openrouter',
      'openai-compatible',
      'anthropic-compatible',
    ]);
    expect(typeSelect.value).toBe('openai');
  });

  it("the decision empty state's add button opens the decision-scoped dialog", async () => {
    renderPane([languageProvider()]);

    await waitFor(() => {
      expect(openaiRow()).not.toBeNull();
    });
    fireEvent.click(decisionTab());
    fireEvent.click(screen.getByTestId('btn-provider-decision-empty-add'));

    const typeSelect = screen.getByLabelText('Provider type') as HTMLSelectElement;
    expect(Array.from(typeSelect.options).map((o) => o.value)).toEqual(['typesafe']);
    expect(typeSelect.value).toBe('typesafe');
  });
});

describe('screens/settings/ProvidersPane — providers.write gating (fix-role-permission-audit)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders rows read-only for a Member without providers.write — no add, edit, toggle, or delete', async () => {
    // Built-in Member set per the decided catalog: reads + channels.*.
    useAuthStore.setState({
      user: { id: 'u1', email: 'u1@acme.dev', name: 'U One', created_at: '', updated_at: '' },
      memberships: [
        {
          workspace_id: 'acme',
          workspace_slug: 'acme',
          role_name: 'Member',
          role: {
            name: 'Member',
            is_owner: false,
            permissions: [
              'workspace.read',
              'members.read',
              'roles.read',
              'providers.read',
              'agents.read',
              'skills.read',
              'scheduler.read',
              'channels.read',
              'channels.write',
            ],
          },
        },
      ] as any,
      status: 'authenticated',
    });

    renderPane([languageProvider()]);

    await waitFor(() => {
      expect(openaiRow()).not.toBeNull();
    });

    // The row itself stays browsable (read is a permission too)…
    expect(screen.getByText('Acme OpenAI')).not.toBeNull();
    // …but no mutation affordance renders.
    expect(screen.queryByTestId('btn-provider-add')).toBeNull();
    expect(screen.queryByTestId('btn-edit-prov-openai')).toBeNull();
    expect(screen.queryByTestId('btn-delete-prov-openai')).toBeNull();
    expect(screen.queryByRole('switch', { name: 'Enable Acme OpenAI' })).toBeNull();

    // The language empty state (a different tab) also offers no add control.
    fireEvent.click(decisionTab());
    expect(screen.queryByTestId('btn-provider-decision-empty-add')).toBeNull();
  });

  it('keeps every affordance in mock mode (no memberships), like the rest of the family', async () => {
    useAuthStore.setState({ user: null, memberships: [], status: 'authenticated' });

    renderPane([languageProvider()]);

    await waitFor(() => {
      expect(openaiRow()).not.toBeNull();
    });
    expect(screen.queryByTestId('btn-provider-add')).not.toBeNull();
    expect(screen.queryByTestId('btn-edit-prov-openai')).not.toBeNull();
    expect(screen.queryByTestId('btn-delete-prov-openai')).not.toBeNull();
    expect(screen.queryByRole('switch', { name: 'Enable Acme OpenAI' })).not.toBeNull();
  });
});
