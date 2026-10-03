package scoreboard

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"
)

func res(taskID, lane string, seed int, status string) TaskResult {
	return TaskResult{TaskID: taskID, Lane: lane, Seed: seed, Status: status, Checks: []CheckResult{}}
}

func TestBuildScoreboardAggregatesAndOrdering(t *testing.T) {
	tasks := []TaskResult{
		res("m1", "memory", 1, StatusPass),
		res("m1", "memory", 2, StatusFail),
		res("m1", "memory", 3, StatusInfraError), // excluded from the lane
		res("c1", "compaction", 1, StatusPass),
		res("z1", "zeta-custom", 1, StatusPass),  // unknown lane
		res("a1", "alpha-custom", 1, StatusFail), // unknown lane
	}
	lanes, overall := BuildScoreboard(tasks)

	wantLanes := []LaneAggregate{
		{Lane: "compaction", TasksRun: 1, Passed: 1, PassRate: 1},
		{Lane: "memory", TasksRun: 2, Passed: 1, PassRate: 0.5},
		{Lane: "alpha-custom", TasksRun: 1, Passed: 0, PassRate: 0},
		{Lane: "zeta-custom", TasksRun: 1, Passed: 1, PassRate: 1},
	}
	if !reflect.DeepEqual(lanes, wantLanes) {
		t.Fatalf("lanes =\n%+v\nwant\n%+v", lanes, wantLanes)
	}
	wantOverall := Overall{TasksRun: 5, Passed: 3, PassRate: 0.6}
	if overall != wantOverall {
		t.Fatalf("overall = %+v, want %+v", overall, wantOverall)
	}
}

func TestBuildScoreboardEmpty(t *testing.T) {
	lanes, overall := BuildScoreboard(nil)
	if lanes == nil || len(lanes) != 0 {
		t.Fatalf("lanes = %#v, want empty non-nil slice", lanes)
	}
	if overall != (Overall{}) {
		t.Fatalf("overall = %+v, want zero", overall)
	}
}

func TestBuildScoreboardInfraOnlyLane(t *testing.T) {
	lanes, overall := BuildScoreboard([]TaskResult{
		res("x1", "anchor", 1, StatusInfraError),
		res("x1", "anchor", 2, StatusInfraError),
	})
	if len(lanes) != 1 {
		t.Fatalf("expected the infra-only lane to still get a row, got %+v", lanes)
	}
	if lanes[0].Lane != "anchor" || lanes[0].TasksRun != 0 || lanes[0].Passed != 0 || lanes[0].PassRate != 0 {
		t.Fatalf("infra-only lane row = %+v, want zero rates", lanes[0])
	}
	if overall != (Overall{}) {
		t.Fatalf("overall = %+v, want zero", overall)
	}
}

