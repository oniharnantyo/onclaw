// Web preview card (generative-ui spec, `$type: "preview"`): browser-like
// chrome — a URL bar showing the content's host, a reload control that
// remounts the frame by key (so a previous address's content never shows
// under a new URL bar — the key also carries the URL), and open-in-new-tab —
// around a sandboxed iframe WITHOUT allow-same-origin (opaque origin: the
// content cannot reach app cookies or storage). While the producing tool call
// is still running the element renders nothing; on failure the card shows a
// failure state, never a blank frame. Height is fixed by the card (design D3).

import { useState } from "react";
import { Icon } from "../../components/ui/Icon";

export function hostOf(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return '';
  }
}

const CHROME_BTN =
  'flex h-6 w-6 shrink-0 items-center justify-center rounded-[4px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg2';

interface PreviewCardProps {
  url: string;
  html: string;
  title: string;
  /** The producing call failed (or delivered no content) — failure state. */
  failed: boolean;
  errorText?: string;
  odId: string;
}

export function PreviewCard({ url, html, title, failed, errorText, odId }: PreviewCardProps) {
  const [reloadKey, setReloadKey] = useState(0);
  const host = hostOf(url) || url;
  const frameTitle = title || host || 'Web preview';

  if (failed) {
    return (
      <div
        data-od-id={odId}
        className="mb-2 overflow-hidden rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]"
      >
        <div className="flex items-center gap-2 px-2.5 py-1.5">
          <Icon name="globe" size={12} className="shrink-0 text-meta"/>
          <span className="min-w-0 truncate rounded-[4px] bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] px-1.5 py-0.5 font-mono text-[10px] text-fg2">
            {host || 'preview unavailable'}
          </span>
        </div>
        <div className="flex items-center gap-2 border-t border-linesoft px-2.5 py-3">
          <Icon name="alert" size={14} className="shrink-0 text-danger"/>
          <div className="min-w-0">
            <p className="text-[12px] text-fg2">Preview failed</p>
            {errorText && <p className="truncate text-[11px] text-muted" title={errorText}>{errorText}</p>}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div data-od-id={odId} className="mb-2 overflow-hidden rounded-md border border-line bg-surface">
      <div className="flex items-center gap-1.5 border-b border-linesoft px-2 py-1.5">
        <Icon name="globe" size={12} className="shrink-0 text-meta"/>
        <span
          className="min-w-0 flex-1 truncate rounded-[4px] bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] px-1.5 py-0.5 font-mono text-[10px] text-fg2"
          title={url || host}
        >
          {host || 'about:blank'}
        </span>
        {/* Reload remounts the frame by key; the key also carries the URL so a
            new address can never show the previous frame's content. */}
        <button type="button" onClick={() => setReloadKey((k) => k + 1)} aria-label="Reload preview" title="Reload" className={CHROME_BTN}>
          <Icon name="refresh" size={12}/>
        </button>
        {url && (
          <a href={url} target="_blank" rel="noreferrer" aria-label="Open in new tab" title="Open in new tab" className={CHROME_BTN}>
            <Icon name="external-link" size={12}/>
          </a>
        )}
      </div>
      <iframe
        key={`${reloadKey}|${url}`}
        title={frameTitle}
        srcDoc={html || undefined}
        src={html ? undefined : url || undefined}
        // Sandboxed without allow-same-origin — unique opaque origin, no app
        // cookie/storage access. Scripts run, navigation and popups do not.
        sandbox="allow-scripts"
        referrerPolicy="no-referrer"
        className="h-64 w-full border-0 bg-white"
      />
    </div>
  );
}
