import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import { Composer } from './Composer';
import { useStore } from '../../store';
import { UploadError } from '../../lib/attachments';

// Real pre-checks, size formatting and paste naming; only the network call is
// mocked so chip states can be driven deterministically.
const { uploadMock } = vi.hoisted(() => ({ uploadMock: vi.fn() }));
vi.mock('../../lib/attachments', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/attachments')>();
  return { ...actual, uploadAttachment: uploadMock };
});

interface Deferred {
  resolve: (v: any) => void;
  reject: (e: any) => void;
  promise: Promise<any>;
}

const deferreds: Deferred[] = [];
const pdfPayload = (name: string, i: number) => ({
  id: 'att_srv_' + i, name, mime: 'application/pdf', size: 5033164, url: '/api/v1/workspaces/acme/attachments/' + i + '/' + name,
});

const pdfFile = () => new File([new Uint8Array(8)], 'report.pdf', { type: 'application/pdf' });

const renderComposer = (override: Record<string, unknown> = {}) => {
  const onSend = vi.fn();
  const utils = render(
    <Composer
      agent={{ name: 'Atlas' }}
      running={false}
      onSend={onSend}
      onCancel={vi.fn()}
      mentionOptions={null}
      allowCommands={false}
      allowAttachments
      workspaceSlug="acme"
      {...override}
    />
  );
  return { onSend, ...utils };
};

const pickFiles = (files: File[]) => {
  const input = screen.getByTestId('composer-file-input');
  fireEvent.change(input, { target: { files } });
};

const textarea = () => screen.getByLabelText('Message input') as HTMLTextAreaElement;
const sendButton = () => screen.getByTestId('btn-send') as HTMLButtonElement;
const chips = () => screen.queryAllByTestId('attachment-chip');

beforeEach(() => {
  // clearAllMocks (not restoreAllMocks): Vitest 4 leaves vi.fn() call
  // history intact across tests otherwise, and every mock.calls[i] index
  // below assumes a fresh history per test.
  vi.clearAllMocks();
  deferreds.length = 0;
  uploadMock.mockImplementation(() => {
    let resolve!: Deferred['resolve'], reject!: Deferred['reject'];
    const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
    deferreds.push({ resolve, reject, promise });
    return promise;
  });
  useStore.setState({ toast: vi.fn() });
});

