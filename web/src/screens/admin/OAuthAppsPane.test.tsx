import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { OAuthAppsPane } from './OAuthAppsPane';
import { adminOAuthAppsApi, type ApiOAuthApp } from '../../lib/connectionsApi';
import { ApiError } from '../../lib/api';

const atlassianApp = (overrides: Partial<ApiOAuthApp> = {}): ApiOAuthApp => ({
  provider: 'atlassian',
  client_id: 'client-atl-123',
  client_secret_hint: 'a1b2',
  redirect_uri: 'https://onclaw.example.com/api/v1/integrations/oauth/callback/atlassian',
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
  ...overrides,
});

function stubClipboard() {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText },
    configurable: true,
  });
  return writeText;
}

describe('screens/admin/OAuthAppsPane', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders a row per provider with registered/unregistered status and the redirect URI', async () => {
    vi.spyOn(adminOAuthAppsApi, 'list').mockResolvedValue({
      apps: [atlassianApp()],
    });

    render(<OAuthAppsPane onToast={vi.fn()} />);

    // All three known providers render; only the registered one shows its state.
    await waitFor(() => {
      expect(screen.getByTestId('oauth-app-row-atlassian')).not.toBeNull();
    });
    expect(screen.getByTestId('oauth-app-row-slack')).not.toBeNull();
    expect(screen.getByTestId('oauth-app-row-linear')).not.toBeNull();

    expect(screen.getByTestId('oauth-app-status-atlassian').textContent?.trim()).toBe('Registered');
    expect(screen.getByTestId('oauth-app-status-slack').textContent?.trim()).toBe('Not registered');
    expect(screen.getByTestId('oauth-app-status-linear').textContent?.trim()).toBe('Not registered');

    // Redirect URI is displayed for copy with the config hint.
    expect(screen.getByTestId('oauth-redirect-uri-atlassian').textContent).toContain(
      '/api/v1/integrations/oauth/callback/atlassian'
    );
    expect(screen.getByTestId('oauth-redirect-atlassian').textContent).toContain(
      "provider's app settings"
    );
    // Unregistered rows have no redirect URI block yet.
    expect(screen.queryByTestId('oauth-redirect-slack')).toBeNull();

    // The secret hint shows last-4 only — never a full secret.
    expect(screen.getByTestId('oauth-app-row-atlassian').textContent).toContain('····a1b2');
    expect(screen.getByTestId('oauth-app-row-atlassian').textContent).not.toContain('supersecret');
  });

  it('registers a provider app: PUT carries both credentials, status flips, secret is write-only', async () => {
    const save = vi.spyOn(adminOAuthAppsApi, 'save').mockResolvedValue({
      app: atlassianApp({ provider: 'slack', client_id: 'client-slack-9', client_secret_hint: 'zz99' }),
    });
    const onToast = vi.fn();
    vi.spyOn(adminOAuthAppsApi, 'list').mockResolvedValue({ apps: [atlassianApp()] });

    render(<OAuthAppsPane onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('oauth-app-row-slack')).not.toBeNull();
    });

    // Save is gated on both fields.
    expect((screen.getByTestId('btn-oauth-save-slack') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId('input-oauth-client-id-slack'), {
      target: { value: 'client-slack-9' },
    });
    expect((screen.getByTestId('btn-oauth-save-slack') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId('input-oauth-secret-slack'), {
      target: { value: 'supersecret' },
    });
    expect((screen.getByTestId('btn-oauth-save-slack') as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(screen.getByTestId('btn-oauth-save-slack'));

    await waitFor(() => {
      expect(save).toHaveBeenCalledWith('slack', {
        client_id: 'client-slack-9',
        client_secret: 'supersecret',
      });
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Slack app registered');
      expect(screen.getByTestId('oauth-app-status-slack').textContent?.trim()).toBe('Registered');
    });
    // Write-only: the field cleared and only the last-4 hint remains.
    await waitFor(() => {
      expect((screen.getByTestId('input-oauth-secret-slack') as HTMLInputElement).value).toBe('');
      expect(screen.getByTestId('oauth-app-row-slack').textContent).toContain('····zz99');
    });
  });

  it('updates an existing app and reports the update in the toast', async () => {
    const save = vi.spyOn(adminOAuthAppsApi, 'save').mockResolvedValue({
      app: atlassianApp({ client_id: 'client-atl-rotated', client_secret_hint: 'b2c3' }),
    });
    const onToast = vi.fn();
    vi.spyOn(adminOAuthAppsApi, 'list').mockResolvedValue({ apps: [atlassianApp()] });

    render(<OAuthAppsPane onToast={onToast} />);

    await waitFor(() => {
      expect((screen.getByTestId('input-oauth-client-id-atlassian') as HTMLInputElement).value).toBe(
        'client-atl-123'
      );
    });
    // Client id prefilled from the saved row; secret always re-entered.
    fireEvent.change(screen.getByTestId('input-oauth-secret-atlassian'), {
      target: { value: 'rotated-secret' },
    });
    fireEvent.click(screen.getByTestId('btn-oauth-save-atlassian'));

    await waitFor(() => {
      expect(save).toHaveBeenCalledWith('atlassian', {
        client_id: 'client-atl-123',
        client_secret: 'rotated-secret',
      });
      expect(onToast).toHaveBeenCalledWith('Atlassian (Jira & Confluence) app updated');
    });
  });

  it('copies the redirect URI to the clipboard', async () => {
    const writeText = stubClipboard();
    vi.spyOn(adminOAuthAppsApi, 'list').mockResolvedValue({ apps: [atlassianApp()] });

    render(<OAuthAppsPane onToast={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-oauth-copy-atlassian')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-oauth-copy-atlassian'));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith(
        'https://onclaw.example.com/api/v1/integrations/oauth/callback/atlassian'
      );
      expect(screen.getByTestId('btn-oauth-copy-atlassian').textContent).toContain('Copied');
    });
  });

  it('surfaces a save failure inline on the provider row', async () => {
    vi.spyOn(adminOAuthAppsApi, 'save').mockRejectedValue(
      new ApiError(403, 'forbidden', 'instance admin permission required')
    );
    vi.spyOn(adminOAuthAppsApi, 'list').mockResolvedValue({ apps: [] });

    render(<OAuthAppsPane onToast={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByTestId('oauth-app-row-linear')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('input-oauth-client-id-linear'), {
      target: { value: 'client-lin-1' },
    });
    fireEvent.change(screen.getByTestId('input-oauth-secret-linear'), {
      target: { value: 'secret' },
    });
    fireEvent.click(screen.getByTestId('btn-oauth-save-linear'));

    await waitFor(() => {
      expect(screen.getByTestId('oauth-app-error-linear').textContent).toContain(
        'instance admin permission required'
      );
    });
    // The row stays unregistered after a failed save.
    expect(screen.getByTestId('oauth-app-status-linear').textContent?.trim()).toBe('Not registered');
  });

  it('shows the error state with a retry when the list fails', async () => {
    vi.spyOn(adminOAuthAppsApi, 'list').mockRejectedValue(new ApiError(500, 'error', 'db down'));

    render(<OAuthAppsPane onToast={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByText("Couldn't load OAuth apps")).not.toBeNull();
    });
    expect(screen.getByText('db down')).not.toBeNull();

    vi.spyOn(adminOAuthAppsApi, 'list').mockResolvedValue({ apps: [atlassianApp()] });
    fireEvent.click(screen.getByText('Retry'));
    await waitFor(() => {
      expect(screen.getByTestId('oauth-app-row-atlassian')).not.toBeNull();
    });
  });
});
