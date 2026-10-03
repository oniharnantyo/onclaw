package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// SkillCandidates returns the SkillCandidateStore sub-port.
func (s *store) SkillCandidates() storeport.SkillCandidateStore {
	return NewSkillCandidateStore(s.db)
}

// skillCandidateStore implements storeport.SkillCandidateStore for PostgreSQL
// over skill_candidates and skill_impact_entries
// (add-skill-curation-from-traces 2.3). Every read carries the workspace
// partition — a foreign-workspace id is indistinguishable from an unknown
// one. Timestamps are app-managed (000054 house rule): the store stamps
// ProposedAt/UpdatedAt/CreatedAt/DecidedAt, never the DB. Impact entries are
// append-only: nothing overwrites or deletes one.
type skillCandidateStore struct {
	db Executor
}

// NewSkillCandidateStore creates a new SkillCandidateStore with the given database executor.
func NewSkillCandidateStore(db Executor) storeport.SkillCandidateStore {
	return &skillCandidateStore{db: db}
}

const skillCandidateColumns = `id, workspace_id, agent_id, cluster_id, skill_name, status,
	proposed_content, is_edit, supersedes_skill_name, superseded_content,
	evidence_event_ids, cited_pattern_refs, reason,
	helpful_count, harmful_count, use_count, proposed_at, decided_at, updated_at`

const skillImpactColumns = `id, workspace_id, cluster_id, skill_name, verdict, diff, reason, reviewer, created_at`

// marshalSkillStringList stores a string list as jsonb, normalizing nil to
// the empty array so the served JSON never shows null.
func marshalSkillStringList(ids []string) ([]byte, error) {
	if ids == nil {
		ids = []string{}
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid skill candidate evidence list: %v", domain.ErrInvalid, err)
	}
	return data, nil
}

