import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ConnectServiceDialog } from './ConnectServiceDialog';
import { connectionsApi, type ApiConnection, type ApiIntegrationRecipe } from '../lib/connectionsApi';
import { api, ApiError } from '../lib/api';

const tenant = { id: 'acme', sub: 'acme' };

// add-recipe-base-url: the GitLab recipe — HTTP-kind PAT over the REST API
// with a declared origin parameter defaulting to the SaaS origin.
const gitlabRecipe = (overrides: Partial<ApiIntegrationRecipe> = {}): ApiIntegrationRecipe => ({
  id: 'gitlab',
  service: 'GitLab',
  icon: 'gitlab',
  auth_kind: 'pat',
  availability: 'available',
  kind: 'http',
  base_url: 'https://gitlab.com',
  origin_param: {
    name: 'GitLab instance URL',
    default: 'https://gitlab.com',
    help: 'The GitLab origin — gitlab.com or a self-managed instance serving its REST API under /api/v4.',
  },
  access_levels: ['read_only'],
  steps: [{ title: 'Create a personal access token' }],
  scopes: [{ access_level: 'read_only', scopes: ['read_api'] }],
  token_header: 'PRIVATE-TOKEN',
  verbs: [{ name: 'gitlab.current_user', method: 'GET', path: '/api/v4/user' }],
  probe: { tool: 'gitlab.current_user', method: 'GET', path: '/api/v4/user' },
  ...overrides,
});

// A recipe without an origin parameter — fixed endpoint, no origin field
// (spec: "Undeclared recipes ignore origin").
const figmaRecipe = (overrides: Partial<ApiIntegrationRecipe> = {}): ApiIntegrationRecipe => ({
  id: 'figma',
  service: 'Figma',
  icon: 'figma',
  auth_kind: 'pat',
  availability: 'available',
  kind: 'http',
  base_url: 'https://api.figma.com',
  token_header: 'X-Figma-Token',
  access_levels: ['read_only'],
  steps: [{ title: 'Create a Figma personal access token' }],
  scopes: [{ access_level: 'read_only', scopes: ['file_dev:read'] }],
  verbs: [{ name: 'figma.get_me', method: 'GET', path: '/v1/me' }],
  probe: { tool: 'figma.get_me' },
  ...overrides,
});

const connectedRow = (overrides: Partial<ApiConnection> = {}): ApiConnection => ({
  id: 'conn-gl',
  workspace_id: 'acme',
  service: 'gitlab',
  access_level: 'read_only',
  status: 'connected',
  status_error: null,
  token_hint: 'a1b2',
  server_id: null,
  server_enabled: false,
  origin: 'https://gitlab.example.com',
  attached_agents: [],
  created_at: '',
  updated_at: '',
  ...overrides,
});

function renderDialog(recipe: ApiIntegrationRecipe, props: Partial<Parameters<typeof ConnectServiceDialog>[0]> = {}) {
  return render(
    <ConnectServiceDialog
      recipe={recipe}
      tenant={tenant}
      onClose={vi.fn()}
      {...props}
    />
  );
}

