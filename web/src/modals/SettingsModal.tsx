import { useState } from "react";
import { cx, uid } from "../lib/helpers";
import { Icon } from "../components/ui/Icon";
import { Modal } from "../components/ui/Modal";
import { Toggle } from "../components/ui/Toggle";
import { Avatar } from "../components/ui/Avatar";
import { Chip } from "../components/ui/Chip";
import { inputCls, labelCls } from "../components/ui/constants";
import { MODELS } from "../lib/constants";

import { McpPane } from "./McpPane";
import { SkillsPane } from "./SkillsPane";
import { KeyRow } from "./KeyRow";
const SETTINGS_TABS = [
  { id: 'workspace', label: 'Workspace', icon: 'shield' },
  { id: 'members', label: 'Members & roles', icon: 'users' },
  { id: 'integrations', label: 'Integrations', icon: 'link' },
  { id: 'mcp', label: 'MCP servers', icon: 'plug' },
  { id: 'skills', label: 'Skills', icon: 'spark' },
  { id: 'keys', label: 'API keys', icon: 'key' },
  { id: 'notifications', label: 'Notifications', icon: 'bell' }
];

export function SettingsModal({ tenant, tab, onTab, onClose, onUpdate, onToast, onDeleteWorkspace  }: any) {
  const [ws, setWs] = useState({ name: tenant.name, tz: tenant.tz, defaultModel: tenant.defaultModel, retention: tenant.retention });
  const [invite, setInvite] = useState({ email: '', role: 'Member' });
  const [notif, setNotif] = useState(tenant.notifications || { cronFail: true, agentErrors: true, digest: false, email: 'ops@' + tenant.sub + '.dev' });
  const [confirmDel, setConfirmDel] = useState(false);

  const saveWorkspace = () => {
    onUpdate((t: any) => ({ ...t, ...ws }));
    onToast('Workspace settings saved');
  };


  return (
    <Modal title={tenant.name + ' settings'} onClose={onClose} wide odId="settings-modal">
      <div className="flex flex-col md:flex-row min-h-[480px]">
        <div className="od-scroll w-full md:w-56 shrink-0 flex md:flex-col overflow-x-auto md:overflow-x-hidden md:overflow-y-auto border-b md:border-b-0 md:border-r border-linesoft py-3" role="tablist" aria-label="Settings sections">
          {SETTINGS_TABS.map((t: any) => (
            <button key={t.id} type="button" role="tab" aria-selected={tab === t.id} onClick={() => onTab(t.id)} data-od-id={'settings-tab-' + t.id}
              className={cx('mx-2 flex h-9 shrink-0 md:shrink items-center gap-2.5 rounded-md px-3 text-left text-[13px] transition-colors',
                tab === t.id ? 'bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] font-medium text-fg' : 'text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg')}>
              <Icon name={t.icon} size={15} className={tab === t.id ? 'text-accent' : 'text-muted'}/>{t.label}
            </button>
          ))}
        </div>

        <div className="od-scroll flex-1 overflow-y-auto p-4 md:p-6">
          {tab === 'workspace' && (
            <div className="max-w-md space-y-5" data-od-id="pane-workspace">
              <div>
                <label className={labelCls} htmlFor="ws-name">Workspace name</label>
                <input id="ws-name" className={inputCls} value={ws.name} onChange={(e) => setWs({ ...ws, name: e.target.value })}/>
              </div>
              <div>
                <label className={labelCls} htmlFor="ws-sub">Workspace URL</label>
                <div className="flex items-center gap-2">
                  <input id="ws-sub" disabled className={cx(inputCls, 'font-mono text-[13px]')} value={tenant.sub + '.onclaw.app'}/>
                  <Chip>{tenant.plan} plan</Chip>
                </div>
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label className={labelCls} htmlFor="ws-tz">Timezone</label>
                  <select id="ws-tz" className={inputCls} value={ws.tz} onChange={(e) => setWs({ ...ws, tz: e.target.value })}>
                    {['America/Los_Angeles', 'America/New_York', 'Europe/London', 'Europe/Berlin', 'Asia/Singapore'].map((z) => <option key={z}>{z}</option>)}
                  </select>
                </div>
                <div>
                  <label className={labelCls} htmlFor="ws-model">Default model</label>
                  <select id="ws-model" className={inputCls} value={ws.defaultModel} onChange={(e) => setWs({ ...ws, defaultModel: e.target.value })}>
                    {MODELS.map((m: any) => <option key={m}>{m}</option>)}
                  </select>
                </div>
              </div>
              <div>
                <label className={labelCls} htmlFor="ws-ret">Thread retention</label>
                <select id="ws-ret" className={inputCls} value={ws.retention} onChange={(e) => setWs({ ...ws, retention: e.target.value })}>
                  {['30 days', '90 days', '1 year', 'Forever'].map((r: any) => <option key={r}>{r}</option>)}
                </select>
              </div>
              <div className="flex justify-end pt-1">
                <button type="button" onClick={saveWorkspace} data-od-id="btn-workspace-save"
                  className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]">
                  Save workspace
                </button>
              </div>
              <div className="mt-8 rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] p-4">
                <p className="text-[13px] font-semibold text-fg">Danger zone</p>
                <p className="mt-1 text-[12px] leading-5 text-muted">Deleting a workspace removes its agents, schedules, threads and keys. This cannot be undone.</p>
                <button type="button" data-od-id="btn-workspace-delete"
                  onClick={() => { if (confirmDel) onDeleteWorkspace(); else { setConfirmDel(true); setTimeout(() => setConfirmDel(false), 4000); } }}
                  className="mt-3 flex h-8 items-center rounded-md border border-[color-mix(in_oklab,var(--danger)_45%,transparent)] px-3 text-[12px] font-medium text-danger transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)]">
                  {confirmDel ? 'Click again to confirm' : 'Delete workspace'}
                </button>
              </div>
            </div>
          )}

          {tab === 'members' && (
            <div className="max-w-xl" data-od-id="pane-members">
              <div className="mb-5 flex gap-2">
                <input className={inputCls} placeholder={'teammate@' + tenant.sub + '.dev'} value={invite.email} aria-label="Invite email"
                  onChange={(e) => setInvite({ ...invite, email: e.target.value })}/>
                <select className={cx(inputCls, 'w-32')} value={invite.role} onChange={(e) => setInvite({ ...invite, role: e.target.value })} aria-label="Role for invite">
                  <option>Admin</option><option>Member</option>
                </select>
                <button type="button" data-od-id="btn-invite" disabled={!invite.email.includes('@')}
                  onClick={() => {
                    onUpdate((t: any) => ({ ...t, members: [...t.members, { id: uid('mb'), name: invite.email.split('@')[0], email: invite.email, role: invite.role }] }));
                    onToast('Invite sent to ' + invite.email);
                    setInvite({ email: '', role: 'Member' });
                  }}
                  className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40 disabled:hover:border-line">
                  Invite
                </button>
              </div>
              <ul className="divide-y divide-[var(--border-soft)]">
                {tenant.members.map((m: any) => (
                  <li key={m.id} className="flex items-center gap-3 py-3" data-od-id={'member-' + m.id}>
                    <Avatar name={m.name} kind={m.id === 'me' ? 'you' : 'other'}/>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-[14px] font-medium text-fg">{m.name}</p>
                      <p className="truncate font-mono text-[11px] text-muted">{m.email}</p>
                    </div>
                    {m.role === 'Owner' ? <Chip>You are the owner</Chip> : (
                      <div className="flex items-center gap-2">
                        <select className="h-8 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_55%,var(--surface))] px-2 text-[12px] text-fg2 focus:border-accent" value={m.role}
                          onChange={(e) => { const role = e.target.value; onUpdate((t: any) => ({ ...t, members: t.members.map((x: any) => (x.id === m.id ? { ...x, role } : x)) })); onToast(m.name + ' is now ' + role.toLowerCase()); }}
                          aria-label={'Role for ' + m.name}>
                          <option>Admin</option><option>Member</option>
                        </select>
                        <button type="button" aria-label={'Remove ' + m.name} onClick={() => { onUpdate((t: any) => ({ ...t, members: t.members.filter((x: any) => x.id !== m.id) })); onToast(m.name + ' removed from ' + tenant.name); }}
                          className="flex h-7 w-7 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] hover:text-danger">
                          <Icon name="x" size={13}/>
                        </button>
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {tab === 'integrations' && (
            <div className="max-w-lg space-y-3" data-od-id="pane-integrations">
              <p className="mb-4 text-[13px] leading-5 text-muted">Connections are shared across every agent in {tenant.name}. Credentials never appear in threads.</p>
              {tenant.integrations.map((it: any) => (
                <div key={it.id} className="flex items-center gap-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3" data-od-id={'integration-' + it.id}>
                  <span className={cx('flex h-9 w-9 items-center justify-center rounded-md',
                    it.connected ? 'bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent' : 'bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted')}>
                    <Icon name={it.id === 'slack' ? 'chat' : it.id === 'github' ? 'terminal' : it.id === 'pagerduty' ? 'bell' : it.id === 'postgres' ? 'db' : it.id === 'linear' ? 'zap' : 'file'} size={16}/>
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="text-[14px] font-medium text-fg">{it.name}</p>
                    <p className={cx('truncate font-mono text-[11px]', it.id === 'pagerduty' && it.connected ? 'text-[color-mix(in_oklab,var(--warn),black_38%)]' : 'text-muted')}>{it.detail}</p>
                  </div>
                  {it.connected ? (
                    <div className="flex items-center gap-2.5">
                      <span className="flex items-center gap-1.5 text-[12px] text-[color-mix(in_oklab,var(--success),black_25%)]"><Icon name="check" size={13}/> Connected</span>
                      <button type="button" onClick={() => { onUpdate((t: any) => ({ ...t, integrations: t.integrations.map((x: Integration) => (x.id === it.id ? { ...x, connected: false } : x)) })); onToast(it.name + ' disconnected'); }}
                        className="h-8 rounded-md px-2.5 text-[12px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2">
                        Disconnect
                      </button>
                    </div>
                  ) : (
                    <button type="button" data-od-id={'connect-' + it.id} onClick={() => { onUpdate((t: any) => ({ ...t, integrations: t.integrations.map((x: Integration) => (x.id === it.id ? { ...x, connected: true, detail: x.detail + ' · connected just now' } : x)) })); onToast(it.name + ' connected'); }}
                      className="h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg">
                      Connect
                    </button>
                  )}
                </div>
              ))}
            </div>
          )}

          {tab === 'mcp' && <McpPane tenant={tenant} onUpdate={onUpdate} onToast={onToast}/>}

          {tab === 'skills' && <SkillsPane tenant={tenant} onUpdate={onUpdate} onToast={onToast}/>}

          {tab === 'keys' && (
            <div className="max-w-xl" data-od-id="pane-keys">
              <div className="mb-4 flex items-center justify-between gap-4">
                <p className="text-[13px] leading-5 text-muted">Keys authenticate API calls that trigger agents. Scope them per service and rotate often.</p>
                <button type="button" data-od-id="btn-new-key"
                  onClick={() => {
                    const suffix = Math.random().toString(36).slice(2, 6);
                    onUpdate((t: any) => ({ ...t, keys: [...t.keys, { id: uid('k'), name: 'key-' + suffix, masked: 'oc_live_••••••••' + suffix, full: 'oc_live_' + Math.random().toString(36).slice(2, 10) + suffix, created: 'Aug 2026' }] }));
                    onToast('API key created — copy it now, it won\'t be shown again');
                  }}
                  className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg">
                  Create key
                </button>
              </div>
              <ul className="divide-y divide-[var(--border-soft)]">
                {tenant.keys.map((k: any) => <KeyRow key={k.id} k={k} onUpdate={onUpdate} onToast={onToast}/>)}
                {tenant.keys.length === 0 && <li className="py-6 text-center text-[13px] text-muted">No keys yet — create the first one above.</li>}
              </ul>
            </div>
          )}

          {tab === 'notifications' && (
            <div className="max-w-md space-y-1" data-od-id="pane-notifications">
              {[
                { id: 'cronFail', label: 'Cron failures', desc: 'A scheduled run fails or misses its window' },
                { id: 'agentErrors', label: 'Agent errors', desc: 'Tool failures, auth expiry, budget thresholds' },
                { id: 'digest', label: 'Weekly digest', desc: 'Monday summary of runs, spend and failures' }
              ].map((row) => (
                <div key={row.id} className="flex items-center justify-between gap-4 rounded-md px-1 py-3">
                  <div>
                    <p className="text-[14px] font-medium text-fg">{row.label}</p>
                    <p className="text-[12px] text-muted">{row.desc}</p>
                  </div>
                  <Toggle on={notif[row.id]} onChange={(v) => setNotif({ ...notif, [row.id]: v })} label={row.label}/>
                </div>
              ))}
              <div className="pt-2">
                <label className={labelCls} htmlFor="nf-email">Route email</label>
                <input id="nf-email" className={cx(inputCls, 'font-mono text-[13px]')} value={notif.email} onChange={(e) => setNotif({ ...notif, email: e.target.value })}/>
              </div>
              <div className="flex justify-end pt-3">
                <button type="button" data-od-id="btn-notif-save"
                  onClick={() => { onUpdate((t: any) => ({ ...t, notifications: notif })); onToast('Notification settings saved'); }}
                  className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]">
                  Save preferences
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
}

