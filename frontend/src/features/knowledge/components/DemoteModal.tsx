/**
 * DemoteModal (add-knowledge-corpus, "Document scoping and promotion"): the
 * global → agent demotion's agent picker. The store rejects a demotion that
 * would collide with an existing filename in the target scope; the backend
 * reason surfaces as a toast from the view. Confirm stays disabled until an
 * agent is picked.
 */
import { useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { ConfirmDialog } from "../../../components/ConfirmDialog";
import { Select } from "../../../components/ui/Select";
import type { KnowledgeDocument } from "../../../lib/client";

export interface DemoteModalProps {
  doc: KnowledgeDocument;
  agents: Agent[];
  /** Disables the confirm while the setScope call is in flight. */
  busy: boolean;
  /** Commits the demotion to the picked agent. */
  onConfirm: (agentID: string) => void;
  onClose: () => void;
}

export function DemoteModal({
  doc,
  agents,
  busy,
  onConfirm,
  onClose,
}: DemoteModalProps): ReactElement {
  // "" = no pick yet; the confirm button stays disabled.
  const [agentID, setAgentID] = useState("");

  return (
    <ConfirmDialog
      title="Demote to agent"
      testId="knowledge-demote-modal"
      confirmLabel="Move"
      tone="primary"
      confirmTestId="btn-confirm-demote"
      cancelTestId="btn-cancel-demote"
      confirmDisabled={agentID === "" || busy}
      onConfirm={() => onConfirm(agentID)}
      onClose={onClose}
    >
      <p className="text-[13px] text-muted">
        Move <span className="font-semibold text-fg">{doc.filename}</span> into
        one agent's scope? Only that agent's turns will read it.
      </p>
      <div className="mt-4 flex flex-col gap-1.5">
        <label
          htmlFor="knowledge-demote-agent"
          className="text-[12px] font-medium text-fg2"
        >
          Agent
        </label>
        <Select
          id="knowledge-demote-agent"
          data-testid="knowledge-demote-agent"
          value={agentID}
          disabled={busy}
          onChange={(e: ChangeEvent<HTMLSelectElement>): void =>
            setAgentID(e.target.value)
          }
        >
          <option value="">Choose an agent…</option>
          {agents.map((a: Agent) => (
            <option key={a.ID} value={a.ID}>
              {a.Name}
            </option>
          ))}
        </Select>
      </div>
    </ConfirmDialog>
  );
}
