/**
 * Right panel (task 8.2, spec chat-panels): a tabbed preview surface beside
 * the transcript. Wide widths dock it as a resizable column (persisted via
 * `buddies.chatPanelWidth`); below the sidebar's xl breakpoint it renders as
 * a full-height overlay sheet capped to the view. Pure view state over the
 * store's `panelTabs` — opening, closing, or resizing it touches nothing the
 * turn lifecycle owns, so it can never interrupt a streaming turn. Tabs are
 * reset by the store on session switch, and with no tabs open the panel
 * renders nothing (zero layout impact).
 *
 * Content registry (task 8.3): each tab kind maps to one renderer. Sources:
 * the agent's three prompt documents (via the existing `getPromptDocuments`
 * binding — design D8: persona docs keep using GetPromptDocuments), one
 * workspace file through the jailed `workspaceFile` reader (also serving
 * subagent trajectory files, which are workspace-relative `.tasks/` paths),
 * and one workspace directory listing through `listWorkspace`. A path the
 * jail refuses renders the refused outcome, never file content. Fetches run
 * only while a tab shows.
 */
import { useEffect, useRef, useState } from "react";
import type {
  CSSProperties,
  PointerEvent as ReactPointerEvent,
  ReactElement,
} from "react";
import { File as FileIcon, Folder, X } from "lucide-react";

import type { PromptDocuments } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { WorkspaceEntry } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { cn } from "../../../lib/cn";
import { client } from "../../../lib/client";
import { useLocalNumberPreference } from "../../../lib/useLocalPreference";
import { useChatStore } from "../store";
import type { ChatStore, PanelTab, PromptDocKey } from "../types";

const PANEL_WIDTH_KEY = "buddies.chatPanelWidth";

const DEFAULT_PANEL_WIDTH = 320;

const MIN_PANEL_WIDTH = 240;

const MAX_PANEL_WIDTH = 560;

/** Rail label + workspace file per prompt document (IdentitySection parity). */
const PROMPT_DOC_META: Record<PromptDocKey, { title: string }> = {
  identity: { title: "IDENTITY" },
  soul: { title: "SOUL" },
  bootstrap: { title: "BOOTSTRAP" },
};

/** Builds a prompt-document preview tab (task 8.3): the trigger wiring for
 * it lands with the panel-sources wave; the store action opens the panel. */
export function promptDocTab(
  agentID: string,
  doc: PromptDocKey
): Omit<PanelTab, "id"> {
  return {
    kind: "prompt-doc",
    identity: `prompt-doc:${agentID}:${doc}`,
    title: PROMPT_DOC_META[doc].title,
    data: { agentID, doc },
  };
}

/** Base name of a workspace path; the workspace root names as "Workspace". */
function baseName(relPath: string): string {
  const parts = relPath.split("/").filter(Boolean);

  return parts.length > 0 ? parts[parts.length - 1] : "Workspace";
}

/** Builds a workspace file preview tab (task 8.3): file links in tool cards
 * and subagent trajectory paths open through this; `relPath` is what the
 * jailed reader receives, so a foreign path opens to the refused outcome. */
export function workspaceFileTab(
  agentID: string,
  relPath: string
): Omit<PanelTab, "id"> {
  return {
    kind: "workspace-file",
    identity: `workspace-file:${agentID}:${relPath}`,
    title: baseName(relPath),
    data: { agentID, relPath, name: baseName(relPath) },
  };
}

/** Builds a workspace directory listing tab (task 8.3); "" lists the
 * workspace root. */
export function workspaceDirTab(
  agentID: string,
  relPath: string
): Omit<PanelTab, "id"> {
  return {
    kind: "workspace-dir",
    identity: `workspace-dir:${agentID}:${relPath}`,
    title: relPath === "" ? "Workspace" : baseName(relPath),
    data: { agentID, relPath },
  };
}

function docContent(docs: PromptDocuments, doc: PromptDocKey): string {
  if (doc === "identity") return docs.Identity;

  if (doc === "soul") return docs.Soul;

  return docs.Bootstrap;
}

interface PromptDocState {
  loading: boolean;
  content: string | null;
  error: string | null;
}

