import { useEffect, useRef, useState } from "react";
import { cx } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { Chip } from "../../components/ui/Chip";
import { Modal } from "../../components/ui/Modal";
import { ErrorState } from "../../components/ErrorState";
import { inputCls, labelCls } from "../../components/ui/constants";
import { DocumentSource } from "../../components/chat/panel/sources/document/DocumentSource";
import {
  api,
  ApiError,
  formatApiError,
  type ApiAgent,
  type ApiChannel,
} from "../../lib/api";
import {
  documentsApi,
  documentTypeLabel,
  DOCUMENT_PICKER_ACCEPT,
  indexStatusLabel,
  precheckDocument,
  scopeBadge,
  useCanPromoteDocuments,
  type ApiReferenceDocument,
} from "../../lib/documentsApi";
import serverErrorSvg from "../../assets/server-error.svg";

export interface DocumentsPaneProps {
  tenant: any;
  onUpdate?: (fn: any) => void;
  onToast?: (text: string, kind?: string) => void;
  /** Override the derived reference_documents.promote check (tests). */
  canPromote?: boolean;
}

// Per-upload progress is COMPONENT-LOCAL state (lib/attachments.ts:13-15 —
// high-frequency store writes trip React 19's nested-update limit). One strip
// row per in-flight upload/replace; it disappears when the request settles.
interface UploadStrip {
  key: string;
  name: string;
  progress: number;
}

let uploadSeq = 0;

const sameIdSet = (a: string[], b: string[]) =>
  [...(a || [])].sort().join("\u0000") === [...(b || [])].sort().join("\u0000");

