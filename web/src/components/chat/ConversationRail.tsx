// Conversation rail — the vendored assistant-ui `conversation-map` element
// wired to OnClaw's transcript. A w-6 vertical minimap of the conversation:
// one tick per rendered entry, the on-screen ones mid-length, the current
// (nearest-to-top) one long, the off-screen ones dim. Click a tick to jump
// to that message; arrow keys walk the rail. Hovering a tick floats a
// preview card with a one-line digest of the message.
//
// State is derived from the transcript itself — no runtime cooperation:
//   activeId   — the entry nearest the top of the scroll viewport.
//   visibleIds — every entry whose box intersects the viewport.
//   onSelect   — scrollIntoView of the entry node.
// Measurement is a plain scroll listener + getBoundingClientRect over the
// `[data-msg-id]` nodes ChatView renders (not IntersectionObserver, so the
// behavior stays testable in jsdom). Entries are read through a ref: the
// array is a fresh slice every render, so depending on it directly would
// resubscribe every render and re-set state in a loop.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ConversationMap, type ConversationMapEntry } from "@/components/assistant-ui/elements/conversation-map";

// One-line digest for the hover preview: the message's own text when it has
// any, else a kind label per author. Truncated by the popup's line-clamp.
function digestOf(m: any): string {
  const text = (m?.text || '').trim();
  if (text) return text;
  switch (m?.author) {
    case 'you': return 'Your message';
    case 'other': return m?.name ? `Message from ${m.name}` : 'Member message';
    case 'agent': return 'Agent reply';
    case 'error': return m?.error ? String(m.error) : 'Run failed';
    case 'notice': return m?.notice?.hook ? `Blocked by ${m.notice.hook}` : 'Prompt blocked';
    case 'memory': return 'Memory ingested';
    case 'compaction': return 'Context compacted';
    default: return 'Message';
  }
}

function titleOf(m: any): string {
  const text = (m?.text || '').trim();
  if (text) return text.length > 60 ? text.slice(0, 60) + '…' : text;
  switch (m?.author) {
    case 'you': return 'You';
    case 'other': return m?.name || 'Member';
    case 'agent': return 'Agent';
    case 'error': return 'Run failed';
    case 'notice': return 'Prompt blocked';
    case 'memory': return 'Memory ingested';
    case 'compaction': return 'Context compacted';
    default: return 'Message';
  }
}

export function ConversationRail({ entries, listRef }: {
  entries: any[];
  listRef: React.RefObject<HTMLElement | null>;
}) {
  const [activeId, setActiveId] = useState<string | undefined>(undefined);
  const [visibleIds, setVisibleIds] = useState<readonly string[]>([]);
  const rafRef = useRef<number | null>(null);
  const entriesRef = useRef(entries);
  entriesRef.current = entries;

  const mapEntries: ConversationMapEntry[] = useMemo(
    () => entries.map((m) => ({ id: m.id, title: titleOf(m), preview: digestOf(m) })),
    [entries],
  );

  const measure = useCallback(() => {
    const list = listRef.current;
    if (!list) return;
    const listRect = list.getBoundingClientRect();
    const inView: string[] = [];
    let nearest: string | undefined;
    let nearestDist = Number.POSITIVE_INFINITY;
    for (const m of entriesRef.current) {
      const el = list.querySelector<HTMLElement>(`[data-msg-id="${m.id}"]`);
      if (!el) continue;
      const r = el.getBoundingClientRect();
      if (r.bottom > listRect.top && r.top < listRect.bottom) {
        inView.push(m.id);
        // Nearest to the viewport top wins the "current" tick.
        const dist = Math.abs(r.top - listRect.top);
        if (dist < nearestDist) {
          nearestDist = dist;
          nearest = m.id;
        }
      }
    }
    // Identity-stable update: a fresh array every measurement would re-render
    // on every scroll frame.
    setVisibleIds((prev) =>
      prev.length === inView.length && prev.every((id, i) => id === inView[i]) ? prev : inView,
    );
    if (nearest) setActiveId(nearest);
  }, [listRef]);

  useEffect(() => {
    const list = listRef.current;
    if (!list) return;
    const schedule = () => {
      if (rafRef.current !== null) return;
      rafRef.current = requestAnimationFrame(() => {
        rafRef.current = null;
        measure();
      });
    };
    measure();
    list.addEventListener('scroll', schedule, { passive: true });
    window.addEventListener('resize', schedule);
    return () => {
      list.removeEventListener('scroll', schedule);
      window.removeEventListener('resize', schedule);
      if (rafRef.current !== null) cancelAnimationFrame(rafRef.current);
    };
  }, [listRef, measure]);

  // The transcript grows between renders; re-measure once it has.
  useEffect(() => {
    measure();
  }, [entries.length, measure]);

  const onSelect = useCallback(
    (id: string) => {
      const list = listRef.current;
      const el = list?.querySelector<HTMLElement>(`[data-msg-id="${id}"]`);
      // Optional call: jsdom (tests) has no scrollIntoView.
      el?.scrollIntoView?.({ behavior: 'smooth', block: 'start' });
    },
    [listRef],
  );

  if (mapEntries.length < 2) return null;

  return (
    // A flex child at the left edge of the chat pane (before the transcript
    // column). Fixed 2.25rem gutter so the rail never crowds the messages;
    // hidden below md where the gutter does not exist.
    <div data-od-id="conversation-rail" className="hidden w-9 shrink-0 md:block">
      <ConversationMap
        entries={mapEntries}
        activeId={activeId}
        visibleIds={visibleIds}
        onSelect={onSelect}
        side="right"
      />
    </div>
  );
}
