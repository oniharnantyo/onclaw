// Reference documents client (add-reference-documents tasks 8.1): ONE module
// owns every shape and route under /workspaces/:ws/documents/ so contract
// drift with the backend is a one-file fix — the same rule
// lib/connectionsApi.ts follows for integrations. Upload and content
// replacement ride XHR (fetch has no upload progress events) and send
// multipart FormData, never JSON. Reads and JSON edits go through request<T>
// so the standard error envelope surfaces as ApiError with status/code.
//
// Visibility model (design D7): scope `attached` limits a document to its
// attached agents/channels; `workspace` (promoted — admin-gated via
// reference_documents.promote) makes it visible to every agent.

import { useAuthStore } from '../store/auth';
import { useStore } from '../store';
import { API_ORIGIN, getToken, request } from './api';
import { UploadError } from './attachments';

/** Index pipeline state surfaced at upload: a born-digital document indexes
 * to `ready`; a scanned PDF (no text layer) lands as `no_text_layer`; large
 * jobs may still be `processing` when the 201 returns. */
export type DocumentIndexStatus = 'ready' | 'no_text_layer' | 'processing';

/** D7 visibility scope: `attached` (agent/channel tiers) or `workspace`
 * (every agent — the promoted tier). */
export type DocumentScope = 'attached' | 'workspace';

/** One reference document as the wire carries it — the 201 upload shape,
 * echoed unchanged by every mutate response. `url` is the capability URL the
 * right-panel preview and downloads ride. */
export interface ApiReferenceDocument {
  id: string;
  name: string;
  description: string;
  mime: string;
  size: number;
  url: string;
  indexStatus: DocumentIndexStatus;
  scope: DocumentScope;
  pageCount: number;
  /** Attached agent ids; the promoted (`workspace`) scope ignores them. */
  agents: string[];
  /** Attached channel ids. */
  channels: string[];
  createdAt: string;
}

/** Optional metadata accepted beside the file on upload. */
export interface DocumentUploadMeta {
  name?: string;
  description?: string;
  agentIds?: string[];
  channelIds?: string[];
}

/** A list lens (D7 visibility, read side): restrict the listing to one
 * agent's or one channel's visible set. */
export interface DocumentLens {
  agent?: string;
  channel?: string;
}

function base(ws: string): string {
  return `/workspaces/${encodeURIComponent(ws)}/documents`;
}

