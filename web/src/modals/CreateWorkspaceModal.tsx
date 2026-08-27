// @ts-nocheck
import { useState } from "react";
import { cx, uid, slugify, fmtUses, providerOf } from "../lib/helpers";
import { Icon } from "../components/ui/Icon";
import { Modal } from "../components/ui/Modal";
import { Toggle } from "../components/ui/Toggle";
import { Avatar } from "../components/ui/Avatar";
import { Chip } from "../components/ui/Chip";
import { inputCls, labelCls } from "../components/ui/constants";
import { PROVIDERS, MODELS, TOOLS, SKILLS, MCP_SERVERS } from "../data/seed";

import { Segmented } from "../components/ui/Segmented";
import { blankTenant } from "../data/seed";

export function CreateWorkspaceModal({ onClose, onCreate, existingSubs }) {
  const [name, setName] = useState('');
  const [sub, setSub] = useState('');
  const [slugTouched, setSlugTouched] = useState(false);
  const [plan, setPlan] = useState('Free');
  const [tz, setTz] = useState('America/Los_Angeles');
  const [starter, setStarter] = useState(true);
  const derived = name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 24);
  const slug = slugTouched ? sub : derived;
  const taken = existingSubs.includes(slug);
  const patternOk = /^[a-z0-9-]*$/.test(slug);
  const valid = name.trim().length > 1 && slug.length > 1 && patternOk && !taken;
  return (
    <Modal title="Create a workspace" onClose={onClose} odId="ws-create-modal"
      footer={<>
        <button type="button" onClick={onClose} className="flex h-9 items-center rounded-md px-3.5 text-[13px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2">Cancel</button>
        <button type="button" disabled={!valid} data-od-id="btn-ws-create" onClick={() => onCreate(blankTenant({ name: name.trim(), sub: slug, plan, tz, starter }))}
          className="flex h-9 items-center rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40 disabled:hover:bg-accent">
          Create workspace
        </button>
      </>}>
      <div className="space-y-5 p-5">
        <div>
          <label className={labelCls} htmlFor="cw-name">Workspace name</label>
          <input id="cw-name" className={inputCls} value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Initech" autoFocus/>
        </div>
        <div>
          <label className={labelCls} htmlFor="cw-sub">Workspace URL</label>
          <div className="flex items-center gap-2">
            <input id="cw-sub" className={cx(inputCls, 'font-mono text-[13px]')} value={slug}
              onChange={(e) => { setSub(e.target.value); setSlugTouched(true); }} placeholder="initech"/>
            <span className="shrink-0 font-mono text-[13px] text-muted">.onclaw.app</span>
          </div>
          {taken && <p className="mt-1.5 text-[11px] text-danger">That URL is taken — pick another.</p>}
          {!patternOk && <p className="mt-1.5 text-[11px] text-danger">Lowercase letters, digits and hyphens only.</p>}
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <span className={labelCls}>Plan</span>
            <Segmented value={plan.toLowerCase()} onChange={(v) => setPlan(v === 'pro' ? 'Pro' : 'Free')}
              options={[{ id: 'free', label: 'Free' }, { id: 'pro', label: 'Pro' }]}/>
          </div>
          <div>
            <label className={labelCls} htmlFor="cw-tz">Timezone</label>
            <select id="cw-tz" className={inputCls} value={tz} onChange={(e) => setTz(e.target.value)}>
              {['America/Los_Angeles', 'America/New_York', 'Europe/London', 'Europe/Berlin', 'Asia/Singapore'].map((z) => <option key={z}>{z}</option>)}
            </select>
          </div>
        </div>
        <div className="flex items-center justify-between rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] px-4 py-3">
          <div>
            <p className="text-[13px] font-medium text-fg">Include a starter agent</p>
            <p className="text-[12px] text-muted">Adds “Guide” and a #general channel so the workspace is usable immediately.</p>
          </div>
          <Toggle on={starter} onChange={setStarter} label="Include a starter agent"/>
        </div>
      </div>
    </Modal>
  );
}

