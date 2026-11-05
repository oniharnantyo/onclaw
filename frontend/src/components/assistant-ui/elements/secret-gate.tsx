"use client";

/**
 * SecretGate (add-browser-login-handover tasks 4.1, design D6): the masked
 * input card a parked secret request renders — a password field (method
 * "password") or digit boxes (method "otp"/"authenticator", one per
 * `digits`), an identifier box (username or email — shown only when the
 * request declared an identifierTarget), plus a save-to-vault checkbox (only
 * when the backend passed a username — SaveVaultLogin needs one). Submit
 * clears the local values REGARDLESS of the call's outcome (setValue("")
 * before the await — OpenBot's clear-on-submit pattern; a retry re-types)
 * and calls
 * `client.chat.resolveInput(sessionID, interruptID, assembledText,
 * identifier, saveLogin)` directly — NOT through the assistant-ui
 * approval-response machinery. The Cancel button resolves the same pending
 * request through `client.chat.cancelInput(sessionID, interruptID)`: the
 * parked submit_secret call unblocks on the cancelled result, the child's
 * freeze is released, and the receipt renders "Cancelled". THE
 * GATE (spec chat-approvals: "The runtime SHALL accept a text response only
 * when a request declares it"): this card only ever renders because the
 * mapping set `approval.display === "text"` on an input part — display "text"
 * exists nowhere else — so the host mapping owns the declaration and the
 * direct call owns the delivery; the value never touches a message part.
 *
 * A resolved entry renders the receipt only: "Filled ✓" (with "(saved as
 * login `<id>`)" when the vault stored it), "Cancelled", "Expired — the
 * agent can re-request" — never the value (there is no value field to
 * render, design D1).
 */
import { useState, type ReactElement } from "react";
import { KeyRoundIcon, Loader2Icon } from "lucide-react";

import type { InputApproval, InputReceipt } from "@/features/chat/runtime/mapping";
import { useChatStore, type ExtendedChatStore } from "@/features/chat/store";
import { client } from "@/lib/client";
import { cn } from "@/lib/utils";
import { field, ghostButton, inkButton, paper } from "./surfaces";

export type SecretGateState = "idle" | "submitting" | "resolved";

/** The receipt wording for one resolution: completed names the save only
 * through the vault login id; cancelled/expired/failed carry their own fixed
 * wording. Returns null while the request is pending. */
export function secretReceiptLine(receipt: InputReceipt): string | null {
  if (receipt.outcome === "completed") {
    return receipt.savedLoginId !== undefined
      ? `Filled ✓ (saved as login \`${receipt.savedLoginId}\`)`
      : "Filled ✓";
  }

  if (receipt.outcome === "cancelled") return "Cancelled";

  if (receipt.outcome === "expired") {
    return "Expired — the agent can re-request";
  }

  return "Submission failed";
}

