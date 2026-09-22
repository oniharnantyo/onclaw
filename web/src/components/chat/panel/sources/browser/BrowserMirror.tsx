// Browser mirror source (add-right-panel 4.1/4.2/4.3): a read-only mirror of
// the agent's browser session built entirely from transcript tool cards —
// latest screenshot through the workspace files lane, address bar from the
// last navigate/open arg, and a feed of the session's browser calls. No
// polling, no live CDP bridge: the mirror moves only when the transcript
// does, and a stale mirror says so (staleness caption, frozen chrome).

import { useEffect, useRef, useState } from "react";
import { useStore } from "../../../../../store";
import { fetchAgentFile } from "../../../../../lib/panel/filesApi";
import { registerPanelCandidate, registerPanelSource, type PanelCardInput, type PanelCandidate } from "../../../../../lib/panel/registry";
import { browserCards, currentUrl, feedName, feedSummary, screenshotPath, stalenessCaption, type BrowserCard } from "./mirror";

// --- Candidate matcher (4.1) ------------------------------------------------
// Any `browser.*` tool card is panel-able. Identity note: the card itself
// carries no session or agent id, so the payload cannot address an
// agent+session pair — the panel slice is per-chat (reset on chat switch by
// goPos), which in agent chats IS one agent+session. Payload is therefore a
// constant and the dedup key yields ONE browser tab per chat; the renderer
// resolves agent/session from the store and source context.

export function browserCandidate(card: PanelCardInput): PanelCandidate | null {
  if (typeof card?.name !== 'string' || !card.name.startsWith('browser.')) return null;
  return { kind: 'browser', title: 'Browser', payload: {} };
}

// Sessions this chat has already shown browser activity for. A later render
// with no browser cards in the active session means the mirror's session was
// torn down or superseded — the closed state — not a chat that never browsed.
const seenBrowserChats = new Set<string>();

export function BrowserMirror({ ctx }: any) {
  const tenantId = useStore((s: any) => s.pos.tenantId);
  const chatId = useStore((s: any) => s.pos.chatId);
  const tenant = useStore((s: any) => s.db[tenantId]);
  const running = useStore((s: any) => s.ui.running);
  const thread = useStore((s: any) => s.db[tenantId]?.threads[chatId]);

  const session = thread && !Array.isArray(thread) ? thread.list?.find((x: any) => x.id === thread.active) : null;
  const cards: BrowserCard[] = browserCards(session?.messages || []);
  const chatKey = `${tenantId}::${chatId}`;

  // The agent the mirror belongs to: the chat's agent, the channel's primary
  // agent (ctx.primaryAgentId), or the chat id as a last-resort slug.
  const agents = tenant?.agents || [];
  const channelId = (tenant?.channels || []).find((c: any) => c.id === chatId)?.agentId;
  const agent =
    agents.find((a: any) => a.slug === chatId || a.id === chatId) ||
    agents.find((a: any) => a.id === channelId) ||
    agents.find((a: any) => a.id === ctx?.primaryAgentId) ||
    null;
  const agentSlug = agent?.slug || agent?.id || ctx?.primaryAgentId || chatId;
  const ws = tenant?.sub || tenant?.id || '';

  // Idle: this chat has never produced a browser.* card — the mirror fills in
  // when the agent browses. Closed: browser cards existed before but none sit
  // in the ACTIVE session — the session was torn down or superseded.
  if (cards.length === 0) {
    const closed = seenBrowserChats.has(chatKey);
    return (
      <div className="p-4" data-od-id={closed ? 'panel-browser-closed' : 'panel-browser-idle'}>
        <Header agentName={agent?.name} />
        <p className="mt-3 text-[12px] leading-5 text-muted">
          {closed
            ? 'This browser session has ended or was superseded by a newer one.'
            : 'No browser activity yet. The mirror fills in here when the agent browses.'}
        </p>
      </div>
    );
  }
  seenBrowserChats.add(chatKey);

  // Mirroring: the run is still active (globally running, or the last card is
  // mid-stream — folded without a result yet). Frozen: the run finished; the
  // last screenshot is retained and the chrome says so.
  const last = cards[cards.length - 1];
  const midStream = last.res === undefined && last.error === undefined;
  const frozen = !running && !midStream;

  const shot = cards.map((c) => c.name).lastIndexOf('browser.screenshot');
  const shotCard = shot >= 0 ? cards[shot] : undefined;
  const shotPath = screenshotPath(shotCard);
  const url = currentUrl(cards);

  return (
    <div className="flex min-h-0 flex-col p-3" data-od-id={frozen ? 'panel-browser-frozen' : 'panel-browser-mirroring'}>
      <Header agentName={agent?.name} live={!frozen} />
      {/* Address bar: the last page the agent navigated to. Frozen sessions
          show it dimmed — it is where the session ended, not where it is. */}
      <div data-od-id="panel-browser-address" title={url || ''}>
        <div className="flex h-8 items-center gap-2 rounded-lg border border-line bg-surface px-2.5">
          <span className="text-[11px] text-muted" aria-hidden>↗</span>
          <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-fg2">{url || 'about:blank'}</span>
        </div>
      </div>
      <Screenshot slug={agentSlug} ws={ws} path={shotPath} frozen={frozen} />
      {shotCard && (
        <p className="mt-1.5 font-mono text-[10px] text-muted" data-od-id="panel-browser-staleness">
          {stalenessCaption(cards)}
        </p>
      )}
      {frozen && (
        <p className="mt-1 font-mono text-[10px] uppercase tracking-[0.14em] text-muted" data-od-id="panel-browser-frozen-note">
          Frozen — run finished, last screenshot retained
        </p>
      )}
      <Feed cards={cards} />
    </div>
  );
}