export const documentsApi = {
  /** Workspace library, optionally through an agent or channel lens. */
  list: (ws: string, lens?: DocumentLens) => {
    const params = new URLSearchParams();
    if (lens?.agent) params.set('agent', lens.agent);
    if (lens?.channel) params.set('channel', lens.channel);
    const qs = params.toString();
    return request<{ documents: ApiReferenceDocument[] }>(
      `${base(ws)}${qs ? `?${qs}` : ''}`,
      { method: 'GET' }
    );
  },

  /** Rename / re-describe. Responds the refreshed document. */
  patch: (ws: string, id: string, body: { name?: string; description?: string }) =>
    request<ApiReferenceDocument>(`${base(ws)}/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body,
    }),

  /** Removes the row and its blob — the token becomes unrecoverable (the
   * attachment-blob precedent: orphaned bytes are harmless). */
  remove: (ws: string, id: string) =>
    request<void>(`${base(ws)}/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  /** D7 attach editor: set the full attached-agent set atomically — unknown
   * ids reject with nothing changed, like connectionsApi.setAgents. */
  setAgents: (ws: string, id: string, agentIds: string[]) =>
    request<ApiReferenceDocument>(`${base(ws)}/${encodeURIComponent(id)}/agents`, {
      method: 'PUT',
      body: { agentIds },
    }),

  /** Same, for channels. */
  setChannels: (ws: string, id: string, channelIds: string[]) =>
    request<ApiReferenceDocument>(`${base(ws)}/${encodeURIComponent(id)}/channels`, {
      method: 'PUT',
      body: { channelIds },
    }),

  /** Admin scope flips (reference_documents.promote): `promote` widens to
   * the workspace tier, `demote` returns to the attached tier. A non-admin
   * gets the standard permission error envelope. */
  promote: (ws: string, id: string) =>
    request<ApiReferenceDocument>(`${base(ws)}/${encodeURIComponent(id)}/promote`, {
      method: 'POST',
    }),

  demote: (ws: string, id: string) =>
    request<ApiReferenceDocument>(`${base(ws)}/${encodeURIComponent(id)}/demote`, {
      method: 'POST',
    }),

  /** Multipart upload with progress; resolves the 201 document payload.
   * Rejections carry UploadError with the HTTP status. */
  upload: (
    ws: string,
    file: File,
    meta: DocumentUploadMeta = {},
    opts: { onProgress?: (percent: number) => void } = {}
  ): Promise<ApiReferenceDocument> =>
    sendDocumentForm('POST', `${API_ORIGIN}/api/v1${base(ws)}`, file, meta, opts),

  /** Replace content in place: new bytes, re-indexed, same document row. */
  replace: (
    ws: string,
    id: string,
    file: File,
    opts: { onProgress?: (percent: number) => void } = {}
  ): Promise<ApiReferenceDocument> =>
    sendDocumentForm(
      'PUT',
      `${API_ORIGIN}/api/v1${base(ws)}/${encodeURIComponent(id)}/content`,
      file,
      {},
      opts
    ),
};

// --- multipart transport (XHR — mirrors uploadAttachment in attachments.ts) --

const fallbackMessage = (status: number) => {
  if (status === 413) return 'The file exceeds the upload size cap';
  if (status === 401) return 'Your session expired — sign in and try again';
  return `Upload failed (HTTP ${status})`;
};

function sendDocumentForm(
  method: 'POST' | 'PUT',
  url: string,
  file: File,
  meta: DocumentUploadMeta,
  opts: { onProgress?: (percent: number) => void }
): Promise<ApiReferenceDocument> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    let settled = false;
    const done = (fn: () => void) => {
      if (settled) return;
      settled = true;
      fn();
    };

    xhr.open(method, url);
    const token = getToken();
    if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`);

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
        if (payload && payload.id && payload.name) {
          done(() => resolve(payload as ApiReferenceDocument));
          return;
        }
        done(() => reject(new UploadError(xhr.status, 'Upload returned an unreadable response')));
        return;
      }
      done(() => reject(new UploadError(xhr.status, serverMessage() || fallbackMessage(xhr.status))));
    };
    xhr.onerror = () => done(() => reject(new UploadError(0, 'Network connection failed while uploading')));

    const body = new FormData();
    body.append('file', file);
    if (meta.name) body.append('name', meta.name);
    if (meta.description) body.append('description', meta.description);
    for (const id of meta.agentIds || []) body.append('agentIds', id);
    for (const id of meta.channelIds || []) body.append('channelIds', id);
    xhr.send(body);
  });
}

// --- client-side precheck (instant feedback; the server stays authoritative) -

const MB = 1024 * 1024;
export const MAX_PDF_DOCUMENT_BYTES = 20 * MB;
export const MAX_DOCUMENT_BYTES = 50 * MB;

/** The reference-library allowlist (tasks 8.1): pdf/office/text formats the
 * converter registry handles. Legacy `.doc`/`.ppt` are out by design
 * (non-goal) and rejected here. */
export const DOCUMENT_EXTENSIONS = ['pdf', 'docx', 'pptx', 'xlsx', 'md', 'txt', 'html', 'csv'] as const;

const DOCUMENT_EXT_SET: Record<string, boolean> = {};
for (const e of DOCUMENT_EXTENSIONS) DOCUMENT_EXT_SET[e] = true;

/** `accept` filter for the file picker; drag-drop bypasses it, which is why
 * precheckDocument exists. */
export const DOCUMENT_PICKER_ACCEPT = DOCUMENT_EXTENSIONS.map((e) => '.' + e).join(',');

const extOf = (name: string) => {
  const i = name.lastIndexOf('.');
  return i > 0 ? name.slice(i + 1).toLowerCase() : '';
};

/** Instant client-side reject reason, or null when the upload should start.
 * The server re-validates (magic bytes + its own caps). */
export function precheckDocument(file: File): string | null {
  if (!file || file.size <= 0) return 'File is empty';
  const ext = extOf(file.name);
  if (!DOCUMENT_EXT_SET[ext]) {
    return `.${ext || 'file'} isn't supported — use ${DOCUMENT_EXTENSIONS.map((e) => '.' + e).join(', ')}`;
  }
  if (ext === 'pdf' && file.size > MAX_PDF_DOCUMENT_BYTES) return 'Exceeds the 20 MB PDF cap';
  if (file.size > MAX_DOCUMENT_BYTES) return 'Exceeds the 50 MB file cap';
  return null;
}

