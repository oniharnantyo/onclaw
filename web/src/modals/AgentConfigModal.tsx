import { useState, useEffect, useMemo } from "react";
import { cx, providerOf } from "../lib/helpers";
import { Modal } from "../components/ui/Modal";
import { Toggle } from "../components/ui/Toggle";
import { inputCls, labelCls } from "../components/ui/constants";
import { PROVIDER_TYPES, PROVIDER_MODELS, TOOLS, SKILLS, MCP_SERVERS } from "../lib/constants";
import { Segmented } from "../components/ui/Segmented";
import { OptionChips } from "../components/ui/OptionChips";
import { MicroLabel } from "../components/ui/MicroLabel";
import { api, type ApiProviderConfig } from "../lib/api";
import { useWorkspace, useStore } from "../store";

const TOOL_ICON: Record<string, string> = { web: "globe", files: "file", shell: "terminal", api: "link", db: "db" };

export function AgentConfigModal({ draft, onClose, onSave, skillOptions, tenant }: any) {
  const isEdit = Boolean(draft);
  const currentWs = useWorkspace();

  const targetWsId = tenant?.sub || tenant?.id || currentWs?.sub || currentWs?.id || useStore.getState().pos.tenantId;

  const [configuredProviders, setConfiguredProviders] = useState<ApiProviderConfig[]>(
    tenant?.providers || currentWs?.providers || []
  );

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
        .catch(() => {
          if (mounted && (tenant?.providers || currentWs?.providers)) {
            setConfiguredProviders(tenant?.providers || currentWs?.providers || []);
          }
        });
    }
    return () => {
      mounted = false;
    };
  }, [targetWsId, tenant, currentWs]);

  const initialProvider = useMemo(() => {
    if (draft?.provider) {
      const match = configuredProviders.find(
        (p) => p.id === draft.provider || p.type === draft.provider
      );
      if (match) return match.id;
      return draft.provider;
    }
    if (draft?.model) {
      const pType = providerOf(draft.model);
      const match = configuredProviders.find((p) => p.type === pType);
      if (match) return match.id;
    }
    if (configuredProviders.length > 0) {
      return configuredProviders[0].id;
    }
    return 'anthropic';
  }, [draft, configuredProviders]);

  const [name, setName] = useState(draft ? draft.name : '');
  const [provider, setProvider] = useState<string>(initialProvider);

  // Sync provider when configuredProviders load and we have no valid selection
  useEffect(() => {
    if (configuredProviders.length > 0) {
      const exists = configuredProviders.some((p) => p.id === provider || p.type === provider);
      if (!exists && !draft?.provider) {
        setProvider(configuredProviders[0].id);
      }
    }
  }, [configuredProviders, provider, draft]);

  // Selected provider object and its capability type
  const selectedProviderObj = configuredProviders.find(
    (p) => p.id === provider || p.type === provider
  );
  const selectedType = selectedProviderObj ? selectedProviderObj.type : provider;
  const availableModels = PROVIDER_MODELS[selectedType] || [];

  const initialModel = draft ? draft.model : availableModels[0] || 'claude-sonnet-5';
  const [model, setModel] = useState<string>(initialModel);
  const [isCustomModel, setIsCustomModel] = useState<boolean>(
    Boolean(draft?.model && availableModels.length > 0 && !availableModels.includes(draft.model)) ||
      availableModels.length === 0
  );
  const [customModelText, setCustomModelText] = useState<string>(
    draft?.model && !availableModels.includes(draft.model) ? draft.model : ''
  );

  const [temp, setTemp] = useState(draft ? draft.temp : 0.4);
  const [role, setRole] = useState(draft ? draft.role : '');
  const [tools, setTools] = useState(draft ? [...draft.tools] : ['web']);
  const [skills, setSkills] = useState(draft ? [...(draft.skills || ['research'])] : ['research']);
  const [mcp, setMcp] = useState(draft ? [...(draft.mcp || [])] : []);
  const [autonomy, setAutonomy] = useState(draft ? draft.autonomy : 'approval');
  const [prompt, setPrompt] = useState(draft ? draft.prompt : '');
  const [channelPost, setChannelPost] = useState(draft ? draft.channelPost : false);

  const valid = name.trim().length > 1;

  const handleProviderChange = (newProviderId: string) => {
    setProvider(newProviderId);
    const newProv = configuredProviders.find(
      (p) => p.id === newProviderId || p.type === newProviderId
    );
    const newType = newProv ? newProv.type : newProviderId;
    const newModels = PROVIDER_MODELS[newType] || [];

    if (newModels.length === 0) {
      setIsCustomModel(true);
      if (!customModelText) {
        setCustomModelText(model || '');
      }
    } else {
      if (!newModels.includes(model)) {
        setIsCustomModel(false);
        setModel(newModels[0]);
      }
    }
  };

  const handleModelSelectChange = (value: string) => {
    if (value === '__custom__') {
      setIsCustomModel(true);
      if (!customModelText) {
        setCustomModelText(model);
      }
    } else {
      setIsCustomModel(false);
      setModel(value);
    }
  };

  const finalModel = isCustomModel ? customModelText.trim() || model : model;

  return (
    <Modal
      title={isEdit && draft?.name ? 'Configure ' + draft.name : 'Deploy a new agent'}
      onClose={onClose}

      wide
      odId="agent-config-modal"
      footer={
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
            disabled={!valid}
            data-od-id="btn-agent-save-modal"
            data-testid="btn-agent-save-modal"
            onClick={() =>
              onSave({
                name: name.trim(),
                role: role.trim() || 'General-purpose agent',
                provider,
                model: finalModel,
                temp,
                autonomy,
                tools,
                skills,
                mcp,
                prompt: prompt.trim(),
                channelPost,
              })
            }
            className="flex h-9 items-center rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40 disabled:hover:bg-accent"
          >
            {isEdit ? 'Save changes' : 'Deploy agent'}
          </button>
        </>
      }
    >
      <div className="grid gap-6 p-5 md:grid-cols-2">
        <div className="space-y-5">
          <MicroLabel>Identity &amp; behavior</MicroLabel>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className={labelCls} htmlFor="ac-name">
                Agent name
              </label>
              <input
                id="ac-name"
                className={inputCls}
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Radar"
                autoFocus
              />
            </div>
            <div>
              <label className={labelCls} htmlFor="ac-provider">
                Provider
              </label>
              <select
                id="ac-provider"
                aria-label="Provider"
                className={inputCls}
                value={provider}
                onChange={(e) => handleProviderChange(e.target.value)}
              >
                {/* Configured providers */}
                {configuredProviders.map((p) => {
                  const typeObj = PROVIDER_TYPES.find((t) => t.id === p.type);
                  const typeLabel = typeObj ? typeObj.label : p.type;
                  return (
                    <option key={p.id} value={p.id}>
                      {p.name} ({typeLabel})
                    </option>
                  );
                })}

                {/* Unconfigured catalog types as disabled options */}
                {PROVIDER_TYPES.filter(
                  (t) => !configuredProviders.some((p) => p.type === t.id)
                ).map((t) => (
                  <option key={'unconfigured-' + t.id} value={t.id} disabled>
                    {t.label} (Configure in Settings → Providers)
                  </option>
                ))}

                {/* Fallback if no providers exist and legacy selection */}
                {configuredProviders.length === 0 && (
                  <option value={provider} disabled>
                    No providers configured (Settings → Providers)
                  </option>
                )}
              </select>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className={labelCls} htmlFor="ac-model">
                Model
              </label>
              <select
                id="ac-model"
                aria-label="Model"
                className={inputCls}
                value={isCustomModel ? '__custom__' : model}
                onChange={(e) => handleModelSelectChange(e.target.value)}
              >
                {availableModels.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
                <option value="__custom__">Custom model ID…</option>
              </select>
              {isCustomModel && (
                <div className="mt-2">
                  <input
                    id="ac-custom-model"
                    aria-label="Custom model ID"
                    className={cx(inputCls, 'font-mono text-[13px]')}
                    placeholder="e.g. meta-llama/llama-3.3-70b"
                    value={customModelText}
                    onChange={(e) => {
                      setCustomModelText(e.target.value);
                      setModel(e.target.value);
                    }}
                  />
                </div>
              )}
            </div>
            <div>
              <div className="mb-1.5 flex items-baseline justify-between">
                <label className={cx(labelCls, 'mb-0')} htmlFor="ac-temp">
                  Temperature
                </label>
                <span className="font-mono text-[12px] text-fg2">{Number(temp).toFixed(1)}</span>
              </div>
              <input
                id="ac-temp"
                type="range"
                min="0"
                max="1"
                step="0.1"
                value={temp}
                onChange={(e) => setTemp(Number(e.target.value))}
                className="h-1.5 w-full cursor-pointer"
              />
              <div className="mt-1 flex justify-between font-mono text-[10px] text-muted">
                <span>precise</span>
                <span>balanced</span>
                <span>loose</span>
              </div>
            </div>
          </div>

          <div>
            <label className={labelCls} htmlFor="ac-role">
              Role in one line
            </label>
            <input
              id="ac-role"
              className={inputCls}
              value={role}
              onChange={(e) => setRole(e.target.value)}
              placeholder="e.g. watches competitor pricing and flags moves over 5%"
            />
          </div>

          <div>
            <label className={labelCls} htmlFor="ac-prompt">
              System prompt
            </label>
            <textarea
              id="ac-prompt"
              rows={7}
              className={cx(inputCls, 'h-auto py-2.5 leading-6')}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="Leave empty to start from the base template — refine it anytime."
            />
          </div>
        </div>

        <div className="space-y-5">
          <MicroLabel>Capabilities</MicroLabel>
          <div>
            <span className={labelCls}>Tools</span>
            <OptionChips
              options={TOOLS}
              value={tools}
              onChange={setTools}
              iconOf={(o: any) => TOOL_ICON[o.id]}
            />
          </div>
          <div>
            <span className={labelCls}>Skills</span>
            <OptionChips options={skillOptions || SKILLS} value={skills} onChange={setSkills} />
            <p className="mt-1.5 text-[11px] leading-4 text-muted">
              Installed and versioned in Settings → Skills.
            </p>
          </div>
          <div>
            <span className={labelCls}>MCP servers</span>
            <OptionChips options={MCP_SERVERS} value={mcp} onChange={setMcp} />
            <p className="mt-1.5 text-[11px] leading-4 text-muted">
              Connectors are managed in Settings → MCP servers and exposed to the agent over the Model Context Protocol.
            </p>
          </div>
          <div>
            <span className={labelCls}>Autonomy</span>
            <Segmented
              value={autonomy}
              onChange={setAutonomy}
              options={[
                { id: 'suggest', label: 'Suggest only' },
                { id: 'approval', label: 'Act with approval' },
                { id: 'full', label: 'Fully autonomous' },
              ]}
            />
          </div>
          <div className="flex items-center justify-between rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_35%,var(--surface))] px-4 py-3">
            <div>
              <p className="text-[13px] font-medium text-fg">Post to channels on my behalf</p>
              <p className="text-[12px] text-muted">Lets this agent publish into its bound channels.</p>
            </div>
            <Toggle on={channelPost} onChange={setChannelPost} label="Post to channels on my behalf" />
          </div>
        </div>
      </div>
    </Modal>
  );
}
