// Patterns sub-tab (add-skill-curation-from-traces 9.3): the workspace's
// skill-wiki pages read-only — title, active/superseded status with the
// successor pointer, the skills citing each page (derived from the
// candidates' cited_pattern_refs), and evidence-run links. There is no
// pattern-editing affordance; maintenance is the cycle's job.
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { cx } from "../../../lib/helpers";
import { Icon } from "../../../components/ui/Icon";
import { Chip } from "../../../components/ui/Chip";
import {
  api,
  type ApiPatternPage,
  type ApiSkillCandidate,
} from "../../../lib/api";

export interface PatternPageViewProps {
  page: ApiPatternPage;
  /** Skill names citing this page (derived from the candidates list). */
  citedBy?: string[];
  /** Click a successor link: swap the viewed page without leaving the context. */
  onOpenSlug?: (slug: string) => void;
  testIdPrefix?: string;
}

/** One wiki page in its page view: identity, status, citations, evidence. */
export function PatternPageView({ page, citedBy = [], onOpenSlug, testIdPrefix = 'pattern-page' }: PatternPageViewProps) {
  const navigate = useNavigate();
  const superseded = page.status === 'superseded';
  return (
    <div data-testid={testIdPrefix} className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <h4 className="text-[14px] font-semibold text-fg" data-testid={testIdPrefix + '-title'}>
          {page.title || page.slug}
        </h4>
        {superseded ? (
          <Chip className="border-[color-mix(in_oklab,var(--warn)_45%,transparent)] text-[color-mix(in_oklab,var(--warn),black_38%)]">
            superseded
          </Chip>
        ) : (
          <Chip className="border-[color-mix(in_oklab,var(--success)_45%,transparent)] text-[color-mix(in_oklab,var(--success),black_25%)]">
            active
          </Chip>
        )}
        <Chip mono>{page.slug}</Chip>
      </div>
      {superseded && page.superseded_by ? (
        <p className="text-[12px] text-muted" data-testid={testIdPrefix + '-superseded-by'}>
          Replaced by{' '}
          <button
            type="button"
            data-testid={testIdPrefix + '-successor'}
            onClick={() => onOpenSlug?.(page.superseded_by!)}
            className="font-mono text-accent transition-colors hover:underline"
          >
            {page.superseded_by}
          </button>
          .
        </p>
      ) : null}
      {citedBy.length > 0 ? (
        <div className="flex flex-wrap items-center gap-1.5" data-testid={testIdPrefix + '-cited-by'}>
          <span className="text-[11px] uppercase tracking-[0.14em] text-muted">Cited by</span>
          {citedBy.map((name) => (
            <Chip key={name} mono>
              {name}
            </Chip>
          ))}
        </div>
      ) : null}
      {page.evidence_runs.length > 0 ? (
        <div>
          <p className="mb-1 text-[11px] uppercase tracking-[0.14em] text-muted">Evidence runs</p>
          <ul className="space-y-1">
            {page.evidence_runs.map((run) => (
              <li key={run}>
                <button
                  type="button"
                  onClick={() => navigate('/runs')}
                  className="inline-flex items-center gap-1.5 rounded-[6px] border border-line px-2 py-0.5 font-mono text-[11px] text-fg2 transition-colors hover:border-accent hover:text-fg"
                  title="Open the run history to inspect this evidence run's transcript"
                  data-testid={testIdPrefix + '-evidence-' + run}
                >
                  <Icon name="external-link" size={11} className="text-muted" />
                  {run}
                </button>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      <pre
        className="od-scroll max-h-64 overflow-auto whitespace-pre-wrap rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] p-3 font-mono text-[11px] leading-4 text-fg2"
        data-testid={testIdPrefix + '-body'}
      >
        {page.body}
      </pre>
    </div>
  );
}

export interface PatternsTabProps {
  wsSlug: string;
  /** Controlled open page slug (deep link from the review takeover). Left
   * undefined, the tab manages its own open page — the standalone sub-tab. */
  openSlug?: string | null;
  onOpenSlugChange?: (slug: string | null) => void;
}

export function PatternsTab({ wsSlug, openSlug, onOpenSlugChange }: PatternsTabProps) {
  const [pages, setPages] = useState<ApiPatternPage[] | null>(null);
  const [candidates, setCandidates] = useState<ApiSkillCandidate[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  // Self-managed open page for the standalone (uncontrolled) sub-tab; the
  // takeover drives the same view through the controlled props.
  const [selfOpen, setSelfOpen] = useState<string | null>(null);
  const open = openSlug !== undefined ? openSlug : selfOpen;
  const setOpen = (slug: string | null) => {
    setSelfOpen(slug);
    onOpenSlugChange?.(slug);
  };

  const load = async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const [patternRes, candidateRes] = await Promise.all([
        api.curation.listPatterns(wsSlug),
        api.curation.listCandidates(wsSlug).catch(() => ({ candidates: [] as ApiSkillCandidate[], count: 0 })),
      ]);
      setPages(patternRes.patterns || []);
      setCandidates(candidateRes.candidates || []);
    } catch (err: unknown) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (wsSlug) void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reload per workspace only
  }, [wsSlug]);

  // Cited-by is derived, not stored: which proposed/curated skills reference
  // each page slug (the candidates list carries cited_pattern_refs).
  const citedByBySlug = useMemo(() => {
    const map = new Map<string, string[]>();
    for (const c of candidates) {
      for (const ref of c.cited_pattern_refs || []) {
        const names = map.get(ref) || [];
        if (!names.includes(c.skill_name)) names.push(c.skill_name);
        map.set(ref, names);
      }
    }
    return map;
  }, [candidates]);

  if (loading && pages === null) {
    return (
      <div className="space-y-3" data-testid="patterns-loading">
        {[0, 1].map((i) => (
          <div key={i} className="h-16 animate-pulse rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]" />
        ))}
      </div>
    );
  }
  if (loadError) {
    return (
      <div className="rounded-md border border-line px-4 py-6 text-center" data-testid="patterns-error">
        <p className="text-[13px] font-medium text-fg">Couldn&apos;t load patterns</p>
        <p className="mt-1 text-[12px] text-muted">{loadError}</p>
        <button
          type="button"
          onClick={() => void load()}
          data-testid="btn-patterns-retry"
          className="mt-3 h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
        >
          Retry
        </button>
      </div>
    );
  }

  const opened = open ? (pages || []).find((p) => p.slug === open) : null;

  if (opened) {
    return (
      <div data-testid="patterns-page-view">
        <button
          type="button"
          onClick={() => setOpen(null)}
          data-testid="btn-patterns-back"
          className="mb-3 inline-flex h-7 items-center gap-1 rounded-md px-2 text-[12px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
        >
          <Icon name="arrow-left" size={12} /> All patterns
        </button>
        <div className="rounded-md border border-line p-4">
          <PatternPageView page={opened} citedBy={citedByBySlug.get(opened.slug) || []} onOpenSlug={setOpen} />
        </div>
      </div>
    );
  }

  return (
    <div data-testid="patterns-list">
      {(pages || []).length === 0 ? (
        <div className="rounded-md border border-line px-4 py-8 text-center" data-testid="patterns-empty">
          <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] text-muted">
            <Icon name="file-text" size={20} />
          </div>
          <p className="text-[14px] font-medium text-fg">No pattern pages yet</p>
          <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
            The wiki grows as the curation cycle maintains shared procedures — pages appear when recurring work
            stabilizes into a documented pattern.
          </p>
        </div>
      ) : (
        <ul className="divide-y divide-[var(--border-soft)]">
          {(pages || []).map((p) => {
            const superseded = p.status === 'superseded';
            const citedBy = citedByBySlug.get(p.slug) || [];
            return (
              <li key={p.slug} className="py-3" data-testid={'pattern-' + p.slug}>
                <div className={cx('flex flex-wrap items-center gap-2', superseded && 'opacity-80')}>
                  <button
                    type="button"
                    onClick={() => setOpen(p.slug)}
                    data-testid={'btn-open-pattern-' + p.slug}
                    className="text-[13px] font-medium text-fg transition-colors hover:text-accent"
                  >
                    {p.title || p.slug}
                  </button>
                  <Chip mono>{p.slug}</Chip>
                  {superseded ? (
                    <Chip className="border-[color-mix(in_oklab,var(--warn)_45%,transparent)] text-[color-mix(in_oklab,var(--warn),black_38%)]">
                      superseded{p.superseded_by ? ' → ' : ''}
                      {p.superseded_by && (
                        <button
                          type="button"
                          data-testid={'btn-successor-' + p.slug}
                          onClick={() => setOpen(p.superseded_by!)}
                          className="font-mono text-accent transition-colors hover:underline"
                        >
                          {p.superseded_by}
                        </button>
                      )}
                    </Chip>
                  ) : (
                    <Chip className="border-[color-mix(in_oklab,var(--success)_45%,transparent)] text-[color-mix(in_oklab,var(--success),black_25%)]">
                      active
                    </Chip>
                  )}
                  {citedBy.map((name) => (
                    <Chip key={name} mono>
                      cited by {name}
                    </Chip>
                  ))}
                  <span className="ml-auto text-[11px] text-muted">
                    {p.evidence_runs.length} evidence {p.evidence_runs.length === 1 ? 'run' : 'runs'}
                  </span>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
