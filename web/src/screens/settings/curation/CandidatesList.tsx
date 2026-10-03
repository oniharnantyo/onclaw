// Candidates sub-tab (add-skill-curation-from-traces 9.2): pending and
// provisional work as review cards. Each card names the proposed skill,
// flags create-vs-edit (an edit names the skill it supersedes), shows the
// lifecycle status, a muted provenance line (owning agent, cluster,
// evidence volume, cited patterns) and — mid-probation — the outcome tally.
//
// Extraction-failed cards (spec: "Cards whose extraction failed SHALL show
// an error chip with a retry action"): the proposer records the dropped
// draft as a failed row; its card carries the error chip, the verbatim
// failure message, and a Retry action that re-runs extraction for the
// candidate's cluster. When the list marks a card source_available=false,
// a muted "source no longer available" note renders — the deep-links may be
// stale, approval stays available (the human gate never checks sessions).
// Tool-call and recovery counts are not exposed, so the provenance line
// shows the evidence volume it has.
import { useEffect, useState } from "react";
import { cx } from "../../../lib/helpers";
import { Icon } from "../../../components/ui/Icon";
import { Chip } from "../../../components/ui/Chip";
import { api, type ApiSkillCandidate } from "../../../lib/api";

/** Whole days since the candidate entered probation (decided_at), falling
 * back to its last update. 0 while pending — only provisional rows show it. */
export function probationDays(c: ApiSkillCandidate, now = new Date()): number {
  const stamp = c.decided_at || c.updated_at;
  if (!stamp) return 0;
  const start = new Date(stamp).getTime();
  if (Number.isNaN(start)) return 0;
  return Math.max(0, Math.floor((now.getTime() - start) / 86400000));
}

export interface CandidatesListProps {
  wsSlug: string;
  onOpen: (candidate: ApiSkillCandidate) => void;
  /** Bumped by the parent after an approve/reject so tallies re-read. */
  refreshKey?: number;
}

