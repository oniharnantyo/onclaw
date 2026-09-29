/**
 * @vitest-environment jsdom
 */
// Members source tests (add-right-panel 1.5): ContextPanel's body lives on as
// a registered panel source with its behavior intact — listing, presence and
// kind markers, remove eligibility (the channel's primary agent is not
// removable), and the add-member flow. Registration is a module-load side
// effect of importing ./members, exactly like the built-in generative-UI
// renderers — so this suite imports it and never resets the registries.
// Reference documents (add-reference-documents 9.2): the channel's read-only
// documents section lists the channel lens (channel-attached + promoted) as
// chips whose click opens the document preview in the right panel.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/react';
import { renderPanelSource } from '../../../../lib/panel/registry';
import { documentsApi } from '../../../../lib/documentsApi';
import { useStore } from '../../../../store';
import './members';

vi.mock('../../../../lib/documentsApi', () => ({
  documentsApi: { list: vi.fn().mockResolvedValue({ documents: [] }) },
  indexStatusLabel: (s: string) =>
    s === 'ready' ? 'Indexed' : s === 'no_text_layer' ? 'No text layer' : s === 'processing' ? 'Processing' : 'Unknown',
}));

const tab = { id: 'tab1', kind: 'members', title: 'Members', payload: { chatId: 'ch-ops' }, dedupKey: 'members::{}' };

const ctx = {
  channelMembers: [
    { id: 'a1', kind: 'agent', name: 'Atlas', agent: { status: 'idle' } },
    { id: 'p1', kind: 'person', name: 'Alice', presence: 'online' },
    { id: 'p2', kind: 'person', name: 'Bob', presence: 'away' },
  ],
  memberCandidates: [{ id: 'p3', kind: 'person', name: 'Cara', presence: 'online' }],
  primaryAgentId: 'a1',
  onAddMember: (id: string) => void id,
  onRemoveMember: (id: string) => void id,
  onOpenMember: (id: string) => void id,
};

const doc = (over: Record<string, unknown> = {}) => ({
  id: 'doc-1',
  name: 'incident-runbook.pdf',
  description: 'What to do when it is on fire',
  mime: 'application/pdf',
  size: 1024,
  url: '/files/wk/docs/incident-runbook.pdf',
  indexStatus: 'ready',
  scope: 'attached',
  pageCount: 12,
  agents: [],
  channels: ['ch-ops'],
  createdAt: '2026-09-01T00:00:00Z',
  ...over,
});

const renderMembers = (over: Record<string, unknown> = {}) =>
  render(<div>{renderPanelSource('members', { tab, ctx: { ...ctx, ...over } })}</div>);

beforeEach(() => {
  vi.mocked(documentsApi.list).mockReset().mockResolvedValue({ documents: [] });
  useStore.setState({ panel: { open: false, tabs: [], activeId: null, badge: false } });
});

