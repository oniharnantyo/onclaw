import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { DocumentsPane } from './DocumentsPane';
import { documentsApi, type ApiReferenceDocument } from '../../lib/documentsApi';
import { api } from '../../lib/api';
import { UploadError } from '../../lib/attachments';
import { useStore } from '../../store';

const mockTenant = { id: 'acme', sub: 'acme', name: 'Acme Corp' };

const runbook: ApiReferenceDocument = {
  id: 'doc-1',
  name: 'runbook',
  description: 'Incident response runbook.',
  mime: 'application/pdf',
  size: 2048,
  url: '/files/wk_1/documents/doc-1/runbook.pdf',
  indexStatus: 'ready',
  scope: 'attached',
  pageCount: 12,
  agents: ['agent-1', 'agent-2'],
  channels: ['ch-1'],
  createdAt: '2026-09-01T00:00:00Z',
};

const sop: ApiReferenceDocument = {
  id: 'doc-2',
  name: 'sop',
  description: 'Deploy SOP.',
  mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  size: 4096,
  url: '/files/wk_1/documents/doc-2/sop.docx',
  indexStatus: 'no_text_layer',
  scope: 'workspace',
  pageCount: 0,
  agents: [],
  channels: [],
  createdAt: '2026-09-02T00:00:00Z',
};

const scanning: ApiReferenceDocument = { ...runbook, id: 'doc-3', name: 'scanned', indexStatus: 'processing' };

const rows = (): { documents: ApiReferenceDocument[] } => ({ documents: [runbook, sop] });

const agentsList = () =>
  Promise.resolve({
    agents: [
      { id: 'agent-1', slug: 'atlas', name: 'Atlas' },
      { id: 'agent-2', slug: 'beacon', name: 'Beacon' },
    ] as any[],
  });

const channelsList = () =>
  Promise.resolve({ channels: [{ id: 'ch-1', slug: 'ops', name: 'Ops' }] as any[] });

