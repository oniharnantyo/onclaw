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
import type * as schedulerModels from "../../bindings/github.com/oniharnantyo/buddies/internal/scheduler/models";
import type * as sessionModels from "../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type * as serviceModels from "../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type * as storeModels from "../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import * as agentsBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/agentsservice";
import * as appInfoBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/appinfo";
import * as chatBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/chatservice";
import * as channelBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/channelservice";
import * as gatewayBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/gatewayservice";
import * as knowledgeBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/knowledgeservice";
import * as scheduleBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/scheduleservice";
import * as settingsBindings from "../../bindings/github.com/oniharnantyo/buddies/internal/services/settingsservice";
import * as windowThemeModule from "../../bindings/github.com/oniharnantyo/buddies/windowthemeservice";

/** Unsubscribe function returned by event subscriptions. */
export type Unsubscribe = () => void;

/**
 * One decoded event payload at the transport boundary: the session channel
 * carries SessionEvents, the gateway channel carries GatewayStateEvents, the
 * MCP OAuth channel carries MCPOAuthCompletedEvents, the schedules channel
 * carries SchedulesChangedEvents, the turns channel carries
 * TurnsChangedEvents, and the channels channels carry the channel change and
 * state events. The runtime hands all of them to `onEvent`, so this is
 * the single documented place where the domains meet before the client
 * narrows them.
 */
export type EventPayload =
  | sessionModels.Event
  | serviceModels.ChannelChangedEvent
  | serviceModels.ChannelStateEvent
  | serviceModels.GatewayStateEvent
  | serviceModels.KnowledgeChangedEvent
  | serviceModels.MCPOAuthCompletedEvent
  | serviceModels.SchedulesChangedEvent
  | serviceModels.TurnsChangedEvent;

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
    /** Stores one upload (base64 data); returns its capability metadata. */
    SaveAttachment(
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
     * inside the screenshots directory; a refused or missing file rejects.
     */
    BrowserScreenshot(
      name: string
    ): Promise<serviceModels.BrowserScreenshotContent>;
    /**
     * Serves one generated document's bytes by the file name the
     * create_document tool reported (add-create-document D7): base64 plus
     * the byte size. The backend refuses any name carrying directory
     * components before touching the filesystem and resolves it solely
     * inside the agent's workspace; a refused or missing file rejects.
     */
    GeneratedDocument(
      agentID: string,
      name: string
    ): Promise<serviceModels.GeneratedDocumentContent>;
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
    SaveWebSearchConfig(config: serviceModels.WebSearchConfig): Promise<void>;
    BrowserConfig(): Promise<serviceModels.BrowserConfig>;
    SaveBrowserConfig(config: serviceModels.BrowserConfig): Promise<void>;
    BrowserProfiles(): Promise<serviceModels.BrowserProfile[] | null>;
    CreateBrowserProfile(name: string): Promise<void>;
    DeleteBrowserProfile(name: string): Promise<void>;
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
     * Dials the stored server definition on demand, discovers its tools, and
     * persists the outcome onto the row's status fields. A failed probe
     * reports on the row (status "error" with the reason) instead of
     * rejecting; it never mutates the definition.
     */
    ProbeMCPServer(id: string): Promise<storeModels.MCPServer>;
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
      TurnState: (sessionID: string): Promise<serviceModels.TurnStateResult> =>
        chatBindings.TurnState(sessionID),
    },
    settings: settingsBindings,
    gateway: gatewayBindings,
    knowledge: knowledgeBindings,
    schedules: scheduleBindings,
    channels: channelBindings,
    windowTheme: windowThemeModule,
    onEvent: (name: string, handler: EventHandler): Unsubscribe =>
      Events.On(name, (ev: Events.WailsEvent): void => handler(ev.data)),
  };
}

export const wailsTransport: Transport = createWailsTransport();
