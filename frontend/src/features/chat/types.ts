/**
 * Types for the chat surface: the transcript entry union the reducer mints,
 * the store's public state shape, and the props of each chat component.
 */
import type { ContextUsage } from "../../../bindings/github.com/oniharnantyo/buddies/internal/agents/models";
import type { Event as SessionEvent } from "../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type {
  Agent,
  AgentGroup,
  AgentGroupMember,
  AgentGroupMessage,
  AgentGroupTask,
  Session,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  ModelCapability,
  ProviderView,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { KnowledgeDocument } from "../../lib/client";
import type { Unsubscribe } from "../../lib/transport";

/**
 * Who authored a group-conversation entry (add-agent-groups design D7): the
 * per-entry attribution the transcript renders instead of the session agent.
 * A member row resolves its avatar + name from `agentID`; a coordinator row
 * is a system line rendered without agent attribution. Ordinary-session
 * entries carry no author and keep the session-agent attribution.
 */
export interface EntryAuthor {
  kind: "member" | "coordinator";
  /** The member agent's id (avatar + name resolve from it); absent on
   * coordinator rows. */
  agentID?: string;
}

/** The group-task linkage a transcript row may carry (add-agent-groups
 * design D8): assignment and result rows (and member rows copied back while
 * a task was in flight) link the work; the task's state renders beside the
 * row and the task controls ride it. Absent on unlinked rows. */
export interface EntryGroupTaskRef {
  /** The linked group task's id (AgentGroupMessage.TaskID). */
  taskID: string;
}

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
  /** The files the turn produced under the workspace's artifacts/ directory
   * (add-turn-artifacts design D4): stamped from the turn's turn_completed
   * event when it carries a non-empty list; absent otherwise. */
  artifacts?: SessionEvent["artifacts"];
  /** True when the backend hit the artifact cap and the list is truncated
   * (add-turn-artifacts design D4); stamped beside artifacts. */
  artifactsTruncated?: boolean;
  /** The entry's group-conversation author (add-agent-groups design D7);
   * absent on ordinary-session entries, which attribute to the session
   * agent. */
  author?: EntryAuthor;
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
  /** The entry's group-conversation author (add-agent-groups design D7) —
   * a group error row is a coordinator line; absent on session entries. */
  author?: EntryAuthor;
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

/** One row of the composer's skill picker: the active agent's eligible skill
 * surface, merged from the two tiers the skill backend serves — "global" for
 * an enabled registry row that passes the agent's skill allowlist, "agent"
 * for the agent's own workspace skills, which are always available to it and
 * shadow a same-named global row. */
export interface PickerSkill {
  Name: string;
  Description: string;
  Tier: "global" | "agent";
}

export interface ChatStore {
  agents: Agent[];
  providers: ProviderView[];
  /** The active agent's eligible skills — the composer's skill picker
   * source and the context meter's skills count. */
  skills: PickerSkill[];

  activeAgentID: string | null;
  /** Every agent's sessions, latest activity first (add-grouped-session-rail
   * design D1); the rail groups the rows by each row's AgentID. */
  sessions: Session[];
  activeSessionID: string | null;

  /**
   * Agent groups (add-agent-groups tasks 5.1): the rail's groups section
   * renders this list; opening one switches the conversation surface to the
   * group transcript.
   */
  groups: AgentGroup[];
  /** The open group's id; null while an agent session is the surface. */
  activeGroupID: string | null;
  /** The open group's roster in add order — the mention picker's source and
   * the settings pane's member list. */
  groupMembers: AgentGroupMember[];
  /** The open group's raw transcript rows in transcript order; the rendered
   * projection rides `entries` (the shared Entry mapping), so one renderer
   * serves both surfaces. */
  groupMessages: AgentGroupMessage[];
  /** The open group's tasks — the transcript task rows resolve their state
   * and controls from it. */
  groupTasks: AgentGroupTask[];
  /** Groups with a member turn in flight (any running task) — the rail's
   * busy dots. */
  busyGroups: Set<string>;
  /** The member the group mention picker addressed (design D4); rides the
   * next group send, then clears. */
  groupRecipient: string | null;

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

  /** The raised tab's id. `null` is the pinned identity tab
   * (add-pinned-identity-tab, design D1): with no tabs open it is the
   * identity island, with tabs open it shows the identity view inside the
   * tab shell while every tab stays listed. Lives in the store because
   * activations come from outside the bar too (a run-card row click raises
   * its trajectory tab). */
  activePanelTabID: string | null;

  /** Loads reference data and consumes the nav store's chat target. */
  init: () => Promise<void>;
  /** Loads the agent and opens sessionID; without one, the agent's most
   * recent session — or the fresh-chat empty state when the agent has none.
   * An explicit null forces that fresh-chat state (no session picked or
   * created): the rail's + button, so a session is minted only by the first
   * send. */
  selectAgent: (agentID: string, sessionID?: string | null) => Promise<void>;
  selectSession: (sessionID: string) => Promise<void>;
  /**
   * Opens a group conversation (add-agent-groups design D9): loads the group,
   * its roster, transcript page, and tasks, subscribes to its live events,
   * and switches the surface — activeSessionID clears, so the composer
   * targets the group. Any agent-session selection clears it back.
   */
  openGroup: (groupID: string) => Promise<void>;
  /** Refetches the group list and the per-group busy states (design D9):
   * fired at boot and on every groups:changed. */
  refreshGroups: () => Promise<void>;
  /**
   * Creates a group with its initial roster (spec "Group entity and
   * membership", scenario "Create a group with members"): the binding writes
   * the row, then refreshGroups aligns the rail's list and per-group
   * subscriptions and openGroup opens the created group's conversation —
   * the new group opens as a conversation. Resolves the created row, or
   * null when the create failed — the failure rides the store's `error`
   * (the caller keeps its form and announces it).
   */
  createGroup: (
    name: string,
    title: string,
    instructions: string,
    memberAgentIDs: string[] | null
  ) => Promise<AgentGroup | null>;
  /** Refetches the open group's tasks (the live task-updated path and the
   * task controls' post-mutation refresh). */
  refreshGroupTasks: () => Promise<void>;
  /** Records (or clears) the mention picker's explicit recipient for the
   * next group send (design D4). */
  setGroupRecipient: (agentID: string | null) => void;
  /**
   * Creates a session for `agentID` — the active agent when omitted (the
   * composer's send-path auto-create calls it bare) — and opens it through
   * the same selectAgent path for same- and cross-agent creation
   * (add-grouped-session-rail design D6).
   */
  newSession: (agentID?: string) => Promise<void>;
  /**
   * Opens a fresh composer for `agentID` — the active agent when omitted —
   * WITHOUT creating a session row: the chat view clears to the empty
   * state and the send path mints the session on the first send. The rail's
   * + button, so pressing it never leaves an Untitled row behind.
   */
  newChat: (agentID?: string) => Promise<void>;
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
  /** Raises an already-open tab; `null` raises the pinned identity tab
   * (design D1). */
  activatePanelTab: (id: string | null) => void;
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
