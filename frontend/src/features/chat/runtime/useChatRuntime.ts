/**
 * The assistant-ui runtime adapter (task 2.3, design D1): one
 * `useExternalStoreRuntime` wired only to the chat store's selectors and
 * actions. The adapter holds no state of its own — messages/branches are the
 * pure mapping of the store's entries (designs D2/D3), the live flags are
 * the store's turn lifecycle, and every callback routes into an existing
 * store action (or, for approvals, the bound decision call the legacy
 * ApprovalCard also uses — the store exposes no approve/deny actions).
 */
import { useMemo } from "react";
import { useExternalStoreRuntime } from "@assistant-ui/react";
import type {
  AppendMessage,
  AssistantRuntime,
  AttachmentAdapter,
  CompleteAttachment,
  ExternalStoreAdapter,
  ExportedMessageRepository,
  PendingAttachment,
  RespondToToolApprovalOptions,
  ThreadMessage,
} from "@assistant-ui/react";

import { client } from "../../../lib/client";
import { useAttachmentStore, type AttachmentChip } from "../attachments";
import { asRichUser } from "../entries";
import { useChatStore, type ExtendedChatStore } from "../store";
import { entriesToRepository } from "./branches";

/** The mime lanes the backend accepts (attachments.ts mirrors them). */
const ATTACHMENT_ACCEPT = "image/*,application/pdf,video/*,text/*";

/** The attachment lane a mime sorts into — the same lanes the transcript's
 * message attachments show. */
function attachmentLane(mime: string): "image" | "document" | "file" {
  if (mime === "application/pdf") return "document";

  if (mime.startsWith("image/")) return "image";

  return "file";
}

function chipAttachment(chip: AttachmentChip, file: File): PendingAttachment {
  if (chip.status === "rejected") {
    return {
      id: chip.id,
      type: attachmentLane(chip.mime),
      name: chip.name,
      contentType: chip.mime,
      file,
      status: { type: "incomplete", reason: "error", message: chip.reason },
    };
  }

  return {
    id: chip.id,
    type: attachmentLane(chip.mime),
    name: chip.name,
    contentType: chip.mime,
    file,
    status: { type: "running", reason: "uploading", progress: -1 },
  };
}

/** The composer's attachment adapter over the attachment chip store: add
 * mints a chip (and its upload), remove drops it, send resolves the stored
 * capability key — which rides the complete attachment's id into `onNew`. */
export const attachmentAdapter: AttachmentAdapter = {
  accept: ATTACHMENT_ACCEPT,

  add: async (state: { file: File }): Promise<PendingAttachment> => {
    const store = useAttachmentStore.getState();

    const before = new Set<string>(
      store.chips.map((c: AttachmentChip): string => c.id)
    );

    await store.addFiles([state.file]);

    const chip = useAttachmentStore
      .getState()
      .chips.find((c: AttachmentChip): boolean => !before.has(c.id));

    if (chip === undefined) throw new Error("Could not attach the file");

    return chipAttachment(chip, state.file);
  },

  remove: async (attachment: { id: string }): Promise<void> => {
    useAttachmentStore.getState().remove(attachment.id);
  },

  send: async (pending: PendingAttachment): Promise<CompleteAttachment> => {
    const chip = useAttachmentStore
      .getState()
      .chips.find((c: AttachmentChip): boolean => c.id === pending.id);

    if (chip === undefined) throw new Error("The attachment was removed");

    if (chip.status === "uploading") {
      throw new Error("The upload is still running");
    }

    if (chip.key === undefined) {
      throw new Error(chip.reason ?? "The upload failed");
    }

    // The stored capability key rides as the id: it is the only handle the
    // send path needs, and onNew reads complete-attachment ids as keys.
    return {
      id: chip.key,
      type: attachmentLane(chip.mime),
      name: chip.name,
      contentType: chip.mime,
      content: [],
      status: { type: "complete" },
    };
  },
};

/** The send text of one composer append: the text parts, concatenated. */
function appendText(message: AppendMessage): string {
  let text = "";

  for (const part of message.content) {
    if (part.type === "text") text += part.text;
  }

  return text;
}