// Workspace reference library (add-reference-documents tasks 8.2/8.3): upload
// with drag-drop + progress, rows with index/scope badges, edit + attach
// editor, replace, delete, and the admin promote/demote scope flip. Upload,
// attach, edit, and delete ride ordinary membership; the promote toggle is
// the only role-gated control (reference_documents.promote).
export function DocumentsPane({ tenant, onToast = () => {}, canPromote }: DocumentsPaneProps) {
  const derivedCanPromote = useCanPromoteDocuments(tenant);
  const promoter = canPromote !== undefined ? canPromote : derivedCanPromote;

  const [docs, setDocs] = useState<ApiReferenceDocument[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<ApiError | Error | null>(null);

  const [uploads, setUploads] = useState<UploadStrip[]>([]);
  const [dragOver, setDragOver] = useState(false);
  const [editing, setEditing] = useState<ApiReferenceDocument | null>(null);
  const [deleting, setDeleting] = useState<ApiReferenceDocument | null>(null);
  const [replaceTarget, setReplaceTarget] = useState<ApiReferenceDocument | null>(null);
  const [previewing, setPreviewing] = useState<ApiReferenceDocument | null>(null);
  const [busy, setBusy] = useState(false);

  const uploadInputRef = useRef<HTMLInputElement>(null);
  const replaceInputRef = useRef<HTMLInputElement>(null);
  const dragDepth = useRef(0);

  const targetWsId = tenant?.sub || tenant?.id;

  const loadDocs = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const res = await documentsApi.list(targetWsId);
      setDocs(res.documents || []);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 0) {
        return;
      }
      setLoadError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (targetWsId) void loadDocs();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload per workspace only
  }, [targetWsId]);

  const patchInPlace = (updated: ApiReferenceDocument) => {
    setDocs((prev) => prev.map((d) => (d.id === updated.id ? updated : d)));
  };

  const startStrip = (name: string): string => {
    const key = `up-${++uploadSeq}`;
    setUploads((prev) => [...prev, { key, name, progress: 0 }]);
    return key;
  };

  const endStrip = (key: string) => {
    setUploads((prev) => prev.filter((u) => u.key !== key));
  };

  const tickStrip = (key: string, progress: number) => {
    setUploads((prev) => prev.map((u) => (u.key === key ? { ...u, progress } : u)));
  };

  // Upload lane: client precheck first (instant feedback — the server re-
  // validates), then sequential multipart uploads sharing the progress strip.
  const handleFiles = async (files: File[]) => {
    const valid: File[] = [];
    const rejected: string[] = [];
    for (const f of files) {
      const reason = precheckDocument(f);
      if (reason) rejected.push(`${f.name}: ${reason}`);
      else valid.push(f);
    }
    if (rejected.length > 0) onToast(rejected.join(" · "), "danger");

    for (const f of valid) {
      const key = startStrip(f.name);
      try {
        const created = await documentsApi.upload(targetWsId, f, {}, {
          onProgress: (p) => tickStrip(key, p),
        });
        endStrip(key);
        setDocs((prev) => [created, ...prev]);
        onToast(`${created.name} uploaded`);
      } catch (err: unknown) {
        endStrip(key);
        onToast(formatApiError(err, `Failed to upload ${f.name}`), "danger");
      }
    }
  };

  const handleReplaceFile = async (file: File | undefined) => {
    const target = replaceTarget;
    if (!file || !target) return;
    const reason = precheckDocument(file);
    if (reason) {
      onToast(`${file.name}: ${reason}`, "danger");
      return;
    }
    const key = startStrip(file.name);
    try {
      const updated = await documentsApi.replace(targetWsId, target.id, file, {
        onProgress: (p) => tickStrip(key, p),
      });
      endStrip(key);
      patchInPlace(updated);
      onToast(`${updated.name} replaced — content re-indexed`);
    } catch (err: unknown) {
      endStrip(key);
      onToast(formatApiError(err, `Failed to replace ${target.name}`), "danger");
    } finally {
      setReplaceTarget(null);
    }
  };

  const handleDelete = async (doc: ApiReferenceDocument) => {
    setBusy(true);
    try {
      await documentsApi.remove(targetWsId, doc.id);
      setDocs((prev) => prev.filter((d) => d.id !== doc.id));
      setDeleting(null);
      onToast(`${doc.name} deleted`);
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to delete ${doc.name}`), "danger");
    } finally {
      setBusy(false);
    }
  };

  const handleScopeFlip = async (doc: ApiReferenceDocument, promote: boolean) => {
    try {
      const updated = promote
        ? await documentsApi.promote(targetWsId, doc.id)
        : await documentsApi.demote(targetWsId, doc.id);
      patchInPlace(updated);
      onToast(
        promote
          ? `${doc.name} promoted — visible to every agent in this workspace`
          : `${doc.name} demoted — back to its attached agents and channels`
      );
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to ${promote ? "promote" : "demote"} ${doc.name}`), "danger");
    }
  };

  // Preview (rework-document-chat-surfaces 1.1): this settings pane has no
  // right-panel host (the panel mounts only in ChatRoute), so the eye button
  // opens a standalone modal hosting the same DocumentSource the panel uses —
  // not openPanelTab, which would write invisible panel state here.
  const openPreview = (doc: ApiReferenceDocument) => {
    setPreviewing(doc);
  };

  const renderRow = (doc: ApiReferenceDocument) => {
    return (
      <li
        key={doc.id}
        className="flex items-center gap-3 py-3"
        data-od-id={"document-" + doc.name}
        data-testid={"document-" + doc.name}
      >
        <span
          title={documentTypeLabel(doc.name, doc.mime)}
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent"
        >
          <Icon name="file" size={16} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <p className="font-mono text-[13px] font-medium text-fg">{doc.name}</p>
            <Chip>
              <span data-testid={"doc-index-" + doc.indexStatus}>{indexStatusLabel(doc.indexStatus)}</span>
            </Chip>
            <Chip>{scopeBadge(doc)}</Chip>
            {doc.pageCount > 0 ? <span className="text-[11px] text-muted">{doc.pageCount} pages</span> : null}
          </div>
          {doc.description ? (
            <p className="mt-0.5 text-[12px] leading-4 text-muted">{doc.description}</p>
          ) : null}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <button
            type="button"
            aria-label={"Preview " + doc.name}
            data-testid={"btn-preview-" + doc.name}
            onClick={() => openPreview(doc)}
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
          >
            <Icon name="eye" size={13} />
          </button>
          <button
            type="button"
            aria-label={"Edit " + doc.name}
            data-od-id={"btn-edit-" + doc.name}
            data-testid={"btn-edit-" + doc.name}
            onClick={() => setEditing(doc)}
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
          >
            <Icon name="edit" size={13} />
          </button>
          <button
            type="button"
            aria-label={"Replace " + doc.name}
            data-testid={"btn-replace-" + doc.name}
            onClick={() => setReplaceTarget(doc)}
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
          >
            <Icon name="refresh" size={13} />
          </button>
          {promoter ? (
            doc.scope === "workspace" ? (
              <button
                type="button"
                aria-label={"Demote " + doc.name}
                data-testid={"btn-demote-" + doc.name}
                onClick={() => void handleScopeFlip(doc, false)}
                className="flex h-7 items-center rounded-[6px] border border-line px-2 text-[11px] font-medium text-muted transition-colors hover:border-accent hover:text-fg"
              >
                Demote
              </button>
            ) : (
              <button
                type="button"
                aria-label={"Promote " + doc.name}
                data-testid={"btn-promote-" + doc.name}
                onClick={() => void handleScopeFlip(doc, true)}
                className="flex h-7 items-center rounded-[6px] border border-line px-2 text-[11px] font-medium text-muted transition-colors hover:border-accent hover:text-fg"
              >
                Promote
              </button>
            )
          ) : null}
          <button
            type="button"
            aria-label={"Delete " + doc.name}
            data-testid={"btn-delete-" + doc.name}
            onClick={() => setDeleting(doc)}
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger"
          >
            <Icon name="trash" size={13} />
          </button>
        </div>
      </li>
    );
  };

  return (
    <div className="max-w-xl" data-od-id="pane-documents" data-testid="pane-documents">
      <div className="mb-4 flex items-center justify-between">
        <div>
          <h3 className="text-[15px] font-semibold text-fg">Reference documents</h3>
          <p className="mt-0.5 text-[12px] text-muted">
            Documents your agents can search and cite. Attach them to agents and channels, or promote one
            workspace-wide.
          </p>
        </div>
        <button
          type="button"
          onClick={() => uploadInputRef.current?.click()}
          data-od-id="btn-upload-document"
          data-testid="btn-upload-document"
          className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
        >
          Upload document
        </button>
      </div>

      {/* Drag/drop zone + picker target. The picker's accept attribute is a
          hint only — drag-drop bypasses it, so the same precheck runs for
          both lanes before anything hits the wire. */}
      <div
        data-testid="documents-dropzone"
        onDragEnter={(e) => {
          e.preventDefault();
          dragDepth.current += 1;
          setDragOver(true);
        }}
        onDragOver={(e) => e.preventDefault()}
        onDragLeave={() => {
          dragDepth.current -= 1;
          if (dragDepth.current <= 0) {
            dragDepth.current = 0;
            setDragOver(false);
          }
        }}
        onDrop={(e) => {
          e.preventDefault();
          dragDepth.current = 0;
          setDragOver(false);
          void handleFiles(Array.from(e.dataTransfer?.files || []));
        }}
        onClick={() => uploadInputRef.current?.click()}
        className={cx(
          "mb-4 cursor-pointer rounded-lg border border-dashed px-4 py-5 text-center transition-colors",
          dragOver
            ? "border-accent bg-[color-mix(in_oklab,var(--accent)_8%,transparent)]"
            : "border-line hover:border-[color-mix(in_oklab,var(--accent)_55%,var(--border))]"
        )}
      >
        <p className="text-[12.5px] text-fg2">
          Drag files here, or <span className="font-medium text-accent">browse</span>
        </p>
        <p className="mt-1 text-[11px] text-muted">
          PDF up to 20 MB · docx, pptx, xlsx, md, txt, html, csv up to 50 MB
        </p>
      </div>
      <input
        ref={uploadInputRef}
        type="file"
        multiple
        accept={DOCUMENT_PICKER_ACCEPT}
        className="hidden"
        data-testid="input-upload-document"
        onChange={(e) => {
          void handleFiles(Array.from(e.target.files || []));
          e.target.value = "";
        }}
      />
      <input
        ref={replaceInputRef}
        type="file"
        accept={DOCUMENT_PICKER_ACCEPT}
        className="hidden"
        data-testid="input-replace-document"
        onChange={(e) => {
          void handleReplaceFile(e.target.files?.[0]);
          e.target.value = "";
        }}
      />
      {/* Replace flows through the picker programmatically once a row's
          replace button has named its target. */}
      <ReplaceTrigger target={replaceTarget} onPick={() => replaceInputRef.current?.click()} />

      {uploads.length > 0 ? (
        <ul className="mb-4 space-y-2" data-testid="document-uploads">
          {uploads.map((u) => (
            <li key={u.key} data-testid={"upload-progress-" + u.name}>
              <div className="flex items-center justify-between text-[11px] text-muted">
                <span className="truncate font-mono">{u.name}</span>
                <span>{u.progress}%</span>
              </div>
              <div className="mt-1 h-1 overflow-hidden rounded-full bg-[color-mix(in_oklab,var(--fg)_8%,transparent)]">
                <div
                  className="h-full rounded-full bg-accent transition-[width] duration-150"
                  style={{ width: `${u.progress}%` }}
                />
              </div>
            </li>
          ))}
        </ul>
      ) : null}

      {loading && docs.length === 0 ? (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-16 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]" />
          ))}
        </div>
      ) : loadError ? (
        <div className="py-6">
          <ErrorState
            illustration={serverErrorSvg}
            title="Couldn't load documents"
            detail={loadError.message}
            primaryAction={{ label: "Retry", onClick: loadDocs }}
          />
        </div>
      ) : (
        <ul className="divide-y divide-[var(--border-soft)]" data-testid="documents-library">
          {docs.map((d) => renderRow(d))}
          {docs.length === 0 && (
            <li className="py-8 text-center" data-od-id="documents-empty" data-testid="documents-empty">
              <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
                <Icon name="file" size={20} />
              </div>
              <p className="text-[14px] font-medium text-fg">No documents yet</p>
              <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
                Upload a PDF or document and every attached agent can search and cite it.
              </p>
              <button
                type="button"
                onClick={() => uploadInputRef.current?.click()}
                data-od-id="btn-upload-document-empty"
                data-testid="btn-upload-document-empty"
                className="mt-3 inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
              >
                <Icon name="up" size={13} /> Upload your first document
              </button>
            </li>
          )}
        </ul>
      )}

      {editing ? (
        <DocumentEditDialog
          ws={targetWsId}
          doc={editing}
          onClose={() => setEditing(null)}
          onSaved={(updated) => {
            patchInPlace(updated);
            setEditing(null);
            onToast(`${updated.name} saved`);
          }}
          onToast={onToast}
        />
      ) : null}

      {deleting ? (
        <Modal
          title={`Delete ${deleting.name}`}
          onClose={() => setDeleting(null)}
          odId="modal-document-delete"
          data-testid="modal-document-delete"
          footer={
            <>
              <button
                type="button"
                onClick={() => setDeleting(null)}
                data-testid="btn-delete-cancel"
                className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void handleDelete(deleting)}
                disabled={busy}
                data-testid="btn-delete-confirm"
                className="flex h-9 items-center rounded-md bg-danger px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
              >
                {busy ? "Deleting…" : "Delete"}
              </button>
            </>
          }
        >
          <p className="p-5 text-[13px] leading-5 text-fg2">
            {deleting.name} is removed from every attached agent and channel and its file is deleted. This
            cannot be undone.
          </p>
        </Modal>
      ) : null}

      {previewing ? (
        <Modal
          title={previewing.name}
          onClose={() => setPreviewing(null)}
          odId="modal-document-preview"
          data-testid="modal-document-preview"
          wide
        >
          {/* Same source the right panel renders, wrapped in the tab shape it
              expects; keyed by {name, url} so a reopened document remounts. */}
          <DocumentSource
            key={previewing.name + "\u0000" + previewing.url}
            tab={{
              kind: "document",
              title: previewing.name,
              payload: { name: previewing.name, url: previewing.url },
            }}
          />
        </Modal>
      ) : null}
    </div>
  );
}