export function SecretGate({ approval }: { approval: InputApproval }): ReactElement {
  const sessionID = useChatStore(
    (s: ExtendedChatStore) => s.activeSessionID
  );

  const digitCount =
    approval.method === "password"
      ? 0
      : Math.min(Math.max(approval.digits ?? 6, 1), 12);

  const [password, setPassword] = useState("");
  const [identifier, setIdentifier] = useState("");
  const [digits, setDigits] = useState<string[]>(() =>
    Array.from({ length: digitCount }, (): string => "")
  );
  const [save, setSave] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const receipt = approval.receipt;
  const resolved = receipt !== undefined;
  // Save-to-vault needs a username (SaveVaultLogin upserts on
  // origin+username), so the offer exists only when the backend passed one.
  const canSave =
    approval.method === "password" &&
    approval.username !== undefined &&
    approval.username !== "";

  const submit = (): void => {
    if (submitting || resolved || sessionID === null) return;

    const text =
      approval.method === "password" ? password : digits.join("");

    setSubmitting(true);
    setError(null);
    // Clear on submit regardless of outcome (design D1): the local value's
    // lifetime is one submission; a retry re-types.
    setPassword("");
    setIdentifier("");
    setDigits(Array.from({ length: digitCount }, (): string => ""));

    void client.chat
      .resolveInput(sessionID, approval.id, text, identifier, canSave && save)
      .catch((err: Error): void => {
        // The backend answers fixed reason texts — shown verbatim; the
        // card re-arms for a fresh (re-typed) submission.
        setError(err.message);
      })
      .finally((): void => setSubmitting(false));
  };

  const cancel = (): void => {
    if (submitting || resolved || sessionID === null) return;

    setSubmitting(true);
    setError(null);
    // The typed values die with the request: nothing is delivered.
    setPassword("");
    setIdentifier("");
    setDigits(Array.from({ length: digitCount }, (): string => ""));

    void client.chat
      .cancelInput(sessionID, approval.id)
      .catch((err: Error): void => {
        setError(err.message);
      })
      .finally((): void => setSubmitting(false));
  };

  const setDigit = (index: number, raw: string): void => {
    const next = digits.slice();

    next[index] = raw.replace(/\D/g, "").slice(-1);
    setDigits(next);
  };

  return (
    <div
      data-testid="secret-gate"
      data-secret-state={resolved ? "resolved" : submitting ? "submitting" : "idle"}
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
          <KeyRoundIcon className="size-4" />
        </span>
        <div className="flex flex-col">
          <p className="text-[13.5px] font-medium">Secret needed</p>
          <p className="text-muted-foreground text-xs">
            {approval.prompt !== "" ? approval.prompt : "The agent paused to ask for credentials"}
          </p>
        </div>
      </div>

      {resolved ? (
        <p
          role="status"
          data-testid="secret-gate-receipt"
          className="text-muted-foreground text-xs"
        >
          {secretReceiptLine(receipt)}
        </p>
      ) : (
        <>
          {approval.identifier === true ? (
            <input
              type="text"
              autoComplete="off"
              data-testid="secret-gate-identifier"
              value={identifier}
              disabled={submitting}
              onChange={(e): void => setIdentifier(e.target.value)}
              className={cn(field, "text-foreground/80 w-full px-3.5 py-2.5 text-xs")}
              placeholder="Username or email"
            />
          ) : null}

          {approval.method === "password" ? (
            <input
              type="password"
              autoComplete="off"
              data-testid="secret-gate-password"
              value={password}
              disabled={submitting}
              onChange={(e): void => setPassword(e.target.value)}
              className={cn(field, "text-foreground/80 w-full px-3.5 py-2.5 text-xs")}
              placeholder="Password"
            />
          ) : (
            <div
              className="flex flex-wrap gap-1.5"
              data-testid="secret-gate-otp"
            >
              {digits.map((digit, i): ReactElement => (
                <input
                  key={i}
                  type="password"
                  inputMode="numeric"
                  autoComplete="off"
                  maxLength={1}
                  data-testid={`secret-gate-otp-${i}`}
                  aria-label={`Digit ${i + 1}`}
                  value={digit}
                  disabled={submitting}
                  onChange={(e): void => setDigit(i, e.target.value)}
                  className={cn(
                    field,
                    "text-foreground/80 h-9 w-9 text-center text-sm"
                  )}
                />
              ))}
            </div>
          )}

          {canSave ? (
            <label className="text-muted-foreground flex items-center gap-2 text-xs">
              <input
                type="checkbox"
                data-testid="save-vault-checkbox"
                checked={save}
                disabled={submitting}
                onChange={(e): void => setSave(e.target.checked)}
              />
              Save in vault ({approval.username})
            </label>
          ) : null}

          {error !== null ? (
            <p
              className="text-dangertext px-1 text-xs"
              role="alert"
              data-testid="secret-gate-error"
            >
              {error}
            </p>
          ) : null}

          <div className="flex min-h-8 flex-wrap items-center justify-end gap-2">
            <button
              type="button"
              data-testid="secret-gate-cancel"
              disabled={submitting}
              onClick={cancel}
              className={cn(
                ghostButton,
                "h-8 px-3.5 text-xs font-medium whitespace-nowrap"
              )}
            >
              Cancel
            </button>
            <button
              type="button"
              data-testid="secret-gate-submit"
              disabled={submitting}
              onClick={submit}
              className={cn(
                inkButton,
                "flex h-8 items-center rounded-full px-3.5 text-xs font-medium whitespace-nowrap"
              )}
            >
              {submitting ? (
                <Loader2Icon className="size-3.5 animate-spin motion-reduce:animate-none" />
              ) : null}
              Submit
            </button>
          </div>
        </>
      )}
    </div>
  );
}
