/**
 * The right bar's knowledge panel (add-knowledge-corpus, spec chat-panels
 * "Knowledge panel"): opened from the bar's identity view, it lists the
 * session agent's scoped documents plus the global ones — name, description,
 * and a scope marker — with upload defaulting to the agent's scope (global
 * as an explicit choice), the corpus pre-gates applied client-side before
 * the round trip (the backend stays the authority), and a promote action on
 * agent-scoped documents. Activation opens the document's preview tab
 * through the bar. The list refetches coalesced on `knowledge:changed` while
 * mounted, so a mid-turn `save_document` save appears without reopening.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";
import { ChevronLeft, File as FileIcon, Upload } from "lucide-react";

import type { KnowledgeDocument } from "../../../lib/client";
import { cn } from "../../../lib/cn";
import { client } from "../../../lib/client";

/** Bursts of knowledge:changed (an upload, an agent save, a scope change)
 * coalesce into one refetch inside this window (scheduler pattern). */
const REFETCH_COALESCE_MS = 100;

/** Lane caps for the knowledge corpus, mirroring the chat-attachments
 * shape: PDFs 20 MB, text 1 MB; a mime in no lane is unsupported. */
const MAX_KNOWLEDGE_PDF_BYTES = 20 << 20;

const MAX_KNOWLEDGE_TEXT_BYTES = 1 << 20;

/** The corpus pre-gate the panel applies before the upload round trip:
 * application/pdf and text/* only, at the lane caps. Returns the rejection
 * reason or null when the file may go up. */
export function knowledgeRejection(mime: string, size: number): string | null {
  if (mime === "application/pdf") {
    if (size > MAX_KNOWLEDGE_PDF_BYTES) return "Exceeds the 20 MB PDF limit";

    return null;
  }

  if (mime.startsWith("text/")) {
    if (size > MAX_KNOWLEDGE_TEXT_BYTES)
      return "Exceeds the 1 MB text-file limit";

    return null;
  }

  return `Unsupported file type "${mime}" — text and PDF documents are accepted`;
}

/** File → base64 (without the data-URL prefix) for the upload binding call;
 * the chat-attachments FileReader shape, local to this panel. */
function fileToBase64(file: File): Promise<string> {
  return new Promise(
    (
      resolve: (value: string) => void,
      reject: (cause: Error) => void
    ): void => {
      const reader = new FileReader();

      reader.onload = (): void => {
        // SAFETY: readAsDataURL always resolves a data: URL string (never an
        // ArrayBuffer); an empty result means the read produced nothing.
        const result = reader.result as string;

        if (result === "") {
          reject(new Error("Could not read the file"));

          return;
        }

        const comma = result.indexOf(",");

        resolve(comma === -1 ? result : result.slice(comma + 1));
      };

      reader.onerror = (): void => reject(new Error("Could not read the file"));
      reader.readAsDataURL(file);
    }
  );
}

/** One document row's scope marker chip. */
function ScopeMarker({
  docID,
  scope,
}: {
  docID: string;
  scope: string;
}): ReactElement {
  return (
    <span
      className="shrink-0 rounded border border-line px-1 py-px text-[10px] uppercase tracking-wide text-muted"
      data-testid={`knowledge-scope-${docID}`}
    >
      {scope}
    </span>
  );
}

