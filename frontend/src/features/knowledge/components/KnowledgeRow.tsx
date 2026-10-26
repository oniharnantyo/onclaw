/**
 * KnowledgeRow (add-knowledge-corpus, "Knowledge management menu"): one
 * document in the Knowledge list — name, description, scope marker (Global
 * or the owning agent's name), human-readable size, and creation date. The
 * row activates the detail pane's preview; it is presentational, every
 * mutation flows up to the view.
 */
import { FileText } from "lucide-react";
import type { ReactElement } from "react";

import type { KnowledgeDocument } from "../../../lib/client";
import { cn } from "../../../lib/cn";
import { formatBytes, formatCreatedDate } from "../helpers";

export interface KnowledgeRowProps {
  doc: KnowledgeDocument;
  /** The owning agent's display name for an agent-scoped document; null
   * falls back to the stored slug (or "Agent" when the agent is gone). */
  agentName: string | null;
  selected: boolean;
  onSelect: () => void;
}

/** The scope marker: Global rides the accent chain, an agent scope the
 * muted one — same bordered-chip idiom as the scheduler rows. */
export function ScopeBadge({
  doc,
  agentName,
}: {
  doc: KnowledgeDocument;
  agentName: string | null;
}): ReactElement {
  const label =
    doc.scope === "global"
      ? "Global"
      : (agentName ?? (doc.agentSlug !== "" ? doc.agentSlug : "Agent"));

  return (
    <span
      data-testid={`knowledge-scope-${doc.id}`}
      className={cn(
        "shrink-0 rounded border px-1.5 py-0.5 text-[11px]",
        doc.scope === "global"
          ? "border-accent text-accenttext"
          : "border-line text-muted"
      )}
    >
      {label}
    </span>
  );
}

export function KnowledgeRow({
  doc,
  agentName,
  selected,
  onSelect,
}: KnowledgeRowProps): ReactElement {
  return (
    <button
      type="button"
      data-testid={`knowledge-row-${doc.id}`}
      aria-pressed={selected}
      onClick={onSelect}
      className={cn(
        "flex w-full items-center gap-3 rounded-md border px-3 py-2.5 text-left transition-colors",
        selected
          ? "border-accent bg-[color-mix(in_oklab,var(--accent)_10%,transparent)]"
          : "border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)]"
      )}
    >
      <FileText aria-hidden="true" className="size-4 shrink-0 text-muted" />

      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-[13px] font-medium text-fg">
            {doc.filename}
          </span>
          <ScopeBadge doc={doc} agentName={agentName} />
        </div>
        <p className="truncate text-[12px] text-muted" title={doc.description}>
          {doc.description !== "" ? doc.description : "No description"}
        </p>
      </div>

      <p
        className="shrink-0 text-[12px] text-fg2"
        data-testid={`knowledge-size-${doc.id}`}
      >
        {formatBytes(doc.size)}
      </p>

      <p
        className="shrink-0 text-[12px] text-muted"
        data-testid={`knowledge-created-${doc.id}`}
      >
        {formatCreatedDate(doc.createdAt)}
      </p>
    </button>
  );
}
