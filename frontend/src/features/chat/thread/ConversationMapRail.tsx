/**
 * Conversation map rail (task 3.4, spec chat-view "Conversation minimap"):
 * the vendored conversation-map composition pinned to the thread viewport's
 * right edge — one tick per turn, the tick being read marked, on-screen
 * ticks deepened, a hover preview naming the turn, click scrolls. Mounted
 * inside the viewport subtree (the context lives there) in the composition's
 * own sticky-overlay form, with a display-gate wrapper: hidden below two
 * rendered turns and at narrow widths (`lg:` grammar, today's minimap
 * breakpoint); in Settings the chat view does not mount at all.
 */
import type { ReactElement } from "react";

import { ConversationMapAui } from "@/components/assistant-ui/elements/conversation-map.aui";
import { useAuiState, type AssistantState } from "@assistant-ui/react";

/** Renders nothing below two turns (sparse transcript). */
export function ConversationMapRail(): ReactElement | null {
  const messages = useAuiState((s: AssistantState) => s.thread.messages);

  let turns = 0;

  for (const message of messages) {
    if (message.role === "user") turns += 1;
  }

  if (turns < 2) return null;

  // `contents` at lg keeps the composition's sticky positioning reading the
  // viewport as its containing block; `hidden` takes it out below lg.
  return (
    <div className="hidden lg:contents" data-testid="chat-minimap">
      <ConversationMapAui side="right" />
    </div>
  );
}
