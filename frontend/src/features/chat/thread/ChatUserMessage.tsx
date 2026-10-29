/**
 * ChatUserMessage (tasks 3.2, 5.5): one user prompt — the right-aligned flat
 * tinted surface the transcript always carried, with the persisted
 * attachments above the text. The day gate and hover-revealed time share the
 * assistant message's grammar.
 *
 * Attachments render through the retained AttachmentView (a lazy
 * capability-gated read, blob object URL revoked on unmount) rather than the
 * vendored UserMessageAttachments: the runtime mapping is a pure synchronous
 * derivation of the store, so a replayed attachment carries metadata only,
 * and the vendored element resolves an image source solely from an
 * attachment's `file`/`content` — neither exists on a replayed message, so it
 * degrades to a generic file-icon tile with no name, size, or unavailable
 * state. The vendored element keeps its lane in the composer, where the
 * attachment adapter supplies the `file` (design D13: vendored sources are
 * never edited). The message id is the user entry's id (the D2 mapping), so
 * the store entry stays the source of truth for the chip metadata.
 *
 * A persisted telegram origin (add-telegram-channel 9.2) badges the row with
 * a small paper-plane chip — channel prompts are visually distinguishable in
 * the transcript; user-origin and legacy rows (empty origin) render bare.
 */
import type { ReactElement } from "react";

import {
  MessagePrimitive,
  type AssistantState,
  type TextMessagePartComponent,
  type TextMessagePartProps,
  useAuiState,
} from "@assistant-ui/react";
import { Send } from "lucide-react";

import type { Attachment as SessionAttachment } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { AttachmentView } from "../components/AttachmentView";
import { asRichUser, type Entry, type UserEntry } from "../entries";
import { useChatStore, type ExtendedChatStore } from "../store";
import { DayGate, MessageTime } from "./DayDivider";

/** The prompt's text: plain pre-wrapped text (never markdown — the composer
 * sends what the reader typed). */
const UserText: TextMessagePartComponent = ({
  text,
}: TextMessagePartProps): ReactElement => (
  <span className="whitespace-pre-wrap">{text}</span>
);

/** The text components at a stable identity (the parts renderer's prop gate). */
const TEXT_COMPONENTS = { Text: UserText };

/** The empty attachment list: a stable reference for payload-less messages. */
const NO_ATTACHMENTS: SessionAttachment[] = [];

/** The user entry behind this message: the mapping mints one user message per
 * user entry and keeps the entry's id, so the entry (with its persisted
 * attachment metadata) resolves from the store without a second source. */
function usePromptEntry(): UserEntry | undefined {
  const messageID = useAuiState((s: AssistantState) => s.message.id);

  return useChatStore((s: ExtendedChatStore) => {
    const entry = s.entries.find(
      (e: Entry): boolean => e.kind === "user" && e.id === messageID
    );

    return entry !== undefined && entry.kind === "user" ? entry : undefined;
  });
}

/** One user message. */
export function ChatUserMessage(): ReactElement {
  const entry = usePromptEntry();

  const attachments =
    entry === undefined
      ? NO_ATTACHMENTS
      : (asRichUser(entry).attachments ?? NO_ATTACHMENTS);

  // The Telegram-origin badge (add-telegram-channel 9.2): only a persisted
  // telegram origin badges the row — user turns and legacy rows persisted
  // before origins were recorded (empty origin) render bare.
  const isTelegram =
    entry !== undefined && asRichUser(entry).origin === "telegram";

  // An attachment-only prompt renders its chips without an empty bubble.
  const hasText = entry === undefined || entry.text.trim() !== "";

  return (
    <div className="group/msg relative flex justify-end">
      <DayGate />
      <MessagePrimitive.Root
        data-role="user"
        data-testid="entry-user"
        className="flex max-w-[85%] flex-col items-end gap-1.5"
      >
        {isTelegram && (
          <div
            data-testid="origin-telegram"
            title="Sent from Telegram"
            className="border-line text-foreground/70 flex items-center gap-1 rounded-[6px] border px-1.5 py-0.5 text-[10px] leading-4"
          >
            <Send size={10} aria-hidden="true" />
            Telegram
          </div>
        )}
        {attachments.length > 0 && (
          <div
            className="flex flex-wrap justify-end gap-1.5"
            data-testid="user-attachments"
          >
            {attachments.map((attachment: SessionAttachment) => (
              <AttachmentView key={attachment.key} attachment={attachment} />
            ))}
          </div>
        )}
        {hasText && (
          <div
            data-testid="user-text"
            className="rounded-xl bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] px-4 py-2 text-sm leading-relaxed whitespace-pre-wrap break-words text-fg"
          >
            <MessagePrimitive.Parts components={TEXT_COMPONENTS} />
          </div>
        )}
      </MessagePrimitive.Root>
      <MessageTime side="start" />
    </div>
  );
}
