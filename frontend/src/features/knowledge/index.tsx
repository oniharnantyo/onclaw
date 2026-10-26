/**
 * Knowledge menu (add-knowledge-corpus, "Knowledge management menu"): the
 * corpus's management surface — the document list with scope markers, size,
 * and creation date; the scope filter (all / global / per agent); upload
 * with an explicit scope choice; the selected document's detail pane
 * (preview, description editing, promote/demote, delete with confirmation).
 * The list fetches on mount (views unmount on menu switch) and refetches
 * coalesced on `knowledge:changed` — the one event every mutation, including
 * mid-turn agent saves, emits — so the menu reflects the corpus live.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";
import { Plus } from "lucide-react";

import type { Agent } from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { ConfirmDialog, EmptyState, PaneHeader } from "../../components";
import { Button } from "../../components/ui/Button";
import { Select } from "../../components/ui/Select";
import { Skeleton } from "../../components/ui/Skeleton";
import { client, type KnowledgeDocument } from "../../lib/client";
import { toast } from "../../lib/toasts";
import { DemoteModal } from "./components/DemoteModal";
import { DetailPane } from "./components/DetailPane";
import { KnowledgeRow } from "./components/KnowledgeRow";
import { UploadModal } from "./components/UploadModal";
import { errorMessage } from "./helpers";

/** Bursts of knowledge:changed (an upload, a scope change, a mid-turn
 * agent save) coalesce into one refetch inside this window. */
const REFETCH_COALESCE_MS = 100;

/** The scope filter's model: the Select's value — "all", "global", or one
 * agent's ID. */
type ScopeFilter = string;

