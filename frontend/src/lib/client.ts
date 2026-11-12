/**
 * Typed client wrapper over the generated Wails bindings (task 8.3, design
 * D1): the single surface UI code imports for backend calls and event
 * subscriptions. All calls route through an injectable Transport so tests
 * can mock the backend without the Wails runtime.
 *
 * Streaming (design D7): text/reasoning deltas are ordinary session events
 * (kind text_delta / reasoning_delta) on the session event channel, so no
 * separate Stream transport helper is needed for the base app.
 */
import type { ContextUsage } from "../../bindings/github.com/oniharnantyo/buddies/internal/agents/models";
import type {
  Preview,
  ScheduleInput,
  ScheduleView,
} from "../../bindings/github.com/oniharnantyo/buddies/internal/scheduler/models";
import type {
  AgentHookAssignment,
  AttachmentContent,
  BrowserConfig,
  BrowserProfile,
  BrowserScreenshotContent,
  ChannelChangedEvent,
  ChannelChatView,
  ChannelInput,
  ChannelStateEvent,
  ChannelView,
  ChromeProfileCandidate,
  ComputerConfigView,
  ComputerDriverState,
  ComputerPermission,
  ComputerStatus,
  CreateAgentInput,
  DocumentPresetEntry,
  GatewayStateEvent,
  GeneratedDocumentContent,
  GroupEvent,
  GroupsChangedEvent,
  IntegrationConnectInput,
  KnowledgeDocument,
  MCPServerView,
  Metadata,
  ModelCapability,
  PromptDocuments,
  ProviderInput,
  ProviderModelInput,
  ProviderModelView,
  ProviderView,
  ToolEntry,
  TurnStateResult,
  VaultLogin,
  VerifyResult,
  WebSearchChainEntryInput,
  WebSearchConfig,
  WebSearchConfigInput,
  WebSearchProviderInfo,
  WebhookDeliveryView,
  WebhookEndpointInput,
  WebhookEndpointView,
  WebhookListenerView,
  WebhookPresetView,
  WebhooksChangedEvent,
  WorkspaceEntry,
  WorkspaceFileBinaryContent,
  WorkspaceSkill,
} from "../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { SendRequest as GroupSendRequest } from "../../bindings/github.com/oniharnantyo/buddies/internal/groups/models";
import type {
  Agent,
  AgentGroup,
  AgentGroupMember,
  AgentGroupMessage,
  AgentGroupTask,
  ChannelPairing,
  GatewaySettings,
  Hook,
  HookExecution,
  MCPServer,
  NotificationSettings,
  Session,
  Skill,
} from "../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  Attachment as SessionAttachment,
  Event as SessionEvent,
} from "../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";

import {
  CHANNELS_CHANGED_EVENT,
  CHANNELS_STATE_EVENT,
  GATEWAY_STATE_EVENT,
  GROUPS_CHANGED_EVENT,
  KNOWLEDGE_CHANGED_EVENT,
  MCP_OAUTH_COMPLETED_EVENT,
  NOTIFICATIONS_OPEN_EVENT,
  SCHEDULES_CHANGED_EVENT,
  SETTINGS_OPEN_EVENT,
  TURNS_CHANGED_EVENT,
  WEBHOOKS_CHANGED_EVENT,
  groupEventName,
  sessionEventName,
  type KnowledgeChangedEvent,
  type MCPOAuthCompletedEvent,
  type NotificationOpenEvent,
  type SchedulesChangedEvent,
  type TurnsChangedEvent,
} from "./events";
import {
  wailsTransport,
  type EventPayload,
  type Transport,
  type Unsubscribe,
} from "./transport";

/** Handler receiving one live session event. */
export type SessionEventHandler = (event: SessionEvent) => void;

/** Anything a liveness probe may resolve with (e.g. app Metadata). */
export type ProbeTarget = object;

/**
 * Drops the store-assigned timestamps from a create payload: fresh drafts
 * carry empty strings (the row type requires them), and Go's time.Time
 * fails to parse "" at the binding layer before the service — which assigns
 * real timestamps anyway — ever runs.
 */
function withoutTimestamps<T extends { CreatedAt: string; UpdatedAt: string }>(
  row: T
): T {
  const { CreatedAt: _createdAt, UpdatedAt: _updatedAt, ...rest } = row;

  // SAFETY: the two omitted keys are store-assigned on insert, never read.
  return rest as T;
}

/**
 * One browser screenshot served through the bound read (design D6, task
 * 1.3): base64 PNG bytes plus the dimensions the tool reported (the
 * thumbnail sizes from them so the transcript does not shift on arrival).
 * The generated binding's own type, re-exported so callers keep one import
 * surface.
 */
export type { BrowserScreenshotContent };

/**
 * One generated document served through the bound read (add-create-document
 * D7): base64 bytes plus the byte size, addressed by the plain file name the
 * create_document tool reported. The generated binding's own type,
 * re-exported so callers keep one import surface.
 */
export type { GeneratedDocumentContent };

/**
 * One workspace file's binary content served through the bound read
 * (add-turn-artifacts design D8): base64 bytes plus the byte size and a mime
 * picked by extension, addressed by the workspace-relative path under the
 * same jail as the text reader. The generated binding's own type,
 * re-exported so callers keep one import surface.
 */
export type { WorkspaceFileBinaryContent };

/**
 * One saved document preset (add-create-document D4), re-exported so feature
 * code imports one surface.
 */
export type { DocumentPresetEntry };

export type { ProviderModelInput, ProviderModelView };

export type { TurnStateResult };

/** The generated scheduler binding types (add-scheduler D13), re-exported so
 * feature code imports one surface. */
export type { Preview, ScheduleInput, ScheduleView };

/** The generated knowledge document view (add-knowledge-corpus D8),
 * re-exported so feature code imports one surface. */
export type { KnowledgeDocument };

/**
 * The web_search chain editor's types (fix-bridge-secret-redaction design
 * D5): the redacted read shape (WebSearchConfig/WebSearchChainEntry — key
 * set-state plus hint, never the key) and the write shape
 * (WebSearchConfigInput/WebSearchChainEntryInput — api_key stays
 * write-only), re-exported so feature code imports one surface.
 */
export type {
  WebSearchChainEntryInput,
  WebSearchConfig,
  WebSearchConfigInput,
  WebSearchProviderInfo,
};

/**
 * The Computer Use settings pane's bound types (add-computer-use tasks 3.1,
 * design D6): the toggle's read/write shape (ComputerConfigView), the pane's
 * status aggregate with its per-permission list and driver download state
 * (ComputerStatus/ComputerPermission/ComputerDriverState), re-exported so
 * feature code imports one surface. The status/driver vocabulary values ride
 * the fields as plain strings (the Go constants do not generate bindings).
 */
export type {
  ComputerConfigView,
  ComputerDriverState,
  ComputerPermission,
  ComputerStatus,
};

/** A document's scope: every agent, or one agent (add-knowledge-corpus D6). */
export type KnowledgeScope = "global" | "agent";

/** Handler receiving one knowledge corpus change notification (design D8). */
export type KnowledgeChangedHandler = (event: KnowledgeChangedEvent) => void;

/** Handler receiving one schedules change notification (design D10). */
export type SchedulesChangedHandler = (event: SchedulesChangedEvent) => void;

/**
 * Handler receiving one session's in-flight turn flip (add-grouped-session-
 * rail design D2).
 */
export type TurnsChangedHandler = (event: TurnsChangedEvent) => void;

/** Handler receiving one gateway state change. */
export type GatewayStateHandler = (event: GatewayStateEvent) => void;

/** Handler receiving one channel mutation notification (task 1.4). */
export type ChannelsChangedHandler = (event: ChannelChangedEvent) => void;

/** Handler receiving one channel connection-state update (design D10). */
export type ChannelsStateHandler = (event: ChannelStateEvent) => void;

/** Handler receiving one agent-group roster/list change (add-agent-groups
 * design D9). */
export type GroupsChangedHandler = (event: GroupsChangedEvent) => void;

/** Handler receiving one group's live transcript/task update (add-agent-groups
 * design D9). */
export type GroupEventHandler = (event: GroupEvent) => void;

/** Handler receiving one clicked-notification deep link (add-desktop-
 * notifications design D5): the payload names the session or group to open. */
export type NotificationOpenHandler = (event: NotificationOpenEvent) => void;

/** Handler receiving one webhook endpoint mutation (add-webhook-triggers
 * task 7.1): the payload names the endpoint that changed. */
export type WebhooksChangedHandler = (event: WebhooksChangedEvent) => void;