// --- permission helpers ------------------------------------------------------
// reference_documents.promote holders: built-in Owner/Admin, Superadmin via
// its all-workspace-permissions set; Member never holds it and custom roles
// only by explicit grant (internal/domain/permissions.go). Mirrors
// canManageIntegrations / canWriteSkills.

export function canPromoteDocuments(memberships: any[], tenant: any): boolean {
  if (!memberships.length) return true; // offline / mock mode — affordances stay visible
  const tenantId = tenant?.sub || tenant?.id;
  const mem = memberships.find(
    (m) =>
      m.workspace_id === tenantId ||
      m.workspace_slug === tenantId ||
      m.workspace_id === tenant?.id ||
      m.workspace_slug === tenant?.sub
  );
  if (!mem) return false;
  const role = mem.role;
  const roleName = (mem.role_name || role?.name || '').toLowerCase();
  const perms: string[] = role?.permissions || [];
  return (
    role?.is_owner === true ||
    roleName === 'superadmin' ||
    roleName === 'owner' ||
    roleName === 'admin' ||
    perms.some(
      (p) =>
        p === '*' ||
        p === 'reference_documents.promote' ||
        p === 'reference_documents.*' ||
        p === 'workspace.*'
    )
  );
}

export function useCanPromoteDocuments(tenant: any): boolean {
  const memberships = useAuthStore((s) => s.memberships);
  const pos = useStore((s: any) => s.pos);
  if (!tenant) return canPromoteDocuments(memberships, { id: pos.tenantId, sub: pos.tenantId });
  return canPromoteDocuments(memberships, tenant);
}

// --- display helpers ----------------------------------------------------------

/** Row label for the index pipeline state (data-testid stays the raw
 * `doc-index-<status>` value). */
export function indexStatusLabel(status: DocumentIndexStatus | string | undefined): string {
  switch (status) {
    case 'ready':
      return 'Indexed';
    case 'no_text_layer':
      return 'No text layer';
    case 'processing':
      return 'Processing';
    default:
      return 'Unknown';
  }
}

/** Scope badge copy: `ALL AGENTS` for the promoted tier, else
 * "N agents · M channels". */
export function scopeBadge(doc: Pick<ApiReferenceDocument, 'scope' | 'agents' | 'channels'>): string {
  if (doc.scope === 'workspace') return 'ALL AGENTS';
  const n = (doc.agents || []).length;
  const m = (doc.channels || []).length;
  if (n === 0 && m === 0) return 'Unattached';
  return `${n} ${n === 1 ? 'agent' : 'agents'} · ${m} ${m === 1 ? 'channel' : 'channels'}`;
}

/** A human type line for the row icon title, e.g. "PDF document". */
export function documentTypeLabel(name: string, mime: string): string {
  const ext = extOf(name);
  const names: Record<string, string> = {
    pdf: 'PDF document',
    docx: 'Word document',
    pptx: 'PowerPoint deck',
    xlsx: 'Excel spreadsheet',
    md: 'Markdown document',
    txt: 'Text file',
    html: 'HTML document',
    csv: 'CSV spreadsheet',
  };
  return names[ext] || mime || 'Document';
}
