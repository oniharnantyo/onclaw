import { useState } from "react";
import { cx, uid, slugify, fmtUses } from "../lib/helpers";
import { Icon } from "../components/ui/Icon";
import { Toggle } from "../components/ui/Toggle";
import { Chip } from "../components/ui/Chip";
import { inputCls } from "../components/ui/constants";

export function SkillsPane({ tenant, onUpdate, onToast }: { tenant: Workspace, onUpdate: (fn: (t: Workspace) => Workspace) => void, onToast: (msg: string, type?: string) => void }) {
  const [adding, setAdding] = useState(false);
  const [form, setForm] = useState({ name: '', desc: '' });
  const lib = tenant.skillLib || [];
  const usersOf = (id) => tenant.agents.filter((a: any) => (a.skills || []).includes(id));
  const install = () => {
    const name = form.name.trim();
    let slug = slugify(name) || uid('sk');
    while (lib.some((x: any) => x.id === slug)) slug += '-2';
    onUpdate((t: any) => ({ ...t, skillLib: [...(t.skillLib || []),
      { id: slug, name, version: '0.1.0', desc: form.desc.trim() || 'Custom workspace skill — no description yet.', uses: 0, enabled: true, source: 'workspace' }] }));
    onToast(name + ' installed — assign it from any agent\'s capabilities');
    setForm({ name: '', desc: '' });
    setAdding(false);
  };

  return (
    <div className="max-w-xl" data-od-id="pane-skills">
      <div className="mb-4 flex items-start justify-between gap-4">
        <p className="text-[13px] leading-5 text-muted">Skills are versioned capability packages agents load at run time. Disabling one suspends it for every agent that references it.</p>
        <button type="button" data-od-id="btn-skill-add" onClick={() => setAdding(!adding)}
          className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg">
          {adding ? 'Cancel' : 'Install skill'}
        </button>
      </div>

      {adding && (
        <div className="od-pop mb-4 space-y-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] p-3" data-od-id="skill-add-form">
          <div className="flex gap-2">
            <input className={inputCls} placeholder="Skill name — e.g. Changelog sweeper" value={form.name} aria-label="Skill name"
              onChange={(e) => setForm({ ...form, name: e.target.value })}/>
            <button type="button" data-od-id="btn-skill-add-confirm" disabled={!form.name.trim()} onClick={install}
              className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40 disabled:hover:border-line">
              Install
            </button>
          </div>
          <input className={cx(inputCls, 'text-[13px]')} placeholder="One-line description (optional)" value={form.desc} aria-label="Skill description"
            onChange={(e) => setForm({ ...form, desc: e.target.value })}/>
        </div>
      )}

      <ul className="divide-y divide-[var(--border-soft)]">
        {lib.map((s: any) => {
          const users = usersOf(s.id);
          return (
            <li key={s.id} className={cx('flex items-center gap-3 py-3', !s.enabled && 'opacity-70')} data-od-id={'skill-' + s.id}>
              <span className={cx('flex h-9 w-9 shrink-0 items-center justify-center rounded-md',
                s.enabled ? 'bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent' : 'bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted')}>
                <Icon name="spark" size={16}/>
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="text-[14px] font-medium text-fg">{s.name}</p>
                  <Chip mono>v{s.version}</Chip>
                  {s.source === 'workspace' && <Chip>custom</Chip>}
                </div>
                <p className="mt-0.5 text-[12px] leading-4 text-muted">{s.desc}</p>
              </div>
              <div className="shrink-0 text-right" title={users.map((u: any) => u.name).join(', ') || undefined}>
                <p className="font-mono text-[11px] text-fg2">{fmtUses(s.uses)} {s.uses === 1 ? 'run' : 'runs'}</p>
                <p className="font-mono text-[11px] text-muted">{users.length === 0 ? 'no agents' : users.length + (users.length === 1 ? ' agent' : ' agents')}</p>
              </div>
              <Toggle on={s.enabled} label={'Enable ' + s.name}
                onChange={(v: boolean) => {
                  onUpdate((t: any) => ({ ...t, skillLib: t.skillLib.map((x: Skill) => (x.id === s.id ? { ...x, enabled: v } : x)) }));
                  onToast(v ? s.name + ' enabled' : s.name + ' disabled — agents fall back to base behavior');
                }}/>
            </li>
          );
        })}
        {lib.length === 0 && <li className="py-6 text-center text-[13px] text-muted">No skills installed yet — install the first one above.</li>}
      </ul>
    </div>
  );
}

