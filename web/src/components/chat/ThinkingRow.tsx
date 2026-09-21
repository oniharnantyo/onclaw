import { useEffect, useState } from "react";
import { Avatar } from "../ui/Avatar";
import { ActivityLabel } from "./ActivityLabel";

/**
 * Pre-first-token waiting row (fix-tool-timeline-fold 4.1): while a turn has
 * produced nothing yet, show the shimmering "Thinking" activity label with a
 * client-measured elapsed time. The row mounts exactly when the turn starts
 * waiting and unmounts when content arrives, so the clock starts on mount and
 * needs no freeze value (present-only, like the ReasoningBubble's live
 * segment). Elapsed is absent until it first ticks, per ActivityLabel.
 */
export function ThinkingRow({ agent  }: any) {
  // Client-measured elapsed since the row mounted; ticks once a second.
  const [ms, setMs] = useState<number | null>(null);
  useEffect(() => {
    const start = Date.now();
    const t = setInterval(() => setMs(Date.now() - start), 1000);
    return () => clearInterval(t);
  }, []);
  return (
    <div className="flex gap-3 px-2 py-1" data-od-id="msg-thinking" role="status" aria-label="Assistant is thinking">
      <Avatar name={agent ? agent.name : 'Agent'} avatar={agent?.avatar} kind="agent" size={26}/>
      <span className="self-center"><ActivityLabel label="Thinking" elapsedMs={ms}/></span>
    </div>
  );
}
