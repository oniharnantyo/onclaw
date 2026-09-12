import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('./api', () => ({
  API_ORIGIN: 'http://api.test',
  getToken: () => 'tok-123',
}));

import {
  uploadAttachment, precheckAttachment, formatSize, defaultPasteName, mimeLabel, UploadError,
} from './attachments';

// --- XHR fake: captures the request, lets tests drive progress/response ----

class FakeXHR {
  static latest: FakeXHR | null = null;

  status = 0;
  responseText = '';
  upload = { onprogress: null as ((e: any) => void) | null };
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onabort: (() => void) | null = null;
  ontimeout: (() => void) | null = null;

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
  abort() {
    this.onabort?.();
  }

  /** test drivers */
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

const file = (name: string, size: number, type = '') => new File([new Uint8Array(Math.max(0, size))], name, { type });
// precheckAttachment only reads name/size — plain stand-ins avoid allocating
// 50 MB buffers for the cap tests.
const fakeFile = (name: string, size: number) => ({ name, size, type: '' }) as unknown as File;

describe('lib/attachments formatSize', () => {
  it('formats bytes, KB and MB per the design copy ("4.8 MB", "212 KB")', () => {
    expect(formatSize(0)).toBe('0 B');
    expect(formatSize(512)).toBe('512 B');
    expect(formatSize(212 * 1024)).toBe('212 KB');
    expect(formatSize(5033164)).toBe('4.8 MB'); // gallery B/C: "PDF · 4.8 MB"
    expect(formatSize(5 * 1024 * 1024)).toBe('5 MB');
    expect(formatSize(1.5 * 1024 * 1024)).toBe('1.5 MB');
  });
});

describe('lib/attachments defaultPasteName', () => {
  it('names unnamed clipboard files "screenshot YYYY-MM-DD HH.mm.ext"', () => {
    const png = new File([new Uint8Array(4)], '', { type: 'image/png' });
    expect(defaultPasteName(png, new Date(2026, 8, 10, 14, 32))).toBe('screenshot 2026-09-10 14.32.png');
  });

  it('pads single-digit time parts and falls back to the filename extension', () => {
    const notes = new File([new Uint8Array(4)], 'notes.txt', { type: 'text/plain' });
    expect(defaultPasteName(notes, new Date(2026, 2, 5, 9, 5))).toBe('screenshot 2026-03-05 09.05.txt');
  });
});

describe('lib/attachments mimeLabel', () => {
  it('labels chips like "PDF · 4.8 MB" / "SQL · 4.1 MB"', () => {
    expect(mimeLabel('application/pdf', 'report.pdf')).toBe('PDF');
    expect(mimeLabel('text/x-sql', 'dump.sql')).toBe('SQL');
    expect(mimeLabel('image/png', 'shot.png')).toBe('PNG');
    expect(mimeLabel('', 'notes.md')).toBe('MD');
  });
});

describe('lib/attachments precheckAttachment (client mirror of the locked lane matrix)', () => {
  it('rejects zero-byte files', () => {
    expect(precheckAttachment(new File([], 'empty.png', { type: 'image/png' }))).toMatch(/empty/i);
  });

  it('rejects office formats with the export-as-PDF guidance', () => {
    for (const ext of ['docx', 'xlsx', 'pptx', 'doc', 'xls', 'ppt']) {
      expect(precheckAttachment(fakeFile(`report.${ext}`, 10))).toMatch(/export as PDF/i);
    }
  });

  it('rejects archives and executables', () => {
    expect(precheckAttachment(fakeFile('bundle.zip', 10))).toMatch(/archive/i);
    expect(precheckAttachment(fakeFile('setup.exe', 10))).toMatch(/executable/i);
  });

  it('enforces the image 5 MB and PDF 20 MB caps, allowing anything at the cap', () => {
    expect(precheckAttachment(fakeFile('shot.png', 5 * 1024 * 1024 + 1))).toMatch(/5 MB/);
    expect(precheckAttachment(fakeFile('shot.png', 5 * 1024 * 1024))).toBeNull();
    expect(precheckAttachment(fakeFile('doc.pdf', 20 * 1024 * 1024 + 1))).toMatch(/20 MB/);
    expect(precheckAttachment(fakeFile('doc.pdf', 20 * 1024 * 1024))).toBeNull();
  });

  it('lets text-family files ride the drop lane up to 50 MB', () => {
    expect(precheckAttachment(fakeFile('notes.txt', 200 * 1024))).toBeNull(); // inline-text
    expect(precheckAttachment(fakeFile('big.log', 30 * 1024 * 1024))).toBeNull(); // drop lane
    expect(precheckAttachment(fakeFile('huge.sql', 50 * 1024 * 1024 + 1))).toMatch(/50 MB/);
  });

  it('leaves unknown extensions to the server sniff (server stays authoritative)', () => {
    expect(precheckAttachment(fakeFile('mystery.weird', 100))).toBeNull();
  });
});

describe('lib/attachments uploadAttachment', () => {
  beforeEach(() => {
    FakeXHR.latest = null;
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('POSTs the file as multipart to the workspace attachments endpoint with the bearer token', async () => {
    const f = file('report.pdf', 3, 'application/pdf');
    const p = uploadAttachment('acme', f);
    const xhr = FakeXHR.latest!;
    expect(xhr.method).toBe('POST');
    expect(xhr.url).toBe('http://api.test/api/v1/workspaces/acme/attachments');
    expect(xhr.headers.Authorization).toBe('Bearer tok-123');
    expect(xhr.body).toBeInstanceOf(FormData);
    expect(xhr.body.get('file')).toBe(f);
    xhr.respond(201, JSON.stringify({ id: 'a1', name: 'report.pdf', mime: 'application/pdf', size: 3, url: '/api/v1/workspaces/acme/attachments/a1/report.pdf' }));
    await expect(p).resolves.toEqual({
      id: 'a1', name: 'report.pdf', mime: 'application/pdf', size: 3,
      url: '/api/v1/workspaces/acme/attachments/a1/report.pdf',
    });
  });

  it('propagates upload progress 0–100', async () => {
    const onProgress = vi.fn();
    const p = uploadAttachment('acme', file('shot.png', 4, 'image/png'), { onProgress });
    const xhr = FakeXHR.latest!;
    xhr.progress(25, 100);
    xhr.progress(100, 100);
    expect(onProgress).toHaveBeenNthCalledWith(1, 25);
    expect(onProgress).toHaveBeenNthCalledWith(2, 100);
    xhr.respond(201, JSON.stringify({ id: 'a2', name: 'shot.png', mime: 'image/png', size: 4, url: '/u' }));
    await p;
  });

  it('surfaces the server error message on non-2xx (413 cap message lands on the chip)', async () => {
    const p = uploadAttachment('acme', file('shot.png', 4, 'image/png'));
    FakeXHR.latest!.respond(413, JSON.stringify({ error: { code: 'payload_too_large', message: 'image exceeds the 5242880 byte upload cap' } }));
    const err = await p.catch((e) => e);
    expect(err).toBeInstanceOf(UploadError);
    expect(err).toMatchObject({ status: 413, message: 'image exceeds the 5242880 byte upload cap' });
  });

  it('falls back to a status-based message for non-JSON error bodies', async () => {
    const p = uploadAttachment('acme', file('a.txt', 2, 'text/plain'));
    FakeXHR.latest!.respond(500, '<html>boom</html>');
    await expect(p).rejects.toMatchObject({ status: 500 });
  });

  it('rejects with a network-class UploadError when the request never leaves', async () => {
    const p = uploadAttachment('acme', file('a.txt', 2, 'text/plain'));
    FakeXHR.latest!.failNetwork();
    await expect(p).rejects.toMatchObject({ status: 0 });
  });

  it('rejects with AbortError when the signal aborts mid-flight', async () => {
    const controller = new AbortController();
    const p = uploadAttachment('acme', file('a.txt', 2, 'text/plain'), { signal: controller.signal });
    controller.abort();
    await expect(p).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('rejects immediately for an already-aborted signal without sending', async () => {
    const controller = new AbortController();
    controller.abort();
    const p = uploadAttachment('acme', file('a.txt', 2, 'text/plain'), { signal: controller.signal });
    await expect(p).rejects.toMatchObject({ name: 'AbortError' });
    expect(FakeXHR.latest).toBeNull();
  });
});
