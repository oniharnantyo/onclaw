// Blocked-prompt notice (integrate-agent-hooks D6): a `prompt_blocked`
// transcript entry renders where the assistant reply would have been — the
// model never ran, so there is no bubble, spinner, or empty turn. Hydrated
// history and the live/catch-up stream both land here (the translator mints
// the same `author: 'notice'` message shape for either source).
import { Icon } from "../ui/Icon";

export function PromptBlockedNotice({ m }: any) {
  const hook = m?.notice?.hook || '';
  const reason = m?.notice?.reason || '';
  return (
    <div className="flex px-2" data-od-id={'msg-' + m.id} data-role="notice">
      <div className="min-w-0 flex-1">
        <div
          role="status"
          data-testid="prompt-blocked-notice"
          className="flex items-start gap-2 rounded-md border border-[color-mix(in_oklab,var(--warn)_38%,transparent)] bg-[color-mix(in_oklab,var(--warn)_8%,transparent)] px-3 py-2"
        >
          <Icon name="shield" size={14} className="mt-0.5 shrink-0 text-[color-mix(in_oklab,var(--warn),black_25%)]"/>
          <p className="min-w-0 flex-1 break-words text-[13px] leading-5 text-fg2">
            Blocked by hook{hook ? ' ' : ''}
            {hook && <span className="rounded-[4px] bg-[color-mix(in_oklab,var(--fg)_8%,transparent)] px-1 py-0.5 font-mono text-[12px] text-danger">{hook}</span>}
            {reason ? `: ${reason}` : ''}
          </p>
          {m.ts && <span className="ml-auto shrink-0 pt-0.5 font-mono text-[10px] text-muted">{m.ts}</span>}
        </div>
      </div>
    </div>
  );
}
