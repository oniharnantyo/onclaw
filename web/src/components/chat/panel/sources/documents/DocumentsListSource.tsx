// Documents listing source (add-reference-documents task 10.5): the right
// panel pane listing the conversation-visible reference documents — the same
// visibility lens the composer popover used (direct chats resolve by agent
// attachments + promoted, channels by channel attachments + promoted; the
// backend's list lens implements the predicate, design D7). A row click opens
// the document preview tab through the same 'document' source the transcript
// chips ride; the per-row Insert action (rework-document-chat-surfaces 2.3)
// inserts the mention into the composer through the mounting screen's bridge.
import { useEffect, useRef, useState } from "react";
import { useStore, useWorkspace } from "../../../../../store";
import { documentsApi, indexStatusLabel, scopeBadge } from "../../../../../lib/documentsApi";
import type { ApiReferenceDocument, DocumentLens } from "../../../../../lib/documentsApi";
import { Icon } from "../../../../ui/Icon";

type Phase = 'loading' | 'error' | 'ready';

/** The conversation scope, from the mounting screen's context when present,
 * otherwise derived from the store's active chat — the same derivation the
 * file source uses for its ws/agent pair. */
function conversationLens(ctx: any, tenant: any): { ws: string; lens?: DocumentLens } {
  const ws: string = ctx?.workspaceId ?? tenant?.sub ?? tenant?.id ?? '';
  if (ctx?.documentsLens) return { ws, lens: ctx.documentsLens };
  const chatId: string = useStore.getState().pos.chatId;
  if (!chatId) return { ws };
  if ((tenant?.agents || []).some((a: any) => a.id === chatId)) return { ws, lens: { agent: chatId } };
  if ((tenant?.channels || []).some((c: any) => c.id === chatId)) return { ws, lens: { channel: chatId } };
  return { ws };
}

export function DocumentsListSource({ ctx }: { tab?: any; ctx?: any }) {
  const tenant = useWorkspace();
  const { ws, lens } = conversationLens(ctx, tenant);

  const [phase, setPhase] = useState<Phase>('loading');
  const [docs, setDocs] = useState<ApiReferenceDocument[]>([]);
  const [message, setMessage] = useState('');
  const seq = useRef(0);

  useEffect(() => {
    if (!ws || !lens) return;
    const id = ++seq.current;
    setPhase('loading');
    documentsApi.list(ws, lens)
      .then((res) => {
        if (seq.current !== id) return;
        setDocs(res.documents || []);
        setPhase('ready');
      })
      .catch((err: unknown) => {
        if (seq.current !== id) return;
        setMessage(err instanceof Error ? err.message : String(err));
        setPhase('error');
      });
  }, [ws, lens?.agent, lens?.channel]);

  const openDocument = (doc: ApiReferenceDocument) => {
    const entry = { kind: 'document', title: doc.name, payload: { name: doc.name, url: doc.url } };
    if (ctx?.openPanelTab) ctx.openPanelTab(entry);
    else useStore.getState().openPanelTab(entry);
  };

  // Mention bridge (rework-document-chat-surfaces 2.3, design D4): preview
  // stays the row's primary click; Insert is an explicit secondary text
  // button that hands the document identity to the composer through the
  // mounting screen's callback. Hidden entirely when no bridge is present —
  // a panel host without a chat composer has nothing to insert into.
  const canInsert = typeof ctx?.insertDocumentMention === 'function';
  const insertDocument = (doc: ApiReferenceDocument) => {
    ctx.insertDocumentMention({ id: doc.id, name: doc.name });
  };

  if (!ws || !lens) {
    return (
      <div className="p-4" data-od-id="panel-documents-unavailable">
        <p className="text-[12.5px] font-medium text-fg2">No conversation in scope</p>
        <p className="mt-1 text-[12px] text-muted">Open an agent chat or a channel to see its visible documents.</p>
      </div>
    );
  }

  if (phase === 'loading') {
    return (
      <div className="flex items-center gap-2 p-4 text-[12px] text-muted" data-testid="panel-documents-loading">
        <span className="od-dot"/><span className="od-dot"/><span className="od-dot"/>
        <span>Loading documents…</span>
      </div>
    );
  }
  if (phase === 'error') {
    return (
      <div className="p-4" data-testid="panel-documents-error">
        <p className="text-[12.5px] font-medium text-danger">Couldn't load documents</p>
        <p className="mt-1 text-[12px] text-muted">{message}</p>
      </div>
    );
  }

  return (
    <div className="p-2" data-testid="panel-documents-list">
      {docs.length === 0 && (
        <p className="px-2 py-3 text-[12px] text-muted" data-testid="panel-documents-empty">
          No reference documents are visible in this conversation yet — upload them in workspace settings.
        </p>
      )}
      <div className="space-y-0.5">
        {docs.map((doc) => (
          <div key={doc.id} className="flex w-full items-center gap-1 rounded-md transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]">
            <button type="button" data-testid={'panel-doc-' + doc.name}
              onClick={() => openDocument(doc)}
              title={'Preview ' + doc.name}
              className="flex min-w-0 flex-1 items-start gap-2.5 px-2 py-2 text-left">
              <span aria-hidden
                className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-[8px] border border-line bg-surface text-muted">
                <Icon name="file" size={14}/>
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[13px] font-medium text-fg" title={doc.name}>{doc.name}</span>
                {doc.description && (
                  <span className="block truncate text-[11.5px] leading-4 text-muted" title={doc.description}>{doc.description}</span>
                )}
                <span className="mt-0.5 flex items-center gap-1.5">
                  <span className="rounded-full border border-line px-1.5 py-px font-mono text-[9px] uppercase tracking-wide text-muted">
                    {scopeBadge(doc)}
                  </span>
                  {doc.pageCount > 0 && (
                    <span className="font-mono text-[9px] text-muted">
                      {doc.pageCount} {doc.pageCount === 1 ? 'page' : 'pages'}
                    </span>
                  )}
                  <span aria-hidden title={indexStatusLabel(doc.indexStatus)}
                    className={'h-1.5 w-1.5 shrink-0 rounded-full ' + (doc.indexStatus === 'ready'
                      ? 'bg-[var(--success)]'
                      : doc.indexStatus === 'processing'
                        ? 'bg-[var(--warn)]'
                        : 'bg-[color-mix(in_oklab,var(--fg)_28%,transparent)]')}/>
                </span>
              </span>
              <Icon name="chevright" size={12} className="mt-2 shrink-0 text-muted"/>
            </button>
            {canInsert && (
              <button type="button" data-testid={'panel-doc-insert-' + doc.name}
                onClick={(e) => { e.stopPropagation(); insertDocument(doc); }}
                aria-label={'Insert ' + doc.name + ' as a mention'}
                title={'Insert ' + doc.name + ' as a mention'}
                className="mr-1 shrink-0 rounded-[5px] px-1.5 py-1 text-[11px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg">
                Insert
              </button>
            )}
          </div>
        ))}
      </div>
      {docs.length > 0 && (
        <p className="px-2 pb-1 pt-2 text-[11px] leading-4 text-muted">
          Click a document to preview it here.{canInsert && ' Insert mentions it in the composer.'}
        </p>
      )}
    </div>
  );
}
