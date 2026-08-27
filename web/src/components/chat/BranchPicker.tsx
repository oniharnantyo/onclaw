// @ts-nocheck
import React from "react";
import { cx, memberHandle } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { StatusDot } from "../ui/StatusDot";
import { MentionText } from "../ui/MentionText";
import { Chip } from "../ui/Chip";
import { STATUS, COMMANDS } from "../../data/seed";

export function BranchPicker({ index, count, onPrev, onNext }) {
  const btn = 'flex h-6 w-6 items-center justify-center rounded-full text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg disabled:pointer-events-none disabled:opacity-30';
  return (
    <div className="inline-flex items-center gap-0.5 text-[11px] text-muted" data-od-id="branch-picker">
      <button type="button" className={btn} onClick={onPrev} disabled={index <= 0} aria-label="Previous response" title="Previous response">
        <Icon name="chevleft" size={13}/>
      </button>
      <span className="min-w-9 text-center font-medium tabular-nums">{index + 1} / {count}</span>
      <button type="button" className={btn} onClick={onNext} disabled={index >= count - 1} aria-label="Next response" title="Next response">
        <Icon name="chevright" size={13}/>
      </button>
    </div>
  );
}

