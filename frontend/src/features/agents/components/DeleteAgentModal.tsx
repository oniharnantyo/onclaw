import { useState } from "react";
import type { ReactElement } from "react";

import { ConfirmDialog } from "../../../components";
import { Input } from "../../../components/ui/Input";
import { client } from "../../../lib/client";
import type { DeleteAgentModalProps } from "../types";

/**
 * Delete confirmation (task 10.4), type-the-name style (design.md): deletion
 * is irreversible, so the confirm button stays disabled until the agent's
 * name is typed exactly. The call fires only on explicit confirm. The red
 * confirm lives in ConfirmDialog alone — the body stays neutral until the
 * dialog itself commits to the destructive tone (tasks.md 4.7).
 */
export function DeleteAgentModal({
  agent,
  onClose,
  onDeleted,
}: DeleteAgentModalProps): ReactElement {
  const [typed, setTyped] = useState("");
  const [deleting, setDeleting] = useState(false);

  const matches = typed.trim() === agent.Name;

  const handleDelete = async (): Promise<void> => {
    if (!matches) return;
    setDeleting(true);

    try {
      await client.agents.delete(agent.ID);
      onDeleted(agent.ID);
      onClose();
    } finally {
      setDeleting(false);
    }
  };

  return (
    <ConfirmDialog
      title="Delete agent"
      testId="delete-agent-modal"
      confirmLabel={deleting ? "Deleting…" : "Delete"}
      confirmTestId="btn-confirm-delete"
      cancelTestId="btn-cancel-delete"
      confirmDisabled={!matches || deleting}
      onConfirm={() => void handleDelete()}
      onClose={onClose}
    >
      <p className="mt-3 text-[13px] text-fg2">
        Delete <span className="font-semibold text-fg">{agent.Name}</span>? Past
        sessions are kept. This cannot be undone.
      </p>
      <div className="mt-4 flex flex-col gap-1.5">
        <label
          className="text-[12px] font-medium text-fg2"
          htmlFor="delete-agent-confirm"
        >
          Type <span className="font-mono text-xs">{agent.Name}</span> to
          confirm
        </label>
        <Input
          id="delete-agent-confirm"
          data-testid="delete-agent-name-input"
          value={typed}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
            setTyped(e.target.value)
          }
          autoComplete="off"
          autoFocus
        />
      </div>
    </ConfirmDialog>
  );
}
