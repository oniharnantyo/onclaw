import { useState } from "react";
import { cx, uid, slugify, fmtUses, providerOf } from "../lib/helpers";
import { Icon } from "../components/ui/Icon";
import { Modal } from "../components/ui/Modal";
import { Toggle } from "../components/ui/Toggle";
import { inputCls, labelCls } from "../components/ui/constants";
import { PROVIDERS, MODELS, TOOLS, SKILLS, MCP_SERVERS } from "../lib/constants";

import { Segmented } from "../components/ui/Segmented";
import { OptionChips } from "../components/ui/OptionChips";
import { MicroLabel } from "../components/ui/MicroLabel";
const TOOL_ICON = { web: "globe", files: "file", shell: "terminal", api: "link", db: "db" };

export function AgentConfigModal({ draft, onClose, onSave, skillOptions  }: any) {
  const isEdit = !!draft;
  const [name, setName] = useState(draft ? draft.name : '');
  const [provider, setProvider] = useState(draft ? providerOf(draft.model) : PROVIDERS[0].id);
  const [model, setModel] = useState(draft ? draft.model : PROVIDERS[0].models[0]);
  const [temp, setTemp] = useState(draft ? draft.temp : 0.4);
  const [role, setRole] = useState(draft ? draft.role : '');
  const [tools, setTools] = useState(draft ? [...draft.tools] : ['web']);
  const [skills, setSkills] = useState(draft ? [...(draft.skills || ['research'])] : ['research']);
  const [mcp, setMcp] = useState(draft ? [...(draft.mcp || [])] : []);
  const [autonomy, setAutonomy] = useState(draft ? draft.autonomy : 'approval');
  const [prompt, setPrompt] = useState(draft ? draft.prompt : '');
  const [channelPost, setChannelPost] = useState(draft ? draft.channelPost : false);
  const providerObj = PROVIDERS.find((p: any) => p.id === provider) || PROVIDERS[0];
  const valid = name.trim().length > 1;
  return (
    <Modal title={isEdit ? 'Configure ' + draft.name : 'Deploy a new agent'} onClose={onClose} wide odId="agent-config-modal"
      footer={<>
        <button type="button" onClick={onClose} className="flex h-9 items-center rounded-md px-3.5 text-[13px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2">Cancel</button>
        <button type="button" disabled={!valid} data-od-id="btn-agent-save-modal"
          onClick={() => onSave({ name: name.trim(), role: role.trim() || 'General-purpose agent', provider, model, temp, autonomy, tools, skills, mcp, prompt: prompt.trim(), channelPost })}
          className="flex h-9 items-center rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40 disabled:hover:bg-accent">
          {isEdit ? 'Save changes' : 'Deploy agent'}
        </button>
      </>}>
      <div className="grid gap-6 p-5 md:grid-cols-2">
        <div className="space-y-5">
          <MicroLabel>Identity &amp; behavior</MicroLabel>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className={labelCls} htmlFor="ac-name">Agent name</label>
              <input id="ac-name" className={inputCls} value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Radar" autoFocus/>
            </div>
            <div>
              <label className={labelCls} htmlFor="ac-provider">Provider</label>
              <select id="ac-provider" className={inputCls} value={provider}
                onChange={(e) => {
                  const pid = e.target.value;
                  setProvider(pid);
                  const p = PROVIDERS.find((x: any) => x.id === pid);
                  if (!p.models.includes(model)) setModel(p.models[0]);
                }}>
                {PROVIDERS.map((p: any) => <option key={p.id} value={p.id}>{p.label}</option>)}
              </select>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className={labelCls} htmlFor="ac-model">Model</label>
              <select id="ac-model" className={inputCls} value={model} onChange={(e) => setModel(e.target.value)}>
                {providerObj.models.map((m: any) => <option key={m} value={m}>{m}</option>)}
              </select>
            </div>
            <div>
              <div className="mb-1.5 flex items-baseline justify-between">
                <label className={cx(labelCls, 'mb-0')} htmlFor="ac-temp">Temperature</label>
                <span className="font-mono text-[12px] text-fg2">{Number(temp).toFixed(1)}</span>
              </div>
              <input id="ac-temp" type="range" min="0" max="1" step="0.1" value={temp} onChange={(e) => setTemp(Number(e.target.value))} className="h-1.5 w-full cursor-pointer"/>
              <div className="mt-1 flex justify-between font-mono text-[10px] text-muted"><span>precise</span><span>balanced</span><span>loose</span></div>
            </div>
          </div>
          <div>
            <label className={labelCls} htmlFor="ac-role">Role in one line</label>
            <input id="ac-role" className={inputCls} value={role} onChange={(e) => setRole(e.target.value)} placeholder="e.g. watches competitor pricing and flags moves over 5%"/>
          </div>
          <div>
            <label className={labelCls} htmlFor="ac-prompt">System prompt</label>
            <textarea id="ac-prompt" rows={7} className={cx(inputCls, 'h-auto py-2.5 leading-6')} value={prompt} onChange={(e) => setPrompt(e.target.value)}
              placeholder="Leave empty to start from the base template — refine it anytime."/>
          </div>
        </div>
        <div className="space-y-5">
          <MicroLabel>Capabilities</MicroLabel>
          <div>
            <span className={labelCls}>Tools</span>
            <OptionChips options={TOOLS} value={tools} onChange={setTools} iconOf={(o) => TOOL_ICON[o.id]}/>
          </div>
          <div>
            <span className={labelCls}>Skills</span>
            <OptionChips options={skillOptions || SKILLS} value={skills} onChange={setSkills}/>
            <p className="mt-1.5 text-[11px] leading-4 text-muted">Installed and versioned in Settings → Skills.</p>
          </div>
          <div>
            <span className={labelCls}>MCP servers</span>
            <OptionChips options={MCP_SERVERS} value={mcp} onChange={setMcp}/>
            <p className="mt-1.5 text-[11px] leading-4 text-muted">Connectors are managed in Settings → MCP servers and exposed to the agent over the Model Context Protocol.</p>
          </div>
          <div>
            <span className={labelCls}>Autonomy</span>
            <Segmented value={autonomy} onChange={setAutonomy}
              options={[{ id: 'suggest', label: 'Suggest only' }, { id: 'approval', label: 'Act with approval' }, { id: 'full', label: 'Fully autonomous' }]}/>
          </div>
          <div className="flex items-center justify-between rounded-md border border-line bg-[color-mix(in_oklab,var(--bg),35%,var(--surface))] px-4 py-3">
            <div>
              <p className="text-[13px] font-medium text-fg">Post to channels on my behalf</p>
              <p className="text-[12px] text-muted">Lets this agent publish into its bound channels.</p>
            </div>
            <Toggle on={channelPost} onChange={setChannelPost} label="Post to channels on my behalf"/>
          </div>
        </div>
      </div>
    </Modal>
  );
}

