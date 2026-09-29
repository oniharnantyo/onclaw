import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// jsdom environment for the FormData + XHR upload paths.
// @vitest-environment jsdom

// Pin the API origin + token for the XHR paths (the attachments.test.ts
// pattern) while keeping the REAL request() so the JSON-route assertions
// exercise the actual error-envelope behavior.
vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>();
  return { ...actual, API_ORIGIN: 'http://api.test', getToken: () => 'tok-123' };
});

import {
  canPromoteDocuments,
  documentTypeLabel,
  documentsApi,
  DOCUMENT_PICKER_ACCEPT,
  indexStatusLabel,
  MAX_DOCUMENT_BYTES,
  MAX_PDF_DOCUMENT_BYTES,
  precheckDocument,
  scopeBadge,
  type ApiReferenceDocument,
} from './documentsApi';
import { UploadError } from './attachments';

const doc: ApiReferenceDocument = {
  id: 'doc-1',
  name: 'runbook',
  description: 'Incident runbook',
  mime: 'application/pdf',
  size: 1024,
  url: '/files/wk_1/documents/doc-1/runbook.pdf',
  indexStatus: 'ready',
  scope: 'attached',
  pageCount: 12,
  agents: ['agent-1'],
  channels: [],
  createdAt: '2026-09-01T00:00:00Z',
};

describe('lib/documentsApi — JSON routes', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  function mockJson(payload: unknown, status = 200) {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: status >= 200 && status < 300,
      status,
      headers: new Headers({ 'Content-Type': 'application/json' }),
      json: async () => payload,
    } as any);
  }

  function mockNoContent() {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 204,
      headers: new Headers(),
    } as any);
  }

  it('lists the workspace library under /documents', async () => {
    mockJson({ documents: [doc] });
    await expect(documentsApi.list('acme')).resolves.toEqual({ documents: [doc] });
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/documents');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('GET');
  });

  it('lists through the agent and channel lenses as query params', async () => {
    mockJson({ documents: [doc] });
    await documentsApi.list('acme', { agent: 'atlas' });
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/documents?agent=atlas');

    mockJson({ documents: [] });
    await documentsApi.list('acme', { channel: 'ops' });
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/documents?channel=ops');

    // Both lens kinds at once is not a thing — agent wins the param set only
    // when declared; empty lens fields contribute nothing.
    mockJson({ documents: [] });
    await documentsApi.list('acme', {});
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/documents');
  });

  it('encodes workspace and document path params', async () => {
    mockNoContent();
    await documentsApi.remove('acme corp', 'doc/1');
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme%20corp/documents/doc%2F1');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('DELETE');
  });

  it('patches name/description with a JSON body and returns the document', async () => {
    mockJson(doc);
    await expect(documentsApi.patch('acme', 'doc-1', { name: 'runbook-v2', description: 'd' })).resolves.toEqual(doc);
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/documents/doc-1');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('PATCH');
    expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(
      JSON.stringify({ name: 'runbook-v2', description: 'd' })
    );
  });

  it('sets the attach sets with exactly {agentIds} / {channelIds} over PUT', async () => {
    mockJson(doc);
    await expect(documentsApi.setAgents('acme', 'doc-1', ['a', 'b'])).resolves.toEqual(doc);
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/documents/doc-1/agents');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('PUT');
    expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(JSON.stringify({ agentIds: ['a', 'b'] }));

    mockJson(doc);
    await documentsApi.setChannels('acme', 'doc-1', ['ops']);
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/documents/doc-1/channels');
    expect((globalThis.fetch as any).mock.calls[0][1].body).toBe(JSON.stringify({ channelIds: ['ops'] }));
  });

  it('posts promote/demote to their subresources without a body', async () => {
    mockJson({ ...doc, scope: 'workspace' });
    await expect(documentsApi.promote('acme', 'doc-1')).resolves.toEqual({ ...doc, scope: 'workspace' });
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/documents/doc-1/promote');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');

    mockJson(doc);
    await documentsApi.demote('acme', 'doc-1');
    expect((globalThis.fetch as any).mock.calls[0][0]).toBe('/api/v1/workspaces/acme/documents/doc-1/demote');
    expect((globalThis.fetch as any).mock.calls[0][1].method).toBe('POST');
  });

  it('propagates the standard permission error envelope on promote', async () => {
    mockJson({ error: { code: 'forbidden', message: 'missing reference_documents.promote' } }, 403);
    await expect(documentsApi.promote('acme', 'doc-1')).rejects.toMatchObject({
      status: 403,
      code: 'forbidden',
      message: 'missing reference_documents.promote',
    });
  });
});