/** One prompt document as read-only mono text; a missing document renders as
 * empty (the binding returns empty content with Exists=false). */
function PromptDocPreview({ tab }: { tab: PanelTab }): ReactElement | null {
  const [state, setState] = useState<PromptDocState>({
    loading: true,
    content: null,
    error: null,
  });

  useEffect(() => {
    if (tab.kind !== "prompt-doc") return;

    // The panel keys each renderer by tab id, so a mount is always a fresh
    // fetch: state only ever moves in the callbacks below, never inline.
    let mounted = true;
    client.agents
      .getPromptDocuments(tab.data.agentID)
      .then((docs: PromptDocuments): void => {
        if (mounted)
          setState({
            loading: false,
            content: docContent(docs, tab.data.doc),
            error: null,
          });
      })
      .catch((err: Error): void => {
        if (mounted)
          setState({
            loading: false,
            content: null,
            error:
              err instanceof Error
                ? err.message
                : "Failed to load prompt document",
          });
      });

    return (): void => {
      mounted = false;
    };
  }, [tab]);

  if (tab.kind !== "prompt-doc") return null;

  return (
    <div data-testid="panel-prompt-doc">
      {state.loading ? (
        <p className="text-sm text-muted" data-testid="panel-preview-loading">
          Loading…
        </p>
      ) : state.error !== null ? (
        <p
          className="text-sm text-dangertext"
          data-testid="panel-preview-error"
        >
          {state.error}
        </p>
      ) : state.content ? (
        <pre
          className="whitespace-pre-wrap break-words font-mono text-xs text-fg2"
          data-testid="panel-preview-content"
        >
          {state.content}
        </pre>
      ) : (
        <p className="text-sm text-muted" data-testid="panel-preview-empty">
          Empty document
        </p>
      )}
    </div>
  );
}

/** True when a workspace read failed because the jail refused the path (the
 * backend words every refusal "workspaces: refused …"), vs any other
 * failure. The refused outcome is its own preview state, per the spec. */
function isRefusal(message: string): boolean {
  return message.includes("refused");
}

/** The refused outcome (task 8.3, spec "Workspace reader stays jailed"): a
 * path outside the agent's workspace was refused and no content is shown. */
function RefusedNotice({ relPath }: { relPath: string }): ReactElement {
  return (
    <div data-testid="panel-preview-refused">
      <p className="text-sm font-medium text-dangertext">Refused</p>
      <p className="mt-1 text-xs text-muted">
        {`"${relPath}" is outside this agent's workspace — the read was refused.`}
      </p>
    </div>
  );
}

interface FilePreviewState {
  loading: boolean;
  content: string | null;
  refused: boolean;
  error: string | null;
}

/** One workspace file (or subagent trajectory file) as read-only mono text,
 * fetched through the jailed `workspaceFile` reader when the tab shows. */
function WorkspaceFilePreview({ tab }: { tab: PanelTab }): ReactElement | null {
  const [state, setState] = useState<FilePreviewState>({
    loading: true,
    content: null,
    refused: false,
    error: null,
  });

  useEffect(() => {
    if (tab.kind !== "workspace-file") return;

    // The panel keys each renderer by tab id, so a mount is always a fresh
    // fetch: state only ever moves in the callbacks below, never inline.
    let mounted = true;
    client.chat
      .workspaceFile(tab.data.agentID, tab.data.relPath)
      .then((content: string): void => {
        if (mounted)
          setState({ loading: false, content, refused: false, error: null });
      })
      .catch((err: Error): void => {
        if (!mounted) return;

        const message =
          err instanceof Error
            ? err.message
            : "Failed to read the workspace file";

        setState({
          loading: false,
          content: null,
          refused: isRefusal(message),
          error: message,
        });
      });

    return (): void => {
      mounted = false;
    };
  }, [tab]);

  if (tab.kind !== "workspace-file") return null;

  return (
    <div data-testid="panel-workspace-file">
      {state.loading ? (
        <p className="text-sm text-muted" data-testid="panel-preview-loading">
          Loading…
        </p>
      ) : state.refused ? (
        <RefusedNotice relPath={tab.data.relPath} />
      ) : state.error !== null ? (
        <p
          className="text-sm text-dangertext"
          data-testid="panel-preview-error"
        >
          {state.error}
        </p>
      ) : state.content ? (
        <pre
          className="whitespace-pre-wrap break-words font-mono text-xs text-fg2"
          data-testid="panel-preview-content"
        >
          {state.content}
        </pre>
      ) : (
        <p className="text-sm text-muted" data-testid="panel-preview-empty">
          Empty file
        </p>
      )}
    </div>
  );
}

