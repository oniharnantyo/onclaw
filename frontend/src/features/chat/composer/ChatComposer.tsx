/**
 * ChatComposer (task 5.1): the assistant-ui composer over the external-store
 * runtime — ComposerPrimitive.Root/Input/Send/Cancel inside the vendored
 * Composer surface, with the legacy centered max-w-3xl bubble and the
 * ring-on-rail layout. Enter sends, Shift+Enter inserts a newline, IME
 * composition never submits, and an open trigger popover owns Enter so its
 * highlighted item is picked instead of sending. While a turn is in flight
 * the send control becomes a stop control (runtime cancel → store.stop) and
 * sends join the store's ordered queue (the adapter deliberately omits
 * queueing; the store drains FIFO at turn terminals).
 *
 * Attachments ride the wave-1 attachment adapter: the paperclip and pasted
 * files mint chips through the runtime, the tray still shows the legacy
 * per-upload state (the adapter snapshots a running attachment, so the
 * vendored chips would spin forever), and send stays blocked while any upload
 * is in flight. Failed uploads are dropped from the runtime draft before a
 * send so one rejected file cannot reject the whole message; the capability
 * warning still names the kinds the effective model cannot read.
 */
import { useEffect } from "react";
import type { KeyboardEvent as ReactKeyboardEvent, ReactElement } from "react";
import { CircleAlert, Loader2, RotateCw, Square, X } from "lucide-react";

import {
  ComposerPrimitive,
  unstable_useTriggerPopoverAriaProps,
  useAui,
  useAuiState,
  type AssistantState,
  type Attachment,
} from "@assistant-ui/react";

import { ComposerAddAttachment } from "@/components/assistant-ui/elements/attachment.aui";
import {
  Composer,
  ComposerActions,
  ComposerAttachments,
  ComposerBar,
  ComposerToolbar,
  ComposerSend,
} from "@/components/assistant-ui/elements/composer";
import { MessageQueue } from "@/components/assistant-ui/elements/message-queue";

import type { ModelCapability } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";

import { cn } from "../../../lib/cn";
import {
  useAttachmentStore,
  type AttachmentChip,
  type AttachmentChipState,
} from "../attachments";
import { formatBytes } from "../components/AttachmentView";
import { useChatStore } from "../store";
import type { ChatStore, QueuedMessage } from "../types";
import { ContextMeter } from "./ComposerContext";
import { ComposerPickers } from "./ComposerPickers";
import { ComposerTriggers } from "./composerTriggers";

/** One trigger-token hint: the token in accent, the word muted. */
function Hint({
  token,
  label,
}: {
  token: string;
  label: string;
}): ReactElement {
  return (
    <span className="font-mono text-[10px] text-muted">
      <span className="text-accenttext">{token}</span> {label}
    </span>
  );
}

/** One chip in the tray: progress (indeterminate), ready, or rejected with
 * its reason and a retry when a retry can succeed (upload failure, not a cap
 * rejection — the same file would reject again). Removal goes through the
 * runtime draft so the adapter drops the chip too. */
function Chip({ chip }: { chip: AttachmentChip }): ReactElement {
  const remove = useAttachmentStore((s: AttachmentChipState) => s.remove);
  const retry = useAttachmentStore((s: AttachmentChipState) => s.retry);
  const aui = useAui();

  const drop = (): void => {
    const draft = aui.composer.getState().attachments;

    const index = draft.findIndex(
      (attachment: Attachment) => attachment.id === chip.id
    );

    if (index >= 0) {
      void aui.composer.attachment({ index }).remove();

      return;
    }

    remove(chip.id);
  };

  return (
    <span
      className={cn(
        "flex max-w-64 items-center gap-1.5 rounded-md border px-2 py-1",
        chip.status === "rejected"
          ? "border-danger/40 bg-danger/5"
          : "border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]"
      )}
      data-testid={`attachment-chip-${chip.id}`}
      data-status={chip.status}
      title={chip.reason ?? chip.name}
    >
      {chip.status === "uploading" ? (
        <Loader2
          aria-hidden="true"
          className="size-3.5 shrink-0 animate-spin text-muted"
        />
      ) : chip.status === "rejected" ? (
        <CircleAlert
          aria-hidden="true"
          className="size-3.5 shrink-0 text-danger"
        />
      ) : (
        <X aria-hidden="true" className="hidden size-3" />
      )}
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[12px] text-fg2">{chip.name}</span>
        <span
          className="block truncate font-mono text-[10px] text-muted"
          data-testid={`chip-state-${chip.id}`}
        >
          {chip.status === "uploading"
            ? "Uploading…"
            : chip.status === "rejected"
              ? (chip.reason ?? "Rejected")
              : formatBytes(chip.size)}
        </span>
      </span>
      {chip.status === "rejected" && chip.retryable && (
        <button
          type="button"
          aria-label={`Retry ${chip.name}`}
          data-testid={`chip-retry-${chip.id}`}
          onClick={() => void retry(chip.id)}
          className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg"
        >
          <RotateCw aria-hidden="true" className="size-3" />
        </button>
      )}
      <button
        type="button"
        aria-label={`Remove ${chip.name}`}
        data-testid={`chip-remove-${chip.id}`}
        onClick={drop}
        className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] hover:text-fg"
      >
        <X aria-hidden="true" className="size-3" />
      </button>
    </span>
  );
}

