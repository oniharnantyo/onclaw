/**
 * DetailPane (add-knowledge-corpus, "Knowledge management menu"): the
 * selected document's management surface — name, scope, size, creation
 * date, the inline description editor (setDescription), the promote/demote
 * actions (setScope; the demote flow opens the view's agent picker), the
 * delete affordance (the view's confirm dialog), and the content preview.
 * Presentational: mutations flow up to the view.
 */
import { useState } from "react";
import type { ChangeEvent, ReactElement } from "react";
import { Pencil, Trash2 } from "lucide-react";

import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { Button } from "../../../components/ui/Button";
import { IconButton } from "../../../components/IconButton";
import { Input } from "../../../components/ui/Input";
import type { KnowledgeDocument } from "../../../lib/client";
import { formatBytes, formatCreatedDate } from "../helpers";
import { DocumentPreview } from "./DocumentPreview";
import { ScopeBadge } from "./KnowledgeRow";

export interface DetailPaneProps {
  doc: KnowledgeDocument;
  /** The owning agent's display name for an agent-scoped document. */
  agentName: string | null;
  /** The agents the demote picker will offer (the view owns that dialog). */
  agents: Agent[];
  /** Disables the description editor while its call is in flight. */
  descBusy: boolean;
  /** Disables the scope actions while a setScope call is in flight. */
  scopeBusy: boolean;
  /** Commits the new description (view calls setDescription + refetch). */
  onEditDescription: (description: string) => void;
  /** Promotes an agent-scoped document to global. */
  onPromote: () => void;
  /** Opens the demote-to-agent picker for a global document. */
  onDemote: () => void;
  /** Opens the delete confirmation for this document. */
  onDelete: () => void;
}

export function DetailPane({
  doc,
  agentName,
  agents,
  descBusy,
  scopeBusy,
  onEditDescription,
  onPromote,
  onDemote,
  onDelete,
}: DetailPaneProps): ReactElement {
  const [editingDesc, setEditingDesc] = useState(false);

  const [descDraft, setDescDraft] = useState("");

  const startEdit = (): void => {
    setDescDraft(doc.description);
    setEditingDesc(true);
  };

  const saveEdit = (): void => {
    if (descBusy) return;

    setEditingDesc(false);
    onEditDescription(descDraft);
  };

  const isGlobal = doc.scope === "global";

  return (
    <div className="flex flex-col gap-4" data-testid="knowledge-detail-pane">
      <div>
        <h2
          className="break-all text-[14px] font-semibold text-fg"
          data-testid="knowledge-detail-name"
        >
          {doc.filename}
        </h2>
        <div className="mt-1.5 flex flex-wrap items-center gap-2 text-[12px] text-muted">
          <ScopeBadge doc={doc} agentName={agentName} />
          <span data-testid="knowledge-detail-size">
            {formatBytes(doc.size)}
          </span>
          <span data-testid="knowledge-detail-created">
            Added {formatCreatedDate(doc.createdAt)}
          </span>
        </div>
      </div>

      <div>
        <div className="flex items-center gap-1.5">
          <span className="text-[12px] font-medium text-fg2">Description</span>
          {!editingDesc && (
            <IconButton
              label="Edit description"
              size="sm"
              testId={`knowledge-desc-edit-${doc.id}`}
              icon={<Pencil aria-hidden="true" className="size-3.5" />}
              onClick={startEdit}
            />
          )}
        </div>
        {editingDesc ? (
          <div className="mt-1.5 flex items-center gap-2">
            <Input
              data-testid="knowledge-desc-input"
              value={descDraft}
              disabled={descBusy}
              placeholder="Optional description"
              autoFocus
              onChange={(e: ChangeEvent<HTMLInputElement>): void =>
                setDescDraft(e.target.value)
              }
            />
            <Button
              size="sm"
              data-testid="knowledge-desc-save"
              disabled={descBusy}
              onClick={saveEdit}
            >
              Save
            </Button>
            <Button
              variant="ghost"
              size="sm"
              data-testid="knowledge-desc-cancel"
              disabled={descBusy}
              onClick={() => setEditingDesc(false)}
            >
              Cancel
            </Button>
          </div>
        ) : (
          <p
            className="mt-1 text-[13px] text-muted"
            data-testid="knowledge-desc-value"
          >
            {doc.description !== "" ? doc.description : "None"}
          </p>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {!isGlobal && (
          <Button
            variant="ghost"
            size="sm"
            data-testid="btn-promote-doc"
            disabled={scopeBusy}
            onClick={onPromote}
          >
            Promote to global
          </Button>
        )}
        {isGlobal && (
          <Button
            variant="ghost"
            size="sm"
            data-testid="btn-demote-doc"
            disabled={scopeBusy || agents.length === 0}
            title={agents.length === 0 ? "No agents to demote to" : undefined}
            onClick={onDemote}
          >
            Demote to agent…
          </Button>
        )}
        <Button
          variant="ghost"
          size="sm"
          data-testid="btn-delete-doc"
          disabled={scopeBusy}
          onClick={onDelete}
        >
          <Trash2 aria-hidden="true" className="size-4" />
          Delete
        </Button>
      </div>

      <DocumentPreview key={doc.id} doc={doc} />
    </div>
  );
}
