import { useState, useEffect, useMemo, useCallback } from "react";
import { cx, providerOf, slugify } from "../lib/helpers";
import { Modal } from "../components/ui/Modal";
import { inputCls, labelCls } from "../components/ui/constants";
import { PROVIDER_TYPES } from "../lib/constants";
import { Segmented } from "../components/ui/Segmented";
import { OptionChips } from "../components/ui/OptionChips";
import { MicroLabel } from "../components/ui/MicroLabel";
import { Icon } from "../components/ui/Icon";
import { Toggle } from "../components/ui/Toggle";
import { AvatarPicker, generateRandomAvatar } from "../components/ui/AvatarPicker";
import { ModelCombobox } from "../components/ui/ModelCombobox";
import { OnboardingLoader } from "../components/ui/OnboardingLoader";
import {
  api,
  formatApiError,
  type ApiProviderConfig,
  type ApiWorkspaceSkill,
  type ApiToolSettings,
  type ApiMcpServer,
  type McpServerPayload,
  type AgentAutonomy,
  type CreateAgentPayload,
} from "../lib/api";
import { useCanWriteSkills, unmetToolDependencies } from "../lib/skills";
import { useCanWriteAgents } from "../lib/agents";
import { McpServerDialog } from "./McpServerDialog";
import { useWorkspace, useStore } from "../store";

// The browser facade: the catalog exposes one Browser chip whose stored
// value is the alias `browser`. Legacy allowlists may carry individual
// `browser.*` names — they collapse to the alias in the UI and on save
// (workspace-tool-catalog D2).
const BROWSER_TOOL_ALIAS = "browser";
const BROWSER_MEMBER_PREFIX = "browser.";
const TIER_HINT: Record<string, string> = {
  system: "System skill — always attached",
  workspace: "Workspace skill — enabled for every agent. Manage in Settings → Skills.",
  agent: "Agent skill — installed into this agent's directory only.",
};

function normalizeBrowserAlias(toolIds: string[]): string[] {
  const hasAlias = toolIds.includes(BROWSER_TOOL_ALIAS);
  const hasMember = toolIds.some((t) => t.startsWith(BROWSER_MEMBER_PREFIX));
  if (!hasMember) return toolIds;
  const withoutMembers = toolIds.filter((t) => !t.startsWith(BROWSER_MEMBER_PREFIX));
  return hasAlias ? withoutMembers : [...withoutMembers, BROWSER_TOOL_ALIAS];
}

// Design D1: the agent's MCP opt-ins are server-assigned UUIDs in
// `enabled_mcps`.
function mcpRefs(a: any): string[] {
  return a?.enabled_mcps ?? [];
}

// Same display contract as the settings pane: the enabled master switch wins
// (Paused), then the probed status — connected/ok green, error red, unknown gray.
function mcpStatusView(s: ApiMcpServer): { dot: string; label: string; errored: boolean } {
  if (!s.enabled) return { dot: 'bg-muted', label: 'Paused', errored: false };
  if (s.status === 'error') return { dot: 'bg-danger', label: 'Error', errored: true };
  if (s.status === 'ok' || s.status === 'connected')
    return { dot: 'bg-success', label: 'Connected', errored: false };
  return { dot: 'bg-muted', label: 'Unknown', errored: false };
}

const ROLE_SUGGESTIONS = [
  "code-reviewer",
  "customer-support",
  "data-analyst",
  "research-assistant",
  "triage-bot",
  "pricing-monitor",
];

const PROMPT_FILES: Array<{ id: 'identity' | 'soul' | 'bootstrap'; name: string; hint: string }> = [
  { id: 'identity', name: 'IDENTITY.md', hint: 'identity' },
  { id: 'soul', name: 'SOUL.md', hint: 'voice' },
  { id: 'bootstrap', name: 'BOOTSTRAP.md', hint: 'ritual' },
];

const AUTONOMY_OPTIONS: Array<{ id: AgentAutonomy; label: string; description: string }> = [
  {
    id: "approval",
    label: "Act with approval",
    description: "The agent asks you before every tool call or external action.",
  },
  {
    id: "suggest",
    label: "Suggest only",
    description: "The agent proposes actions for you to run — it never executes anything on its own.",
  },
  {
    id: "full",
    label: "Fully autonomous",
    description: "The agent runs tools and posts results without asking each time.",
  },
];

export interface AgentConfigModalProps {
  draft?: any | null;
  onClose: () => void;
  onSave: (values: any) => void;
  tenant?: any;
}