// unmarshalSkillStringList reads one jsonb string list back, normalizing nil
// to the empty array.
func unmarshalSkillStringList(data []byte) ([]string, error) {
	if len(data) == 0 {
		return []string{}, nil
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, fmt.Errorf("%w: invalid skill candidate evidence list jsonb: %v", domain.ErrInvalid, err)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

func scanSkillCandidate(row pgx.Row) (*domain.SkillCandidate, error) {
	var c domain.SkillCandidate
	var status string
	var evidence, cited []byte
	err := row.Scan(&c.ID, &c.WorkspaceID, &c.AgentID, &c.ClusterID, &c.SkillName, &status,
		&c.ProposedContent, &c.IsEdit, &c.SupersedesSkillName, &c.SupersededContent,
		&evidence, &cited, &c.Reason,
		&c.HelpfulCount, &c.HarmfulCount, &c.UseCount, &c.ProposedAt, &c.DecidedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, convertError(err)
	}
	c.Status = domain.SkillCandidateStatus(status)
	if c.EvidenceEventIDs, err = unmarshalSkillStringList(evidence); err != nil {
		return nil, err
	}
	if c.CitedPatternRefs, err = unmarshalSkillStringList(cited); err != nil {
		return nil, err
	}
	return &c, nil
}

func scanSkillImpactEntry(row pgx.Row) (*domain.SkillImpactEntry, error) {
	var e domain.SkillImpactEntry
	var verdict string
	err := row.Scan(&e.ID, &e.WorkspaceID, &e.ClusterID, &e.SkillName, &verdict,
		&e.Diff, &e.Reason, &e.Reviewer, &e.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, convertError(err)
	}
	e.Verdict = domain.SkillImpactVerdict(verdict)
	return &e, nil
}

func (st *skillCandidateStore) Save(ctx context.Context, candidate *domain.SkillCandidate) error {
	if candidate.Status == "" {
		candidate.Status = domain.SkillCandidatePending
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	if candidate.ID == "" {
		candidate.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if candidate.ProposedAt.IsZero() {
		candidate.ProposedAt = now
	}
	candidate.UpdatedAt = now
	evidence, err := marshalSkillStringList(candidate.EvidenceEventIDs)
	if err != nil {
		return err
	}
	cited, err := marshalSkillStringList(candidate.CitedPatternRefs)
	if err != nil {
		return err
	}

	const query = `
		INSERT INTO skill_candidates (
			id, workspace_id, agent_id, cluster_id, skill_name, status,
			proposed_content, is_edit, supersedes_skill_name, superseded_content,
			evidence_event_ids, cited_pattern_refs, reason,
			helpful_count, harmful_count, use_count, proposed_at, decided_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	`
	if _, err := st.db.Exec(ctx, query,
		candidate.ID, candidate.WorkspaceID, candidate.AgentID, candidate.ClusterID,
		candidate.SkillName, string(candidate.Status),
		candidate.ProposedContent, candidate.IsEdit, candidate.SupersedesSkillName,
		candidate.SupersededContent, evidence, cited, candidate.Reason,
		candidate.HelpfulCount, candidate.HarmfulCount, candidate.UseCount,
		candidate.ProposedAt, candidate.DecidedAt, candidate.UpdatedAt); err != nil {
		return convertError(err)
	}
	return nil
}

func (st *skillCandidateStore) Get(ctx context.Context, workspaceID, id string) (*domain.SkillCandidate, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	const query = `
		SELECT ` + skillCandidateColumns + `
		FROM skill_candidates
		WHERE workspace_id = $1 AND id = $2
	`
	c, err := scanSkillCandidate(st.db.QueryRow(ctx, query, workspaceID, id))
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (st *skillCandidateStore) List(ctx context.Context, workspaceID string, status domain.SkillCandidateStatus) ([]domain.SkillCandidate, error) {
	if workspaceID == "" {
		return []domain.SkillCandidate{}, nil
	}

	query := `
		SELECT ` + skillCandidateColumns + `
		FROM skill_candidates
		WHERE workspace_id = $1`
	args := []any{workspaceID}
	if status != "" {
		query += ` AND status = $2`
		args = append(args, string(status))
	}
	query += `
		ORDER BY proposed_at DESC, id DESC`

	return st.querySkillCandidates(ctx, query, args...)
}

func (st *skillCandidateStore) ListByCluster(ctx context.Context, workspaceID, clusterID string) ([]domain.SkillCandidate, error) {
	if workspaceID == "" || clusterID == "" {
		return []domain.SkillCandidate{}, nil
	}

	const query = `
		SELECT ` + skillCandidateColumns + `
		FROM skill_candidates
		WHERE workspace_id = $1 AND cluster_id = $2
		ORDER BY proposed_at DESC, id DESC
	`
	return st.querySkillCandidates(ctx, query, workspaceID, clusterID)
}

func (st *skillCandidateStore) querySkillCandidates(ctx context.Context, query string, args ...any) ([]domain.SkillCandidate, error) {
	rows, err := st.db.Query(ctx, query, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	candidates := make([]domain.SkillCandidate, 0)
	for rows.Next() {
		c, err := scanSkillCandidate(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return candidates, nil
}

func (st *skillCandidateStore) UpdateStatus(ctx context.Context, workspaceID, id string, status domain.SkillCandidateStatus, reason string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}
	if !domain.ValidSkillCandidateStatus(status) {
		return fmt.Errorf("%w: unknown skill candidate status %q", domain.ErrInvalid, status)
	}

	now := time.Now().UTC()
	const query = `
		UPDATE skill_candidates
		SET status = $3, reason = $4, decided_at = $5, updated_at = $6
		WHERE workspace_id = $1 AND id = $2
	`
	tag, err := st.db.Exec(ctx, query, workspaceID, id, string(status), reason, now, now)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// UpdateDraft rewrites one candidate's draft payload in place — the manual
// retry's success path (extraction-failed cards): the failed row becomes a
// fresh pending proposal. The merged row is validated through the domain
// layer first; Reason clears, DecidedAt clears, and ProposedAt/UpdatedAt are
// stamped app-side (the 000054 house rule). Absent or foreign-workspace
// candidates are domain.ErrNotFound (zero rows affected).
func (st *skillCandidateStore) UpdateDraft(ctx context.Context, workspaceID, id string, draft *domain.SkillCandidate) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}
	if draft == nil {
		return fmt.Errorf("%w: skill candidate draft is required", domain.ErrInvalid)
	}

	// Validate the merged row (the stored scope tuple + the new draft) so
	// the write shape matches Save's discipline.
	current, err := st.Get(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	merged := *current
	merged.SkillName = draft.SkillName
	merged.Status = domain.SkillCandidatePending
	merged.ProposedContent = draft.ProposedContent
	merged.IsEdit = draft.IsEdit
	merged.SupersedesSkillName = draft.SupersedesSkillName
	merged.SupersededContent = draft.SupersededContent
	merged.EvidenceEventIDs = draft.EvidenceEventIDs
	merged.CitedPatternRefs = draft.CitedPatternRefs
	merged.Reason = ""
	if err := merged.Validate(); err != nil {
		return err
	}
	evidence, err := marshalSkillStringList(merged.EvidenceEventIDs)
	if err != nil {
		return err
	}
	cited, err := marshalSkillStringList(merged.CitedPatternRefs)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	const query = `
		UPDATE skill_candidates
		SET skill_name = $3, status = 'pending', proposed_content = $4,
			is_edit = $5, supersedes_skill_name = $6, superseded_content = $7,
			evidence_event_ids = $8, cited_pattern_refs = $9,
			reason = '', proposed_at = $10, decided_at = NULL, updated_at = $10
		WHERE workspace_id = $1 AND id = $2
	`
	tag, err := st.db.Exec(ctx, query, workspaceID, id,
		merged.SkillName, merged.ProposedContent, merged.IsEdit,
		merged.SupersedesSkillName, merged.SupersededContent,
		evidence, cited, now)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (st *skillCandidateStore) CountPending(ctx context.Context, workspaceID string) (int, error) {
	if workspaceID == "" {
		return 0, nil
	}

	const query = `
		SELECT COUNT(*)
		FROM skill_candidates
		WHERE workspace_id = $1 AND status = 'pending'
	`
	var count int
	err := st.db.QueryRow(ctx, query, workspaceID).Scan(&count)
	if err != nil {
		return 0, convertError(err)
	}
	return count, nil
}

func (st *skillCandidateStore) CountApprovedCuratedByAgent(ctx context.Context, workspaceID, agentID string) (int, error) {
	if workspaceID == "" || agentID == "" {
		return 0, nil
	}

	// Live curated = approved + provisional — the statuses whose skill
	// directories sit in the agent's catalog. Disabled rows are archived out
	// of every catalog, so they stop counting against budget (D6).
	const query = `
		SELECT COUNT(*)
		FROM skill_candidates
		WHERE workspace_id = $1 AND agent_id = $2 AND status IN ('approved', 'provisional')
	`
	var count int
	err := st.db.QueryRow(ctx, query, workspaceID, agentID).Scan(&count)
	if err != nil {
		return 0, convertError(err)
	}
	return count, nil
}

// IncrementOutcomeCounts tallies one probation outcome (6.2, D6): the
// helpful/harmful counter and, when used, the use counter move in SQL so a
// concurrent tally never loses an increment; UpdatedAt is stamped app-side
// (the 000054 house rule). Absent or foreign-workspace candidates are
// domain.ErrNotFound (zero rows affected).
func (st *skillCandidateStore) IncrementOutcomeCounts(ctx context.Context, workspaceID, candidateID string, helpful, used bool) error {
	if workspaceID == "" || candidateID == "" {
		return domain.ErrNotFound
	}

	now := time.Now().UTC()
	helpfulInc, harmfulInc, usedInc := 0, 0, 0
	if helpful {
		helpfulInc = 1
	} else {
		harmfulInc = 1
	}
	if used {
		usedInc = 1
	}
	const query = `
		UPDATE skill_candidates
		SET helpful_count = helpful_count + $3,
			harmful_count = harmful_count + $4,
			use_count = use_count + $5,
			updated_at = $6
		WHERE workspace_id = $1 AND id = $2
	`
	tag, err := st.db.Exec(ctx, query, workspaceID, candidateID, helpfulInc, harmfulInc, usedInc, now)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (st *skillCandidateStore) AppendImpactEntry(ctx context.Context, entry *domain.SkillImpactEntry) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}

	const query = `
		INSERT INTO skill_impact_entries (
			id, workspace_id, cluster_id, skill_name, verdict, diff, reason, reviewer, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	if _, err := st.db.Exec(ctx, query,
		entry.ID, entry.WorkspaceID, entry.ClusterID, entry.SkillName,
		string(entry.Verdict), entry.Diff, entry.Reason, entry.Reviewer,
		entry.CreatedAt); err != nil {
		return convertError(err)
	}
	return nil
}

func (st *skillCandidateStore) ListImpactEntries(ctx context.Context, workspaceID string, since time.Time) ([]domain.SkillImpactEntry, error) {
	if workspaceID == "" {
		return []domain.SkillImpactEntry{}, nil
	}

	query := `
		SELECT ` + skillImpactColumns + `
		FROM skill_impact_entries
		WHERE workspace_id = $1`
	args := []any{workspaceID}
	if !since.IsZero() {
		query += ` AND created_at >= $2`
		args = append(args, since)
	}
	query += `
		ORDER BY created_at ASC, id ASC
	`
	rows, err := st.db.Query(ctx, query, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	entries := make([]domain.SkillImpactEntry, 0)
	for rows.Next() {
		e, err := scanSkillImpactEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return entries, nil
}

func (st *skillCandidateStore) LatestImpactEntryForCluster(ctx context.Context, workspaceID, clusterID string) (*domain.SkillImpactEntry, error) {
	if workspaceID == "" || clusterID == "" {
		return nil, nil
	}

	const query = `
		SELECT ` + skillImpactColumns + `
		FROM skill_impact_entries
		WHERE workspace_id = $1 AND cluster_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`
	e, err := scanSkillImpactEntry(st.db.QueryRow(ctx, query, workspaceID, clusterID))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

const skillClusterRunColumns = `id, workspace_id, agent_id, cluster_id, session_id, turn_id,
	run_status, origin, qualifying, tool_calls, distinct_tools, recoveries, error_results,
	total_tool_latency_ms, window_end_event_id, indexed_at`

func scanSkillClusterRun(row pgx.Row) (*domain.SkillClusterRun, error) {
	var r domain.SkillClusterRun
	var status string
	err := row.Scan(&r.ID, &r.WorkspaceID, &r.AgentID, &r.ClusterID, &r.SessionID, &r.TurnID,
		&status, &r.Origin, &r.Qualifying, &r.ToolCalls, &r.DistinctTools, &r.Recoveries,
		&r.ErrorResults, &r.TotalToolLatencyMS, &r.WindowEndEventID, &r.IndexedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, convertError(err)
	}
	r.RunStatus = domain.SkillClusterRunStatus(status)
	return &r, nil
}

// IndexClusterRun upserts one run's cluster membership over the
// (workspace, session, turn) unique key: re-ingesting the same run rewrites
// its row in place and keeps the store-assigned id stable (RETURNING id),
// so the evidence pool never doubles-counts a run.
func (st *skillCandidateStore) IndexClusterRun(ctx context.Context, run *domain.SkillClusterRun) error {
	if err := run.Validate(); err != nil {
		return err
	}
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	if run.IndexedAt.IsZero() {
		run.IndexedAt = time.Now().UTC()
	}

	const query = `
		INSERT INTO skill_cluster_runs (
			id, workspace_id, agent_id, cluster_id, session_id, turn_id,
			run_status, origin, qualifying, tool_calls, distinct_tools, recoveries,
			error_results, total_tool_latency_ms, window_end_event_id, indexed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (workspace_id, session_id, turn_id) DO UPDATE SET
			agent_id = EXCLUDED.agent_id,
			cluster_id = EXCLUDED.cluster_id,
			run_status = EXCLUDED.run_status,
			origin = EXCLUDED.origin,
			qualifying = EXCLUDED.qualifying,
			tool_calls = EXCLUDED.tool_calls,
			distinct_tools = EXCLUDED.distinct_tools,
			recoveries = EXCLUDED.recoveries,
			error_results = EXCLUDED.error_results,
			total_tool_latency_ms = EXCLUDED.total_tool_latency_ms,
			window_end_event_id = EXCLUDED.window_end_event_id,
			indexed_at = EXCLUDED.indexed_at
		RETURNING id
	`
	var id string
	if err := st.db.QueryRow(ctx, query,
		run.ID, run.WorkspaceID, run.AgentID, run.ClusterID, run.SessionID, run.TurnID,
		string(run.RunStatus), run.Origin, run.Qualifying, run.ToolCalls, run.DistinctTools,
		run.Recoveries, run.ErrorResults, run.TotalToolLatencyMS, run.WindowEndEventID,
		run.IndexedAt).Scan(&id); err != nil {
		return convertError(err)
	}
	run.ID = id
	return nil
}

func (st *skillCandidateStore) CountQualifyingByCluster(ctx context.Context, workspaceID, clusterID string) (int, error) {
	if workspaceID == "" || clusterID == "" {
		return 0, nil
	}

	const query = `
		SELECT COUNT(*)
		FROM skill_cluster_runs
		WHERE workspace_id = $1 AND cluster_id = $2 AND qualifying
	`
	var count int
	err := st.db.QueryRow(ctx, query, workspaceID, clusterID).Scan(&count)
	if err != nil {
		return 0, convertError(err)
	}
	return count, nil
}

func (st *skillCandidateStore) ListClusterRunsByCluster(ctx context.Context, workspaceID, clusterID string) ([]domain.SkillClusterRun, error) {
	if workspaceID == "" || clusterID == "" {
		return []domain.SkillClusterRun{}, nil
	}

	const query = `
		SELECT ` + skillClusterRunColumns + `
		FROM skill_cluster_runs
		WHERE workspace_id = $1 AND cluster_id = $2
		ORDER BY indexed_at ASC, id ASC
	`
	rows, err := st.db.Query(ctx, query, workspaceID, clusterID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	runs := make([]domain.SkillClusterRun, 0)
	for rows.Next() {
		r, err := scanSkillClusterRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return runs, nil
}

func (st *skillCandidateStore) ListClusterIDs(ctx context.Context, workspaceID string) ([]string, error) {
	if workspaceID == "" {
		return []string{}, nil
	}

	const query = `
		SELECT DISTINCT cluster_id
		FROM skill_cluster_runs
		WHERE workspace_id = $1
	`
	rows, err := st.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, convertError(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return ids, nil
}
