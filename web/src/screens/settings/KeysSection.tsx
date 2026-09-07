import { useEffect, useState } from "react";
import { KeyRow } from "./KeyRow";
import { ApiKeyDialog } from "../../modals/ApiKeyDialog";
import { Icon } from "../../components/ui/Icon";
import { apiKeys, type ApiWorkspaceKey } from "../../lib/api";

export interface KeysSectionProps {
  tenant: any;
  onToast: (text: string, kind?: string) => void;
  onUpdate: (fn: any) => void;
}

export function KeysSection({ tenant, onToast, onUpdate }: KeysSectionProps) {
  const [createOpen, setCreateOpen] = useState(false);
  // Live keys come from the native backend; when it is unreachable (mock mode)
  // the section falls back to the local tenant.keys list.
  const [liveKeys, setLiveKeys] = useState<ApiWorkspaceKey[] | null>(null);
  // Plaintext of the just-created key — the backend returns it exactly once.
  const [recent, setRecent] = useState<{ id: string; plaintext: string } | null>(null);

  useEffect(() => {
    let cancelled = false;
    apiKeys.list(tenant.id)
      .then((r) => { if (!cancelled) setLiveKeys(r.api_keys.filter((k) => !k.revoked_at)); })
      .catch(() => { if (!cancelled) setLiveKeys(null); });
    return () => { cancelled = true; };
  }, [tenant.id]);

  const live = liveKeys !== null;
  const keys: any[] = live
    ? liveKeys!.map((k) => ({
        id: k.id,
        name: k.name,
        masked: `${k.key_prefix}••••••••${k.key_suffix}`,
        full: recent && recent.id === k.id ? recent.plaintext : undefined,
        created: k.created_at ? new Date(k.created_at).toLocaleDateString(undefined, { month: 'short', year: 'numeric' }) : '',
      }))
    : (tenant.keys || []);

  const revoke = async (k: any) => {
    if (!live) {
      onUpdate((t: any) => ({ ...t, keys: t.keys.filter((x: any) => x.id !== k.id) }));
      onToast(k.name + ' revoked', 'danger');
      return;
    }
    try {
      await apiKeys.revoke(tenant.id, k.id);
      setLiveKeys((ks) => (ks || []).filter((x) => x.id !== k.id));
      if (recent && recent.id === k.id) setRecent(null);
      onToast(k.name + ' revoked', 'danger');
    } catch (err: any) {
      onToast(err?.message || 'Failed to revoke key', 'danger');
    }
  };

  const createKey = live
    ? async (name: string) => {
        const r = await apiKeys.create(tenant.id, name);
        setLiveKeys((ks) => [r.api_key, ...(ks || [])]);
        setRecent({ id: r.api_key.id, plaintext: r.key });
        return { id: r.api_key.id, name: r.api_key.name, plaintext: r.key };
      }
    : undefined;

  return (
    <div className="max-w-xl" data-od-id="pane-keys" data-testid="pane-keys">
      {(keys.length > 0 || live) && (
        <div className="mb-4 flex justify-end">
          <button
            type="button"
            data-od-id="btn-new-key"
            data-testid="btn-new-key"
            onClick={() => setCreateOpen(true)}
            className="h-9 shrink-0 rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
          >
            Create key
          </button>
        </div>
      )}
      <ul className="divide-y divide-[var(--border-soft)]">
        {keys.map((k: any) => (
          <KeyRow key={k.id} k={k} onUpdate={onUpdate} onToast={onToast} onRevoke={revoke} />
        ))}
        {keys.length === 0 && (
          <li className="py-8 text-center" data-od-id="keys-empty" data-testid="keys-empty">
            <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
              <Icon name="key" size={20} />
            </div>
            <p className="text-[14px] font-medium text-fg">No API keys</p>
            <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
              Create an API key to authenticate requests.
            </p>
            <button
              type="button"
              data-od-id="btn-key-empty-add"
              data-testid="btn-key-empty-add"
              onClick={() => setCreateOpen(true)}
              className="mt-3 inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
            >
              <Icon name="plus" size={13} /> Create your first key
            </button>
          </li>
        )}
      </ul>

      {createOpen && (
        <ApiKeyDialog
          onClose={() => setCreateOpen(false)}
          onSave={(newKey) => {
            onUpdate((t: any) => ({
              ...t,
              keys: [...(t.keys || []), newKey],
            }));
          }}
          createKey={createKey}
          onToast={onToast}
        />
      )}
    </div>
  );
}