describe('screens/settings/DocumentsPane', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useStore.setState({ panel: { open: false, tabs: [], activeId: null, badge: false } } as any);
  });

  it('renders rows with index-status badges, scope badges, and page counts', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue(rows());

    render(<DocumentsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('document-runbook')).not.toBeNull();
    });
    expect(screen.getByTestId('document-sop')).not.toBeNull();
    expect(screen.getByTestId('doc-index-ready').textContent).toBe('Indexed');
    expect(screen.getByTestId('doc-index-no_text_layer').textContent).toBe('No text layer');
    // attached scope shows the counts; workspace scope shows ALL AGENTS
    expect(screen.getByText('2 agents · 1 channel')).not.toBeNull();
    expect(screen.getByText('ALL AGENTS')).not.toBeNull();
    expect(screen.getByText('12 pages')).not.toBeNull();
  });

  it('renders the processing badge while the index pipeline runs', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [scanning] });

    render(<DocumentsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('doc-index-processing')).not.toBeNull();
    });
  });

  it('uploads through the picker with a progress strip and lands the row', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [] });
    const upload = vi.spyOn(documentsApi, 'upload').mockResolvedValue(runbook);
    const onToast = vi.fn();

    render(<DocumentsPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('documents-empty')).not.toBeNull();
    });
    const input = screen.getByTestId('input-upload-document');
    const file = new File([new Uint8Array(10)], 'runbook.pdf', { type: 'application/pdf' });
    Object.defineProperty(input, 'files', { value: [file] });
    fireEvent.change(input);

    await waitFor(() => {
      expect(upload).toHaveBeenCalledWith('acme', file, {}, expect.any(Object));
    });
    await waitFor(() => {
      expect(screen.getByTestId('document-runbook')).not.toBeNull();
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('runbook uploaded');
    });
    // The strip row clears when the upload settles.
    expect(screen.queryByTestId('upload-progress-runbook.pdf')).toBeNull();
  });

  it('rejects disallowed files client-side with the precheck reason', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [] });
    const upload = vi.spyOn(documentsApi, 'upload');
    const onToast = vi.fn();

    render(<DocumentsPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('documents-empty')).not.toBeNull();
    });
    const input = screen.getByTestId('input-upload-document');
    Object.defineProperty(input, 'files', {
      value: [new File([new Uint8Array(4)], 'virus.exe', { type: 'application/octet-stream' })],
    });
    fireEvent.change(input);

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith(expect.stringMatching(/virus\.exe: .*isn't supported/), 'danger');
    });
    expect(upload).not.toHaveBeenCalled();
  });

  it('toasts a server upload rejection without crashing', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [] });
    vi.spyOn(documentsApi, 'upload').mockRejectedValue(new UploadError(400, 'unsupported mime type'));
    const onToast = vi.fn();

    render(<DocumentsPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('documents-empty')).not.toBeNull();
    });
    const input = screen.getByTestId('input-upload-document');
    Object.defineProperty(input, 'files', {
      value: [new File([new Uint8Array(4)], 'notes.md', { type: 'text/markdown' })],
    });
    fireEvent.change(input);

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('unsupported mime type', 'danger');
    });
  });

  it('drops files onto the dropzone and uploads them', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [] });
    const upload = vi.spyOn(documentsApi, 'upload').mockResolvedValue(runbook);

    render(<DocumentsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('documents-empty')).not.toBeNull();
    });
    const zone = screen.getByTestId('documents-dropzone');
    const file = new File([new Uint8Array(6)], 'handbook.pdf', { type: 'application/pdf' });
    fireEvent.drop(zone, { dataTransfer: { files: [file] } });

    await waitFor(() => {
      expect(upload).toHaveBeenCalledWith('acme', file, {}, expect.any(Object));
    });
    await waitFor(() => {
      expect(screen.getByTestId('document-runbook')).not.toBeNull();
    });
  });

  it('saves edits through patch and drives the attach sets only when they change', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue(rows());
    vi.spyOn(api.agents, 'list').mockImplementation(agentsList);
    vi.spyOn(api.channels, 'list').mockImplementation(channelsList);
    const patch = vi.spyOn(documentsApi, 'patch').mockResolvedValue({ ...runbook, description: 'Updated.' });
    const setAgents = vi.spyOn(documentsApi, 'setAgents').mockResolvedValue({
      ...runbook,
      description: 'Updated.',
      agents: ['agent-1'],
    });
    const setChannels = vi.spyOn(documentsApi, 'setChannels');
    const onToast = vi.fn();

    render(<DocumentsPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-edit-runbook')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-edit-runbook'));
    expect(screen.getByTestId('modal-document-edit')).not.toBeNull();

    // attach editor loaded the workspace agents and channels
    await waitFor(() => {
      expect(screen.getByTestId('attach-agent-atlas')).not.toBeNull();
    });
    expect((screen.getByTestId('attach-agent-atlas') as HTMLInputElement).checked).toBe(true);
    expect((screen.getByTestId('attach-agent-beacon') as HTMLInputElement).checked).toBe(true);

    // detach beacon; channels untouched
    fireEvent.click(screen.getByTestId('attach-agent-beacon'));
    fireEvent.click(screen.getByTestId('btn-document-edit-save'));

    await waitFor(() => {
      expect(patch).toHaveBeenCalledWith('acme', 'doc-1', { name: 'runbook', description: 'Incident response runbook.' });
    });
    await waitFor(() => {
      expect(setAgents).toHaveBeenCalledWith('acme', 'doc-1', ['agent-1']);
    });
    expect(setChannels).not.toHaveBeenCalled();
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('runbook saved');
    });
  });

  it('renames through the edit dialog and patches the row in place', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue(rows());
    vi.spyOn(api.agents, 'list').mockResolvedValue({ agents: [] });
    vi.spyOn(api.channels, 'list').mockResolvedValue({ channels: [] });
    vi.spyOn(documentsApi, 'patch').mockResolvedValue({ ...runbook, name: 'runbook-v2' });

    render(<DocumentsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-edit-runbook')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-edit-runbook'));
    fireEvent.change(screen.getByTestId('input-document-name'), { target: { value: 'runbook-v2' } });
    fireEvent.click(screen.getByTestId('btn-document-edit-save'));

    await waitFor(() => {
      expect(screen.getByTestId('document-runbook-v2')).not.toBeNull();
    });
    expect(screen.queryByTestId('document-runbook')).toBeNull();
  });

  it('deletes through a confirm dialog', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue(rows());
    const remove = vi.spyOn(documentsApi, 'remove').mockResolvedValue(undefined);
    const onToast = vi.fn();

    render(<DocumentsPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-delete-runbook')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-delete-runbook'));
    expect(screen.getByTestId('modal-document-delete')).not.toBeNull();
    fireEvent.click(screen.getByTestId('btn-delete-confirm'));

    await waitFor(() => {
      expect(remove).toHaveBeenCalledWith('acme', 'doc-1');
    });
    await waitFor(() => {
      expect(screen.queryByTestId('document-runbook')).toBeNull();
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('runbook deleted');
    });
  });

  it('shows the promote toggle only for admins and flips the scope', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue(rows());
    const promote = vi.spyOn(documentsApi, 'promote').mockResolvedValue({ ...runbook, scope: 'workspace' });
    const demote = vi.spyOn(documentsApi, 'demote').mockResolvedValue({ ...sop, scope: 'attached' });
    const onToast = vi.fn();

    render(<DocumentsPane tenant={mockTenant} onToast={onToast} canPromote />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-promote-runbook')).not.toBeNull();
    });
    // workspace-scope row offers demote instead
    expect(screen.getByTestId('btn-demote-sop')).not.toBeNull();
    expect(screen.queryByTestId('btn-demote-runbook')).toBeNull();

    fireEvent.click(screen.getByTestId('btn-promote-runbook'));
    await waitFor(() => {
      expect(promote).toHaveBeenCalledWith('acme', 'doc-1');
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('runbook promoted — visible to every agent in this workspace');
    });

    fireEvent.click(screen.getByTestId('btn-demote-sop'));
    await waitFor(() => {
      expect(demote).toHaveBeenCalledWith('acme', 'doc-2');
    });
  });

  it('hides promote/demote for members (reference_documents.promote only)', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue(rows());

    render(<DocumentsPane tenant={mockTenant} canPromote={false} />);

    await waitFor(() => {
      expect(screen.getByTestId('document-runbook')).not.toBeNull();
    });
    expect(screen.queryByTestId('btn-promote-runbook')).toBeNull();
    expect(screen.queryByTestId('btn-demote-sop')).toBeNull();
    // upload/edit/delete ride ordinary membership — still present
    expect(screen.getByTestId('btn-upload-document')).not.toBeNull();
    expect(screen.getByTestId('btn-edit-runbook')).not.toBeNull();
  });

  it('opens the capability-URL preview in a standalone modal (1.1)', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue(rows());

    render(<DocumentsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-preview-runbook')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-preview-runbook'));

    // The document source renders inside the modal. 'runbook' carries no
    // extension, so the source dispatches to its degrade card — the payload
    // still rides through (name in the caption, capability URL on download).
    const modal = screen.getByTestId('modal-document-preview');
    expect(modal).not.toBeNull();
    const card = modal.querySelector('[data-od-id="panel-degrade-card"]');
    expect(card).not.toBeNull();
    expect(card!.textContent).toContain('runbook');
    const dl = modal.querySelector(
      'a[data-od-id="panel-degrade-download"]'
    ) as HTMLAnchorElement;
    expect(dl.getAttribute('href')).toContain('/files/wk_1/documents/doc-1/runbook.pdf');

    // Panel-less surface: the panel slice stays untouched — nothing to
    // navigate to and no tab minted.
    const state = useStore.getState() as any;
    expect(state.panel.open).toBe(false);
    expect(state.panel.tabs).toHaveLength(0);

    // Closing the modal returns to the pane.
    fireEvent.click(screen.getByLabelText('Close dialog'));
    await waitFor(() => {
      expect(screen.queryByTestId('modal-document-preview')).toBeNull();
    });
    expect(screen.getByTestId('documents-library')).not.toBeNull();
  });

  it('replaces content through the file picker and re-indexes in place', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue(rows());
    const replace = vi
      .spyOn(documentsApi, 'replace')
      .mockResolvedValue({ ...runbook, size: 99, pageCount: 14 });
    const onToast = vi.fn();

    render(<DocumentsPane tenant={mockTenant} onToast={onToast} />);

    await waitFor(() => {
      expect(screen.getByTestId('btn-replace-runbook')).not.toBeNull();
    });
    fireEvent.click(screen.getByTestId('btn-replace-runbook'));

    const input = screen.getByTestId('input-replace-document');
    const file = new File([new Uint8Array(9)], 'runbook.pdf', { type: 'application/pdf' });
    Object.defineProperty(input, 'files', { value: [file] });
    fireEvent.change(input);

    await waitFor(() => {
      expect(replace).toHaveBeenCalledWith('acme', 'doc-1', file, expect.any(Object));
    });
    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('runbook replaced — content re-indexed');
    });
    expect(screen.getByText('14 pages')).not.toBeNull();
  });

  it('renders the empty state with an upload affordance', async () => {
    vi.spyOn(documentsApi, 'list').mockResolvedValue({ documents: [] });

    render(<DocumentsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByTestId('documents-empty')).not.toBeNull();
    });
    expect(screen.getByText('No documents yet')).not.toBeNull();
    expect(screen.getByTestId('btn-upload-document-empty')).not.toBeNull();
  });

  it('renders the error state with Retry and recovers', async () => {
    const list = vi
      .spyOn(documentsApi, 'list')
      .mockRejectedValueOnce(new Error('database unreachable'))
      .mockResolvedValueOnce(rows());

    render(<DocumentsPane tenant={mockTenant} />);

    await waitFor(() => {
      expect(screen.getByText("Couldn't load documents")).not.toBeNull();
    });
    expect(screen.getByText('database unreachable')).not.toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => {
      expect(screen.getByTestId('document-runbook')).not.toBeNull();
    });
    expect(list).toHaveBeenCalledTimes(2);
  });

  it('shows the loading skeleton before the first page arrives', async () => {
    let resolveList: (v: { documents: ApiReferenceDocument[] }) => void = () => {};
    vi.spyOn(documentsApi, 'list').mockReturnValue(
      new Promise((res) => {
        resolveList = res;
      })
    );

    render(<DocumentsPane tenant={mockTenant} />);
    expect(screen.getByTestId('pane-documents')).not.toBeNull();
    expect(document.querySelectorAll('.animate-pulse').length).toBe(3);

    resolveList(rows());
    await waitFor(() => {
      expect(screen.getByTestId('document-runbook')).not.toBeNull();
    });
  });
});
