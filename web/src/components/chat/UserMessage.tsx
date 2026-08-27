import { useState, useEffect, useRef } from "react";
import { cx, memberHandle } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { StatusDot } from "../ui/StatusDot";
import { MentionText } from "../ui/MentionText";
import { Chip } from "../ui/Chip";
import { STATUS, COMMANDS } from "../../lib/constants";

export function UserMessage({ m, onEdit, members  }: any) {
  const [editing, setEditing] = useState(false);
  const [val, setVal] = useState(m.text);
  const startEdit = () => { setVal(m.text); setEditing(true); };
  const submitEdit = () => {
    const t = val.trim();
    setEditing(false);
    if (t && t !== m.text) onEdit(m.id, t);
  };
  if (editing) {
    return (
      <div className="flex flex-col px-2" data-od-id={'msg-' + m.id}>
        <div className="ms-auto flex w-full max-w-[85%] flex-col rounded-[24px] border border-line bg-surface p-2 transition-colors focus-within:border-accent">
          <textarea autoFocus rows={Math.min(6, val.split('\n').length + 1)} value={val} aria-label="Edit message"
            onChange={(e) => setVal(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') { e.preventDefault(); setEditing(false); }
              if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submitEdit(); }
            }}
            className="w-full resize-none bg-transparent px-2.5 py-1.5 text-[15px] leading-6 text-fg outline-none"/>
          <div className="mx-1 mb-1 flex items-center justify-end gap-1.5">
            <button type="button" onClick={() => setEditing(false)}
              className="h-8 rounded-full px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg">
              Cancel
            </button>
            <button type="button" onClick={submitEdit} data-od-id="btn-update-message"
              className="h-8 rounded-full bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]">
              Update
            </button>
          </div>
        </div>
      </div>
    );
  }
  return (
    <div className="group flex justify-end px-2" data-od-id={'msg-' + m.id} data-role="user">
      <div className="relative max-w-[85%]">
        <button type="button" onClick={startEdit} aria-label="Edit message" title="Edit"
          className="absolute right-full top-1/2 mr-2 flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded-full text-muted opacity-0 transition-opacity hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg focus-visible:opacity-100 group-hover:opacity-100">
          <Icon name="edit" size={13}/>
        </button>
        <div className="whitespace-pre-wrap rounded-xl bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] px-4 py-2 text-[15px] leading-relaxed text-fg">
          <MentionText text={m.text} members={members}/>
        </div>
      </div>
    </div>
  );
}