describe('panel sources/members — registered source behavior (1.5)', () => {
  it('is registered under the members kind', () => {
    expect(renderPanelSource('members', { tab, ctx: {} })).not.toBeNull();
  });

  it('renders member rows with kind and presence markers', () => {
    const { container } = renderMembers();
    expect(container.textContent).toContain('Atlas');
    expect(container.textContent).toContain('Agent');
    expect(container.textContent).toContain('Member · online');
    expect(container.textContent).toContain('Member · away');
  });

  it('remove eligibility: the primary agent is not removable, others are', () => {
    const { container } = renderMembers();
    expect(container.querySelector('[data-od-id="drawer-member-remove-a1"]')).toBeNull();
    expect(container.querySelector('[data-od-id="drawer-member-remove-p1"]')).not.toBeNull();
  });

  it('the add-member flow lists eligible candidates and empties out when none remain', () => {
    const { container } = renderMembers();
    fireEvent.click(container.querySelector('[data-od-id="btn-add-member"]') as HTMLButtonElement);
    expect(container.querySelector('[data-od-id="add-member-list"]')).not.toBeNull();
    expect(container.querySelector('[data-od-id="add-member-p3"]')).not.toBeNull();

    const empty = renderMembers({ memberCandidates: [] });
    fireEvent.click(empty.container.querySelector('[data-od-id="btn-add-member"]') as HTMLButtonElement);
    expect(empty.container.textContent).toContain('Everyone here is already a member.');
  });

  it('an empty channel says so instead of rendering a blank pane', () => {
    const { container } = renderMembers({ channelMembers: [] });
    expect(container.textContent).toContain('No members yet.');
  });

  it('clicking add and remove routes through the context callbacks', () => {
    const added: string[] = [];
    const removed: string[] = [];
    const { container } = renderMembers({
      onAddMember: (id: string) => added.push(id),
      onRemoveMember: (id: string) => removed.push(id),
    });
    fireEvent.click(container.querySelector('[data-od-id="drawer-member-remove-p2"]') as HTMLButtonElement);
    fireEvent.click(container.querySelector('[data-od-id="btn-add-member"]') as HTMLButtonElement);
    fireEvent.click(container.querySelector('[data-od-id="add-member-p3"]') as HTMLButtonElement);
    expect(removed).toEqual(['p2']);
    expect(added).toEqual(['p3']);
  });
});

describe('panel sources/members — channel reference documents (9.2)', () => {
  it('lists the channel lens result as read-only chips with scope + index markers', async () => {
    vi.mocked(documentsApi.list).mockResolvedValueOnce({
      documents: [
        doc(),
        doc({ id: 'doc-2', name: 'postmortems.md', scope: 'workspace', channels: [] }),
      ],
    } as any);
    const { container } = renderMembers();
    await waitFor(() => {
      expect(container.querySelector('[data-testid="channel-documents-section"]')).not.toBeNull();
    });
    // The lens rode the channel id from the tab payload.
    expect(documentsApi.list).toHaveBeenCalledWith(expect.any(String), { channel: 'ch-ops' });
    const runbook = container.querySelector('[data-testid="channel-document-doc-1"]')!;
    expect(runbook.textContent).toContain('incident-runbook.pdf');
    expect(runbook.textContent).toContain('Indexed');
    // Promoted documents carry the workspace-scope badge; attached ones do not.
    expect(runbook.textContent).not.toContain('All agents');
    expect(container.querySelector('[data-testid="channel-document-doc-2"]')!.textContent).toContain('All agents');
  });

  it('a chip click opens the document preview tab with the capability URL', async () => {
    vi.mocked(documentsApi.list).mockResolvedValueOnce({ documents: [doc()] } as any);
    const { container } = renderMembers();
    await waitFor(() => {
      expect(container.querySelector('[data-testid="channel-document-doc-1"]')).not.toBeNull();
    });
    fireEvent.click(container.querySelector('[data-testid="channel-document-doc-1"]')!);
    const panel = useStore.getState().panel;
    expect(panel.open).toBe(true);
    expect(panel.tabs).toHaveLength(1);
    expect(panel.tabs[0].kind).toBe('document');
    expect(panel.tabs[0].title).toBe('incident-runbook.pdf');
    expect(panel.tabs[0].payload).toEqual({
      name: 'incident-runbook.pdf',
      url: '/files/wk/docs/incident-runbook.pdf',
    });
  });

  it('an empty lens renders the empty state pointing at Settings → Documents', async () => {
    const { container } = renderMembers();
    await waitFor(() => {
      expect(container.querySelector('[data-testid="channel-documents-section"]')).not.toBeNull();
    });
    expect(container.querySelector('[data-testid="channel-documents-empty"]')!.textContent).toContain(
      'Settings → Documents'
    );
  });

  it('a failed lens fetch leaves the section out entirely', async () => {
    vi.mocked(documentsApi.list).mockRejectedValueOnce(new Error('offline'));
    const { container } = renderMembers();
    await waitFor(() => {
      expect(documentsApi.list).toHaveBeenCalled();
    });
    expect(container.querySelector('[data-testid="channel-documents-section"]')).toBeNull();
  });
});
