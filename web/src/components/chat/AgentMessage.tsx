import { Fragment, memo, useEffect, useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";
import { cx } from "../../lib/helpers";
import { parseArgs } from "../../lib/toolDisplay";
import { parseTodoPlan, TodoUpdatedSummary } from "../../lib/generativeUi";
import { toolCatalog } from "../../lib/toolCatalog";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { MentionText } from "../ui/MentionText";

import { ToolCall } from "./ToolCall";
import { SchedulerChip } from "./SchedulerChip";
import { BranchPicker } from "./BranchPicker";
import { ReasoningBubble } from "./ReasoningBubble";
import { changedFileCount, ToolTimelineHeader } from "./ToolTimelineHeader";
import { getTurnTiming } from "../../chat/turnTiming";
const variantsOf = (m) => m.branches || [{ text: m.text, tools: m.tools, reasoning: m.reasoning, parts: m.parts }];

// Message timing (adopt-assistant-ui-elements 9.1): hover figures read as
// "first token 0.8 s · total 6.2 s · 42 tok/s" — whole seconds once they run
// past ten.
const fmtSecs = (ms: number) =>
  ms >= 9950 ? `${Math.round(ms / 1000)} s` : `${(ms / 1000).toFixed(1)} s`;

// Tool group timeline collapse (adopt-assistant-ui-elements 10.1, design
// D8): a turn with this many tool calls or more collapses its card stack
// behind the timeline header by default. Collapse is additive, never muting
// — light turns keep their inline cards exactly as before.
const COLLAPSE_THRESHOLD = 4;

// Expanded fold body nests under the header (fix-tool-timeline-fold D8,
// user-approved 2026-09-21 visual pass): the rows sit behind a thin left
// rail so the header reads as their parent, instead of rendering as the
// header's siblings. Collapsed shows only the header; light turns and the
// reply text stay flush.
const FOLD_RAIL = 'ms-3 border-s border-linesoft ps-3';

// Math gate (adopt-assistant-ui-elements 11.1, design D10): block `$$…$$`,
// or inline `$…$` whose edges hug non-space characters — mirroring
// remark-math's own fence rules so prose money ("$5 and $10") stays prose.
// This gate decides whether the KaTeX bundle and stylesheet load at all.
export function containsMathDelimiters(text: string): boolean {
  return /\$\$[\s\S]+?\$\$|\$[^\s$](?:[^$\n]*[^\s$])?\$/.test(text);
}

// react-markdown replaces raw text nodes with element trees, so mention
// highlighting is applied by overriding text-bearing renderers: every string
// child goes through the same MentionText splitter used elsewhere in chat.
function withMentions(node: any, members: any[]): any {
  if (node == null || typeof node === 'boolean') return node;
  if (typeof node === 'string' || typeof node === 'number') {
    return <MentionText text={String(node)} members={members}/>;
  }
  if (Array.isArray(node)) {
    return node.map((c, i) => <Fragment key={i}>{withMentions(c, members)}</Fragment>);
  }
  return node;
}

// Memoized per design D6: streaming re-parses per delta, so the tree is only
// rebuilt when the text (or member list) actually changes. No GFM plugin, no
// rehype-raw — raw HTML stays escaped by default. Math (design D10): the
// remark-math + rehype-katex plugins and KaTeX's stylesheet (~300KB gz) load
// lazily, only when the message actually carries math delimiters — until
// they land (or on a failed chunk load) the raw delimiters render as plain
// text, exactly as before. Non-math messages never touch the bundle.
const MarkdownBody = memo(function MarkdownBody({ text, members }: { text: string; members?: any[] }) {
  const components = useMemo(() => ({
    p: ({ children }) => <p className="mb-2 last:mb-0">{withMentions(children, members)}</p>,
    li: ({ children }) => <li className="mb-1 ml-5 list-disc">{withMentions(children, members)}</li>,
    a: ({ href, children }) => <a href={href} target="_blank" rel="noreferrer" className="text-accent underline underline-offset-2 hover:opacity-80">{children}</a>,
    blockquote: ({ children }) => <blockquote className="mb-2 border-s-2 border-line ps-3 text-fg2">{children}</blockquote>,
    code: ({ children }) => (
      <code className="rounded-[4px] bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] px-1 py-0.5 font-mono text-[13px]">{children}</code>
    ),
    pre: ({ children }) => (
      <pre className="od-scroll mb-2 overflow-x-auto rounded-lg border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] p-3 font-mono text-[12.5px] leading-5 text-fg2">{children}</pre>
    ),
  }), [members]);
  const hasMath = useMemo(() => containsMathDelimiters(text), [text]);
  const [math, setMath] = useState<{ remark: any; rehype: any } | null>(null);
  useEffect(() => {
    if (!hasMath) return;
    let alive = true;
    Promise.all([
      import('remark-math'),
      import('rehype-katex'),
      // The stylesheet rides the same lazy chunk — nothing KaTeX-related is
      // in the initial bundle (11.1).
      import('katex/dist/katex.min.css'),
    ]).then(([remarkMath, rehypeKatex]) => {
      if (alive) setMath({ remark: remarkMath.default, rehype: rehypeKatex.default });
    }).catch(() => {
      // Degradation is present-only: without the plugins the delimiters
      // stay plain text; the message render never breaks.
    });
    return () => { alive = false; };
  }, [hasMath]);
  return (
    <ReactMarkdown
      components={components}
      remarkPlugins={math ? [math.remark] : []}
      rehypePlugins={math ? [math.rehype] : []}
    >{text}</ReactMarkdown>
  );
});

export function AgentMessage({ m, agent, inChannel, busy, isLast, onCopy, onRefresh, onBranch, members, onResolveApproval, sessionId }: any) {
  const variants = variantsOf(m);
  const v = variants[m.branch || 0] || variants[0];
  const [copied, setCopied] = useState(false);
  const n = v.text.length;

  const streaming = busy && isLast;
  const reasoning: string = (v.reasoning ?? m.reasoning) || '';
  const parts: any[] | undefined = v.parts ?? m.parts;
  const tools: any[] | undefined = v.tools ?? m.tools;
  const shown = v.text;
  // Turn elapsed clock (fix-tool-timeline-fold D4): one client-measured
  // figure for the whole turn, ticking while the turn streams. It feeds the
  // timeline header's activity label; when the turn ends the tick stops and
  // the header yields to the resting summary, so no frozen value is kept.
  // Hydrated turns never stream, so they never render an elapsed figure —
  // present-only honesty, like the tool card's latency row.
  const [turnElapsedMs, setTurnElapsedMs] = useState<number | null>(null);
  useEffect(() => {
    if (!streaming) return;
    const startedAt = Date.now();
    setTurnElapsedMs(Date.now() - startedAt);
    const tick = setInterval(() => setTurnElapsedMs(Date.now() - startedAt), 1000);
    return () => clearInterval(tick);
  }, [streaming]);
  // Pending-tool activity label (fix-tool-timeline-fold D2+D3): while
  // streaming, the LAST still-pending call names the header's activity —
  // resolved through the same catalog display-name lookup the cards use, so
  // it reads "Running Shell", never "Running execute". Nothing pending (the
  // model is between calls) passes null and the header falls back to
  // "Thinking". Not streaming → null, no activity is claimed.
  const pendingDisplay = (() => {
    if (!streaming || !Array.isArray(tools)) return null;
    for (let i = tools.length - 1; i >= 0; i -= 1) {
      const t = tools[i];
      if (t && !t.res && !t.error) return toolCatalog.displayName(t.name) ?? t.name;
    }
    return null;
  })();
  // Heavy turn (10.1–10.2): ≥COLLAPSE_THRESHOLD tool calls render the
  // timeline header instead of the card stack by default. Open state is
  // user-controlled — it persists for the turn's life and never auto-expands.
  const heavy = !!tools && tools.length >= COLLAPSE_THRESHOLD;
  const [groupOpen, setGroupOpen] = useState(false);
  const showCards = !heavy || groupOpen;
  const filesChanged = useMemo(() => (heavy ? changedFileCount(tools) : 0), [heavy, tools]);
  const doCopy = () => { setCopied(true); onCopy(v.text); setTimeout(() => setCopied(false), 1400); };
  const actBtn = 'flex h-7 w-7 items-center justify-center rounded-full text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg';

  // Message timing (9.1): client-measured figures from the runtime-local
  // registry, revealed on hover of the reply's action row. Present-only
  // twice over — the registry holds entries for completed live turns only
  // (hydrated history has none), and the line never renders while the turn
  // is still streaming.
  const timing = !streaming ? getTurnTiming(m.id) : undefined;
  const [actHover, setActHover] = useState(false);

  // Approval cards resolve against their real native addresses: the
  // session/interrupt ids threaded from the onclaw:approval_required
  // payload. Only cards that carry (or belong to) a bound session are
  // actionable — legacy decorative cards render read-only.
  const approvalFor = (t: any) => {
    const approvalSession =
      t.approval && (t.approval.sessionId || t.approval.session_id
        || (typeof sessionId === 'string' && sessionId.startsWith('sess_') ? sessionId : null));
    return onResolveApproval && t.approval && approvalSession
      ? (interruptId: string, approved: boolean) =>
          onResolveApproval(agent?.slug || m.agentId, approvalSession, interruptId, approved)
      : undefined;
  };
  // Todo rewrite collapse (adopt-assistant-ui-elements 6.2): in a turn with
  // multiple todo_write calls, earlier ones collapse to a one-line "plan
  // updated" summary — only the newest renders the full checklist. A turn
  // with no todo_write renders nothing todo-related (present-only).
  const lastTodoWrite = (() => {
    if (!Array.isArray(tools)) return -1;
    for (let i = tools.length - 1; i >= 0; i -= 1) {
      if (tools[i]?.name === 'todo_write') return i;
    }
    return -1;
  })();
  const renderTool = (t: any, key: any, index: number) => {
    if (t.name === 'todo_write' && index !== lastTodoWrite) {
      const plan = parseTodoPlan(parseArgs(t.args), parseArgs(t.res));
      return <TodoUpdatedSummary key={key} odId={'todo-updated-' + index} revision={plan ? plan.revision : null}/>;
    }
    // Dots are per-card: a card that already carries its result (or error)
    // shows its latency even while the turn keeps running — only the
    // still-pending call pulses. `live` latches reveal-style generative-UI
    // elements: hydrated turns skip the stagger (streaming honesty).
    return (
      <ToolCall
        key={key}
        t={t}
        running={busy && isLast && !t.res && !t.error}
        approval={approvalFor(t)}
        // The same message's cards feed the ref→name lookup (design D5);
        // undefined is tolerated (no siblings → refs stay ref chips).
        siblings={tools}
        live={busy && isLast}
      />
    );
  };
  // Ordered activity body (parts path), hoisted so the expanded fold can
  // mount it either flush (light turn) or inside the left rail (heavy).
  const partsBody = parts && parts.length > 0
    ? parts.map((p: any, i: number) => {
        if (p.k === 'tool') {
          const t = (tools || [])[p.i];
          return t ? renderTool(t, 'part-' + i, p.i) : null;
        }
        // Whitespace-only segments (some providers emit blank reasoning
        // chunks) render as an empty bubble — skip them.
        if (!p.text || !p.text.trim()) return null;
        const live = streaming && i === parts.length - 1 && n === 0;
        return <ReasoningBubble key={'part-' + i} text={p.text} live={live} odId={'msg-reasoning-' + m.id + '-' + i}/>;
      })
    : null;

  return (
    <div className="group flex gap-3 px-2" data-od-id={'msg-' + m.id} data-role="assistant">
      <Avatar name={agent ? agent.name : 'Agent'} avatar={agent?.avatar} kind="agent" size={26}/>
      <div className="min-w-0 flex-1">
        {inChannel && (
          <div className="mb-0.5 flex items-center gap-2">
            <span className="text-[13px] font-semibold text-fg">{agent ? agent.name : 'Agent'}</span>
            {m.scheduler !== undefined && <SchedulerChip name={m.scheduler}/>}
          </div>
        )}
        {!inChannel && m.scheduler !== undefined && <div className="mb-1"><SchedulerChip name={m.scheduler}/></div>}
        {parts && parts.length > 0 ? (
          // Ordered turn body (design D6/D2): each reasoning segment is its
          // own bubble, interleaved with the tool cards in stream order —
          // reasoning → tool call → reasoning → response. The tail segment
          // stays expanded ("Thinking…") until text starts landing. On a
          // heavy turn (10.1) the fold owns the whole activity stream
          // (fix-tool-timeline-fold): the timeline header hides the cards
          // AND the reasoning rows until the user expands it — a collapsed
          // "6 steps" turn must not leak orphaned Thought rows above it.
          // Expanding reveals them nested under the header behind the left
          // rail (D8) — the header reads as their parent, not a sibling.
          <>
            {heavy && (
              <ToolTimelineHeader
                odId={'tool-group-' + m.id}
                steps={tools ? tools.length : 0}
                filesChanged={filesChanged}
                open={groupOpen}
                onToggle={() => setGroupOpen(!groupOpen)}
                streaming={streaming}
                activityLabel={pendingDisplay ? 'Running ' + pendingDisplay : null}
                elapsedMs={turnElapsedMs}
              />
            )}
            {showCards && (heavy ? <div className={FOLD_RAIL}>{partsBody}</div> : partsBody)}
          </>
        ) : (
          <>
            {heavy && (
              <ToolTimelineHeader
                odId={'tool-group-' + m.id}
                steps={tools ? tools.length : 0}
                filesChanged={filesChanged}
                open={groupOpen}
                onToggle={() => setGroupOpen(!groupOpen)}
                streaming={streaming}
                activityLabel={pendingDisplay ? 'Running ' + pendingDisplay : null}
                elapsedMs={turnElapsedMs}
              />
            )}
            {showCards && (heavy ? (
              <div className={FOLD_RAIL}>
                {tools && tools.map((t: any, i: number) => renderTool(t, i, i))}
                {reasoning.length > 0 && (
                  // Legacy flat field (seeded/old messages): one reasoning
                  // bubble above the text — expanded while the turn streams.
                  // Same gate + rail as the parts path (fix-tool-timeline-fold).
                  <ReasoningBubble text={reasoning} live={streaming && n === 0} odId={'msg-reasoning-' + m.id}/>
                )}
              </div>
            ) : (
              <>
                {tools && tools.map((t: any, i: number) => renderTool(t, i, i))}
                {reasoning.length > 0 && (
                  <ReasoningBubble text={reasoning} live={streaming && n === 0} odId={'msg-reasoning-' + m.id}/>
                )}
              </>
            ))}
          </>
        )}

        <div className="md-body text-[15px] leading-relaxed text-fg">
          {n > 0 ? (
            <>
              <MarkdownBody text={shown} members={members}/>
              {busy && isLast && <span className="od-caret" aria-hidden="true"/>}
            </>
          ) : busy && isLast ? (
            // Loading placeholder only while this turn is actually streaming —
            // a message left empty by a failed/retracted turn renders nothing.
            <span className="inline-flex items-center gap-1 py-1"><span className="od-dot"/><span className="od-dot"/><span className="od-dot"/></span>
          ) : null}
        </div>
        {!busy && (
          <div className="-ms-1.5 mt-1 flex items-center gap-2" data-od-id={'msg-actions-' + m.id}
            onMouseEnter={() => setActHover(true)} onMouseLeave={() => setActHover(false)}>
            {variants.length > 1 && (
              <BranchPicker index={m.branch || 0} count={variants.length}
                onPrev={() => onBranch(m.id, -1)} onNext={() => onBranch(m.id, 1)}/>
            )}
            <div className={cx('flex items-center gap-0.5', !isLast && 'opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100')}>
              <button type="button" onClick={doCopy} data-od-id={'msg-copy-' + m.id} aria-label="Copy message" title="Copy"
                className={actBtn}>
                <Icon name={copied ? 'check' : 'copy'} size={13}/>
              </button>
              {!inChannel && (
                <button type="button" onClick={() => onRefresh(m.id)} data-od-id={'msg-refresh-' + m.id} aria-label="Refresh — generate a new response" title="Refresh"
                  className={actBtn}>
                  <Icon name="refresh" size={13}/>
                </button>
              )}
            </div>
            {timing && (
              <span data-od-id={'msg-timing-' + m.id}
                className={cx('text-[11px] leading-7 text-muted transition-opacity duration-150', actHover ? 'opacity-100' : 'opacity-0')}>
                first token {fmtSecs(timing.firstMs)} · total {fmtSecs(timing.totalMs)}{timing.tps ? ` · ${timing.tps} tok/s` : ''}
              </span>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
