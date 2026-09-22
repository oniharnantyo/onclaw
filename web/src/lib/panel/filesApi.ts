// Workspace-files API client (add-right-panel D3): the authenticated byte
// lane to an agent's jail directory. Thin on purpose — URL building, the
// bearer header (exactly how lib/api.ts attaches it), and a 404 → null
// mapping so "no longer there" is a value, not an error state.

import { API_ORIGIN, getToken } from '../api';

export const API_FILES_BASE = `${API_ORIGIN}/api/v1/workspaces`;

/** One row of a directory listing (`&mode=list`). */
export interface AgentDirEntry {
  name: string;
  kind: 'file' | 'directory';
  size: number;
  modified: string;
}

export interface AgentFileBytes {
  bytes: ArrayBuffer;
  contentType: string;
}

/** URL of one file under the agent's jail root — for `<img>`/`<iframe>`
 * sources and download links where an Authorization header cannot ride along.
 * `path` is relative to the agent root; any encoding is preserved verbatim. */
export function agentFileUrl(ws: string, agent: string, path: string): string {
  return `${API_FILES_BASE}/${encodeURIComponent(ws)}/agents/${encodeURIComponent(agent)}/files?path=${encodeURIComponent(path)}`;
}

/** Fetches one file's bytes. Resolves null on 404 (missing path or an escape
 * outside the jail root — the API refuses to distinguish). Other failures
 * throw so the caller can render its error state. */
export async function fetchAgentFile(ws: string, agent: string, path: string, signal?: AbortSignal): Promise<AgentFileBytes | null> {
  const headers = new Headers();
  const token = getToken();
  if (token) headers.set('Authorization', `Bearer ${token}`);
  const res = await fetch(agentFileUrl(ws, agent, path), { headers, signal });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`files api: ${res.status}`);
  return { bytes: await res.arrayBuffer(), contentType: res.headers.get('Content-Type') || 'application/octet-stream' };
}

/** Lists ONE directory level (`&mode=list`). Resolves null on 404 (missing
 * directory, or the path addressed a file — the API refuses both the same
 * way). */
export async function listAgentDir(ws: string, agent: string, path: string, signal?: AbortSignal): Promise<AgentDirEntry[] | null> {
  const headers = new Headers();
  const token = getToken();
  if (token) headers.set('Authorization', `Bearer ${token}`);
  const res = await fetch(`${agentFileUrl(ws, agent, path)}&mode=list`, { headers, signal });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`files api: ${res.status}`);
  return (await res.json()) as AgentDirEntry[];
}