export function AgentConfigModal({
  draft,
  onClose,
  onSave,
  tenant,
}: AgentConfigModalProps) {
  const isEdit = Boolean(draft);
  const currentWs = useWorkspace();
  const targetWsId =
    tenant?.sub || tenant?.id || currentWs?.sub || currentWs?.id || useStore.getState().pos.tenantId;

  // Step in wizard (1 = Identity, 2 = Model, 3 = Capabilities)
  const [step, setStep] = useState<1 | 2 | 3>(1);

  // Tab in edit mode ('identity' | 'capabilities' | 'prompts')
  const [editTab, setEditTab] = useState<'identity' | 'model' | 'capabilities' | 'prompts'>('identity');

  // Loading detail state for edit mode
  const [loadingDetail, setLoadingDetail] = useState(isEdit);

  // Configured providers
  const [configuredProviders, setConfiguredProviders] = useState<ApiProviderConfig[]>(
    tenant?.providers || currentWs?.providers || []
  );

  // Workspace skills (system + workspace tiers) and this agent's own skills
  const [workspaceSkills, setWorkspaceSkills] = useState<ApiWorkspaceSkill[]>([]);
  const [agentSkills, setAgentSkills] = useState<ApiWorkspaceSkill[]>([]);
  const [agentSkillFormOpen, setAgentSkillFormOpen] = useState(false);
  const skillsWritable = useCanWriteSkills(tenant || currentWs);

  // MCP: the workspace registry rows (opt-in toggles) and this agent's own
  // private servers (edit mode only). Private add/edit rides the shared
  // structured dialog, gated to agents.write holders (design D9).
  const [wsMcpServers, setWsMcpServers] = useState<ApiMcpServer[]>([]);
  const [agentMcpServers, setAgentMcpServers] = useState<ApiMcpServer[]>([]);
  const [agentMcpDialog, setAgentMcpDialog] = useState<
    { mode: 'add' } | { mode: 'edit'; server: ApiMcpServer } | null
  >(null);
  const agentsWritable = useCanWriteAgents(tenant || currentWs);

  const [toolCatalog, setToolCatalog] = useState<ApiToolSettings[]>([]);

  // Regenerating state
  const [regenerating, setRegenerating] = useState(false);

  // Prompts tab: selected file in the list/preview view + the regenerate
  // change-request form
  const [selectedDoc, setSelectedDoc] = useState<'identity' | 'soul' | 'bootstrap'>('identity');
  const [regenFormOpen, setRegenFormOpen] = useState(false);
  const [regenInstruction, setRegenInstruction] = useState('');

  // Advanced model configuration collapsed state
  const [advancedOpen, setAdvancedOpen] = useState(false);

  // Form fields (not seeded from store draft in edit mode)
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugEdited, setSlugEdited] = useState(false);
  const [role, setRole] = useState("");
  const [description, setDescription] = useState("");
  const [brief, setBrief] = useState("");
  const [identity, setIdentity] = useState("");
  const [soul, setSoul] = useState("");
  const [bootstrap, setBootstrap] = useState("");
  const [avatar, setAvatar] = useState<Record<string, any>>(generateRandomAvatar());

  const initialProvider = useMemo(() => {
    if (configuredProviders.length > 0) {
      return configuredProviders[0].id;
    }
    return "anthropic";
  }, [configuredProviders]);

  const [provider, setProvider] = useState<string>(initialProvider);
  const [model, setModel] = useState<string>("");
  const [temp, setTemp] = useState<number>(1.0);
  const [maxTokens, setMaxTokens] = useState<string>("");
  const [contextWindow, setContextWindow] = useState<string>("");
  const [contextWindowTouched, setContextWindowTouched] = useState(false);
  const [catalogContextLimit, setCatalogContextLimit] = useState<number | null>(null);
  const [effort, setEffort] = useState<string | null>(null);
  const [availableEfforts, setAvailableEfforts] = useState<string[]>([]);
  const [autonomy, setAutonomy] = useState<AgentAutonomy>('approval');

  // Untouched Step 3 enables no registry tools and opts into no MCP servers —
  // the deployer opts in (scenarios: Step 3 skippable).
  const [tools, setTools] = useState<string[]>([]);
  const [enabledMcps, setEnabledMcps] = useState<string[]>([]);

  const [promptStatus, setPromptStatus] = useState<string>('ready');
  const [promptError, setPromptError] = useState<string | null | undefined>(null);

  // Validation errors
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [generalError, setGeneralError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  // Hydrate agent details from GET endpoint in edit mode; rerun after a
  // regeneration so the Prompts tab shows the refreshed documents.
  const loadDetail = useCallback(async () => {
    const agentId = draft?.id || draft?.slug;
    if (!isEdit || !targetWsId || !agentId) return;
    setLoadingDetail(true);
    try {
      const res = await api.agents.get(targetWsId, agentId);
      if (res?.agent) {
            const a = res.agent;
            setName(a.name || "");
            setSlug(a.slug || slugify(a.name || ""));
            setSlugEdited(true);
            setRole(a.role || "");
            setDescription(a.description || "");
            setBrief(a.brief || "");
            setIdentity(a.identity || "");
            setSoul(a.soul || "");
            setBootstrap(a.bootstrap || "");
            if (a.avatar && Object.keys(a.avatar).length > 0) {
              setAvatar(a.avatar);
            }
            if (a.provider_id) {
              setProvider(a.provider_id);
            }
            setModel(a.model || "");
            setTemp(a.temperature ?? 1.0);
            setMaxTokens(a.max_tokens !== undefined && a.max_tokens !== null ? String(a.max_tokens) : "");
            // The stored value is authoritative in edit mode; the client
            // auto-fill effect must not clobber it.
            setContextWindowTouched(true);
            setContextWindow(a.context_window !== undefined && a.context_window !== null ? String(a.context_window) : "");
            setEffort(a.effort || null);
            if (a.autonomy && ['approval', 'suggest', 'full'].includes(a.autonomy)) {
              setAutonomy(a.autonomy as AgentAutonomy);
            }
            if (a.tools) setTools(normalizeBrowserAlias([...a.tools]));
            setEnabledMcps([...mcpRefs(a)]);
            setPromptStatus(a.prompts_status || 'ready');
            setPromptError(a.prompts_error || null);
      }
    } catch (err: unknown) {
      setGeneralError(formatApiError(err, "Failed to load agent details"));
    } finally {
      setLoadingDetail(false);
    }
  }, [isEdit, targetWsId, draft?.id, draft?.slug]);

  useEffect(() => {
    void loadDetail();
  }, [loadDetail]);

  // Load providers and skills
  useEffect(() => {
    let mounted = true;
    if (targetWsId) {
      api.providers
        .list(targetWsId)
        .then((res) => {
          if (mounted && res?.providers) {
            setConfiguredProviders(res.providers);
          }
        })
        .catch(() => {});

      api.skills
        .list(targetWsId)
        .then((res) => {
          if (mounted && res?.skills) {
            setWorkspaceSkills(res.skills);
          }
        })
        .catch(() => {});

      api.tools
        .list(targetWsId)
        .then((res) => {
          if (mounted && res?.tools) {
            setToolCatalog(res.tools);
          }
        })
        .catch(() => {});

      // Workspace MCP registry — reads are tools.read, every built-in role
      // holds them, so the opt-in rows render for anyone configuring an agent.
      api.mcp
        .list(targetWsId)
        .then((res) => {
          if (mounted && res?.servers) {
            setWsMcpServers(res.servers);
          }
        })
        .catch(() => {});
    }
    return () => {
      mounted = false;
    };
  }, [targetWsId]);

  // Hydrate agent-tier skills and private MCP servers in edit mode.
  useEffect(() => {
    let mounted = true;
    const agentId = draft?.id || draft?.slug;
    if (isEdit && agentId && targetWsId) {
      // Agent-tier skills live in this agent's own directory — edit mode only.
      api.agents
        .listSkills(targetWsId, agentId)
        .then((res) => {
          if (mounted && res?.skills) setAgentSkills(res.skills);
        })
        .catch(() => {});
      // Agent-private MCP servers hydrate from the agent detail endpoints.
      api.agents
        .listMcpServers(targetWsId, agentId)
        .then((res) => {
          if (mounted && res?.servers) setAgentMcpServers(res.servers);
        })
        .catch(() => {});
    }
    return () => {
      mounted = false;
    };
  }, [isEdit, draft?.id, draft?.slug, targetWsId]);

  // Sync provider default when list loads (create mode only)
  useEffect(() => {
    if (!isEdit && configuredProviders.length > 0) {
      const exists = configuredProviders.some((p) => p.id === provider || p.type === provider);
      if (!exists) {
        setProvider(configuredProviders[0].id);
      }
    }
  }, [isEdit, configuredProviders, provider]);

  // Auto-fill context window from the selected model's catalog limit whenever
  // the model changes and the user has not typed a custom value. A manually
  // entered value wins until the user clears it (reset-to-auto).
  useEffect(() => {
    if (!contextWindowTouched && catalogContextLimit) {
      setContextWindow(String(catalogContextLimit));
    }
  }, [catalogContextLimit, contextWindowTouched]);

  // Handle name changes + auto-suggest slug
  const handleNameChange = (val: string) => {
    setName(val);
    if (!slugEdited && !isEdit) {
      setSlug(slugify(val));
    }
  };

  const handleSlugChange = (val: string) => {
    setSlugEdited(true);
    setSlug(val.toLowerCase().replace(/[^a-z0-9-]/g, ""));
  };

  // Validate step 1 fields
  const validateStep1 = () => {
    const errors: Record<string, string> = {};
    if (!name.trim()) errors.name = "Agent name is required";
    if (!slug.trim()) errors.slug = "Agent slug is required";
    if (!role.trim()) errors.role = "Role is required";
    if (!brief.trim()) errors.brief = "Goal & behavior brief is required";


    setFieldErrors(errors);



    return Object.keys(errors).length === 0;
  };

  const validateStep2 = () => {
    const step2Errors: Record<string, string> = {};
    if (!provider) step2Errors.provider = "Provider is required";
    if (!model.trim()) step2Errors.model = "Model is required";
    if (temp < 0 || temp > 2) step2Errors.temp = "Temperature must be between 0.0 and 2.0";
    if (maxTokens.trim()) {
      const num = parseInt(maxTokens.trim(), 10);
      if (isNaN(num) || num <= 0) {
        step2Errors.maxTokens = "Max tokens must be a positive integer";
      }
    }
    if (contextWindow.trim()) {
      const num = parseInt(contextWindow.trim(), 10);
      if (isNaN(num) || num <= 0) {
        step2Errors.contextWindow = "Context window must be a positive integer";
      }
    }
    if (Object.keys(step2Errors).length > 0) {
      setFieldErrors(step2Errors);
      if (step2Errors.temp || step2Errors.maxTokens || step2Errors.effort || step2Errors.contextWindow) {
        setAdvancedOpen(true);
      }
      return false;
    }
    return true;
  };

  const handleNextStep = () => {
    if (step === 1) {
      if (validateStep1()) setStep(2);
    } else if (step === 2) {
      if (validateStep2()) setStep(3);
    }
  };

  const handleCreateSubmit = async () => {
    if (!validateStep1()) {
      setStep(1);
      return;
    }
    if (!validateStep2()) {
      setStep(2);
      return;
    }

    setSubmitting(true);
    setGeneralError(null);

    const parsedMaxTokens = maxTokens.trim() ? parseInt(maxTokens.trim(), 10) : undefined;
    const parsedContextWindow = contextWindow.trim() ? parseInt(contextWindow.trim(), 10) : undefined;

    // Design D1: MCP opt-ins ride `enabled_mcps` (server UUIDs).
    const payload: CreateAgentPayload = {
      name: name.trim(),
      slug: slug.trim(),
      role: role.trim(),
      description: description.trim(),
      brief: brief.trim(),
      provider_id: provider,
      model: model.trim(),
      temperature: temp,
      max_tokens: parsedMaxTokens,
      context_window: parsedContextWindow,
      effort: effort || undefined,
      autonomy,
      tools: normalizeBrowserAlias(tools),
      enabled_mcps: enabledMcps,
      avatar,
    };

    try {
      if (targetWsId) {
        const res = await api.agents.create(targetWsId, payload);
        if (res?.agent) {
          onSave(res.agent);
          onClose();
          return;
        }
      }
      onSave(payload);
      onClose();
    } catch (err: unknown) {
      const msg = formatApiError(err, "Failed to create agent");
      setGeneralError(msg);
      useStore.getState().toast(msg, "danger");
    } finally {
      setSubmitting(false);
    }
  };

  const handleEditSave = async () => {
    setSubmitting(true);
    setGeneralError(null);

    const parsedMaxTokens = maxTokens.trim() ? parseInt(maxTokens.trim(), 10) : undefined;
    const parsedContextWindow = contextWindow.trim() ? parseInt(contextWindow.trim(), 10) : undefined;

    // The slug is immutable after creation; it is omitted from PATCH so the
    // server's managed-field semantics need never fire.
    const patchPayload = {
      name: name.trim(),
      role: role.trim(),
      description: description.trim(),
      brief: brief.trim(),
      identity: identity,
      soul: soul,
      provider_id: provider,
      model: model.trim(),
      temperature: temp,
      max_tokens: parsedMaxTokens,
      context_window: parsedContextWindow,
      effort: effort || undefined,
      autonomy,
      tools: normalizeBrowserAlias(tools),
      enabled_mcps: enabledMcps,
      avatar,
    };

    const agentId = draft?.id || draft?.slug;
    try {
      if (targetWsId && agentId) {
        const res = await api.agents.patch(targetWsId, agentId, patchPayload);
        if (res?.agent) {
          onSave(res.agent);
          onClose();
          return;
        }
      }
      onSave({ ...draft, ...patchPayload });
      onClose();
    } catch (err: unknown) {
      const msg = formatApiError(err, "Failed to update agent");
      setGeneralError(msg);
      useStore.getState().toast(msg, "danger");
    } finally {
      setSubmitting(false);
    }
  };

  const handleRegenerate = async (instruction?: string) => {
    const agentId = draft?.id || draft?.slug;
    if (!targetWsId || !agentId || regenerating) return;
    setRegenerating(true);
    try {
      await useStore.getState().regenerateAgent(targetWsId, agentId, instruction || undefined);
      // Stay on the Prompts tab and refresh: the user sees the enhanced
      // documents without reopening the modal.
      await loadDetail();
      setRegenFormOpen(false);
      setRegenInstruction('');
    } catch (err: unknown) {
      useStore.getState().toast(formatApiError(err, "Regeneration failed"), "danger");
    } finally {
      setRegenerating(false);
    }
  };

  const handleAgentSkillInstalled = (skill: ApiWorkspaceSkill) => {
    setAgentSkills((prev) => [...prev.filter((s) => s.name !== skill.name), skill]);
    setAgentSkillFormOpen(false);
    useStore.getState().toast(`${skill.name} installed for this agent only`);
  };

  const handleAgentSkillRemove = async (skill: ApiWorkspaceSkill) => {
    const agentId = draft?.id || draft?.slug;
    if (!targetWsId || !agentId) return;
    try {
      await api.agents.removeSkill(targetWsId, agentId, skill.name);
      setAgentSkills((prev) => prev.filter((s) => s.name !== skill.name));
      useStore.getState().toast(`${skill.name} removed from this agent`);
    } catch (err: unknown) {
      useStore.getState().toast(formatApiError(err, `Failed to remove ${skill.name}`), "danger");
    }
  };

  // Opt-in toggle: storing/removing the server UUID in the draft `enabled_mcps`
  // — persisted only on save. Anyone who can edit the agent can toggle.
  const toggleMcpServer = (id: string) => {
    setEnabledMcps((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]
    );
  };

  // Agent-private MCP server save through the shared structured dialog. The
  // dialog owns error display: a rejection keeps it open with the message
  // inline, so API failures propagate untouched.
  const handleAgentMcpSave = async (payload: McpServerPayload) => {
    const agentId = draft?.id || draft?.slug;
    if (!targetWsId || !agentId || !agentMcpDialog) return;
    if (agentMcpDialog.mode === 'edit') {
      const res = await api.agents.updateMcpServer(targetWsId, agentId, agentMcpDialog.server.id, payload);
      if (res?.server) {
        const saved = res.server;
        setAgentMcpServers((prev) => prev.map((s) => (s.id === saved.id ? saved : s)));
        useStore.getState().toast(`${payload.name} updated`);
      }
    } else {
      const res = await api.agents.createMcpServer(targetWsId, agentId, payload);
      if (res?.server) {
        const saved = res.server;
        setAgentMcpServers((prev) => [...prev, saved]);
        useStore.getState().toast(`${saved.name} attached to this agent`);
      }
    }
  };

  const handleAgentMcpRemove = async (server: ApiMcpServer) => {
    const agentId = draft?.id || draft?.slug;
    if (!targetWsId || !agentId) return;
    try {
      await api.agents.deleteMcpServer(targetWsId, agentId, server.id);
      setAgentMcpServers((prev) => prev.filter((s) => s.id !== server.id));
      useStore.getState().toast(`${server.name} removed from this agent`);
    } catch (err: unknown) {
      useStore.getState().toast(formatApiError(err, `Failed to remove ${server.name}`), "danger");
    }
  };

  // Skills attached to every agent: system tier always, workspace tier when the
  // master switch is on. Never toggleable per agent — tiers are the only model.
  const lockedSkillChips = useMemo(
    () =>
      workspaceSkills.filter(
        (s) => s.tier === "system" || (s.tier === "workspace" && s.enabled !== false)
      ),
    [workspaceSkills]
  );

  // One inventory: system + enabled workspace tiers, then this agent's own.
  const allSkills = useMemo(() => [...lockedSkillChips, ...agentSkills], [lockedSkillChips, agentSkills]);

  return (
    <Modal
      title={isEdit ? (name ? `Configure ${name}` : draft?.name ? `Configure ${draft.name}` : "Configure agent") : "Deploy a new agent"}
      onClose={onClose}
      wide
      odId="agent-config-modal"
      footer={
        !isEdit ? (
          step === 1 ? (
            <>
              <button
                type="button"
                onClick={onClose}
                className="flex h-9 items-center rounded-md px-3.5 text-[13px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={handleNextStep}
                data-od-id="btn-agent-next-step"
                data-testid="btn-agent-next-step"
                className="flex h-9 items-center gap-1.5 rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]"
              >
                Continue to model
                <Icon name="arrow-right" size={14} />
              </button>
            </>
          ) : step === 2 ? (
            <>
              <button
                type="button"
                onClick={() => setStep(1)}
                data-od-id="btn-agent-back-step"
                data-testid="btn-agent-back-step"
                className="flex h-9 items-center gap-1.5 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
              >
                <Icon name="arrow-left" size={14} />
                Back
              </button>
              <div className="flex-1" />
              <button
                type="button"
                onClick={handleNextStep}
                data-od-id="btn-agent-next-step"
                data-testid="btn-agent-next-step"
                className="flex h-9 items-center gap-1.5 rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]"
              >
                Continue to capabilities
                <Icon name="arrow-right" size={14} />
              </button>
            </>
          ) : (
            <>
              <button
                type="button"
                onClick={() => setStep(2)}
                data-od-id="btn-agent-back-step"
                data-testid="btn-agent-back-step"
                className="flex h-9 items-center gap-1.5 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
              >
                <Icon name="arrow-left" size={14} />
                Back
              </button>
              <div className="flex-1" />

              <button
                type="button"
                disabled={submitting}
                onClick={() => handleCreateSubmit()}
                data-od-id="btn-agent-save-modal"
                data-testid="btn-agent-save-modal"
                className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40"
              >
                {submitting ? "Deploying…" : "Deploy agent"}
              </button>
            </>
          )
        ) : (
          <>
            <button
              type="button"
              onClick={onClose}
              className="flex h-9 items-center rounded-md px-3.5 text-[13px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={submitting || loadingDetail}
              onClick={handleEditSave}
              data-od-id="btn-agent-save-modal"
              data-testid="btn-agent-save-modal"
              className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40"
            >
              {submitting ? "Saving…" : "Save changes"}
            </button>
          </>
        )
      }
    >
      <div className="p-5">
        {generalError && (
          <div className="mb-4 rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] p-3 text-[13px] text-danger">
            {generalError}
          </div>
        )}

        {isEdit && loadingDetail ? (
          <div className="flex h-64 items-center justify-center text-[13px] text-muted" data-testid="agent-modal-loading">
            <div className="flex items-center gap-2">
              <div className="h-4 w-4 animate-spin rounded-full border-2 border-line border-t-accent" />
              Loading agent details…
            </div>
          </div>
        ) : ((!isEdit && submitting) || regenerating) ? (
          <OnboardingLoader
            label={regenerating ? "Regenerating prompts…" : `Onboarding ${name.trim() || "agent"}…`}
          />
        ) : (
          <>

        {/* Wizard Step Indicator */}
        {!isEdit && (
          <div className="mb-6 flex items-center justify-between border-b border-line pb-3">
            <div className="flex items-center gap-3">
              {[
                { n: 1, label: "Identity" },
                { n: 2, label: "Model" },
                { n: 3, label: "Capabilities" },
              ].map(({ n, label }) => (
                <div key={n} className="flex items-center gap-2">
                  {n > 1 && <span className="text-muted">/</span>}
                  <span
                    className={cx(
                      "flex h-6 w-6 items-center justify-center rounded-full text-[12px] font-semibold",
                      step === n
                        ? "bg-accent text-accenton"
                        : "bg-[color-mix(in_oklab,var(--fg)_10%,transparent)] text-muted"
                    )}
                  >
                    {n}
                  </span>
                  <span className={cx("text-[13px] font-medium", step === n ? "text-fg" : "text-muted")}>
                    {label}
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Edit Mode Tabs */}
        {isEdit && (
          <div className="mb-6 flex border-b border-line">
            <button
              type="button"
              onClick={() => setEditTab('identity')}
              className={cx(
                "border-b-2 px-4 py-2 text-[13px] font-medium transition-colors",
                editTab === 'identity'
                  ? "border-accent text-fg font-semibold"
                  : "border-transparent text-muted hover:text-fg"
              )}
            >
              Identity &amp; Soul
            </button>
            <button
              type="button"
              onClick={() => setEditTab('model')}
              className={cx(
                "border-b-2 px-4 py-2 text-[13px] font-medium transition-colors",
                editTab === 'model'
                  ? "border-accent text-fg font-semibold"
                  : "border-transparent text-muted hover:text-fg"
              )}
            >
              Model
            </button>
            <button
              type="button"
              onClick={() => setEditTab('capabilities')}
              className={cx(
                "border-b-2 px-4 py-2 text-[13px] font-medium transition-colors",
                editTab === 'capabilities'
                  ? "border-accent text-fg font-semibold"
                  : "border-transparent text-muted hover:text-fg"
              )}
            >
              Capabilities
            </button>
            <button
              type="button"
              onClick={() => setEditTab('prompts')}
              className={cx(
                "border-b-2 px-4 py-2 text-[13px] font-medium transition-colors",
                editTab === 'prompts'
                  ? "border-accent text-fg font-semibold"
                  : "border-transparent text-muted hover:text-fg"
              )}
            >
              Prompts
            </button>
          </div>
        )}

        {/* Wizard Step 1 OR Edit Tab Identity */}
        {(!isEdit && step === 1) || (isEdit && editTab === 'identity') ? (
          <div className="space-y-6 max-w-xl">
            <div className="space-y-4">
              <MicroLabel>Identity &amp; Persona</MicroLabel>

              {/* Avatar Picker */}
              <div>
                <label className={labelCls}>Avatar</label>
                <AvatarPicker value={avatar} onChange={setAvatar} />
              </div>

              {/* Name & Slug */}
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className={labelCls} htmlFor="ac-name">
                    Agent name
                  </label>
                  <input
                    id="ac-name"
                    data-testid="input-agent-name"
                    className={cx(inputCls, fieldErrors.name && "border-danger")}
                    value={name}
                    onChange={(e) => handleNameChange(e.target.value)}
                    placeholder="e.g. Radar"
                    autoFocus={!isEdit}
                  />
                  {fieldErrors.name && <p className="mt-1 text-[12px] text-danger">{fieldErrors.name}</p>}
                </div>
                <div>
                  <label className={labelCls} htmlFor="ac-slug">
                    Slug
                  </label>
                  {isEdit ? (
                    <input
                      id="ac-slug"
                      data-testid="input-agent-slug"
                      className={cx(inputCls, "font-mono text-[13px]", "bg-surface-2")}
                      value={slug}
                      onChange={() => {}}
                      placeholder="e.g. radar"
                      readOnly
                    />
                  ) : (
                    <input
                      id="ac-slug"
                      data-testid="input-agent-slug"
                      className={cx(inputCls, "font-mono text-[13px]", fieldErrors.slug && "border-danger")}
                      value={slug}
                      onChange={(e) => handleSlugChange(e.target.value)}
                      placeholder="e.g. radar"
                    />
                  )}
                  {fieldErrors.slug && <p className="mt-1 text-[12px] text-danger">{fieldErrors.slug}</p>}
                </div>
              </div>

              {/* Role + Kebab suggestions */}
              <div>
                <label className={labelCls} htmlFor="ac-role">
                  Role in one line
                </label>
                <input
                  id="ac-role"
                  data-testid="input-agent-role"
                  className={cx(inputCls, fieldErrors.role && "border-danger")}
                  value={role}
                  onChange={(e) => setRole(e.target.value)}
                  placeholder="e.g. watches competitor pricing and flags moves over 5%"
                />
                <div className="mt-1.5 flex flex-wrap gap-1.5">
                  {ROLE_SUGGESTIONS.map((sug) => (
                    <button
                      key={sug}
                      type="button"
                      onClick={() => setRole(sug)}
                      className="rounded bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] px-2 py-0.5 font-mono text-[11px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_12%,transparent)] hover:text-fg"
                    >
                      {sug}
                    </button>
                  ))}
                </div>
                {fieldErrors.role && <p className="mt-1 text-[12px] text-danger">{fieldErrors.role}</p>}
              </div>

              {/* Description */}
              <div>
                <label className={labelCls} htmlFor="ac-description">
                  Description <span className="text-muted font-normal">(optional summary)</span>
                </label>
                <textarea
                  id="ac-description"
                  data-testid="input-agent-description"
                  rows={2}
                  className={cx(inputCls, "h-auto py-2")}
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder="Short description for teammates in the roster…"
                />
              </div>

              {/* Goal & behavior brief (Generation driver) */}
              <div>
                <label className={labelCls} htmlFor="ac-brief">
                  Goal &amp; behavior brief <span className="text-accent font-normal">(drives prompt generation)</span>
                </label>
                <textarea
                  id="ac-brief"
                  data-testid="input-agent-brief"
                  rows={4}
                  className={cx(inputCls, "h-auto py-2 leading-relaxed", fieldErrors.brief && "border-danger")}
                  value={brief}
                  onChange={(e) => setBrief(e.target.value)}
                  placeholder="Explain what this agent does, its tone, boundaries, and how it should interact with tools and users…"
                />
                {fieldErrors.brief && <p className="mt-1 text-[12px] text-danger">{fieldErrors.brief}</p>}
              </div>
            </div>

            </div>
        ) : null}

        {/* Wizard Step 2 OR Edit Tab Model */}
        {(!isEdit && step === 2) || (isEdit && editTab === 'model') ? (
          <div className="space-y-6 max-w-xl">
              {/* Provider Selection */}
              <div>
                <label className={labelCls} htmlFor="ac-provider">
                  Provider
                </label>
                <select
                  id="ac-provider"
                  aria-label="Provider"
                  data-testid="select-agent-provider"
                  className={inputCls}
                  value={provider}
                  onChange={(e) => setProvider(e.target.value)}
                >
                  {configuredProviders.map((p) => {
                    const typeObj = PROVIDER_TYPES.find((t) => t.id === p.type);
                    const typeLabel = typeObj ? typeObj.label : p.type;
                    return (
                      <option key={p.id} value={p.id}>
                        {p.name} ({typeLabel})
                      </option>
                    );
                  })}
                  {PROVIDER_TYPES.filter(
                    (t) => !configuredProviders.some((p) => p.type === t.id)
                  ).map((t) => (
                    <option key={"unconfigured-" + t.id} value={t.id} disabled>
                      {t.label} (Configure in Settings → Providers)
                    </option>
                  ))}
                  {configuredProviders.length === 0 && (
                    <option value={provider} disabled>
                      No providers configured (Settings → Providers)
                    </option>
                  )}
                </select>
              </div>

              {/* Model Combobox & Reasoning Effort */}
              <ModelCombobox
                workspaceId={targetWsId}
                providerId={provider}
                model={model}
                onModelChange={setModel}
                  onAvailableEffortsChange={setAvailableEfforts}
                onContextLimitChange={setCatalogContextLimit}
                effort={effort}
                onEffortChange={setEffort}
                modelError={fieldErrors.model}
                effortError={fieldErrors.effort}
              />

              {/* Collapsed Advanced Model Configuration Section */}
              <div className="rounded-lg border border-line p-3">
                <button
                  type="button"
                  onClick={() => setAdvancedOpen(!advancedOpen)}
                  data-testid="btn-toggle-advanced"
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
                  <div className="mt-3 space-y-4 pt-3 border-t border-line" data-testid="advanced-model-section">
                    <div>
                      <div className="mb-1.5 flex items-baseline justify-between">
                        <label className={cx(labelCls, "mb-0")} htmlFor="ac-temp">
                          Temperature
                        </label>
                        <span className="font-mono text-[12px] text-fg2">{Number(temp).toFixed(2)}</span>
                      </div>
                      <input
                        id="ac-temp"
                        type="range"
                        min="0"
                        max="2"
                        step="0.05"
                        value={temp}
                        onChange={(e) => setTemp(Number(e.target.value))}
                        className="h-1.5 w-full cursor-pointer"
                      />
                      <div className="mt-1 flex justify-between font-mono text-[10px] text-muted">
                        <span>0.0 (precise)</span>
                        <span>1.0 (standard)</span>
                        <span>2.0 (creative)</span>
                      </div>
                      {fieldErrors.temp && <p className="mt-1 text-[12px] text-danger">{fieldErrors.temp}</p>}
                    </div>

                    <div>
                      <label className={labelCls} htmlFor="ac-max-tokens">
                        Max tokens <span className="text-muted font-normal">(optional limit)</span>
                      </label>
                      <input
                        id="ac-max-tokens"
                        data-testid="input-max-tokens"
                        type="number"
                        min="1"
                        className={cx(inputCls, fieldErrors.maxTokens && "border-danger")}
                        placeholder="e.g. 4096"
                        value={maxTokens}
                        onChange={(e) => setMaxTokens(e.target.value)}
                      />
                      {fieldErrors.maxTokens && (
                        <p className="mt-1 text-[12px] text-danger">{fieldErrors.maxTokens}</p>
                      )}
                    </div>

                    <div>
                      <div className="mb-1.5 flex items-center justify-between gap-2">
                        <label className={cx(labelCls, "mb-0")} htmlFor="ac-context-window">
                          Context window
                        </label>
                        <button
                          type="button"
                          onClick={() => {
                            setContextWindowTouched(false);
                            if (catalogContextLimit) {
                              setContextWindow(String(catalogContextLimit));
                            } else {
                              setContextWindow("");
                            }
                          }}
                          data-testid="btn-reset-context-window"
                          className="text-[11px] font-medium text-accent transition-colors hover:text-[var(--accent-hover)]"
                        >
                          Reset to auto
                        </button>
                      </div>
                      <div className="relative">
                        <input
                          id="ac-context-window"
                          data-testid="input-context-window"
                          type="number"
                          min="1"
                          step="1"
                          className={cx(inputCls, "pr-14", fieldErrors.contextWindow && "border-danger")}
                          placeholder={catalogContextLimit ? String(catalogContextLimit) : "Auto (model default)"}
                          value={contextWindow}
                          onChange={(e) => {
                            setContextWindowTouched(true);
                            setContextWindow(e.target.value);
                          }}
                        />
                        {contextWindow && (
                          <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center font-mono text-[11px] text-muted">
                            tokens
                          </span>
                        )}
                      </div>
                      <p className="mt-1 text-[11px] leading-4 text-muted">
                        {contextWindowTouched || contextWindow
                          ? "Leave empty to fall back to the model's default window."
                          : `Auto-fills from the model's context limit${catalogContextLimit ? ` (${catalogContextLimit.toLocaleString()} tokens)` : " when known"}.`}
                      </p>
                      {fieldErrors.contextWindow && (
                        <p className="mt-1 text-[12px] text-danger">{fieldErrors.contextWindow}</p>
                      )}
                    </div>
                  </div>
                )}
              </div>
          </div>
        ) : null}

        {/* Wizard Step 3 OR Edit Tab Capabilities */}
        {(!isEdit && step === 3) || (isEdit && editTab === 'capabilities') ? (
          <div className="space-y-6 max-w-xl">
            <MicroLabel>Capabilities &amp; Integrations</MicroLabel>

            <div>
              <span className={labelCls}>Built-in Tools</span>
              {toolCatalog.length > 0 ? (
                <OptionChips
                  options={toolCatalog.map((t) => ({ id: t.key, label: t.display_name }))}
                  value={tools}
                  onChange={setTools}
                  iconOf={(o: any) => toolCatalog.find((t) => t.key === o.id)?.icon_key || "plug"}
                  disabledOf={(o: any) => !toolCatalog.find((t) => t.key === o.id)?.enabled}
                />
              ) : (
                <p className="text-[12px] text-muted" data-testid="agent-tools-unavailable">
                  Tool catalog unavailable.
                </p>
              )}
            </div>

            <div data-testid="agent-skills-inventory">
              <div className="flex items-center justify-between">
                <span className={labelCls}>Skills</span>
                {isEdit && skillsWritable ? (
                  <button
                    type="button"
                    onClick={() => setAgentSkillFormOpen(true)}
                    data-testid="btn-add-agent-skill"
                    className="flex h-7 items-center gap-1.5 rounded-md border border-line px-2.5 text-[11px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                  >
                    <Icon name="plus" size={12} /> Add skill
                  </button>
                ) : null}
              </div>
              {allSkills.length > 0 ? (
                <div className="flex flex-wrap gap-2" data-testid="skill-chips">
                  {allSkills.map((s) => {
                    // A workspace skill whose tool dependency the agent's
                    // allowlist lacks warns inline and points at the chips below.
                    const missingTools = s.tier === "agent" ? [] : unmetToolDependencies(s).filter((t) => !tools.includes(t));
                    return (
                      <div key={s.tier + "-" + s.name} className="relative">
                        <span
                          title={TIER_HINT[s.tier]}
                          className="flex h-8 cursor-default items-center gap-1.5 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-3 text-[12px] font-medium text-fg2"
                          data-testid={s.tier === "agent" ? "agent-skill-" + s.name : "locked-skill-" + s.name}
                        >
                          {s.tier === "system" ? <Icon name="lock" size={12} /> : <Icon name="spark" size={12} className="text-accent" />}
                          {s.name}
                          <span className="rounded border border-line px-1 py-px font-mono text-[9px] uppercase tracking-wide text-muted" data-testid={"skill-tier-" + s.name}>
                            {s.tier}
                          </span>
                          {s.tier === "agent" && isEdit && skillsWritable ? (
                            <button
                              type="button"
                              aria-label={"Remove " + s.name}
                              onClick={() => handleAgentSkillRemove(s)}
                              data-testid={"btn-remove-agent-skill-" + s.name}
                              className="text-muted transition-colors hover:text-danger"
                            >
                              <Icon name="x" size={13} />
                            </button>
                          ) : null}
                        </span>
                        {missingTools.length > 0 ? (
                          <p
                            className="mt-1 flex items-center gap-1 text-[11px] leading-4 text-[color-mix(in_oklab,var(--warn),black_38%)]"
                            data-testid={"skill-dep-warn-" + s.name}
                          >
                            <Icon name="alert" size={11} />
                            Requires {missingTools.join(", ")} — enable it in the tool chips below.
                          </p>
                        ) : null}
                      </div>
                    );
                  })}
                </div>
              ) : (
                <p className="text-[12px] text-muted" data-testid="locked-skills-empty">
                  No skills attached yet — install one in Settings → Skills.
                </p>
              )}
              <p className="mt-1.5 text-[11px] leading-4 text-muted">
                System skills are always attached, workspace skills follow the Settings → Skills master switch, and agent skills (marked <span className="font-mono uppercase">agent</span>) live only on this agent.
                {!isEdit ? " Agent-tier skills can be added after it is deployed." : ""}
              </p>
            </div>

            <div data-testid="agent-mcp-section">
              <span className={labelCls}>MCP Servers</span>
              {wsMcpServers.length > 0 ? (
                <div className="space-y-2">
                  {wsMcpServers.map((s) => {
                    const st = mcpStatusView(s);
                    const optedIn = enabledMcps.includes(s.id);
                    return (
                      <div
                        key={s.id}
                        data-testid={'agent-mcp-row-' + s.id}
                        className={cx(
                          'rounded-md border border-line px-3 py-2.5',
                          !s.enabled && 'opacity-70'
                        )}
                      >
                        <div className="flex items-center gap-3">
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2">
                              <p className="truncate text-[13px] font-medium text-fg">{s.name}</p>
                              <span
                                className={cx(
                                  'inline-flex items-center gap-1.5 text-[11px]',
                                  st.errored
                                    ? 'text-danger'
                                    : s.enabled
                                    ? 'text-[color-mix(in_oklab,var(--success),black_25%)]'
                                    : 'text-muted'
                                )}
                                data-testid={'agent-mcp-status-' + s.id}
                              >
                                <span className={cx('h-1.5 w-1.5 rounded-full', st.dot)} />
                                {st.label}
                              </span>
                            </div>
                            <p className="truncate font-mono text-[11px] text-muted">{s.transport}</p>
                          </div>
                          <Toggle
                            on={optedIn}
                            label={'Opt this agent into ' + s.name}
                            onChange={() => toggleMcpServer(s.id)}
                          />
                        </div>
                        {optedIn && !s.enabled && (
                          <p
                            className="mt-1.5 flex items-center gap-1 text-[11px] leading-4 text-[color-mix(in_oklab,var(--warn),black_38%)]"
                            data-testid={'agent-mcp-paused-warn-' + s.id}
                          >
                            <Icon name="alert" size={11} />
                            Paused at workspace level — it contributes no tools until it is resumed in Settings → MCP servers.
                          </p>
                        )}
                      </div>
                    );
                  })}
                </div>
              ) : (
                <p className="text-[12px] text-muted" data-testid="agent-mcp-empty">
                  No MCP servers registered yet — add one in Settings → MCP servers.
                </p>
              )}
              <p className="mt-1.5 text-[11px] leading-4 text-muted">
                An agent gains a server's tools only by opting in — toggles store or remove the server for this agent on save.
              </p>
            </div>

            <div data-testid="agent-mcp-servers">
              <div className="flex items-center justify-between">
                <span className={labelCls}>Agent MCP servers</span>
                {isEdit && agentsWritable ? (
                  <button
                    type="button"
                    onClick={() => setAgentMcpDialog({ mode: 'add' })}
                    data-testid="btn-add-agent-mcp"
                    className="flex h-7 items-center gap-1.5 rounded-md border border-line px-2.5 text-[11px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
                  >
                    <Icon name="plus" size={12} /> Add server
                  </button>
                ) : null}
              </div>
              {agentMcpServers.length > 0 ? (
                <div className="space-y-2">
                  {agentMcpServers.map((s) => {
                    const st = mcpStatusView(s);
                    return (
                      <div
                        key={s.id}
                        data-testid={'agent-mcp-server-' + s.id}
                        className="flex items-center gap-3 rounded-md border border-line px-3 py-2.5"
                      >
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-2">
                            <p className="truncate text-[13px] font-medium text-fg">{s.name}</p>
                            <span
                              className={cx(
                                'inline-flex items-center gap-1.5 text-[11px]',
                                st.errored
                                  ? 'text-danger'
                                  : s.enabled
                                  ? 'text-[color-mix(in_oklab,var(--success),black_25%)]'
                                  : 'text-muted'
                              )}
                              data-testid={'agent-mcp-server-status-' + s.id}
                            >
                              <span className={cx('h-1.5 w-1.5 rounded-full', st.dot)} />
                              {st.label}
                            </span>
                          </div>
                          <p className="truncate font-mono text-[11px] text-muted">{s.transport}</p>
                          {st.errored && s.status_error && (
                            <p className="truncate text-[11px] text-danger" title={s.status_error}>
                              {s.status_error}
                            </p>
                          )}
                        </div>
                        {isEdit && agentsWritable ? (
                          <>
                            <button
                              type="button"
                              aria-label={'Edit ' + s.name}
                              data-testid={'btn-edit-agent-mcp-' + s.id}
                              onClick={() => setAgentMcpDialog({ mode: 'edit', server: s })}
                              className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
                            >
                              <Icon name="edit" size={13} />
                            </button>
                            <button
                              type="button"
                              aria-label={'Remove ' + s.name}
                              data-testid={'btn-remove-agent-mcp-' + s.id}
                              onClick={() => void handleAgentMcpRemove(s)}
                              className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)] hover:text-danger"
                            >
                              <Icon name="x" size={13} />
                            </button>
                          </>
                        ) : null}
                      </div>
                    );
                  })}
                </div>
              ) : (
                <p className="text-[12px] text-muted" data-testid="agent-mcp-servers-empty">
                  {isEdit
                    ? "No private servers — servers added here are attached to this agent only."
                    : "Private servers can be attached to this agent after it is deployed."}
                </p>
              )}
              <p className="mt-1.5 text-[11px] leading-4 text-muted">
                Private servers use the same structured form as Settings → MCP servers and are visible only to this agent.
              </p>
            </div>

            <div>
              <span className={labelCls}>Autonomy</span>
              <Segmented
                value={autonomy}
                onChange={(v) => setAutonomy(v as AgentAutonomy)}
                options={AUTONOMY_OPTIONS.map(({ id, label }) => ({ id, label }))}
              />
              <p className="mt-1.5 text-[11px] leading-4 text-muted">
                {AUTONOMY_OPTIONS.find((o) => o.id === autonomy)?.description}
              </p>
            </div>
          </div>
        ) : null}

        {/* Edit Tab Prompts — generated prompt files, list left / preview right */}
        {isEdit && editTab === 'prompts' && (
          <div className="space-y-4" data-testid="agent-prompts-pane">
            <div className="flex items-center justify-between">
              <MicroLabel>Generated Prompts</MicroLabel>
              <div className="flex items-center gap-2">
                {promptStatus === 'generating' && (
                  <span className="flex items-center gap-1.5 text-[12px] text-accent font-medium">
                    <span className="h-2 w-2 animate-ping rounded-full bg-accent" />
                    Generating prompts…
                  </span>
                )}
                {promptStatus === 'failed' && (
                  <span className="flex items-center gap-1.5 text-[12px] text-danger font-medium">
                    <Icon name="alert" size={13} />
                    Failed
                  </span>
                )}
                {promptStatus === 'ready' && (
                  <span className="flex items-center gap-1.5 text-[12px] text-success font-medium">
                    <Icon name="check" size={13} />
                    Ready
                  </span>
                )}
                <button
                  type="button"
                  onClick={() => setRegenFormOpen((v) => !v)}
                  disabled={promptStatus === 'generating' || regenerating}
                  data-testid="btn-regenerate-prompts"
                  className="flex h-7 items-center gap-1.5 rounded border border-line px-2 text-[11px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40"
                >
                  <Icon name="spark" size={12} />
                  Regenerate
                </button>
              </div>
            </div>

            {regenFormOpen && (
              <div
                className="rounded-md border border-accent/30 bg-[color-mix(in_oklab,var(--accent)_6%,transparent)] p-3"
                data-testid="regenerate-form"
              >
                <label className={labelCls} htmlFor="ac-regen-instruction">
                  What should change?
                </label>
                <textarea
                  id="ac-regen-instruction"
                  data-testid="input-regen-instruction"
                  rows={3}
                  maxLength={2000}
                  autoFocus
                  className={cx(inputCls, "h-auto py-2")}
                  value={regenInstruction}
                  onChange={(e) => setRegenInstruction(e.target.value)}
                  placeholder="e.g. make the tone sharper and add incident-triage duties"
                />
                <p className="mt-1 text-[11px] leading-4 text-muted">
                  The current IDENTITY.md, SOUL.md, and BOOTSTRAP.md are always sent along — the model
                  enhances them with your change applied. Leave empty to enhance without a specific change.
                </p>
                <div className="mt-2 flex items-center justify-end gap-2">
                  <button
                    type="button"
                    onClick={() => {
                      setRegenFormOpen(false);
                      setRegenInstruction('');
                    }}
                    data-testid="btn-cancel-regenerate"
                    className="flex h-8 items-center rounded-md px-3 text-[12px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
                  >
                    Cancel
                  </button>
                  <button
                    type="button"
                    onClick={() => handleRegenerate(regenInstruction.trim())}
                    disabled={regenerating}
                    data-testid="btn-confirm-regenerate"
                    className="flex h-8 items-center gap-1.5 rounded-md bg-accent px-3 text-[12px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] disabled:opacity-40"
                  >
                    <Icon name="spark" size={12} />
                    Enhance prompts
                  </button>
                </div>
              </div>
            )}

            {promptStatus === 'failed' && promptError && (
              <div className="rounded-md border border-danger/30 bg-danger/10 p-2.5 text-[12px] text-danger">
                <p className="font-semibold">Prompt generation failed:</p>
                <p className="mt-0.5">{promptError}</p>
              </div>
            )}

            <div className="grid grid-cols-1 gap-3 md:grid-cols-[190px_minmax(0,1fr)]">
              {/* Left: file list */}
              <div className="flex gap-1.5 overflow-x-auto md:flex-col md:overflow-visible">
                {PROMPT_FILES.map((doc) => (
                  <button
                    key={doc.id}
                    type="button"
                    onClick={() => setSelectedDoc(doc.id)}
                    data-testid={`prompt-file-${doc.id}`}
                    className={cx(
                      "flex shrink-0 items-center justify-between gap-2 rounded-md border px-2.5 py-2 text-left transition-colors",
                      selectedDoc === doc.id
                        ? "border-accent/40 bg-[color-mix(in_oklab,var(--accent)_8%,transparent)]"
                        : "border-line hover:border-[color-mix(in_oklab,var(--fg)_24%,transparent)]"
                    )}
                  >
                    <span className="font-mono text-[12px] text-fg">{doc.name}</span>
                    <span className="hidden text-[10px] text-muted md:inline">{doc.hint}</span>
                  </button>
                ))}
              </div>

              {/* Right: preview of the selected file */}
              <div className="min-w-0">
                {selectedDoc === 'identity' && (
                  <div>
                    <label className={labelCls} htmlFor="ac-identity">
                      <span className="font-mono">IDENTITY.md</span>
                      <span className="text-muted font-normal"> — editable</span>
                    </label>
                    <textarea
                      id="ac-identity"
                      data-testid="input-agent-identity"
                      rows={12}
                      disabled={promptStatus === 'generating'}
                      className={cx(inputCls, "h-auto py-2 font-mono text-[12px] leading-relaxed")}
                      value={identity}
                      onChange={(e) => setIdentity(e.target.value)}
                      placeholder={promptStatus === 'generating' ? "Generating identity…" : "Agent system identity…"}
                    />
                  </div>
                )}

                {selectedDoc === 'soul' && (
                  <div>
                    <label className={labelCls} htmlFor="ac-soul">
                      <span className="font-mono">SOUL.md</span>
                      <span className="text-muted font-normal"> — editable</span>
                    </label>
                    <textarea
                      id="ac-soul"
                      data-testid="input-agent-soul"
                      rows={12}
                      disabled={promptStatus === 'generating'}
                      className={cx(inputCls, "h-auto py-2 font-mono text-[12px] leading-relaxed")}
                      value={soul}
                      onChange={(e) => setSoul(e.target.value)}
                      placeholder={promptStatus === 'generating' ? "Generating soul…" : "Agent soul & behavioral rules…"}
                    />
                  </div>
                )}

                {selectedDoc === 'bootstrap' && (
                  <div>
                    <span className={labelCls}>
                      <span className="font-mono">BOOTSTRAP.md</span>
                      <span className="text-muted font-normal"> — read-only</span>
                    </span>
                    <div
                      data-testid="agent-bootstrap-doc"
                      className="mt-1.5 max-h-72 overflow-auto rounded-md border border-line bg-warm p-3 font-mono text-[12px] leading-relaxed text-fg2 whitespace-pre-wrap"
                    >
                      {bootstrap || "Not generated yet."}
                    </div>
                  </div>
                )}
              </div>
            </div>

            <p className="text-[12px] leading-4 text-muted">
              Regeneration enhances these files and keeps the previous version beside each as {'<name>.bak'}.
            </p>
          </div>
        )}
          </>
        )}

        {agentSkillFormOpen && isEdit ? (
          <AgentSkillAddDialog
            wsSlug={targetWsId}
            agentId={draft?.id || draft?.slug}
            onClose={() => setAgentSkillFormOpen(false)}
            onInstalled={handleAgentSkillInstalled}
          />
        ) : null}

        {agentMcpDialog && isEdit ? (
          <McpServerDialog
            server={agentMcpDialog.mode === 'edit' ? agentMcpDialog.server : null}
            existingServers={agentMcpServers}
            onClose={() => setAgentMcpDialog(null)}
            onSave={handleAgentMcpSave}
          />
        ) : null}
      </div>
    </Modal>
  );
}