describe('Composer attachment tray (add-chat-attachments galleries B–E)', () => {
  it('runs the chip lifecycle uploading → ready, showing progress and doc meta', async () => {
    renderComposer();
    pickFiles([pdfFile()]);

    // Gallery B: uploading chip immediately, send gated (hard, D12).
    expect(chips()).toHaveLength(1);
    expect(chips()[0].dataset.chipState).toBe('uploading');
    expect(screen.getByText('report.pdf')).not.toBeNull();
    expect(sendButton().disabled).toBe(true);
    expect(uploadMock).toHaveBeenCalledWith('acme', expect.any(File), expect.objectContaining({ signal: expect.any(AbortSignal) }));

    await act(async () => { deferreds[0].resolve(pdfPayload('report.pdf', 1)); });

    // Gallery C: ready chip with "PDF · 4.8 MB" meta; send lit with empty text.
    await waitFor(() => expect(chips()[0].dataset.chipState).toBe('ready'));
    expect(screen.getByText('PDF · 4.8 MB')).not.toBeNull();
    expect(sendButton().disabled).toBe(false);
  });

  it('shows the reject reason inline and treats the chip as absent by the gate', async () => {
    const { onSend } = renderComposer();
    pickFiles([new File([new Uint8Array(8)], 'report.docx', { type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document' })]);

    expect(chips()).toHaveLength(1);
    expect(chips()[0].dataset.chipState).toBe('rejected');
    expect(screen.getByText(/Not supported — export as PDF/i)).not.toBeNull();
    expect(uploadMock).not.toHaveBeenCalled();
    // Absent by the gate: no text, no ready chips → send disabled…
    expect(sendButton().disabled).toBe(true);
    // …but typing re-enables — the rejected chip never blocks a text send.
    fireEvent.change(textarea(), { target: { value: 'See attached earlier' } });
    expect(sendButton().disabled).toBe(false);
    fireEvent.click(sendButton());
    expect(onSend).toHaveBeenCalledWith('See attached earlier', []);
  });

  it('offers Retry on failed uploads and re-sends the SAME retained File', async () => {
    renderComposer();
    const f = pdfFile();
    pickFiles([f]);
    await act(async () => { deferreds[0].reject(new UploadError(0, 'Network connection failed while uploading')); });

    expect(chips()[0].dataset.chipState).toBe('failed');
    expect(screen.getByText('Upload failed')).not.toBeNull();
    expect(sendButton().disabled).toBe(true); // failed ≠ ready; empty text → gated

    fireEvent.click(screen.getByTestId('chip-retry'));
    expect(uploadMock).toHaveBeenCalledTimes(2);
    expect(uploadMock.mock.calls[1][1]).toBe(f); // identity: no re-pick, same File
    expect(chips()[0].dataset.chipState).toBe('uploading');
    await act(async () => { deferreds[1].resolve(pdfPayload('report.pdf', 2)); });
    await waitFor(() => expect(chips()[0].dataset.chipState).toBe('ready'));
  });

  it('marks transport-level 5xx responses failed, server 4xx rejected', async () => {
    renderComposer();
    pickFiles([pdfFile(), new File([new Uint8Array(4)], 'notes.txt', { type: 'text/plain' })]);
    await act(async () => { deferreds[0].reject(new UploadError(503, 'Service unavailable')); });
    await act(async () => { deferreds[1].reject(new UploadError(422, 'file content is binary, not the text its name suggests')); });

    const states = chips().map((c) => c.dataset.chipState);
    expect(states).toContain('failed');
    expect(states).toContain('rejected');
    expect(screen.getByText(/file content is binary/i)).not.toBeNull();
  });

  it('cancels an in-flight upload from the chip ✕ (abort) and removes the chip', async () => {
    renderComposer();
    pickFiles([pdfFile()]);
    const { signal } = uploadMock.mock.calls[0][2];
    fireEvent.click(screen.getByLabelText('Cancel upload report.pdf'));

    expect(chips()).toHaveLength(0);
    expect(signal.aborted).toBe(true);
    // A settled-then-cancelled chip is still removable (abort is best-effort).
    pickFiles([pdfFile()]);
    await act(async () => { deferreds[1].resolve(pdfPayload('report.pdf', 3)); });
    await waitFor(() => expect(chips()[0].dataset.chipState).toBe('ready'));
    fireEvent.click(screen.getByLabelText('Remove report.pdf'));
    expect(chips()).toHaveLength(0);
    expect(sendButton().disabled).toBe(true);
  });

  it('unmount aborts the in-flight upload (session switch clears the tray)', async () => {
    const { unmount } = renderComposer();
    pickFiles([pdfFile()]);
    const { signal } = uploadMock.mock.calls[0][2];
    unmount();
    expect(signal.aborted).toBe(true);
  });

  it('images render a thumbnail chip from the capability URL once ready', async () => {
    renderComposer();
    pickFiles([new File([new Uint8Array(8)], 'shot.png', { type: 'image/png' })]);
    await act(async () => {
      deferreds[0].resolve({ id: 'a9', name: 'shot.png', mime: 'image/png', size: 8, url: '/api/v1/workspaces/acme/attachments/a9/shot.png' });
    });
    await waitFor(() => expect(chips()[0].dataset.chipState).toBe('ready'));
    const img = screen.getByAltText('shot.png');
    expect(img.getAttribute('src')).toBe('/api/v1/workspaces/acme/attachments/a9/shot.png');
  });
});

describe('Composer attachment send gate (design D12)', () => {
  it('Enter mid-upload is a no-op and the send control stays dimmed', async () => {
    const { onSend } = renderComposer();
    pickFiles([pdfFile()]);
    fireEvent.keyDown(textarea(), { key: 'Enter' });
    expect(onSend).not.toHaveBeenCalled();
    expect(sendButton().disabled).toBe(true);
  });

  it('sends attachment-only when a ready chip exists with empty text, then clears the tray', async () => {
    const { onSend } = renderComposer();
    pickFiles([pdfFile()]);
    await act(async () => { deferreds[0].resolve(pdfPayload('report.pdf', 4)); });

    expect(textarea().value).toBe('');
    expect(sendButton().disabled).toBe(false);
    fireEvent.keyDown(textarea(), { key: 'Enter' });
    expect(onSend).toHaveBeenCalledTimes(1);
    const [, sentChips] = onSend.mock.calls[0];
    expect(sentChips).toHaveLength(1);
    expect(sentChips[0]).toMatchObject({ id: 'att_srv_4', name: 'report.pdf', state: 'ready', url: pdfPayload('report.pdf', 4).url });
    expect(chips()).toHaveLength(0);
    expect(sendButton().disabled).toBe(true);
  });

  it('clicking send with text plus a ready chip passes exactly the ready chips', async () => {
    const { onSend } = renderComposer();
    pickFiles([pdfFile(), new File([new Uint8Array(8)], 'report.docx')]); // one rejected at the door
    await act(async () => { deferreds[0].resolve(pdfPayload('report.pdf', 5)); });

    fireEvent.change(textarea(), { target: { value: 'Here is the audit' } });
    fireEvent.click(sendButton());
    const [sentText, sentChips] = onSend.mock.calls[0];
    expect(sentText).toBe('Here is the audit');
    expect(sentChips).toHaveLength(1); // the rejected chip is never carried
  });
});

describe('Composer paste path (design D14)', () => {
  const pasteEvent = (files: File[]) => ({
    clipboardData: {
      items: files.map((f) => ({ kind: 'file', getAsFile: () => f })),
    },
  });

  it('creates an uploading chip with a timestamped name from a clipboard image', async () => {
    renderComposer();
    fireEvent.paste(textarea(), pasteEvent([new File([new Uint8Array(8)], 'image.png', { type: 'image/png' })]));

    expect(chips()).toHaveLength(1);
    expect(chips()[0].dataset.chipState).toBe('uploading');
    expect(screen.getByText(/^screenshot \d{4}-\d{2}-\d{2} \d{2}\.\d{2}\.png$/)).not.toBeNull();
    expect(uploadMock).toHaveBeenCalledTimes(1);
  });

  it('leaves text paste untouched', () => {
    renderComposer();
    fireEvent.change(textarea(), { target: { value: 'typed words ' } });
    fireEvent.paste(textarea(), {
      clipboardData: { items: [{ kind: 'string', type: 'text/plain' }] },
    });
    expect(chips()).toHaveLength(0);
    expect(uploadMock).not.toHaveBeenCalled();
    expect(textarea().value).toBe('typed words ');
  });
});

describe('Composer per-message cap', () => {
  it('toasts "Up to 4 attachments per message" and keeps only the first four', () => {
    const toast = vi.fn();
    useStore.setState({ toast });
    renderComposer();
    pickFiles([0, 1, 2, 3, 4].map((i) => new File([new Uint8Array(4)], 'f' + i + '.txt', { type: 'text/plain' })));

    expect(chips()).toHaveLength(4);
    expect(toast).toHaveBeenCalledWith('Up to 4 attachments per message');
    // The four kept chips upload; the fifth never starts.
    expect(uploadMock).toHaveBeenCalledTimes(4);
  });

  it('counts uploading chips toward the cap across selections', () => {
    const toast = vi.fn();
    useStore.setState({ toast });
    renderComposer();
    pickFiles([0, 1, 2].map((i) => new File([new Uint8Array(4)], 'a' + i + '.txt', { type: 'text/plain' })));
    pickFiles([0, 1].map((i) => new File([new Uint8Array(4)], 'b' + i + '.txt', { type: 'text/plain' })));
    expect(chips()).toHaveLength(4);
    expect(toast).toHaveBeenCalledWith('Up to 4 attachments per message');
  });
});

describe('Composer text-only surfaces (channels keep today\'s composer)', () => {
  it('hides the attach affordance without allowAttachments', () => {
    renderComposer({ allowAttachments: false });
    expect(screen.queryByTestId('btn-attach')).toBeNull();
    expect(screen.queryByTestId('composer-file-input')).toBeNull();
    fireEvent.change(textarea(), { target: { value: 'hello' } });
    expect(sendButton().disabled).toBe(false);
  });
});

describe('Composer attachment capability hint (fix-image-attachment-lane)', () => {
  const imageFile = () => new File([new Uint8Array(8)], 'shot.png', { type: 'image/png' });
  const imagePayload = () => ({
    id: 'att_img_1', name: 'shot.png', mime: 'image/png', size: 8, url: '/api/v1/workspaces/acme/attachments/img1/shot.png',
  });
  const warning = () => screen.queryByTestId('chip-modality-warning');

  it('warns on an image chip when the model affirmatively cannot see images — send stays enabled', async () => {
    const { onSend } = renderComposer({
      agent: { name: 'Atlas', input_modalities: { image: 'unsupported', pdf: 'unknown' } },
    });
    pickFiles([imageFile()]);

    // The hint lands with the chip (before the upload settles)…
    expect(warning()?.textContent).toBe("this model can't see images — will attach as reference only");
    // …and never gates sending: once the chip is ready, send is lit and works.
    expect(sendButton().disabled).toBe(true); // still uploading (D12 gate, unchanged)
    await act(async () => { deferreds[0].resolve(imagePayload()); });
    await waitFor(() => expect(chips()[0].dataset.chipState).toBe('ready'));

    expect(warning()?.textContent).toBe("this model can't see images — will attach as reference only");
    expect(sendButton().disabled).toBe(false);
    fireEvent.click(sendButton());
    expect(onSend).toHaveBeenCalledTimes(1);
  });

  it('shows the PDF variant when the model affirmatively cannot read PDFs', () => {
    renderComposer({
      agent: { name: 'Atlas', input_modalities: { image: 'supported', pdf: 'unsupported' } },
    });
    pickFiles([pdfFile()]);

    expect(warning()?.textContent).toBe("this model can't read PDFs — will attach as reference only");
  });

  it('stays silent when capability is supported, unknown, or the field is absent', () => {
    const agents = [
      { name: 'Atlas', input_modalities: { image: 'supported', pdf: 'supported' } },
      { name: 'Atlas', input_modalities: { image: 'unknown', pdf: 'unknown' } },
      { name: 'Atlas' }, // no input_modalities at all
    ];
    for (const agent of agents) {
      const { unmount } = renderComposer({ agent });
      pickFiles([imageFile(), pdfFile()]);
      expect(screen.queryAllByTestId('chip-modality-warning')).toHaveLength(0);
      unmount();
    }
  });
});
