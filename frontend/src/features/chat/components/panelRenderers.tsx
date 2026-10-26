/**
 * The right panel's per-kind content registry (moved from the old RightPanel
 * as part of merge-right-bar-previews): each tab kind maps to one renderer.
 * Sources: the agent's three prompt documents (via the existing
 * `getPromptDocuments` binding — design D8: persona docs keep using
 * GetPromptDocuments), one workspace file through the jailed
 * `workspaceFile` reader (with a Preview|Raw markdown toggle, design D6),
 * one workspace directory listing through `listWorkspace`, a delegated
 * task's trajectory (designs D3–D5), read through the same jailed reader at
 * the task's recorded transcript path, and one knowledge document
 * (add-knowledge-corpus) through the knowledge-scoped `content` reader. A
 * path the jail refuses renders the refused outcome, never file content.
 * Fetches run only while a tab shows.
 */
import { useEffect, useRef, useState } from "react";
import type { ReactElement } from "react";
import { File as FileIcon, Folder } from "lucide-react";

import type { ToolCallMessagePartProps } from "@assistant-ui/react";
import { MarkdownBody } from "@/components/assistant-ui/elements/MarkdownBody";
import {
  ReasoningContent,
  ReasoningRoot,
  ReasoningText,
  ReasoningTrigger,
} from "@/components/assistant-ui/elements/reasoning.aui";
import { ThinkingIndicator } from "@/components/assistant-ui/elements/thinking-indicator";
import type { PromptDocuments } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { WorkspaceEntry } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { ToolStatus } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { cn } from "../../../lib/cn";
import { client } from "../../../lib/client";
import { toolRenderers } from "../tools/aui";
import { ToolCard } from "../tools/aui/ToolCallFrame";
import { FallbackBody } from "../tools/aui/fallbackBody";
import { parseArgs } from "../tools/parse";
import { useChatStore } from "../store";
import type { ChatStore, Entry, PanelTab, PromptDocKey } from "../types";
import {
  parseTrajectory,
  trajectoryBlocks,
  type TrajectoryBlock,
} from "./trajectoryModel";

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

/** Builds a delegated task's trajectory preview tab (design D3): the
 * identity pins agent + task so re-activating the run-card row reuses
 * the open tab. */
