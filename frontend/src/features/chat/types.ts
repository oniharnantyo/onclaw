/**
 * Types for the chat surface: the transcript entry union the reducer mints,
 * the store's public state shape, and the props of each chat component.
 */
import type { ContextUsage } from "../../../bindings/github.com/oniharnantyo/buddies/internal/agents/models";
import type { Event as SessionEvent } from "../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type {
  Agent,
  Session,
  Skill,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  ModelCapability,
  ProviderView,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { KnowledgeDocument } from "../../lib/client";
import type { Unsubscribe } from "../../lib/transport";

export interface UserEntry {
  kind: "user";
  id: string;
  turnID?: string;
  text: string;
  at: string;
}

export interface AssistantEntry {
  kind: "assistant";
  id: string;
  turnID?: string;
  text: string;
  /** True while the turn is still appending deltas to this entry. */
  streaming: boolean;
  at: string;
}

export interface ReasoningEntry {
  kind: "reasoning";
  id: string;
  turnID?: string;
  text: string;
  streaming: boolean;
  at: string;
}

export interface ToolEntry {
  kind: "tool";
  id: string;
  turnID?: string;
  call: SessionEvent["tool"];
  at: string;
}

export interface TaskEntry {
  kind: "task";
  id: string;
  turnID?: string;
  task: SessionEvent["task"];
  at: string;
}

export interface CompactionEntry {
  kind: "compaction";
  id: string;
  compaction: SessionEvent["compaction"];
  at: string;
}

/**
 * The agent's task plan (the plantask tools' tasks) as of its latest
 * snapshot. A session holds at most one: each plan_activity event replaces
 * the previous entry in place. Never rendered in the transcript — the run
 * island reads it.
 */
export interface PlanEntry {
  kind: "plan";
  id: string;
  turnID?: string;
  plan: SessionEvent["plan"];
  at: string;
}

export interface ErrorEntry {
  kind: "error";
  id: string;
  turnID?: string;
  message: string;
  at: string;
}

export type Entry =
  | UserEntry
  | AssistantEntry
  | ReasoningEntry
  | ToolEntry
  | TaskEntry
  | PlanEntry
  | CompactionEntry
  | ErrorEntry;

/** One locally queued send, waiting for the in-flight turn to finish. */
export interface QueuedMessage {
  id: string;
  text: string;
}

/** Which workspace prompt document a preview tab shows (task 8.3). */
export type PromptDocKey = "identity" | "soul" | "bootstrap";

/** Data carried by a prompt-document preview tab. */
export interface PromptDocTabData {
  agentID: string;
  doc: PromptDocKey;
}

/** Data carried by a workspace file preview tab (task 8.3): the jailed
 * reader address plus the display name the tab and links show. */
export interface WorkspaceFileTabData {
  agentID: string;
  /** Workspace-relative path the jailed reader fetches; a path the jail
   * refuses renders the refused outcome. */
  relPath: string;
  /** Display name (the path's base name). */
  name: string;
}

/** Data carried by a workspace directory listing tab (task 8.3). */
export interface WorkspaceDirTabData {
  agentID: string;
  /** Workspace-relative directory path; "" lists the workspace root. */
  relPath: string;
}

/** Data carried by a delegated task's trajectory preview tab (design D3):
 * the jailed reader address of the child's materialized transcript. */
export interface TrajectoryTabData {
  agentID: string;
  taskID: string;
  /** Workspace-relative transcript path (".tasks/<tool-call-id>.output") the
   * jailed reader fetches; unresolvable paths render the read-error state. */
  transcriptPath: string;
}

/** Data carried by a knowledge document preview tab (add-knowledge-corpus,
 * spec chat-panels "Knowledge document opens preview"): the catalog id the
 * knowledge-scoped reader fetches plus the display name and mime the tab
 * renders from. */
export interface KnowledgeDocTabData {
  docId: string;
  filename: string;
  mime: string;
}

/**
 * One open right-panel tab (task 8.2). `kind` selects the renderer in the
 * panel's content registry; `identity` deduplicates re-opens of the same
 * content. Sources (task 8.3): prompt documents, workspace files (also
 * serving subagent trajectory files — those are workspace-relative paths in
 * `.tasks/`), workspace directory listings, delegated task trajectories
 * (design D3, spec merge-right-bar-previews), and knowledge documents
 * (add-knowledge-corpus, spec chat-panels).
 */
export type PanelTab =
  | {
      id: string;
      kind: "prompt-doc";
      identity: string;
      title: string;
      data: PromptDocTabData;
    }
  | {
      id: string;
      kind: "workspace-file";
      identity: string;
      title: string;
      data: WorkspaceFileTabData;
    }
  | {
      id: string;
      kind: "workspace-dir";
      identity: string;
      title: string;
      data: WorkspaceDirTabData;
    }
  | {
      id: string;
      kind: "trajectory";
      identity: string;
      title: string;
      data: TrajectoryTabData;
    }
  | {
      id: string;
      kind: "knowledge-doc";
      identity: string;
      title: string;
      data: KnowledgeDocTabData;
    };

