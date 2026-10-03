package suite

import (
	"fmt"
	"path/filepath"
	"testing"
)

// The external lane files: authored outside the internal-lane set
// (compaction/tool-error/instruction-fidelity), each covering exactly one
// canonical lane. Task-count bounds are the per-lane spec from design D6 and
// tasks.md (delegation 5, memory 5–6, anchor 3–5).
var externalLaneFiles = []struct {
	file     string
	lane     string
	minTasks int
	maxTasks int
}{
	{file: "delegation.json", lane: LaneDelegation, minTasks: 5, maxTasks: 5},
	{file: "memory.json", lane: LaneMemory, minTasks: 5, maxTasks: 6},
	{file: "anchor.json", lane: LaneAnchor, minTasks: 3, maxTasks: 5},
}

// TestExternalLaneFilesLoadLintAndCount validates the three externally
// authored lane files end to end: strict-JSON Load, zero lint issues, the
// canonical lane id, suite version "1", per-lane task counts, no judge
// checks (deterministic graders only), non-empty tools_used per task, and
// task-id uniqueness across all three files (they share one scoreboard).
func TestExternalLaneFilesLoadLintAndCount(t *testing.T) {
	seenIDs := map[string]string{} // task id -> file that first declared it

	for _, tt := range externalLaneFiles {
		t.Run(tt.file, func(t *testing.T) {
			path := filepath.Join("..", "suites", tt.file)
			s, err := Load(path)
			if err != nil {
				t.Fatalf("Load(%s) failed: %v", path, err)
			}

			if issues := Lint(s); len(issues) > 0 {
				for _, issue := range issues {
					t.Errorf("lint issue in %s: %s", tt.file, externalLaneIssueString(issue))
				}
			}

			if !IsCanonicalLane(s.Lane) {
				t.Errorf("%s: lane %q is not a canonical lane", tt.file, s.Lane)
			}
			if s.Lane != tt.lane {
				t.Errorf("%s: lane = %q, want %q", tt.file, s.Lane, tt.lane)
			}
			if s.SuiteVersion != "1" {
				t.Errorf("%s: suite_version = %q, want \"1\"", tt.file, s.SuiteVersion)
			}
			if s.JudgeAllowed {
				t.Errorf("%s: judge_allowed must not be set; external lanes are deterministic-only", tt.file)
			}
			if len(s.Tasks) < tt.minTasks || len(s.Tasks) > tt.maxTasks {
				t.Errorf("%s: task count = %d, want %d-%d", tt.file, len(s.Tasks), tt.minTasks, tt.maxTasks)
			}

			for _, task := range s.Tasks {
				if prev, dup := seenIDs[task.ID]; dup {
					t.Errorf("%s: task id %q already declared in %s; ids must be unique across the external lane files", tt.file, task.ID, prev)
				} else {
					seenIDs[task.ID] = tt.file
				}
				if len(task.ToolsUsed) == 0 {
					t.Errorf("%s: task %s has an empty tools_used", tt.file, task.ID)
				}
				for i, check := range task.Verify {
					if check.Kind == CheckJudge {
						t.Errorf("%s: task %s verify[%d] is a judge check; external lanes forbid the judge (deterministic graders only)", tt.file, task.ID, i)
					}
				}
			}
		})
	}
}

// externalLaneIssueString renders one lint Issue for test output.
func externalLaneIssueString(i Issue) string {
	if i.TaskID == "" {
		return fmt.Sprintf("%s: %s", i.Field, i.Message)
	}
	return fmt.Sprintf("task %s at %s: %s", i.TaskID, i.Field, i.Message)
}
