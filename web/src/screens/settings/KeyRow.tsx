import { useState } from "react";
import { Icon } from "../../components/ui/Icon";

export function KeyRow({ k, onUpdate, onToast, onRevoke  }: any) {
  const [shown, setShown] = useState(false);
  const [revoking, setRevoking] = useState(false);
  const doRevoke = async () => {
    if (revoking) return;
    setRevoking(true);
    try {
      await onRevoke(k);
    } finally {
      setRevoking(false);
    }
  };
  return (
    <li className="flex items-center gap-3 py-3" data-od-id={'key-' + k.id}>
      <div className="min-w-0 flex-1">
        <p className="text-[14px] font-medium text-fg">{k.name}</p>
        <p className="font-mono text-[12px] text-muted">{(shown && k.full) ? k.full : k.masked} · created {k.created}</p>
      </div>
      {k.full && (
        <>
          <button type="button" onClick={() => setShown(!shown)} aria-label={shown ? 'Hide key' : 'Reveal key'}
            className="flex h-8 w-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2">
            <Icon name="eye" size={14}/>
          </button>
          <button type="button" aria-label="Copy key" onClick={() => {
            if (navigator.clipboard && navigator.clipboard.writeText) {
              navigator.clipboard.writeText(k.full).then(() => onToast('Key copied to clipboard'), () => onToast('Clipboard blocked by the browser', 'danger'));
            } else {
              onToast('Clipboard API not available', 'danger');
            }
          }}
            className="flex h-8 w-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2">
            <Icon name="copy" size={14}/>
          </button>
        </>
      )}
      <button type="button" disabled={revoking} onClick={doRevoke}
        className="h-8 rounded-md px-2.5 text-[12px] font-medium text-danger transition-colors hover:bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] disabled:opacity-50">
        {revoking ? 'Revoking…' : 'Revoke'}
      </button>
    </li>
  );
}
