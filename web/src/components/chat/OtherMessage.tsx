// @ts-nocheck
import React from "react";
import { cx, memberHandle } from "../../lib/helpers";
import { Icon } from "../ui/Icon";
import { Avatar } from "../ui/Avatar";
import { StatusDot } from "../ui/StatusDot";
import { MentionText } from "../ui/MentionText";
import { Chip } from "../ui/Chip";
import { STATUS, COMMANDS } from "../../data/seed";

export function OtherMessage({ m, members }) {
  return (
    <div className="flex flex-col px-2" data-od-id={'msg-' + m.id} data-role="member">
      <div className="mb-1 flex items-center gap-2">
        <Avatar name={m.name || 'Member'} size={20}/>
        <span className="text-[13px] font-semibold text-fg">{m.name || 'Member'}</span>
      </div>
      <p className="whitespace-pre-wrap text-[15px] leading-relaxed text-fg"><MentionText text={m.text} members={members}/></p>
    </div>
  );
}

