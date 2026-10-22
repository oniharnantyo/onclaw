import { useEffect, useRef, useState } from "react";
import type { ReactElement } from "react";
import { ArrowDown, ArrowUp, Trash2 } from "lucide-react";
import type { AvatarFullConfig } from "react-nice-avatar";

import type {
  Hook,
  MCPServer,
  Skill,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import type {
  AgentHookAssignment,
  ModelCapability,
  ProviderView,
  ToolEntry,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import { AgentAvatar, FormField, IconButton } from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Chip } from "../../../components/ui/Chip";
import { Combobox } from "../../../components/ui/Combobox";
import { Icon } from "../../../components/ui/Icon";
import { Input } from "../../../components/ui/Input";
import { MicroLabel } from "../../../components/ui/MicroLabel";
import { OptionChips } from "../../../components/ui/OptionChips";
import type { OptionChipOption } from "../../../components/ui/OptionChips";
import { Select } from "../../../components/ui/Select";
import {
  StatusDot,
  type StatusDotStatus,
} from "../../../components/ui/StatusDot";
import { Textarea } from "../../../components/ui/Textarea";
import { Toggle } from "../../../components/ui/Toggle";
import { cn } from "../../../lib/cn";
import { client } from "../../../lib/client";
import { catalogContextWindow } from "../modelContextWindow";
import { parseOptionalInt } from "../parseOptionalInt";
import { PROMPTS_STATUS } from "../types";
import type { AgentConfigPaneProps } from "../types";
import { AvatarPicker, generateRandomAvatar } from "./AvatarPicker";
import { DeleteAgentModal } from "./DeleteAgentModal";
import { IdentitySection } from "./IdentitySection";
import { McpServerList } from "./McpServerList";

/**
 * Full agent configuration (tasks 6.1–6.3), laid out as a detail pane inside
 * the agents master-detail layout: a top header with avatar, name, and presence
 * status, save and delete action triggers, and a tab strip over five panels —
 * Identity, Model, Capabilities, Hooks, Prompts.
 *
 * Replaces the modal dialog shell (design D8); every panel stays mounted with
 * Tailwind's `hidden` so drafts survive tab switches.
 *
 * Model selection (design D6): the provider's stored enabled rows are the only
 * model choices; the legacy free-text "Custom model ID…" option is dropped.
 */

/** The edit-mode tabs, in onclaw-v2's order. */
type TabKey = "identity" | "model" | "capabilities" | "hooks" | "prompts";

const TABS: readonly { key: TabKey; label: string }[] = [
  { key: "identity", label: "Identity" },
  { key: "model", label: "Model" },
  { key: "capabilities", label: "Capabilities" },
  { key: "hooks", label: "Hooks" },
  { key: "prompts", label: "Prompts" },
];

/** Per-field validation errors in the Advanced section. */
type ParamName = "temperature" | "maxTokens" | "contextWindow";

type ParamErrors = Partial<Record<ParamName, string>>;

export function AgentConfigPane({
  agent,
  onClose,
  onSaved,
  onDeleted,
}: AgentConfigPaneProps): ReactElement {
  const [tab, setTab] = useState<TabKey>("identity");
  const [providers, setProviders] = useState<ProviderView[]>([]);

  const [modelCapabilities, setModelCapabilities] = useState<{
    providerID: string;
    models: ModelCapability[];
  }>({ providerID: "", models: [] });

  const [skills, setSkills] = useState<Skill[]>([]);
  const [tools, setTools] = useState<ToolEntry[]>([]);
  const [mcpServers, setMcpServers] = useState<MCPServer[]>([]);
  const [hooks, setHooks] = useState<Hook[]>([]);
  const [assignedHooks, setAssignedHooks] = useState<AgentHookAssignment[]>([]);
  const [hookToAdd, setHookToAdd] = useState("");

  // Hook ids as loaded from the backend, so Save can tell a removal from a
  // hook that simply was never assigned.
  const loadedHookIDsRef = useRef<string[]>([]);

  const [providerID, setProviderID] = useState(agent.ProviderID);
  const [model, setModel] = useState(agent.Model);
  // The agent's default reasoning effort (task 2.4, spec agent-management):
  // options ride ONLY the selected model's effort set, and a switch to a
  // model without effort support clears the choice.
  const [effort, setEffort] = useState(agent.Effort || "");
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [temperature, setTemperature] = useState(String(agent.Temperature));
  const [fieldErrors, setFieldErrors] = useState<ParamErrors>({});

  // A stored override reads as hand-edited so the catalog never clobbers it;
  // a fresh 0 auto-fills from the model's known context limit until touched.
  const [contextWindow, setContextWindow] = useState(
    agent.ContextWindow ? String(agent.ContextWindow) : ""
  );

  const [contextWindowTouched, setContextWindowTouched] = useState(
    agent.ContextWindow > 0
  );

  // The stored config hydrates as-is; an agent without one starts from a
  // fresh random roll (onclaw-v2's edit behavior) — the preview always
  // shows exactly what saving will store.
  const [avatar, setAvatar] = useState<AvatarFullConfig>(() =>
    agent.Avatar && Object.keys(agent.Avatar).length > 0
      ? agent.Avatar
      : generateRandomAvatar()
  );

  // Identity edits: name and description are this pane's job (the list
  // reflects both on save); slug and brief stay as loaded — the service
  // keeps the stored values no matter what the caller sends.
  const [name, setName] = useState(agent.Name);
  const [description, setDescription] = useState(agent.Description || "");
  const [nameError, setNameError] = useState<string | null>(null);

  const [maxTokens, setMaxTokens] = useState(
    agent.MaxTokens ? String(agent.MaxTokens) : ""
  );

  const [selectedSkills, setSelectedSkills] = useState<string[]>(
    agent.Skills ?? []
  );

  const [selectedTools, setSelectedTools] = useState<string[]>(
    agent.Tools ?? []
  );

  const [selectedMcp, setSelectedMcp] = useState<string[]>(
    agent.MCPServers ?? []
  );

  // Subagents capability (task 5.6): the agent tool's on/off switch, stored
  // on the agent row itself rather than an allowlist.
  const [subagents, setSubagents] = useState(agent.SubagentsEnabled);

  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  // Delete opens the confirmation modal; a confirmed delete calls onDeleted.
  const [deleting, setDeleting] = useState(false);

  useEffect(() => {
    let mounted = true;

    void client.settings.providers().then((p: ProviderView[]): void => {
      if (mounted) setProviders(p);
    });
    void client.settings.skills().then((s: Skill[]): void => {
      if (mounted) setSkills(s);
    });
    void client.settings.tools().then((t: ToolEntry[]): void => {
      if (mounted) setTools(t);
    });
    void client.settings.mcpServers().then((m: MCPServer[]): void => {
      if (mounted) setMcpServers(m);
    });
    void client.settings.hooks().then((h: Hook[]): void => {
      if (mounted) setHooks(h);
    });
    // The agent's assigned hooks arrive in evaluation order (assigned tier
    // before the global tier), so the list renders top-down as the dispatcher
    // will run them.
    void client.settings
      .agentHooks(agent.ID)
      .then((a: AgentHookAssignment[]): void => {
        if (!mounted) return;
        loadedHookIDsRef.current = a.map(
          (x: AgentHookAssignment): string => x.HookID
        );
        setAssignedHooks(a);
      });

    return (): void => {
      mounted = false;
    };
  }, [agent.ID]);

  // Models populate whenever the chosen provider changes.
  useEffect(() => {
    if (!providerID) return;

    let mounted = true;

    void client.settings
      .providerModels(providerID)
      .then((m: ModelCapability[]): void => {
        if (mounted) setModelCapabilities({ providerID, models: m });
      })
      .catch((): void => {
        if (mounted) setModelCapabilities({ providerID, models: [] });
      });

    return (): void => {
      mounted = false;
    };
  }, [providerID]);

  const models =
    modelCapabilities.providerID === providerID ? modelCapabilities.models : [];

  const providerType = providers.find(
    (p: ProviderView): boolean => p.ID === providerID
  )?.Type;

  /** The selected model's supported effort set; empty hides the control and
   * invalidates any saved effort (task 2.4). */
  const efforts: string[] =
    models.find((m: ModelCapability): boolean => m.ID === model)?.Efforts ?? [];

  const catalogLimit =
    providerType !== undefined && model
      ? catalogContextWindow(providerType, model)
      : null;

  // Auto-fill the context window from the static catalog until the user
  // edits it. Derived during render rather than synced by an effect.
  const effectiveContextWindow =
    !contextWindowTouched && catalogLimit !== null
      ? String(catalogLimit)
      : contextWindow;

  const handleProviderChange = (value: string): void => {
    setProviderID(value);
    setModel("");
    setEffort("");
    setContextWindowTouched(false);
    setContextWindow("");
    setFieldErrors({});
  };

  /** Keeps the effort only when the newly picked model supports it. */
  const syncEffort = (nextEfforts: string[]): void => {
    if (nextEfforts.length === 0 || !nextEfforts.includes(effort)) {
      setEffort("");
    }
  };

  const handleModelChange = (value: string): void => {
    setModel(value);
    syncEffort(
      models.find((m: ModelCapability): boolean => m.ID === value)?.Efforts ??
        []
    );
  };

  const resetContextWindow = (): void => {
    setContextWindowTouched(false);
    setContextWindow("");
  };

  // Empty list = every catalog tool enabled (default state).
  const allToolsOn = selectedTools.length === 0;

  /** The chips' pressed set: the default renders every tool pressed. */
  const toolValue = allToolsOn
    ? tools.map((t: ToolEntry): string => t.Name)
    : selectedTools;

  const handleToolsChange = (next: string[]): void => {
    setSelectedTools(next);
  };

  /** Lifecycle event of a hook, resolved from the global definitions. */
  const hookEvent = (hookID: string): string =>
    hooks.find((h: Hook): boolean => h.ID === hookID)?.Event ?? "";

  const assignHook = (hookID: string): void => {
    const hook = hooks.find((h: Hook): boolean => h.ID === hookID);

    if (!hook) return;

    setAssignedHooks((prev: AgentHookAssignment[]): AgentHookAssignment[] => [
      ...prev,
      { HookID: hook.ID, HookName: hook.Name, Position: prev.length },
    ]);
    setHookToAdd("");
  };

  /** Swaps one assignment with its neighbour. */
  const moveHook = (index: number, target: number): void => {
    setAssignedHooks((prev: AgentHookAssignment[]): AgentHookAssignment[] => {
      if (target < 0 || target >= prev.length) return prev;

      const next = [...prev];
      const [moved] = next.splice(index, 1);

      next.splice(target, 0, moved);

      return next;
    });
  };

  const unassignHook = (hookID: string): void => {
    setAssignedHooks((prev: AgentHookAssignment[]): AgentHookAssignment[] =>
      prev.filter((a: AgentHookAssignment): boolean => a.HookID !== hookID)
    );
  };

  const handleSave = async (): Promise<void> => {
    if (!name.trim()) {
      setNameError("Agent name is required");
      setTab("identity");

      return;
    }

    setNameError(null);

    const errors: ParamErrors = {};

    const parsedTemp = parseFloat(temperature);

    if (Number.isNaN(parsedTemp) || parsedTemp < 0 || parsedTemp > 2) {
      errors.temperature = "Temperature must be between 0.0 and 2.0";
    }

    let parsedMax = 0;

    if (maxTokens.trim()) {
      const candidate = parseOptionalInt(maxTokens);

      if (candidate === null) {
        errors.maxTokens = "Max tokens must be a positive integer";
      } else {
        parsedMax = candidate;
      }
    }

    let parsedContext = 0;

    if (effectiveContextWindow.trim()) {
      const candidate = parseOptionalInt(effectiveContextWindow);

      if (candidate === null) {
        errors.contextWindow =
          "Context window must be a positive integer (tokens)";
      } else {
        parsedContext = candidate;
      }
    }

    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);
      setAdvancedOpen(true);

      return;
    }

    setFieldErrors({});
    setSubmitting(true);
    setError(null);

    try {
      const saved = await client.agents.update({
        ...agent,
        Name: name.trim(),
        Description: description,
        Avatar: avatar,
        ProviderID: providerID,
        Model: model,
        Effort: effort,
        Temperature: parsedTemp,
        MaxTokens: parsedMax,
        ContextWindow: parsedContext,
        Skills: selectedSkills,
        Tools: selectedTools,
        MCPServers: selectedMcp,
        SubagentsEnabled: subagents,
      });

      const keptIDs = assignedHooks.map(
        (a: AgentHookAssignment): string => a.HookID
      );

      for (const id of loadedHookIDsRef.current.filter(
        (previous: string): boolean => !keptIDs.includes(previous)
      )) {
        await client.settings.unassignHook(agent.ID, id);
      }

      for (const [position, assignment] of assignedHooks.entries()) {
        await client.settings.assignHook(agent.ID, assignment.HookID, position);
      }

      loadedHookIDsRef.current = keptIDs;

      onSaved(saved);
      onClose?.();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to save agent");
    } finally {
      setSubmitting(false);
    }
  };

  const generating = agent.PromptsStatus === PROMPTS_STATUS.GENERATING;
  const failed = agent.PromptsStatus === PROMPTS_STATUS.FAILED;

  const presence: StatusDotStatus = failed
    ? "error"
    : generating
      ? "running"
      : "idle";

  const modelOptions = [
    ...models.map((m: ModelCapability) => ({
      value: m.ID,
      label: m.ID,
    })),
    ...(model && !models.some((m: ModelCapability) => m.ID === model)
      ? [{ value: model, label: model }]
      : []),
  ];

  return (
    <div
      className="flex h-full min-h-0 flex-1 flex-col overflow-hidden"
      data-testid="agent-config-pane"
    >
      {/* Pane header with avatar, name, presence status dot, delete and save triggers */}
      <header className="flex h-14 shrink-0 items-center justify-between border-b border-linesoft px-4 md:px-6">
        <div className="flex min-w-0 items-center gap-3">
          <AgentAvatar id={agent.ID} avatar={avatar} size={36} />
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h2
                className="truncate text-base font-semibold text-fg"
                data-testid="config-header-name"
              >
                {name.trim() || agent.Name}
              </h2>
              <StatusDot status={presence} live className="shrink-0" />
            </div>
            <p className="truncate font-mono text-xs text-muted">
              {agent.Slug}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="destructive"
            size="sm"
            data-testid="btn-config-delete-agent"
            disabled={submitting}
            onClick={() => setDeleting(true)}
          >
            Delete
          </Button>
          <Button
            size="sm"
            data-testid="btn-save-agent"
            disabled={submitting}
            onClick={() => void handleSave()}
          >
            {submitting ? "Saving..." : "Save changes"}
          </Button>
        </div>
      </header>

      {/* Pane body: scrollable tabs and sections */}
      <div className="od-scroll min-h-0 flex-1 overflow-y-auto p-4 md:p-6">
        {error && (
          <div className="mb-4" role="alert">
            <Alert tone="danger" testId="config-error">
              {error}
            </Alert>
          </div>
        )}

        {/* Tab strip */}
        <div
          role="tablist"
          aria-label="Agent configuration"
          className="mb-6 flex border-b border-line"
        >
          {TABS.map((t: { key: TabKey; label: string }): ReactElement => (
            <button
              key={t.key}
              type="button"
              role="tab"
              aria-selected={tab === t.key}
              data-testid={`config-tab-${t.key}`}
              onClick={(): void => setTab(t.key)}
              className={cn(
                "border-b-2 px-4 py-2 text-[13px] font-medium transition-colors",
                tab === t.key
                  ? "border-accent text-fg font-semibold"
                  : "border-transparent text-muted hover:text-fg"
              )}
            >
              {t.label}
            </button>
          ))}
        </div>

        {/* --- Identity: the stored procedural face plus immutable context. */}
        <div
          role="tabpanel"
          data-testid="config-section-identity"
          className={cn("max-w-xl space-y-6", tab !== "identity" && "hidden")}
        >
          <section data-testid="config-section-avatar" className="space-y-1.5">
            <MicroLabel>Identity & Persona</MicroLabel>
            <AvatarPicker value={avatar} onChange={setAvatar} />
          </section>

          <div className="space-y-3">
            <div className="space-y-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] p-3">
              <FormField
                id="config-agent-name"
                label="Agent name"
                message={nameError ?? undefined}
                invalid={nameError !== null}
              >
                <Input
                  id="config-agent-name"
                  data-testid="input-config-name"
                  invalid={nameError !== null}
                  value={name}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>): void => {
                    setName(e.target.value);
                    setNameError(null);
                  }}
                  placeholder="e.g. Scout"
                />
              </FormField>

              <FormField
                id="config-agent-description"
                label={
                  <>
                    Description{" "}
                    <span className="font-normal text-muted">
                      (optional summary)
                    </span>
                  </>
                }
              >
                <Textarea
                  id="config-agent-description"
                  data-testid="input-config-description"
                  rows={2}
                  value={description}
                  onChange={(e: React.ChangeEvent<HTMLTextAreaElement>): void =>
                    setDescription(e.target.value)
                  }
                  placeholder="One or two lines shown on the agent's entry..."
                />
              </FormField>

              <div className="flex items-baseline justify-between gap-3 border-t border-linesoft pt-3">
                <span className="text-[12px] font-medium text-fg2">Slug</span>
                <span
                  data-testid="config-readonly-slug"
                  title="Fixed at creation"
                  className="min-w-0 truncate font-mono text-[13px] text-fg2"
                >
                  {agent.Slug}
                </span>
              </div>

              <div className="space-y-1">
                <span className="text-[12px] font-medium text-fg2">
                  Goal & behavior brief{" "}
                  <span className="font-normal text-muted">
                    (fixed at creation)
                  </span>
                </span>
                <p
                  data-testid="config-readonly-brief"
                  className="whitespace-pre-line text-[13px] leading-relaxed text-fg2"
                >
                  {agent.Brief}
                </p>
              </div>
            </div>
            <p className="text-[11px] leading-4 text-muted">
              Slug and brief drive prompt generation and are fixed at creation.
              Edit the generated documents on the Prompts tab.
            </p>
          </div>
        </div>

        {/* --- Model: provider/model pick plus Advanced collapse (Design D6: stored rows only). */}
        <div
          role="tabpanel"
          data-testid="config-section-model"
          className={cn("max-w-xl space-y-6", tab !== "model" && "hidden")}
        >
          <section className="space-y-4">
            <FormField id="config-provider" label="Provider">
              <Select
                id="config-provider"
                data-testid="select-config-provider"
                value={providerID}
                onChange={(e: React.ChangeEvent<HTMLSelectElement>): void =>
                  handleProviderChange(e.target.value)
                }
              >
                <option value="">Select a provider...</option>
                {providers.map((p: ProviderView) => (
                  <option key={p.ID} value={p.ID}>
                    {p.Name}
                  </option>
                ))}
              </Select>
            </FormField>

            <FormField id="config-model" label="Model">
              <Combobox
                id="config-model"
                data-testid="select-config-model"
                options={modelOptions}
                value={model}
                onChange={handleModelChange}
                placeholder={
                  models.length === 0
                    ? "No models available"
                    : "Select a model..."
                }
                searchPlaceholder="Search models..."
                disabled={models.length === 0 && !model}
              />
            </FormField>

            {efforts.length > 0 && (
              <FormField
                id="config-effort"
                label={
                  <>
                    Default reasoning effort{" "}
                    <span className="font-normal text-muted">
                      (used when a session sets none)
                    </span>
                  </>
                }
              >
                <Select
                  id="config-effort"
                  data-testid="select-config-effort"
                  value={effort}
                  onChange={(e: React.ChangeEvent<HTMLSelectElement>): void =>
                    setEffort(e.target.value)
                  }
                >
                  <option value="">Provider default</option>
                  {efforts.map((e: string) => (
                    <option key={e} value={e}>
                      {e}
                    </option>
                  ))}
                </Select>
              </FormField>
            )}
          </section>

          {/* Bordered collapsible — sampling parameters stay out of the pane until asked for. */}
          <div
            className="rounded-lg border border-line p-3"
            data-testid="config-section-params"
          >
            <button
              type="button"
              data-testid="btn-toggle-advanced"
              aria-expanded={advancedOpen}
              onClick={(): void => setAdvancedOpen((prev: boolean) => !prev)}
              className="flex w-full items-center justify-between text-left text-[13px] font-medium text-fg"
            >
              <span>Advanced model configuration</span>
              <Icon
                name={advancedOpen ? "up" : "down"}
                size={16}
                className="text-muted"
              />
            </button>

            {advancedOpen && (
              <div className="mt-3 space-y-4 border-t border-line pt-3">
                <div className="flex flex-col gap-1.5">
                  <div className="flex items-baseline justify-between">
                    <label
                      htmlFor="config-temperature"
                      className="text-[12px] font-medium text-fg2"
                    >
                      Temperature
                    </label>
                    <span className="font-mono text-[12px] text-fg2">
                      {Number(temperature).toFixed(2)}
                    </span>
                  </div>
                  <input
                    id="config-temperature"
                    data-testid="input-config-temperature"
                    type="range"
                    min="0"
                    max="2"
                    step="0.05"
                    value={temperature}
                    onChange={(
                      e: React.ChangeEvent<HTMLInputElement>
                    ): void => {
                      setTemperature(e.target.value);
                      setFieldErrors((prev: ParamErrors): ParamErrors => {
                        if (!prev.temperature) return prev;
                        const next = { ...prev };
                        delete next.temperature;

                        return next;
                      });
                    }}
                    className="h-1.5 w-full cursor-pointer"
                  />
                  <div className="flex justify-between font-mono text-[10px] text-muted">
                    <span>0.0 (precise)</span>
                    <span>1.0 (standard)</span>
                    <span>2.0 (creative)</span>
                  </div>
                  {fieldErrors.temperature && (
                    <p
                      className="text-xs text-dangertext"
                      role="alert"
                      data-testid="field-error-config-temperature"
                    >
                      {fieldErrors.temperature}
                    </p>
                  )}
                </div>

                <FormField
                  id="config-max-tokens"
                  label={
                    <>
                      Max tokens{" "}
                      <span className="font-normal text-muted">
                        (0 = provider default)
                      </span>
                    </>
                  }
                  message={fieldErrors.maxTokens}
                  invalid={fieldErrors.maxTokens !== undefined}
                >
                  <Input
                    id="config-max-tokens"
                    data-testid="input-config-max-tokens"
                    type="number"
                    min="1"
                    placeholder="e.g. 4096"
                    invalid={fieldErrors.maxTokens !== undefined}
                    value={maxTokens}
                    onChange={(
                      e: React.ChangeEvent<HTMLInputElement>
                    ): void => {
                      setMaxTokens(e.target.value);
                      setFieldErrors((prev: ParamErrors): ParamErrors => {
                        if (!prev.maxTokens) return prev;
                        const next = { ...prev };
                        delete next.maxTokens;

                        return next;
                      });
                    }}
                  />
                </FormField>

                <div className="flex flex-col gap-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <label
                      htmlFor="config-context-window"
                      className="text-[12px] font-medium text-fg2"
                    >
                      Context window
                    </label>
                    <button
                      type="button"
                      data-testid="btn-reset-config-context-window"
                      onClick={resetContextWindow}
                      className="text-[11px] font-medium text-accent transition-colors hover:text-[var(--accent-hover)]"
                    >
                      Reset to auto
                    </button>
                  </div>
                  <div className="relative">
                    <Input
                      id="config-context-window"
                      data-testid="input-config-context-window"
                      type="number"
                      min="1"
                      step="1"
                      className={effectiveContextWindow ? "pr-14" : undefined}
                      invalid={fieldErrors.contextWindow !== undefined}
                      placeholder={
                        catalogLimit === null
                          ? "Auto (model default)"
                          : String(catalogLimit)
                      }
                      value={effectiveContextWindow}
                      onChange={(
                        e: React.ChangeEvent<HTMLInputElement>
                      ): void => {
                        setContextWindowTouched(true);
                        setContextWindow(e.target.value);
                        setFieldErrors((prev: ParamErrors): ParamErrors => {
                          if (!prev.contextWindow) return prev;
                          const next = { ...prev };
                          delete next.contextWindow;

                          return next;
                        });
                      }}
                    />
                    {effectiveContextWindow && (
                      <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center font-mono text-[11px] text-muted">
                        tokens
                      </span>
                    )}
                  </div>
                  <p className="text-[11px] leading-4 text-muted">
                    {contextWindowTouched
                      ? "Leave empty to fall back to the model's default window."
                      : `Auto-fills from the model's context limit${
                          catalogLimit === null
                            ? " when known"
                            : ` (${catalogLimit.toLocaleString()} tokens)`
                        }.`}
                  </p>
                  {fieldErrors.contextWindow && (
                    <p
                      className="text-xs text-dangertext"
                      role="alert"
                      data-testid="field-error-config-context-window"
                    >
                      {fieldErrors.contextWindow}
                    </p>
                  )}
                </div>
              </div>
            )}
          </div>
        </div>

        {/* --- Capabilities: chip clouds for tools/skills, opt-in rows for MCP, and subagents switch. */}
        <div
          role="tabpanel"
          data-testid="config-section-capabilities"
          className={cn(
            "max-w-xl space-y-6",
            tab !== "capabilities" && "hidden"
          )}
        >
          <MicroLabel>Capabilities</MicroLabel>

          <div data-testid="config-section-tools">
            <h4 className="mb-1.5 block text-[12px] font-medium text-fg2">
              Built-in Tools
            </h4>
            <p className="mb-2 text-xs text-muted">
              {allToolsOn
                ? "All catalog tools are enabled (default). Deselecting any tool switches to an explicit selection."
                : "Explicit selection — only the pressed tools below are enabled for this agent."}
            </p>
            {tools.length === 0 ? (
              <p className="text-xs text-muted">No tools in the registry.</p>
            ) : (
              <OptionChips
                testIdPrefix="config-tool"
                options={tools.map((t: ToolEntry) => ({
                  id: t.Name,
                  label: t.DisplayName || t.Name,
                  detail: t.Description,
                }))}
                value={toolValue}
                onChange={handleToolsChange}
              />
            )}
          </div>

          <div data-testid="config-section-skills">
            <h4 className="mb-1.5 block text-[12px] font-medium text-fg2">
              Skills
            </h4>
            {skills.length === 0 ? (
              <p className="text-xs text-muted">No skills in the registry.</p>
            ) : (
              <OptionChips
                testIdPrefix="config-skill"
                disabledTitle="Disabled in Settings"
                options={skills.map((s: Skill) => ({
                  id: s.Name,
                  label: s.Name,
                  detail: s.Description,
                }))}
                value={selectedSkills}
                onChange={setSelectedSkills}
                disabledOf={(o: OptionChipOption) =>
                  skills.find((s: Skill): boolean => s.Name === o.id)
                    ?.Enabled === false
                }
              />
            )}
          </div>

          <div data-testid="config-section-mcp">
            <h4 className="mb-1.5 block text-[12px] font-medium text-fg2">
              MCP Servers
            </h4>
            <p className="text-xs text-muted">
              Every enabled server is available by default; turn servers off to
              restrict this agent (stored on save). To remove a server from
              every agent, pause it in Settings.
            </p>
            {mcpServers.length === 0 ? (
              <p className="mt-1 text-xs text-muted">
                No MCP servers configured.
              </p>
            ) : (
              <McpServerList
                servers={mcpServers}
                selected={selectedMcp}
                onChange={setSelectedMcp}
                testIdPrefix="config-mcp"
              />
            )}
          </div>

          <div
            data-testid="config-section-subagents"
            className="rounded-md border border-line px-3 py-2.5"
          >
            <div className="flex items-center gap-3">
              <div className="min-w-0 flex-1">
                <span className="text-[13px] font-medium text-fg">
                  Subagents
                </span>
                <p className="mt-0.5 text-[11px] leading-4 text-muted">
                  Let the agent delegate work to a general-purpose subagent
                  through its agent tool.
                </p>
              </div>
              <Toggle
                on={subagents}
                onChange={setSubagents}
                label="Enable subagents"
                testId="config-subagents-toggle"
              />
            </div>
          </div>
        </div>

        {/* --- Hooks: per-agent assignment in evaluation order. */}
        <div
          role="tabpanel"
          data-testid="config-section-hooks"
          className={cn("max-w-xl space-y-4", tab !== "hooks" && "hidden")}
        >
          <div>
            <MicroLabel>Hooks</MicroLabel>
            <p className="mt-1 text-[11px] leading-4 text-muted">
              Assigned hooks run before the global ones, in this order.
            </p>
          </div>

          {assignedHooks.length === 0 ? (
            <p className="text-sm text-muted" data-testid="config-hooks-empty">
              No hooks assigned to this agent.
            </p>
          ) : (
            <ul
              className="divide-y divide-linesoft rounded-md border border-line"
              data-testid="config-hook-list"
            >
              {assignedHooks.map(
                (assignment: AgentHookAssignment, index: number) => (
                  <li
                    key={assignment.HookID}
                    className="flex items-center gap-2 px-3 py-2"
                    data-testid={`config-hook-${assignment.HookID}`}
                  >
                    <span className="min-w-0 flex-1 truncate text-sm text-fg">
                      {assignment.HookName}
                    </span>
                    {hookEvent(assignment.HookID) && (
                      <Chip>{hookEvent(assignment.HookID)}</Chip>
                    )}
                    <IconButton
                      label={`Move ${assignment.HookName} up`}
                      size="sm"
                      disabled={index === 0}
                      testId={`config-hook-up-${assignment.HookID}`}
                      icon={<ArrowUp aria-hidden="true" className="size-4" />}
                      onClick={() => moveHook(index, index - 1)}
                    />
                    <IconButton
                      label={`Move ${assignment.HookName} down`}
                      size="sm"
                      disabled={index === assignedHooks.length - 1}
                      testId={`config-hook-down-${assignment.HookID}`}
                      icon={<ArrowDown aria-hidden="true" className="size-4" />}
                      onClick={() => moveHook(index, index + 1)}
                    />
                    <IconButton
                      label={`Unassign ${assignment.HookName}`}
                      size="sm"
                      testId={`config-hook-remove-${assignment.HookID}`}
                      icon={<Trash2 aria-hidden="true" className="size-4" />}
                      onClick={() => unassignHook(assignment.HookID)}
                    />
                  </li>
                )
              )}
            </ul>
          )}

          {hooks.length === 0 ? (
            <p className="text-sm text-muted" data-testid="config-hooks-none">
              No hooks configured in Settings.
            </p>
          ) : (
            <div className="flex items-center gap-2">
              <Select
                className="w-full"
                data-testid="config-hook-add-select"
                value={hookToAdd}
                onChange={(e: React.ChangeEvent<HTMLSelectElement>) =>
                  setHookToAdd(e.target.value)
                }
              >
                <option value="">Select a hook...</option>
                {hooks
                  .filter(
                    (h: Hook): boolean =>
                      !assignedHooks.some(
                        (a: AgentHookAssignment): boolean => a.HookID === h.ID
                      )
                  )
                  .map((h: Hook) => (
                    <option key={h.ID} value={h.ID}>
                      {h.Name} — {h.Event}
                    </option>
                  ))}
              </Select>
              <Button
                size="sm"
                data-testid="config-hook-add"
                disabled={!hookToAdd}
                onClick={() => assignHook(hookToAdd)}
              >
                Assign
              </Button>
            </div>
          )}
        </div>

        {/* --- Prompts: the file-rail document editors. */}
        <div
          role="tabpanel"
          className={cn("max-w-xl", tab !== "prompts" && "hidden")}
        >
          <IdentitySection agent={agent} onAgentRefreshed={onSaved} />
        </div>
      </div>

      {/* Delete confirmation modal */}
      {deleting && (
        <DeleteAgentModal
          agent={agent}
          onClose={() => setDeleting(false)}
          onDeleted={onDeleted}
        />
      )}
    </div>
  );
}
