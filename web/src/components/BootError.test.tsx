import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { BootError } from './BootError';
import { useAuthStore } from '../store/auth';

describe('BootError component', () => {
  beforeEach(() => {
    localStorage.clear();
    useAuthStore.setState({
      user: null,
      memberships: [],
      status: 'error',
      bootError: null,
      boot: vi.fn(),
      clearSession: vi.fn(() => {
        useAuthStore.setState({ user: null, memberships: [], status: 'unauthenticated', bootError: null });
      }),
    });
    vi.restoreAllMocks();
  });

  it('renders title, body copy, shield icon, and buttons', () => {
    render(
      <MemoryRouter>
        <BootError />
      </MemoryRouter>
    );

    expect(screen.getByText("Couldn't reach OnClaw")).not.toBeNull();
    expect(screen.getByText(/session could not be verified/i)).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Retry' })).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Log in instead' })).not.toBeNull();
    expect(screen.queryByTestId('boot-error-detail')).toBeNull();
  });

  it('renders bootError detail line when bootError is present in state', () => {
    useAuthStore.setState({
      bootError: 'Network error: Failed to fetch',
    });

    render(
      <MemoryRouter>
        <BootError />
      </MemoryRouter>
    );

    const detail = screen.getByTestId('boot-error-detail');
    expect(detail).not.toBeNull();
    expect(detail.textContent).toBe('Network error: Failed to fetch');
  });

  it('renders bootError detail line when passed as a prop', () => {
    render(
      <MemoryRouter>
        <BootError error="500 Internal Server Error" />
      </MemoryRouter>
    );

    const detail = screen.getByTestId('boot-error-detail');
    expect(detail).not.toBeNull();
    expect(detail.textContent).toBe('500 Internal Server Error');
  });

  it('re-enters loading state and calls boot() on Retry click', () => {
    const mockBoot = vi.fn();
    useAuthStore.setState({
      status: 'error',
      boot: mockBoot,
    });

    render(
      <MemoryRouter>
        <BootError />
      </MemoryRouter>
    );

    const retryButton = screen.getByRole('button', { name: 'Retry' });
    fireEvent.click(retryButton);

    expect(useAuthStore.getState().status).toBe('loading');
    expect(mockBoot).toHaveBeenCalledTimes(1);
  });

  it('clears session and navigates to /login on Log in instead click', () => {
    const clearSessionSpy = vi.fn(() => {
      useAuthStore.setState({ user: null, memberships: [], status: 'unauthenticated', bootError: null });
    });
    useAuthStore.setState({
      clearSession: clearSessionSpy,
    });

    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route path="/" element={<BootError />} />
          <Route path="/login" element={<div data-testid="login-view-target">Login View</div>} />
        </Routes>
      </MemoryRouter>
    );

    const loginButton = screen.getByRole('button', { name: 'Log in instead' });
    fireEvent.click(loginButton);

    expect(clearSessionSpy).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('login-view-target')).not.toBeNull();
  });
});
