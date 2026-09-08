import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { UserMemoryModal } from './UserMemoryModal';
import { api, ApiError } from '../lib/api';

const memoryPayload = {
  content: 'Prefers concise answers.',
  max_chars: 4096,
  updated_at: '2026-09-01T00:00:00Z' as string | null,
};

describe('modals/UserMemoryModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('loads the current memory on open and shows the live counter from the server cap', async () => {
    const getMine = vi.spyOn(api.memory, 'getMine').mockResolvedValue({ ...memoryPayload });

    render(<UserMemoryModal wsSlug="acme" onClose={vi.fn()} />);

    await waitFor(() => {
      expect((screen.getByTestId('input-user-memory') as HTMLTextAreaElement).value).toBe(
        'Prefers concise answers.'
      );
    });
    expect(getMine).toHaveBeenCalledWith('acme');
    expect(screen.getByTestId('memory-char-counter').textContent).toBe('24 / 4096 chars');
    expect(screen.queryByTestId('memory-loading')).toBeNull();
  });

  it('shows a loading state until the memory payload arrives', async () => {
    let resolveGet: (v: any) => void = () => {};
    vi.spyOn(api.memory, 'getMine').mockImplementation(
      () => new Promise((res) => { resolveGet = res; })
    );

    render(<UserMemoryModal wsSlug="acme" onClose={vi.fn()} />);

    expect(screen.getByTestId('memory-loading')).not.toBeNull();
    expect(screen.queryByTestId('input-user-memory')).toBeNull();

    resolveGet({ ...memoryPayload });
    await waitFor(() => {
      expect(screen.queryByTestId('memory-loading')).toBeNull();
    });
  });

  it('saves edits via the user memory PUT payload and confirms success', async () => {
    vi.spyOn(api.memory, 'getMine').mockResolvedValue({ ...memoryPayload });
    const updateMine = vi
      .spyOn(api.memory, 'updateMine')
      .mockResolvedValue({ ...memoryPayload, content: 'Updated note.' });

    render(<UserMemoryModal wsSlug="acme" onClose={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByTestId('input-user-memory')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('input-user-memory'), { target: { value: 'Updated note.' } });
    fireEvent.click(screen.getByTestId('btn-memory-save'));

    await waitFor(() => {
      expect(updateMine).toHaveBeenCalledWith('acme', { content: 'Updated note.' });
    });
    await waitFor(() => {
      expect(screen.getByTestId('memory-saved-indicator')).not.toBeNull();
    });
    expect(screen.queryByTestId('memory-save-error')).toBeNull();
  });

  it('keeps the modal open with content intact and surfaces an over-cap 422 inline', async () => {
    vi.spyOn(api.memory, 'getMine').mockResolvedValue({ ...memoryPayload });
    vi.spyOn(api.memory, 'updateMine').mockRejectedValue(
      new ApiError(422, 'invalid_request', 'Memory exceeds the 4096 character limit')
    );

    render(<UserMemoryModal wsSlug="acme" onClose={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByTestId('input-user-memory')).not.toBeNull();
    });
    fireEvent.change(screen.getByTestId('input-user-memory'), {
      target: { value: 'Way too long a memory body' },
    });
    fireEvent.click(screen.getByTestId('btn-memory-save'));

    const err = await waitFor(() => screen.getByTestId('memory-save-error'));
    expect(err.textContent).toContain('4096 character limit');
    // Modal stays open, draft preserved, no success indicator
    expect(screen.getByTestId('modal-user-memory')).not.toBeNull();
    expect((screen.getByTestId('input-user-memory') as HTMLTextAreaElement).value).toBe(
      'Way too long a memory body'
    );
    expect(screen.queryByTestId('memory-saved-indicator')).toBeNull();
  });
});
