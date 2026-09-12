// Chat attachment upload client (add-chat-attachments design D13).
//
// The client mirrors the server's locked lane matrix (internal/attachments
// Classify) for INSTANT feedback only — the server's magic-byte sniff stays
// authoritative, so unknown extensions are not pre-rejected here and a server
// 4xx surfaces as a rejected chip with the server's reason. Uploads ride XHR
// because fetch has no upload progress events.

import { API_ORIGIN, getToken } from './api';

export type AttachmentChipState = 'uploading' | 'ready' | 'rejected' | 'failed';

/** Composer-local chip (design D11): NEVER lifted into the global store —
 * progress ticks are high-frequency and per-tick store writes already tripped
 * React 19's nested-update limit once in this codebase. */
export interface AttachmentChip {
  key: string; // local uid, stable across progress re-renders
  file?: File; // retained until the upload lands so Retry can re-send it
  id?: string; // server attachment id once ready
  name: string;
  mime: string;
  size: number;
  url?: string; // capability URL — the wire token for the later turn wave
  state: AttachmentChipState;
  progress: number; // 0–100
  reason?: string; // rejected chips only
}

export interface UploadedAttachment {
  id: string;
  name: string;
  mime: string;
  size: number;
  url: string;
}

/** Upload failure with an HTTP status: 4xx means the server rejected the
 * file (rejected chip), 0/5xx means transport or server trouble (failed chip
 * with Retry). */
export class UploadError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'UploadError';
    this.status = status;
  }
}

// --- Locked lane matrix (design D4), mirrored from internal/attachments ----

const MB = 1024 * 1024;

export const ATTACHMENTS_PER_MESSAGE = 4;
export const MAX_IMAGE_BYTES = 5 * MB;
export const MAX_PDF_BYTES = 20 * MB;
export const MAX_DROP_BYTES = 50 * MB;

const set = (...items: string[]) => {
  const m: Record<string, boolean> = {};
  for (const it of items) m[it] = true;
  return m;
};

const IMAGE_EXTS = set('png', 'jpg', 'jpeg', 'webp', 'gif');
const PDF_EXTS = set('pdf');
const OFFICE_EXTS = set('docx', 'xlsx', 'pptx', 'doc', 'xls', 'ppt');
const ARCHIVE_EXTS = set('zip', 'gz', 'tgz', 'tar', '7z', 'rar', 'bz2', 'xz', 'zst');
const EXECUTABLE_EXTS = set('exe', 'dll', 'msi', 'dmg', 'pkg', 'app', 'so', 'dylib', 'bin', 'com', 'scr');
// Same text family the server classifies: ≤200 KB rides inline-text, larger
// members fall to the drop lane automatically, both capped at 50 MB.
const TEXT_EXTS = set(
  'txt', 'md', 'markdown', 'csv', 'tsv', 'json', 'jsonl', 'ndjson', 'yaml', 'yml', 'toml', 'ini', 'cfg', 'conf', 'env',
  'xml', 'html', 'htm', 'css', 'scss',
  'py', 'js', 'mjs', 'cjs', 'ts', 'tsx', 'jsx', 'go', 'rb', 'sh', 'bash', 'zsh', 'fish', 'sql',
  'java', 'kt', 'kts', 'scala', 'c', 'h', 'cpp', 'cc', 'hpp', 'cs', 'm', 'mm', 'swift', 'rs', 'php', 'pl', 'lua', 'r', 'jl',
  'dart', 'vue', 'svelte', 'proto', 'graphql', 'tf', 'hcl', 'dockerfile', 'makefile', 'cmake', 'gradle',
  'log', 'diff', 'patch', 'gitignore', 'editorconfig', 'properties'
);

/** `accept` filter for the file picker (paste and drag-drop bypass it —
 * D13 — which is exactly why the pre-check below exists). */
export const PICKER_ACCEPT = ['.png', '.jpg', '.jpeg', '.webp', '.gif', '.pdf']
  .concat(Object.keys(TEXT_EXTS).map((e) => '.' + e))
  .join(',');

const extOf = (name: string) => {
  const i = name.lastIndexOf('.');
  return i > 0 ? name.slice(i + 1).toLowerCase() : '';
};

const extFromMime = (mime: string) => {
  switch ((mime || '').toLowerCase()) {
    case 'image/png': return 'png';
    case 'image/jpeg': return 'jpg';
    case 'image/webp': return 'webp';
    case 'image/gif': return 'gif';
    case 'application/pdf': return 'pdf';
    case 'text/plain': return 'txt';
    case 'text/csv': return 'csv';
    case 'text/markdown': return 'md';
    case 'application/json': return 'json';
    default: return '';
  }
};

/** Instant client-side reject reason, or null when the upload should start
 * (the server's sniff gets the final word — design D13). */
