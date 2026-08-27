import { useState, useEffect, useRef } from "react";
import { cx, memberHandle } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { StatusDot } from "../ui/StatusDot";
import { MentionText } from "../ui/MentionText";
import { Chip } from "../ui/Chip";
import { STATUS, COMMANDS } from "../../lib/constants";

import { ToolCall } from "./ToolCall";
import { CronChip } from "./CronChip";
import { BranchPicker } from "./BranchPicker";
export const variantsOf = (m) => m.branches || [{ text: m.text, tools: m.tools }];

export function AgentMessage({ m, agent, inChannel, streaming, busy, isLast, onDone, onCopy, onGrow, onRefresh, onBranch, members  }: any) {
  const variants = variantsOf(m);
  const v = variants[m.branch || 0] || variants[0];
  const [n, setN] = useState(streaming ? 0 : v.text.length);
  const doneRef = useRef(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!streaming) { setN(v.text.length); doneRef.current = false; return; }
    setN(0); doneRef.current = false;
    const iv = setInterval(() => {
      if (onGrow) onGrow();
      setN((x: any) => {
        if (x + 3 >= v.text.length) {
          clearInterval(iv);
          if (!doneRef.current) { doneRef.current = true; setTimeout(onDone, 0); }
          return v.text.length;
        }
        return x + 3;
      });
    }, 22);
    return () => clearInterval(iv);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [streaming, m.id]);

  useEffect(() => {
    if (!streaming) setN(variantsOf(m)[m.branch || 0].text.length);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [m.branch]);

  const shown = v.text.slice(0, n);
  const doCopy = () => { setCopied(true); onCopy(v.text); setTimeout(() => setCopied(false), 1400); };
  const actBtn = 'flex h-7 w-7 items-center justify-center rounded-full text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg';

  return (
    <div className="group flex gap-3 px-2" data-od-id={'msg-' + m.id} data-role="assistant">
      <Avatar name={agent ? agent.name : 'Agent'} kind="agent" size={26}/>
      <div className="min-w-0 flex-1">
        {inChannel && (
          <div className="mb-0.5 flex items-center gap-2">
            <span className="text-[13px] font-semibold text-fg">{agent ? agent.name : 'Agent'}</span>
            {m.cron && <CronChip id={m.cron}/>}
          </div>
        )}
        {!inChannel && m.cron && <div className="mb-1"><CronChip id={m.cron}/></div>}
        {v.tools && v.tools.map((t, i) => <ToolCall key={i} t={t}/>)}
        <div className="whitespace-pre-wrap text-[15px] leading-relaxed text-fg">
          {streaming || n > 0 ? (
            <>
              <MentionText text={shown} members={members}/>
              {streaming && n < v.text.length && <span className="od-caret"/>}
            </>
          ) : (
            <span className="inline-flex items-center gap-1 py-1"><span className="od-dot"/><span className="od-dot"/><span className="od-dot"/></span>
          )}
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
              <button type="button" onClick={() => onRefresh(m.id)} data-od-id={'msg-refresh-' + m.id} aria-label="Refresh — generate a new response" title="Refresh"
                className={actBtn}>
                <Icon name="refresh" size={13}/>
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