export default function KnowledgeView(): ReactElement {
  const [docs, setDocs] = useState<KnowledgeDocument[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [filter, setFilter] = useState<ScopeFilter>("all");
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const [uploadOpen, setUploadOpen] = useState(false);

  const [pendingDelete, setPendingDelete] = useState<KnowledgeDocument | null>(
    null
  );

  const [demoteTarget, setDemoteTarget] = useState<KnowledgeDocument | null>(
    null
  );

  const [deleteBusy, setDeleteBusy] = useState(false);
  const [descBusy, setDescBusy] = useState(false);
  const [scopeBusy, setScopeBusy] = useState(false);

  // Handlers and the event subscription outlive render passes; they must
  // not write state after the menu switched away (views unmount).
  const mountedRef = useRef(true);

  useEffect((): (() => void) => {
    return (): void => {
      mountedRef.current = false;
    };
  }, []);

  const refetch = useCallback((): Promise<void> => {
    return client.knowledge
      .list()
      .then((list: KnowledgeDocument[]): void => {
        if (!mountedRef.current) return;
        setDocs(list);
        setError(null);
      })
      .catch((err: Error): void => {
        if (!mountedRef.current) return;
        setError(errorMessage(err, "Failed to load the documents"));
      });
  }, []);

  // Mount-time fetch plus the agents the filter, the upload scope picker,
  // and the demote picker need. A failed agents load keeps the pickers
  // empty rather than dead.
  useEffect((): (() => void) => {
    let live = true;

    void refetch().finally((): void => {
      if (live) setLoading(false);
    });
    client.agents
      .list()
      .then((list: Agent[]): void => {
        if (live) setAgents(list);
      })
      .catch((): void => undefined);

    return (): void => {
      live = false;
    };
  }, [refetch]);

  // Live updates: every mutation — upload, agent save, scope change,
  // description edit, delete — emits knowledge:changed; bursts coalesce
  // into one refetch.
  useEffect((): (() => void) => {
    let timer: ReturnType<typeof setTimeout> | null = null;

    const off = client.events.onKnowledgeChanged((): void => {
      if (timer !== null) return;
      timer = setTimeout((): void => {
        timer = null;
        void refetch();
      }, REFETCH_COALESCE_MS);
    });

    return (): void => {
      off();

      if (timer !== null) clearTimeout(timer);
    };
  }, [refetch]);

  const agentNameOf = (doc: KnowledgeDocument): string | null => {
    if (doc.scope !== "agent") return null;

    return (
      agents.find((a: Agent): boolean => a.ID === doc.agentId)?.Name ??
      (doc.agentSlug !== "" ? doc.agentSlug : null)
    );
  };

  const visible = docs.filter((d: KnowledgeDocument): boolean =>
    filter === "all"
      ? true
      : filter === "global"
        ? d.scope === "global"
        : d.scope === "agent" && d.agentId === filter
  );

  // The selected row resolves from the fresh list every render, so a
  // delete (here or from the right bar) drops the detail pane with it.
  const selected =
    selectedId === null
      ? null
      : (docs.find((d: KnowledgeDocument): boolean => d.id === selectedId) ??
        null);

  const handleConfirmDelete = (): void => {
    const target = pendingDelete;

    if (!target) return;
    setDeleteBusy(true);
    client.knowledge
      .delete(target.id)
      .then((): Promise<void> => refetch())
      .then((): void => {
        setPendingDelete(null);
      })
      .catch((err: Error): void => {
        toast(errorMessage(err, "Failed to delete the document"), "danger");
        setPendingDelete(null);
      })
      .finally((): void => {
        if (mountedRef.current) setDeleteBusy(false);
      });
  };

  const handleEditDescription = (
    doc: KnowledgeDocument,
    description: string
  ): void => {
    setDescBusy(true);
    client.knowledge
      .setDescription(doc.id, description)
      .then((): Promise<void> => refetch())
      .catch((err: Error): void => {
        toast(errorMessage(err, "Failed to update the description"), "danger");
      })
      .finally((): void => {
        if (mountedRef.current) setDescBusy(false);
      });
  };

  const handlePromote = (doc: KnowledgeDocument): void => {
    setScopeBusy(true);
    client.knowledge
      .setScope(doc.id, "global", "")
      .then((): Promise<void> => refetch())
      .catch((err: Error): void => {
        toast(errorMessage(err, "Failed to promote the document"), "danger");
      })
      .finally((): void => {
        if (mountedRef.current) setScopeBusy(false);
      });
  };

  const handleConfirmDemote = (agentID: string): void => {
    const target = demoteTarget;

    if (!target || agentID === "") return;
    setScopeBusy(true);
    client.knowledge
      .setScope(target.id, "agent", agentID)
      .then((): Promise<void> => refetch())
      .then((): void => {
        setDemoteTarget(null);
      })
      .catch((err: Error): void => {
        toast(errorMessage(err, "Failed to demote the document"), "danger");
        setDemoteTarget(null);
      })
      .finally((): void => {
        if (mountedRef.current) setScopeBusy(false);
      });
  };

  return (
    <div
      className="flex h-full min-h-0 min-w-0 flex-1 flex-col gap-4 p-6 lg:pt-10"
      data-testid="knowledge-view"
    >
      <PaneHeader
        title="Knowledge"
        description="Documents agents search with their filesystem tools — global documents reach every agent; agent-scoped ones stay with one agent."
        actions={
          <Button
            size="sm"
            data-testid="btn-upload-knowledge"
            onClick={() => setUploadOpen(true)}
          >
            <Plus aria-hidden="true" className="size-4" />
            Upload document
          </Button>
        }
      />

      {error && (
        <p
          className="text-sm text-danger"
          role="alert"
          data-testid="knowledge-error"
        >
          {error}
        </p>
      )}

      <div className="flex items-center gap-2">
        <label
          htmlFor="knowledge-scope-filter"
          className="text-[12px] text-muted"
        >
          Scope
        </label>
        <Select
          id="knowledge-scope-filter"
          data-testid="knowledge-scope-filter"
          className="w-56"
          value={filter}
          onChange={(e: ChangeEvent<HTMLSelectElement>): void =>
            setFilter(e.target.value)
          }
        >
          <option value="all">All documents</option>
          <option value="global">Global</option>
          {agents.map((a: Agent) => (
            <option key={a.ID} value={a.ID}>
              {a.Name}
            </option>
          ))}
        </Select>
      </div>

      {loading ? (
        <div className="flex flex-col gap-2" aria-label="Loading documents">
          {[0, 1, 2].map((i: number) => (
            <Skeleton key={i} className="h-16 w-full" />
          ))}
        </div>
      ) : docs.length === 0 ? (
        <div className="flex flex-1 items-start justify-center pt-8">
          <EmptyState
            testId="knowledge-empty"
            title="No documents yet"
            description="Upload a text or PDF document for the agents to search — or ask an agent in chat: agents can save documents with their save tool."
            action={
              <Button
                size="sm"
                data-testid="btn-upload-knowledge-empty"
                onClick={() => setUploadOpen(true)}
              >
                Upload document
              </Button>
            }
          />
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 gap-4">
          <div
            className="od-scroll flex w-1/2 min-w-0 flex-col gap-2 overflow-y-auto"
            role="list"
            aria-label="Knowledge documents"
            data-testid="knowledge-list"
          >
            {visible.map((doc: KnowledgeDocument) => (
              <KnowledgeRow
                key={doc.id}
                doc={doc}
                agentName={agentNameOf(doc)}
                selected={selected?.id === doc.id}
                onSelect={(): void => setSelectedId(doc.id)}
              />
            ))}
            {visible.length === 0 && (
              <p
                className="text-[13px] text-muted"
                data-testid="knowledge-filter-empty"
              >
                No documents in this scope.
              </p>
            )}
          </div>

          <aside
            className="od-scroll min-w-0 flex-1 overflow-y-auto rounded-md border border-line p-4"
            data-testid="knowledge-detail"
          >
            {selected ? (
              <DetailPane
                doc={selected}
                agentName={agentNameOf(selected)}
                agents={agents}
                descBusy={descBusy}
                scopeBusy={scopeBusy}
                onEditDescription={(description: string): void =>
                  handleEditDescription(selected, description)
                }
                onPromote={(): void => handlePromote(selected)}
                onDemote={(): void => setDemoteTarget(selected)}
                onDelete={(): void => setPendingDelete(selected)}
              />
            ) : (
              <p
                className="text-[13px] text-muted"
                data-testid="knowledge-detail-empty"
              >
                Select a document to preview and manage it.
              </p>
            )}
          </aside>
        </div>
      )}

      {uploadOpen && (
        <UploadModal
          agents={agents}
          onClose={(): void => setUploadOpen(false)}
          onUploaded={(): void => {
            void refetch();
          }}
        />
      )}

      {pendingDelete && (
        <ConfirmDialog
          title="Delete document"
          testId="delete-knowledge-dialog"
          confirmTestId="btn-confirm-delete-doc"
          cancelTestId="btn-cancel-delete-doc"
          confirmDisabled={deleteBusy}
          onConfirm={handleConfirmDelete}
          onClose={(): void => {
            if (!deleteBusy) setPendingDelete(null);
          }}
        >
          <p className="text-[13px] text-muted">
            Deleting “{pendingDelete.filename}” removes it from the list,
            catalog, and disk. This cannot be undone.
          </p>
        </ConfirmDialog>
      )}

      {demoteTarget && (
        <DemoteModal
          doc={demoteTarget}
          agents={agents}
          busy={scopeBusy}
          onConfirm={(agentID: string): void => handleConfirmDemote(agentID)}
          onClose={(): void => setDemoteTarget(null)}
        />
      )}
    </div>
  );
}
