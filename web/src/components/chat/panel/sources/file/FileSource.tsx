// File source renderer (add-right-panel task 3.1): the panel pane for one
// file under the agent jail root. Fetches bytes through the files API
// (lib/panel/filesApi), shows explicit loading / not-found / error states,
// then dispatches by extension + content type: markdown (frontmatter chip +
// the transcript's own markdown pipeline), highlighted code, PDF iframe,
// image, CSV table, and a captioned degrade card for anything that can't
// render inline (binary formats, svg attachments, over-threshold text).
import { useEffect, useMemo, useRef, useState } from "react";
import { agentFileUrl, fetchAgentFile } from "../../../../../lib/panel/filesApi";
import { extractFrontmatter, rewriteRelativeAssets } from "../../../../../lib/panel/prerender";
import { useStore, useWorkspace } from "../../../../../store";
import { SyntaxHighlighter } from "@/components/assistant-ui/elements/shiki-highlighter";
import { MarkdownBody } from "../../../AgentMessage";
import { CsvTable } from "../../CsvTable";
import { DegradeCard } from "../../DegradeCard";

/** Text files larger than this render a head excerpt + download instead of
 * the whole body (task 3.3 size-threshold degrade). */
export const FILE_TEXT_MAX_BYTES = 512 * 1024;
/** Characters of an over-threshold text file shown before the download card. */
const EXCERPT_CHARS = 4096;

const MARKDOWN_EXT = new Set(['md', 'markdown']);
const IMAGE_EXT = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp']);
const CODE_EXT = new Set([
  'go', 'ts', 'tsx', 'js', 'jsx', 'json', 'py', 'sh', 'bash', 'yaml', 'yml',
  'toml', 'sql', 'rb', 'rs', 'java', 'c', 'h', 'cpp', 'css', 'html', 'txt',
  'xml', 'ini', 'env',
]);
const DEGRADE_EXT = new Set(['xlsx', 'xls', 'docx', 'pptx', 'zip', 'svg']);

export function extensionOf(path: string): string {
  const base = path.replace(/\\/g, '/').split('/').pop() ?? path;
  const dot = base.lastIndexOf('.');
  return dot === -1 ? '' : base.slice(dot + 1).toLowerCase();
}

/** Directory part of a jail-relative path — the base for resolving an
 * asset reference inside a markdown file. */
function dirOf(path: string): string {
  const norm = path.replace(/\\/g, '/');
  const cut = norm.lastIndexOf('/');
  return cut === -1 ? '' : norm.slice(0, cut);
}

/** `assets/x.png` relative to the file's directory → jail-relative path.
 * `./` and `../` segments resolve; a `..` escaping the jail stays relative
 * (the API then 404s, which the image just shows as broken — never a crash). */
function resolveAssetRef(dir: string, ref: string): string {
  const segs: string[] = [];
  for (const seg of ref.replace(/\\/g, '/').split('/')) {
    if (!seg || seg === '.') continue;
    if (seg === '..') segs.pop();
    else segs.push(seg);
  }
  return dir ? dir + '/' + segs.join('/') : segs.join('/');
}

// --- type dispatch -----------------------------------------------------------

type FileKind = 'markdown' | 'code' | 'pdf' | 'image' | 'csv' | 'degrade' | 'unknown-text';

function dispatch(ext: string, contentType: string): FileKind {
  if (MARKDOWN_EXT.has(ext)) return 'markdown';
  if (ext === 'pdf' || contentType === 'application/pdf') return 'pdf';
  if (IMAGE_EXT.has(ext)) return 'image';
  if (ext === 'csv') return 'csv';
  if (DEGRADE_EXT.has(ext)) return 'degrade';
  if (CODE_EXT.has(ext)) return 'code';
  // No known extension: trust the content type — text served as text stays
  // readable; anything else (unknown binary) degrades.
  if (contentType.startsWith('text/') || contentType.includes('json') || contentType.includes('xml') || contentType.includes('javascript')) return 'unknown-text';
  return 'degrade';
}