// Inline install of an agent-tier skill: authored (name, description, body)
// straight into this agent's skills directory.
function AgentSkillAddDialog({
  wsSlug,
  agentId,
  onClose,
  onInstalled,
}: {
  wsSlug: string;
  agentId: string;
  onClose: () => void;
  onInstalled: (skill: ApiWorkspaceSkill) => void;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [body, setBody] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const slug = name.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
  const valid = slug.length > 0 && body.trim().length > 0;

  const submit = async () => {
    if (!valid) return;
    setSaving(true);
    setError(null);
    try {
      const res = await api.agents.installSkill(wsSlug, agentId, {
        name: slug,
        description: description.trim() || undefined,
        body,
      });
      onInstalled(res.skill);
    } catch (err: unknown) {
      setError(formatApiError(err, "Failed to install skill"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title="Add agent skill"
      onClose={onClose}
      odId="modal-agent-skill-add"
      data-testid="modal-agent-skill-add"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={!valid || saving}
            data-testid="btn-agent-skill-install"
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] disabled:opacity-40"
          >
            {saving ? "Installing…" : "Install for this agent"}
          </button>
        </>
      }
    >
      <div className="space-y-4 p-5">
        {error ? <p className="text-[13px] text-danger">{error}</p> : null}
        <div>
          <label className={labelCls} htmlFor="agent-skill-name">
            Skill name
          </label>
          <input
            id="agent-skill-name"
            className={inputCls}
            value={name}
            aria-label="Agent skill name"
            data-testid="input-agent-skill-name"
            onChange={(e) => setName(e.target.value)}
            autoFocus
          />
        </div>
        <div>
          <label className={labelCls} htmlFor="agent-skill-desc">
            Description <span className="text-muted font-normal">(optional)</span>
          </label>
          <input
            id="agent-skill-desc"
            className={inputCls}
            value={description}
            aria-label="Agent skill description"
            data-testid="input-agent-skill-desc"
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
        <div>
          <label className={labelCls} htmlFor="agent-skill-body">
            SKILL.md body <span className="text-accent font-normal">(markdown)</span>
          </label>
          <textarea
            id="agent-skill-body"
            rows={8}
            className={cx(inputCls, "h-auto py-2 font-mono text-[12px] leading-relaxed")}
            value={body}
            aria-label="Agent skill body"
            data-testid="input-agent-skill-body"
            onChange={(e) => setBody(e.target.value)}
          />
        </div>
      </div>
    </Modal>
  );
}
