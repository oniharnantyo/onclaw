import { useState } from "react";
import { KeyRow } from "./KeyRow";
import { ApiKeyDialog } from "../../modals/ApiKeyDialog";
import { Icon } from "../../components/ui/Icon";

export interface KeysSectionProps {
  tenant: any;
  onToast: (text: string, kind?: string) => void;
  onUpdate: (fn: any) => void;
}

export function KeysSection({ tenant, onToast, onUpdate }: KeysSectionProps) {
  const [createOpen, setCreateOpen] = useState(false);

  return (
    <div className="max-w-xl" data-od-id="pane-keys" data-testid="pane-keys">
      {(tenant.keys && tenant.keys.length > 0) && (
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
        {(tenant.keys || []).map((k: any) => (
          <KeyRow key={k.id} k={k} onUpdate={onUpdate} onToast={onToast} />
        ))}
        {(!tenant.keys || tenant.keys.length === 0) && (
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
          onToast={onToast}
        />
      )}
    </div>
  );
}
