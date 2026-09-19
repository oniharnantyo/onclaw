import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryIngestedChip, memoryChipSummary } from './MemoryIngestedChip';
import { api, type ApiMemoryNote } from '../../lib/api';

const chipMessage = {
  id: 'h-mem-1',
  author: 'memory',
  ts: '2026-09-16T01:00:00Z',
  text: '',
  memory: {
    noteIds: ['11111111-aaaa-4bbb-8ccc-000000000001', '22222222-aaaa-4bbb-8ccc-000000000002'],
    eventIds: [],
    counts: { shared: 2, user: 1, agent: 0 },
  },
};

// A fact whose content would leak if the drawer ever rendered it — the chip
// is counts + provenance only (D11).
const detailNote = (overrides: Partial<ApiMemoryNote> = {}): ApiMemoryNote => ({
  id: '11111111-aaaa-4bbb-8ccc-000000000001',
  workspace_id: 'ws-1',
  visibility: 'shared',
  user_id: null,
  agent_id: null,
  origin: 'dialogue',
  event_time: '2026-09-16T00:59:00Z',
  learned_at: '2026-09-16T01:00:05Z',
  source_event_id: 'src-event-0001',
  content: 'SECRET-INVOICE-CONTENT that must never render in the chip',
  importance: 5,
  pinned: false,
  topic: null,
  conflict_flag: null,
  supersedes: null,
  superseded_by: null,
  promoted_by: null,
  promoted_at: null,
  tombstoned_at: null,
  ...overrides,
});

describe('components/chat/MemoryIngestedChip', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders counts and the visibility breakdown, never content', () => {
    const { container } = render(<MemoryIngestedChip m={chipMessage} workspaceId="acme" />);

    const chip = screen.getByTestId('memory-chip-h-mem-1');
    expect(chip.textContent).toContain('Remembered');
    expect(chip.textContent).toContain('2 shared · 1 private');
    // No fact content anywhere, even before the drawer opens.
    expect(container.textContent).not.toContain('SECRET-INVOICE-CONTENT');
  });

  it('summary formatting covers each tier and the empty map', () => {
    expect(memoryChipSummary({ shared: 2, user: 1, agent: 0 })).toBe('2 shared · 1 private');
    expect(memoryChipSummary({ shared: 0, user: 0, agent: 3 })).toBe('3 agent');
    expect(memoryChipSummary({ shared: 0, user: 0, agent: 0 })).toBe('');
  });

  it('click opens the provenance drawer with ids and provenance, still without content', async () => {
    vi.spyOn(api.memory, 'note').mockImplementation(async (_ws, id) => ({
      note: detailNote({ id, visibility: id.startsWith('2') ? 'user' : 'shared' }),
      evidence: [],
    }));
    render(<MemoryIngestedChip m={chipMessage} workspaceId="acme" />);

    fireEvent.click(screen.getByTestId('memory-chip-toggle-h-mem-1'));

    await waitFor(() => {
      expect(screen.getByTestId('memory-chip-row-11111111-aaaa-4bbb-8ccc-000000000001')).not.toBeNull();
    });
    // Both facts listed with their provenance (origin, source event).
    const row = screen.getByTestId('memory-chip-row-11111111-aaaa-4bbb-8ccc-000000000001');
    expect(row.textContent).toContain('dialogue');
    expect(row.textContent).toContain('src-even');
    expect(row.textContent).toContain('shared');
    expect(screen.getByTestId('memory-chip-row-22222222-aaaa-4bbb-8ccc-000000000002').textContent).toContain('user');
    // The drawer proves content was fetched but never rendered.
    expect(screen.getByTestId('memory-chip-h-mem-1').textContent).not.toContain('SECRET-INVOICE-CONTENT');
  });

  it('deletes a fact from the drawer and drops its row', async () => {
    vi.spyOn(api.memory, 'note').mockImplementation(async (_ws, id) => ({
      note: detailNote({ id }),
      evidence: [],
    }));
    const del = vi.spyOn(api.memory, 'deleteNote').mockResolvedValue(undefined);
    const onToast = vi.fn();
    render(<MemoryIngestedChip m={chipMessage} workspaceId="acme" onToast={onToast} />);

    fireEvent.click(screen.getByTestId('memory-chip-toggle-h-mem-1'));
    await waitFor(() => {
      expect(screen.getByTestId('memory-chip-delete-11111111-aaaa-4bbb-8ccc-000000000001')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('memory-chip-delete-11111111-aaaa-4bbb-8ccc-000000000001'));

    await waitFor(() => {
      expect(del).toHaveBeenCalledWith('acme', '11111111-aaaa-4bbb-8ccc-000000000001');
    });
    await waitFor(() => {
      expect(screen.queryByTestId('memory-chip-row-11111111-aaaa-4bbb-8ccc-000000000001')).toBeNull();
    });
    expect(onToast).toHaveBeenCalledWith('Fact deleted — hidden everywhere, the deletion is recorded');
  });

  it('hydration parity: the same entry shape renders identically after a remount', async () => {
    vi.spyOn(api.memory, 'note').mockImplementation(async (_ws, id) => ({
      note: detailNote({ id }),
      evidence: [],
    }));
    // The hydrated reload mounts a fresh chip with the translator-minted
    // entry (identical shape, regenerated id prefix) — the visible summary
    // and drawer behavior must be indistinguishable from the live turn.
    const hydrated = {
      ...chipMessage,
      id: 'cu-mem-9',
      memory: { ...chipMessage.memory },
    };
    const first = render(<MemoryIngestedChip m={chipMessage} workspaceId="acme" />);
    const liveSummary = screen.getByTestId('memory-chip-toggle-h-mem-1').textContent;
    first.unmount();

    render(<MemoryIngestedChip m={hydrated} workspaceId="acme" />);
    expect(screen.getByTestId('memory-chip-toggle-cu-mem-9').textContent).toBe(liveSummary);

    fireEvent.click(screen.getByTestId('memory-chip-toggle-cu-mem-9'));
    await waitFor(() => {
      expect(screen.getByTestId('memory-chip-row-11111111-aaaa-4bbb-8ccc-000000000001')).not.toBeNull();
    });
  });

  it('renders nothing for an empty payload', () => {
    const { container } = render(
      <MemoryIngestedChip
        m={{ id: 'x', author: 'memory', memory: { noteIds: [], eventIds: [], counts: { shared: 0, user: 0, agent: 0 } } }}
        workspaceId="acme"
      />
    );
    expect(container.querySelector('[data-role="memory-chip"]')).toBeNull();
  });
});
