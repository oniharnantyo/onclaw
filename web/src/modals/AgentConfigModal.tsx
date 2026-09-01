import { useState, useEffect, useMemo, useCallback } from "react";
import { cx, providerOf, slugify } from "../lib/helpers";
import { Modal } from "../components/ui/Modal";
import { inputCls, labelCls } from "../components/ui/constants";
import { PROVIDER_TYPES, TOOLS, SKILLS, MCP_SERVERS } from "../lib/constants";
import { Segmented } from "../components/ui/Segmented";
import { OptionChips } from "../components/ui/OptionChips";
import { MicroLabel } from "../components/ui/MicroLabel";
import { Icon } from "../components/ui/Icon";
import { AvatarPicker, generateRandomAvatar } from "../components/ui/AvatarPicker";
import { ModelCombobox } from "../components/ui/ModelCombobox";
import { OnboardingLoader } from "../components/ui/OnboardingLoader";
import {
  api,
  formatApiError,
  type ApiProviderConfig,
  type ApiWorkspaceSkill,
  type ApiAgentMemory,
  type AgentAutonomy,
} from "../lib/api";
import { useWorkspace, useStore } from "../store";

const TOOL_ICON: Record<string, string> = {
  web: "globe",
  files: "file",
  shell: "terminal",
  api: "link",
  db: "db",
};

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
  skillOptions?: Array<{ id: string; label: string }>;
  tenant?: any;
}