interface DirPreviewState {
  loading: boolean;
  entries: WorkspaceEntry[];
  refused: boolean;
  error: string | null;
}

/** Joins a listing directory path with one child name. */
function joinRel(dir: string, name: string): string {
  return dir === "" ? name : `${dir.replace(/\/+$/, "")}/${name}`;
}

/** The parent of a workspace directory path; the root has none. */
function parentRel(dir: string): string {
  return dir.split("/").filter(Boolean).slice(0, -1).join("/");
}

/** One workspace directory listing (task 8.3): subdirectories navigate in
 * place, file entries open their own preview tab through the store's
 * `openPanelTab`. */
function WorkspaceDirPreview({ tab }: { tab: PanelTab }): ReactElement | null {
  const openPanelTab = useChatStore((s: ChatStore) => s.openPanelTab);

  // Navigating within the listing is pure local view state (task 8.3): the
  // tab keeps its identity, so re-opening it starts back at its own path.
  const [path, setPath] = useState(
    tab.kind === "workspace-dir" ? tab.data.relPath : ""
  );

  const [state, setState] = useState<DirPreviewState>({
    loading: true,
    entries: [],
    refused: false,
    error: null,
  });

  useEffect(() => {
    if (tab.kind !== "workspace-dir") return;

    let mounted = true;
    client.chat
      .listWorkspace(tab.data.agentID, path)
      .then((entries: WorkspaceEntry[]): void => {
        if (mounted)
          setState({ loading: false, entries, refused: false, error: null });
      })
      .catch((err: Error): void => {
        if (!mounted) return;

        const message =
          err instanceof Error
            ? err.message
            : "Failed to list the workspace directory";

        setState({
          loading: false,
          entries: [],
          refused: isRefusal(message),
          error: message,
        });
      });

    return (): void => {
      mounted = false;
    };
  }, [tab, path]);

  if (tab.kind !== "workspace-dir") return null;

  return (
    <div data-testid="panel-workspace-dir">
      <div className="flex items-center justify-between gap-2">
        <p
          className="min-w-0 truncate font-mono text-[11px] text-muted"
          data-testid="panel-dir-path"
        >
          {path === "" ? "/" : path}
        </p>
        {path !== "" ? (
          <button
            type="button"
            className="shrink-0 cursor-pointer rounded-md border border-line px-1.5 py-0.5 text-[11px] text-muted hover:text-fg"
            data-testid="panel-dir-up"
            onClick={() => setPath(parentRel(path))}
          >
            Up
          </button>
        ) : null}
      </div>
      {state.loading ? (
        <p
          className="mt-2 text-sm text-muted"
          data-testid="panel-preview-loading"
        >
          Loading…
        </p>
      ) : state.refused ? (
        <div className="mt-2">
          <RefusedNotice relPath={path} />
        </div>
      ) : state.error !== null ? (
        <p
          className="mt-2 text-sm text-dangertext"
          data-testid="panel-preview-error"
        >
          {state.error}
        </p>
      ) : state.entries.length === 0 ? (
        <p
          className="mt-2 text-sm text-muted"
          data-testid="panel-preview-empty"
        >
          Empty directory
        </p>
      ) : (
        <ul className="mt-2 space-y-0.5" data-testid="panel-dir-listing">
          {state.entries.map((entry: WorkspaceEntry) => (
            <li key={entry.name}>
              <button
                type="button"
                className="flex w-full cursor-pointer items-center gap-1.5 rounded-md px-1.5 py-1 text-left text-xs text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)]"
                data-testid={`panel-dir-entry-${entry.name}`}
                onClick={() => {
                  const child = joinRel(path, entry.name);

                  if (entry.dir) setPath(child);
                  else openPanelTab(workspaceFileTab(tab.data.agentID, child));
                }}
              >
                {entry.dir ? (
                  <Folder aria-hidden="true" className="size-3.5 shrink-0" />
                ) : (
                  <FileIcon aria-hidden="true" className="size-3.5 shrink-0" />
                )}
                <span className="truncate">{entry.name}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** Per-tab content registry: kind → renderer. */
const PANEL_RENDERERS: Record<
  PanelTab["kind"],
  (props: { tab: PanelTab }) => ReactElement | null
> = {
  "prompt-doc": PromptDocPreview,
  "workspace-file": WorkspaceFilePreview,
  "workspace-dir": WorkspaceDirPreview,
};

export function RightPanel(): ReactElement | null {
  const tabs = useChatStore((s: ChatStore) => s.panelTabs);
  const closePanelTab = useChatStore((s: ChatStore) => s.closePanelTab);

  const [width, setWidth] = useLocalNumberPreference(
    PANEL_WIDTH_KEY,
    DEFAULT_PANEL_WIDTH
  );

  const [activeID, setActiveID] = useState<string | null>(null);

  const asideRef = useRef<HTMLElement | null>(null);

  if (tabs.length === 0) return null;

  const active =
    tabs.find((t: PanelTab) => t.id === activeID) ?? tabs[tabs.length - 1];

  const Renderer = PANEL_RENDERERS[active.kind];

  const onResizeStart = (e: ReactPointerEvent<HTMLDivElement>): void => {
    e.preventDefault();
    const aside = asideRef.current;

    if (!aside) return;

    const move = (ev: PointerEvent): void => {
      const right = aside.getBoundingClientRect().right;

      setWidth(
        Math.min(
          MAX_PANEL_WIDTH,
          Math.max(MIN_PANEL_WIDTH, Math.round(right - ev.clientX))
        )
      );
    };

    const stop = (): void => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
    };

    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop);
  };

  return (
    <aside
      ref={asideRef}
      className={cn(
        "absolute inset-y-0 right-0 z-30 flex w-[min(100%,var(--panel-width))] shrink-0 flex-col border-l border-line bg-surface",
        "shadow-[var(--elev-raised)] xl:relative xl:z-auto xl:w-[var(--panel-width)] xl:shadow-none"
      )}
      style={
        // SAFETY: the panel reads its width through the CSS custom property
        // in the width classes; React's style typing does not model
        // custom properties, so the record is asserted once here.
        { "--panel-width": `${width}px` } as CSSProperties
      }
      data-testid="chat-right-panel"
    >
      <div
        className="flex items-center gap-1 overflow-x-auto border-b border-line p-1"
        data-testid="panel-tabs"
      >
        {tabs.map((tab: PanelTab) => (
          <div
            key={tab.id}
            className={cn(
              "flex min-w-0 items-center gap-1 rounded-md border px-1.5 py-1",
              tab.id === active.id
                ? "border-line bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] text-fg"
                : "border-transparent text-muted"
            )}
          >
            <button
              type="button"
              className="min-w-0 cursor-pointer truncate text-xs"
              data-testid={`panel-tab-${tab.id}`}
              onClick={() => setActiveID(tab.id)}
            >
              {tab.title}
            </button>
            <button
              type="button"
              className="flex shrink-0 cursor-pointer items-center rounded-full p-0.5 text-muted hover:text-fg"
              aria-label={`Close ${tab.title}`}
              title={`Close ${tab.title}`}
              data-testid={`panel-tab-close-${tab.id}`}
              onClick={() => closePanelTab(tab.id)}
            >
              <X aria-hidden="true" className="size-3" />
            </button>
          </div>
        ))}
      </div>
      <div
        className="od-scroll min-h-0 flex-1 overflow-y-auto p-3"
        data-testid="panel-content"
      >
        <Renderer key={active.id} tab={active} />
      </div>
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize panel"
        onPointerDown={onResizeStart}
        className="absolute inset-y-0 left-0 z-10 hidden w-1.5 cursor-col-resize hover:bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] xl:block"
        data-testid="panel-resize"
      />
    </aside>
  );
}
