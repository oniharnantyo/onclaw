package scoreboard

import "fmt"

// GateOverall is the special gate key for the cross-lane overall aggregate;
// all other gate keys are lane ids.
const GateOverall = "overall"

// Gate is one pre-registered gate (design D4: registered before the
// after-run). Rates are fractions in [0,1]. The after-run pass rate must not
// fall more than MaxRegression below the baseline rate, and — when
// MinImprovement is set (> 0) — must rise at least MinImprovement above it.
// Zero values mean unset. The default gate for a lane with no registered
// gate is the zero Gate: any regression fails, no improvement required.
type Gate struct {
	MaxRegression  float64 `json:"max_regression"`
	MinImprovement float64 `json:"min_improvement"`
}

// Verdict values on a Report row.
const (
	VerdictPass = "pass"
	VerdictFail = "fail"
)

// Row is one compare line: a lane, or the overall aggregate (Lane ==
// GateOverall).
type Row struct {
	Lane         string  `json:"lane"`
	BaselineRate float64 `json:"baseline_rate"`
	RunRate      float64 `json:"run_rate"`
	Delta        float64 `json:"delta"` // RunRate - BaselineRate
	Verdict      string  `json:"verdict"`
	Reason       string  `json:"reason"`
}

// Report is the compare output: per-lane rows in deterministic order
// (canonical lanes first, unknown lanes alphabetically, overall row last)
// plus the headline verdict. Pass is true only when every row passes.
type Report struct {
	Pass bool  `json:"pass"`
	Rows []Row `json:"rows"`
}

// Baseline is a committed baseline artifact: the previous run's scoreboard
// plus the gates pre-registered for the next cycle. Gates are keyed by lane
// id, plus GateOverall for the overall aggregate.
type Baseline struct {
	SchemaVersion int             `json:"schema_version"`
	SuiteVersion  string          `json:"suite_version"`
	OnclawGitSHA  string          `json:"onclaw_git_sha"`
	ModelPin      string          `json:"model_pin"`
	Gates         map[string]Gate `json:"gates"`
	Scoreboard    Scoreboard      `json:"scoreboard"`
}

// GateFor returns the gate registered for lane, or the default zero Gate
// (any regression fails, no improvement required) when none is registered.
func (b Baseline) GateFor(lane string) Gate {
	if g, ok := b.Gates[lane]; ok {
		return g
	}
	return Gate{}
}

// Compare diffs a run's scoreboard against a committed baseline and
// evaluates the pre-registered gates. Both sides' rates are recomputed from
// their TaskResults so stored aggregates can never disagree with the rows
// they summarize.
//
// Semantics per lane: the lane fails if run_rate < baseline_rate -
// max_regression, or (min_improvement > 0 and run_rate < baseline_rate +
// min_improvement). A lane present in the baseline scoreboard but absent
// from the run fails unconditionally (suite drift); a lane present only in
// the run is evaluated against a zero baseline rate with its registered (or
// default) gate. The overall row uses the GateOverall gate the same way.
func Compare(run Scoreboard, baseline Baseline) Report {
	runRates := laneRates(run.Tasks)
	baseRates := laneRates(baseline.Scoreboard.Tasks)

	laneSet := make(map[string]bool, len(runRates)+len(baseRates))
	for lane := range runRates {
		laneSet[lane] = true
	}
	for lane := range baseRates {
		laneSet[lane] = true
	}
	laneOrder := make([]string, 0, len(laneSet))
	for lane := range laneSet {
		laneOrder = append(laneOrder, lane)
	}

	report := Report{Rows: []Row{}}
	pass := true

	for _, lane := range orderedLanes(laneOrder) {
		baseRate, inBase := baseRates[lane]
		runRate, inRun := runRates[lane]

		var row Row
		switch {
		case inBase && !inRun:
			row = Row{
				Lane:         lane,
				BaselineRate: baseRate,
				RunRate:      0,
				Delta:        -baseRate,
				Verdict:      VerdictFail,
				Reason:       "lane present in the baseline scoreboard but absent from the run (suite drift)",
			}
		case !inBase:
			row = evaluate(lane, 0, runRate, baseline.GateFor(lane))
			if row.Verdict == VerdictPass {
				row.Reason = "new lane with no baseline rate; " + row.Reason
			}
		default:
			row = evaluate(lane, baseRate, runRate, baseline.GateFor(lane))
		}
		if row.Verdict == VerdictFail {
			pass = false
		}
		report.Rows = append(report.Rows, row)
	}

	_, runOverall := BuildScoreboard(run.Tasks)
	_, baseOverall := BuildScoreboard(baseline.Scoreboard.Tasks)
	overallRow := evaluate(GateOverall, baseOverall.PassRate, runOverall.PassRate, baseline.GateFor(GateOverall))
	if overallRow.Verdict == VerdictFail {
		pass = false
	}
	report.Rows = append(report.Rows, overallRow)

	report.Pass = pass
	return report
}

// evaluate applies one gate to one rate pair.
func evaluate(lane string, baselineRate, runRate float64, g Gate) Row {
	row := Row{
		Lane:         lane,
		BaselineRate: baselineRate,
		RunRate:      runRate,
		Delta:        runRate - baselineRate,
	}
	floor := baselineRate - g.MaxRegression
	target := baselineRate + g.MinImprovement
	switch {
	case runRate < floor:
		row.Verdict = VerdictFail
		row.Reason = fmt.Sprintf("run_rate %.4f is below the gate floor baseline %.4f - max_regression %.4f = %.4f", runRate, baselineRate, g.MaxRegression, floor)
	case g.MinImprovement > 0 && runRate < target:
		row.Verdict = VerdictFail
		row.Reason = fmt.Sprintf("run_rate %.4f is below the required improvement baseline %.4f + min_improvement %.4f = %.4f", runRate, baselineRate, g.MinImprovement, target)
	default:
		row.Verdict = VerdictPass
		row.Reason = fmt.Sprintf("run_rate %.4f within gate: baseline %.4f, max_regression %.4f, min_improvement %.4f", runRate, baselineRate, g.MaxRegression, g.MinImprovement)
	}
	return row
}

func laneRates(tasks []TaskResult) map[string]float64 {
	lanes, _ := BuildScoreboard(tasks)
	rates := make(map[string]float64, len(lanes))
	for _, lane := range lanes {
		rates[lane.Lane] = lane.PassRate
	}
	return rates
}
