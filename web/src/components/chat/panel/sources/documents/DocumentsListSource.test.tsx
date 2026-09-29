/**
 * @vitest-environment jsdom
 */
// Documents listing source tests (add-reference-documents task 10.5): the
// panel pane lists the conversation-visible documents through the same lens
// rule as the composer popover, opens the document preview on click, and
// degrades to explicit empty/error states. Registration is a module-load side
// effect of importing ./index, exactly like the other built-in sources.
import { describe, it, expect, vi, afterEach } from 'vitest';
import { fireEvent, render, waitFor } from '@testing-library/react';
import { renderPanelSource } from '../../../../../lib/panel/registry';
import { documentsApi } from '../../../../../lib/documentsApi';
import type { ApiReferenceDocument } from '../../../../../lib/documentsApi';
import { useStore } from '../../../../../store';
import './index';

const doc = (over: Partial<ApiReferenceDocument> = {}): ApiReferenceDocument => ({
  id: 'doc-1',
  name: 'twilio-api.pdf',
  description: 'Twilio API manual.',
  mime: 'application/pdf',
  size: 2048,
  url: '/api/v1/workspaces/acme/documents/doc-1/file',
  indexStatus: 'ready',
  scope: 'workspace',
  pageCount: 31,
  agents: [],
  channels: [],
  createdAt: '2026-09-01T00:00:00Z',
  ...over,
});

const tab = { id: 't1', kind: 'documents', title: 'Documents', payload: {}, dedupKey: 'documents::{}' };

const renderSource = (ctx: Record<string, unknown>) =>
  render(<div>{renderPanelSource('documents', { tab, ctx } as any)}</div>);

describe('panel sources/documents — registered listing source (10.5)', () => {
  const originalOpenPanelTab = useStore.getState().openPanelTab;
  afterEach(() => {
    vi.restoreAllMocks();
    useStore.setState({ openPanelTab: originalOpenPanelTab } as any);
  });

  it('is registered under the documents kind', () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [] });
    expect(renderPanelSource('documents', { tab, ctx: {} })).not.toBeNull();
  });

  it('lists rows with name, description, scope badge, and page count', async () => {
    const list = vi.spyOn(documentsApi, 'list').mockResolvedValue({
      documents: [doc(), doc({ id: 'doc-2', name: 'runbook.md', description: '', scope: 'attached', agents: ['a1'], pageCount: 0, indexStatus: 'no_text_layer' })],
    });
    const { container } = renderSource({ workspaceId: 'acme', documentsLens: { agent: 'a1' } });

    await waitFor(() => {
      expect(container.querySelector('[data-testid="panel-doc-twilio-api.pdf"]')).not.toBeNull();
    });
    expect(list).toHaveBeenCalledWith('acme', { agent: 'a1' });
    const row = container.querySelector('[data-testid="panel-doc-twilio-api.pdf"]')!;
    expect(row.textContent).toContain('Twilio API manual.');
    expect(row.textContent).toContain('ALL AGENTS');
    expect(row.textContent).toContain('31 pages');
    const second = container.querySelector('[data-testid="panel-doc-runbook.md"]')!;
    expect(second.textContent).toContain('1 agent · 0 channels');
    expect(second.textContent).not.toContain('pages');
  });

  it('channels resolve through the channel lens', async () => {
    const list = vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [] });
    const { container } = renderSource({ workspaceId: 'acme', documentsLens: { channel: 'c1' } });

    await waitFor(() => {
      expect(list).toHaveBeenCalledWith('acme', { channel: 'c1' });
    });
    await waitFor(() => {
      expect(container.querySelector('[data-testid="panel-documents-empty"]')).not.toBeNull();
    });
  });

  it('a row click opens the document preview tab through the context callback', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [doc()] });
    const openPanelTab = vi.fn();
    const { container } = renderSource({ workspaceId: 'acme', documentsLens: { agent: 'a1' }, openPanelTab });

    await waitFor(() => {
      expect(container.querySelector('[data-testid="panel-doc-twilio-api.pdf"]')).not.toBeNull();
    });
    fireEvent.click(container.querySelector('[data-testid="panel-doc-twilio-api.pdf"]')!);

    expect(openPanelTab).toHaveBeenCalledWith({
      kind: 'document',
      title: 'twilio-api.pdf',
      payload: { name: 'twilio-api.pdf', url: doc().url },
    });
  });

  it('without a context callback the click falls through to the store action', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [doc()] });
    const openPanelTab = vi.fn();
    useStore.setState({ openPanelTab } as any);
    const { container } = renderSource({ workspaceId: 'acme', documentsLens: { agent: 'a1' } });

    await waitFor(() => {
      expect(container.querySelector('[data-testid="panel-doc-twilio-api.pdf"]')).not.toBeNull();
    });
    fireEvent.click(container.querySelector('[data-testid="panel-doc-twilio-api.pdf"]')!);
    expect(openPanelTab).toHaveBeenCalledTimes(1);
  });

  it('an empty visible set renders the empty state, a failure the error state', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [] });
    const { container } = renderSource({ workspaceId: 'acme', documentsLens: { agent: 'a1' } });
    await waitFor(() => {
      expect(container.querySelector('[data-testid="panel-documents-empty"]')).not.toBeNull();
    });

    vi.spyOn(documentsApi, 'list').mockRejectedValue(new Error('Network connection failed'));
    const err = renderSource({ workspaceId: 'acme', documentsLens: { agent: 'a1' } });
    await waitFor(() => {
      expect(err.container.querySelector('[data-testid="panel-documents-error"]')).not.toBeNull();
    });
    expect(err.container.textContent).toContain('Network connection failed');
  });

  it('no conversation in scope renders the unavailable state and never fetches', () => {
    const list = vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [] });
    const { container } = renderSource({ workspaceId: '' });
    expect(container.querySelector('[data-od-id="panel-documents-unavailable"]')).not.toBeNull();
    expect(list).not.toHaveBeenCalled();
  });
});