// --- the renderer -------------------------------------------------------------

export function FileSource({ tab, ctx }: { tab: any; ctx?: any }) {
  const path = typeof tab?.payload?.path === 'string' ? tab.payload.path : '';
  const fileName = path.replace(/\\/g, '/').split('/').pop() || path;

  // Workspace + agent identity: ChatRoute's sourceContext may carry them;
  // otherwise they derive from the store's active position (the workspace
  // entry and the chat the panel is mounted over — the same agent the
  // transcript's tool cards ran in).
  const tenant = useWorkspace();
  const pos = useStore((s: any) => s.pos);
  const ws: string = ctx?.ws ?? tenant?.sub ?? tenant?.id ?? tenant?.slug ?? '';
  const agents: any[] = tenant?.agents || [];
  const chatAgent = agents.find((a) => a.slug === pos.chatId || a.id === pos.chatId);
  const agent: string = ctx?.agentSlug ?? chatAgent?.slug ?? chatAgent?.id ?? pos.chatId ?? '';

  const [state, setState] = useState<
    | { phase: 'loading' }
    | { phase: 'notfound' }
    | { phase: 'error'; message: string }
    | { phase: 'ready'; bytes: ArrayBuffer; contentType: string }
  >({ phase: 'loading' });
  const seq = useRef(0);

  useEffect(() => {
    if (!path) {
      setState({ phase: 'notfound' });
      return;
    }
    const id = ++seq.current;
    const abort = new AbortController();
    setState({ phase: 'loading' });
    fetchAgentFile(ws, agent, path, abort.signal)
      .then((res) => {
        if (seq.current !== id || abort.signal.aborted) return;
        setState(res ? { phase: 'ready', ...res } : { phase: 'notfound' });
      })
      .catch((err: unknown) => {
        if (seq.current !== id || abort.signal.aborted) return;
        setState({ phase: 'error', message: err instanceof Error ? err.message : String(err) });
      });
    return () => abort.abort();
  }, [ws, agent, path]);

  const fileUrl = useMemo(() => (path ? agentFileUrl(ws, agent, path) : ''), [ws, agent, path]);

  if (state.phase === 'loading') {
    return (
      <div className="flex items-center gap-2 p-4 text-[12px] text-muted" data-od-id="panel-file-loading">
        <span className="od-dot"/><span className="od-dot"/><span className="od-dot"/>
        <span>Loading {fileName}…</span>
      </div>
    );
  }
  if (state.phase === 'notfound') {
    return (
      <div className="p-4" data-od-id="panel-file-notfound">
        <p className="text-[12.5px] font-medium text-fg2">File not found</p>
        <p className="mt-1 break-all font-mono text-[11px] text-muted">{path}</p>
        <p className="mt-1 text-[12px] text-muted">It isn't in this agent's workspace — it may have been moved or deleted.</p>
      </div>
    );
  }
  if (state.phase === 'error') {
    return (
      <div className="p-4" data-od-id="panel-file-error">
        <p className="text-[12.5px] font-medium text-danger">Couldn't load {fileName}</p>
        <p className="mt-1 text-[12px] text-muted">{state.message}</p>
      </div>
    );
  }

  const text = () => new TextDecoder().decode(state.bytes);
  const kind = dispatch(extensionOf(path), state.contentType);

  switch (kind) {
    case 'markdown': {
      if (state.bytes.byteLength > FILE_TEXT_MAX_BYTES) return <TextDegrade path={path} fileUrl={fileUrl} text={text()}/>;
      const { meta, body } = extractFrontmatter(text());
      // Relative asset refs resolve against the file's own directory, so a
      // doc and its images travel together (task 3.1). Fences and inline
      // code are protected by the rewriter itself.
      const resolved = rewriteRelativeAssets(body, (ref) =>
        agentFileUrl(ws, agent, ref.startsWith('/') ? resolveAssetRef('', ref) : resolveAssetRef(dirOf(path), ref))
      );
      return (
        <div className="p-4">
          {meta && <FrontmatterChip meta={meta}/>}
          <div className="md-body text-[14px] leading-relaxed text-fg" data-od-id="panel-file-markdown">
            <MarkdownBody text={resolved}/>
          </div>
        </div>
      );
    }
    case 'code':
    case 'unknown-text': {
      if (state.bytes.byteLength > FILE_TEXT_MAX_BYTES) return <TextDegrade path={path} fileUrl={fileUrl} text={text()}/>;
      const ext = extensionOf(path);
      return (
        <div className="p-3" data-od-id="panel-file-code">
          <SyntaxHighlighter code={text()} language={ext || 'text'} className="[&_pre]:!rounded-lg [&_pre]:border-line"/>
        </div>
      );
    }
    case 'pdf':
      return (
        <div className="p-3">
          <object data={fileUrl} type="application/pdf" className="h-full min-h-[360px] w-full rounded-lg border border-line" data-od-id="panel-file-pdf">
            <DegradeCard title={`PDF · ${fileName}`} subtitle="Inline preview isn't available in this browser." downloadUrl={fileUrl} downloadName={fileName}/>
          </object>
        </div>
      );
    case 'image':
      return (
        <div className="flex justify-center p-3">
          <img src={fileUrl} alt={fileName} data-od-id="panel-file-image" className="max-h-full max-w-full rounded-lg border border-line object-contain"/>
        </div>
      );
    case 'csv':
      return (
        <div className="p-2">
          <CsvTable text={text()}/>
        </div>
      );
    default:
      return (
        <div className="p-3">
          <DegradeCard
            title={`${degradeTitle(extensionOf(path), state.contentType)} · ${fileName}`}
            subtitle={DEGRADE_EXT.has(extensionOf(path)) && extensionOf(path) === 'svg'
              ? "Vector files download as attachments — they can't preview inline."
              : "This file type can't be previewed inline."}
            downloadUrl={fileUrl}
            downloadName={fileName}
          />
        </div>
      );
  }
}

