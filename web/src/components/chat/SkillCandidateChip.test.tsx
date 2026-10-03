import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { SkillCandidateChip, candidateReviewHref } from './SkillCandidateChip';

// The entry shape the transcript translator mints for a `skill_candidate`
// event (identical live and hydrated — see livechat.ts).
const chipMessage = {
  id: 'h-skc-1',
  author: 'skill_candidate',
  ts: '2026-09-16T01:00:00Z',
  text: '',
  skillCandidate: {
    workspaceId: 'acme',
    agentId: 'ag-radar',
    sessionId: 'sess-123',
    runId: 'turn-9',
    clusterId: 'cl-deploy',
    skillName: 'deploy-rollout',
    candidateId: 'cand-1234',
  },
};

describe('components/chat/SkillCandidateChip', () => {
  it('renders the drafted-candidate notice styled like the memory chip', () => {
    const { container } = render(
      <MemoryRouter>
        <SkillCandidateChip m={chipMessage} />
      </MemoryRouter>
    );

    const chip = screen.getByTestId('skill-candidate-chip-h-skc-1');
    expect(chip.textContent).toContain('A skill candidate was drafted from this run');
    // The draft's skill name, once the proposer has emitted it.
    expect(chip.textContent).toContain('deploy-rollout');
    // Same wrapper shape as the memory chip.
    expect(container.querySelector('[data-role="skill-candidate-chip"]')).not.toBeNull();
  });

  it('links to the Curation review surface focused on the candidate', () => {
    render(
      <MemoryRouter>
        <SkillCandidateChip m={chipMessage} />
      </MemoryRouter>
    );

    const link = screen.getByTestId('skill-candidate-link-h-skc-1');
    expect(link.getAttribute('href')).toBe('/settings/skills?tab=curation&candidate=cand-1234');
    expect(link.textContent).toContain('Review');
  });

  it('links to the Curation queue without a candidate focus while only qualification is known', () => {
    const qualificationOnly = {
      ...chipMessage,
      id: 'h-skc-2',
      skillCandidate: { ...chipMessage.skillCandidate, candidateId: '', skillName: '' },
    };
    render(
      <MemoryRouter>
        <SkillCandidateChip m={qualificationOnly} />
      </MemoryRouter>
    );

    expect(screen.getByTestId('skill-candidate-link-h-skc-2').getAttribute('href')).toBe('/settings/skills?tab=curation');
    // No skill name yet — the proposer has not drafted one.
    expect(screen.getByTestId('skill-candidate-chip-h-skc-2').textContent).not.toContain('·');
  });

  it('renders nothing without a payload (defensive — never a dead chip)', () => {
    const { container } = render(<SkillCandidateChip m={{ id: 'x', author: 'skill_candidate' }} />);
    expect(container.querySelector('[data-role="skill-candidate-chip"]')).toBeNull();
  });

  it('href helper accepts the raw wire shape too', () => {
    expect(candidateReviewHref({ candidate_id: 'raw-1' })).toBe('/settings/skills?tab=curation&candidate=raw-1');
    expect(candidateReviewHref(undefined)).toBe('/settings/skills?tab=curation');
  });
});
