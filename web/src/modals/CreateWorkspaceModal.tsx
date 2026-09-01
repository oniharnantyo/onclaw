import { useState } from 'react';
import { Modal } from '../components/ui/Modal';
import { TimezoneSelect } from '../components/ui/TimezoneSelect';
import { ModelCombobox } from '../components/ui/ModelCombobox';
import { AvatarPicker, generateRandomAvatar } from '../components/ui/AvatarPicker';
import { OnboardingLoader } from '../components/ui/OnboardingLoader';
import { Segmented } from '../components/ui/Segmented';
import { inputCls, labelCls } from '../components/ui/constants';
import { PROVIDER_TYPES } from '../lib/constants';
import { cx, slugify } from '../lib/helpers';
import { Icon } from '../components/ui/Icon';
import {
  api,
  formatApiError,
  ApiError,
  type CreateWorkspaceResult,
  type AgentAutonomy,
} from '../lib/api';
import { useAuthStore } from '../store/auth';
import { useStore } from '../store';

export interface CreateWorkspaceModalProps {
  onClose: () => void;
  onCreateSuccess?: (result: CreateWorkspaceResult) => void;
  onToast?: (text: string, kind?: string) => void;
}

const ROLE_SUGGESTIONS = [
  'code-reviewer',
  'customer-support',
  'data-analyst',
  'research-assistant',
  'triage-bot',
  'pricing-monitor',
];

