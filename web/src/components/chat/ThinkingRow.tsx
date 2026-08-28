import React from "react";
import { Avatar } from "../ui/Avatar";

export function ThinkingRow({ agent  }: any) {
  return (
    <div className="flex gap-3 px-2 py-1" data-od-id="msg-thinking" role="status" aria-label="Assistant is thinking">
      <Avatar name={agent ? agent.name : 'Agent'} kind="agent" size={26}/>
      <span className="inline-block h-2 w-2 animate-pulse self-center rounded-full bg-fg"/>
    </div>
  );
}