export function KnowledgePanel({
  agentID,
  onOpenDoc,
  onBack,
}: {
  agentID: string;
  onOpenDoc: (doc: KnowledgeDocument) => void;
  onBack: () => void;
}): ReactElement {
  const [docs, setDocs] = useState<KnowledgeDocument[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Upload form: the scope defaults to the session agent, global is the
  // explicit choice; the description is optional.
  const [scope, setScope] = useState<"agent" | "global">("agent");
  const [description, setDescription] = useState("");
  const [uploading, setUploading] = useState(false);

  // Promote is per-row busy so one slow setScope never locks the list.
  const [promoteBusyID, setPromoteBusyID] = useState<string | null>(null);

  /** The latest backend rejection (upload or promote), surfaced inline. */
  const [actionError, setActionError] = useState<string | null>(null);

  // Handlers and the event subscription outlive render passes; they must
  // not write state after the panel unmounted (the bar swapped away).
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

        // The session agent's scope plus every global document; other
        // agents' scoped documents stay out of this panel.
        setDocs(
          list.filter(
            (d: KnowledgeDocument): boolean =>
              d.scope === "global" ||
              (d.scope === "agent" && d.agentId === agentID)
          )
        );
        setError(null);
      })
      .catch((err: Error): void => {
        if (!mountedRef.current) return;

        setError(
          err instanceof Error ? err.message : "Failed to load documents"
        );
      });
  }, [agentID]);

  // Mount-time fetch; the panel remounts per browsing session, so the list
  // is always fresh on open.
  useEffect((): void => {
    void refetch();
  }, [refetch]);

  // Live updates (spec "Mid-turn save appears in the panel"): every corpus
  // mutation emits knowledge:changed; bursts coalesce into one refetch.
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

  const handleFile = async (
    event: ChangeEvent<HTMLInputElement>
  ): Promise<void> => {
    const file = event.target.files?.[0];

    // Reset the input so picking the same file again re-fires change.
    event.target.value = "";

    if (!file) return;

    const reason = knowledgeRejection(file.type, file.size);

    if (reason !== null) {
      setActionError(reason);

      return;
    }

    setActionError(null);
    setUploading(true);

    try {
      const base64 = await fileToBase64(file);

      await client.knowledge.upload(
        file.name,
        file.type,
        base64,
        scope,
        scope === "global" ? "" : agentID,
        description
      );

      if (mountedRef.current) setDescription("");
      await refetch();
    } catch (err: unknown) {
      if (mountedRef.current)
        setActionError(err instanceof Error ? err.message : "Upload failed");
    } finally {
      if (mountedRef.current) setUploading(false);
    }
  };

  const promote = (doc: KnowledgeDocument): void => {
    setPromoteBusyID(doc.id);
    setActionError(null);
    client.knowledge
      .setScope(doc.id, "global", "")
      .then((): Promise<void> => refetch())
      .catch((err: Error): void => {
        if (mountedRef.current)
          setActionError(
            err instanceof Error
              ? err.message
              : "Failed to promote the document"
          );
      })
      .finally((): void => {
        if (mountedRef.current) setPromoteBusyID(null);
      });
  };

  return (
    <div className="flex w-full flex-col gap-3" data-testid="knowledge-panel">
      <div className="flex items-center gap-1">
        <button
          type="button"
          className="flex cursor-pointer items-center rounded-md p-1 text-muted hover:text-fg"
          aria-label="Back to session"
          data-testid="knowledge-back"
          onClick={onBack}
        >
          <ChevronLeft aria-hidden="true" className="size-4" />
        </button>
        <p className="text-sm font-semibold text-fg">Knowledge</p>
      </div>

      {/* Upload form: the corpus pre-gates reject before the round trip;
        backend rejections surface in the same inline slot. */}
      <div className="flex flex-col gap-1.5 rounded-md border border-line p-2">
        <div className="flex items-center gap-1.5">
          <select
            className="min-w-0 flex-1 cursor-pointer rounded-md border border-line bg-transparent px-1.5 py-1 text-xs text-fg2"
            aria-label="Upload scope"
            value={scope}
            disabled={uploading}
            data-testid="knowledge-upload-scope"
            onChange={(e: ChangeEvent<HTMLSelectElement>): void =>
              setScope(e.target.value === "global" ? "global" : "agent")
            }
          >
            <option value="agent">This agent</option>
            <option value="global">Global</option>
          </select>
          <input
            className="min-w-0 flex-1 rounded-md border border-line bg-transparent px-1.5 py-1 text-xs text-fg2 placeholder:text-muted focus:border-accent focus:outline-none"
            placeholder="Description (optional)"
            value={description}
            disabled={uploading}
            data-testid="knowledge-upload-description"
            onChange={(e: ChangeEvent<HTMLInputElement>): void =>
              setDescription(e.target.value)
            }
          />
        </div>
        <label
          className={cn(
            "flex cursor-pointer items-center justify-center gap-1.5 rounded-md border border-line px-3 py-1.5 text-[12px] font-medium text-fg2",
            "hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg",
            uploading && "cursor-not-allowed opacity-50"
          )}
        >
          <Upload aria-hidden="true" className="size-3.5" />
          {uploading ? "Uploading…" : "Add document"}
          <input
            type="file"
            className="hidden"
            accept="text/*,application/pdf,.md,.markdown,.txt"
            disabled={uploading}
            data-testid="knowledge-upload-input"
            onChange={handleFile}
          />
        </label>
      </div>

      {actionError !== null ? (
        <p
          className="text-xs text-dangertext"
          data-testid="knowledge-action-error"
        >
          {actionError}
        </p>
      ) : null}

      {error !== null ? (
        <p className="text-sm text-dangertext" data-testid="knowledge-error">
          {error}
        </p>
      ) : docs === null ? (
        <p className="text-sm text-muted" data-testid="knowledge-loading">
          Loading…
        </p>
      ) : docs.length === 0 ? (
        <p className="text-sm text-muted" data-testid="knowledge-empty">
          No documents yet — add one above or have the agent save findings with
          save_document.
        </p>
      ) : (
        <ul className="space-y-0.5" data-testid="knowledge-list">
          {docs.map((doc: KnowledgeDocument) => (
            <li
              key={doc.id}
              className="flex items-start gap-1.5 rounded-md px-1.5 py-1 hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]"
            >
              <FileIcon
                aria-hidden="true"
                className="mt-0.5 size-3.5 shrink-0 text-muted"
              />
              <button
                type="button"
                className="min-w-0 flex-1 cursor-pointer text-left"
                data-testid={`knowledge-doc-${doc.id}`}
                onClick={() => onOpenDoc(doc)}
              >
                <span className="block truncate text-xs text-fg">
                  {doc.filename}
                </span>
                {doc.description !== "" ? (
                  <span className="block truncate text-[11px] text-muted">
                    {doc.description}
                  </span>
                ) : null}
              </button>
              <ScopeMarker docID={doc.id} scope={doc.scope} />
              {doc.scope === "agent" ? (
                <button
                  type="button"
                  className="shrink-0 cursor-pointer rounded-md border border-line px-1.5 py-0.5 text-[11px] text-muted hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
                  title="Promote to global"
                  disabled={promoteBusyID === doc.id}
                  data-testid={`knowledge-promote-${doc.id}`}
                  onClick={() => promote(doc)}
                >
                  Promote
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
