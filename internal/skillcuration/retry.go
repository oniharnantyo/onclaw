package skillcuration

import (
	"context"
	"fmt"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// The manual retry (extraction-failed cards): a failed candidate's retry
// action re-attempts extraction for its cluster through the SAME draft path
// the cycle runs (proposer.go's draft) — one model call, no retry loop (the
// cycle owns the bounded-retry budget; the human trigger is the second
// chance). Approval semantics are untouched: nothing here feeds the review
// gate, and the candidate store is the only system of record touched.

// RetryCandidate re-attempts extraction for one failed candidate's cluster.
// On success the row moves back to pending carrying the fresh draft's name,
// content, edit shape, and evidence (UpdateDraft — the review restarts);
// on any extraction failure the row's error message (Reason) is refreshed
// and the error returns, so the caller can surface it while the card stays
// in the queue with the updated message. Unknown or foreign-workspace
// candidates are domain.ErrNotFound; any non-failed status is
// domain.ErrConflict. Fail-soft: the row's failure message update is
// best-effort — the original failure is what propagates.
func (p *Proposer) RetryCandidate(ctx context.Context, workspaceID, candidateID string) (*domain.SkillCandidate, error) {
	candidate, err := p.clusters.Get(ctx, workspaceID, candidateID)
	if err != nil {
		return nil, err
	}
	if candidate.Status != domain.SkillCandidateFailed {
		return nil, fmt.Errorf("%w: candidate %s is %s, only a failed candidate can be retried",
			domain.ErrConflict, candidate.ID, candidate.Status)
	}

	ctx, cancel := context.WithTimeout(ctx, p.budget)
	defer cancel()

	req := ProposeRequest{WorkspaceID: workspaceID, AgentID: candidate.AgentID, ClusterID: candidate.ClusterID}

	membership, err := p.clusters.ListClusterRunsByCluster(ctx, workspaceID, candidate.ClusterID)
	if err != nil {
		return nil, fmt.Errorf("skillcuration: list cluster runs: %w", err)
	}
	clusterCandidates, err := p.clusters.ListByCluster(ctx, workspaceID, candidate.ClusterID)
	if err != nil {
		return nil, fmt.Errorf("skillcuration: list cluster candidates: %w", err)
	}
	qualifying, err := ClusterQualifyingCount(ctx, p.clusters, workspaceID, candidate.ClusterID)
	if err != nil {
		return nil, err
	}

	out, err := p.draft(ctx, req, membership, qualifying, liveSibling(clusterCandidates), 0)
	if err == nil && out.candidate != nil {
		if err := p.clusters.UpdateDraft(ctx, workspaceID, candidate.ID, out.candidate); err != nil {
			return nil, fmt.Errorf("skillcuration: reset candidate to pending: %w", err)
		}
		p.log.InfoContext(ctx, "skillcuration: candidate retry drafted",
			"workspace_id", workspaceID, "agent_id", candidate.AgentID,
			"cluster_id", candidate.ClusterID, "candidate_id", candidate.ID,
			"skill_name", out.candidate.SkillName)
		return p.clusters.Get(ctx, workspaceID, candidate.ID)
	}

	// Every failure shape refreshes the row's error message — the card in
	// the review queue stays and shows why the retry did not land.
	failure := out.problems
	switch {
	case err != nil:
		failure = err
	case out.declined != "":
		failure = fmt.Errorf("retry found no extractable draft: %s",
			strings.TrimPrefix(out.declined, "skillcuration: "))
	}
	if uerr := p.clusters.UpdateStatus(ctx, workspaceID, candidate.ID, domain.SkillCandidateFailed, failure.Error()); uerr != nil {
		p.log.ErrorContext(ctx, "skillcuration: candidate retry failure not recorded",
			"workspace_id", workspaceID, "candidate_id", candidate.ID, "error", uerr)
	}
	p.log.InfoContext(ctx, "skillcuration: candidate retry failed",
		"workspace_id", workspaceID, "candidate_id", candidate.ID, "error", failure.Error())
	return nil, failure
}
