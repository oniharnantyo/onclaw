import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, createEvent } from '@testing-library/react';
import { ChatView } from './ChatView';
import { useStore } from '../../store';

// Only the upload call is mocked; pre-checks and naming stay real.
const { uploadMock } = vi.hoisted(() => ({ uploadMock: vi.fn() }));
vi.mock('../../lib/attachments', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/attachments')>();
  return { ...actual, uploadAttachment: uploadMock };
});
vi.mock('../../lib/api', () => ({
  api: {
    // onUnauthorized is required by the store's import chain (store/auth
    // wires it at module scope).
    onUnauthorized: () => () => {},
    skills: { list: vi.fn(async () => ({ skills: [] })) },
    agents: { listSkills: vi.fn(async () => ({ skills: [] })) },
  },
}));
vi.mock('../../lib/toolCatalog', () => ({
  toolCatalog: { ensure: vi.fn(async () => ({})) },
}));

const agent = { id: 'a-atlas', slug: 'atlas', name: 'Atlas', role: 'Ops copilot', status: 'idle', lastActive: '2h ago' };
const tenant = { id: 'acme', name: 'Acme', agents: [agent] };

const png = (name: string) => new File([new Uint8Array(8)], name, { type: 'image/png' });

const renderChatView = (override: Record<string, unknown> = {}) =>
  render(
    <ChatView
      tenant={tenant}
      target={{ kind: 'agent', obj: agent }}
      agent={agent}
      thread={[]}
      session={null}
      channelMembers={null}
      typing={false}
      busy={false}
      compacting={false}
      onOpenMembers={vi.fn()}
      onSend={vi.fn()}
      onCancel={vi.fn()}
      onCopy={vi.fn()}
      onRefresh={vi.fn()}
      onBranch={vi.fn()}
      onEditSubmit={vi.fn()}
      allowAttachments
      {...override}
    />
  );

const surface = () => screen.getByLabelText(/conversation with atlas/i);
const chips = () => screen.queryAllByTestId('attachment-chip');

beforeEach(() => {
  vi.clearAllMocks();
  uploadMock.mockImplementation(() => new Promise(() => {})); // stay "uploading"
  useStore.setState({ toast: vi.fn() });
});
describe('ChatView drag-drop attach surface (add-chat-attachments gallery F)', () => {
  it('shows the dashed "Drop to attach" overlay for file drags only', () => {
    renderChatView();
    expect(screen.queryByTestId('drop-overlay')).toBeNull();

    // Text drags never trigger the overlay (types gate, design D14).
    fireEvent.dragEnter(surface(), { dataTransfer: { types: ['text/plain'] } });
    expect(screen.queryByTestId('drop-overlay')).toBeNull();

    fireEvent.dragEnter(surface(), { dataTransfer: { types: ['Files'] } });
    const overlay = screen.getByTestId('drop-overlay');
    expect(overlay.textContent).toContain('Drop to attach');

    fireEvent.dragLeave(surface(), { dataTransfer: { types: ['Files'] } });
    expect(screen.queryByTestId('drop-overlay')).toBeNull();
  });

  it('keeps the overlay through nested drag boundaries (enter/leave counter)', () => {
    renderChatView();
    fireEvent.dragEnter(surface(), { dataTransfer: { types: ['Files'] } });
    fireEvent.dragEnter(surface(), { dataTransfer: { types: ['Files'] } });
    fireEvent.dragLeave(surface(), { dataTransfer: { types: ['Files'] } });
    expect(screen.getByTestId('drop-overlay')).not.toBeNull();
    fireEvent.dragLeave(surface(), { dataTransfer: { types: ['Files'] } });
    expect(screen.queryByTestId('drop-overlay')).toBeNull();
  });

  it('preventDefaults dragover AND drop — the browser must not navigate to the file', () => {
    renderChatView();
    const node = surface();

    const overEvent = createEvent.dragOver(node, { dataTransfer: { types: ['Files'] } });
    const overPrevent = vi.spyOn(overEvent, 'preventDefault');
    fireEvent(node, overEvent);
    expect(overPrevent).toHaveBeenCalledTimes(1);

    const dropEvent = createEvent.drop(node, {
      dataTransfer: { types: ['Files'], files: [png('one.png'), png('two.png')], items: [] },
    });
    const dropPrevent = vi.spyOn(dropEvent, 'preventDefault');
    fireEvent(node, dropEvent);
    expect(dropPrevent).toHaveBeenCalledTimes(1);
  });

  it('dropping two files hands both to the Composer tray as uploading chips', () => {
    renderChatView();
    const one = png('one.png');
    const two = png('two.png');
    fireEvent.dragEnter(surface(), { dataTransfer: { types: ['Files'] } });
    fireEvent.drop(surface(), {
      dataTransfer: { types: ['Files'], files: [one, two], items: [] },
    });

    expect(chips()).toHaveLength(2);
    expect(chips().map((c) => c.dataset.chipState)).toEqual(['uploading', 'uploading']);
    expect(screen.getByText('one.png')).not.toBeNull();
    expect(screen.getByText('two.png')).not.toBeNull();
    // Uploads go to the workspace slug through the shared upload client.
    expect(uploadMock).toHaveBeenCalledWith('acme', one, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(uploadMock).toHaveBeenCalledWith('acme', two, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    // The overlay clears once the drop lands.
    expect(screen.queryByTestId('drop-overlay')).toBeNull();
  });

  it('rejects folder drops with an explicit toast (Firefox shape: zero-byte type-less File)', () => {
    const toast = vi.fn();
    useStore.setState({ toast });
    renderChatView();
    fireEvent.drop(surface(), {
      dataTransfer: { types: ['Files'], files: [new File([], 'screenshots')], items: [] },
    });
    expect(toast).toHaveBeenCalledWith("Folders can't be attached — drop files instead");
    expect(chips()).toHaveLength(0);
    expect(uploadMock).not.toHaveBeenCalled();
  });

  it('rejects folder drops with an explicit toast (Chrome shape: no files, file-kind items)', () => {
    const toast = vi.fn();
    useStore.setState({ toast });
    renderChatView();
    fireEvent.drop(surface(), {
      dataTransfer: { types: ['Files'], files: [], items: [{ kind: 'file' }] },
    });
    expect(toast).toHaveBeenCalledWith("Folders can't be attached — drop files instead");
    expect(chips()).toHaveLength(0);
  });

  it('ignores file drags entirely when attachments are not allowed (channels/teams)', () => {
    const toast = vi.fn();
    useStore.setState({ toast });
    renderChatView({ allowAttachments: false });
    fireEvent.dragEnter(surface(), { dataTransfer: { types: ['Files'] } });
    expect(screen.queryByTestId('drop-overlay')).toBeNull();
    fireEvent.drop(surface(), {
      dataTransfer: { types: ['Files'], files: [png('one.png')], items: [] },
    });
    expect(chips()).toHaveLength(0);
    expect(uploadMock).not.toHaveBeenCalled();
    expect(toast).not.toHaveBeenCalled();
    // And the composer carries no attach affordance at all.
    expect(screen.queryByTestId('btn-attach')).toBeNull();
  });
});
