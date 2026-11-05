"use client";

/**
 * TakeoverCard (add-browser-login-handover task 4.2, design D8): the browser
 * takeover card a parked `request_takeover` renders. States:
 * pending (Approve/Cancel) → Approve calls `client.chat.beginBrowserTakeover`
 * and flips a LIVE-ONLY local state to active (Done/Cancel); Done calls
 * `client.chat.resolveTakeover(outcome "completed")`, Cancel — from either
 * state — outcome "cancelled". A resolved entry (input_resolved receipt on
 * the part) renders the receipt line instead of any controls: "Handed over —
 * take a fresh snapshot" / "Cancelled" / "Expired — the agent can
 * re-request".
 *
 * Like SecretGate, the card renders only because the mapping set
 * `approval.display === "decision"` with `attention: "takeover"` on an input
 * part — the shell-gate approval decision (no attention marker) keeps
 * rendering the vendored ApprovalGate.
 */
import { useState, type ReactElement } from "react";
import { HandIcon, Loader2Icon } from "lucide-react";

import type { InputApproval } from "@/features/chat/runtime/mapping";
import { useChatStore, type ExtendedChatStore } from "@/features/chat/store";
import { client } from "@/lib/client";
import { cn } from "@/lib/utils";
import { ghostButton, inkButton, paper } from "./surfaces";

export type TakeoverState = "pending" | "active" | "resolved";
export type TakeoverOutcome = "completed" | "cancelled" | "expired" | "failed";

/** The receipt wording for one resolution (design D8). */
export function takeoverReceiptLine(outcome: TakeoverOutcome): string {
  if (outcome === "completed") return "Handed over — take a fresh snapshot";

  if (outcome === "cancelled") return "Cancelled";

  if (outcome === "expired") return "Expired — the agent can re-request";

  return "Takeover failed";
}

export function TakeoverCard({ approval }: { approval: InputApproval }): ReactElement {
  const sessionID = useChatStore(
    (s: ExtendedChatStore) => s.activeSessionID
  );

  const receipt = approval.receipt;
  const [active, setActive] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const resolved = receipt !== undefined;
  const state: TakeoverState = resolved ? "resolved" : active ? "active" : "pending";

  const guard = (): string | null => {
    if (submitting || resolved) return null;

    return sessionID;
  };

  const begin = (): void => {
    const sid = guard();

    if (sid === null) return;

    setSubmitting(true);
    setError(null);

    void client.chat
      .beginBrowserTakeover(sid, approval.id)
      .then((): void => setActive(true))
      .catch((err: Error): void => setError(err.message))
      .finally((): void => setSubmitting(false));
  };

  const resolve = (outcome: TakeoverOutcome): void => {
    const sid = guard();

    if (sid === null) return;

    setSubmitting(true);
    setError(null);

    void client.chat
      .resolveTakeover(sid, approval.id, outcome)
      .catch((err: Error): void => setError(err.message))
      .finally((): void => setSubmitting(false));
  };

  return (
    <div
      data-testid="takeover-card"
      data-takeover-state={state}
      role="group"
      className={cn(
        paper,
        "flex w-full max-w-sm flex-col gap-3.5 rounded-[20px] p-4"
      )}
    >
      <div className="flex items-center gap-3">
        <span
          aria-hidden
          className="bg-foreground/[0.05] text-muted-foreground flex size-9 shrink-0 items-center justify-center rounded-xl"
        >
          <HandIcon className="size-4" />
        </span>
        <div className="flex flex-col">
          <p className="text-[13.5px] font-medium">Browser takeover</p>
          <p className="text-muted-foreground text-xs">
            {approval.prompt !== ""
              ? approval.prompt
              : "The agent paused to hand you the browser"}
          </p>
        </div>
      </div>

      {resolved ? (
        <p
          role="status"
          data-testid="takeover-receipt"
          className="text-muted-foreground text-xs"
        >
          {takeoverReceiptLine(
            (receipt.outcome as TakeoverOutcome) ?? "failed"
          )}
        </p>
      ) : (
        <>
          {error !== null ? (
            <p
              className="text-dangertext px-1 text-xs"
              role="alert"
              data-testid="takeover-error"
            >
              {error}
            </p>
          ) : null}

          <div className="flex min-h-8 flex-wrap items-center justify-end gap-2">
            {state === "pending" ? (
              <button
                type="button"
                data-testid="takeover-approve"
                disabled={submitting}
                onClick={begin}
                className={cn(
                  inkButton,
                  "flex h-8 items-center rounded-full px-3.5 text-xs font-medium whitespace-nowrap"
                )}
              >
                {submitting ? (
                  <Loader2Icon className="size-3.5 animate-spin motion-reduce:animate-none" />
                ) : null}
                Approve
              </button>
            ) : (
              <button
                type="button"
                data-testid="takeover-done"
                disabled={submitting}
                onClick={(): void => resolve("completed")}
                className={cn(
                  inkButton,
                  "flex h-8 items-center rounded-full px-3.5 text-xs font-medium whitespace-nowrap"
                )}
              >
                {submitting ? (
                  <Loader2Icon className="size-3.5 animate-spin motion-reduce:animate-none" />
                ) : null}
                Done
              </button>
            )}
            <button
              type="button"
              data-testid="takeover-cancel"
              disabled={submitting}
              onClick={(): void => resolve("cancelled")}
              className={cn(
                ghostButton,
                "h-8 px-3.5 text-xs font-medium whitespace-nowrap"
              )}
            >
              Cancel
            </button>
          </div>
        </>
      )}
    </div>
  );
}