export function CreateWorkspaceModal({
  onClose,
  onCreateSuccess,
  onToast,
}: CreateWorkspaceModalProps) {
  // Wizard steps: 1 = Workspace details, 2 = Provider config, 3 = Starter agent (optional)
  const [step, setStep] = useState<1 | 2 | 3>(1);

  // Step 1: Workspace details
  const [wsName, setWsName] = useState('');
  const [wsSlug, setWsSlug] = useState('');
  const [wsSlugEdited, setWsSlugEdited] = useState(false);
  const [timezone, setTimezone] = useState('America/Los_Angeles');

  // Step 2: Provider config
  const [providerType, setProviderType] = useState('anthropic');
  const [providerName, setProviderName] = useState('Anthropic Primary');
  const [baseURL, setBaseURL] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [selectedModel, setSelectedModel] = useState('');

  // Step 3: Starter agent (slim fields)
  const [includeAgent, setIncludeAgent] = useState(true);
  const [agentName, setAgentName] = useState('Radar');
  const [agentSlug, setAgentSlug] = useState('radar');
  const [agentSlugEdited, setAgentSlugEdited] = useState(false);
  const [agentRole, setAgentRole] = useState('General-purpose research assistant');
  const [agentBrief, setAgentBrief] = useState(
    'Help teammates research topics, summarize documents, and draft clear responses.'
  );
  const [agentModel, setAgentModel] = useState('');
  const [agentAvatar, setAgentAvatar] = useState<Record<string, any>>(generateRandomAvatar());
  const [agentAutonomy, setAgentAutonomy] = useState<AgentAutonomy>('approval');

  // State management
  const [submitting, setSubmitting] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [generalError, setGeneralError] = useState<string | null>(null);

  const toast = onToast || useStore.getState().toast;

  // Handle workspace name changes & slug auto-suggest
  const handleWsNameChange = (val: string) => {
    setWsName(val);
    if (!wsSlugEdited) {
      setWsSlug(slugify(val));
    }
  };

  const handleWsSlugChange = (val: string) => {
    setWsSlugEdited(true);
    setWsSlug(val.toLowerCase().replace(/[^a-z0-9-]/g, ''));
  };

  // Handle provider type change
  const handleProviderTypeChange = (type: string) => {
    setProviderType(type);
    const typeObj = PROVIDER_TYPES.find((t) => t.id === type);
    const defaultLabel = typeObj ? `${typeObj.label} Primary` : `${type} Provider`;
    setProviderName(defaultLabel);
    setSelectedModel('');
  };

  // Handle agent name changes & slug auto-suggest
  const handleAgentNameChange = (val: string) => {
    setAgentName(val);
    if (!agentSlugEdited) {
      setAgentSlug(slugify(val));
    }
  };

  const handleAgentSlugChange = (val: string) => {
    setAgentSlugEdited(true);
    setAgentSlug(val.toLowerCase().replace(/[^a-z0-9-]/g, ''));
  };

  const selectedProviderTypeObj = PROVIDER_TYPES.find((t) => t.id === providerType);
  const requiresBaseURL =
    providerType === 'openai-compatible' || providerType === 'anthropic-compatible';

  // Step 1 Validation
  const validateStep1 = () => {
    const errors: Record<string, string> = {};
    if (!wsName.trim()) errors.wsName = 'Workspace name is required';
    if (!wsSlug.trim()) errors.wsSlug = 'Workspace slug is required';
    setFieldErrors(errors);
    return Object.keys(errors).length === 0;
  };

  // Step 2 Validation
  const validateStep2 = () => {
    const errors: Record<string, string> = {};
    if (!providerName.trim()) errors.providerName = 'Provider name is required';
    if (requiresBaseURL && !baseURL.trim()) errors.baseURL = 'Base URL is required for compatible providers';
    setFieldErrors(errors);
    return Object.keys(errors).length === 0;
  };

  // Step 3 Validation
  const validateStep3 = () => {
    if (!includeAgent) return true;
    const errors: Record<string, string> = {};
    if (!agentName.trim()) errors.agentName = 'Agent name is required';
    if (!agentSlug.trim()) errors.agentSlug = 'Agent slug is required';
    if (!agentRole.trim()) errors.agentRole = 'Role is required';
    if (!agentBrief.trim()) errors.agentBrief = 'Goal & behavior brief is required';
    setFieldErrors(errors);
    return Object.keys(errors).length === 0;
  };

  const handleNextFromStep1 = () => {
    if (validateStep1()) {
      setStep(2);
    }
  };

  const handleNextFromStep2 = () => {
    if (validateStep2()) {
      if (!agentModel) {
        setAgentModel(selectedModel);
      }
      setStep(3);
    }
  };

  // Atomic Birth Submission
  const handleBirth = async (skipAgent = false) => {
    if (!skipAgent && !validateStep3()) {
      return;
    }

    setSubmitting(true);
    setGeneralError(null);
    setFieldErrors({});

    const payload = {
      name: wsName.trim(),
      slug: wsSlug.trim(),
      timezone,
      provider: {
        type: providerType,
        name: providerName.trim(),
        base_url: baseURL.trim() || undefined,
        key: apiKey.trim() || undefined,
        enabled: true,
      },
      starter_agent:
        !skipAgent && includeAgent && agentName.trim()
          ? {
              name: agentName.trim(),
              slug: agentSlug.trim(),
              role: agentRole.trim(),
              description: '',
              brief: agentBrief.trim(),
              model: agentModel.trim() || selectedModel.trim() || 'claude-3-7-sonnet',
              temperature: 1.0,
              autonomy: agentAutonomy,
              avatar: agentAvatar,
              tools: [],
              skills: [],
              mcp: [],
            }
          : undefined,
    };

    try {
      const res = await api.workspaces.create(payload);

      toast(`Workspace "${res.workspace.name}" created!`);

      // Refresh auth memberships
      try {
        const meRes = await api.auth.me();
        if (meRes?.memberships) {
          useAuthStore.setState({ memberships: meRes.memberships });
        }
      } catch {
        // ignore me refresh error
      }

      if (onCreateSuccess) {
        onCreateSuccess(res);
      } else {
        const nextId = res.workspace.slug || res.workspace.id;
        useStore.getState().switchTenant(nextId);
      }

      onClose();
    } catch (err: unknown) {
      if (err instanceof ApiError) {
        if (err.details && err.details.length > 0) {
          const detailMap: Record<string, string> = {};
          err.details.forEach((d) => {
            if (d.field) {
              if (d.field.startsWith('starter_agent.')) {
                const subField = d.field.replace('starter_agent.', '');
                detailMap['agent' + subField.charAt(0).toUpperCase() + subField.slice(1)] = d.message;
                setStep(3);
              } else if (d.field.startsWith('provider.')) {
                const subField = d.field.replace('provider.', '');
                detailMap['provider' + subField.charAt(0).toUpperCase() + subField.slice(1)] = d.message;
                setStep(2);
              } else {
                detailMap[d.field] = d.message;
                setStep(1);
              }
            }
          });
          setFieldErrors(detailMap);
        }
        const msg = formatApiError(err, 'Failed to create workspace');
        setGeneralError(msg);
        toast(msg, 'danger');
      } else {
        const msg = formatApiError(err, 'Failed to create workspace');
        setGeneralError(msg);
        toast(msg, 'danger');
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      title="Create workspace"
      onClose={onClose}
      wide
      odId="modal-create-workspace"
      data-testid="modal-create-workspace"
      footer={
        step === 1 ? (
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
              onClick={handleNextFromStep1}
              data-testid="btn-ws-next-step1"
              className="flex h-9 items-center gap-1.5 rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]"
            >
              Next: Configure provider
              <Icon name="arrow-right" size={14} />
            </button>
          </>
        ) : step === 2 ? (
          <>
            <button
              type="button"
              onClick={() => setStep(1)}
              data-testid="btn-ws-back-step2"
              className="flex h-9 items-center gap-1.5 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
            >
              <Icon name="arrow-left" size={14} />
              Back
            </button>
            <div className="flex-1" />
            <button
              type="button"
              disabled={submitting}
              onClick={() => handleBirth(true)}
              data-testid="btn-ws-skip-starter"
              className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-muted transition-colors hover:border-accent hover:text-fg"
            >
              Skip starter agent &amp; create
            </button>
            <button
              type="button"
              onClick={handleNextFromStep2}
              data-testid="btn-ws-next-step2"
              className="flex h-9 items-center gap-1.5 rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]"
            >
              Next: Starter agent
              <Icon name="arrow-right" size={14} />
            </button>
          </>
        ) : (
          <>
            <button
              type="button"
              onClick={() => setStep(2)}
              data-testid="btn-ws-back-step3"
              className="flex h-9 items-center gap-1.5 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
            >
              <Icon name="arrow-left" size={14} />
              Back
            </button>
            <div className="flex-1" />
            <button
              type="button"
              disabled={submitting}
              onClick={() => handleBirth(false)}
              data-testid="btn-ws-submit-birth"
              className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-50"
            >
              {submitting ? 'Creating workspace…' : 'Create workspace & deploy'}
            </button>
          </>
        )
      }
    >
      <div className="p-5">
        {generalError && (
          <div className="mb-4 rounded-md border border-danger/30 bg-danger/10 p-3 text-[13px] text-danger">
            {generalError}
          </div>
        )}

        {submitting && (
          <OnboardingLoader label={`Creating ${wsName.trim() || "workspace"}…`} />
        )}
        {!submitting && (
        <>
        {/* Step Indicator */}
        <div className="mb-6 flex items-center justify-between border-b border-line pb-3">
          <div className="flex items-center gap-2.5 text-[13px]">
            <span
              className={cx(
                'flex h-6 w-6 items-center justify-center rounded-full text-[12px] font-semibold',
                step === 1 ? 'bg-accent text-accenton' : 'bg-[color-mix(in_oklab,var(--fg)_10%,transparent)] text-muted'
              )}
            >
              1
            </span>
            <span className={cx('font-medium', step === 1 ? 'text-fg' : 'text-muted')}>
              Workspace
            </span>

            <span className="text-muted">/</span>

            <span
              className={cx(
                'flex h-6 w-6 items-center justify-center rounded-full text-[12px] font-semibold',
                step === 2 ? 'bg-accent text-accenton' : 'bg-[color-mix(in_oklab,var(--fg)_10%,transparent)] text-muted'
              )}
            >
              2
            </span>
            <span className={cx('font-medium', step === 2 ? 'text-fg' : 'text-muted')}>
              Provider
            </span>

            <span className="text-muted">/</span>

            <span
              className={cx(
                'flex h-6 w-6 items-center justify-center rounded-full text-[12px] font-semibold',
                step === 3 ? 'bg-accent text-accenton' : 'bg-[color-mix(in_oklab,var(--fg)_10%,transparent)] text-muted'
              )}
            >
              3
            </span>
            <span className={cx('font-medium', step === 3 ? 'text-fg' : 'text-muted')}>
              Starter agent
            </span>
          </div>
        </div>

        {/* Step 1: Workspace details */}
        {step === 1 && (
          <div className="space-y-4 max-w-lg" data-testid="ws-step-1">
            <div>
              <label className={labelCls} htmlFor="ws-name">
                Workspace name
              </label>
              <input
                id="ws-name"
                data-testid="input-ws-name"
                className={cx(inputCls, fieldErrors.wsName && 'border-danger')}
                placeholder="e.g. Acme Corp"
                value={wsName}
                onChange={(e) => handleWsNameChange(e.target.value)}
                autoFocus
              />
              {fieldErrors.wsName && <p className="mt-1 text-[12px] text-danger">{fieldErrors.wsName}</p>}
            </div>

            <div>
              <label className={labelCls} htmlFor="ws-slug">
                Workspace slug
              </label>
              <input
                id="ws-slug"
                data-testid="input-ws-slug"
                className={cx(inputCls, 'font-mono text-[13px]', fieldErrors.wsSlug && 'border-danger')}
                placeholder="e.g. acme"
                value={wsSlug}
                onChange={(e) => handleWsSlugChange(e.target.value)}
              />
              {fieldErrors.wsSlug ? (
                <p className="mt-1 text-[12px] text-danger">{fieldErrors.wsSlug}</p>
              ) : (
                <p className="mt-1 text-[11px] text-muted">
                  Used in URL routes and references. Lowercase letters, numbers, and hyphens.
                </p>
              )}
            </div>

            <div>
              <label className={labelCls} htmlFor="ws-tz">
                Timezone
              </label>
              <TimezoneSelect
                id="ws-tz"
                data-testid="select-ws-tz"
                value={timezone}
                onChange={(tz) => setTimezone(tz)}
              />
            </div>
          </div>
        )}

        {/* Step 2: Provider step */}
        {step === 2 && (
          <div className="space-y-4 max-w-lg" data-testid="ws-step-2">
            <div>
              <label className={labelCls} htmlFor="prov-type">
                Provider Type
              </label>
              <select
                id="prov-type"
                data-testid="select-provider-type"
                className={inputCls}
                value={providerType}
                onChange={(e) => handleProviderTypeChange(e.target.value)}
              >
                {PROVIDER_TYPES.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.label}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label className={labelCls} htmlFor="prov-name">
                Provider Label
              </label>
              <input
                id="prov-name"
                data-testid="input-provider-name"
                className={cx(inputCls, fieldErrors.providerName && 'border-danger')}
                placeholder="e.g. Anthropic Primary"
                value={providerName}
                onChange={(e) => setProviderName(e.target.value)}
              />
              {fieldErrors.providerName && (
                <p className="mt-1 text-[12px] text-danger">{fieldErrors.providerName}</p>
              )}
            </div>

            {requiresBaseURL && (
              <div>
                <label className={labelCls} htmlFor="prov-base-url">
                  Base URL <span className="text-accent font-normal">(required for compatible endpoint)</span>
                </label>
                <input
                  id="prov-base-url"
                  data-testid="input-provider-base-url"
                  className={cx(inputCls, 'font-mono text-[13px]', fieldErrors.baseURL && 'border-danger')}
                  placeholder="https://api.together.xyz/v1"
                  value={baseURL}
                  onChange={(e) => setBaseURL(e.target.value)}
                />
                {fieldErrors.baseURL && <p className="mt-1 text-[12px] text-danger">{fieldErrors.baseURL}</p>}
              </div>
            )}

            <div>
              <label className={labelCls} htmlFor="prov-key">
                API Key
              </label>
              <input
                id="prov-key"
                data-testid="input-provider-key"
                type="password"
                className={inputCls}
                placeholder="sk-…"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
              />
              <p className="mt-1 text-[11px] text-muted">
                Your key is encrypted at rest and never exposed in responses.
              </p>
            </div>

            {/* Model Combobox powered by models-preview */}
            <div className="pt-2">
              <ModelCombobox
                previewCreds={{
                  type: providerType,
                  base_url: baseURL,
                  key: apiKey,
                }}
                model={selectedModel}
                onModelChange={setSelectedModel}
                hideEffort
              />
            </div>
          </div>
        )}

        {/* Step 3: Starter Agent (slim fields) */}
        {step === 3 && (
          <div className="space-y-4 max-w-lg" data-testid="ws-step-3">
            <div className="flex items-center justify-between rounded-lg border border-line bg-warm p-3">
              <div>
                <p className="text-[13px] font-semibold text-fg">Deploy Starter Agent</p>
                <p className="text-[12px] text-muted">A starter agent will be ready to chat immediately upon birth.</p>
              </div>
              <input
                type="checkbox"
                checked={includeAgent}
                onChange={(e) => setIncludeAgent(e.target.checked)}
                className="h-4 w-4 rounded border-line text-accent cursor-pointer"
              />
            </div>

            {includeAgent && (
              <div className="space-y-4 pt-2">
                <div>
                  <label className={labelCls}>Avatar</label>
                  <AvatarPicker value={agentAvatar} onChange={setAgentAvatar} />
                </div>

                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className={labelCls} htmlFor="starter-agent-name">
                      Agent name
                    </label>
                    <input
                      id="starter-agent-name"
                      data-testid="input-starter-agent-name"
                      className={cx(inputCls, fieldErrors.agentName && 'border-danger')}
                      placeholder="e.g. Radar"
                      value={agentName}
                      onChange={(e) => handleAgentNameChange(e.target.value)}
                    />
                    {fieldErrors.agentName && (
                      <p className="mt-1 text-[12px] text-danger">{fieldErrors.agentName}</p>
                    )}
                  </div>
                  <div>
                    <label className={labelCls} htmlFor="starter-agent-slug">
                      Slug
                    </label>
                    <input
                      id="starter-agent-slug"
                      data-testid="input-starter-agent-slug"
                      className={cx(inputCls, 'font-mono text-[13px]', fieldErrors.agentSlug && 'border-danger')}
                      placeholder="e.g. radar"
                      value={agentSlug}
                      onChange={(e) => handleAgentSlugChange(e.target.value)}
                    />
                    {fieldErrors.agentSlug && (
                      <p className="mt-1 text-[12px] text-danger">{fieldErrors.agentSlug}</p>
                    )}
                  </div>
                </div>

                <div>
                  <label className={labelCls} htmlFor="starter-agent-role">
                    Role in one line
                  </label>
                  <input
                    id="starter-agent-role"
                    data-testid="input-starter-agent-role"
                    className={cx(inputCls, fieldErrors.agentRole && 'border-danger')}
                    placeholder="e.g. watches competitor pricing"
                    value={agentRole}
                    onChange={(e) => setAgentRole(e.target.value)}
                  />
                  <div className="mt-1.5 flex flex-wrap gap-1.5">
                    {ROLE_SUGGESTIONS.map((sug) => (
                      <button
                        key={sug}
                        type="button"
                        onClick={() => setAgentRole(sug)}
                        className="rounded bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] px-2 py-0.5 font-mono text-[11px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_12%,transparent)] hover:text-fg"
                      >
                        {sug}
                      </button>
                    ))}
                  </div>
                  {fieldErrors.agentRole && (
                    <p className="mt-1 text-[12px] text-danger">{fieldErrors.agentRole}</p>
                  )}
                </div>

                <div>
                  <label className={labelCls} htmlFor="starter-agent-brief">
                    Goal &amp; behavior brief
                  </label>
                  <textarea
                    id="starter-agent-brief"
                    data-testid="input-starter-agent-brief"
                    rows={3}
                    className={cx(inputCls, 'h-auto py-2 leading-relaxed', fieldErrors.agentBrief && 'border-danger')}
                    value={agentBrief}
                    onChange={(e) => setAgentBrief(e.target.value)}
                    placeholder="Explain what this agent does and how it should behave…"
                  />
                  {fieldErrors.agentBrief && (
                    <p className="mt-1 text-[12px] text-danger">{fieldErrors.agentBrief}</p>
                  )}
                </div>

                <div>
                  <label className={labelCls} htmlFor="starter-agent-model">
                    Model
                  </label>
                  <input
                    id="starter-agent-model"
                    data-testid="input-starter-agent-model"
                    className={cx(inputCls, 'font-mono text-[13px]')}
                    placeholder="Model identifier…"
                    value={agentModel || selectedModel}
                    onChange={(e) => setAgentModel(e.target.value)}
                  />
                </div>

                <div>
                  <label className={labelCls}>Autonomy</label>
                  <Segmented
                    value={agentAutonomy}
                    onChange={(v) => setAgentAutonomy(v as AgentAutonomy)}
                    options={[
                      { id: 'approval', label: 'Act with approval' },
                      { id: 'suggest', label: 'Suggest only' },
                      { id: 'full', label: 'Fully autonomous' },
                    ]}
                  />
                </div>
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
