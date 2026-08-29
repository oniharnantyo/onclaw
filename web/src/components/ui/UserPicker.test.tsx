import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { UserPicker } from './UserPicker';
import { api, type ApiUser } from '../../lib/api';

describe('components/ui/UserPicker', () => {
  const mockUsers: ApiUser[] = [
    {
      id: 'u1',
      email: 'alice@example.com',
      name: 'Alice Smith',
      created_at: '2026-08-01T00:00:00Z',
      updated_at: '2026-08-01T00:00:00Z',
      is_superadmin: true,
    },
    {
      id: 'u2',
      email: 'bob@example.com',
      name: 'Bob Jones',
      created_at: '2026-08-02T00:00:00Z',
      updated_at: '2026-08-02T00:00:00Z',
      disabled_at: '2026-08-20T00:00:00Z',
    },
    {
      id: 'u3',
      email: 'carol@example.com',
      name: 'Carol Danvers',
      created_at: '2026-08-03T00:00:00Z',
      updated_at: '2026-08-03T00:00:00Z',
    },
  ];

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('fetches users from api.admin.users.list on mount', async () => {
    const listSpy = vi.spyOn(api.admin.users, 'list').mockResolvedValue({
      users: mockUsers,
    });

    render(
      <UserPicker
        value="u1"
        onChange={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(listSpy).toHaveBeenCalledTimes(1);
      expect(screen.getByRole('combobox').textContent).toContain('Alice Smith');
    });
  });

  it('uses prop-provided users without calling API', () => {
    const listSpy = vi.spyOn(api.admin.users, 'list');

    render(
      <UserPicker
        users={mockUsers}
        value="u1"
        onChange={vi.fn()}
      />
    );

    expect(listSpy).not.toHaveBeenCalled();
    expect(screen.getByRole('combobox').textContent).toContain('Alice Smith');
  });

  it('renders user details (name, email, superadmin, disabled) in options list', () => {
    render(
      <UserPicker
        users={mockUsers}
        onChange={vi.fn()}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));

    expect(screen.getByText('Alice Smith')).not.toBeNull();
    expect(screen.getByText('alice@example.com')).not.toBeNull();
    expect(screen.getByText('Superadmin')).not.toBeNull();

    expect(screen.getByText('Bob Jones')).not.toBeNull();
    expect(screen.getByText('bob@example.com')).not.toBeNull();
    expect(screen.getByText('Disabled')).not.toBeNull();
  });

  it('disallows selecting disabled users', () => {
    const onChange = vi.fn();
    render(
      <UserPicker
        users={mockUsers}
        onChange={onChange}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));
    const bobOption = screen.getByTestId('combobox-option-u2');
    expect(bobOption.getAttribute('aria-disabled')).toBe('true');

    fireEvent.mouseDown(bobOption);
    expect(onChange).not.toHaveBeenCalled();
  });

  it('filters users by name and email substring', () => {
    render(
      <UserPicker
        users={mockUsers}
        onChange={vi.fn()}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));
    const searchInput = screen.getByTestId('combobox-search-input');

    // Filter by name
    fireEvent.change(searchInput, { target: { value: 'Carol' } });
    expect(screen.getByText('Carol Danvers')).not.toBeNull();
    expect(screen.queryByText('Alice Smith')).toBeNull();

    // Filter by email
    fireEvent.change(searchInput, { target: { value: 'alice@' } });
    expect(screen.getByText('Alice Smith')).not.toBeNull();
    expect(screen.queryByText('Carol Danvers')).toBeNull();
  });

  it('selects user and emits user id and object to onChange', () => {
    const onChange = vi.fn();
    render(
      <UserPicker
        users={mockUsers}
        onChange={onChange}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));
    const carolOption = screen.getByTestId('combobox-option-u3');
    fireEvent.mouseDown(carolOption);

    expect(onChange).toHaveBeenCalledWith('u3', mockUsers[2]);
  });

  it('supports valueKey="email" for email-based pickers', () => {
    const onChange = vi.fn();
    render(
      <UserPicker
        users={mockUsers}
        valueKey="email"
        onChange={onChange}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));
    const aliceOption = screen.getByTestId('combobox-option-alice@example.com');
    fireEvent.mouseDown(aliceOption);

    expect(onChange).toHaveBeenCalledWith('alice@example.com', mockUsers[0]);
  });

  it('respects excludeUserIds and disabledUserIds props', () => {
    render(
      <UserPicker
        users={mockUsers}
        excludeUserIds={['u1']}
        disabledUserIds={['u3']}
        onChange={vi.fn()}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));

    expect(screen.queryByText('Alice Smith')).toBeNull();
    const carolOption = screen.getByTestId('combobox-option-u3');
    expect(carolOption.getAttribute('aria-disabled')).toBe('true');
  });

  it('handles fetch errors gracefully', async () => {
    vi.spyOn(api.admin.users, 'list').mockRejectedValue(new Error('Network error'));

    render(
      <UserPicker
        onChange={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(screen.queryByText('Loading users…')).toBeNull();
    });

    fireEvent.click(screen.getByRole('combobox'));
    expect(screen.getByText(/Error: Network error/i)).not.toBeNull();
  });
});
