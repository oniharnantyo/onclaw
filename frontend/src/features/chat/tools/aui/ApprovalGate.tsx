/**
 * ApprovalGate (task 4.4, design D4 verdict in VENDORED.md (d)): the
 * dangerous-command gate rendered through the vendored `ApprovalCard`. A
 * pending approval is a tool-call part carrying `approval` with
 * `approved === undefined`; the decision rides the part's
 * `respondToApproval` — the external-store runtime resolves it against the
 * adapter's own messages and reaches wave 1's `onRespondToToolApproval` →
 * the bound `resolveApproval` call. A decision the runtime could not record
 * (a stale interrupt id) rejects the promise, surfaces as a visible error,
 * and keeps the card pending so the decision can be re-submitted. The
 * pending state is plain data on the part, so a replayed transcript rebuilds
 * it — nothing is stored locally.
 */
import { useState } from "react";
import type { ReactElement } from "react";
import { ShieldAlertIcon } from "lucide-react";
import type { ToolCallMessagePartProps } from "@assistant-ui/react";

import { ApprovalCard } from "@/components/assistant-ui/elements/approval-card";
import type { ApprovalState } from "@/components/assistant-ui/elements/approval-card";

import { parseArgs, str } from "../parse";
import { prettyOrVerbatim } from "./fallbackBody";

/** The narrow type guard the gate reads results through. */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

/** The receipt the gate renders: the ApprovalCard state plus its status
 * wording when it is not the element's default. */
interface GateReceipt {
  state: ApprovalState;
  statusLabel?: string;
}

/** The receipt state and label one approval part renders: pending → the
 * actionable request; approved without a result → "Approved, running";
 * approved with the resumed turn's result → "Approved"; denied → "Denied";
 * a cancelled (or expired) resolution → its own wording. */
function gateState(part: ToolCallMessagePartProps): GateReceipt {
  const approval = part.approval;

  if (approval === undefined) return { state: "request" };

  if (approval.resolution !== undefined) {
    return {
      state: "done",
      statusLabel:
        approval.resolution === "cancelled"
          ? "Cancelled before a decision"
          : "Expired before a decision",
    };
  }

  if (approval.approved === true) {
    const result = isTextResult(part.result) ? part.result : "";

    return result === ""
      ? { state: "running" }
      : { state: "done", statusLabel: "Approved" };
  }

  if (approval.approved === false) return { state: "denied" };

  return { state: "request" };
}

export function ApprovalGate({
  part,
}: {
  part: ToolCallMessagePartProps;
}): ReactElement {
  const approval = part.approval;
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (approval === undefined) return <></>;

  const args = parseArgs(part.argsText);
  const command = (args === null ? "" : str(args, "command")) || part.argsText;
  const resultText = isTextResult(part.result) ? part.result : "";
  const { state, statusLabel } = gateState(part);
  const pending = state === "request";

  const respond = (approved: boolean): void => {
    if (submitting) return;

    setSubmitting(true);
    setError(null);

    part
      .respondToApproval({ approved })
      .then((): void => {
        // The approval_resolved event drives the resolved state; the call
        // itself carries nothing to apply.
      })
      .catch((err: Error): void => {
        setError(err.message);
      })
      .finally((): void => setSubmitting(false));
  };

  return (
    <div
      data-testid="entry-approval"
      data-resolved={pending ? "false" : "true"}
      className="w-full"
    >
      <ApprovalCard
        state={state}
        title={pending ? "Approval required" : "Command approval"}
        subtitle={
          pending
            ? "The agent paused before running this command"
            : "The decision has been recorded"
        }
        command={command}
        variant="destructive"
        icon={<ShieldAlertIcon className="size-4" />}
        {...(pending
          ? {
              onAllowOnce: (): void => respond(true),
              onDeny: (): void => respond(false),
            }
          : {})}
        {...(statusLabel !== undefined ? { statusLabel } : {})}
        {...(resultText !== ""
          ? {
              details: [
                { label: "Result", value: prettyOrVerbatim(resultText) },
              ],
            }
          : {})}
      />
      {error !== null ? (
        <p
          className="text-dangertext mt-1 px-1 text-xs"
          role="alert"
          data-testid="approval-error"
        >
          {error}
        </p>
      ) : null}
    </div>
  );
}
