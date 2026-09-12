import { Fragment, memo, useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { MentionText } from "../ui/MentionText";

import { ToolCall } from "./ToolCall";
import { SchedulerChip } from "./SchedulerChip";
import { BranchPicker } from "./BranchPicker";
import { ReasoningBubble } from "./ReasoningBubble";
const variantsOf = (m) => m.branches || [{ text: m.text, tools: m.tools, reasoning: m.reasoning, parts: m.parts }];

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
// rehype-raw — raw HTML stays escaped by default.
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
  return <ReactMarkdown components={components}>{text}</ReactMarkdown>;
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
  const doCopy = () => { setCopied(true); onCopy(v.text); setTimeout(() => setCopied(false), 1400); };
  const actBtn = 'flex h-7 w-7 items-center justify-center rounded-full text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg';

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
  const renderTool = (t: any, key: any) => (
    // Dots are per-card: a card that already carries its result (or error)
    // shows its latency even while the turn keeps running — only the
    // still-pending call pulses.
    <ToolCall
      key={key}
      t={t}
      running={busy && isLast && !t.res && !t.error}
      approval={approvalFor(t)}
      // The same message's cards feed the ref→name lookup (design D5);
      // undefined is tolerated (no siblings → refs stay ref chips).
      siblings={tools}
    />
  );

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
          // stays expanded ("Thinking…") until text starts landing.
          parts.map((p: any, i: number) => {
            if (p.k === 'tool') {
              const t = (tools || [])[p.i];
              return t ? renderTool(t, 'part-' + i) : null;
            }
            // Whitespace-only segments (some providers emit blank reasoning
            // chunks) render as an empty bubble — skip them.
            if (!p.text || !p.text.trim()) return null;
            const live = streaming && i === parts.length - 1 && n === 0;
            return <ReasoningBubble key={'part-' + i} text={p.text} live={live} odId={'msg-reasoning-' + m.id + '-' + i}/>;
          })
        ) : (
          <>
            {tools && tools.map((t: any, i: number) => renderTool(t, i))}
            {reasoning.length > 0 && (
              // Legacy flat field (seeded/old messages): one reasoning bubble
              // above the text — expanded while the turn streams.
              <ReasoningBubble text={reasoning} live={streaming && n === 0} odId={'msg-reasoning-' + m.id}/>
            )}
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
          <div className="-ms-1.5 mt-1 flex items-center gap-2">
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
          </div>
        )}
      </div>
    </div>
  );
}