export function trajectoryTab(
  agentID: string,
  taskID: string,
  transcriptPath: string,
  title: string
): Omit<PanelTab, "id"> {
  return {
    kind: "trajectory",
    identity: `trajectory:${agentID}:${taskID}`,
    title,
    data: { agentID, taskID, transcriptPath },
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

/** True when a file name previews as markdown (design D6): `.md` or
 * `.markdown`, case-insensitive. */
function isMarkdownName(name: string): boolean {
  return /\.(md|markdown)$/i.test(name);
}

/** One workspace file (or subagent trajectory file) as read-only mono text,
 * fetched through the jailed `workspaceFile` reader when the tab shows.
 * Markdown files (design D6) grow a Preview|Raw segmented toggle above the
 * body — Raw stays the default; the toggle state is local and resets per
 * mount. */
function WorkspaceFilePreview({ tab }: { tab: PanelTab }): ReactElement | null {
  const [state, setState] = useState<FilePreviewState>({
    loading: true,
    content: null,
    refused: false,
    error: null,
  });

  const [preview, setPreview] = useState(false);

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
        <div>
          {isMarkdownName(tab.data.name) ? (
            <div
              className="mb-2 inline-flex items-center gap-0.5 rounded-md border border-line p-0.5"
              data-testid="panel-preview-modes"
            >
              <button
                type="button"
                className={cn(
                  "cursor-pointer rounded px-1.5 py-0.5 text-[11px]",
                  preview
                    ? "text-muted"
                    : "bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] text-fg"
                )}
                data-testid="panel-preview-mode-raw"
                onClick={() => setPreview(false)}
              >
                Raw
              </button>
              <button
                type="button"
                className={cn(
                  "cursor-pointer rounded px-1.5 py-0.5 text-[11px]",
                  preview
                    ? "bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] text-fg"
                    : "text-muted"
                )}
                data-testid="panel-preview-mode-preview"
                onClick={() => setPreview(true)}
              >
                Preview
              </button>
            </div>
          ) : null}
          {preview ? (
            <div data-testid="panel-preview-markdown">
              <MarkdownBody text={state.content} />
            </div>
          ) : (
            <pre
              className="whitespace-pre-wrap break-words font-mono text-xs text-fg2"
              data-testid="panel-preview-content"
            >
              {state.content}
            </pre>
          )}
        </div>
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

/** How often a running task's open trajectory tab refetches the growing
 * transcript file (design D5). The transcript appends complete records as
 * the child works, so a half-second poll streams each step in with the same
 * felt cadence as the main transcript's deltas. */
const TRAJECTORY_POLL_MS = 500;

/** The scroll distance from the bottom inside which the running trajectory
 * keeps following the stream (the transcript's follow-pin grammar). */
const TRAJECTORY_FOLLOW_SLACK_PX = 120;

/** The narrow type guard the fallback body reads results through (the
 * part's result is delivered as the persisted string) — the transcript's
 * own renderers share this shape. */
function isTextResult(value: unknown): value is string {
  return typeof value === "string";
}

/** One synthetic tool-call part fed to the transcript's prop-driven
 * renderers (design D4): the paired call/result record projected onto the
 * shape the tool-card anatomy reads. The renderers read only the data
 * fields — the runtime callbacks (addResult/resume/approval) are unreachable
 * outside the message context, so they are stubbed to satisfy the part
 * contract without a runtime behind the part. */
function trajectoryToolPart(
  block: Extract<TrajectoryBlock, { kind: "tool" }>,
  running: boolean
): ToolCallMessagePartProps {
  return {
    type: "tool-call",
    toolCallId: `traj-${block.id}`,
    toolName: block.toolName,
    argsText: block.argsText,
    args: parseArgs(block.argsText) ?? {},
    result: block.result,
    isError: false,
    status:
      block.pending && running ? { type: "running" } : { type: "complete" },
    addResult: (): void => undefined,
    resume: (): void => undefined,
    respondToApproval: (): Promise<never> =>
      Promise.reject(new Error("trajectory parts carry no runtime approvals")),
  };
}

/** One tool block as the transcript renders it (design D4): the paired
 * call/result through the tool's own shaped renderer — the same cards the
 * main chat mounts, with the running card's live state while the result is
 * still open. Unknown tools (MCP, unrecognized names) render through the
 * card shell directly: the vendored fallback reads root-scope runtime state
 * that does not exist outside the message context, while the shell plus its
 * fallback body carry the same anatomy and degrade to the delivered strings. */
function TrajectoryToolView({
  block,
  running,
}: {
  block: Extract<TrajectoryBlock, { kind: "tool" }>;
  running: boolean;
}): ReactElement {
  const part = trajectoryToolPart(block, running);
  const Renderer = toolRenderers[block.toolName];

  return (
    <div className="my-2" data-testid="trajectory-step-tool">
      {Renderer ? (
        <Renderer {...part} />
      ) : (
        <ToolCard part={part}>
          <FallbackBody
            argsText={part.argsText}
            resultText={isTextResult(part.result) ? part.result : ""}
            isError={false}
            testidPrefix={`tool-fallback-${part.toolCallId}`}
          />
        </ToolCard>
      )}
    </div>
  );
}

/** One trajectory block rendered as its transcript element (design D4): the
 * task prompt as a user-style message, text as markdown, reasoning as the
 * transcript's quiet ghost disclosure, and tool activity through the
 * transcript's tool cards. The final `result` record is not rendered — the
 * parent transcript relays the child's answer. */
function TrajectoryBlockView({
  block,
  running,
  isLast,
}: {
  block: TrajectoryBlock;
  running: boolean;
  isLast: boolean;
}): ReactElement {
  switch (block.kind) {
    case "task":
      // The delegated prompt mirrors the thread's user message: a
      // right-aligned tinted bubble with a muted label (design D4).
      return (
        <div className="flex justify-end" data-testid="trajectory-step-task">
          <div className="max-w-[85%]">
            <p className="mb-1 text-[11px] text-muted">Task</p>
            <div className="rounded-xl bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] px-4 py-2 text-sm leading-relaxed whitespace-pre-wrap break-words text-fg">
              {block.content}
            </div>
          </div>
        </div>
      );
    case "text":
      return (
        <div data-testid="trajectory-step-text">
          <MarkdownBody text={block.content} />
        </div>
      );
    case "reasoning":
      // The transcript's own composition (the group-reasoning case): the
      // ghost variant keeps it a quiet disclosure line, not a panel; the
      // last block of a running task carries the live streaming state.
      return (
        <ReasoningRoot
          variant="ghost"
          streaming={isLast && running}
          className="my-2"
          data-testid="trajectory-step-reasoning"
        >
          <ReasoningTrigger
            active={isLast && running}
            className="max-w-none gap-1.5 py-0.5 text-[13px] text-muted-foreground/90 hover:text-foreground"
          />
          <ReasoningContent aria-busy={isLast && running}>
            <ReasoningText className="mt-1 max-h-80 ps-3 text-[13px] leading-relaxed text-muted-foreground/80">
              <MarkdownBody text={block.content} />
            </ReasoningText>
          </ReasoningContent>
        </ReasoningRoot>
      );
    case "tool":
      return <TrajectoryToolView block={block} running={running} />;
  }
}

/** The parsed blocks in order (design D4). A transcript whose every line is
 * blank, malformed, or unknown renders as empty. */
function TrajectoryTimeline({
  raw,
  running,
}: {
  raw: string;
  running: boolean;
}): ReactElement | null {
  const blocks = trajectoryBlocks(parseTrajectory(raw));

  if (blocks.length === 0) {
    return (
      <p className="text-sm text-muted" data-testid="panel-preview-empty">
        Empty transcript
      </p>
    );
  }

  return (
    <div className="space-y-3">
      {blocks.map((block: TrajectoryBlock, index: number) => (
        <TrajectoryBlockView
          key={block.id}
          block={block}
          running={running}
          isLast={index === blocks.length - 1}
        />
      ))}
    </div>
  );
}

/** One delegated task's trajectory (designs D3–D5): the child's recorded
 * transcript fetched through the jailed `workspaceFile` reader at the
 * task's materialized path. While the store still holds the task as
 * running, the tab refetches every 500ms and re-parses the whole file
 * (records only append, so the re-parse is idempotent), streams new blocks
 * in with the thread's live tail, and keeps the view pinned to the bottom
 * unless the reader has scrolled away; a terminal task fetches once on
 * open. */
function TrajectoryPreview({ tab }: { tab: PanelTab }): ReactElement | null {
  const [state, setState] = useState<FilePreviewState>({
    loading: true,
    content: null,
    refused: false,
    error: null,
  });

  // Live growth (design D5): liveness and the child's latest activity both
  // derive from the store's task entries, not a prop. The id is extracted
  // ahead of the selector because hooks run before the render's kind gate
  // narrows `tab`.
  const taskID = tab.kind === "trajectory" ? tab.data.taskID : "";

  const task = useChatStore((s: ChatStore) => {
    const entry = s.entries.find(
      (e: Entry) =>
        e.kind === "task" &&
        e.task !== undefined &&
        e.task !== null &&
        e.task.task_id === taskID
    );

    return entry !== undefined && entry.kind === "task"
      ? (entry.task ?? null)
      : null;
  });

  const running = task?.status === ToolStatus.ToolStatusRunning;

  // The follow pin (design D5): while the task runs, newly appended blocks
  // keep the view pinned to the bottom — always on the first stream, then
  // only while the reader sits near the bottom, so scrolling up to inspect
  // earlier steps wins over the pin until they scroll back down. The pin
  // rides the panel's own scroll container.
  const endRef = useRef<HTMLDivElement | null>(null);
  const followRef = useRef(true);

  useEffect(() => {
    if (!running) return;

    const end = endRef.current;

    if (!end) return;

    const scroller = end.closest<HTMLDivElement>(
      '[data-testid="panel-content"]'
    );

    if (!scroller) return;

    const distance =
      scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight;

    if (!followRef.current || distance > TRAJECTORY_FOLLOW_SLACK_PX) {
      if (distance > TRAJECTORY_FOLLOW_SLACK_PX) followRef.current = false;

      return;
    }

    scroller.scrollTop = scroller.scrollHeight;
  }, [state.content, running]);

  useEffect(() => {
    if (tab.kind !== "trajectory") return;

    let mounted = true;
    let timer: number | null = null;

    const fetchOnce = (): void => {
      client.chat
        .workspaceFile(tab.data.agentID, tab.data.transcriptPath)
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
    };

    fetchOnce();

    // The interval clears on unmount and whenever `running` flips false —
    // the effect then runs once more, landing the terminal state.
    if (running) {
      timer = window.setInterval(fetchOnce, TRAJECTORY_POLL_MS);
    }

    return (): void => {
      mounted = false;

      if (timer !== null) window.clearInterval(timer);
    };
  }, [tab, running]);

  if (tab.kind !== "trajectory") return null;

  const progress = task?.progress?.trim() ?? "";

  return (
    <div data-testid="panel-trajectory">
      {state.loading ? (
        <p className="text-sm text-muted" data-testid="panel-preview-loading">
          Loading…
        </p>
      ) : state.refused ? (
        <RefusedNotice relPath={tab.data.transcriptPath} />
      ) : state.error !== null ? (
        <p
          className="text-sm text-dangertext"
          data-testid="panel-preview-error"
        >
          {state.error}
        </p>
      ) : state.content ? (
        <>
          <TrajectoryTimeline raw={state.content} running={running} />
          {running ? (
            <div ref={endRef}>
              <ThinkingIndicator
                label={progress === "" ? "Working…" : progress}
                className="mt-3"
                data-testid="trajectory-tail"
              />
            </div>
          ) : (
            <div ref={endRef} />
          )}
        </>
      ) : (
        <p className="text-sm text-muted" data-testid="panel-preview-empty">
          Empty transcript
        </p>
      )}
    </div>
  );
}

/** True when a preview names a PDF document (add-knowledge-corpus): the
 * corpus gate admits only text and PDF, so the mime decides the renderer. */
function isPdfMime(mime: string): boolean {
  return mime === "application/pdf";
}

/** Decodes base64 bytes to UTF-8 text unicode-safely: atob yields a binary
 * string one byte per char, so the bytes round-trip through a Uint8Array
 * before the TextDecoder sees them (multi-byte characters survive). */
function decodeBase64ToText(base64: string): string {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);

  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);

  return new TextDecoder().decode(bytes);
}

interface KnowledgeDocState {
  loading: boolean;
  /** Decoded text of a text document (null for PDFs). */
  text: string | null;
  /** Raw base64 bytes of a PDF document (null for text). */
  pdfBase64: string | null;
  error: string | null;
}

/** One knowledge document (add-knowledge-corpus, spec "Knowledge document
 * opens preview"): the bytes fetched base64 through the knowledge-scoped
 * `content` reader when the tab shows — text renders as read-only mono
 * text, `.md`/`.markdown` names grow the workspace preview's Raw|Preview
 * toggle (raw stays the default, design D6 parity), and PDFs render as an
 * embedded document. Fetches run only while a tab shows. */
function KnowledgeDocPreview({ tab }: { tab: PanelTab }): ReactElement | null {
  const [state, setState] = useState<KnowledgeDocState>({
    loading: true,
    text: null,
    pdfBase64: null,
    error: null,
  });

  const [preview, setPreview] = useState(false);

  useEffect(() => {
    if (tab.kind !== "knowledge-doc") return;

    // The panel keys each renderer by tab id, so a mount is always a fresh
    // fetch: state only ever moves in the callbacks below, never inline.
    let mounted = true;
    client.knowledge
      .content(tab.data.docId)
      .then((base64: string): void => {
        if (!mounted) return;

        if (isPdfMime(tab.data.mime)) {
          setState({
            loading: false,
            text: null,
            pdfBase64: base64,
            error: null,
          });

          return;
        }

        setState({
          loading: false,
          text: decodeBase64ToText(base64),
          pdfBase64: null,
          error: null,
        });
      })
      .catch((err: Error): void => {
        if (!mounted) return;

        setState({
          loading: false,
          text: null,
          pdfBase64: null,
          error:
            err instanceof Error
              ? err.message
              : "Failed to read the knowledge document",
        });
      });

    return (): void => {
      mounted = false;
    };
  }, [tab]);

  if (tab.kind !== "knowledge-doc") return null;

  return (
    <div data-testid="panel-knowledge-doc">
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
      ) : state.pdfBase64 !== null ? (
        <object
          className="h-[60vh] w-full rounded-md border border-line"
          type="application/pdf"
          aria-label={tab.data.filename}
          data={`data:application/pdf;base64,${state.pdfBase64}`}
          data-testid="panel-knowledge-pdf"
        >
          <p className="text-sm text-muted">
            This view cannot display the PDF inline.
          </p>
        </object>
      ) : state.text !== null && state.text !== "" ? (
        <div>
          {isMarkdownName(tab.data.filename) ? (
            <div
              className="mb-2 inline-flex items-center gap-0.5 rounded-md border border-line p-0.5"
              data-testid="panel-preview-modes"
            >
              <button
                type="button"
                className={cn(
                  "cursor-pointer rounded px-1.5 py-0.5 text-[11px]",
                  preview
                    ? "text-muted"
                    : "bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] text-fg"
                )}
                data-testid="panel-preview-mode-raw"
                onClick={() => setPreview(false)}
              >
                Raw
              </button>
              <button
                type="button"
                className={cn(
                  "cursor-pointer rounded px-1.5 py-0.5 text-[11px]",
                  preview
                    ? "bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] text-fg"
                    : "text-muted"
                )}
                data-testid="panel-preview-mode-preview"
                onClick={() => setPreview(true)}
              >
                Preview
              </button>
            </div>
          ) : null}
          {preview ? (
            <div data-testid="panel-preview-markdown">
              <MarkdownBody text={state.text} />
            </div>
          ) : (
            <pre
              className="whitespace-pre-wrap break-words font-mono text-xs text-fg2"
              data-testid="panel-preview-content"
            >
              {state.text}
            </pre>
          )}
        </div>
      ) : (
        <p className="text-sm text-muted" data-testid="panel-preview-empty">
          Empty document
        </p>
      )}
    </div>
  );
}

/** Per-tab content registry: kind → renderer. */
export const PANEL_RENDERERS: Record<
  PanelTab["kind"],
  (props: { tab: PanelTab }) => ReactElement | null
> = {
  "prompt-doc": PromptDocPreview,
  "workspace-file": WorkspaceFilePreview,
  "workspace-dir": WorkspaceDirPreview,
  trajectory: TrajectoryPreview,
  "knowledge-doc": KnowledgeDocPreview,
};
