// @ts-nocheck
import { useState, useEffect, useRef } from "react";
import { cx, memberHandle } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { StatusDot } from "../ui/StatusDot";
import { MentionText } from "../ui/MentionText";
import { Chip } from "../ui/Chip";
import { STATUS, COMMANDS } from "../../data/seed";

import { SlashMenu } from "./SlashMenu";
import { MentionMenu } from "./MentionMenu";

export function Composer({ agent, running, onSend, onCancel, onAttach, mentionOptions }) {
  const [text, setText] = useState('');
  const [idx, setIdx] = useState(0);
  const ta = useRef(null);
  const slashQ = text.startsWith('/') ? text.split(' ')[0] : '';
  const slashOpen = !!slashQ && !text.includes(' ') && COMMANDS.some((c) => c.cmd.startsWith(slashQ.toLowerCase()));
  const slashList = slashOpen ? COMMANDS.filter((c) => c.cmd.startsWith(slashQ.toLowerCase())) : [];
  const mentionMatch = mentionOptions ? (text.match(/@([A-Za-z]*)$/) || null) : null;
  const mentionList = mentionMatch
    ? mentionOptions.filter((m) => memberHandle(m).startsWith(mentionMatch[1].toLowerCase()))
    : [];
  const menu = slashOpen ? 'slash' : (mentionMatch && mentionList.length ? 'mention' : null);
  useEffect(() => { setIdx(0); }, [menu]);

  const grow = () => { const el = ta.current; if (el) { el.style.height = 'auto'; el.style.height = Math.min(192, el.scrollHeight) + 'px'; } };
  const submit = (raw) => {
    if (running) return;
    const val = (raw !== undefined ? raw : text).trim();
    if (!val) return;
    onSend(val);
    setText('');
    requestAnimationFrame(grow);
  };

  const pickMention = (m) => {
    const handle = m.kind === 'agent' ? m.name : m.name.split(' ')[0];
    setText(text.replace(/@[A-Za-z]*$/, '@' + handle + ' '));
    requestAnimationFrame(() => ta.current && ta.current.focus());
  };

  const onKeyDown = (e) => {
    if (menu && e.key === 'ArrowDown') { e.preventDefault(); const n = menu === 'slash' ? slashList.length : mentionList.length; setIdx((i) => Math.min(n - 1, i + 1)); return; }
    if (menu && e.key === 'ArrowUp') { e.preventDefault(); setIdx((i) => Math.max(0, i - 1)); return; }
    if (e.key === 'Escape' && menu) { e.preventDefault(); return; }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      if (menu === 'slash' && slashList[idx]) { setText(slashList[idx].cmd + ' '); setIdx(0); return; }
      if (menu === 'mention' && mentionList[idx]) { pickMention(mentionList[idx]); setIdx(0); return; }
      submit();
    }
  };

  return (
    <div className="relative" data-od-id="composer">
      {menu === 'slash' && <SlashMenu q={slashQ} idx={idx} onPick={(cmd) => { setText(cmd + ' '); setIdx(0); ta.current && ta.current.focus(); }}/>}
      {menu === 'mention' && <MentionMenu options={mentionList} idx={idx} onPick={pickMention}/>}
      <div role="presentation" onClick={() => ta.current && ta.current.focus()}
        className="cursor-text rounded-[24px] border border-line bg-surface p-2 transition-colors focus-within:border-accent">
        <textarea ref={ta} rows={1} value={text} autoFocus aria-label={'Message input'}
          onChange={(e) => { setText(e.target.value); grow(); }} onKeyDown={onKeyDown}
          placeholder={mentionOptions ? 'Message the channel — @ to mention' : agent ? 'Message ' + agent.name + '…' : 'Send a message…'}
          enterKeyHint="send"
          className="max-h-48 min-h-10 w-full resize-none bg-transparent px-2.5 py-1.5 text-[15px] leading-6 text-fg outline-none placeholder:text-muted"/>
        <div className="flex items-center justify-between pt-0.5">
          <button type="button" onClick={(e) => { e.stopPropagation(); onAttach(); }} aria-label="Attach a file" title="Attach a file"
            className="flex h-7 w-7 items-center justify-center rounded-full text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg">
            <Icon name="clip" size={14}/>
          </button>
          {!running ? (
            <button type="button" onClick={(e) => { e.stopPropagation(); submit(); }} disabled={!text.trim()} data-od-id="btn-send"
              aria-label="Send message" title="Send message"
              className="flex h-8 w-8 items-center justify-center rounded-full bg-accent text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-35 disabled:hover:bg-accent">
              <Icon name="up" size={15} sw={2.4}/>
            </button>
          ) : (
            <button type="button" onClick={(e) => { e.stopPropagation(); onCancel(); }} data-od-id="btn-cancel"
              aria-label="Stop generating" title="Stop generating"
              className="flex h-8 w-8 items-center justify-center rounded-full bg-accent text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]">
              <Icon name="stop" size={12}/>
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

