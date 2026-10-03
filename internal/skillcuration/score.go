package skillcuration

import (
	"sort"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/ingest"
)

// The nightly budget (add-skill-curation-from-traces 3.2, design D2 "soft
// score ranks the nightly budget"): after the qualifier's hard gates, the
// cycle spends at most cfg.NightlyBudgetK side-calls per tick on the
// qualified runs that look most worth distilling. The score is a pure,
// deterministic function of the membership row's stored tally — the same
// input always produces the same score and the same order — so the budget
// is auditable and reproducible, never a coin flip.

// Component weights (sum to 1). The recovery shape dominates: a procedure
// that struggled and recovered is exactly the knowledge worth encoding.
const (
	weightRecovery  = 0.40
	weightEffort    = 0.20
	weightDiversity = 0.15
	weightGrowth    = 0.15
	weightOrigin    = 0.10
)

// Saturation points for the ratio components: per-call effort saturates at
// five seconds per tool call, total effort at one minute of tool time, and
// tool diversity at six distinct tools (the gate minimum is two; a run
// orchestrating six is already a rich procedure).
const (
	effortPerCallSaturationMS = int64(5000)
	effortTotalSaturationMS   = int64(60000)
	diversitySaturation       = 6
)

// QualifiedRun is one gate-passing run as the nightly budget sees it: the
// cluster membership row plus its cluster's qualifying size (within the
// selection's input set) and the soft score that ranked it.
type QualifiedRun struct {
	Run domain.SkillClusterRun
	// ClusterID mirrors Run.ClusterID — the field the proposer keys on.
	ClusterID string
	// ClusterQualifying is the number of qualifying runs in the run's
	// cluster within the input set (the growth signal that scored it).
	ClusterQualifying int
	// Score is the deterministic soft score in [0, 1].
	Score float64
}

// SelectForCycle ranks the cycle's qualified runs and caps the result at
// budget (cfg.NightlyBudgetK) — the side-calls one cycle may spend. The
// cluster-growth signal derives from the input set itself: callers decide
// the window (all qualifying runs for a consolidation cycle, new-since-last
// for an incremental one) and the score stays a function of exactly what
// was handed in. Non-qualifying rows in the input are skipped — the budget
// funds gate-passing evidence only. budget <= 0 selects nothing; the result
// is never nil.
//
// One family monopolizing a cycle is structurally bounded: the proposer
// drafts at most one create-or-edit per cluster per cycle (design D4), so
// spending several budget slots on one cluster's runs feeds that single
// draft, never several.
func SelectForCycle(qualified []domain.SkillClusterRun, budget int) []QualifiedRun {
	selected := make([]QualifiedRun, 0, min(len(qualified), max(budget, 0)))
	if budget <= 0 {
		return selected
	}

	clusterQualifying := map[string]int{}
	for _, run := range qualified {
		if run.Qualifying {
			clusterQualifying[run.ClusterID]++
		}
	}

	for _, run := range qualified {
		if !run.Qualifying {
			continue
		}
		selected = append(selected, QualifiedRun{
			Run:               run,
			ClusterID:         run.ClusterID,
			ClusterQualifying: clusterQualifying[run.ClusterID],
			Score:             ScoreRun(run, clusterQualifying[run.ClusterID]),
		})
	}

	// Deterministic total order: score desc, then oldest evidence first,
	// then id — equal-input runs can never shuffle between cycles.
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].Score != selected[j].Score {
			return selected[i].Score > selected[j].Score
		}
		if !selected[i].Run.IndexedAt.Equal(selected[j].Run.IndexedAt) {
			return selected[i].Run.IndexedAt.Before(selected[j].Run.IndexedAt)
		}
		return selected[i].Run.ID < selected[j].Run.ID
	})
	if len(selected) > budget {
		selected = selected[:budget]
	}
	return selected
}

// ScoreRun computes the deterministic soft score in [0, 1] for one
// qualifying run: the weighted sum of the recovery shape, the latency
// effort, the tool diversity, the cluster growth, and the origin weight.
// clusterQualifying counts the run's own cluster's qualifying runs within
// the caller's selection window (the run itself included).
func ScoreRun(run domain.SkillClusterRun, clusterQualifying int) float64 {
	score := weightRecovery*recoveryScore(run.Recoveries) +
		weightEffort*effortScore(run.TotalToolLatencyMS, run.ToolCalls) +
		weightDiversity*diversityScore(run.DistinctTools) +
		weightGrowth*growthScore(clusterQualifying) +
		weightOrigin*originScore(run.Origin)
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

// recoveryScore values the recovery shape: the sweet spot is 2–3 recoveries
// — enough struggle to encode a real workaround, not a flailing run. One
// recovery (the gate minimum) ranks mid; past ~5 the run looks like
// thrashing and the score decays toward (never below) a small floor — a
// recovered-but-catastrophic run still taught the agent something.
func recoveryScore(recoveries int) float64 {
	switch {
	case recoveries <= 0:
		return 0
	case recoveries == 1:
		return 0.5
	case recoveries <= 3:
		return 1.0
	case recoveries == 4:
		return 0.75
	case recoveries == 5:
		return 0.5
	default:
		score := 0.5 - 0.1*float64(recoveries-5)
		if score < 0.1 {
			return 0.1
		}
		return score
	}
}

// effortScore values the work the run put in — a procedure that held tools
// busy encodes more than a two-second drive-by. Half the component is the
// per-call effort (average tool latency, saturating at five seconds), half
// the total effort (summed tool latency, saturating at one minute). A
// zero-latency tally (timestamps unavailable) scores zero effort — the
// other components still rank the run.
func effortScore(totalLatencyMS int64, toolCalls int) float64 {
	if toolCalls <= 0 || totalLatencyMS <= 0 {
		return 0
	}
	perCall := float64(totalLatencyMS) / float64(toolCalls) / float64(effortPerCallSaturationMS)
	total := float64(totalLatencyMS) / float64(effortTotalSaturationMS)
	return clamp01(0.5*perCall + 0.5*total)
}

// diversityScore values orchestration breadth: distinct tools over the
// six-tool saturation point, capped at one.
func diversityScore(distinctTools int) float64 {
	if distinctTools <= 0 {
		return 0
	}
	return clamp01(float64(distinctTools) / float64(diversitySaturation))
}

// growthScore prefers runs whose family is already converging: a cluster
// holding prior qualifying runs is closer to (or past) the proposal gate,
// so consolidating it outranks opening a new family. The run's own
// membership counts one; each additional qualifying member adds a quarter
// of the component, saturating at four siblings.
func growthScore(clusterQualifying int) float64 {
	return clamp01(float64(clusterQualifying-1) / 4.0)
}

// originScore down-weights unattended runs: scheduler and heartbeat origins
// have no human steering the procedure, so their evidence ranks below an
// attended run's — but never to zero (a scheduled backup-and-verify loop is
// still a procedure worth encoding). Every other origin (user, channel,
// telegram, empty = direct chat) is fully weighted.
func originScore(origin string) float64 {
	switch strings.TrimSpace(origin) {
	case ingest.OriginScheduler, ingest.OriginHeartbeat:
		return 0.4
	default:
		return 1.0
	}
}

// clamp01 clamps v into [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
