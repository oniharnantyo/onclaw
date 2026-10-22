/**
 * Turn activity (task 3.2, spec chat-view "Chat transcript rendering"): the
 * turn-activity line at the tail of the in-flight turn — the typing
 * indicator before the first token arrives, and once a tool is running at
 * the tail, a live status line naming the running tool and its elapsed
 * time. The line disappears when the turn reaches a terminal outcome and
 * holds under reduced motion (static text, no spinner). Mounted once after
 * the message list, so the "no assistant output yet" state — where no
 * assistant message exists to hang an indicator on — shows the typing dots
 * too. The message list's own synthetic indicator is disabled (the
 * assistant message returns null for it) so the tail can never double up.
 */
import { useEffect, useState } from "react";
import type { ReactElement } from "react";

import { TypingIndicator } from "@/components/assistant-ui/elements/typing-indicator";
import { useAuiState, type AssistantState } from "@assistant-ui/react";

import { formatElapsed, toolDisplayInfo } from "../tools/toolDisplay";
import { useReducedMotion } from "../tools/useReducedMotion";
import { useChatStore, type ExtendedChatStore } from "../store";

/** The start timestamp of the entry a tool-call part was mapped from (part
 * ids are entry ids, design D2) — the elapsed clock's zero. */
function entryStart(
  entries: ExtendedChatStore["entries"],
  entryID: string
): string | null {
  for (const entry of entries) {
    if (entry.id === entryID) return entry.at;
  }

  return null;
}

/** A one-second wall clock, mounted only while a running tool is at the
 * tail. Starts at 0 so the first frame already carries an honest "0:00". */
function useTickingNow(): number {
  const [now, setNow] = useState<number>(0);

  useEffect((): (() => void) => {
    const id = window.setInterval((): void => {
      setNow(Date.now());
    }, 1000);

    return (): void => {
      window.clearInterval(id);
    };
  }, []);

  return now;
}

/** The status line: the tool's display name plus its live elapsed time.
 * Static under reduced motion — the spinner drops, the text stays. */
function RunningToolLine({
  toolCallId,
  toolName,
}: {
  toolCallId: string;
  toolName: string;
}): ReactElement {
  const catalog = useChatStore((s: ExtendedChatStore) => s.toolCatalog);

  const startedAt = useChatStore((s: ExtendedChatStore) =>
    entryStart(s.entries, toolCallId)
  );

  const reduced = useReducedMotion();
  const now = useTickingNow();
  const { displayName } = toolDisplayInfo(toolName, catalog);

  return (
    <div
      className="flex items-center gap-1.5 py-1 text-xs text-muted"
      data-testid="turn-status-line"
      role="status"
    >
      {!reduced && (
        <span
          aria-hidden="true"
          className="h-3 w-3 animate-spin rounded-full border-2 border-muted/40 border-t-muted motion-reduce:animate-none"
        />
      )}
      <span>{displayName}</span>
      <span className="tabular-nums" data-testid="turn-status-elapsed">
        {formatElapsed(startedAt ?? "", now)}
      </span>
    </div>
  );
}

/** True while the part is a tool call awaiting the reader's decision — the
 * approval card at the tail is the visible activity then; no line. */
function isPendingApproval(part: {
  approval?: { approved?: boolean; resolution?: string } | undefined;
}): boolean {
  return (
    part.approval !== undefined &&
    part.approval.approved === undefined &&
    part.approval.resolution === undefined
  );
}

/** The turn-tail activity line. Renders nothing once nothing is in flight
 * or while text visibly streams at the tail. */
export function TurnActivity(): ReactElement | null {
  const isRunning = useAuiState((s: AssistantState) => s.thread.isRunning);
  const messages = useAuiState((s: AssistantState) => s.thread.messages);

  if (!isRunning) return null;

  const last = messages[messages.length - 1];

  // No assistant output yet — the reply will appear below the prompt.
  if (last === undefined || last.role === "user") {
    return <TypingIndicator variant="bare" />;
  }

  const tail = last.content[last.content.length - 1];

  if (
    tail !== undefined &&
    (tail.type === "text" || tail.type === "reasoning")
  ) {
    // Text visibly streams — no additional indicator beside it.
    return tail.status?.type === "running" ? null : (
      <TypingIndicator variant="bare" />
    );
  }

  if (tail !== undefined && tail.type === "tool-call") {
    if (isPendingApproval(tail)) return null;

    if (tail.result === undefined) {
      return (
        <RunningToolLine
          toolCallId={tail.toolCallId}
          toolName={tail.toolName}
        />
      );
    }
  }

  return <TypingIndicator variant="bare" />;
}