/** Builds a knowledge-document preview tab (add-knowledge-corpus): the
 * knowledge panel and the Knowledge menu open documents through this; the
 * identity pins the catalog id, so re-activating the same document raises
 * the open tab instead of duplicating it. */
export function knowledgeDocTab(doc: KnowledgeDocument): Omit<PanelTab, "id"> {
  return {
    kind: "knowledge-doc",
    identity: `knowledge-doc:${doc.id}`,
    title: doc.filename,
    data: { docId: doc.id, filename: doc.filename, mime: doc.mime },
  };
}

export interface ChatStore {
  agents: Agent[];
  providers: ProviderView[];
  /** Enabled skills only — the composer's skill picker source. */
  skills: Skill[];

  activeAgentID: string | null;
  /** Active agent's sessions, latest activity first. */
  sessions: Session[];
  activeSessionID: string | null;

  /** The active agent's provider models with their capability data
   * (task 2.2) — the composer's model/effort pickers render from it. */
  models: ModelCapability[];
  /**
   * The open session's model override (design D1); "" inherits the agent's
   * configured model. Hydrated from the session row on select (task 2.1).
   */
  modelOverride: string;
  /** The open session's reasoning-effort override; "" inherits. */
  effortOverride: string;

  entries: Entry[];
  usage: ContextUsage | null;
  loadingSession: boolean;
  error: string | null;

  /** True from the moment a send starts until its turn reaches a terminal
   * state — the gate behind queueing. */
  busy: boolean;
  dispatching: boolean;
  activeTurnID: string | null;
  finishedTurnIDs: Set<string>;
  queue: QueuedMessage[];

  sidebarCollapsed: boolean;

  /** Open right-panel preview tabs (task 8.2); cleared when the session
   * switches so the panel reopens closed for the new session. */
  panelTabs: PanelTab[];

  /** The raised tab's id; null falls back to the last open tab. Lives in
   * the store because activations come from outside the bar too (a run-card
   * row click raises its trajectory tab). */
  activePanelTabID: string | null;

  /** Loads reference data and consumes the nav store's chat target. */
  init: () => Promise<void>;
  selectAgent: (agentID: string, sessionID?: string) => Promise<void>;
  selectSession: (sessionID: string) => Promise<void>;
  newSession: () => Promise<void>;
  /** Sends now, queues while a turn is in flight, or — with no open session
   * (task 3.4) — first creates the active agent's session, then sends. */
  send: (text: string) => Promise<void>;
  /**
   * Rewrites the open session's model override (task 2.1, design D1); ""
   * clears back to inherit. Persists through SetSessionOverrides, updates the
   * session row, and records the sticky per-agent default that seeds newly
   * created sessions (design D9). An effort the new model cannot express is
   * cleared (design D2).
   */
  setModelOverride: (model: string) => Promise<void>;
  /** Rewrites the open session's effort override ("" = clear to inherit). */
  setEffortOverride: (effort: string) => Promise<void>;
  /** Cancels the open session's in-flight turn (task 3.3). */
  stop: () => void;
  removeQueued: (id: string) => void;
  toggleSidebar: () => void;
  /** Opens a preview tab, raising it; an identical open tab is raised, not
   * duplicated. */
  openPanelTab: (tab: Omit<PanelTab, "id">) => void;
  /** Raises an already-open tab. */
  activatePanelTab: (id: string) => void;
  closePanelTab: (id: string) => void;
}

/** Which of the composer's three trigger-token menus is open. */
export type ActiveMenu =
  | { kind: "slash"; query: string; startPos: number }
  | { kind: "mention"; query: string; startPos: number }
  | { kind: "skill"; query: string; startPos: number }
  | null;

export interface MentionOption {
  id: string;
  name: string;
}

export interface SlashCommand {
  cmd: string;
  desc: string;
}

export interface SkillOption {
  name: string;
  description: string;
}

export interface EntryProps<E> {
  entry: E;
}

export interface TranscriptProps {
  entries: Entry[];
}

export interface ContextRingProps {
  used: number;
  limit: number;
}

export interface QueueStackProps {
  items: QueuedMessage[];
  running: boolean;
  agentName?: string;
  onRemove: (id: string) => void;
}

/** Props of the composer's three trigger-token menus. */
export interface MenuProps<O> {
  options: O[];
  /** Index of the keyboard-highlighted option. */
  idx: number;
  onPick: (option: O) => void;
}

export type { Unsubscribe };
