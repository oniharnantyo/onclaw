import type { ReactElement } from "react";

import { ModalShell } from "./ModalShell";
import { Button } from "./ui/Button";
import type { ConfirmDialogProps } from "./types";

/** Dialog `tone` → kit Button variant. */
const TONE_VARIANT = {
  primary: "primary",
  error: "destructive",
} as const;

/**
 * Destructive-action confirmation: cancel plus a tone-colored confirm,
 * both disabled-aware. Every remove/delete flow in the app routes through
 * here so the action row never drifts between panes.
 */
export function ConfirmDialog({
  title,
  children,
  testId,
  confirmLabel = "Delete",
  cancelLabel = "Cancel",
  tone = "error",
  confirmDisabled,
  confirmTestId,
  cancelTestId,
  onConfirm,
  onClose,
}: ConfirmDialogProps): ReactElement {
  return (
    <ModalShell
      title={title}
      testId={testId}
      onClose={onClose}
      actions={
        <>
          <Button
            variant="ghost"
            size="sm"
            data-testid={cancelTestId}
            onClick={onClose}
          >
            {cancelLabel}
          </Button>
          <Button
            variant={TONE_VARIANT[tone]}
            size="sm"
            data-testid={confirmTestId}
            disabled={confirmDisabled}
            onClick={onConfirm}
          >
            {confirmLabel}
          </Button>
        </>
      }
    >
      {children ?? (
        <p className="text-[13px] text-muted">This cannot be undone.</p>
      )}
    </ModalShell>
  );
}