/** The attachment keys of one composer append: the attachment adapter sets
 * the stored capability key as the complete attachment's id, so the keys
 * pass straight through to the store's send action. */
function appendAttachmentKeys(message: AppendMessage): string[] {
  const keys: string[] = [];

  for (const attachment of message.attachments ?? []) {
    keys.push(attachment.id);
  }

  return keys;
}

/** The turn a reload targets: the id the runtime reports is the user
 * message's entry id, whose entry carries the turn stamp. */
function turnOfUserMessage(
  messageID: string | null,
  entries: ExtendedChatStore["entries"]
): string | null {
  if (messageID === null) return null;

  for (const entry of entries) {
    if (entry.kind !== "user" || entry.id !== messageID) continue;

    return entry.turnID ?? null;
  }

  return null;
}

/** Branch switch (design D3): the runtime reports the switched-to head via
 * setMessages; the head message id is the variant turn's id, which maps back
 * to the anchor's slot index for the store's local switch. A head that is
 * already the active variant (e.g. the runtime re-applying a list) is a
 * no-op, so a resync cannot churn the store. */
function switchToHeadVariant(messages: readonly ThreadMessage[]): void {
  const head = messages[messages.length - 1];

  if (head === undefined) return;

  for (const entry of useChatStore.getState().entries) {
    if (entry.kind !== "user") continue;

    const anchor = asRichUser(entry);
    const slot = anchor.variantTurns;

    if (slot === undefined || !slot.includes(head.id)) continue;

    const index = slot.indexOf(head.id);
    const active = anchor.activeVariant ?? slot.length - 1;

    if (index !== active && entry.turnID !== undefined) {
      useChatStore.getState().setActiveVariant(entry.turnID, index);
    }

    return;
  }
}

/** The external-store adapter over the chat store: the reactive slices are
 * selected (and memoise the adapter), the callbacks read the live store at
 * call time so no closure can go stale. */
export function useChatRuntime(): AssistantRuntime {
  const entries = useChatStore((s: ExtendedChatStore) => s.entries);
  const busy = useChatStore((s: ExtendedChatStore) => s.busy);
  const oldestSeq = useChatStore((s: ExtendedChatStore) => s.oldestSeq);

  const repository = useMemo(
    (): ExportedMessageRepository => entriesToRepository(entries),
    [entries]
  );

  const adapter = useMemo(
    (): ExternalStoreAdapter => ({
      messageRepository: repository,
      isRunning: busy,
      hasEarlier: oldestSeq !== null,

      onLoadEarlier: async (): Promise<void> => {
        await useChatStore.getState().loadEarlier();
      },

      onNew: async (message: AppendMessage): Promise<void> => {
        await useChatStore
          .getState()
          .send(appendText(message), appendAttachmentKeys(message));
      },

      onCancel: async (): Promise<void> => {
        useChatStore.getState().stop();
      },

      onReload: async (parentId: string | null): Promise<void> => {
        const turnID = turnOfUserMessage(
          parentId,
          useChatStore.getState().entries
        );

        if (turnID !== null) await useChatStore.getState().regenerate(turnID);
      },

      setMessages: (messages: readonly ThreadMessage[]): void => {
        switchToHeadVariant(messages);
      },

      // Gap (waves 2–3 own the store surface): the store exposes no
      // approve/deny actions — the durable decision rides the bound
      // resolveApproval call, exactly like the legacy ApprovalCard. The
      // approval id is the interrupt id the D2 mapping put on the part.
      onRespondToToolApproval: (
        options: RespondToToolApprovalOptions
      ): void => {
        const sessionID = useChatStore.getState().activeSessionID;

        if (sessionID === null) return;

        void client.chat.resolveApproval(
          sessionID,
          options.approvalId,
          options.approved
        );
      },

      adapters: { attachments: attachmentAdapter },
    }),
    [repository, busy, oldestSeq]
  );

  return useExternalStoreRuntime(adapter);
}