describe('modals/ConnectServiceDialog — origin parameter (add-recipe-base-url)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: [] });
  });

  it('renders the origin field preset to the recipe default, labeled, editable, with help guidance', () => {
    renderDialog(gitlabRecipe());

    // Labeled and preset to the declared SaaS default.
    const input = screen.getByTestId('input-connect-origin') as HTMLInputElement;
    expect(screen.getByLabelText('GitLab instance URL')).not.toBeNull();
    expect(input.value).toBe('https://gitlab.com');
    // The recipe's help copy is the field guidance.
    expect(
      screen.getByText(
        'The GitLab origin — gitlab.com or a self-managed instance serving its REST API under /api/v4.'
      )
    ).not.toBeNull();

    // Editable in place.
    fireEvent.change(input, { target: { value: 'https://gitlab.example.com' } });
    expect((screen.getByTestId('input-connect-origin') as HTMLInputElement).value).toBe(
      'https://gitlab.example.com'
    );
  });

  it('submits the edited origin in the connect payload', async () => {
    const connect = vi
      .spyOn(connectionsApi, 'connect')
      .mockResolvedValue({ connection: connectedRow() });

    renderDialog(gitlabRecipe());

    fireEvent.change(screen.getByTestId('input-connect-origin'), {
      target: { value: 'https://gitlab.example.com' },
    });
    fireEvent.change(screen.getByTestId('input-connect-token'), { target: { value: 'glpat-x' } });
    fireEvent.click(screen.getByTestId('btn-connect-confirm'));

    await waitFor(() => {
      expect(connect).toHaveBeenCalledWith('acme', {
        recipe_id: 'gitlab',
        access_level: 'read_only',
        token: 'glpat-x',
        origin: 'https://gitlab.example.com',
      });
    });
    await waitFor(() => {
      expect(screen.getByTestId('connect-success')).not.toBeNull();
    });
  });

  it('submits an empty origin as empty — the backend resolves the declared default', async () => {
    const connect = vi
      .spyOn(connectionsApi, 'connect')
      .mockResolvedValue({ connection: connectedRow({ origin: 'https://gitlab.com' }) });

    renderDialog(gitlabRecipe());

    // Clear the preset; empty still connects (empty = the recipe default).
    fireEvent.change(screen.getByTestId('input-connect-origin'), { target: { value: '' } });
    fireEvent.change(screen.getByTestId('input-connect-token'), { target: { value: 'glpat-x' } });
    expect((screen.getByTestId('btn-connect-confirm') as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByTestId('btn-connect-confirm'));

    await waitFor(() => {
      expect(connect).toHaveBeenCalledWith('acme', {
        recipe_id: 'gitlab',
        access_level: 'read_only',
        token: 'glpat-x',
        origin: '',
      });
    });
  });

  it('renders no origin field for a recipe without an origin parameter', () => {
    renderDialog(figmaRecipe());

    expect(screen.queryByTestId('input-connect-origin')).toBeNull();
    expect(screen.queryByTestId('connect-origin-error')).toBeNull();
    // The ordinary PAT flow renders untouched.
    expect(screen.getByTestId('input-connect-token')).not.toBeNull();
  });

  it('lands a base-URL validation failure on the origin field, not the shared banner', async () => {
    vi.spyOn(connectionsApi, 'connect').mockRejectedValue(
      new ApiError(
        400,
        'invalid_request',
        'GitLab instance URL: origin "https://gitlab.example.com/api/v4" must be an absolute http(s) origin (scheme://host[:port]) without path, query, fragment, or userinfo'
      )
    );

    renderDialog(gitlabRecipe());

    fireEvent.change(screen.getByTestId('input-connect-token'), { target: { value: 'glpat-x' } });
    fireEvent.click(screen.getByTestId('btn-connect-confirm'));

    await waitFor(() => {
      expect(screen.getByTestId('connect-origin-error').textContent).toContain('GitLab instance URL');
    });
    // The shared banner stays clear — the error names its field.
    expect(screen.queryByTestId('connect-error')).toBeNull();
    // Nothing stored — the dialog stays open on a corrected value.
    expect(screen.queryByTestId('connect-success')).toBeNull();
  });

  it('keeps non-origin connect failures on the shared banner', async () => {
    vi.spyOn(connectionsApi, 'connect').mockRejectedValue(
      new ApiError(409, 'conflict', 'GitLab is already connected in this workspace for origin https://gitlab.com')
    );

    renderDialog(gitlabRecipe());

    fireEvent.change(screen.getByTestId('input-connect-token'), { target: { value: 'glpat-x' } });
    fireEvent.click(screen.getByTestId('btn-connect-confirm'));

    await waitFor(() => {
      expect(screen.getByTestId('connect-error').textContent).toContain('already connected');
    });
    expect(screen.queryByTestId('connect-origin-error')).toBeNull();
  });
});