describe('panel sources/documents — insert-as-mention row action (rework-document-chat-surfaces 2.3)', () => {
  const originalOpenPanelTab = useStore.getState().openPanelTab;
  afterEach(() => {
    vi.restoreAllMocks();
    useStore.setState({ openPanelTab: originalOpenPanelTab } as any);
  });

  it('the row Insert action hands the document identity to the bridge without opening the preview', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [doc()] });
    const insertDocumentMention = vi.fn();
    const openPanelTab = vi.fn();
    const { container } = renderSource({
      workspaceId: 'acme', documentsLens: { agent: 'a1' }, openPanelTab, insertDocumentMention,
    });

    await waitFor(() => {
      expect(container.querySelector('[data-testid="panel-doc-twilio-api.pdf"]')).not.toBeNull();
    });
    fireEvent.click(container.querySelector('[data-testid="panel-doc-insert-twilio-api.pdf"]')!);

    expect(insertDocumentMention).toHaveBeenCalledTimes(1);
    expect(insertDocumentMention).toHaveBeenCalledWith({ id: 'doc-1', name: 'twilio-api.pdf' });
    // D4: preview stays the row's primary click — Insert never opens the tab.
    expect(openPanelTab).not.toHaveBeenCalled();
  });

  it('a row click still opens the preview tab and never inserts', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [doc()] });
    const insertDocumentMention = vi.fn();
    const openPanelTab = vi.fn();
    const { container } = renderSource({
      workspaceId: 'acme', documentsLens: { agent: 'a1' }, openPanelTab, insertDocumentMention,
    });

    await waitFor(() => {
      expect(container.querySelector('[data-testid="panel-doc-twilio-api.pdf"]')).not.toBeNull();
    });
    fireEvent.click(container.querySelector('[data-testid="panel-doc-twilio-api.pdf"]')!);

    expect(openPanelTab).toHaveBeenCalledWith({
      kind: 'document',
      title: 'twilio-api.pdf',
      payload: { name: 'twilio-api.pdf', url: doc().url },
    });
    expect(insertDocumentMention).not.toHaveBeenCalled();
  });

  it('without the bridge the Insert action is hidden entirely', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [doc()] });
    const { container } = renderSource({ workspaceId: 'acme', documentsLens: { agent: 'a1' } });

    await waitFor(() => {
      expect(container.querySelector('[data-testid="panel-doc-twilio-api.pdf"]')).not.toBeNull();
    });
    expect(container.querySelector('[data-testid="panel-doc-insert-twilio-api.pdf"]')).toBeNull();
  });
});