export interface Client {
  appInfo: {
    /** Application name and version. */
    get(): Promise<Metadata>;
  };
  agents: {
    list(): Promise<Agent[]>;
    get(id: string): Promise<Agent>;
    create(input: CreateAgentInput): Promise<Agent>;
    update(agent: Agent): Promise<Agent>;
    delete(id: string): Promise<void>;
    /**
     * Reads the agent's three workspace prompt documents; a missing file
     * comes back with empty content and Exists=false (the normal state of a
     * failed or never-generated agent).
     */
    getPromptDocuments(agentID: string): Promise<PromptDocuments>;
    /**
     * Rewrites one workspace prompt document; doc is the workspace file name
     * ("IDENTITY.md", "SOUL.md", or "BOOTSTRAP.md").
     */
    savePromptDocument(
      agentID: string,
      doc: string,
      content: string
    ): Promise<void>;
    /**
     * Regenerates the documents from the stored brief, with the optional
     * instruction riding along; rejected as a conflict while a generation is
     * already in flight.
     */
    regeneratePrompts(agentID: string, instruction: string): Promise<void>;
    /**
     * Lists the skills in the agent's own workspace skills directory (spec
     * agent-management "Agent workspace skills management"): one entry per
     * skill directory with the description parsed from its SKILL.md. Reads
     * only the workspace tier; an agent without skills lists empty.
     */
    listWorkspaceSkills(agentID: string): Promise<WorkspaceSkill[]>;
    /**
     * Copies one workspace skill into the global skills library and
     * registers it enabled; the workspace copy stays and keeps shadowing the
     * promoted skill. A name that already exists in the library rejects with
     * an error naming the collision and changes nothing.
     */
    promoteWorkspaceSkill(agentID: string, name: string): Promise<void>;
    /**
     * Removes one workspace-tier skill directory; the global library is
     * untouched. A nonexistent skill name rejects.
     */
    deleteWorkspaceSkill(agentID: string, name: string): Promise<void>;
  };
  chat: {
    sessions(agentID: string): Promise<Session[]>;
    /**
     * Every agent's sessions, most recent activity first (add-grouped-
     * session-rail design D1): the one listing the grouped rail renders
     * from; each row carries its AgentID for the grouping.
     */
    allSessions(): Promise<Session[]>;
    /**
     * The sessions with a turn in flight — running or parked on a pending
     * approval (add-grouped-session-rail design D2) — for the frontend's
     * boot-time and focus-time sync of the rail's running dots.
     */
    runningSessions(): Promise<string[]>;
    createSession(agentID: string): Promise<Session>;
    /**
     * Submits one user message and resolves with the turn's ID promptly
     * (D7). attachmentKeys names uploads made through saveAttachment; a
     * message may carry attachments and no text.
     */
    send(
      sessionID: string,
      message: string,
      attachmentKeys?: string[]
    ): Promise<string>;
    /**
     * Stops the session's in-flight turn (spec agent-runtime "Turn
     * cancellation"); also discards a turn parked on a pending approval. A
     * safe no-op when no turn is in flight.
     */
    cancel(sessionID: string): Promise<void>;
    /**
     * Regenerates the answer for a turn as a new variant (design D7): a new
     * real turn re-runs the original prompt and resolves with the new turn
     * id; its events carry the variant_of marker.
     */
    regenerate(sessionID: string, turnID: string): Promise<string>;
    /**
     * Submits the user's decision for the session's pending approval (spec
     * chat-approvals); fails with a conflict error for a stale interrupt id.
     */
    resolveApproval(
      sessionID: string,
      interruptID: string,
      approved: boolean
    ): Promise<void>;
    /**
     * Submits the user-typed secret for the session's pending browser
     * attention request; the value rides this call only, never the event
     * log (add-browser-login-handover design D3).
     */
    resolveInput(
      sessionID: string,
      interruptID: string,
      text: string,
      identifier: string,
      saveLogin: boolean
    ): Promise<void>;
    /**
     * Ends the session's pending browser secret request with the user's
     * cancel — the secret card's Cancel button. The parked submit_secret
     * call unblocks on the cancelled result and the receipt renders
     * "Cancelled".
     */
    cancelInput(sessionID: string, interruptID: string): Promise<void>;
    /** Approves the session's pending browser takeover (headed relaunch). */
    beginBrowserTakeover(sessionID: string, interruptID: string): Promise<void>;
    /** Ends the session's pending browser takeover (completed/cancelled). */
    resolveTakeover(
      sessionID: string,
      interruptID: string,
      outcome: string
    ): Promise<void>;
    transcript(sessionID: string): Promise<SessionEvent[]>;
    /**
     * One transcript window (design D6): the newest `limit` events strictly
     * before `beforeSeq` — or the newest page when beforeSeq is 0 — in
     * replay order, for "load earlier" history paging.
     */
    transcriptPage(
      sessionID: string,
      beforeSeq: number,
      limit: number
    ): Promise<SessionEvent[]>;
    /**
     * Removes the session and its transcript; refused while the session has
     * a turn in flight.
     */
    deleteSession(sessionID: string): Promise<void>;
    /**
     * Stores one upload (base64-encoded data) and returns its capability
     * metadata; over-cap or unsupported-type files reject with the reason.
     * sessionID folders the file under the attachments directory's
     * per-session folder (schema v19); an empty sessionID — the fresh-chat
     * composer, where the send path creates the session — keeps the flat
     * legacy layout. Reads resolve either from the key alone.
     */
    saveAttachment(
      sessionID: string,
      name: string,
      mime: string,
      data: string
    ): Promise<SessionAttachment>;
    /** Serves one capability key's metadata and base64 content. */
    attachmentContent(key: string): Promise<AttachmentContent>;
    /**
     * Serves one browser screenshot's bytes by the file name the browser
     * tool wrote (design D6, task 1.3): base64 PNG plus the dimensions the
     * tool reported. The backend validates the name shape and resolves it
     * solely inside the calling session's workspace screenshots directory; a
     * refused or missing file rejects.
     */
    browserScreenshot(
      sessionID: string,
      name: string
    ): Promise<BrowserScreenshotContent>;
    /**
     * Serves one computer-use observation screenshot's bytes by the file name
     * the observation wrote (add-computer-use, spec chat-tool-cards): base64
     * PNG plus the dimensions the observation reported, the browser read's
     * same content shape. The backend validates the name shape
     * (`^\d+\.png$`) and resolves it solely inside the calling session's
     * computer screenshots directory; a refused or missing file rejects.
     */
    computerScreenshot(
      sessionID: string,
      name: string
    ): Promise<BrowserScreenshotContent>;
    /**
     * Serves one generated document's bytes by the file name the
     * create_document tool reported (add-create-document D7): base64 plus
     * the byte size. The backend refuses any name carrying directory
     * components — and any non-conforming session id — before touching the
     * filesystem, and resolves the name newest-home-first inside the
     * agent's workspace: the calling session's artifacts directory
     * (sessions/<id>/artifacts), then the legacy artifacts/, then the
     * pre-artifacts workspace root; a refused or missing file rejects.
     */
    generatedDocument(
      agentID: string,
      sessionID: string,
      name: string
    ): Promise<GeneratedDocumentContent>;
    /**
     * Exports one generated document through the host's native save dialog
     * (add-artifact-save-as design D4); resolves with the user-chosen
     * destination path. sessionID locates the generation's session-scoped
     * artifacts home exactly like generatedDocument. Rejects with
     * "save as: cancelled" when the user cancels the dialog (the UI stays
     * silent) and with "save as: no native dialog on this host" where the
     * host has no dialog service.
     */
    saveGeneratedDocumentAs(
      agentID: string,
      sessionID: string,
      name: string
    ): Promise<string>;
    /** Reads one jailed workspace file of the agent (design D8). */
    workspaceFile(agentID: string, relPath: string): Promise<string>;
    /**
     * Serves one jailed workspace file's binary content (add-turn-artifacts
     * design D8): base64 bytes plus the byte size and a mime picked by
     * extension (`application/pdf` for `.pdf`, otherwise
     * `application/octet-stream`). The jail matches `workspaceFile` exactly.
     */
    workspaceFileBinary(
      agentID: string,
      relPath: string
    ): Promise<WorkspaceFileBinaryContent>;
    /** Lists one jailed workspace directory of the agent (design D8). */
    listWorkspace(agentID: string, relPath: string): Promise<WorkspaceEntry[]>;
    /**
     * Attaches to the session's live event channel and invokes the handler
     * for each event (deltas, tool activity, turn completion, errors).
     * Resolves once the backend subscription is in place; the returned
     * function detaches the local listener only (use unsubscribe() to
     * detach on the backend).
     */
    subscribe(
      sessionID: string,
      handler: SessionEventHandler
    ): Promise<Unsubscribe>;
    /** Detaches the session's live event stream on the backend. */
    unsubscribe(sessionID: string): Promise<void>;
    /**
     * Re-fires the session's composite in-flight snapshot to every live
     * subscriber (enhance-resumable-streams design D5): the resync triggers'
     * bound call (design D6). Idle or unknown sessions are a safe no-op.
     */
    resync(sessionID: string): Promise<void>;
    /** Reports whether the session has a turn in flight and its turn ID. */
    turnState(sessionID: string): Promise<TurnStateResult>;
    /** Context-window fill state (used vs the model's limit) for the session. */
    contextUsage(sessionID: string): Promise<ContextUsage>;
    /**
     * Rewrites the session's model and reasoning-effort overrides (design
     * D1); an empty value clears that override back to inherit. Applies from
     * the next send onward.
     */
    setSessionOverrides(
      sessionID: string,
      model: string,
      effort: string
    ): Promise<void>;
  };
  settings: {
    providers(): Promise<ProviderView[]>;
    provider(id: string): Promise<ProviderView>;
    /**
     * Stores a new provider from the write-only ProviderInput (the store
     * assigns ID and timestamps); the response is the secret-free
     * ProviderView.
     */
    createProvider(input: ProviderInput): Promise<ProviderView>;
    /**
     * Rewrites an existing provider, addressed by id. A blank APIKey
     * preserves the stored key (design D2: the edit form resubmits only what
     * changed); a non-blank key replaces it. The response is the secret-free
     * ProviderView.
     */
    updateProvider(id: string, input: ProviderInput): Promise<ProviderView>;
    deleteProvider(id: string): Promise<void>;
    /** The models available for the provider with their capability data. */
    providerModels(id: string): Promise<ModelCapability[]>;
    /** Stored model rows (both enabled and disabled) for the provider configuration UI. */
    listProviderModels(providerID: string): Promise<ProviderModelView[]>;
    createModel(
      providerID: string,
      input: ProviderModelInput
    ): Promise<ProviderModelView>;
    updateModel(
      id: number,
      input: ProviderModelInput
    ): Promise<ProviderModelView>;
    deleteModel(id: number): Promise<void>;
    toggleModel(id: number, enabled: boolean): Promise<ProviderModelView>;
    /**
     * Verifies unsaved form values against the provider endpoint without
     * persisting anything. When providerID is set and the input key is blank
     * — an edit that leaves the key field untouched — the stored key is
     * verified instead (design D2). Verification failures land in the result,
     * not in a rejected promise.
     */
    verifyProviderDraft(
      input: ProviderInput,
      providerID: string
    ): Promise<VerifyResult>;
    /**
     * Verifies a saved provider's stored credential against its endpoint
     * without modifying it. Verification failures land in the result, not in
     * a rejected promise.
     */
    verifyProvider(id: string): Promise<VerifyResult>;
    skills(): Promise<Skill[]>;
    setSkillEnabled(name: string, enabled: boolean): Promise<void>;
    tools(): Promise<ToolEntry[]>;
    setToolEnabled(name: string, enabled: boolean): Promise<void>;
    /** The registered web_search providers with their credential kinds. */
    webSearchProviders(): Promise<WebSearchProviderInfo[]>;
    /**
     * The persisted web_search failover chain (design D8). The redacted read
     * shape: each entry carries KeySet/KeyHint, never the key itself.
     */
    webSearchConfig(): Promise<WebSearchConfig>;
    /**
     * Create-or-replaces the web_search failover chain. Rejects when an
     * entry's provider is unknown or its required credential is missing. A
     * blank api_key on an entry carrying its stable ID keeps the stored key
     * backend-side (merge-by-ID, fix-bridge-secret-redaction design D4); a
     * non-blank key replaces it.
     */
    saveWebSearchConfig(config: WebSearchConfigInput): Promise<void>;
    /** The persisted browser tool configuration (spec add-browser-tool D9). */
    browserConfig(): Promise<BrowserConfig>;
    /** Stores the browser tool configuration; validates name and timeouts. */
    saveBrowserConfig(config: BrowserConfig): Promise<void>;
    /**
     * The Computer Use pane's surface (add-computer-use tasks 3.1, design
     * D6): the config round trip, the status aggregate (available flag,
     * per-permission grants with Manage deep links, driver download state,
     * supervision status), the Check again re-probe (restarting the driver
     * on a grant change), and the enable action's host-side prompt raise.
     * Server builds answer computerStatus with available=false and refuse
     * the save/prompt (design D9).
     */
    computerConfig(): Promise<ComputerConfigView>;
    saveComputerConfig(view: ComputerConfigView): Promise<void>;
    computerStatus(): Promise<ComputerStatus>;
    checkComputerPermissions(): Promise<ComputerStatus>;
    promptComputerPermissions(): Promise<void>;
    openComputerManageURL(url: string): Promise<void>;
    /**
     * The browser profiles sorted by name; always includes "default", even
     * before its directory exists.
     */
    browserProfiles(): Promise<BrowserProfile[]>;
    /** Creates a profile's directory; rejects for an invalid or taken name. */
    createBrowserProfile(name: string): Promise<void>;
    /**
     * Removes a profile (its running browser closes first); refuses
     * "default" and resets the configured default profile when it was the
     * deleted one.
     */
    deleteBrowserProfile(name: string): Promise<void>;
    /**
     * Copies an existing Chrome/Chromium user-data directory into the
     * browser profiles registry; on a name collision the backend appends a
     * suffix and returns the profile's final registry name.
     */
    importBrowserProfile(sourcePath: string, name: string): Promise<string>;
    /**
     * Imports one detected Chrome profile in one click; the source resolves
     * from the directory name against this machine's Chrome installation.
     */
    importChromeProfile(dirName: string): Promise<string>;
    /** Lists this machine's Google Chrome profiles for one-click import. */
    detectChromeProfiles(): Promise<ChromeProfileCandidate[]>;
    /** Saved logins, redacted — the password never crosses the bridge. */
    vaultLogins(): Promise<VaultLogin[]>;
    /**
     * Stores (or updates, for a known origin+username pair) one login's
     * password in the OS-sealed vault.
     */
    saveVaultLogin(
      name: string,
      origin: string,
      username: string,
      password: string,
      addedBy: string
    ): Promise<void>;
    /** Removes one saved login; an unknown id is an error. */
    removeVaultLogin(id: string): Promise<void>;
    /** Reveals one saved login's password (settings-only affordance). */
    revealVaultLogin(id: string): Promise<string>;
    /** The format's document presets, ordered by name (add-create-document D4). */
    documentPresets(format: string): Promise<DocumentPresetEntry[]>;
    /**
     * Stores a new document preset for the format; a name colliding with
     * another preset of the format (case-insensitively) rejects.
     */
    createDocumentPreset(
      format: string,
      name: string,
      configJSON: string
    ): Promise<DocumentPresetEntry>;
    /** Rewrites a preset's name and config; its format is preserved. */
    updateDocumentPreset(
      id: string,
      name: string,
      configJSON: string
    ): Promise<DocumentPresetEntry>;
    /** Copies a preset to a new name within its format, config included. */
    duplicateDocumentPreset(
      id: string,
      newName: string
    ): Promise<DocumentPresetEntry>;
    /**
     * Removes the preset. A stored default naming it dangles by design —
     * the tool resolves an unresolvable default to "no default".
     */
    deleteDocumentPreset(id: string): Promise<void>;
    /** The format's default preset name; empty when none is set. */
    defaultDocumentPreset(format: string): Promise<string>;
    /** Stores the default preset name (canonicalized); empty clears it. */
    setDefaultDocumentPreset(format: string, name: string): Promise<void>;
    /**
     * Stores one preset's uploaded skeleton document (add-document-editing
     * D6): the base64 bytes land in the template store under the preset's id
     * and the preset row records the upload. The file name's extension must
     * match the preset's format (.docx / .pptx); PDF presets take no
     * template.
     */
    setDocumentPresetTemplate(
      presetID: string,
      filename: string,
      base64Data: string
    ): Promise<void>;
    hooks(): Promise<Hook[]>;
    createHook(hook: Hook): Promise<Hook>;
    updateHook(hook: Hook): Promise<Hook>;
    deleteHook(id: string): Promise<void>;
    /**
     * The agent's assigned hooks in evaluation order (the assigned tier runs
     * before the global tier; see the agent-hooks resolution spec).
     */
    agentHooks(agentID: string): Promise<AgentHookAssignment[]>;
    /** Assigns a hook to an agent at an evaluation position. */
    assignHook(
      agentID: string,
      hookID: string,
      position: number
    ): Promise<void>;
    /** Removes the assignment, returning the hook to the global tier. */
    unassignHook(agentID: string, hookID: string): Promise<void>;
    /** The hook-evaluation audit trail, newest first. */
    hookExecutions(): Promise<HookExecution[]>;
    mcpServers(): Promise<MCPServer[]>;
    mcpServer(id: string): Promise<MCPServer>;
    createMCPServer(server: MCPServer): Promise<MCPServer>;
    updateMCPServer(server: MCPServer): Promise<MCPServer>;
    deleteMCPServer(id: string): Promise<void>;
    /**
     * Reports a row's connection state on demand (spec agent-mcp "Probe and
     * tool discovery"). A stored server is dialed fresh and the outcome
     * persists onto the row's status fields; a supervised system server has
     * no stored definition — the supervision loop is its prober — so its
     * probe reports the live state (spec agent-mcp "System-provided server":
     * probed on demand like a user row). A failed probe reports on the row
     * (status "error" with the reason) instead of rejecting; it never
     * mutates a definition.
     */
    probeMCPServer(id: string): Promise<MCPServerView>;
    /**
     * Starts the server's OAuth authorization flow (design D2): the system
     * browser opens the server's authorization page and the call returns
     * immediately — the row is unaffected until the flow completes, which
     * arrives as an mcp:oauth:completed event.
     */
    authorizeMCPServer(id: string): Promise<void>;
    /**
     * Revokes and clears the server's stored OAuth tokens (design D9),
     * returning the row in the authorization-required state.
     */
    disconnectMCPServer(id: string): Promise<MCPServer>;
    /**
     * Creates the service-tagged MCP server row(s) for one catalog connect
     * submission; the returned row is the redacted first created row
     * (family: one row per product).
     */
    connectIntegration(input: IntegrationConnectInput): Promise<MCPServer>;
    /** Removes an integration's server definition. */
    disconnectIntegration(id: string): Promise<void>;
    gateway(): Promise<GatewaySettings>;
    saveGateway(settings: GatewaySettings): Promise<void>;
  };
  gateway: {
    state(): Promise<string>;
    start(): Promise<void>;
    pause(): Promise<void>;
    resume(): Promise<void>;
    /** Subscribes to gateway state changes (`gateway:state`). */
    subscribe(handler: GatewayStateHandler): Unsubscribe;
  };
  groups: {
    /** Every group, ordered by creation time (the rail's groups section). */
    list(): Promise<AgentGroup[]>;
    /** One group; an unknown id rejects. */
    get(groupID: string): Promise<AgentGroup>;
    /**
     * Creates a group with its roster; the first member becomes the lead
     * (design D5). A roster agent that does not exist rejects.
     */
    create(
      name: string,
      title: string,
      instructions: string,
      memberAgentIDs: string[] | null
    ): Promise<AgentGroup>;
    /** Rewrites the group's editable fields (name, title, instructions). */
    update(
      groupID: string,
      name: string,
      title: string,
      instructions: string
    ): Promise<AgentGroup>;
    /** Removes the group and — through the store's cascades — its roster,
     * transcript, cursors, tasks, and summary. */
    delete(groupID: string): Promise<void>;
    /** The group's roster in add order. */
    members(groupID: string): Promise<AgentGroupMember[]>;
    /** Adds one agent to the roster; a duplicate add rejects. */
    addMember(groupID: string, agentID: string): Promise<void>;
    /** Drops one roster row; a removed lead falls to the first remaining
     * member (design D5). */
    removeMember(groupID: string, agentID: string): Promise<void>;
    /** Assigns the lead to a roster member (design D5). */
    setLead(groupID: string, agentID: string): Promise<void>;
    /**
     * One transcript window (the session windowing contract): the newest
     * `limit` rows strictly below `beforeSeq` — or the newest page when 0 —
     * in transcript order.
     */
    transcriptPage(
      groupID: string,
      beforeSeq: number,
      limit: number
    ): Promise<AgentGroupMessage[]>;
    /** The group's tasks, ordered by task id. */
    tasks(groupID: string): Promise<AgentGroupTask[]>;
    /**
     * Submits one user message into the group (design D4): the text with
     * optional attachment metadata, an optional explicit recipient from the
     * mention picker, and an optional reply target.
     */
    send(req: GroupSendRequest): Promise<void>;
    /** Pauses the task and its descendants with a visible reason (design D8). */
    stopTask(taskID: string, reason: string): Promise<void>;
    /** Re-dispatches a paused task with the continue message as follow-up. */
    continueTask(taskID: string, message: string): Promise<void>;
    /** Moves an open task to another roster member and re-dispatches it. */
    reassignTask(taskID: string, newOwnerAgentID: string): Promise<void>;
    /** Ends every open task of the task's root chain (design D8). */
    cancelTask(taskID: string): Promise<void>;
    /**
     * Subscribes to one group's live transcript/task updates
     * (`group:<id>:event`, add-agent-groups design D9).
     */
    onGroupEvent(groupID: string, handler: GroupEventHandler): Unsubscribe;
  };
  schedules: {
    /** Every schedule's view, in the store's name order. */
    list(): Promise<ScheduleView[]>;
    /** One schedule's view; an unknown id rejects. */
    get(id: string): Promise<ScheduleView>;
    /**
     * Validates and stores a new schedule; a rejected write rejects with the
     * backend's `"<field>: <message>"` validation error.
     */
    create(input: ScheduleInput): Promise<ScheduleView>;
    /** Rewrites a schedule's input fields; validation as in create. */
    update(id: string, input: ScheduleInput): Promise<ScheduleView>;
    /** Toggles a schedule; enabling recomputes its next fire time. */
    setEnabled(id: string, enabled: boolean): Promise<ScheduleView>;
    /** Removes the schedule. */
    delete(id: string): Promise<void>;
    /** Runs the schedule on demand through the normal fire path. */
    runNow(id: string): Promise<ScheduleView>;
    /**
     * The recurrence's description and next three fires for the editor's
     * live preview; an invalid expression lands in the result's error, not
     * in a rejected promise.
     */
    preview(expr: string): Promise<Preview>;
  };
  channels: {
    /**
     * One channel's secret-free view; an absent row reads as the
     * not-configured default (disabled, no token) rather than rejecting.
     */
    channel(id: string): Promise<ChannelView>;
    /**
     * Upserts the channel's configuration; a blank BotToken keeps the stored
     * token (task 1.4: the token field is write-only).
     */
    saveChannel(id: string, input: ChannelInput): Promise<ChannelView>;
    /** Toggles the channel without touching the stored token or proxy. */
    setEnabled(id: string, enabled: boolean): Promise<ChannelView>;
    /** Every stored channel's secret-free view, ordered by id (task 9.3). */
    list(): Promise<ChannelView[]>;
    /** Every chat's state row, ordered by chat id; empty when none is known. */
    chats(): Promise<ChannelChatView[]>;
    /** The pending authorization requests, ordered by request time. */
    pendingPairings(): Promise<ChannelPairing[]>;
    /** Allowlists one pending request's user and drops the pairing. */
    approvePairing(telegramUserID: number): Promise<void>;
    /** Drops one pending request without allowlisting its user. */
    dismissPairing(telegramUserID: number): Promise<void>;
    /** Pins (or, with an empty agentID, unpins) the chat's agent override. */
    setChatPinnedAgent(
      chatID: number,
      agentID: string
    ): Promise<ChannelChatView>;
    /** Marks the chat the announcement target; the store clears the rest. */
    setHomeChat(chatID: number): Promise<ChannelChatView>;
    /** Toggles the group chat's unaddressed-message gate. */
    setChatFreeResponse(
      chatID: number,
      free: boolean
    ): Promise<ChannelChatView>;
  };
  /**
   * The Channels pane's Webhooks section (add-webhook-triggers tasks 7.1):
   * endpoint CRUD with write-only verifier secrets (a blank secret on an
   * update keeps the stored one), the deliveries log, the self-test, and
   * the listener state and address.
   */
  webhooks: {
    /** Every endpoint's secret-free view, in name order. */
    list(): Promise<WebhookEndpointView[]>;
    /** One endpoint's view; an unknown id rejects. */
    get(id: string): Promise<WebhookEndpointView>;
    /**
     * Validates and stores a new endpoint; the store assigns the ID. A name
     * already in use (case-insensitively) rejects, and the target must name
     * an existing agent or group.
     */
    create(input: WebhookEndpointInput): Promise<WebhookEndpointView>;
    /**
     * Rewrites an endpoint's definition and verifier list. A blank Secret on
     * a verifier row keeps the stored secret (write-only, design D6); a
     * non-blank one rotates it.
     */
    update(
      id: string,
      input: WebhookEndpointInput
    ): Promise<WebhookEndpointView>;
    /** Removes the endpoint; its verifiers and delivery log go with it. */
    delete(id: string): Promise<void>;
    /** Toggles the endpoint without touching its definition or secrets. */
    setEnabled(id: string, enabled: boolean): Promise<WebhookEndpointView>;
    /** One endpoint's delivery log, newest first. */
    deliveries(endpointID: string): Promise<WebhookDeliveryView[]>;
    /** The receiver's live serving state (a bind failure reads as
     * serving=false with bindError set). */
    listenerState(): Promise<WebhookListenerView>;
    /**
     * Stores the receiver's listen address (empty resets the loopback
     * default); the listener rebinds without an app restart.
     */
    setListenerAddress(addr: string): Promise<void>;
    /**
     * Delivers a self-signed test payload through the real listener; a
     * failed test rejects with the surfaced error.
     */
    test(endpointID: string): Promise<void>;
    /** The sender preset catalog with where-to-find-it hints. */
    presets(): Promise<WebhookPresetView[]>;
  };
  knowledge: {
    /** Every cataloged document, in the store's name order. */
    list(): Promise<KnowledgeDocument[]>;
    /**
     * Uploads one document (base64 content, the chat-attachments pattern);
     * an agent scope targets the agent resolved from agentID. The corpus
     * gate's rejections (unsupported kind, oversize, duplicate name in the
     * scope) reject with the backend's reason.
     */
    upload(
      filename: string,
      mime: string,
      data: string,
      scope: KnowledgeScope,
      agentID: string,
      description: string
    ): Promise<KnowledgeDocument>;
    /** Removes one document from the list, catalog, and disk. */
    delete(id: string): Promise<void>;
    /** Promotes to global or demotes to the agent resolved from agentID. */
    setScope(
      id: string,
      scope: KnowledgeScope,
      agentID: string
    ): Promise<KnowledgeDocument>;
    /** Rewrites one document's description. */
    setDescription(id: string, description: string): Promise<KnowledgeDocument>;
      /** One document's base64-encoded bytes, for previews. */
      content(id: string): Promise<string>;
    };
    notifications: {
      /**
       * The desktop-notification settings singleton (add-desktop-
       * notifications design D4); when nothing was ever saved the backend
       * answers the defaults — notifications on, every category on.
       */
      getNotificationSettings(): Promise<NotificationSettings>;
      /**
       * Create-or-replaces the settings singleton; the save path refreshes
       * the engine's cache, so changes apply immediately without a restart.
       */
      saveNotificationSettings(settings: NotificationSettings): Promise<void>;
      /**
       * Mirrors the currently viewed context for foreground suppression
       * (design D3): kind is "session" or "group" and id names the
       * conversation on screen; an empty id or unknown kind clears the
       * mirror, so an uninformative report just delivers.
       */
      reportFocus(kind: string, id: string): Promise<void>;
      /**
       * Subscribes to clicked-notification deep links (`notifications:open`,
       * design D5): emitted when the user clicks a notification, with the
       * session or group that notification concerned.
       */
      onNotificationOpen(handler: NotificationOpenHandler): Unsubscribe;
    };
    events: {
    /**
     * Subscribes to the native menu's Settings… signal (`settings:open`,
     * design D5). The event carries no payload.
     */
    onSettingsOpen(handler: () => void): Unsubscribe;
    /**
     * Subscribes to MCP OAuth completions (`mcp:oauth:completed`, design
     * D2): emitted once per authorization flow with its outcome.
     */
    onMcpOAuthCompleted(
      handler: (event: MCPOAuthCompletedEvent) => void
    ): Unsubscribe;
    /**
     * Subscribes to schedule changes (`schedules:changed`, design D10):
     * emitted for every mutation, fire, and miss.
     */
    onSchedulesChanged(handler: SchedulesChangedHandler): Unsubscribe;
    /**
     * Subscribes to one session's in-flight turn flip (`turns:changed`,
     * add-grouped-session-rail design D2): emitted when a turn claims the
     * session and again when it reaches a terminal outcome.
     */
    onTurnsChanged(handler: TurnsChangedHandler): Unsubscribe;
    /**
     * Subscribes to knowledge corpus changes (`knowledge:changed`,
     * add-knowledge-corpus D8): emitted for every mutation — upload, agent
     * save, scope change, description edit, delete — with an empty payload,
     * so surfaces re-list.
     */
    onKnowledgeChanged(handler: KnowledgeChangedHandler): Unsubscribe;
    /**
     * Subscribes to channel mutations (`channels:changed`, add-telegram-
     * channel task 1.4): emitted for every channel configuration and pairing
     * change, so the Channels pane refetches.
     */
    onChannelsChanged(handler: ChannelsChangedHandler): Unsubscribe;
    /**
     * Subscribes to channel connection states (`channels:state`, design
     * D10): emitted on every state transition with the state and the failure
     * detail when the state is error.
     */
    onChannelsState(handler: ChannelsStateHandler): Unsubscribe;
    /**
     * Subscribes to agent-group roster/list changes (`groups:changed`,
     * add-agent-groups design D9): emitted for every group mutation, so the
     * surfaces re-list.
     */
    onGroupsChanged(handler: GroupsChangedHandler): Unsubscribe;
    /**
     * Subscribes to webhook endpoint mutations (`webhooks:changed`,
     * add-webhook-triggers task 7.1): emitted for every save, enable
     * toggle, delete, and completed self-test, so the Webhooks section
     * refetches.
     */
    onWebhooksChanged(handler: WebhooksChangedHandler): Unsubscribe;
  };
  windowTheme: {
    /** Forces the native window appearance to match the app theme. */
    setLight(light: boolean): Promise<void>;
  };
}