// Clicking a row's replace button names the target first; this effect opens
// the picker once that target is set.
function ReplaceTrigger({ target, onPick }: { target: ApiReferenceDocument | null; onPick: () => void }) {
  useEffect(() => {
    if (target) onPick();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fires per target change
  }, [target]);
  return null;
}

// Edit surface (single dialog, the skills-gallery pattern): rename +
// re-describe, and the attach editor for the agent/channel visibility tiers.
// Saving always patches the fields; the attach PUTs fire only when a set
// actually changed.
function DocumentEditDialog({
  ws,
  doc,
  onClose,
  onSaved,
  onToast,
}: {
  ws: string;
  doc: ApiReferenceDocument;
  onClose: () => void;
  onSaved: (doc: ApiReferenceDocument) => void;
  onToast: (text: string, kind?: string) => void;
}) {
  const [name, setName] = useState(doc.name);
  const [description, setDescription] = useState(doc.description || "");
  const [agentIds, setAgentIds] = useState<string[]>(doc.agents || []);
  const [channelIds, setChannelIds] = useState<string[]>(doc.channels || []);
  const [agents, setAgents] = useState<ApiAgent[]>([]);
  const [channels, setChannels] = useState<ApiChannel[]>([]);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    void api.agents
      .list(ws)
      .then((res) => alive && setAgents(res.agents || []))
      .catch(() => {});
    void api.channels
      .list(ws)
      .then((res) => alive && setChannels(res.channels || []))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [ws]);

  const toggle = (list: string[], id: string, set: (v: string[]) => void) => {
    set(list.includes(id) ? list.filter((x) => x !== id) : [...list, id]);
  };

  const save = async () => {
    setBusy(true);
    try {
      let updated = await documentsApi.patch(ws, doc.id, {
        name: name.trim() || doc.name,
        description,
      });
      if (!sameIdSet(agentIds, doc.agents || [])) {
        updated = await documentsApi.setAgents(ws, doc.id, agentIds);
      }
      if (!sameIdSet(channelIds, doc.channels || [])) {
        updated = await documentsApi.setChannels(ws, doc.id, channelIds);
      }
      onSaved(updated);
    } catch (err: unknown) {
      onToast(formatApiError(err, `Failed to save ${doc.name}`), "danger");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      title={`Edit ${doc.name}`}
      onClose={onClose}
      odId="modal-document-edit"
      data-testid="modal-document-edit"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            data-testid="btn-document-edit-cancel"
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => void save()}
            disabled={busy}
            data-testid="btn-document-edit-save"
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] disabled:opacity-40"
          >
            {busy ? "Saving…" : "Save"}
          </button>
        </>
      }
    >
      <div className="space-y-4 p-5">
        <div>
          <label className={labelCls} htmlFor="document-edit-name">
            Name
          </label>
          <input
            id="document-edit-name"
            className={inputCls}
            value={name}
            data-testid="input-document-name"
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div>
          <label className={labelCls} htmlFor="document-edit-description">
            Description
          </label>
          <input
            id="document-edit-description"
            className={inputCls}
            value={description}
            data-testid="input-document-description"
            placeholder="What this document covers — agents recite it when deciding to open it"
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>

        <div>
          <p className={labelCls}>Attached agents</p>
          {agents.length === 0 ? (
            <p className="text-[12px] text-muted">No agents in this workspace yet.</p>
          ) : (
            <ul className="mt-1 max-h-40 space-y-1 overflow-y-auto rounded-md border border-line p-2">
              {agents.map((a) => (
                <li key={a.id}>
                  <label className="flex cursor-pointer items-center gap-2 text-[12.5px] text-fg2">
                    <input
                      type="checkbox"
                      checked={agentIds.includes(a.id)}
                      data-testid={"attach-agent-" + a.slug}
                      onChange={() => toggle(agentIds, a.id, setAgentIds)}
                    />
                    <span className="font-mono">{a.name}</span>
                  </label>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div>
          <p className={labelCls}>Attached channels</p>
          {channels.length === 0 ? (
            <p className="text-[12px] text-muted">No channels in this workspace yet.</p>
          ) : (
            <ul className="mt-1 max-h-40 space-y-1 overflow-y-auto rounded-md border border-line p-2">
              {channels.map((c) => (
                <li key={c.id}>
                  <label className="flex cursor-pointer items-center gap-2 text-[12.5px] text-fg2">
                    <input
                      type="checkbox"
                      checked={channelIds.includes(c.id)}
                      data-testid={"attach-channel-" + c.slug}
                      onChange={() => toggle(channelIds, c.id, setChannelIds)}
                    />
                    <span className="font-mono">#{c.slug}</span>
                  </label>
                </li>
              ))}
            </ul>
          )}
        </div>
        <p className="text-[11px] leading-4 text-muted">
          Attached documents are visible to exactly these agents and channels. Promote from the list to make
          one workspace-wide.
        </p>
      </div>
    </Modal>
  );
}