// scoreboardFixture builds a baseline run (3 lanes x 3 tasks) and an after
// run with the same suite, plus a Baseline carrying pre-registered gates for
// the compare tests.
func scoreboardFixture() (Scoreboard, Baseline) {
	baseline := Scoreboard{
		SchemaVersion: SchemaVersion,
		RunID:         "run-baseline",
		SuiteVersion:  "2026.10.0",
		OnclawGitSHA:  "aaa1111",
		ModelPin:      "glm-5.3-flash",
		BaseURL:       "http://127.0.0.1:8080",
		StartedAt:     time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
		FinishedAt:    time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC),
		Seeds:         3,
		Tasks: []TaskResult{
			res("c1", "compaction", 1, StatusPass),
			res("c2", "compaction", 1, StatusPass),
			res("c3", "compaction", 1, StatusFail),
			res("t1", "tool-error", 1, StatusPass),
			res("t2", "tool-error", 1, StatusFail),
			res("t3", "tool-error", 1, StatusFail),
			res("m1", "memory", 1, StatusPass),
			res("m2", "memory", 1, StatusPass),
			res("m3", "memory", 1, StatusPass),
		},
		InfraErrors: 0,
	}
	run := Scoreboard{
		SchemaVersion: SchemaVersion,
		RunID:         "run-after",
		SuiteVersion:  "2026.10.0",
		OnclawGitSHA:  "bbb2222",
		ModelPin:      "glm-5.3-flash",
		BaseURL:       "http://127.0.0.1:8080",
		StartedAt:     time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC),
		FinishedAt:    time.Date(2026, 10, 3, 9, 35, 0, 0, time.UTC),
		Seeds:         3,
		Tasks: []TaskResult{
			res("c1", "compaction", 1, StatusPass),
			res("c2", "compaction", 1, StatusPass),
			res("c3", "compaction", 1, StatusFail),
			res("t1", "tool-error", 1, StatusPass),
			res("t2", "tool-error", 1, StatusPass),
			res("t3", "tool-error", 1, StatusPass),
			res("m1", "memory", 1, StatusPass),
			res("m2", "memory", 1, StatusPass),
			res("m3", "memory", 1, StatusFail),
		},
		InfraErrors: 0,
	}
	baselineLanes, baselineOverall := BuildScoreboard(baseline.Tasks)
	baseline.Lanes, baseline.Overall = baselineLanes, baselineOverall
	runLanes, runOverall := BuildScoreboard(run.Tasks)
	run.Lanes, run.Overall = runLanes, runOverall

	base := Baseline{
		SchemaVersion: SchemaVersion,
		SuiteVersion:  "2026.10.0",
		OnclawGitSHA:  "aaa1111",
		ModelPin:      "glm-5.3-flash",
		Gates: map[string]Gate{
			"compaction": {MaxRegression: 0.15},
			"tool-error": {MinImprovement: 0.2},
			"memory":     {MaxRegression: 0.1},
		},
		Scoreboard: baseline,
	}
	return run, base
}

