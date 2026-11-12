/**
 * Transport seam between the app and the Wails runtime (design D1): every
 * backend interaction goes through this interface, so UI code and tests never
 * touch @wailsio/runtime or the generated binding modules directly.
 *
 * The service interfaces mirror the generated binding modules structurally
 * (plain promises — CancellablePromise satisfies them), so tests substitute
 * simple async mocks without the Wails runtime.
 */
import { Events } from "@wailsio/runtime";

import type * as agentsModels from "../../bindings/github.com/oniharnantyo/buddies/internal/agents/models";
import type * as groupsModels from "../../bindings/github.com/oniharnantyo/buddies/internal/groups/models";
import type * as schedulerModels from "../../bindings/github.com/oniharnantyo/buddies/internal/scheduler/models";
import type * as sessionModels from "../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type * as serviceModels from "../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type * as storeModels from "../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import * as agentsBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/agentsservice";
import * as appInfoBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/appinfo";
import * as chatBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/chatservice";
import * as channelBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/channelservice";
import * as gatewayBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/gatewayservice";
import * as groupsBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/groupsservice";
import * as knowledgeBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/knowledgeservice";
import * as notificationsBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/notificationsservice";
import * as scheduleBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/scheduleservice";
import * as settingsBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/settingsservice";
import * as webhookBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/webhookservice";
import * as windowThemeModule from "../../bindings/github.com/oniharnantyo/buddies/windowthemeservice";

/** Unsubscribe function returned by event subscriptions. */
export type Unsubscribe = () => void;

/**
 * One decoded event payload at the transport boundary: the session channel
 * carries SessionEvents, the gateway channel carries GatewayStateEvents, the
 * MCP OAuth channel carries MCPOAuthCompletedEvents, the schedules channel
 * carries SchedulesChangedEvents, the turns channel carries
 * TurnsChangedEvents, the channels channels carry the channel change and
 * state events, the groups channels carry the group event and
 * groups-changed payloads, the notifications channel carries the
 * notification-open payload, and the webhooks channel carries the webhook
 * endpoint change payload. The runtime hands all of them to `onEvent`, so
 * this is the single documented place where the domains meet before the
 * client narrows them.
 */
export type EventPayload =
  | sessionModels.Event
  | serviceModels.ChannelChangedEvent
  | serviceModels.ChannelStateEvent
  | serviceModels.GatewayStateEvent
  | serviceModels.GroupEvent
  | serviceModels.GroupsChangedEvent
  | serviceModels.KnowledgeChangedEvent
  | serviceModels.MCPOAuthCompletedEvent
  | serviceModels.NotificationOpenEvent
  | serviceModels.SchedulesChangedEvent
  | serviceModels.TurnsChangedEvent
  | serviceModels.WebhooksChangedEvent;

/** Handler receiving the payload data of one emitted event. */
export type EventHandler = (data: EventPayload) => void;

