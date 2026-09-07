// In-chat error entry (design D8): a thread message with author 'error'
// marks a failed turn where the retracted optimistic row used to be. Compact
// danger styling, no actor avatar — the error has no author.
import { Icon } from "../ui/Icon";

// In-chat error entry (design D8): a thread message with author 'error'
// marks a failed turn where the retracted optimistic row used to be. Compact
// danger styling, no actor avatar — the error has no author.
export function ErrorEntry({ m }: any) {
  return (
    <div className="flex px-2" data-od-id={'msg-' + m.id} data-role="error">
      <div className="min-w-0 flex-1">
        <div role="alert" className="flex items-start gap-2 rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_7%,transparent)] px-3 py-2">
          <Icon name="alert" size={14} className="mt-0.5 shrink-0 text-danger"/>
          <div className="min-w-0 flex-1">
            <p className="text-[12px] font-medium text-danger">Run failed</p>
            <p className="break-words text-[13px] leading-5 text-fg2">{m.error}</p>
          </div>
          {m.ts && <span className="ml-auto shrink-0 pt-0.5 font-mono text-[10px] text-muted">{m.ts}</span>}
        </div>
      </div>
    </div>
  );
}
