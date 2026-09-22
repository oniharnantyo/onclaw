// File panel candidates (add-right-panel task 3.4): transcript tool cards
// that produced files under the agent jail root map to `{kind:'file',
// title:<filename>, payload:{path:<jail-relative path>}}` — the same payload
// shape the file source renderer consumes. Matchers parse tolerantly:
// mid-stream cards carry no result yet and mint nothing (a failed or
// in-flight call produced no file to open).

import type { PanelCandidate, PanelCardInput } from "../../../../../lib/panel/registry";

/** The backend jail mount (internal/agents/backend/fs_jailed_backend.go):
 * absolute tool paths under it map to the jail-relative path the files API
 * addresses. */
const MOUNT_POINT = '/workspace';

function parseJsonObject(raw: unknown): Record<string, unknown> | null {
  if (typeof raw !== 'string' || !raw.trim()) return null;
  try {
    const v = JSON.parse(raw);
    return v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
  } catch {
    return null;
  }
}

/** Filename of a path — the candidate title (tab label) and download name. */
export function baseName(p: string): string {
  const parts = p.replace(/\\/g, '/').split('/').filter(Boolean);
  return parts.length ? parts[parts.length - 1] : p;
}

/** Maps a tool-provided path to the jail-relative path the files API
 * addresses: `/workspace/...` and bare absolute paths collapse to their tail,
 * leading separators drop, `..` (jail escape) and empty results reject. */
export function jailRelativePath(p: string): string | null {
  let s = String(p).replace(/\\/g, '/');
  if (s === MOUNT_POINT) s = '';
  else if (s.startsWith(MOUNT_POINT + '/')) s = s.slice(MOUNT_POINT.length + 1);
  s = s.replace(/^\/+/, '');
  if (!s || s.split('/').includes('..')) return null;
  return s;
}

function candidateFromPath(rawPath: unknown): PanelCandidate | null {
  if (typeof rawPath !== 'string' || !rawPath.trim()) return null;
  const path = jailRelativePath(rawPath);
  if (!path) return null;
  return { kind: 'file', title: baseName(path), payload: { path } };
}

function fromEntryArray(entries: unknown): PanelCandidate[] {
  if (!Array.isArray(entries)) return [];
  const out: PanelCandidate[] = [];
  for (const e of entries) {
    const c = candidateFromPath(e && typeof e === 'object' ? (e as any).path : e);
    if (c) out.push(c);
  }
  return out;
}

/** Every file this card produced, one candidate per file (a batch envelope
 * carrying several files yields several candidates). Pure — tests and the
 * registered matcher both read it. */
export function fileCandidatesFromCard(card: PanelCardInput): PanelCandidate[] {
  if (!card || typeof card !== 'object' || card.error) return [];
  const name = typeof card.name === 'string' ? card.name : '';
  const args = parseJsonObject(card.args);
  const res = parseJsonObject(card.res);
  if (name === 'document.create') {
    // A batch result carries a files[] array; a single-document result
    // carries its own path (the trusted envelope — args fall back for
    // mid-stream cards whose result hasn't landed). Legacy envelopes
    // (`name`) don't carry a jail path and mint nothing.
    if (res && Array.isArray(res.files)) return fromEntryArray(res.files);
    return [candidateFromPath(res?.path ?? args?.path)].filter(Boolean) as PanelCandidate[];
  }
  if (name === 'files.write') {
    if (args && Array.isArray(args.files)) return fromEntryArray(args.files);
    return [candidateFromPath(args?.path ?? args?.file_path)].filter(Boolean) as PanelCandidate[];
  }
  // Runtime filesystem tools (internal/agents/tool_gate.go FilesystemToolNames):
  // write_file/edit_file produce a file at args.file_path; read_file inspected
  // one — opening any of them is useful inspection. Live payloads carry the
  // key `file_path` with a `/workspace/...` mount-prefixed absolute path.
  if (name === 'write_file' || name === 'edit_file' || name === 'read_file') {
    return [candidateFromPath(args?.file_path ?? args?.path)].filter(Boolean) as PanelCandidate[];
  }
  return [];
}

/** The registered matcher (first candidate wins — one tool card opens one
 * panel tab per affordance; batch files surface via their own cards). */
export function fileCandidateMatcher(card: PanelCardInput): PanelCandidate | null {
  return fileCandidatesFromCard(card)[0] ?? null;
}
