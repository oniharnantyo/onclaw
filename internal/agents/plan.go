package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The agent's task plan lives in its workspace as the plantask middleware's
// per-task JSON files (<workspace>/.tasks/<id>.json). Rather than have the UI
// parse plan tool results, the drain re-reads the directory after any plan
// tool completes and records the whole plan as one KindPlanActivity snapshot
// — persisted, so replay folds to the newest.

// planDirFor returns the plan directory of a rooted workspace; "" without one.
func planDirFor(agentDir string) string {
	if agentDir == "" {
		return ""
	}
	return filepath.Join(agentDir, workspaces.DirTasks)
}

// planDirForSlug resolves the plan directory from the agent's workspace when
// no turn surface is at hand (a resumed run); "" when unresolvable.
func (m *manager) planDirForSlug(slug string) string {
	if slug == "" || m.workspaces == nil {
		return ""
	}
	dir, err := m.workspaces.Dir(slug)
	if err != nil {
		return ""
	}
	return planDirFor(dir)
}

// planMutatingTools are the plantask tools whose success changes the plan.
var planMutatingTools = map[string]bool{
	ToolTaskCreate: true,
	ToolTaskUpdate: true,
}

// recordPlanSnapshot records a plan snapshot when msg carries a successful
// result of a plan-mutating tool. Best effort: a snapshot that cannot be read
// or recorded never fails the turn.
func recordPlanSnapshot(msg *schema.AgenticMessage, planDir string, record recordFn, toolErrors *toolErrorResult) {
	if planDir == "" || msg == nil || record == nil {
		return
	}
	changed := false
	for _, b := range msg.ContentBlocks {
		if b == nil || b.Type != schema.ContentBlockTypeFunctionToolResult || b.FunctionToolResult == nil {
			continue
		}
		if !planMutatingTools[b.FunctionToolResult.Name] {
			continue
		}
		if toolErrors != nil {
			if _, failed := toolErrors.FailureOf(b.FunctionToolResult.CallID); failed {
				continue
			}
		}
		changed = true
	}
	if !changed {
		return
	}
	snap, err := readPlanSnapshot(planDir)
	if err != nil {
		return
	}
	_ = record(&sessions.Event{Kind: sessions.KindPlanActivity, Plan: snap}, true)
}

// planTaskFile is the on-disk shape of one plantask record (the middleware's
// own JSON); only the display fields are read.
type planTaskFile struct {
	ID          string   `json:"id"`
	Subject     string   `json:"subject"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	ActiveForm  string   `json:"activeForm"`
	BlockedBy   []string `json:"blockedBy"`
}

const planStatusDeleted = "deleted"

// readPlanSnapshot reads every task file under dir into a snapshot ordered by
// numeric id. Deleted tasks and unreadable files are skipped; a missing
// directory is an empty plan.
func readPlanSnapshot(dir string) (*sessions.PlanSnapshot, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &sessions.PlanSnapshot{Tasks: []sessions.PlanTask{}}, nil
		}
		return nil, err
	}
	tasks := make([]sessions.PlanTask, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var file planTaskFile
		if err := json.Unmarshal(raw, &file); err != nil || file.ID == "" || file.Status == planStatusDeleted {
			continue
		}
		tasks = append(tasks, sessions.PlanTask{
			ID:          file.ID,
			Subject:     file.Subject,
			Description: file.Description,
			Status:      file.Status,
			ActiveForm:  file.ActiveForm,
			BlockedBy:   file.BlockedBy,
		})
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		a, aErr := strconv.Atoi(tasks[i].ID)
		b, bErr := strconv.Atoi(tasks[j].ID)
		if aErr == nil && bErr == nil {
			return a < b
		}
		return tasks[i].ID < tasks[j].ID
	})
	return &sessions.PlanSnapshot{Tasks: tasks}, nil
}
