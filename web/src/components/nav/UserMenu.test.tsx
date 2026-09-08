import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { UserMenu } from './UserMenu';
import { useAuthStore } from '../../store/auth';
import { api } from '../../lib/api';

describe('components/nav/UserMenu', () => {
  beforeEach(() => {
    useAuthStore.setState({
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice Smith', created_at: '', updated_at: '' },
      memberships: [],
      status: 'authenticated',
    });
    vi.restoreAllMocks();
  });

  it('renders user chip button', () => {
    render(<UserMenu />);
    const chip = screen.getByRole('button', { name: /user menu for alice smith/i });
    expect(chip).not.toBeNull();
  });

  it('toggles menu popover when clicked and shows user details and logout option', () => {
    render(<UserMenu />);
    const chip = screen.getByRole('button', { name: /user menu for alice smith/i });

    // Initially closed
    expect(screen.queryByRole('menu')).toBeNull();

    // Click to open
    fireEvent.click(chip);
    expect(screen.queryByRole('menu')).not.toBeNull();
    expect(screen.getByText('Alice Smith')).not.toBeNull();
    expect(screen.getByText('alice@example.com')).not.toBeNull();
    expect(screen.getByRole('menuitem', { name: /log out/i })).not.toBeNull();

    // Click to close
    fireEvent.click(chip);
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('triggers logout handler when log out button is clicked', async () => {
    const onLogout = vi.fn();
    render(<UserMenu onLogout={onLogout} />);

    fireEvent.click(screen.getByRole('button', { name: /user menu for alice smith/i }));
    const logoutBtn = screen.getByRole('menuitem', { name: /log out/i });
    fireEvent.click(logoutBtn);

    expect(onLogout).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('closes popover on Escape key press', () => {
    render(<UserMenu />);
    fireEvent.click(screen.getByRole('button', { name: /user menu for alice smith/i }));
    expect(screen.queryByRole('menu')).not.toBeNull();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('opens the My memory editor from the menu and saves via the user memory endpoint', async () => {
    vi.spyOn(api.memory, 'getMine').mockResolvedValue({
      content: '',
      max_chars: 4096,
      updated_at: null,
    });
    const updateMine = vi.spyOn(api.memory, 'updateMine').mockResolvedValue({
      content: 'note',
      max_chars: 4096,
      updated_at: '2026-09-01T00:00:00Z',
    });

    render(<UserMenu />);
    fireEvent.click(screen.getByRole('button', { name: /user menu for alice smith/i }));
    expect(screen.getByRole('menuitem', { name: /my memory/i })).not.toBeNull();

    fireEvent.click(screen.getByRole('menuitem', { name: /my memory/i }));

    // Menu closes, modal opens and loads the current memory
    await waitFor(() => {
      expect(screen.getByTestId('modal-user-memory')).not.toBeNull();
    });
    expect(screen.queryByRole('menu')).toBeNull();
    expect(screen.getByTestId('memory-char-counter').textContent).toBe('0 / 4096 chars');

    fireEvent.change(screen.getByTestId('input-user-memory'), { target: { value: 'note' } });
    fireEvent.click(screen.getByTestId('btn-memory-save'));

    await waitFor(() => {
      expect(updateMine).toHaveBeenCalledWith('acme', { content: 'note' });
    });
    await waitFor(() => {
      expect(screen.getByTestId('memory-saved-indicator')).not.toBeNull();
    });
  });
});
