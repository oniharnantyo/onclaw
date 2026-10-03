import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ZeroMembershipPane } from './ZeroMembershipPane';

describe('screens/ZeroMembershipPane — zero-membership ask-your-admin state (fix-role-permission-audit 5.3)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('explains that an instance administrator creates workspaces and offers no creation flow', () => {
    render(<ZeroMembershipPane email="u@acme.dev" onSignOut={vi.fn()} />);

    expect(screen.getByTestId('zero-membership-pane')).not.toBeNull();
    expect(screen.getByText('No workspace yet')).not.toBeNull();
    expect(screen.getByText(/instance's administrator/i)).not.toBeNull();
    // No onboarding create entry and no workspace-scoped navigation.
    expect(screen.queryByTestId('btn-onboarding-deploy')).toBeNull();
    expect(screen.queryByTestId('ws-switcher-popover')).toBeNull();
  });

  it('copies a ready-made request quoting the account email', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });

    render(<ZeroMembershipPane email="u@acme.dev" onSignOut={vi.fn()} />);
    fireEvent.click(screen.getByTestId('btn-copy-workspace-request'));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledTimes(1);
    });
    const copied = writeText.mock.calls[0][0] as string;
    expect(copied).toContain('u@acme.dev');
    expect(copied).toContain('OnClaw workspace');
  });

  it('signs out from the secondary action', () => {
    const onSignOut = vi.fn();
    render(<ZeroMembershipPane email="u@acme.dev" onSignOut={onSignOut} />);

    fireEvent.click(screen.getByTestId('btn-zero-membership-signout'));
    expect(onSignOut).toHaveBeenCalledTimes(1);
  });

  it('surfaces a danger toast when the browser blocks the clipboard', async () => {
    const onToast = vi.fn();
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockRejectedValue(new Error('blocked')) },
    });

    render(<ZeroMembershipPane email="u@acme.dev" onToast={onToast} onSignOut={vi.fn()} />);
    fireEvent.click(screen.getByTestId('btn-copy-workspace-request'));

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Clipboard blocked by the browser', 'danger');
    });
  });
});