function Header({ agentName, live }: { agentName?: string; live?: boolean }) {
  return (
    <div className="mb-2 flex items-center gap-2" data-od-id="panel-browser-header">
      <p className="min-w-0 flex-1 truncate text-[13px] font-semibold text-fg">
        {agentName ? agentName + ' browser' : 'Browser'}
      </p>
      {live && (
        <span className="flex items-center gap-1 rounded-full border border-line px-1.5 py-0.5 font-mono text-[9px] font-semibold uppercase tracking-[0.14em] text-muted" data-od-id="panel-browser-live">
          <span className="h-1.5 w-1.5 rounded-full bg-accent" aria-hidden/> Mirroring
        </span>
      )}
    </div>
  );
}

/** Screenshot through the authenticated files lane: bytes → blob URL (the
 * workspace-files endpoint wants a bearer header an <img src> cannot carry). */
function Screenshot({ slug, ws, path, frozen }: { slug: string; ws: string; path: string | null; frozen: boolean }) {
  const [src, setSrc] = useState<string | null>(null);
  const [missing, setMissing] = useState(false);
  const [failed, setFailed] = useState(false);
  const latest = useRef<string | null>(null);

  useEffect(() => {
    let alive = true;
    setSrc(null);
    setMissing(false);
    setFailed(false);
    if (!path || !ws || !slug) return;
    fetchAgentFile(ws, slug, path)
      .then((file) => {
        if (!alive) return;
        if (!file) setMissing(true);
        else {
          const blob = URL.createObjectURL(new Blob([file.bytes], { type: file.contentType || 'image/png' }));
          latest.current = blob;
          setSrc(blob);
        }
      })
      .catch(() => alive && setFailed(true));
    return () => {
      alive = false;
      if (latest.current) URL.revokeObjectURL(latest.current);
      latest.current = null;
    };
  }, [ws, slug, path]);

  return (
    <div className="mt-2 overflow-hidden rounded-xl border border-line bg-surface" data-od-id="panel-browser-screenshot">
      {src ? (
        <img src={src} alt={frozen ? 'Last captured screenshot (frozen session)' : 'Current page screenshot'}
          data-od-id="panel-browser-image"
          className={'block w-full' + (frozen ? ' opacity-80 saturate-50' : '')}/>
      ) : (
        <div className="flex h-40 items-center justify-center px-4 text-center">
          <p className="text-[12px] text-muted" data-od-id={missing ? 'panel-browser-missing' : failed ? 'panel-browser-error' : 'panel-browser-loading'}>
            {missing ? 'Screenshot no longer in the workspace.' : failed ? 'Screenshot could not be loaded.' : 'Loading screenshot…'}
          </p>
        </div>
      )}
    </div>
  );
}

function Feed({ cards }: { cards: BrowserCard[] }) {
  return (
    <div className="mt-3 min-h-0" data-od-id="panel-browser-feed">
      <p className="px-0.5 pb-1 font-mono text-[9px] font-semibold uppercase tracking-[0.14em] text-muted">Actions</p>
      <div className="space-y-0.5">
        {[...cards].reverse().map((c, i) => (
          <div key={(c as any).callId || i} className="flex items-baseline gap-2 rounded-md px-1.5 py-1 hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]">
            <span className="w-16 shrink-0 truncate text-[11px] font-medium text-fg2">{feedName(c)}</span>
            <span className="min-w-0 flex-1 truncate font-mono text-[10px] text-muted" title={feedSummary(c)}>{feedSummary(c)}</span>
            <span className="shrink-0 font-mono text-[10px] text-muted">{c.ms}ms</span>
          </div>
        ))}
      </div>
    </div>
  );
}

registerPanelSource('browser', ({ ctx }) => <BrowserMirror ctx={ctx}/>);
registerPanelCandidate(browserCandidate);
