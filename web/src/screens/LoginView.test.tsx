import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { LoginView } from './LoginView';
import { useAuthStore } from '../store/auth';

describe('screens/LoginView', () => {
  beforeEach(() => {
    useAuthStore.setState({
      user: null,
      memberships: [],
      status: 'unauthenticated',
    });
    vi.restoreAllMocks();
  });

  it('renders login form with inputs and disabled submit button when empty', () => {
    render(
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginView />} />
        </Routes>
      </MemoryRouter>
    );

    expect(screen.getByLabelText(/email address/i)).not.toBeNull();
    expect(screen.getByLabelText(/password/i)).not.toBeNull();
    const submitBtn = screen.getByRole('button', { name: /sign in/i }) as HTMLButtonElement;
    expect(submitBtn.disabled).toBe(true);
  });

  it('enables submit button when email and password are provided', () => {
    render(
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginView />} />
        </Routes>
      </MemoryRouter>
    );

    const emailInput = screen.getByLabelText(/email address/i);
    const passwordInput = screen.getByLabelText(/password/i);
    const submitBtn = screen.getByRole('button', { name: /sign in/i }) as HTMLButtonElement;

    fireEvent.change(emailInput, { target: { value: 'user@example.com' } });
    fireEvent.change(passwordInput, { target: { value: 'password123' } });

    expect(submitBtn.disabled).toBe(false);
  });

  it('submits credentials and handles successful login', async () => {
    const loginMock = vi.fn().mockResolvedValue({
      user: { id: 'u1', email: 'user@example.com', name: 'User' },
      memberships: [],
    });
    useAuthStore.setState({
      login: loginMock,
    });

    render(
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginView />} />
          <Route path="/" element={<div>Home Page</div>} />
        </Routes>
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText(/email address/i), { target: { value: 'user@example.com' } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: 'password123' } });
    fireEvent.click(screen.getByRole('button', { name: /sign in/i }));

    expect(loginMock).toHaveBeenCalledWith('user@example.com', 'password123');
    await waitFor(() => {
      expect(screen.getByText('Home Page')).not.toBeNull();
    });
  });

  it('displays generic error message on login failure', async () => {
    const loginMock = vi.fn().mockRejectedValue(new Error('Unauthorized'));
    useAuthStore.setState({
      login: loginMock,
    });

    render(
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginView />} />
        </Routes>
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText(/email address/i), { target: { value: 'user@example.com' } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: 'wrongpass' } });
    fireEvent.click(screen.getByRole('button', { name: /sign in/i }));

    await waitFor(() => {
      expect(screen.getByText('Invalid email or password')).not.toBeNull();
    });
  });
});
