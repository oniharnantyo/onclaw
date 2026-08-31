import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { ChatRoute } from './ChatRoute';
import { useStore } from '../store';
import { useAuthStore } from '../store/auth';
import { seedDb } from '../data/seed';

vi.mock('@assistant-ui/react', () => ({
  useExternalStoreRuntime: vi.fn((opts) => opts),
  AssistantRuntimeProvider: ({ children }: any) => <div>{children}</div>,
}));

describe('ChatRoute component', () => {
  beforeEach(() => {
    localStorage.clear();
    useAuthStore.setState({
      user: { id: 'u1', email: 'alice@example.com', name: 'Alice', created_at: '', updated_at: '' },
      memberships: [],
      status: 'authenticated',
      boot: vi.fn(),
    });
    useStore.setState({
      db: seedDb(),
      pos: { tenantId: 'acme', view: 'chats', chatId: 'a-atlas', showContext: false, railExpanded: false },
    });
    vi.restoreAllMocks();
  });

  it('renders not-found ErrorState for a well-formed but unknown chat ID', async () => {
    render(
      <MemoryRouter initialEntries={['/c/a-unknown-agent']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByText('Chat not found')).not.toBeNull();
      expect(screen.getByText(/the requested agent, channel, or person does not exist/i)).not.toBeNull();
      expect(screen.getByRole('button', { name: 'Back to chats' })).not.toBeNull();
      expect(screen.getByRole('button', { name: 'View agents' })).not.toBeNull();
    });
  });

  it('redirects malformed chat ID to first agent', async () => {
    render(
      <MemoryRouter initialEntries={['/c/@!invalid$$']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
          <Route path="/c/a-atlas" element={<div data-testid="atlas-chat">Atlas Agent</div>} />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTestId('atlas-chat')).not.toBeNull();
    });
  });

  it('renders active chat view for a valid agent ID', async () => {
    render(
      <MemoryRouter initialEntries={['/c/a-atlas']}>
        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByLabelText(/conversation with atlas/i)).not.toBeNull();
    });
  });
});

