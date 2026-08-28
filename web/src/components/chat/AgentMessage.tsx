import { useState } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { MentionText } from "../ui/MentionText";

import { ToolCall } from "./ToolCall";
import { CronChip } from "./CronChip";
import { BranchPicker } from "./BranchPicker";
const variantsOf = (m) => m.branches || [{ text: m.text, tools: m.tools }];

export function AgentMessage({ m, agent, inChannel, busy, isLast, onCopy, onRefresh, onBranch, members  }: any) {
  const variants = variantsOf(m);
  const v = variants[m.branch || 0] || variants[0];
  const [copied, setCopied] = useState(false);
  const n = v.text.length;

  const shown = v.text;
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
        {v.tools && v.tools.map((t: any, i: number) => <ToolCall key={i} t={t} running={busy && isLast}/>)}
        <div className="whitespace-pre-wrap text-[15px] leading-relaxed text-fg">
          {n > 0 ? (
            <>
              <MentionText text={shown} members={members}/>
              {busy && isLast && <span className="od-caret" aria-hidden="true"/>}
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

