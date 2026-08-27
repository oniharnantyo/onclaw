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

export function CronEditorModal({ job, tenant, onClose, onSave, onDelete }) {
  const isNew = !job.id;
  const [name, setName] = useState(job.name);
  const [agentId, setAgentId] = useState(job.agentId || tenant.agents[0].id);
  const [expr, setExpr] = useState(job.expr);
  const [human, setHuman] = useState(job.human);
  const [enabled, setEnabled] = useState(job.enabled !== false);
  const valid = name.trim().length > 1 && /^[0-9*/,-\s]+$/.test(expr.trim());
  return (
    <Modal title={isNew ? 'New schedule' : 'Edit schedule'} onClose={onClose} odId="cron-editor-modal"
      footer={<>
        {!isNew && (
          <button type="button" onClick={() => onDelete(job)} data-od-id="btn-cron-delete"
            className="mr-auto flex h-9 items-center gap-1.5 rounded-md px-3 text-[13px] font-medium text-danger transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_12%,transparent)]">
            <Icon name="x" size={14}/> Delete
          </button>
        )}
        <button type="button" onClick={onClose} className="flex h-9 items-center rounded-md px-3.5 text-[13px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2">Cancel</button>
        <button type="button" disabled={!valid} data-od-id="btn-cron-save" onClick={() => onSave({ ...job, name: name.trim(), agentId, expr: expr.trim(), human: human.trim() || expr.trim(), enabled })}
          className="flex h-9 items-center rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40 disabled:hover:bg-accent">
          {isNew ? 'Create schedule' : 'Save changes'}
        </button>
      </>}>
      <div className="space-y-5 p-5">
        <div>
          <label className={labelCls} htmlFor="cr-name">Schedule name</label>
          <input id="cr-name" className={inputCls} value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Nightly ETL check" autoFocus/>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label className={labelCls} htmlFor="cr-agent">Agent</label>
            <select id="cr-agent" className={inputCls} value={agentId} onChange={(e) => setAgentId(e.target.value)}>
              {tenant.agents.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
            </select>
          </div>
          <div>
            <label className={labelCls} htmlFor="cr-tz">Timezone</label>
            <input id="cr-tz" className={cx(inputCls, 'font-mono text-[13px]')} value={tenant.tz} disabled title="Workspace timezone — change it in Settings → Workspace"/>
          </div>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label className={labelCls} htmlFor="cr-expr">Cron expression</label>
            <input id="cr-expr" className={cx(inputCls, 'font-mono text-[13px]')} value={expr} onChange={(e) => setExpr(e.target.value)} placeholder="0 9 * * 1-5"/>
            {!valid && expr.length > 0 && <p className="mt-1.5 text-[11px] text-danger">Required. Digits, spaces, * / , - only.</p>}
          </div>
          <div>
            <label className={labelCls} htmlFor="cr-human">Human label</label>
            <input id="cr-human" className={inputCls} value={human} onChange={(e) => setHuman(e.target.value)} placeholder="Weekdays · 9:00 AM"/>
          </div>
        </div>
        <div className="flex items-center justify-between rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_35%,var(--surface))] px-4 py-3">
          <div>
            <p className="text-[13px] font-medium text-fg">Enabled</p>
            <p className="text-[12px] text-muted">Disabled schedules keep their history and fire nothing.</p>
          </div>
          <Toggle on={enabled} onChange={setEnabled} label="Enable this schedule"/>
        </div>
      </div>
    </Modal>
  );
}