export interface Transport {
  appInfo: {
    Get(): Promise<serviceModels.Metadata>;
  };
  agents: {
    List(): Promise<storeModels.Agent[] | null>;
    Get(id: string): Promise<storeModels.Agent>;
    Create(input: serviceModels.CreateAgentInput): Promise<storeModels.Agent>;
    Update(agent: storeModels.Agent): Promise<storeModels.Agent>;
    Delete(id: string): Promise<void>;
    GetPromptDocuments(agentID: string): Promise<serviceModels.PromptDocuments>;
    SavePromptDocument(
      agentID: string,
      doc: string,
      content: string
    ): Promise<void>;
    RegeneratePrompts(agentID: string, instruction: string): Promise<void>;
    /**
     * Lists the agent's own workspace skills (add-agent-created-skills task
     * 2.1): name plus the SKILL.md description per skill directory. Only
     * reads the workspace tier; an agent without skills lists empty.
     */
    ListWorkspaceSkills(
      agentID: string
    ): Promise<serviceModels.WorkspaceSkill[] | null>;
    /**
     * Copies one workspace skill into the global skills library and
     * registers it enabled; the workspace copy stays. A name already in the
     * library rejects with an error naming the collision and changes nothing.
     */
    PromoteWorkspaceSkill(agentID: string, name: string): Promise<void>;
    /** Removes one workspace-tier skill directory; the library is untouched. */
    DeleteWorkspaceSkill(agentID: string, name: string): Promise<void>;
  };
  chat: {
    CreateSession(agentID: string): Promise<storeModels.Session>;
    /** Submits one user message; the keys name prior SaveAttachment uploads. */
    Send(
      sessionID: string,
      message: string,
      attachmentKeys: string[] | null
    ): Promise<string>;
    /** Stops the session's in-flight (or approval-parked) turn; a safe no-op when idle. */
    Cancel(sessionID: string): Promise<void>;
    /** Re-runs a turn's prompt as a new variant; resolves with the new turn id. */
    Regenerate(sessionID: string, turnID: string): Promise<string>;
    Sessions(agentID: string): Promise<storeModels.Session[] | null>;
    /** Lists every agent's sessions (add-grouped-session-rail D1). */
    AllSessions(): Promise<storeModels.Session[] | null>;
    /** Reports the sessions with a turn in flight (add-grouped-session-rail D2). */
    RunningSessions(): Promise<string[] | null>;
    /** Rewrites the session's model/effort overrides; empty clears to inherit. */
    SetSessionOverrides(
      sessionID: string,
      model: string,
      effort: string
    ): Promise<void>;
    /** Submits the user's decision for the session's pending approval. */
    ResolveApproval(
      sessionID: string,
      interruptID: string,
      approved: boolean
    ): Promise<void>;
    /**
     * Submits the user-typed secret for the session's pending browser
     * attention request (add-browser-login-handover design D3); the value
     * rides this call only, never the event log.
     */
    ResolveInput(
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
    CancelInput(sessionID: string, interruptID: string): Promise<void>;
    /** Approves the session's pending browser takeover (headed relaunch). */
    BeginBrowserTakeover(sessionID: string, interruptID: string): Promise<void>;
    /** Ends the session's pending browser takeover (completed/cancelled). */
    ResolveTakeover(
      sessionID: string,
      interruptID: string,
      outcome: string
    ): Promise<void>;
    Subscribe(sessionID: string): Promise<void>;
    Transcript(sessionID: string): Promise<sessionModels.Event[] | null>;
    /**
     * One transcript window (design D6): the newest `limit` events strictly
     * before `beforeSeq` — or the newest page when 0 — in replay order.
     */
    TranscriptPage(
      sessionID: string,
      beforeSeq: number,
      limit: number
    ): Promise<sessionModels.Event[] | null>;
    /** Removes the session and its transcript; refused while a turn runs. */
    DeleteSession(sessionID: string): Promise<void>;
    /** Stores one upload (base64 data); sessionID folders the bytes under
     * the per-session directory (empty = the flat legacy layout). Returns
     * its capability metadata. */
    SaveAttachment(
      sessionID: string,
      name: string,
      mime: string,
      data: string
    ): Promise<sessionModels.Attachment>;
    /** Serves one capability key's metadata and base64 content. */
    AttachmentContent(key: string): Promise<serviceModels.AttachmentContent>;
    /**
     * Serves one browser screenshot's bytes by the file name the browser
     * tool wrote (design D6): base64 PNG plus the dimensions the tool
     * reported. The backend validates the name shape and resolves it solely
     * inside the calling session's workspace screenshots directory; a
     * refused or missing file rejects.
     */
    BrowserScreenshot(
      sessionID: string,
      name: string
    ): Promise<serviceModels.BrowserScreenshotContent>;
    /**
     * Serves one computer-use observation screenshot's bytes by the file name
     * the observation wrote (add-computer-use, spec chat-tool-cards): the
     * browser read's same base64-PNG shape, resolved solely inside the
     * calling session's computer screenshots directory; a refused or missing
     * file rejects.
     */
    ComputerScreenshot(
      sessionID: string,
      name: string
    ): Promise<serviceModels.BrowserScreenshotContent>;
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
    GeneratedDocument(
      agentID: string,
      sessionID: string,
      name: string
    ): Promise<serviceModels.GeneratedDocumentContent>;
    /**
     * Exports one generated document to a destination the user picks in the
     * host's native save dialog (add-artifact-save-as design D1); the name is
     * refused exactly like GeneratedDocument before any dialog or filesystem
     * access, and the chosen destination path is the resolution. sessionID
     * locates the generation's session-scoped artifacts home exactly like
     * GeneratedDocument. A user cancellation rejects with
     * "save as: cancelled" and a host without a dialog service rejects with
     * "save as: no native dialog on this host" — the messages the frontend
     * matches on.
     */
    SaveGeneratedDocumentAs(
      agentID: string,
      sessionID: string,
      name: string
    ): Promise<string>;
    /** Reads one jailed workspace file of the agent. */
    WorkspaceFile(agentID: string, relPath: string): Promise<string>;
    /**
     * Serves one jailed workspace file's binary content (add-turn-artifacts
     * design D8): base64 bytes plus the byte size and a mime picked by
     * extension (`application/pdf` for `.pdf`, otherwise
     * `application/octet-stream`). The jail matches `WorkspaceFile` exactly;
     * a refused or missing path rejects.
     */
    WorkspaceFileBinary(
      agentID: string,
      relPath: string
    ): Promise<serviceModels.WorkspaceFileBinaryContent>;
    /** Lists one jailed workspace directory of the agent. */
    ListWorkspace(
      agentID: string,
      relPath: string
    ): Promise<serviceModels.WorkspaceEntry[] | null>;
    Unsubscribe(sessionID: string): Promise<void>;
    /** Re-delivers the in-flight turn's full state to every subscriber. */
    Resync(sessionID: string): Promise<void>;
    /** Reports whether the session has a turn in flight and its turn ID. */
    TurnState(sessionID: string): Promise<serviceModels.TurnStateResult>;
    ContextUsage(sessionID: string): Promise<agentsModels.ContextUsage>;
  };
  settings: {
    Providers(): Promise<serviceModels.ProviderView[] | null>;
    Provider(id: string): Promise<serviceModels.ProviderView>;
    CreateProvider(
      input: serviceModels.ProviderInput
    ): Promise<serviceModels.ProviderView>;
    UpdateProvider(
      id: string,
      input: serviceModels.ProviderInput
    ): Promise<serviceModels.ProviderView>;
    DeleteProvider(id: string): Promise<void>;
    ProviderModels(id: string): Promise<serviceModels.ModelCapability[] | null>;
    CreateModel(
      providerID: string,
      input: serviceModels.ProviderModelInput
    ): Promise<serviceModels.ProviderModelView | null>;
    UpdateModel(
      id: number,
      input: serviceModels.ProviderModelInput
    ): Promise<serviceModels.ProviderModelView | null>;
    DeleteModel(id: number): Promise<void>;
    ToggleModel(
      id: number,
      enabled: boolean
    ): Promise<serviceModels.ProviderModelView | null>;
    ListProviderModels(
      providerID: string
    ): Promise<(serviceModels.ProviderModelView | null)[] | null>;
    VerifyProviderDraft(
      input: serviceModels.ProviderInput,
      providerID: string
    ): Promise<serviceModels.VerifyResult>;
    VerifyProvider(id: string): Promise<serviceModels.VerifyResult>;
    Skills(): Promise<storeModels.Skill[] | null>;
    SetSkillEnabled(name: string, enabled: boolean): Promise<void>;
    Tools(): Promise<serviceModels.ToolEntry[] | null>;
    SetToolEnabled(name: string, enabled: boolean): Promise<void>;
    WebSearchProviders(): Promise<serviceModels.WebSearchProviderInfo[] | null>;
    WebSearchConfig(): Promise<serviceModels.WebSearchConfig>;
    // The write shape: a blank api_key on an entry carrying its stable ID
    // keeps the stored key backend-side (merge-by-ID, design D4).
    SaveWebSearchConfig(
      config: serviceModels.WebSearchConfigInput
    ): Promise<void>;
    BrowserConfig(): Promise<serviceModels.BrowserConfig>;
    SaveBrowserConfig(config: serviceModels.BrowserConfig): Promise<void>;
    /**
     * The Computer Use pane's surface (add-computer-use tasks 3.1, design
     * D6): the config round trip, the status aggregate (available flag,
     * per-permission grants with Manage deep links, driver download state,
     * supervision status), the Check again re-probe (restarting the driver
     * on a grant change), and the enable action's host-side prompt raise.
     * Server builds answer ComputerStatus with available=false and refuse
     * the save/prompt (design D9).
     */
    ComputerConfig(): Promise<serviceModels.ComputerConfigView>;
    SaveComputerConfig(view: serviceModels.ComputerConfigView): Promise<void>;
    ComputerStatus(): Promise<serviceModels.ComputerStatus>;
    CheckComputerPermissions(): Promise<serviceModels.ComputerStatus>;
    PromptComputerPermissions(): Promise<void>;
    /** Opens one of the pane's Manage deep links host-side; the webview
     * cannot open custom schemes itself, and only service-issued URLs pass
     * the allowlist (add-computer-use design D6). */
    OpenComputerManageURL(url: string): Promise<void>;
    BrowserProfiles(): Promise<serviceModels.BrowserProfile[] | null>;
    CreateBrowserProfile(name: string): Promise<void>;
    DeleteBrowserProfile(name: string): Promise<void>;
    /** Imports a profile directory; returns the final (possibly suffixed) name. */
    ImportBrowserProfile(sourcePath: string, name: string): Promise<string>;
    /**
     * Imports one detected Chrome profile in one click; the source resolves
     * from the directory name against this machine's Chrome installation
     * and the registry name derives backend-side (collision-suffixed).
     */
    ImportChromeProfile(dirName: string): Promise<string>;
    /** Lists this machine's Google Chrome profiles for one-click import. */
    DetectChromeProfiles(): Promise<
      serviceModels.ChromeProfileCandidate[] | null
    >;
    /** Saved logins, redacted — the password never crosses the bridge. */
    VaultLogins(): Promise<serviceModels.VaultLogin[] | null>;
    /** Stores (or updates, per origin+username pair) one login's password. */
    SaveVaultLogin(
      name: string,
      origin: string,
      username: string,
      password: string,
      addedBy: string
    ): Promise<void>;
    /** Removes one saved login; an unknown id is an error. */
    RemoveVaultLogin(id: string): Promise<void>;
    /** Reveals one saved login's password (settings-only affordance). */
    RevealVaultLogin(id: string): Promise<string>;
    /** The format's document presets, ordered by name (add-create-document D4). */
    ListDocumentPresets(
      format: string
    ): Promise<serviceModels.DocumentPresetEntry[] | null>;
    /** Stores a new document preset; a name collision in the format rejects. */
    CreateDocumentPreset(
      format: string,
      name: string,
      configJSON: string
    ): Promise<serviceModels.DocumentPresetEntry>;
    /** Rewrites a preset's name and config; the format is preserved. */
    UpdateDocumentPreset(
      id: string,
      name: string,
      configJSON: string
    ): Promise<serviceModels.DocumentPresetEntry>;
    /** Copies a preset to a new name within its format. */
    DuplicateDocumentPreset(
      id: string,
      newName: string
    ): Promise<serviceModels.DocumentPresetEntry>;
    /** Removes the preset; a dangling stored default resolves to none. */
    DeleteDocumentPreset(id: string): Promise<void>;
    /** The format's default preset name, empty when none is set. */
    DefaultDocumentPreset(format: string): Promise<string>;
    /** Stores the default preset name; empty clears it. */
    SetDefaultDocumentPreset(format: string, name: string): Promise<void>;
    /**
     * Stores one preset's uploaded skeleton document (add-document-editing
     * design D6): the base64 bytes land in the template store under the
     * preset's id and the row records the upload. The file name's extension
     * must match the preset's format (.docx / .pptx); PDF presets take no
     * template.
     */
    SetDocumentPresetTemplate(
      presetID: string,
      filename: string,
      base64Data: string
    ): Promise<void>;
    Hooks(): Promise<storeModels.Hook[] | null>;
    CreateHook(hook: storeModels.Hook): Promise<storeModels.Hook>;
    UpdateHook(hook: storeModels.Hook): Promise<storeModels.Hook>;
    DeleteHook(id: string): Promise<void>;
    /** The agent's assigned hooks in evaluation order. */
    AgentHooks(
      agentID: string
    ): Promise<serviceModels.AgentHookAssignment[] | null>;
    /** Links a hook to an agent at a position; re-assigning moves it. */
    AssignHook(
      agentID: string,
      hookID: string,
      position: number
    ): Promise<void>;
    /** Removes the link, returning the hook to the global tier. */
    UnassignHook(agentID: string, hookID: string): Promise<void>;
    /** The hook-evaluation audit trail, newest first. */
    HookExecutions(): Promise<storeModels.HookExecution[] | null>;
    MCPServers(): Promise<storeModels.MCPServer[] | null>;
    MCPServer(id: string): Promise<storeModels.MCPServer>;
    CreateMCPServer(
      server: storeModels.MCPServer
    ): Promise<storeModels.MCPServer>;
    UpdateMCPServer(
      server: storeModels.MCPServer
    ): Promise<storeModels.MCPServer>;
    DeleteMCPServer(id: string): Promise<void>;
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
    ProbeMCPServer(id: string): Promise<serviceModels.MCPServerView>;
    /**
     * Starts one server's OAuth browser flow (design D2) and returns
     * immediately; completion arrives as the mcp:oauth:completed event.
     */
    AuthorizeMCPServer(id: string): Promise<void>;
    /**
     * Revokes and clears one server's stored OAuth tokens (design D9),
     * returning the row in the authorization-required state.
     */
    DisconnectMCPServer(id: string): Promise<storeModels.MCPServer>;
    /**
     * Creates the service-tagged MCP server row(s) for one catalog connect
     * submission (add-integrations); the response is the redacted first
     * created row (family: one row per product).
     */
    ConnectIntegration(
      input: serviceModels.IntegrationConnectInput
    ): Promise<storeModels.MCPServer>;
    /** Removes an integration's server definition (a tagged MCPServer row). */
    DisconnectIntegration(id: string): Promise<void>;
    Gateway(): Promise<storeModels.GatewaySettings>;
    SaveGateway(settings: storeModels.GatewaySettings): Promise<void>;
  };
  gateway: {
    State(): Promise<string>;
    Start(): Promise<void>;
    Pause(): Promise<void>;
    Resume(): Promise<void>;
  };
  schedules: {
    List(): Promise<schedulerModels.ScheduleView[] | null>;
    Get(id: string): Promise<schedulerModels.ScheduleView>;
    Create(
      input: schedulerModels.ScheduleInput
    ): Promise<schedulerModels.ScheduleView>;
    Update(
      id: string,
      input: schedulerModels.ScheduleInput
    ): Promise<schedulerModels.ScheduleView>;
    SetEnabled(
      id: string,
      enabled: boolean
    ): Promise<schedulerModels.ScheduleView>;
    Delete(id: string): Promise<void>;
    RunNow(id: string): Promise<schedulerModels.ScheduleView>;
    Preview(expr: string): Promise<schedulerModels.Preview>;
  };
  /** The groups surface (add-agent-groups tasks 4.2): the generated
   * GroupsService binding's methods, structurally mirrored like every other
   * service section. */
  groups: {
    ListGroups(): Promise<storeModels.AgentGroup[] | null>;
    Group(groupID: string): Promise<storeModels.AgentGroup>;
    CreateGroup(
      name: string,
      title: string,
      instructions: string,
      memberAgentIDs: string[] | null
    ): Promise<storeModels.AgentGroup>;
    UpdateGroup(
      groupID: string,
      name: string,
      title: string,
      instructions: string
    ): Promise<storeModels.AgentGroup>;
    DeleteGroup(groupID: string): Promise<void>;
    Members(groupID: string): Promise<storeModels.AgentGroupMember[] | null>;
    AddMember(groupID: string, agentID: string): Promise<void>;
    RemoveMember(groupID: string, agentID: string): Promise<void>;
    SetLead(groupID: string, agentID: string): Promise<void>;
    TranscriptPage(
      groupID: string,
      beforeSeq: number,
      limit: number
    ): Promise<storeModels.AgentGroupMessage[] | null>;
    Tasks(groupID: string): Promise<storeModels.AgentGroupTask[] | null>;
    Send(req: groupsModels.SendRequest): Promise<void>;
    StopTask(taskID: string, reason: string): Promise<void>;
    ContinueTask(taskID: string, message: string): Promise<void>;
    ReassignTask(taskID: string, newOwnerAgentID: string): Promise<void>;
    CancelTask(taskID: string): Promise<void>;
  };
  /** The Channels pane's backend surface (add-telegram-channel task 1.4). */
  channels: {
    GetChannel(id: string): Promise<serviceModels.ChannelView>;
    SaveChannel(
      id: string,
      input: serviceModels.ChannelInput
    ): Promise<serviceModels.ChannelView>;
    SetEnabled(
      id: string,
      enabled: boolean
    ): Promise<serviceModels.ChannelView>;
    /** Every stored channel's secret-free view, ordered by id (task 9.3). */
    ListChannels(): Promise<serviceModels.ChannelView[] | null>;
    ListChats(): Promise<serviceModels.ChannelChatView[] | null>;
    ListPendingPairings(): Promise<storeModels.ChannelPairing[] | null>;
    ApprovePairing(telegramUserID: number): Promise<void>;
    DismissPairing(telegramUserID: number): Promise<void>;
    SetChatPinnedAgent(
      chatID: number,
      agentID: string
    ): Promise<serviceModels.ChannelChatView>;
    SetHomeChat(chatID: number): Promise<serviceModels.ChannelChatView>;
    SetChatFreeResponse(
      chatID: number,
      free: boolean
    ): Promise<serviceModels.ChannelChatView>;
  };
  /**
   * The Channels pane's Webhooks section (add-webhook-triggers tasks 7.1):
   * the generated WebhookService binding's methods, structurally mirrored
   * like every other service section. Secrets are write-only — the read
   * shapes carry presence and last-four hints only.
   */
  webhooks: {
    ListEndpoints(): Promise<serviceModels.WebhookEndpointView[] | null>;
    GetEndpoint(id: string): Promise<serviceModels.WebhookEndpointView>;
    CreateEndpoint(
      input: serviceModels.WebhookEndpointInput
    ): Promise<serviceModels.WebhookEndpointView>;
    UpdateEndpoint(
      id: string,
      input: serviceModels.WebhookEndpointInput
    ): Promise<serviceModels.WebhookEndpointView>;
    DeleteEndpoint(id: string): Promise<void>;
    SetEnabled(
      id: string,
      enabled: boolean
    ): Promise<serviceModels.WebhookEndpointView>;
    /** One endpoint's delivery log, newest first. */
    Deliveries(
      endpointID: string
    ): Promise<serviceModels.WebhookDeliveryView[] | null>;
    ListenerState(): Promise<serviceModels.WebhookListenerView>;
    /** Stores the listener address; empty resets to the loopback default. */
    SetListenerAddress(addr: string): Promise<void>;
    /** The self-test: a signed delivery through the real listener. */
    Test(endpointID: string): Promise<void>;
    /** The sender preset catalog with where-to-find-it hints. */
    Presets(): Promise<serviceModels.WebhookPresetView[] | null>;
  };
  knowledge: {
    /** Every cataloged document in name order; scope indexes are never listed. */
    List(): Promise<serviceModels.KnowledgeDocument[] | null>;
    /**
     * Stores one upload (add-knowledge-corpus D8): base64 data, scope
     * "global" or "agent" (an agent scope resolves agentID to its slug);
     * the corpus gate's rejections carry the reason.
     */
    Upload(
      filename: string,
      mime: string,
      data: string,
      scope: string,
      agentID: string,
      description: string
    ): Promise<serviceModels.KnowledgeDocument>;
    /** Removes one document — catalog row and stored file. */
    Delete(id: string): Promise<void>;
    /**
     * Promotes ("global") or demotes ("agent" + target agentID) one
     * document; the file move and catalog flip are one store operation.
     */
    SetScope(
      id: string,
      scope: string,
      agentID: string
    ): Promise<serviceModels.KnowledgeDocument>;
    /** Rewrites one document's description. */
    SetDescription(
      id: string,
      description: string
    ): Promise<serviceModels.KnowledgeDocument>;
    /** One document's base64-encoded bytes, for previews. */
    Content(id: string): Promise<string>;
  };
  /** The desktop-notification preferences surface (add-desktop-notifications
   * tasks 5.1/5.2): the settings singleton's round trip and the frontend's
   * focus reports for foreground suppression. */
  notifications: {
    GetNotificationSettings(): Promise<storeModels.NotificationSettings>;
    SaveNotificationSettings(
      settings: storeModels.NotificationSettings
    ): Promise<void>;
    /** Mirrors the viewed context for foreground suppression: kind is
     * "session" or "group" and id names the conversation on screen; an empty
     * id or an unknown kind clears the mirror. */
    ReportFocus(kind: string, id: string): Promise<void>;
  };
  windowTheme: {
    SetLight(light: boolean): Promise<void>;
  };
  /**
   * Registers a handler for a backend-emitted event (e.g. a session event
   * channel or the gateway state event). Returns an unsubscribe function.
   */
  onEvent(name: string, handler: EventHandler): Unsubscribe;
}

export function createWailsTransport(): Transport {
  return {
    appInfo: appInfoBindings,
    agents: agentsBindings,
    chat: {
      ...chatBindings,
      /** Exports one generated document through the host's native save dialog
       * (add-artifact-save-as); resolves with the user-chosen destination. */
      SaveGeneratedDocumentAs: (
        agentID: string,
        sessionID: string,
        name: string
      ): Promise<string> =>
        chatBindings.SaveGeneratedDocumentAs(agentID, sessionID, name),
      TurnState: (sessionID: string): Promise<serviceModels.TurnStateResult> =>
        chatBindings.TurnState(sessionID),
    },
    settings: settingsBindings,
    gateway: gatewayBindings,
    groups: groupsBindings,
    knowledge: knowledgeBindings,
    schedules: scheduleBindings,
    channels: channelBindings,
    webhooks: webhookBindings,
    notifications: notificationsBindings,
    windowTheme: windowThemeModule,
    onEvent: (name: string, handler: EventHandler): Unsubscribe =>
      Events.On(name, (ev: Events.WailsEvent): void => handler(ev.data)),
  };
}

export const wailsTransport: Transport = createWailsTransport();
