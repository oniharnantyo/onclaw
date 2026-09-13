import { useState } from "react";
import { Icon } from "../components/ui/Icon";
import { Avatar } from "../components/ui/Avatar";
import { Chip } from "../components/ui/Chip";
import { useStore, useWorkspace } from "../store";

export interface AgentCardProps {
  a: Agent;
  onChat: (id: string) => void;
  onConfigure: (id: string) => void;
  onRegenerate?: (id: string) => void;
}

export function AgentCard({ a, onChat, onConfigure, onRegenerate }: AgentCardProps) {
  const currentWs = useWorkspace();
  const wsId = currentWs?.id || currentWs?.sub || useStore.getState().pos.tenantId;

  const [retrying, setRetrying] = useState(false);

  const handleRetry = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (retrying) return;
    setRetrying(true);
    try {
      if (onRegenerate) {
        await onRegenerate(a.id);
      } else if (wsId) {
        await useStore.getState().regenerateAgent(wsId, a.id);
      }
    } catch {
      // Failure surfaced via the roster's refreshed prompts_status.
    } finally {
      setRetrying(false);
    }
  };

  const isGenerating = a.prompts_status === 'generating';
  const isFailed = a.prompts_status === 'failed';

  // Nothing on the card repeats the name: a role or description identical to
  // it is already displayed, and empty renders as no line at all — no filler.
  const nameKey = a.name.trim().toLowerCase();
  const role = (a.role || '').trim();
  const roleLine = role && role.toLowerCase() !== nameKey ? role : '';
  const description = (a.description || '').trim();
  const tagline =
    description && description.toLowerCase() !== nameKey ? description : '';

  return (
    <article
      data-od-id={'agent-card-' + a.id}
      data-testid={'agent-card-' + a.id}
      className="flex flex-col rounded-lg border border-line bg-[color-mix(in_oklab,var(--bg)_35%,var(--surface))] p-4 transition-colors hover:border-[color-mix(in_oklab,var(--fg)_24%,transparent)]"
    >
      <div className="flex items-start gap-3">
        <Avatar name={a.name} avatar={a.avatar} kind="agent" size={36} />
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-[15px] font-semibold text-fg">{a.name}</h3>
          <div className="mt-0.5 flex min-w-0 items-center gap-1.5 font-mono text-[11px] text-muted">
            {roleLine && <span className="min-w-0 truncate">{roleLine}</span>}
            {roleLine && a.model && <span className="shrink-0">·</span>}
            {a.model && <span className="min-w-0 flex-1 truncate">{a.model}</span>}
          </div>
        </div>
      </div>

      {/* Autonomy chip — the model lives once, in the identity line above */}
      {a.autonomy && (
        <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
          <Chip>
            {a.autonomy === 'approval'
              ? 'with approval'
              : a.autonomy === 'suggest'
                ? 'suggest only'
                : 'autonomous'}
          </Chip>
        </div>
      )}

      {/* Prompt generation status banners */}
      {isGenerating && (
        <div
          data-testid="generating-indicator"
          className="mt-3 flex items-center gap-2 rounded-md border border-accent/25 bg-[color-mix(in_oklab,var(--accent)_10%,transparent)] px-2.5 py-1.5 text-[12px] font-medium text-accenttext"
        >
          <span className="relative flex h-2 w-2">
            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-accent opacity-75" />
            <span className="relative inline-flex rounded-full h-2 w-2 bg-accent" />
          </span>
          Generating prompts…
        </div>
      )}

      {isFailed && (
        <div
          data-testid="failed-indicator"
          className="mt-3 flex items-center justify-between gap-2 rounded-md border border-danger/30 bg-danger/10 p-2.5 text-[12px] text-danger"
        >
          <div className="min-w-0 flex-1">
            <p className="font-semibold flex items-center gap-1">
              <Icon name="alert" size={13} />
              Prompt generation failed
            </p>
            {a.prompts_error && <p className="mt-0.5 text-[11px] truncate" title={a.prompts_error}>{a.prompts_error}</p>}
          </div>
          <button
            type="button"
            onClick={handleRetry}
            disabled={retrying}
            data-testid="btn-retry-generation"
            className="shrink-0 flex items-center gap-1 rounded border border-danger/40 bg-surface px-2 py-1 text-[11px] font-semibold text-danger transition-colors hover:bg-danger hover:text-accenton disabled:opacity-50"
          >
            <Icon name="spark" size={11} />
            {retrying ? "Retrying…" : "Retry"}
          </button>
        </div>
      )}

      {tagline && <p className="mt-3 text-[13px] leading-5 text-fg2">{tagline}</p>}

      <div className="flex-1" />

      {a.status === 'error' && (
        <p className="mb-3 flex items-center gap-1.5 text-[12px] text-danger">
          <Icon name="alert" size={13} /> Needs attention — see latest run
        </p>
      )}

      <div className="mt-4 flex items-center gap-2">
        <button
          type="button"
          onClick={() => onChat(a.id)}
          data-od-id={'agent-chat-' + a.id}
          data-testid={'agent-chat-' + a.id}
          className="flex h-8 flex-1 items-center justify-center gap-1.5 rounded-md border border-line text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
        >
          <Icon name="chat" size={14} /> Open chat
        </button>
        <button
          type="button"
          onClick={() => onConfigure(a.id)}
          data-od-id={'agent-configure-' + a.id}
          data-testid={'agent-configure-' + a.id}
          className="flex h-8 items-center justify-center gap-1.5 rounded-md px-3 text-[13px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
        >
          <Icon name="sliders" size={14} /> Configure
        </button>
      </div>
    </article>
  );
}
