// Candidate review takeover (add-skill-curation-from-traces 9.2): one wide
// modal, content on the left — a readable preview for creates, a before/after
// diff for edits — and the trust surface on the right: the cluster's evidence
// runs (deep-linked to the transcript surface) plus the cited-patterns panel
// where each pattern opens its wiki page without leaving the review.
//
// Footer: Approve and Reject (Owner/Admin only — Members read the same
// content without action affordances). Reject requires choosing a reason
// before it can be recorded; 409 conflicts surface the server's message
// (budget remedy / race collision) without losing the review.
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { cx } from "../../../lib/helpers";
import { Icon } from "../../../components/ui/Icon";
import { Chip } from "../../../components/ui/Chip";
import { Modal } from "../../../components/ui/Modal";
import { ApiError, api, type ApiPatternPage, type ApiSkillCandidate } from "../../../lib/api";
import { lineDiff } from "./diff";
import { PatternPageView } from "./PatternsTab";

/** The reviewer-facing rejection reasons (spec: wrong procedure / already
 * covered / contradicts newer information / other + free text). The server
 * accepts any non-empty string and records it verbatim. */
export const REJECT_REASONS = [
  'Wrong procedure',
  'Already covered',
  'Contradicts newer information',
  'Other',
] as const;

export interface CandidateTakeoverProps {
  wsSlug: string;
  candidateId: string;
  /** Owner/Admin gate — same skills.write check the pane derives. */
  canWrite: boolean;
  onClose: () => void;
  /** Fired after a recorded approve/reject so the queue and badge refresh. */
  onDecided?: () => void;
  onToast?: (text: string, kind?: string) => void;
}

