import { useState } from "react";
import { cx, uid, slugify, fmtUses, providerOf } from "../lib/helpers";
import { Icon } from "../components/ui/Icon";
import { Toggle } from "../components/ui/Toggle";
import { Chip } from "../components/ui/Chip";
import { inputCls, labelCls } from "../components/ui/constants";
import { PROVIDERS, MODELS, TOOLS, SKILLS, MCP_SERVERS } from "../lib/constants";

const MCP_STATUS = {
  connected: { dot: 'bg-success', label: 'Connected' },
  disabled: { dot: 'bg-muted', label: 'Paused' },
  error: { dot: 'bg-danger', label: 'Error' }
};

export function McpPane({ tenant, onUpdate, onToast  }: any) {
  const [open, setOpen] = useState([]);
  const [adding, setAdding] = useState(false);
  const [form, setForm] = useState({ name: '', transport: '' });
  const servers = tenant.mcpServers || [];
  const usersOf = (id) => tenant.agents.filter((a: any) => (a.mcp || []).includes(id));
  const expanded = (id) => open.includes(id);
  const setStatus = (id, status) => onUpdate((t: any) => ({ ...t, mcpServers: t.mcpServers.map((s: any) => (s.id === id ? { ...s, status } : s)) }));
  const addServer = () => {
    const name = form.name.trim();
    let slug = slugify(name) || uid('mcp');
    while (servers.some((x: any) => x.id === slug)) slug += '-2';
    onUpdate((t: any) => ({ ...t, mcpServers: [...(t.mcpServers || []),
      { id: slug, name, transport: form.transport.trim(), auth: 'No credentials yet — configured on first launch', tools: 0, status: 'connected', sample: [] }] }));
    onToast(name + ' added — tools sync on the first handshake');
    setForm({ name: '', transport: '' });
    setAdding(false);
  };

  return (
    <div className="max-w-xl" data-od-id="pane-mcp">
      <div className="mb-4 flex items-start justify-between gap-4">
        <p className="text-[13px] leading-5 text-muted">Model Context Protocol servers expose tools to every agent that references them. Pausing a server revokes access on the next run.</p>
        <button type="button" data-od-id="btn-mcp-add" onClick={() => setAdding(!adding)}
          className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg">
          {adding ? 'Cancel' : 'Add server'}
        </button>
      </div>

      {adding && (
        <div className="od-pop mb-4 flex gap-2 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] p-3" data-od-id="mcp-add-form">
          <input className={inputCls} placeholder="Server name — e.g. Sentry" value={form.name} aria-label="Server name"
            onChange={(e) => setForm({ ...form, name: e.target.value })}/>
          <input className={cx(inputCls, 'font-mono text-[13px]')} placeholder="stdio · sentry-mcp serve" value={form.transport} aria-label="Transport command"
            onChange={(e) => setForm({ ...form, transport: e.target.value })}/>
          <button type="button" data-od-id="btn-mcp-add-confirm" disabled={!(form.name.trim() && form.transport.trim())} onClick={addServer}
            className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40 disabled:hover:border-line">
            Add
          </button>
        </div>
      )}

      <div className="space-y-2.5">
        {servers.map((s: any) => {
          const st = (MCP_STATUS as any)[s.status] || MCP_STATUS.connected;
          const users = usersOf(s.id);
          const shown = s.sample || [];
          const inactive = s.status !== 'connected' && users.length > 0;
          return (
            <div key={s.id} className={cx('rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))]', s.status === 'disabled' && 'opacity-70')}
              data-od-id={'mcp-' + s.id}>
              <div className="flex items-center gap-3 px-4 py-3">
                <span className={cx('flex h-9 w-9 shrink-0 items-center justify-center rounded-md',
                  s.status === 'connected' ? 'bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent'
                    : s.status === 'error' ? 'bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] text-danger'
                    : 'bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted')}>
                  <Icon name="plug" size={16}/>
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <p className="text-[14px] font-medium text-fg">{s.name}</p>
                    <span className={cx('inline-flex items-center gap-1.5 text-[11px]',
                      s.status === 'connected' ? 'text-[color-mix(in_oklab,var(--success),black_25%)]' : s.status === 'error' ? 'text-danger' : 'text-muted')}>
                      <span className={cx('h-1.5 w-1.5 rounded-full', st.dot)}/>{st.label}
                    </span>
                  </div>
                  <p className="truncate font-mono text-[11px] text-muted">{s.transport}</p>
                  {s.status === 'error' && <p className="truncate text-[11px] text-danger">{s.error}</p>}
                </div>
                <div className="shrink-0 text-right" title={users.map((u: any) => u.name).join(', ') || undefined}>
                  <p className="font-mono text-[11px] text-fg2">{s.tools} {s.tools === 1 ? 'tool' : 'tools'}</p>
                  <p className={cx('font-mono text-[11px]', inactive ? 'text-[color-mix(in_oklab,var(--warn),black_38%)]' : 'text-muted')}>
                    {users.length === 0 ? 'no agents' : users.length + (users.length === 1 ? ' agent' : ' agents') + (inactive ? ' · inactive' : '')}
                  </p>
                </div>
                {s.status === 'error' ? (
                  <button type="button" data-od-id={'mcp-retry-' + s.id}
                    onClick={() => { setStatus(s.id, 'connected'); onToast(s.name + ' reconnected — ' + s.tools + ' tools available'); }}
                    className="h-8 shrink-0 rounded-md border border-[color-mix(in_oklab,var(--danger)_45%,transparent)] px-2.5 text-[12px] font-medium text-danger transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_10%,transparent)]">
                    Retry
                  </button>
                ) : (
                  <Toggle on={s.status === 'connected'} label={'Enable ' + s.name}
                    onChange={(v) => {
                      setStatus(s.id, v ? 'connected' : 'disabled');
                      onToast(v ? s.name + ' connected — ' + s.tools + ' tools exposed' : s.name + ' paused — agents lose access on the next run');
                    }}/>
                )}
                <button type="button" onClick={() => setOpen((o) => (o.includes(s.id) ? o.filter((x: any) => x !== s.id) : [...o, s.id]))}
                  aria-expanded={expanded(s.id)} aria-label={'Tools exposed by ' + s.name}
                  className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2">
                  <Icon name="chevright" size={13} className={cx('transition-transform', expanded(s.id) && 'rotate-90')}/>
                </button>
              </div>
              {expanded(s.id) && (
                <div className="border-t border-[var(--border-soft)] px-4 py-3">
                  <p className="mb-2 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-muted">Tools exposed</p>
                  {shown.length ? (
                    <div className="flex flex-wrap gap-1.5">
                      {shown.map((t: any) => <Chip key={t} mono>{t}</Chip>)}
                      {s.tools > shown.length && <span className="self-center font-mono text-[11px] text-muted">+{s.tools - shown.length} more</span>}
                    </div>
                  ) : (
                    <p className="text-[12px] text-muted">No tools yet — the manifest syncs on the first handshake.</p>
                  )}
                  <p className="mt-2.5 font-mono text-[11px] text-muted">{s.auth}</p>
                </div>
              )}
            </div>
          );
        })}
        {servers.length === 0 && <p className="py-6 text-center text-[13px] text-muted">No MCP servers yet — add the first one above.</p>}
      </div>
    </div>
  );
}