// Over-threshold text (task 3.3): a head excerpt keeps the file inspectable;
// the download card covers the rest. No spinner-time pretense of rendering
// the whole thing.
function TextDegrade({ path, fileUrl, text }: { path: string; fileUrl: string; text: string }) {
  const fileName = path.replace(/\\/g, '/').split('/').pop() || path;
  return (
    <div className="p-3" data-od-id="panel-file-oversize">
      <pre className="od-scroll max-h-[60vh] overflow-auto rounded-lg border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] p-3 font-mono text-[12.5px] leading-5 text-fg2" data-od-id="panel-file-excerpt">
        {text.slice(0, EXCERPT_CHARS)}
      </pre>
      <p className="mt-1 text-[11px] text-muted" data-od-id="panel-file-oversize-note">
        Showing the first {EXCERPT_CHARS.toLocaleString()} characters — the file is {(text.length / 1024).toFixed(0)} kB.
      </p>
      <div className="mt-2">
        <DegradeCard title={`Large text file · ${fileName}`} downloadUrl={fileUrl} downloadName={fileName}/>
      </div>
    </div>
  );
}

// Frontmatter chip: one compact key/value row block above the markdown body.
function FrontmatterChip({ meta }: { meta: Record<string, string> }) {
  return (
    <div className="mb-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] px-2.5 py-2" data-od-id="panel-file-frontmatter">
      {Object.entries(meta).map(([k, v]) => (
        <p key={k} className="truncate text-[11px] leading-5">
          <span className="font-mono text-muted">{k}</span>
          <span className="text-fg2"> {v}</span>
        </p>
      ))}
    </div>
  );
}

function degradeTitle(ext: string, contentType: string): string {
  const names: Record<string, string> = {
    xlsx: 'Excel spreadsheet', xls: 'Excel spreadsheet', docx: 'Word document',
    pptx: 'PowerPoint deck', zip: 'Zip archive', svg: 'SVG image',
  };
  if (names[ext]) return names[ext];
  if (ext) return `.${ext} file`;
  return contentType !== 'application/octet-stream' ? contentType : 'Binary file';
}