describe('lib/documentsApi — multipart upload (XHR)', () => {
  class FakeXHR {
    static latest: FakeXHR | null = null;

    status = 0;
    responseText = '';
    upload = { onprogress: null as ((e: any) => void) | null };
    onload: (() => void) | null = null;
    onerror: (() => void) | null = null;

    method = '';
    url = '';
    headers: Record<string, string> = {};
    body: any = null;

    open(method: string, url: string) {
      this.method = method;
      this.url = url;
    }
    setRequestHeader(k: string, v: string) {
      this.headers[k] = v;
    }
    send(body: any) {
      this.body = body;
      FakeXHR.latest = this;
    }
    progress(loaded: number, total: number) {
      this.upload.onprogress?.({ lengthComputable: true, loaded, total });
    }
    respond(status: number, responseText: string) {
      this.status = status;
      this.responseText = responseText;
      this.onload?.();
    }
    failNetwork() {
      this.onerror?.();
    }
  }

  beforeEach(() => {
    FakeXHR.latest = null;
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('POSTs the file plus repeated attach fields as multipart with the bearer token', async () => {
    const f = new File([new Uint8Array(3)], 'runbook.pdf', { type: 'application/pdf' });
    const p = documentsApi.upload(
      'acme',
      f,
      { name: 'runbook', description: 'Incident runbook', agentIds: ['a1', 'a2'], channelIds: ['ops'] },
      {}
    );
    const xhr = FakeXHR.latest!;
    expect(xhr.method).toBe('POST');
    expect(xhr.url).toBe('http://api.test/api/v1/workspaces/acme/documents');
    expect(xhr.headers.Authorization).toBe('Bearer tok-123');
    expect(xhr.body).toBeInstanceOf(FormData);
    expect(xhr.body.get('file')).toBe(f);
    expect(xhr.body.get('name')).toBe('runbook');
    expect(xhr.body.get('description')).toBe('Incident runbook');
    expect(xhr.body.getAll('agentIds')).toEqual(['a1', 'a2']);
    expect(xhr.body.getAll('channelIds')).toEqual(['ops']);
    // Never JSON-encoded.
    expect(typeof xhr.body).not.toBe('string');
    xhr.respond(201, JSON.stringify(doc));
    await expect(p).resolves.toEqual(doc);
  });

  it('propagates upload progress 0–100', async () => {
    const onProgress = vi.fn();
    const p = documentsApi.upload('acme', new File([new Uint8Array(4)], 'a.md', { type: 'text/markdown' }), {}, { onProgress });
    const xhr = FakeXHR.latest!;
    xhr.progress(25, 100);
    xhr.progress(100, 100);
    expect(onProgress).toHaveBeenNthCalledWith(1, 25);
    expect(onProgress).toHaveBeenNthCalledWith(2, 100);
    xhr.respond(201, JSON.stringify(doc));
    await p;
  });

  it('PUTs replacement content to the /content subresource', async () => {
    const f = new File([new Uint8Array(2)], 'runbook-v2.pdf', { type: 'application/pdf' });
    const p = documentsApi.replace('acme', 'doc-1', f);
    const xhr = FakeXHR.latest!;
    expect(xhr.method).toBe('PUT');
    expect(xhr.url).toBe('http://api.test/api/v1/workspaces/acme/documents/doc-1/content');
    expect(xhr.body.get('file')).toBe(f);
    xhr.respond(200, JSON.stringify({ ...doc, size: 2 }));
    await expect(p).resolves.toEqual({ ...doc, size: 2 });
  });

  it('rejects with UploadError carrying the server message and status', async () => {
    const p = documentsApi.upload('acme', new File([new Uint8Array(4)], 'a.txt', { type: 'text/plain' }));
    FakeXHR.latest!.respond(413, JSON.stringify({ error: { code: 'payload_too_large', message: 'file exceeds the cap' } }));
    const err = await p.catch((e) => e);
    expect(err).toBeInstanceOf(UploadError);
    expect(err).toMatchObject({ status: 413, message: 'file exceeds the cap' });
  });

  it('rejects with a network-class UploadError when the request never leaves', async () => {
    const p = documentsApi.upload('acme', new File([new Uint8Array(2)], 'a.txt'));
    FakeXHR.latest!.failNetwork();
    await expect(p).rejects.toMatchObject({ status: 0 });
  });
});

describe('lib/documentsApi — precheck (client mirror of the allowlist)', () => {
  const fakeFile = (name: string, size: number) => ({ name, size, type: '' }) as unknown as File;

  it('accepts every allowlisted extension', () => {
    for (const ext of ['pdf', 'docx', 'pptx', 'xlsx', 'md', 'txt', 'html', 'csv']) {
      expect(precheckDocument(fakeFile(`doc.${ext}`, 10))).toBeNull();
    }
    expect(DOCUMENT_PICKER_ACCEPT).toBe('.pdf,.docx,.pptx,.xlsx,.md,.txt,.html,.csv');
  });

  it('rejects unsupported formats, including the legacy doc/ppt pair', () => {
    for (const name of ['legacy.doc', 'old.ppt', 'mystery.exe', 'archive.zip']) {
      expect(precheckDocument(fakeFile(name, 10))).toMatch(/isn't supported/);
    }
    expect(precheckDocument(fakeFile('noext', 10))).toMatch(/isn't supported/);
  });

  it('rejects empty files and enforces the pdf 20 MB / others 50 MB caps', () => {
    expect(precheckDocument(fakeFile('empty.pdf', 0))).toMatch(/empty/i);
    expect(precheckDocument(fakeFile('big.pdf', MAX_PDF_DOCUMENT_BYTES + 1))).toMatch(/20 MB/);
    expect(precheckDocument(fakeFile('big.pdf', MAX_PDF_DOCUMENT_BYTES))).toBeNull();
    expect(precheckDocument(fakeFile('big.md', MAX_DOCUMENT_BYTES + 1))).toMatch(/50 MB/);
    expect(precheckDocument(fakeFile('big.md', MAX_DOCUMENT_BYTES))).toBeNull();
  });
});

describe('lib/documentsApi — permission + display helpers', () => {
  const tenant = { id: 'acme', sub: 'acme' };

  it('derives reference_documents.promote like the sibling write-permission helpers', () => {
    const owner = [{ workspace_id: 'acme', role: { is_owner: true, permissions: [] } }];
    const admin = [{ workspace_id: 'acme', role_name: 'Admin', role: { permissions: ['workspace.*'] } }];
    const member = [{ workspace_id: 'acme', role_name: 'Member', role: { name: 'Member', permissions: ['tools.read'] } }];
    const granted = [
      { workspace_id: 'acme', role_name: 'Librarian', role: { name: 'Librarian', permissions: ['reference_documents.promote'] } },
    ];
    const toolsWrite = [
      { workspace_id: 'acme', role_name: 'Tooling', role: { name: 'Tooling', permissions: ['tools.write'] } },
    ];

    expect(canPromoteDocuments([], tenant)).toBe(true); // offline / mock mode
    expect(canPromoteDocuments(owner, tenant)).toBe(true);
    expect(canPromoteDocuments(admin, tenant)).toBe(true);
    expect(canPromoteDocuments(member, tenant)).toBe(false);
    expect(canPromoteDocuments(granted, tenant)).toBe(true);
    expect(canPromoteDocuments(toolsWrite, tenant)).toBe(false);
    expect(canPromoteDocuments(member, { id: 'other' })).toBe(false);
  });

  it('labels index statuses and scope badges per the pane copy', () => {
    expect(indexStatusLabel('ready')).toBe('Indexed');
    expect(indexStatusLabel('no_text_layer')).toBe('No text layer');
    expect(indexStatusLabel('processing')).toBe('Processing');
    expect(indexStatusLabel(undefined)).toBe('Unknown');

    expect(scopeBadge({ scope: 'workspace', agents: ['a'], channels: [] })).toBe('ALL AGENTS');
    expect(scopeBadge({ scope: 'attached', agents: ['a', 'b'], channels: ['ops', 'inc'] })).toBe('2 agents · 2 channels');
    expect(scopeBadge({ scope: 'attached', agents: ['a'], channels: [] })).toBe('1 agent · 0 channels');
    expect(scopeBadge({ scope: 'attached', agents: [], channels: [] })).toBe('Unattached');
  });

  it('types documents by extension for the row title', () => {
    expect(documentTypeLabel('runbook.pdf', 'application/pdf')).toBe('PDF document');
    expect(documentTypeLabel('deck.pptx', '')).toBe('PowerPoint deck');
    expect(documentTypeLabel('notes.weird', 'application/octet-stream')).toBe('application/octet-stream');
  });
});
