package suite

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// internalLaneFile describes one authored lane file under bench/suites and
// the invariants this package's own lanes must hold. The external lane files
// (delegation, memory, anchor) are covered by external_lanes_test.go; every
// helper here is prefixed internalLane to stay collision-free with it.
type internalLaneFile struct {
	// file is the path relative to this package's directory.
	file string
	// wantLane is the exact canonical lane id the file must declare.
	wantLane string
	// minTasks/maxTasks bound the task count the lane's design calls for
	// (design D6: compaction 7–8, tool-error 7–8, instruction-fidelity 5–6).
	minTasks int
	maxTasks int
}

// internalLaneFiles lists the three internal lanes authored by this change's
// task-suite worker, in canonical lane order.
var internalLaneFiles = []internalLaneFile{
	{file: "compaction.json", wantLane: LaneCompaction, minTasks: 7, maxTasks: 8},
	{file: "tool_error.json", wantLane: LaneToolError, minTasks: 7, maxTasks: 8},
	{file: "instruction_fidelity.json", wantLane: LaneInstructionFidelity, minTasks: 5, maxTasks: 6},
}

// internalLaneRenderIssues formats lint findings readably for failure output.
func internalLaneRenderIssues(issues []Issue) string {
	parts := make([]string, 0, len(issues))
	for _, is := range issues {
		parts = append(parts, fmt.Sprintf("  task=%q field=%s: %s", is.TaskID, is.Field, is.Message))
	}
	return strings.Join(parts, "\n")
}

// TestInternalLaneFilesLoadLintAndCount loads each authored internal lane
// file, runs the full ABC lint, and asserts the suite-level invariants:
// canonical lane id, pinned suite version, the design's task-count band, no
// judge checks anywhere (deterministic graders only), non-empty tools_used,
// and task ids unique across the three files (they share one scoreboard and
// one fixture agent, so a collision would merge two tasks' verdicts).
func TestInternalLaneFilesLoadLintAndCount(t *testing.T) {
	seenIDs := map[string]string{} // task id -> file it first appeared in

	for _, lf := range internalLaneFiles {
		t.Run(lf.file, func(t *testing.T) {
			path := filepath.Join("..", "suites", lf.file)

			s, err := Load(path)
			if err != nil {
				t.Fatalf("Load(%s) failed: %v", path, err)
			}

			if issues := Lint(s); len(issues) > 0 {
				t.Fatalf("Lint(%s) returned %d issues:\n%s", lf.file, len(issues), internalLaneRenderIssues(issues))
			}

			if !IsCanonicalLane(s.Lane) {
				t.Errorf("lane %q is not canonical", s.Lane)
			}
			if s.Lane != lf.wantLane {
				t.Errorf("lane = %q, want %q", s.Lane, lf.wantLane)
			}
			if s.SuiteVersion != "4" {
				t.Errorf("suite_version = %q, want \"4\"", s.SuiteVersion)
			}
			if s.JudgeAllowed {
				t.Errorf("judge_allowed is set; the internal lanes are fully deterministic and must not opt into the judge")
			}
			if len(s.Tasks) < lf.minTasks || len(s.Tasks) > lf.maxTasks {
				t.Errorf("task count = %d, want within [%d, %d]", len(s.Tasks), lf.minTasks, lf.maxTasks)
			}

			for _, task := range s.Tasks {
				for _, check := range task.Verify {
					if check.Kind == CheckJudge {
						t.Errorf("task %s carries a judge check; internal lanes must stay deterministic", task.ID)
					}
				}
				if len(task.ToolsUsed) == 0 {
					t.Errorf("task %s has empty tools_used", task.ID)
				}
				if prev := seenIDs[task.ID]; prev != "" {
					t.Errorf("task id %s appears in both %s and %s; ids must be unique across the internal lane files", task.ID, prev, lf.file)
				}
				seenIDs[task.ID] = lf.file
			}
		})
	}
}
