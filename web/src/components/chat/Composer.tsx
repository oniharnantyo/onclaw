import { useState, useRef } from "react";
import { memberHandle } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { COMMANDS } from "../../lib/constants";

import { SlashMenu } from "./SlashMenu";
import { MentionMenu } from "./MentionMenu";
import { SkillMenu } from "./SkillMenu";
import type { SkillMenuGroup } from "./SkillMenu";

export function Composer({ agent, running, onSend, onCancel, onAttach, mentionOptions, skillGroups  }: any) {
  const [text, setText] = useState('');
  const [idx, setIdx] = useState(0);
  const [menuDismissed, setMenuDismissed] = useState(false);
  const ta = useRef(null);
  const slashQ = text.startsWith('/') ? text.split(' ')[0] : '';
  const slashOpen = !!slashQ && !text.includes(' ') && COMMANDS.some((c) => c.cmd.startsWith(slashQ.toLowerCase()));
  const slashList = slashOpen ? COMMANDS.filter((c) => c.cmd.startsWith(slashQ.toLowerCase())) : [];
  const mentionMatch = mentionOptions ? (text.match(/@([A-Za-z]*)$/) || null) : null;
  const mentionList = mentionMatch
    ? mentionOptions.filter((m: any) => memberHandle(m).startsWith(mentionMatch[1].toLowerCase()))
    : [];
  // $token triggers the skill invocation menu (groups: System / Workspace /
  // This agent). An unmatched $name sends as ordinary text.
  const skillMatch = skillGroups && skillGroups.length ? (text.match(/(^|\s)\$([A-Za-z0-9-]*)$/) || null) : null;
  const skillQuery = skillMatch ? skillMatch[2] : '';
  const skillList = skillMatch
    ? (skillGroups as SkillMenuGroup[])
        .flatMap((g) => g.skills)
        .filter((s) => s.name.toLowerCase().startsWith(skillQuery.toLowerCase()))
    : [];
  const menu = menuDismissed
    ? null
    : slashOpen
      ? 'slash'
      : mentionMatch && mentionList.length
        ? 'mention'
        : skillMatch && skillList.length
          ? 'skill'
          : null;
  const menuLength = menu === 'slash' ? slashList.length : menu === 'mention' ? mentionList.length : skillList.length;
  const [lastMenu, setLastMenu] = useState(menu);
  if (menu !== lastMenu) { setLastMenu(menu); setIdx(0); }

  const grow = () => { const el = ta.current; if (el) { el.style.height = 'auto'; el.style.height = Math.min(192, el.scrollHeight) + 'px'; } };
  const submit = (raw?: any) => {
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
    requestAnimationFrame(() => { if (ta.current) ta.current.focus(); });
  };

  const pickSkill = (name: string) => {
    setText(text.replace(/(^|\s)\$[A-Za-z0-9-]*$/, (m0, pre) => (pre || '') + '$' + name + ' '));
    setIdx(0);
    requestAnimationFrame(() => { if (ta.current) ta.current.focus(); });
  };

  const onKeyDown = (e) => {
    if (menu && e.key === 'ArrowDown') { e.preventDefault(); setIdx((i) => Math.min(menuLength - 1, i + 1)); return; }
    if (menu && e.key === 'ArrowUp') { e.preventDefault(); setIdx((i) => Math.max(0, i - 1)); return; }
    if (e.key === 'Escape' && menu) { e.preventDefault(); setMenuDismissed(true); return; }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      if (menu === 'slash' && slashList[idx]) { setText(slashList[idx].cmd + ' '); setIdx(0); return; }
      if (menu === 'mention' && mentionList[idx]) { pickMention(mentionList[idx]); setIdx(0); return; }
      if (menu === 'skill' && skillList[idx]) { pickSkill(skillList[idx].name); return; }
      submit(undefined as any);
    }
  };

  return (
    <div className="relative" data-od-id="composer">
      {menu === 'slash' && <SlashMenu q={slashQ} idx={idx} onPick={(cmd) => { setText(cmd + ' '); setIdx(0); if (ta.current) ta.current.focus(); }}/>}
      {menu === 'mention' && <MentionMenu options={mentionList} idx={idx} onPick={pickMention}/>}
      {menu === 'skill' && <SkillMenu groups={skillGroups} query={skillQuery} idx={idx} onPick={pickSkill}/>}
      <div onClick={() => { if (ta.current) ta.current.focus(); }}
        className="cursor-text rounded-[24px] border border-line bg-surface p-2 transition-colors focus-within:border-accent">
        <textarea ref={ta} rows={1} value={text} autoFocus aria-label={'Message input'}
          onChange={(e) => { setText(e.target.value); setMenuDismissed(false); grow(); }} onKeyDown={onKeyDown}
          placeholder={mentionOptions ? 'Message the channel — @ to mention' : agent ? 'Message ' + agent.name + '…' : 'Send a message…'}
          enterKeyHint="send"
          className="max-h-48 min-h-10 w-full resize-none bg-transparent px-2.5 py-1.5 text-[15px] leading-6 text-fg outline-none placeholder:text-muted"/>
        <div className="flex items-center justify-between pt-0.5">
          <button type="button" onClick={(e) => { e.stopPropagation(); onAttach(); }} aria-label="Attach a file" title="Attach a file"
            className="flex h-7 w-7 items-center justify-center rounded-full text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg">
            <Icon name="clip" size={14}/>
          </button>
          {!running ? (
            <button type="button" onClick={(e) => { e.stopPropagation(); submit(undefined as any); }} disabled={!text.trim()} data-od-id="btn-send"
              aria-label="Send message" title="Send message"
              className="flex h-8 w-8 items-center justify-center rounded-full bg-accent text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-35 disabled:hover:bg-accent">
              <Icon name="up" size={15} sw={2.4}/>
            </button>
          ) : (
            <button type="button" onClick={(e) => { e.stopPropagation(); onCancel(null as any); }} data-od-id="btn-cancel"
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

