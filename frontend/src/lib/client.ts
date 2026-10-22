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
  CreateAgentInput,
  GatewayStateEvent,
  Metadata,
  ModelCapability,
  PromptDocuments,
  ProviderInput,
  ProviderModelInput,
  ProviderModelView,
  ProviderView,
  ToolEntry,
  VerifyResult,
  WebSearchConfig,
  WebSearchProviderInfo,
  WorkspaceEntry,
} from "../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type {
  Agent,
  GatewaySettings,
  Hook,
  HookExecution,
  MCPServer,
  Session,
  Skill,
} from "../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  Attachment as SessionAttachment,
  Event as SessionEvent,
} from "../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";

import {
  GATEWAY_STATE_EVENT,
  MCP_OAUTH_COMPLETED_EVENT,
  SCHEDULES_CHANGED_EVENT,
  SETTINGS_OPEN_EVENT,
  sessionEventName,
  type MCPOAuthCompletedEvent,
  type SchedulesChangedEvent,
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

export type { ProviderModelInput, ProviderModelView };

/** The generated scheduler binding types (add-scheduler D13), re-exported so
 * feature code imports one surface. */
export type { Preview, ScheduleInput, ScheduleView };

/** Handler receiving one schedules change notification (design D10). */
export type SchedulesChangedHandler = (event: SchedulesChangedEvent) => void;

/** Handler receiving one gateway state change. */
export type GatewayStateHandler = (event: GatewayStateEvent) => void;

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
  };
  chat: {
    sessions(agentID: string): Promise<Session[]>;
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
     */
    saveAttachment(
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
     * solely inside the screenshots directory; a refused or missing file
     * rejects.
     */
    browserScreenshot(name: string): Promise<BrowserScreenshotContent>;
    /** Reads one jailed workspace file of the agent (design D8). */
    workspaceFile(agentID: string, relPath: string): Promise<string>;
    /** Lists one jailed workspace directory of the agent (design D8). */
    listWorkspace(agentID: string, relPath: string): Promise<WorkspaceEntry[]>;
    /**
     * Attaches to the session's live event channel and invokes the handler
     * for each event (deltas, tool activity, turn completion, errors).
     * Resolves once the backend subscription is in place; the returned
     * function detaches both locally and on the backend.
     */
    subscribe(
      sessionID: string,
      handler: SessionEventHandler
    ): Promise<Unsubscribe>;
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
    /** The persisted web_search failover chain (design D8). */
    webSearchConfig(): Promise<WebSearchConfig>;
    /**
     * Create-or-replaces the web_search failover chain. Rejects when an
     * entry's provider is unknown or its required credential is missing.
     */
    saveWebSearchConfig(config: WebSearchConfig): Promise<void>;
    /** The persisted browser tool configuration (spec add-browser-tool D9). */
    browserConfig(): Promise<BrowserConfig>;
    /** Stores the browser tool configuration; validates name and timeouts. */
    saveBrowserConfig(config: BrowserConfig): Promise<void>;
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
     * Dials the stored server definition on demand, discovers its tools, and
     * persists the outcome onto the row's status fields. A failed probe
     * reports on the row (status "error" with the reason) instead of
     * rejecting; it never mutates the definition.
     */
    probeMCPServer(id: string): Promise<MCPServer>;
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
    },
    chat: {
      sessions: async (agentID: string): Promise<Session[]> =>
        unwrap(await transport.chat.Sessions(agentID)),
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
        name: string,
        mime: string,
        data: string
      ): Promise<SessionAttachment> =>
        transport.chat.SaveAttachment(name, mime, data),
      attachmentContent: (key: string): Promise<AttachmentContent> =>
        transport.chat.AttachmentContent(key),
      browserScreenshot: async (
        name: string
      ): Promise<BrowserScreenshotContent> =>
        unwrap(await transport.chat.BrowserScreenshot(name)),
      workspaceFile: (agentID: string, relPath: string): Promise<string> =>
        transport.chat.WorkspaceFile(agentID, relPath),
      listWorkspace: async (
        agentID: string,
        relPath: string
      ): Promise<WorkspaceEntry[]> =>
        unwrap(await transport.chat.ListWorkspace(agentID, relPath)),
      subscribe: async (
        sessionID: string,
        handler: SessionEventHandler
      ): Promise<Unsubscribe> => {
        await transport.chat.Subscribe(sessionID);

        const off = transport.onEvent(
          sessionEventName(sessionID),
          (data: EventPayload): void => {
            // SAFETY: sessionEventName channel only carries SessionEvents.
            handler(data as SessionEvent);
          }
        );

        return (): void => {
          off();
          void transport.chat.Unsubscribe(sessionID);
        };
      },
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
      saveWebSearchConfig: (config: WebSearchConfig): Promise<void> =>
        transport.settings.SaveWebSearchConfig(config),
      browserConfig: (): Promise<BrowserConfig> =>
        transport.settings.BrowserConfig(),
      saveBrowserConfig: (config: BrowserConfig): Promise<void> =>
        transport.settings.SaveBrowserConfig(config),
      // The bridge maps an empty Go slice to null; the profile list iterates
      // the result, so normalize it once here.
      browserProfiles: async (): Promise<BrowserProfile[]> =>
        unwrap(await transport.settings.BrowserProfiles()),
      createBrowserProfile: (name: string): Promise<void> =>
        transport.settings.CreateBrowserProfile(name),
      deleteBrowserProfile: (name: string): Promise<void> =>
        transport.settings.DeleteBrowserProfile(name),
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
      probeMCPServer: (id: string): Promise<MCPServer> =>
        transport.settings.ProbeMCPServer(id),
      authorizeMCPServer: (id: string): Promise<void> =>
        transport.settings.AuthorizeMCPServer(id),
      disconnectMCPServer: (id: string): Promise<MCPServer> =>
        transport.settings.DisconnectMCPServer(id),
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
    },
    windowTheme: {
      setLight: (light: boolean): Promise<void> =>
        transport.windowTheme.SetLight(light),
    },
  };
}

/** Application-wide client bound to the real Wails transport. */
export const client = createClient();
