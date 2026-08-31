import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { TenantsPane } from './admin/TenantsPane';
import { UsersPane } from './admin/UsersPane';
import { MembersSection } from './settings/MembersSection';
import { ProvidersPane } from './settings/ProvidersPane';
import { api, ApiError } from '../lib/api';
import { useAuthStore } from '../store/auth';

describe('Data load error handling in views', () => {
  const onToast = vi.fn();
  const mockTenant = { id: 'test-ws', sub: 'test-ws', name: 'Test Workspace', tz: 'UTC', providers: [] };

  beforeEach(() => {
    vi.restoreAllMocks();
    onToast.mockClear();
    useAuthStore.setState({
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      status: 'authenticated',
      boot: vi.fn(),
    });
  });

  describe('TenantsPane', () => {
    it('renders full-page ErrorState with in-place Retry on 5xx error and recovers on Retry click', async () => {
      const listSpy = vi.spyOn(api.admin.workspaces, 'list')
        .mockRejectedValueOnce(new ApiError(500, 'server_error', 'Database connection failed'))
        .mockResolvedValueOnce({
          workspaces: [
            { id: 'w1', slug: 'w1', name: 'Workspace One', timezone: 'UTC', is_master: false, created_at: '', updated_at: '', member_count: 1 },
          ],
        });

      render(<TenantsPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByText("Couldn't load workspaces")).not.toBeNull();
        expect(screen.getByText("Database connection failed")).not.toBeNull();
        expect(screen.getByRole('button', { name: 'Retry' })).not.toBeNull();
      });

      // Click Retry
      fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

      await waitFor(() => {
        expect(screen.getByText('Workspace One')).not.toBeNull();
        expect(screen.queryByText("Couldn't load workspaces")).toBeNull();
      });

      expect(listSpy).toHaveBeenCalledTimes(2);
    });

    it('keeps loading/empty state without full ErrorState on status 0 network error', async () => {
      vi.spyOn(api.admin.workspaces, 'list')
        .mockRejectedValue(new ApiError(0, 'network', 'Network connection failed'));

      render(<TenantsPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.queryByText("Couldn't load workspaces")).toBeNull();
      });
    });
  });

  describe('UsersPane', () => {
    it('renders full-page ErrorState with in-place Retry on 5xx error and recovers on Retry click', async () => {
      const listSpy = vi.spyOn(api.admin.users, 'list')
        .mockRejectedValueOnce(new ApiError(503, 'service_unavailable', 'Service unavailable'))
        .mockResolvedValueOnce({
          users: [
            { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '', membership_count: 1 },
          ],
        });

      render(<UsersPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByText("Couldn't load users")).not.toBeNull();
        expect(screen.getByText("Service unavailable")).not.toBeNull();
        expect(screen.getByRole('button', { name: 'Retry' })).not.toBeNull();
      });

      // Click Retry
      fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

      await waitFor(() => {
        expect(screen.getByText('alice@example.com')).not.toBeNull();
        expect(screen.queryByText("Couldn't load users")).toBeNull();
      });

      expect(listSpy).toHaveBeenCalledTimes(2);
    });

    it('keeps loading/empty state without full ErrorState on status 0 network error', async () => {
      vi.spyOn(api.admin.users, 'list')
        .mockRejectedValue(new ApiError(0, 'network', 'Network connection failed'));

      render(<UsersPane onToast={onToast} />);

      await waitFor(() => {
        expect(screen.queryByText("Couldn't load users")).toBeNull();
      });
    });
  });

  describe('MembersSection', () => {
    it('renders ErrorState with in-place Retry on 5xx error', async () => {
      vi.spyOn(api.members, 'list')
        .mockRejectedValue(new ApiError(500, 'server_error', 'Failed to fetch members'));
      vi.spyOn(api.roles, 'list')
        .mockResolvedValue({ roles: [] });

      render(<MembersSection tenant={mockTenant} onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByText("Couldn't load members")).not.toBeNull();
        expect(screen.getByText("Failed to fetch members")).not.toBeNull();
        expect(screen.getByRole('button', { name: 'Retry' })).not.toBeNull();
      });
    });

    it('keeps empty state without ErrorState on status 0 network error', async () => {
      vi.spyOn(api.members, 'list')
        .mockRejectedValue(new ApiError(0, 'network', 'Network connection failed'));
      vi.spyOn(api.roles, 'list')
        .mockResolvedValue({ roles: [] });

      render(<MembersSection tenant={mockTenant} onToast={onToast} />);

      await waitFor(() => {
        expect(screen.queryByText("Couldn't load members")).toBeNull();
      });
    });
  });

  describe('ProvidersPane', () => {
    it('renders ErrorState with in-place Retry on 5xx error', async () => {
      vi.spyOn(api.providers, 'list')
        .mockRejectedValue(new ApiError(502, 'bad_gateway', 'Bad Gateway'));

      render(<ProvidersPane tenant={mockTenant} onToast={onToast} />);

      await waitFor(() => {
        expect(screen.getByText("Couldn't load providers")).not.toBeNull();
        expect(screen.getByText("Bad Gateway")).not.toBeNull();
        expect(screen.getByRole('button', { name: 'Retry' })).not.toBeNull();
      });
    });

    it('keeps empty state without ErrorState on status 0 network error', async () => {
      vi.spyOn(api.providers, 'list')
        .mockRejectedValue(new ApiError(0, 'network', 'Network connection failed'));

      render(<ProvidersPane tenant={mockTenant} onToast={onToast} />);

      await waitFor(() => {
        expect(screen.queryByText("Couldn't load providers")).toBeNull();
      });
    });
  });
});