export function AgentConfigModal({
  draft,
  onClose,
  onSave,
  skillOptions: propSkillOptions,
  tenant,
}: AgentConfigModalProps) {
  const isEdit = Boolean(draft);
  const currentWs = useWorkspace();
  const targetWsId =
    tenant?.sub || tenant?.id || currentWs?.sub || currentWs?.id || useStore.getState().pos.tenantId;

  // Step in wizard (1 = Identity, 2 = Model, 3 = Capabilities)
  const [step, setStep] = useState<1 | 2 | 3>(1);

  // Tab in edit mode ('identity' | 'capabilities' | 'prompts' | 'memory')
  const [editTab, setEditTab] = useState<'identity' | 'model' | 'capabilities' | 'prompts' | 'memory'>('identity');

  // Loading detail state for edit mode
  const [loadingDetail, setLoadingDetail] = useState(isEdit);

  // Configured providers
  const [configuredProviders, setConfiguredProviders] = useState<ApiProviderConfig[]>(
    tenant?.providers || currentWs?.providers || []
  );

  // Workspace custom skills
  const [workspaceSkills, setWorkspaceSkills] = useState<ApiWorkspaceSkill[]>([]);

  // User's own memory for the agent (in edit mode)
  const [memory, setMemory] = useState<ApiAgentMemory | null>(null);
  const [loadingMemory, setLoadingMemory] = useState(false);

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
  const [effort, setEffort] = useState<string | null>(null);
  const [availableEfforts, setAvailableEfforts] = useState<string[]>([]);
  const [autonomy, setAutonomy] = useState<AgentAutonomy>('approval');

  const [tools, setTools] = useState<string[]>(["web"]);
  const [skills, setSkills] = useState<string[]>(["research"]);
  const [mcp, setMcp] = useState<string[]>([]);

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
            setEffort(a.effort || null);
            if (a.autonomy && ['approval', 'suggest', 'full'].includes(a.autonomy)) {
              setAutonomy(a.autonomy as AgentAutonomy);
            }
            if (a.tools) setTools([...a.tools]);
            if (a.skills) setSkills([...a.skills]);
            if (a.mcp) setMcp([...a.mcp]);
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
    }
    return () => {
      mounted = false;
    };
  }, [targetWsId]);

  // Load memory in edit mode
  useEffect(() => {
    let mounted = true;
    const agentId = draft?.id || draft?.slug;
    if (isEdit && agentId && targetWsId) {
      setLoadingMemory(true);
      api.agents
        .getMemory(targetWsId, agentId)
        .then((res) => {
          if (mounted && res) {
            setMemory(res);
          }
        })
        .catch(() => {})
        .finally(() => {
          if (mounted) setLoadingMemory(false);
        });
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

  // Skill options merged
  const availableSkillOptions = useMemo(() => {
    if (propSkillOptions && propSkillOptions.length > 0) return propSkillOptions;
    const base = SKILLS.map((s) => ({ id: s.id, label: s.label }));
    const custom = workspaceSkills
      .filter((ws) => ws.enabled && !base.some((b) => b.id === ws.name || b.id === ws.id))
      .map((ws) => ({ id: ws.name, label: ws.name }));
    return [...base, ...custom];
  }, [propSkillOptions, workspaceSkills]);

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
    if (Object.keys(step2Errors).length > 0) {
      setFieldErrors(step2Errors);
      if (step2Errors.temp || step2Errors.maxTokens || step2Errors.effort) {
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

  const handleCreateSubmit = async (overrideCapabilities?: { tools: string[]; skills: string[]; mcp: string[] }) => {
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

    const finalTools = overrideCapabilities ? overrideCapabilities.tools : tools;
    const finalSkills = overrideCapabilities ? overrideCapabilities.skills : skills;
    const finalMcp = overrideCapabilities ? overrideCapabilities.mcp : mcp;

    const parsedMaxTokens = maxTokens.trim() ? parseInt(maxTokens.trim(), 10) : undefined;

    const payload = {
      name: name.trim(),
      slug: slug.trim(),
      role: role.trim(),
      description: description.trim(),
      brief: brief.trim(),
      provider_id: provider,
      model: model.trim(),
      temperature: temp,
      max_tokens: parsedMaxTokens,
      effort: effort || undefined,
      autonomy,
      tools: finalTools,
      skills: finalSkills,
      mcp: finalMcp,
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
      effort: effort || undefined,
      autonomy,
      tools,
      skills,
      mcp,
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

  const handleResetMemory = async () => {
    const agentId = draft?.id || draft?.slug;
    if (!targetWsId || !agentId) return;
    try {
      await api.agents.deleteMemory(targetWsId, agentId);
      setMemory(null);
      useStore.getState().toast("Agent memory reset successfully");
    } catch (err: unknown) {
      useStore.getState().toast(formatApiError(err, "Failed to reset memory"), "danger");
    }
  };

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
            <button
              type="button"
              onClick={() => setEditTab('memory')}
              className={cx(
                "border-b-2 px-4 py-2 text-[13px] font-medium transition-colors",
                editTab === 'memory'
                  ? "border-accent text-fg font-semibold"
                  : "border-transparent text-muted hover:text-fg"
              )}
            >
              Memory
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
              <OptionChips
                options={TOOLS}
                value={tools}
                onChange={setTools}
                iconOf={(o: any) => TOOL_ICON[o.id]}
              />
            </div>

            <div>
              <span className={labelCls}>Skills</span>
              <OptionChips
                options={availableSkillOptions}
                value={skills}
                onChange={setSkills}
              />
              <p className="mt-1.5 text-[11px] leading-4 text-muted">
                Skills extend agent behavior with specialized instructions. Configured in Settings → Skills.
              </p>
            </div>

            <div>
              <span className={labelCls}>MCP Servers</span>
              <OptionChips options={MCP_SERVERS} value={mcp} onChange={setMcp} />
              <p className="mt-1.5 text-[11px] leading-4 text-muted">
                Connectors configured in Settings → MCP servers exposed over the Model Context Protocol.
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

        {/* Edit Tab Memory */}
        {isEdit && editTab === 'memory' && (
          <div className="space-y-4 max-w-xl" data-testid="agent-memory-pane">
            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-[14px] font-semibold text-fg">Your personal agent memory</h3>
                <p className="text-[12px] text-muted">
                  Memories this agent has recorded from your past conversations. Only visible to you.
                </p>
              </div>
              {memory && memory.content && (
                <button
                  type="button"
                  onClick={handleResetMemory}
                  data-testid="btn-reset-memory"
                  className="flex h-8 items-center gap-1.5 rounded-md border border-danger/40 bg-danger/10 px-3 text-[12px] font-medium text-danger transition-colors hover:bg-danger/20"
                >
                  <Icon name="trash" size={13} />
                  Reset memory
                </button>
              )}
            </div>

            {loadingMemory ? (
              <div className="py-8 text-center text-[13px] text-muted font-mono">Loading memory…</div>
            ) : memory && memory.content ? (
              <div className="rounded-lg border border-line bg-warm p-4 font-mono text-[12px] leading-relaxed text-fg whitespace-pre-wrap">
                {memory.content}
              </div>
            ) : (
              <div className="rounded-lg border border-dashed border-line p-6 text-center text-[13px] text-muted">
                No memories recorded yet for your conversations with this agent.
              </div>
            )}
          </div>
        )}
          </>
        )}
      </div>
    </Modal>
  );
}