function unwrap<T>(value: T | null | undefined): T {
  // SAFETY: the backend returns null/undefined only for absent collections,
  // which every caller renders as empty; the value itself is already typed T.
  return value as T;
}

export function createClient(transport: Transport = wailsTransport): Client {
  return {
    appInfo: {
      get: (): Promise<Metadata> => transport.appInfo.Get(),
    },
    agents: {
      list: async (): Promise<Agent[]> => unwrap(await transport.agents.List()),
      get: (id: string): Promise<Agent> => transport.agents.Get(id),
      create: (input: CreateAgentInput): Promise<Agent> =>
        transport.agents.Create(input),
      update: (agent: Agent): Promise<Agent> => transport.agents.Update(agent),
      delete: (id: string): Promise<void> => transport.agents.Delete(id),
      getPromptDocuments: (agentID: string): Promise<PromptDocuments> =>
        transport.agents.GetPromptDocuments(agentID),
      savePromptDocument: (
        agentID: string,
        doc: string,
        content: string
      ): Promise<void> =>
        transport.agents.SavePromptDocument(agentID, doc, content),
      regeneratePrompts: (
        agentID: string,
        instruction: string
      ): Promise<void> =>
        transport.agents.RegeneratePrompts(agentID, instruction),
      listWorkspaceSkills: async (agentID: string): Promise<WorkspaceSkill[]> =>
        unwrap(await transport.agents.ListWorkspaceSkills(agentID)),
      promoteWorkspaceSkill: (agentID: string, name: string): Promise<void> =>
        transport.agents.PromoteWorkspaceSkill(agentID, name),
      deleteWorkspaceSkill: (agentID: string, name: string): Promise<void> =>
        transport.agents.DeleteWorkspaceSkill(agentID, name),
    },
    chat: {
      sessions: async (agentID: string): Promise<Session[]> =>
        unwrap(await transport.chat.Sessions(agentID)),
      allSessions: async (): Promise<Session[]> =>
        unwrap(await transport.chat.AllSessions()),
      runningSessions: async (): Promise<string[]> =>
        unwrap(await transport.chat.RunningSessions()),
      createSession: (agentID: string): Promise<Session> =>
        transport.chat.CreateSession(agentID),
      send: (
        sessionID: string,
        message: string,
        attachmentKeys?: string[]
      ): Promise<string> =>
        transport.chat.Send(sessionID, message, attachmentKeys ?? null),
      cancel: (sessionID: string): Promise<void> =>
        transport.chat.Cancel(sessionID),
      regenerate: (sessionID: string, turnID: string): Promise<string> =>
        transport.chat.Regenerate(sessionID, turnID),
      resolveApproval: (
        sessionID: string,
        interruptID: string,
        approved: boolean
      ): Promise<void> =>
        transport.chat.ResolveApproval(sessionID, interruptID, approved),
      /**
       * Submits the user-typed secret for the session's pending browser
       * attention request (add-browser-login-handover design D3). The value
       * rides this call only — never the event log; saveLogin vaults the
       * login before the turn resumes (a failed save does not stop it).
       */
      resolveInput: (
        sessionID: string,
        interruptID: string,
        text: string,
        identifier: string,
        saveLogin: boolean
      ): Promise<void> =>
        transport.chat.ResolveInput(
          sessionID,
          interruptID,
          text,
          identifier,
          saveLogin
        ),
      /**
       * Ends the session's pending browser secret request with the user's
       * cancel (the secret card's Cancel button): the parked submit_secret
       * call unblocks on the cancelled result — the turn continues — and the
       * receipt records the cancelled outcome only.
       */
      cancelInput: (sessionID: string, interruptID: string): Promise<void> =>
        transport.chat.CancelInput(sessionID, interruptID),
      /**
       * Approves the session's pending browser takeover: the profile's
       * browser relaunches headed and focused and the agent's tools hold
       * (design D8). The card flips active when this resolves.
       */
      beginBrowserTakeover: (
        sessionID: string,
        interruptID: string
      ): Promise<void> =>
        transport.chat.BeginBrowserTakeover(sessionID, interruptID),
      /** Ends the session's pending browser takeover (completed/cancelled). */
      resolveTakeover: (
        sessionID: string,
        interruptID: string,
        outcome: string
      ): Promise<void> =>
        transport.chat.ResolveTakeover(sessionID, interruptID, outcome),
      transcript: async (sessionID: string): Promise<SessionEvent[]> =>
        unwrap(await transport.chat.Transcript(sessionID)),
      transcriptPage: async (
        sessionID: string,
        beforeSeq: number,
        limit: number
      ): Promise<SessionEvent[]> =>
        unwrap(
          await transport.chat.TranscriptPage(sessionID, beforeSeq, limit)
        ),
      deleteSession: (sessionID: string): Promise<void> =>
        transport.chat.DeleteSession(sessionID),
      saveAttachment: (
        sessionID: string,
        name: string,
        mime: string,
        data: string
      ): Promise<SessionAttachment> =>
        transport.chat.SaveAttachment(sessionID, name, mime, data),
      attachmentContent: (key: string): Promise<AttachmentContent> =>
        transport.chat.AttachmentContent(key),
      browserScreenshot: async (
        sessionID: string,
        name: string
      ): Promise<BrowserScreenshotContent> =>
        unwrap(await transport.chat.BrowserScreenshot(sessionID, name)),
      computerScreenshot: async (
        sessionID: string,
        name: string
      ): Promise<BrowserScreenshotContent> =>
        unwrap(await transport.chat.ComputerScreenshot(sessionID, name)),
      generatedDocument: async (
        agentID: string,
        sessionID: string,
        name: string
      ): Promise<GeneratedDocumentContent> =>
        unwrap(
          await transport.chat.GeneratedDocument(agentID, sessionID, name)
        ),
      saveGeneratedDocumentAs: (
        agentID: string,
        sessionID: string,
        name: string
      ): Promise<string> =>
        transport.chat.SaveGeneratedDocumentAs(agentID, sessionID, name),
      workspaceFile: (agentID: string, relPath: string): Promise<string> =>
        transport.chat.WorkspaceFile(agentID, relPath),
      workspaceFileBinary: async (
        agentID: string,
        relPath: string
      ): Promise<WorkspaceFileBinaryContent> =>
        unwrap(await transport.chat.WorkspaceFileBinary(agentID, relPath)),
      listWorkspace: async (
        agentID: string,
        relPath: string
      ): Promise<WorkspaceEntry[]> =>
        unwrap(await transport.chat.ListWorkspace(agentID, relPath)),
      subscribe: async (
        sessionID: string,
        handler: SessionEventHandler
      ): Promise<Unsubscribe> => {
        // Listener-before-subscribe: the backend starts pumping the
        // subscribe-time catch-up snapshot before the Subscribe RPC returns,
        // and the Wails dispatch only reaches currently registered listeners —
        // so events emitted in that window must already have a listener
        // (enhance-resumable-streams design D1).
        const off = transport.onEvent(
          sessionEventName(sessionID),
          (data: EventPayload): void => {
            // SAFETY: sessionEventName channel only carries SessionEvents.
            handler(data as SessionEvent);
          }
        );

        await transport.chat.Subscribe(sessionID);

        return (): void => {
          off();
        };
      },
      unsubscribe: (sessionID: string): Promise<void> =>
        transport.chat.Unsubscribe(sessionID),
      resync: (sessionID: string): Promise<void> =>
        transport.chat.Resync(sessionID),
      turnState: (sessionID: string): Promise<TurnStateResult> =>
        transport.chat.TurnState(sessionID),
      contextUsage: (sessionID: string): Promise<ContextUsage> =>
        transport.chat.ContextUsage(sessionID),
      setSessionOverrides: (
        sessionID: string,
        model: string,
        effort: string
      ): Promise<void> =>
        transport.chat.SetSessionOverrides(sessionID, model, effort),
    },
    settings: {
      providers: async (): Promise<ProviderView[]> =>
        unwrap(await transport.settings.Providers()),
      provider: (id: string): Promise<ProviderView> =>
        transport.settings.Provider(id),
      createProvider: (input: ProviderInput): Promise<ProviderView> =>
        transport.settings.CreateProvider(input),
      updateProvider: (
        id: string,
        input: ProviderInput
      ): Promise<ProviderView> => transport.settings.UpdateProvider(id, input),
      deleteProvider: (id: string): Promise<void> =>
        transport.settings.DeleteProvider(id),
      // The bridge maps an empty Go slice to null; callers iterate the list,
      // so normalize once here.
      providerModels: async (id: string): Promise<ModelCapability[]> => {
        const models = unwrap(await transport.settings.ProviderModels(id));

        return Array.isArray(models) ? models : [];
      },
      listProviderModels: async (
        providerID: string
      ): Promise<ProviderModelView[]> => {
        const models = unwrap(
          await transport.settings.ListProviderModels(providerID)
        );

        // SAFETY: the bridge maps an empty Go slice to null and may carry
        // null rows; filter(Boolean) drops the nulls before the narrowing.
        return Array.isArray(models)
          ? (models.filter(Boolean) as ProviderModelView[])
          : [];
      },
      createModel: async (
        providerID: string,
        input: ProviderModelInput
      ): Promise<ProviderModelView> => {
        const res = await transport.settings.CreateModel(providerID, input);

        if (!res) throw new Error("Failed to create provider model");

        return res;
      },
      updateModel: async (
        id: number,
        input: ProviderModelInput
      ): Promise<ProviderModelView> => {
        const res = await transport.settings.UpdateModel(id, input);

        if (!res) throw new Error("Failed to update provider model");

        return res;
      },
      deleteModel: (id: number): Promise<void> =>
        transport.settings.DeleteModel(id),
      toggleModel: async (
        id: number,
        enabled: boolean
      ): Promise<ProviderModelView> => {
        const res = await transport.settings.ToggleModel(id, enabled);

        if (!res) throw new Error("Failed to toggle provider model");

        return res;
      },
      verifyProviderDraft: (
        input: ProviderInput,
        providerID: string
      ): Promise<VerifyResult> =>
        transport.settings.VerifyProviderDraft(input, providerID),
      verifyProvider: (id: string): Promise<VerifyResult> =>
        transport.settings.VerifyProvider(id),
      skills: async (): Promise<Skill[]> =>
        unwrap(await transport.settings.Skills()),
      setSkillEnabled: (name: string, enabled: boolean): Promise<void> =>
        transport.settings.SetSkillEnabled(name, enabled),
      tools: async (): Promise<ToolEntry[]> =>
        unwrap(await transport.settings.Tools()),
      setToolEnabled: (name: string, enabled: boolean): Promise<void> =>
        transport.settings.SetToolEnabled(name, enabled),
      webSearchProviders: async (): Promise<WebSearchProviderInfo[]> =>
        unwrap(await transport.settings.WebSearchProviders()),
      // The bridge maps an empty Go slice to null; the chain editor iterates
      // the list, so normalize it once here.
      webSearchConfig: async (): Promise<WebSearchConfig> => {
        const config = await transport.settings.WebSearchConfig();

        return {
          ...config,
          entries: Array.isArray(config.entries) ? config.entries : [],
        };
      },
      saveWebSearchConfig: (config: WebSearchConfigInput): Promise<void> =>
        transport.settings.SaveWebSearchConfig(config),
      browserConfig: (): Promise<BrowserConfig> =>
        transport.settings.BrowserConfig(),
      saveBrowserConfig: (config: BrowserConfig): Promise<void> =>
        transport.settings.SaveBrowserConfig(config),
      // The Computer Use pane's round trips (add-computer-use tasks 3.1):
      // thin pass-throughs — the status aggregate and vocabulary live
      // backend-side, the pane renders them verbatim.
      computerConfig: (): Promise<ComputerConfigView> =>
        transport.settings.ComputerConfig(),
      saveComputerConfig: (view: ComputerConfigView): Promise<void> =>
        transport.settings.SaveComputerConfig(view),
      computerStatus: (): Promise<ComputerStatus> =>
        transport.settings.ComputerStatus(),
      checkComputerPermissions: (): Promise<ComputerStatus> =>
        transport.settings.CheckComputerPermissions(),
      promptComputerPermissions: (): Promise<void> =>
        transport.settings.PromptComputerPermissions(),
      openComputerManageURL: (url: string): Promise<void> =>
        transport.settings.OpenComputerManageURL(url),
      // The bridge maps an empty Go slice to null; the profile list iterates
      // the result, so normalize it once here.
      browserProfiles: async (): Promise<BrowserProfile[]> =>
        unwrap(await transport.settings.BrowserProfiles()),
      createBrowserProfile: (name: string): Promise<void> =>
        transport.settings.CreateBrowserProfile(name),
      deleteBrowserProfile: (name: string): Promise<void> =>
        transport.settings.DeleteBrowserProfile(name),
      // Copies an existing Chrome/Chromium user-data directory into the
      // browser profiles dir (rebrand-browser-use-system-mcp design D9).
      // A name collision is not an error: the backend appends a suffix and
      // returns the profile's final name.
      importBrowserProfile: (
        sourcePath: string,
        name: string
      ): Promise<string> =>
        transport.settings.ImportBrowserProfile(sourcePath, name),
      importChromeProfile: (dirName: string): Promise<string> =>
        transport.settings.ImportChromeProfile(dirName),
      // The bridge maps an empty Go slice to null; the import list iterates
      // the result, so normalize it once here.
      detectChromeProfiles: async (): Promise<ChromeProfileCandidate[]> => {
        const candidates = unwrap(
          await transport.settings.DetectChromeProfiles()
        );

        return Array.isArray(candidates) ? candidates : [];
      },
      vaultLogins: async (): Promise<VaultLogin[]> => {
        const logins = unwrap(await transport.settings.VaultLogins());

        return Array.isArray(logins) ? logins : [];
      },
      saveVaultLogin: (
        name: string,
        origin: string,
        username: string,
        password: string,
        addedBy: string
      ): Promise<void> =>
        transport.settings.SaveVaultLogin(
          name,
          origin,
          username,
          password,
          addedBy
        ),
      removeVaultLogin: (id: string): Promise<void> =>
        transport.settings.RemoveVaultLogin(id),
      revealVaultLogin: (id: string): Promise<string> =>
        transport.settings.RevealVaultLogin(id),
      // The bridge maps an empty Go slice to null; the preset list iterates
      // the result, so normalize it once here.
      documentPresets: async (
        format: string
      ): Promise<DocumentPresetEntry[]> => {
        const presets = unwrap(
          await transport.settings.ListDocumentPresets(format)
        );

        return Array.isArray(presets) ? presets : [];
      },
      createDocumentPreset: (
        format: string,
        name: string,
        configJSON: string
      ): Promise<DocumentPresetEntry> =>
        transport.settings.CreateDocumentPreset(format, name, configJSON),
      updateDocumentPreset: (
        id: string,
        name: string,
        configJSON: string
      ): Promise<DocumentPresetEntry> =>
        transport.settings.UpdateDocumentPreset(id, name, configJSON),
      duplicateDocumentPreset: (
        id: string,
        newName: string
      ): Promise<DocumentPresetEntry> =>
        transport.settings.DuplicateDocumentPreset(id, newName),
      deleteDocumentPreset: (id: string): Promise<void> =>
        transport.settings.DeleteDocumentPreset(id),
      defaultDocumentPreset: (format: string): Promise<string> =>
        transport.settings.DefaultDocumentPreset(format),
      setDefaultDocumentPreset: (format: string, name: string): Promise<void> =>
        transport.settings.SetDefaultDocumentPreset(format, name),
      setDocumentPresetTemplate: (
        presetID: string,
        filename: string,
        base64Data: string
      ): Promise<void> =>
        transport.settings.SetDocumentPresetTemplate(
          presetID,
          filename,
          base64Data
        ),
      hooks: async (): Promise<Hook[]> =>
        unwrap(await transport.settings.Hooks()),
      createHook: (hook: Hook): Promise<Hook> =>
        transport.settings.CreateHook(withoutTimestamps(hook)),
      updateHook: (hook: Hook): Promise<Hook> =>
        transport.settings.UpdateHook(hook),
      deleteHook: (id: string): Promise<void> =>
        transport.settings.DeleteHook(id),
      agentHooks: async (agentID: string): Promise<AgentHookAssignment[]> =>
        unwrap(await transport.settings.AgentHooks(agentID)),
      assignHook: (
        agentID: string,
        hookID: string,
        position: number
      ): Promise<void> =>
        transport.settings.AssignHook(agentID, hookID, position),
      unassignHook: (agentID: string, hookID: string): Promise<void> =>
        transport.settings.UnassignHook(agentID, hookID),
      hookExecutions: async (): Promise<HookExecution[]> =>
        unwrap(await transport.settings.HookExecutions()),
      mcpServers: async (): Promise<MCPServer[]> =>
        unwrap(await transport.settings.MCPServers()),
      mcpServer: (id: string): Promise<MCPServer> =>
        transport.settings.MCPServer(id),
      createMCPServer: (server: MCPServer): Promise<MCPServer> =>
        transport.settings.CreateMCPServer(withoutTimestamps(server)),
      updateMCPServer: (server: MCPServer): Promise<MCPServer> =>
        transport.settings.UpdateMCPServer(server),
      deleteMCPServer: (id: string): Promise<void> =>
        transport.settings.DeleteMCPServer(id),
      probeMCPServer: (id: string): Promise<MCPServerView> =>
        transport.settings.ProbeMCPServer(id),
      authorizeMCPServer: (id: string): Promise<void> =>
        transport.settings.AuthorizeMCPServer(id),
      disconnectMCPServer: (id: string): Promise<MCPServer> =>
        transport.settings.DisconnectMCPServer(id),
      connectIntegration: (
        input: IntegrationConnectInput
      ): Promise<MCPServer> => transport.settings.ConnectIntegration(input),
      disconnectIntegration: (id: string): Promise<void> =>
        transport.settings.DisconnectIntegration(id),
      gateway: (): Promise<GatewaySettings> => transport.settings.Gateway(),
      saveGateway: (settings: GatewaySettings): Promise<void> =>
        transport.settings.SaveGateway(settings),
    },
    gateway: {
      state: (): Promise<string> => transport.gateway.State(),
      start: (): Promise<void> => transport.gateway.Start(),
      pause: (): Promise<void> => transport.gateway.Pause(),
      resume: (): Promise<void> => transport.gateway.Resume(),
      subscribe: (handler: GatewayStateHandler): Unsubscribe =>
        transport.onEvent(GATEWAY_STATE_EVENT, (data: EventPayload): void => {
          // SAFETY: GATEWAY_STATE_EVENT only carries GatewayStateEvents.
          handler(data as GatewayStateEvent);
        }),
    },
    groups: {
      list: async (): Promise<AgentGroup[]> =>
        unwrap(await transport.groups.ListGroups()),
      get: (groupID: string): Promise<AgentGroup> =>
        transport.groups.Group(groupID),
      create: (
        name: string,
        title: string,
        instructions: string,
        memberAgentIDs: string[] | null
      ): Promise<AgentGroup> =>
        transport.groups.CreateGroup(name, title, instructions, memberAgentIDs),
      update: (
        groupID: string,
        name: string,
        title: string,
        instructions: string
      ): Promise<AgentGroup> =>
        transport.groups.UpdateGroup(groupID, name, title, instructions),
      delete: (groupID: string): Promise<void> =>
        transport.groups.DeleteGroup(groupID),
      members: async (groupID: string): Promise<AgentGroupMember[]> =>
        unwrap(await transport.groups.Members(groupID)),
      addMember: (groupID: string, agentID: string): Promise<void> =>
        transport.groups.AddMember(groupID, agentID),
      removeMember: (groupID: string, agentID: string): Promise<void> =>
        transport.groups.RemoveMember(groupID, agentID),
      setLead: (groupID: string, agentID: string): Promise<void> =>
        transport.groups.SetLead(groupID, agentID),
      transcriptPage: async (
        groupID: string,
        beforeSeq: number,
        limit: number
      ): Promise<AgentGroupMessage[]> =>
        unwrap(
          await transport.groups.TranscriptPage(groupID, beforeSeq, limit)
        ),
      tasks: async (groupID: string): Promise<AgentGroupTask[]> =>
        unwrap(await transport.groups.Tasks(groupID)),
      send: (req: GroupSendRequest): Promise<void> =>
        transport.groups.Send(req),
      stopTask: (taskID: string, reason: string): Promise<void> =>
        transport.groups.StopTask(taskID, reason),
      continueTask: (taskID: string, message: string): Promise<void> =>
        transport.groups.ContinueTask(taskID, message),
      reassignTask: (taskID: string, newOwnerAgentID: string): Promise<void> =>
        transport.groups.ReassignTask(taskID, newOwnerAgentID),
      cancelTask: (taskID: string): Promise<void> =>
        transport.groups.CancelTask(taskID),
      onGroupEvent: (
        groupID: string,
        handler: GroupEventHandler
      ): Unsubscribe =>
        transport.onEvent(
          groupEventName(groupID),
          (data: EventPayload): void => {
            // SAFETY: the group:<id>:event channel only carries GroupEvents.
            handler(data as GroupEvent);
          }
        ),
    },
    schedules: {
      list: async (): Promise<ScheduleView[]> =>
        unwrap(await transport.schedules.List()),
      get: (id: string): Promise<ScheduleView> => transport.schedules.Get(id),
      create: (input: ScheduleInput): Promise<ScheduleView> =>
        transport.schedules.Create(input),
      update: (id: string, input: ScheduleInput): Promise<ScheduleView> =>
        transport.schedules.Update(id, input),
      setEnabled: (id: string, enabled: boolean): Promise<ScheduleView> =>
        transport.schedules.SetEnabled(id, enabled),
      delete: (id: string): Promise<void> => transport.schedules.Delete(id),
      runNow: (id: string): Promise<ScheduleView> =>
        transport.schedules.RunNow(id),
      preview: (expr: string): Promise<Preview> =>
        transport.schedules.Preview(expr),
    },
    channels: {
      channel: (id: string): Promise<ChannelView> =>
        transport.channels.GetChannel(id),
      saveChannel: (id: string, input: ChannelInput): Promise<ChannelView> =>
        transport.channels.SaveChannel(id, input),
      setEnabled: (id: string, enabled: boolean): Promise<ChannelView> =>
        transport.channels.SetEnabled(id, enabled),
      list: async (): Promise<ChannelView[]> =>
        unwrap(await transport.channels.ListChannels()),
      chats: async (): Promise<ChannelChatView[]> =>
        unwrap(await transport.channels.ListChats()),
      pendingPairings: async (): Promise<ChannelPairing[]> =>
        unwrap(await transport.channels.ListPendingPairings()),
      approvePairing: (telegramUserID: number): Promise<void> =>
        transport.channels.ApprovePairing(telegramUserID),
      dismissPairing: (telegramUserID: number): Promise<void> =>
        transport.channels.DismissPairing(telegramUserID),
      setChatPinnedAgent: (
        chatID: number,
        agentID: string
      ): Promise<ChannelChatView> =>
        transport.channels.SetChatPinnedAgent(chatID, agentID),
      setHomeChat: (chatID: number): Promise<ChannelChatView> =>
        transport.channels.SetHomeChat(chatID),
      setChatFreeResponse: (
        chatID: number,
        free: boolean
      ): Promise<ChannelChatView> =>
        transport.channels.SetChatFreeResponse(chatID, free),
    },
    webhooks: {
      list: async (): Promise<WebhookEndpointView[]> =>
        unwrap(await transport.webhooks.ListEndpoints()),
      get: (id: string): Promise<WebhookEndpointView> =>
        transport.webhooks.GetEndpoint(id),
      create: (input: WebhookEndpointInput): Promise<WebhookEndpointView> =>
        transport.webhooks.CreateEndpoint(input),
      update: (
        id: string,
        input: WebhookEndpointInput
      ): Promise<WebhookEndpointView> =>
        transport.webhooks.UpdateEndpoint(id, input),
      delete: (id: string): Promise<void> =>
        transport.webhooks.DeleteEndpoint(id),
      setEnabled: (id: string, enabled: boolean): Promise<WebhookEndpointView> =>
        transport.webhooks.SetEnabled(id, enabled),
      deliveries: async (endpointID: string): Promise<WebhookDeliveryView[]> =>
        unwrap(await transport.webhooks.Deliveries(endpointID)),
      listenerState: (): Promise<WebhookListenerView> =>
        transport.webhooks.ListenerState(),
      setListenerAddress: (addr: string): Promise<void> =>
        transport.webhooks.SetListenerAddress(addr),
      test: (endpointID: string): Promise<void> =>
        transport.webhooks.Test(endpointID),
      presets: async (): Promise<WebhookPresetView[]> =>
        unwrap(await transport.webhooks.Presets()),
    },
    knowledge: {
      list: async (): Promise<KnowledgeDocument[]> =>
        unwrap(await transport.knowledge.List()),
      upload: (
        filename: string,
        mime: string,
        data: string,
        scope: KnowledgeScope,
        agentID: string,
        description: string
      ): Promise<KnowledgeDocument> =>
        transport.knowledge.Upload(
          filename,
          mime,
          data,
          scope,
          agentID,
          description
        ),
      delete: (id: string): Promise<void> => transport.knowledge.Delete(id),
      setScope: (
        id: string,
        scope: KnowledgeScope,
        agentID: string
      ): Promise<KnowledgeDocument> =>
        transport.knowledge.SetScope(id, scope, agentID),
      setDescription: (
        id: string,
        description: string
      ): Promise<KnowledgeDocument> =>
        transport.knowledge.SetDescription(id, description),
      content: (id: string): Promise<string> => transport.knowledge.Content(id),
    },
    notifications: {
      getNotificationSettings: (): Promise<NotificationSettings> =>
        transport.notifications.GetNotificationSettings(),
      saveNotificationSettings: (
        settings: NotificationSettings
      ): Promise<void> =>
        transport.notifications.SaveNotificationSettings(settings),
      reportFocus: (kind: string, id: string): Promise<void> =>
        transport.notifications.ReportFocus(kind, id),
      onNotificationOpen: (handler: NotificationOpenHandler): Unsubscribe =>
        transport.onEvent(
          NOTIFICATIONS_OPEN_EVENT,
          (data: EventPayload): void => {
            // SAFETY: NOTIFICATIONS_OPEN_EVENT only carries
            // NotificationOpenEvents.
            handler(data as NotificationOpenEvent);
          }
        ),
    },
    events: {
      onSettingsOpen: (handler: () => void): Unsubscribe =>
        transport.onEvent(SETTINGS_OPEN_EVENT, (): void => {
          handler();
        }),
      onMcpOAuthCompleted: (
        handler: (event: MCPOAuthCompletedEvent) => void
      ): Unsubscribe =>
        transport.onEvent(
          MCP_OAUTH_COMPLETED_EVENT,
          (data: EventPayload): void => {
            // SAFETY: MCP_OAUTH_COMPLETED_EVENT only carries
            // MCPOAuthCompletedEvents.
            handler(data as MCPOAuthCompletedEvent);
          }
        ),
      onSchedulesChanged: (handler: SchedulesChangedHandler): Unsubscribe =>
        transport.onEvent(
          SCHEDULES_CHANGED_EVENT,
          (data: EventPayload): void => {
            // SAFETY: SCHEDULES_CHANGED_EVENT only carries
            // SchedulesChangedEvents.
            handler(data as SchedulesChangedEvent);
          }
        ),
      onTurnsChanged: (handler: TurnsChangedHandler): Unsubscribe =>
        transport.onEvent(TURNS_CHANGED_EVENT, (data: EventPayload): void => {
          // SAFETY: TURNS_CHANGED_EVENT only carries TurnsChangedEvents.
          handler(data as TurnsChangedEvent);
        }),
      onKnowledgeChanged: (handler: KnowledgeChangedHandler): Unsubscribe =>
        transport.onEvent(KNOWLEDGE_CHANGED_EVENT, (): void => {
          // SAFETY: KNOWLEDGE_CHANGED_EVENT carries an empty payload; the
          // event object itself is the whole notification.
          handler({} as KnowledgeChangedEvent);
        }),
      onChannelsChanged: (handler: ChannelsChangedHandler): Unsubscribe =>
        transport.onEvent(
          CHANNELS_CHANGED_EVENT,
          (data: EventPayload): void => {
            // SAFETY: CHANNELS_CHANGED_EVENT only carries ChannelChangedEvents.
            handler(data as ChannelChangedEvent);
          }
        ),
      onChannelsState: (handler: ChannelsStateHandler): Unsubscribe =>
        transport.onEvent(CHANNELS_STATE_EVENT, (data: EventPayload): void => {
          // SAFETY: CHANNELS_STATE_EVENT only carries ChannelStateEvents.
          handler(data as ChannelStateEvent);
        }),
      onGroupsChanged: (handler: GroupsChangedHandler): Unsubscribe =>
        transport.onEvent(GROUPS_CHANGED_EVENT, (): void => {
          // SAFETY: GROUPS_CHANGED_EVENT carries an empty payload; the event
          // object itself is the whole notification.
          handler({} as GroupsChangedEvent);
        }),
      /**
       * Subscribes to webhook endpoint mutations (`webhooks:changed`,
       * add-webhook-triggers task 7.1): emitted for every save, enable
       * toggle, delete, and completed self-test, so the Webhooks section
       * refetches.
       */
      onWebhooksChanged: (handler: WebhooksChangedHandler): Unsubscribe =>
        transport.onEvent(WEBHOOKS_CHANGED_EVENT, (data: EventPayload): void => {
          // SAFETY: WEBHOOKS_CHANGED_EVENT only carries WebhooksChangedEvents.
          handler(data as WebhooksChangedEvent);
        }),
    },
    windowTheme: {
      setLight: (light: boolean): Promise<void> =>
        transport.windowTheme.SetLight(light),
    },
  };
}

/** Application-wide client bound to the real Wails transport. */
export const client = createClient();
