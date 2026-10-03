// Package scoreboard defines the bench scoreboard JSON schema (design D8),
// the per-lane aggregation over task results, and the baseline +
// pre-registered-gate comparison used to gate one harness delta per cycle
// (design D4).
package scoreboard

import (
	"sort"
	"time"

	"github.com/oniharnantyo/onclaw-bench/suite"
)

// SchemaVersion is the current scoreboard/baseline schema version, written
// into every artifact.
const SchemaVersion = 1

// TaskResult status values. infra-error marks an infrastructure failure
// (network / 5xx / timeout): it is excluded from pass-rate denominators and
// counted separately, never as a task failure (design Risks).
const (
	StatusPass       = "pass"
	StatusFail       = "fail"
	StatusInfraError = "infra-error"
)

// CheckResult is one check's outcome for a task execution.
type CheckResult struct {
	Kind   string `json:"kind"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// TaskResult is one task execution: one (task, seed) pair.
type TaskResult struct {
	TaskID     string        `json:"task_id"`
	Lane       string        `json:"lane"`
	Seed       int           `json:"seed"`
	Status     string        `json:"status"` // StatusPass | StatusFail | StatusInfraError
	Checks     []CheckResult `json:"checks"`
	Answer     string        `json:"answer,omitempty"`
	ToolCalls  int           `json:"tool_calls"`
	DurationMS int64         `json:"duration_ms"`
	Error      string        `json:"error,omitempty"` // infra-error cause, empty otherwise
}

// LaneAggregate folds one lane's task results: TasksRun counts pass+fail
// only (infra-errors are excluded), and PassRate is Passed/TasksRun as a
// fraction in [0,1] (0 when nothing ran).
type LaneAggregate struct {
	Lane     string  `json:"lane"`
	TasksRun int     `json:"tasks_run"`
	Passed   int     `json:"passed"`
	PassRate float64 `json:"pass_rate"`
}

// Overall is the cross-lane aggregate, computed the same way as a lane.
type Overall struct {
	TasksRun int     `json:"tasks_run"`
	Passed   int     `json:"passed"`
	PassRate float64 `json:"pass_rate"`
}

// Scoreboard is one bench run's artifact: per-task rows, per-lane
// aggregates, and the run-level identity (suite version, OnClaw git sha,
// pinned model). One scoreboard JSON is committed per run and diffed
// against the previous baseline.
type Scoreboard struct {
	SchemaVersion int             `json:"schema_version"`
	RunID         string          `json:"run_id"`
	SuiteVersion  string          `json:"suite_version"`
	OnclawGitSHA  string          `json:"onclaw_git_sha"`
	ModelPin      string          `json:"model_pin"`
	BaseURL       string          `json:"base_url"`
	StartedAt     time.Time       `json:"started_at"`
	FinishedAt    time.Time       `json:"finished_at"`
	Seeds         int             `json:"seeds"`
	Tasks         []TaskResult    `json:"tasks"`
	Lanes         []LaneAggregate `json:"lanes"`
	Overall       Overall         `json:"overall"`
	InfraErrors   int             `json:"infra_errors"`
}

// BuildScoreboard folds task results into per-lane aggregates and the
// overall aggregate. It is a pure function: the runner calls it after a run
// and compare recomputes it for both sides so stored aggregates can never
// disagree with the rows they summarize.
//
// A task counts toward its lane only when Status is StatusPass or
// StatusFail; StatusInfraError (and any unknown status) is excluded from
// both the denominator and the numerator. Lanes are returned in the
// deterministic canonical order — the six canonical lane ids in fixed order,
// then any other lane ids alphabetically — and every lane that appears in
// the results gets a row, even an all-infra lane with a zero rate.
func BuildScoreboard(tasks []TaskResult) (lanes []LaneAggregate, overall Overall) {
	type tally struct{ ran, passed int }
	byLane := map[string]*tally{}
	order := make([]string, 0, 8)

	for _, t := range tasks {
		agg := byLane[t.Lane]
		if agg == nil {
			agg = &tally{}
			byLane[t.Lane] = agg
			order = append(order, t.Lane)
		}
		switch t.Status {
		case StatusPass:
			agg.ran++
			agg.passed++
		case StatusFail:
			agg.ran++
		}
	}

	lanes = make([]LaneAggregate, 0, len(order))
	for _, lane := range orderedLanes(order) {
		agg := byLane[lane]
		lanes = append(lanes, LaneAggregate{
			Lane:     lane,
			TasksRun: agg.ran,
			Passed:   agg.passed,
			PassRate: passRate(agg.passed, agg.ran),
		})
	}

	for _, agg := range byLane {
		overall.TasksRun += agg.ran
		overall.Passed += agg.passed
	}
	overall.PassRate = passRate(overall.Passed, overall.TasksRun)
	return lanes, overall
}

func passRate(passed, ran int) float64 {
	if ran == 0 {
		return 0
	}
	return float64(passed) / float64(ran)
}

// orderedLanes returns the distinct lanes in deterministic order: canonical
// lane ids in fixed order first, then any other lane ids alphabetically.
func orderedLanes(lanes []string) []string {
	present := make(map[string]bool, len(lanes))
	var rest []string
	for _, lane := range lanes {
		if present[lane] {
			continue
		}
		present[lane] = true
		if !suite.IsCanonicalLane(lane) {
			rest = append(rest, lane)
		}
	}
	out := make([]string, 0, len(lanes))
	for _, lane := range suite.CanonicalLanes() {
		if present[lane] {
			out = append(out, lane)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}