func rowByLane(t *testing.T, r Report, lane string) Row {
	t.Helper()
	for _, row := range r.Rows {
		if row.Lane == lane {
			return row
		}
	}
	t.Fatalf("report has no row for lane %q: %+v", lane, r.Rows)
	return Row{}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestCompareDeltasAndVerdicts(t *testing.T) {
	run, baseline := scoreboardFixture()
	report := Compare(run, baseline)

	if len(report.Rows) != 4 { // 3 lanes + overall, canonical order
		t.Fatalf("expected 4 rows, got %d: %+v", len(report.Rows), report.Rows)
	}
	wantOrder := []string{"compaction", "tool-error", "memory", GateOverall}
	for i, lane := range wantOrder {
		if report.Rows[i].Lane != lane {
			t.Fatalf("row %d = %q, want %q", i, report.Rows[i].Lane, lane)
		}
	}

	// compaction: 2/3 -> 2/3, within max_regression 0.15.
	c := rowByLane(t, report, "compaction")
	if c.Verdict != VerdictPass || !approx(c.Delta, 0) || !approx(c.BaselineRate, 2.0/3.0) || !approx(c.RunRate, 2.0/3.0) {
		t.Fatalf("compaction row = %+v", c)
	}
	// tool-error: 1/3 -> 3/3, min_improvement 0.2 requires >= 0.5333.
	te := rowByLane(t, report, "tool-error")
	if te.Verdict != VerdictPass || !approx(te.Delta, 2.0/3.0) {
		t.Fatalf("tool-error row = %+v", te)
	}
	// memory: 3/3 -> 2/3, max_regression 0.1 puts the floor at 0.9.
	m := rowByLane(t, report, "memory")
	if m.Verdict != VerdictFail || !approx(m.Delta, -1.0/3.0) {
		t.Fatalf("memory row = %+v", m)
	}
	// Overall: 6/9 -> 7/9, default gate (not registered).
	o := rowByLane(t, report, GateOverall)
	if o.Verdict != VerdictPass || !approx(o.BaselineRate, 2.0/3.0) || !approx(o.RunRate, 7.0/9.0) {
		t.Fatalf("overall row = %+v", o)
	}
	if report.Pass {
		t.Fatal("report.Pass = true, want false (memory failed)")
	}
}

func TestCompareDefaultGate(t *testing.T) {
	baseline := Scoreboard{Tasks: []TaskResult{
		res("m1", "memory", 1, StatusPass),
		res("m2", "memory", 1, StatusFail),
	}}
	base := Baseline{Scoreboard: baseline} // no gates registered at all

	failRun := Scoreboard{Tasks: []TaskResult{
		res("m1", "memory", 1, StatusPass),
		res("m2", "memory", 1, StatusFail),
		res("m3", "memory", 1, StatusFail),
		res("m4", "memory", 1, StatusFail),
	}}
	report := Compare(failRun, base)
	row := rowByLane(t, report, "memory")
	if row.Verdict != VerdictFail {
		t.Fatalf("1/4 vs baseline 1/2 must fail the default gate: %+v", row)
	}
	if report.Pass {
		t.Fatal("report.Pass = true, want false")
	}

	passRun := Scoreboard{Tasks: []TaskResult{
		res("m1", "memory", 1, StatusPass),
		res("m2", "memory", 1, StatusFail),
	}}
	report = Compare(passRun, base)
	if row := rowByLane(t, report, "memory"); row.Verdict != VerdictPass {
		t.Fatalf("1/2 vs baseline 1/2 must pass the default gate (run == floor): %+v", row)
	}
	if !report.Pass {
		t.Fatal("report.Pass = false, want true")
	}
}

func TestCompareMissingLaneFails(t *testing.T) {
	baseline := Scoreboard{Tasks: []TaskResult{
		res("m1", "memory", 1, StatusPass),
		res("d1", "delegation", 1, StatusPass),
	}}
	base := Baseline{Scoreboard: baseline}
	run := Scoreboard{Tasks: []TaskResult{
		res("m1", "memory", 1, StatusPass),
	}}
	report := Compare(run, base)

	row := rowByLane(t, report, "delegation")
	if row.Verdict != VerdictFail {
		t.Fatalf("lane present in baseline but absent from run must fail: %+v", row)
	}
	if row.RunRate != 0 || !approx(row.BaselineRate, 1) || !approx(row.Delta, -1) {
		t.Fatalf("missing-lane row = %+v", row)
	}
	if report.Pass {
		t.Fatal("report.Pass = true, want false (suite drift)")
	}
}

func TestCompareNewLanePassesByDefault(t *testing.T) {
	baseline := Scoreboard{Tasks: []TaskResult{
		res("m1", "memory", 1, StatusPass),
	}}
	base := Baseline{Scoreboard: baseline}
	run := Scoreboard{Tasks: []TaskResult{
		res("m1", "memory", 1, StatusPass),
		res("a1", "anchor", 1, StatusPass),
	}}
	report := Compare(run, base)
	row := rowByLane(t, report, "anchor")
	if row.Verdict != VerdictPass || row.BaselineRate != 0 || !approx(row.RunRate, 1) {
		t.Fatalf("new-lane row = %+v, want pass with zero baseline", row)
	}
	if !report.Pass {
		t.Fatal("report.Pass = false, want true")
	}
}

func TestCompareMinImprovement(t *testing.T) {
	baseline := Scoreboard{Tasks: []TaskResult{
		res("m1", "memory", 1, StatusPass),
		res("m2", "memory", 1, StatusFail),
	}}
	base := Baseline{
		Scoreboard: baseline,
		Gates:      map[string]Gate{"memory": {MinImprovement: 0.25}},
	}

	short := Scoreboard{Tasks: []TaskResult{
		res("m1", "memory", 1, StatusPass),
		res("m2", "memory", 1, StatusPass),
		res("m3", "memory", 1, StatusFail),
		res("m4", "memory", 1, StatusPass),
		res("m5", "memory", 1, StatusFail),
	}} // 3/5 = 0.6 < 0.5 + 0.25
	if row := rowByLane(t, Compare(short, base), "memory"); row.Verdict != VerdictFail {
		t.Fatalf("run 0.6 must fail min_improvement 0.25 over baseline 0.5: %+v", row)
	}

	exact := Scoreboard{Tasks: []TaskResult{
		res("m1", "memory", 1, StatusPass),
		res("m2", "memory", 1, StatusPass),
	}} // 2/2 = 1.0 >= 0.75
	if row := rowByLane(t, Compare(exact, base), "memory"); row.Verdict != VerdictPass {
		t.Fatalf("run 1.0 must pass min_improvement 0.25 over baseline 0.5: %+v", row)
	}
}

func TestCompareOverallGateCatchesDilution(t *testing.T) {
	// Every lane row passes, but a new zero-rate anchor lane dilutes the
	// overall below the pre-registered overall gate.
	baseline := Scoreboard{Tasks: []TaskResult{
		res("m1", "memory", 1, StatusPass),
		res("m2", "memory", 1, StatusFail),
		res("t1", "tool-error", 1, StatusPass),
		res("t2", "tool-error", 1, StatusPass),
	}}
	base := Baseline{
		Scoreboard: baseline, // overall 3/4 = 0.75
		Gates: map[string]Gate{
			"overall":    {MaxRegression: 0.1}, // floor 0.65
			"memory":     {MaxRegression: 0.1},
			"tool-error": {MaxRegression: 0.1},
		},
	}
	run := Scoreboard{Tasks: []TaskResult{
		res("m1", "memory", 1, StatusPass),
		res("m2", "memory", 1, StatusFail),
		res("t1", "tool-error", 1, StatusPass),
		res("t2", "tool-error", 1, StatusPass),
		res("a1", "anchor", 1, StatusFail), // new lane, 0/1
	}}
	report := Compare(run, base)

	for _, lane := range []string{"memory", "tool-error", "anchor"} {
		if row := rowByLane(t, report, lane); row.Verdict != VerdictPass {
			t.Fatalf("lane %q should pass: %+v", lane, row)
		}
	}
	if row := rowByLane(t, report, GateOverall); row.Verdict != VerdictFail {
		t.Fatalf("overall row = %+v, want fail (3/5 = 0.6 < floor 0.65)", row)
	}
	if report.Pass {
		t.Fatal("report.Pass = true, want false (overall gate)")
	}
}

func TestGateForDefaults(t *testing.T) {
	var b Baseline // nil Gates map
	if g := b.GateFor("memory"); g != (Gate{}) {
		t.Fatalf("GateFor on nil map = %+v, want zero gate", g)
	}
	b.Gates = map[string]Gate{"overall": {MaxRegression: 0.05, MinImprovement: 0.1}}
	if g := b.GateFor("memory"); g != (Gate{}) {
		t.Fatalf("unregistered lane gate = %+v, want zero gate", g)
	}
	if g := b.GateFor(GateOverall); g.MaxRegression != 0.05 || g.MinImprovement != 0.1 {
		t.Fatalf("registered gate = %+v", g)
	}
}

func TestScoreboardRoundTrip(t *testing.T) {
	run, _ := scoreboardFixture()
	run.Tasks[0].Checks = []CheckResult{{Kind: "answer_contains", Passed: true, Detail: "alias hit"}}
	run.Tasks[0].Answer = "The compaction preserved the fact."
	run.Tasks[0].ToolCalls = 2
	run.Tasks[0].DurationMS = 4210
	run.Tasks[2].Error = "upstream 503 after retries"

	data, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Scoreboard
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(run, out) {
		t.Fatalf("round trip mismatch:\nwant %+v\ngot  %+v", run, out)
	}
	again, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(data) != string(again) {
		t.Fatal("re-marshal differs from original bytes")
	}
}

func TestBaselineRoundTrip(t *testing.T) {
	_, base := scoreboardFixture()
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Baseline
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(base, out) {
		t.Fatalf("round trip mismatch:\nwant %+v\ngot  %+v", base, out)
	}
	again, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(data) != string(again) {
		t.Fatal("re-marshal differs from original bytes")
	}
}