export function CandidateTakeover({ wsSlug, candidateId, canWrite, onClose, onDecided, onToast = () => {} }: CandidateTakeoverProps) {
  const navigate = useNavigate();
  const [candidate, setCandidate] = useState<ApiSkillCandidate | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // Reject flow: the picker replaces the footer actions until resolved.
  const [rejectOpen, setRejectOpen] = useState(false);
  const [reason, setReason] = useState<string | null>(null);
  const [reasonText, setReasonText] = useState('');
  const [conflict, setConflict] = useState<string | null>(null);
  // Nested pattern view (the evidence-chain walk).
  const [openPattern, setOpenPattern] = useState<string | null>(null);
  const [patterns, setPatterns] = useState<ApiPatternPage[] | null>(null);

  const load = async () => {
    setLoadError(null);
    try {
      const res = await api.curation.getCandidate(wsSlug, candidateId);
      setCandidate(res.candidate);
    } catch (err: unknown) {
      setLoadError(err instanceof Error ? err.message : String(err));
    }
  };

  useEffect(() => {
    if (wsSlug && candidateId) void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- load per candidate only
  }, [wsSlug, candidateId]);

  const ensurePatterns = async (): Promise<ApiPatternPage[]> => {
    if (patterns) return patterns;
    const res = await api.curation.listPatterns(wsSlug);
    setPatterns(res.patterns || []);
    return res.patterns || [];
  };

  const openPatternPage = async (slug: string) => {
    setOpenPattern(slug);
    try {
      await ensurePatterns();
    } catch {
      // The pattern panel shows its own absence; the review stays usable.
    }
  };

  const patchPattern = (slug: string): ApiPatternPage | null =>
    (patterns || []).find((p) => p.slug === slug) || null;

  const review = async (kind: 'approve' | 'reject') => {
    if (!candidate) return;
    setBusy(true);
    setConflict(null);
    try {
      if (kind === 'approve') {
        await api.curation.approve(wsSlug, candidate.id);
        onToast(`${candidate.skill_name} approved — now provisional on its agent`);
      } else {
        const finalReason = reason === 'Other' ? (reasonText.trim() || 'Other') : (reason || '');
        await api.curation.reject(wsSlug, candidate.id, finalReason);
        onToast(`${candidate.skill_name} rejected — the reason is recorded in the audit`);
      }
      onDecided?.();
      onClose();
    } catch (err: unknown) {
      if (err instanceof ApiError && err.status === 409) {
        // Budget refusal / race collision / already decided — the server's
        // message names the remedy; the review stays open.
        setConflict(err.message);
      } else {
        setConflict(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setBusy(false);
    }
  };

  const patternPage = openPattern ? patchPattern(openPattern) : null;
  const footerVisible = canWrite && candidate?.status === 'pending';

  const diffLines = candidate?.is_edit
    ? lineDiff(candidate.superseded_content || '', candidate.proposed_content || '')
    : [];

  return (
    <Modal
      title="Review skill candidate"
      onClose={onClose}
      wide
      odId="modal-candidate-takeover"
      data-testid="modal-candidate-takeover"
      footer={
        footerVisible ? (
          rejectOpen ? (
            <>
              <button
                type="button"
                onClick={() => {
                  setRejectOpen(false);
                  setReason(null);
                  setReasonText('');
                }}
                data-testid="btn-reject-cancel"
                className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void review('reject')}
                disabled={busy || !reason || (reason === 'Other' && !reasonText.trim())}
                data-testid="btn-reject-confirm"
                className="flex h-9 items-center rounded-md bg-danger px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
              >
                {busy ? 'Recording…' : 'Record rejection'}
              </button>
            </>
          ) : (
            <>
              <button
                type="button"
                onClick={() => void review('approve')}
                disabled={busy}
                data-testid="btn-approve"
                className="flex h-9 items-center gap-1.5 rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:opacity-90 disabled:opacity-40"
              >
                <Icon name="check" size={13} /> {busy ? 'Working…' : 'Approve'}
              </button>
              <button
                type="button"
                onClick={() => setRejectOpen(true)}
                disabled={busy}
                data-testid="btn-reject"
                className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-danger hover:text-danger disabled:opacity-40"
              >
                Reject
              </button>
            </>
          )
        ) : undefined
      }
    >
      {loadError ? (
        <div className="px-5 py-8 text-center" data-testid="takeover-error">
          <p className="text-[13px] font-medium text-fg">Couldn&apos;t load the candidate</p>
          <p className="mt-1 text-[12px] text-muted">{loadError}</p>
          <button
            type="button"
            onClick={() => void load()}
            data-testid="btn-takeover-retry"
            className="mt-3 h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
          >
            Retry
          </button>
        </div>
      ) : !candidate ? (
        <div className="px-5 py-8" data-testid="takeover-loading">
          <div className="mx-auto h-8 w-8 animate-spin rounded-full border-2 border-line border-t-accent" />
        </div>
      ) : (
        <div className="space-y-3 p-5">
          {/* Identity strip */}
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-[14px] font-medium text-fg" data-testid="takeover-skill-name">
              {candidate.skill_name}
            </span>
            {candidate.is_edit ? (
              <Chip className="border-[color-mix(in_oklab,var(--warn)_45%,transparent)] text-[color-mix(in_oklab,var(--warn),black_38%)]">
                edit · supersedes <span className="font-mono">{candidate.supersedes_skill_name}</span>
              </Chip>
            ) : (
              <Chip>new skill</Chip>
            )}
            <Chip>{candidate.status}</Chip>
            <Chip mono>agent · {candidate.agent_id}</Chip>
            <Chip mono>cluster · {candidate.cluster_id}</Chip>
          </div>

          {conflict ? (
            <p
              className="rounded-md border border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] px-3 py-2 text-[12px] text-danger"
              data-testid="takeover-conflict"
              role="alert"
            >
              {conflict}
            </p>
          ) : null}

          {rejectOpen && footerVisible ? (
            <div className="rounded-md border border-line p-3" data-testid="reject-reason-picker">
              <p className="mb-2 text-[12px] font-medium text-fg2">Why is this proposal rejected?</p>
              <div className="flex flex-wrap gap-2">
                {REJECT_REASONS.map((r) => (
                  <button
                    key={r}
                    type="button"
                    onClick={() => setReason(r)}
                    aria-pressed={reason === r}
                    data-testid={'reject-reason-' + r.toLowerCase().replace(/[^a-z]+/g, '-')}
                    className={cx(
                      'flex h-8 items-center rounded-md border px-3 text-[12px] font-medium transition-colors',
                      reason === r
                        ? 'border-accent bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg'
                        : 'border-line text-muted hover:border-[color-mix(in_oklab,var(--fg)_26%,transparent)] hover:text-fg2'
                    )}
                  >
                    {r}
                  </button>
                ))}
              </div>
              {reason === 'Other' ? (
                <input
                  type="text"
                  value={reasonText}
                  onChange={(e) => setReasonText(e.target.value)}
                  placeholder="Describe the reason — it is recorded verbatim"
                  data-testid="reject-reason-text"
                  className="mt-2 h-9 w-full rounded-md border border-line bg-surface px-3 text-[13px] text-fg placeholder:text-muted focus:border-accent focus:outline-none"
                />
              ) : null}
            </div>
          ) : null}

          {/* Content (left) / evidence (right) */}
          <div className="grid gap-4 lg:grid-cols-2">
            {/* Left: the proposed content */}
            <div className="min-w-0" data-testid="takeover-content">
              <p className="mb-1.5 text-[10px] font-medium uppercase tracking-[0.14em] text-muted">
                {candidate.is_edit ? 'Proposed changes' : 'Proposed skill content'}
              </p>
              {candidate.is_edit ? (
                <div className="od-scroll max-h-[26rem] overflow-auto rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] p-3" data-testid="takeover-diff">
                  {diffLines.map((l, i) => (
                    <div
                      key={i}
                      className={cx(
                        'whitespace-pre-wrap font-mono text-[11px] leading-4',
                        l.kind === 'added' && 'bg-[color-mix(in_oklab,var(--success)_14%,transparent)] text-[color-mix(in_oklab,var(--success),black_15%)]',
                        l.kind === 'removed' && 'bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] text-danger'
                      )}
                    >
                      <span className={cx('mr-1.5 inline-block w-2 select-none', l.kind === 'added' && 'text-[color-mix(in_oklab,var(--success),black_15%)]', l.kind === 'removed' && 'text-danger')}>
                        {l.kind === 'added' ? '+' : l.kind === 'removed' ? '−' : ' '}
                      </span>
                      {l.text}
                    </div>
                  ))}
                </div>
              ) : (
                <pre
                  className="od-scroll max-h-[26rem] overflow-auto whitespace-pre-wrap rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_3%,transparent)] p-3 font-mono text-[11px] leading-4 text-fg2"
                  data-testid="takeover-preview"
                >
                  {candidate.proposed_content}
                </pre>
              )}
            </div>

            {/* Right: the trust surface */}
            <div className="min-w-0" data-testid="takeover-evidence">
              {patternPage ? (
                <>
                  <div className="mb-1.5 flex items-center justify-between">
                    <p className="text-[10px] font-medium uppercase tracking-[0.14em] text-muted">Cited pattern</p>
                    <button
                      type="button"
                      onClick={() => setOpenPattern(null)}
                      data-testid="btn-pattern-back"
                      className="inline-flex h-6 items-center gap-1 rounded px-1.5 text-[11px] font-medium text-muted transition-colors hover:text-fg"
                    >
                      <Icon name="arrow-left" size={11} /> Evidence
                    </button>
                  </div>
                  <div className="rounded-md border border-line p-3">
                    <PatternPageView
                      page={patternPage}
                      testIdPrefix="takeover-pattern"
                      onOpenSlug={(slug) => void openPatternPage(slug)}
                    />
                  </div>
                </>
              ) : (
                <>
                  <p className="mb-1.5 text-[10px] font-medium uppercase tracking-[0.14em] text-muted">Evidence</p>
                  <div className="space-y-3">
                    <div>
                      <p className="mb-1 text-[11px] text-muted">
                        {candidate.evidence_event_ids.length} evidence {candidate.evidence_event_ids.length === 1 ? 'event' : 'events'} from{' '}
                        {candidate.evidence_event_ids.length === 1 ? 'this run' : 'these runs'} — open the transcript:
                      </p>
                      <ul className="flex flex-wrap gap-1.5">
                        {candidate.evidence_event_ids.map((ev) => (
                          <li key={ev}>
                            <button
                              type="button"
                              onClick={() => navigate('/c/' + encodeURIComponent(candidate.agent_id))}
                              title="Open this agent's transcript — the run's session lives there"
                              data-testid={'takeover-evidence-' + ev}
                              className="inline-flex items-center gap-1 rounded-[6px] border border-line px-2 py-0.5 font-mono text-[11px] text-fg2 transition-colors hover:border-accent hover:text-fg"
                            >
                              <Icon name="external-link" size={11} className="text-muted" />
                              {ev}
                            </button>
                          </li>
                        ))}
                      </ul>
                    </div>
                    <div>
                      <p className="mb-1 text-[11px] text-muted">
                        {(candidate.cited_pattern_refs || []).length === 0
                          ? 'No wiki patterns cited.'
                          : 'Builds on these wiki patterns:'}
                      </p>
                      <ul className="flex flex-wrap gap-1.5">
                        {(candidate.cited_pattern_refs || []).map((ref) => (
                          <li key={ref}>
                            <button
                              type="button"
                              onClick={() => void openPatternPage(ref)}
                              data-testid={'takeover-cited-pattern-' + ref}
                              className="inline-flex items-center gap-1 rounded-[6px] border border-line px-2 py-0.5 font-mono text-[11px] text-fg2 transition-colors hover:border-accent hover:text-fg"
                            >
                              <Icon name="file-text" size={11} className="text-muted" />
                              {ref}
                            </button>
                          </li>
                        ))}
                      </ul>
                    </div>
                    {patterns !== null && patternPage === null && openPattern && !patchPattern(openPattern) ? (
                      <p className="text-[11px] text-muted" data-testid="takeover-pattern-missing">
                        The cited pattern page is no longer in the wiki.
                      </p>
                    ) : null}
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      )}
    </Modal>
  );
}