export function CandidatesList({ wsSlug, onOpen, refreshKey = 0 }: CandidatesListProps) {
  const [candidates, setCandidates] = useState<ApiSkillCandidate[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  // The card whose retry is in flight (the button shows its own progress).
  const [retryingId, setRetryingId] = useState<string | null>(null);

  const load = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const res = await api.curation.listCandidates(wsSlug);
      // The review view: pending and failed (needs a decision or an action)
      // first, then the rest.
      const order: Record<string, number> = { pending: 0, failed: 1, provisional: 2, approved: 3, rejected: 4, disabled: 5 };
      setCandidates((res.candidates || []).slice().sort((a, b) => (order[a.status] ?? 9) - (order[b.status] ?? 9)));
    } catch (err: unknown) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  // Retry re-runs extraction for one failed candidate's cluster; either way
  // the queue reloads — success renders the fresh pending card, failure the
  // refreshed error message.
  const retry = async (candidate: ApiSkillCandidate) => {
    setRetryingId(candidate.id);
    try {
      await api.curation.retryCandidate(wsSlug, candidate.id);
    } catch {
      // The row's error message was refreshed server-side; the reload
      // below renders it on the card.
    } finally {
      await load();
      setRetryingId(null);
    }
  };

  useEffect(() => {
    if (wsSlug) void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload per workspace/refresh only
  }, [wsSlug, refreshKey]);

  if (loading && candidates === null) {
    return (
      <div className="space-y-3" data-testid="candidates-loading">
        {[0, 1, 2].map((i) => (
          <div key={i} className="h-16 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]" />
        ))}
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="rounded-md border border-line px-4 py-6 text-center" data-testid="candidates-error">
        <p className="text-[13px] font-medium text-fg">Couldn&apos;t load candidates</p>
        <p className="mt-1 text-[12px] text-muted">{loadError}</p>
        <button
          type="button"
          onClick={() => void load()}
          data-testid="btn-candidates-retry"
          className="mt-3 h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
        >
          Retry
        </button>
      </div>
    );
  }

  if ((candidates || []).length === 0) {
    return (
      <div className="rounded-md border border-line px-4 py-8 text-center" data-testid="candidates-empty">
        <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
          <Icon name="spark" size={20} />
        </div>
        <p className="text-[14px] font-medium text-fg">No candidates in review</p>
        <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
          Candidates appear when agents solve similar problems repeatedly — the curation loop clusters those runs
          and drafts a skill proposal for review.
        </p>
      </div>
    );
  }

  return (
    <ul className="space-y-3" data-testid="candidates-list">
      {(candidates || []).map((c) => {
        const pending = c.status === 'pending';
        const provisional = c.status === 'provisional';
        const failed = c.status === 'failed';
        return (
          <li key={c.id}>
            <div
              className={cx(
                'rounded-md border transition-colors',
                failed
                  ? 'border-[color-mix(in_oklab,var(--danger)_35%,transparent)]'
                  : 'border-line hover:border-accent'
              )}
            >
              <button
                type="button"
                onClick={() => onOpen(c)}
                data-testid={'candidate-' + c.skill_name}
                className="w-full px-4 py-3 text-left"
              >
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-mono text-[13px] font-medium text-fg">
                    {c.skill_name || 'extraction failed'}
                  </span>
                  {!failed && c.is_edit ? (
                    <Chip className="border-[color-mix(in_oklab,var(--warn)_45%,transparent)] text-[color-mix(in_oklab,var(--warn),black_38%)]">
                      edit · supersedes <span className="font-mono">{c.supersedes_skill_name}</span>
                    </Chip>
                  ) : null}
                  {!failed && !c.is_edit ? <Chip>new skill</Chip> : null}
                  {pending ? <Chip>pending</Chip> : null}
                  {provisional ? (
                    <span data-testid={'candidate-probation-' + c.skill_name}>
                      <Chip className="border-[color-mix(in_oklab,var(--accent)_45%,transparent)] text-[color-mix(in_oklab,var(--accent),black_25%)]">
                        provisional · day {probationDays(c)}
                      </Chip>
                    </span>
                  ) : null}
                  {c.status === 'rejected' ? <Chip>rejected</Chip> : null}
                  {c.status === 'disabled' ? <Chip>disabled</Chip> : null}
                  {c.status === 'approved' ? <Chip>approved</Chip> : null}
                  {provisional ? (
                    <span
                      className="ml-auto text-[11px] text-muted"
                      data-testid={'candidate-tally-' + c.skill_name}
                      title="Probation outcome tally: helpful · harmful · mounted runs"
                    >
                      <span className="text-[color-mix(in_oklab,var(--success),black_25%)]">+{c.helpful_count} helpful</span>
                      {' · '}
                      <span className="text-danger">−{c.harmful_count} harmful</span>
                      {' · '}
                      {c.use_count} runs
                    </span>
                  ) : null}
                </div>
                <div className={cx('mt-1.5 flex flex-wrap items-center gap-1.5 text-[11px] text-muted')}>
                  <Chip mono>agent · {c.agent_id}</Chip>
                  <Chip mono>cluster · {c.cluster_id}</Chip>
                  <span>
                    {c.evidence_event_ids.length} evidence {c.evidence_event_ids.length === 1 ? 'event' : 'events'}
                  </span>
                  {(c.cited_pattern_refs || []).map((ref) => (
                    <Chip key={ref} mono>
                      pattern · {ref}
                    </Chip>
                  ))}
                  {c.source_available === false ? (
                    <span data-testid={'candidate-source-missing-' + c.skill_name}>
                      source no longer available
                    </span>
                  ) : null}
                  <span className="ml-auto inline-flex items-center gap-1 text-accent">
                    Review <Icon name="chevright" size={11} />
                  </span>
                </div>
              </button>
              {failed ? (
                <div
                  className="flex flex-wrap items-center gap-2 border-t border-[color-mix(in_oklab,var(--danger)_20%,transparent)] px-4 py-2.5"
                  data-testid={'candidate-failed-' + c.skill_name}
                >
                  <Chip className="border-[color-mix(in_oklab,var(--danger)_45%,transparent)] text-danger">extraction failed</Chip>
                  <span
                    className="min-w-0 flex-1 truncate text-[11px] text-muted"
                    title={c.reason || undefined}
                  >
                    {c.reason || 'The proposal could not be extracted from the evidence.'}
                  </span>
                  <button
                    type="button"
                    onClick={() => void retry(c)}
                    disabled={retryingId === c.id}
                    data-testid={'btn-candidate-retry-' + c.skill_name}
                    className="h-7 rounded-md border border-line px-2.5 text-[11px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg disabled:opacity-40"
                  >
                    {retryingId === c.id ? 'Retrying…' : 'Retry'}
                  </button>
                </div>
              ) : null}
            </div>
          </li>
        );
      })}
    </ul>
  );
}