/** The composer surface: all state lives here, inside TriggerPopoverRoot so
 * the trigger plugin owns Enter while a popover is open. */
function ComposerSurface(): ReactElement {
  const aui = useAui();
  const text = useAuiState((s: AssistantState) => s.composer.text);
  const triggerAria = unstable_useTriggerPopoverAriaProps();

  const agents = useChatStore((s: ChatStore) => s.agents);
  const models = useChatStore((s: ChatStore) => s.models);
  const modelOverride = useChatStore((s: ChatStore) => s.modelOverride);
  const activeAgentID = useChatStore((s: ChatStore) => s.activeAgentID);
  const activeSessionID = useChatStore((s: ChatStore) => s.activeSessionID);
  const busy = useChatStore((s: ChatStore) => s.busy);
  const queue = useChatStore((s: ChatStore) => s.queue);
  const removeQueued = useChatStore((s: ChatStore) => s.removeQueued);
  const newSession = useChatStore((s: ChatStore) => s.newSession);

  const chips = useAttachmentStore((s: AttachmentChipState) => s.chips);

  const clearFinished = useAttachmentStore(
    (s: AttachmentChipState) => s.clearFinished
  );

  const clearChips = useAttachmentStore((s: AttachmentChipState) => s.clear);

  // The tray is the draft of the open session's next message: switching
  // sessions discards it (and the runtime draft behind it).
  useEffect((): void => {
    clearChips();
    void aui.composer.clearAttachments();
  }, [activeSessionID, clearChips]);

  const uploading = chips.some(
    (chip: AttachmentChip): boolean => chip.status === "uploading"
  );

  // SAFETY: the filter keeps only chips with a defined capability key, so
  // the map's cast can never observe undefined.
  const readyKeys = chips
    .filter(
      (chip: AttachmentChip): boolean =>
        chip.status === "ready" && chip.key !== undefined
    )
    .map((chip: AttachmentChip): string => chip.key as string);

  const agent = agents.find((a: Agent) => a.ID === activeAgentID) ?? null;

  const agentName = agent?.Name;

  // Capability warning (spec chat-attachments "Model capability
  // degradation"): the effective model's multimodal flags vs the pending
  // kinds. Unknown models warn nothing — the backend degrades to a
  // reference.
  const effectiveModel = modelOverride || agent?.Model || "";

  const capability = models.find(
    (model: ModelCapability) => model.ID === effectiveModel
  );

  const unsupported: string[] = [];

  if (capability) {
    if (
      !capability.ImageInput &&
      chips.some(
        (chip: AttachmentChip) =>
          chip.status !== "rejected" && chip.mime.startsWith("image/")
      )
    ) {
      unsupported.push("images");
    }

    if (
      !capability.PDFInput &&
      chips.some(
        (chip: AttachmentChip) =>
          chip.status !== "rejected" && chip.mime === "application/pdf"
      )
    ) {
      unsupported.push("PDFs");
    }

    if (
      !capability.VideoInput &&
      chips.some(
        (chip: AttachmentChip) =>
          chip.status !== "rejected" && chip.mime.startsWith("video/")
      )
    ) {
      unsupported.push("videos");
    }
  }

  const canSend =
    activeAgentID !== null &&
    !uploading &&
    (text.trim().length > 0 || readyKeys.length > 0);

  const runningLabel = agentName ? `${agentName} is responding` : "Responding";

  /** Drops runtime-draft attachments the adapter already rejected: their
   * status can never resolve, and the submission would throw on them. */
  const dropRejectedAttachments = (): void => {
    const draft = aui.composer.getState().attachments;

    for (let i = draft.length - 1; i >= 0; i -= 1) {
      if (draft[i].status.type === "incomplete") {
        void aui.composer.attachment({ index: i }).remove();
      }
    }
  };

  const submit = (): void => {
    const trimmed = text.trim();

    // Send is blocked while any upload is in flight (spec chat-attachments);
    // an attachment-only message (no typed text) is sendable.
    if (uploading || !activeAgentID) return;

    if (!trimmed && readyKeys.length === 0) return;

    if (trimmed === "/new" && readyKeys.length === 0) {
      aui.composer.setText("");
      void newSession();

      return;
    }

    dropRejectedAttachments();
    aui.composer.send();
    // The adapter's prepare step is async and reads the chips by id; the
    // finished chips clear once it has had its pass.
    window.setTimeout(clearFinished, 0);
  };

  const onKeyDown = (event: ReactKeyboardEvent<HTMLTextAreaElement>): void => {
    if (event.key !== "Enter" || event.shiftKey) return;

    const native = event.nativeEvent;

    if (native.isComposing || native.keyCode === 229) return;

    // An open trigger popover owns Enter: the primitive's plugin picks the
    // highlighted item, and preventing the default here would skip it.
    if (triggerAria["aria-expanded"] === true) return;

    event.preventDefault();
    submit();
  };

  return (
    <div className="w-full px-4 pb-3">
      {queue.length > 0 && (
        <div
          className="mx-auto w-full max-w-3xl pb-1"
          data-testid="message-queue"
          aria-label="Message queue"
        >
          <MessageQueue
            running={runningLabel}
            queued={queue}
            onCancel={removeQueued}
            className="max-w-none"
          />
          {/* ChatView's queue seams (wave-4 candidate: fold into the vendored
              element once its rows carry stable ids). Hidden from sight, the
              accessibility tree, and the tab order — the vendored rows above
              are the real controls. */}
          <div hidden>
            {busy && <div data-testid="queue-running-row">{runningLabel}</div>}
            {queue.map((queued: QueuedMessage): ReactElement => (
              <button
                key={queued.id}
                type="button"
                data-testid="queue-remove"
                aria-label={`Remove queued message ${queued.text}`}
                onClick={(): void => removeQueued(queued.id)}
              />
            ))}
          </div>
        </div>
      )}
      <Composer className="mx-auto max-w-3xl">
        <ComposerTriggers />
        <ComposerPrimitive.Root>
          <ComposerBar className="gap-1.5">
            {chips.length > 0 && (
              <ComposerAttachments
                data-testid="attachment-tray"
                className="gap-1.5 pb-1.5"
              >
                {chips.map((chip: AttachmentChip): ReactElement => (
                  <Chip key={chip.id} chip={chip} />
                ))}
              </ComposerAttachments>
            )}
            {unsupported.length > 0 && (
              <p
                className="pb-1.5 text-[11px] leading-4 text-warn"
                data-testid="attachment-capability-warning"
                role="status"
              >
                {effectiveModel} can't read {unsupported.join(" or ")} — the
                files ride along as references only.
              </p>
            )}
            <ComposerPrimitive.Input
              className="od-scroll block min-h-10 w-full resize-none bg-transparent px-1.5 py-1.5 text-sm leading-relaxed text-fg outline-none placeholder:text-muted"
              rows={2}
              placeholder={
                agentName ? `Message ${agentName}…` : "Send a message…"
              }
              aria-label="Message"
              data-testid="chat-composer"
              onKeyDown={onKeyDown}
            />
            <ComposerToolbar className="pt-0.5">
              <div className="flex min-w-0 items-center gap-3">
                {/* Session controls on the rail (design D9): agent / model /
                    effort popovers — context and override state, not textarea
                    tokens. */}
                <ComposerPickers />
                {/* Attach: the runtime's AddAttachment joins the chip tray;
                    pasted files land on the same adapter path. */}
                <ComposerAddAttachment />
                {/* Trigger-token discoverability, out of the placeholder — one
                    quiet mono row that never competes with the prompt. */}
                <div
                  className="flex items-center gap-3"
                  data-testid="composer-hints"
                >
                  <Hint token="/" label="commands" />
                  <Hint token="@" label="agents" />
                  <Hint token="$" label="skills" />
                </div>
              </div>
              <ComposerActions className="shrink-0 gap-3">
                {/* Context ring on the bubble's rail, at its right end beside
                    send. */}
                <ContextMeter />
                {busy ? (
                  <ComposerPrimitive.Cancel asChild>
                    <button
                      type="button"
                      aria-label="Stop"
                      data-testid="btn-stop"
                      className="flex size-8 shrink-0 items-center justify-center rounded-full bg-danger text-accenton transition-opacity hover:opacity-90"
                    >
                      <Square
                        aria-hidden="true"
                        className="size-3 fill-current"
                      />
                    </button>
                  </ComposerPrimitive.Cancel>
                ) : (
                  <ComposerSend
                    aria-label="Send"
                    data-testid="btn-send"
                    streaming={false}
                    idle={!canSend}
                    disabled={!canSend}
                    onClick={submit}
                  />
                )}
              </ComposerActions>
            </ComposerToolbar>
          </ComposerBar>
        </ComposerPrimitive.Root>
      </Composer>
    </div>
  );
}

/** The composer entry point: the trigger registry wraps the surface so the
 * trigger plugins and the ARIA combobox state share one root. */
export function ChatComposer(): ReactElement {
  return (
    <ComposerPrimitive.TriggerPopoverRoot>
      <ComposerSurface />
    </ComposerPrimitive.TriggerPopoverRoot>
  );
}
