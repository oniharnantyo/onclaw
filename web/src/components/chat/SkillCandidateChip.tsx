// In-chat skill-candidate chip (add-skill-curation-from-traces 9.4b): a run
// that passed every qualification gate renders this chip in its transcript —
// styled consistently with the memory-ingested chip — stating that a skill
// candidate was drafted from the run and linking to the Curation review
// surface (Settings → Skills → Curation) where approvers walk the evidence.
//
// Hydration-safe: the transcript translator mints the same entry shape for
// the live/catch-up stream and the reloaded history, so the chip renders
// identically after a reload — it is just another session event.
import { Link } from "react-router-dom";
import { cx } from "../../lib/helpers";
import { Icon } from "../ui/Icon";

export interface SkillCandidateChipProps {
  m: any;
}

/** The review-surface deep link: the Curation tab, focused on the candidate
 * when the payload already carries its row id (the proposer's emission does;
 * the bare qualification signal leaves it empty). */
export function candidateReviewHref(skillCandidate: any): string {
  const candidateId = skillCandidate?.candidateId || skillCandidate?.candidate_id || '';
  return candidateId
    ? `/settings/skills?tab=curation&candidate=${encodeURIComponent(candidateId)}`
    : '/settings/skills?tab=curation';
}

export function SkillCandidateChip({ m }: SkillCandidateChipProps) {
  const payload = m?.skillCandidate;
  if (!payload) return null;

  return (
    <div className="flex px-2" data-od-id={'msg-' + m.id} data-role="skill-candidate-chip">
      <div className="min-w-0 flex-1">
        <div
          data-testid={'skill-candidate-chip-' + m.id}
          className="inline-flex max-w-full flex-col rounded-md border border-[color-mix(in_oklab,var(--accent)_28%,transparent)] bg-[color-mix(in_oklab,var(--accent)_6%,transparent)]"
        >
          <div className="flex items-center gap-2 px-3 py-1.5">
            <span aria-hidden className="shrink-0 text-[12px]">
              🧪
            </span>
            <span className="text-[12px] font-medium text-fg2">
              A skill candidate was drafted from this run
              {payload.skillName ? (
                <span className="font-mono text-muted"> · {payload.skillName}</span>
              ) : null}
            </span>
            <Link
              to={candidateReviewHref(payload)}
              data-testid={'skill-candidate-link-' + m.id}
              className={cx(
                'ml-auto inline-flex shrink-0 items-center gap-1 rounded-[6px] border border-[color-mix(in_oklab,var(--accent)_35%,transparent)]',
                'px-2 py-0.5 text-[11px] font-medium text-accent transition-colors hover:bg-[color-mix(in_oklab,var(--accent)_10%,transparent)]'
              )}
              title="Open the Curation review queue in workspace settings"
            >
              Review
              <Icon name="arrow-right" size={11} />
            </Link>
          </div>
        </div>
      </div>
    </div>
  );
}