export function precheckAttachment(file: File): string | null {
  if (!file || file.size <= 0) return 'File is empty';
  const ext = extOf(file.name);
  if (OFFICE_EXTS[ext]) return 'Not supported — export as PDF and attach that instead';
  if (ARCHIVE_EXTS[ext]) return 'Archives are not a supported attachment type';
  if (EXECUTABLE_EXTS[ext]) return 'Executables are not a supported attachment type';
  if (IMAGE_EXTS[ext] && file.size > MAX_IMAGE_BYTES) return 'Exceeds the 5 MB image cap';
  if (PDF_EXTS[ext] && file.size > MAX_PDF_BYTES) return 'Exceeds the 20 MB PDF cap';
  if ((TEXT_EXTS[ext] || IMAGE_EXTS[ext] || PDF_EXTS[ext]) && file.size > MAX_DROP_BYTES) {
    return 'Exceeds the 50 MB file cap';
  }
  return null;
}

/** "4.8 MB" / "212 KB" / "512 B" — chip and transcript sizing copy. */
export function formatSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  if (bytes < 1024) return `${bytes} B`;
  const kb = bytes / 1024;
  if (kb < 1024) return `${Math.round(kb)} KB`;
  const mb = kb / 1024;
  if (mb < 1024) {
    const v = mb >= 100 ? Math.round(mb) : Math.round(mb * 10) / 10;
    return `${v} MB`;
  }
  const gb = Math.round((mb / 1024) * 10) / 10;
  return `${gb} GB`;
}

/** Short lane label for chip meta: "PDF · 4.8 MB", "SQL · 4.1 MB" (gallery I). */
export function mimeLabel(mime: string, name: string): string {
  const m = (mime || '').toLowerCase();
  if (m === 'application/pdf') return 'PDF';
  if (m.startsWith('image/')) return m.slice(6).toUpperCase();
  if (m.startsWith('text/')) return (extOf(name) || m.slice(5)).toUpperCase();
  const sub = m.includes('/') ? m.slice(m.indexOf('/') + 1) : '';
  return (sub || extOf(name) || 'file').toUpperCase();
}

/** Timestamped default name for unnamed clipboard files (design D14):
 * "screenshot 2026-09-10 14.32.png". */
export function defaultPasteName(file: File, at: Date = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  const date = `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}`;
  const time = `${pad(at.getHours())}.${pad(at.getMinutes())}`;
  const ext = extOf(file.name) || extFromMime(file.type) || 'dat';
  return `screenshot ${date} ${time}.${ext}`;
}

// --- Upload client ----------------------------------------------------------

const fallbackMessage = (status: number) => {
  if (status === 413) return 'The file exceeds the upload size cap';
  if (status === 401) return 'Your session expired — sign in and try again';
  return `Upload failed (HTTP ${status})`;
};

/** Resolves the 201 payload; rejects with UploadError carrying the HTTP
 * status (0 for network/abort-level failures). */
export function uploadAttachment(
  ws: string,
  file: File,
  opts: { signal?: AbortSignal; onProgress?: (percent: number) => void } = {}
): Promise<UploadedAttachment> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    let settled = false;
    const done = (fn: () => void) => {
      if (settled) return;
      settled = true;
      opts.signal?.removeEventListener('abort', onAbort);
      fn();
    };
    const onAbort = () => xhr.abort();

    xhr.open('POST', `${API_ORIGIN}/api/v1/workspaces/${encodeURIComponent(ws)}/attachments`);
    const token = getToken();
    if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`);

    if (opts.signal) {
      if (opts.signal.aborted) {
        done(() => reject(new DOMException('Upload aborted', 'AbortError')));
        return;
      }
      opts.signal.addEventListener('abort', onAbort, { once: true });
    }

    xhr.upload.onprogress = (e) => {
      if (!opts.onProgress || !e.lengthComputable || e.total <= 0) return;
      opts.onProgress(Math.min(100, Math.round((e.loaded / e.total) * 100)));
    };

    const serverMessage = () => {
      try {
        const data = JSON.parse(xhr.responseText);
        const err = data && data.error;
        if (typeof err === 'string') return err;
        if (err && typeof err.message === 'string') return err.message;
      } catch {
        // non-JSON error body — fall through to the status-based message
      }
      return '';
    };

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        let payload: any = null;
        try {
          payload = JSON.parse(xhr.responseText);
        } catch {
          payload = null;
        }
        if (payload && payload.id && payload.url) {
          done(() => resolve({
            id: payload.id,
            name: payload.name || file.name,
            mime: payload.mime || file.type,
            size: typeof payload.size === 'number' ? payload.size : file.size,
            url: payload.url,
          }));
          return;
        }
        done(() => reject(new UploadError(xhr.status, 'Upload returned an unreadable response')));
        return;
      }
      done(() => reject(new UploadError(xhr.status, serverMessage() || fallbackMessage(xhr.status))));
    };
    xhr.onerror = () => done(() => reject(new UploadError(0, 'Network connection failed while uploading')));
    xhr.onabort = () => done(() => reject(new DOMException('Upload aborted', 'AbortError')));

    const body = new FormData();
    body.append('file', file);
    xhr.send(body);
  });
}
