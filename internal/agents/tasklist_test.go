package agents

import (
	"encoding/json"
	"testing"
)

// ---- 2.4: the merged task list ----

// TestMergeSessionTasks is 2.4's verification: the pure join covers live-only,
// historical-only, created-lost-to-restart, and terminal-beats-live inputs,
// keeps the pinned status set honest (created never renders as running,
// live terminal snapshots agree with their persisted outcome), collapses
// duplicate persisted payloads, sorts deterministically, and returns an empty
// slice — never nil.
func TestMergeSessionTasks(t *testing.T) {
	t.Run("live-only", func(t *testing.T) {
		live := []LiveTask{{
			TaskID:      "task_live",
			Kind:        "delegation",
			Status:      "running",
			Description: "research sweep\nwith detail",
			OutputPath:  TasksOutputDir + "/task_live.output",
		}}
		entries := MergeSessionTasks(live, nil, nil)
		if len(entries) != 1 {
			t.Fatalf("entries = %d, want 1", len(entries))
		}
		entry := entries[0]
		want := TaskListEntry{
			TaskID:      "task_live",
			Kind:        "delegation",
			Status:      TaskStatusRunning,
			Description: "research sweep\nwith detail",
			OutputPath:  TasksOutputDir + "/task_live.output",
			Summary:     "research sweep",
		}
		if entry != want {
			t.Fatalf("entry = %+v, want %+v", entry, want)
		}

		// A live task already terminal in the store (completion event still in
		// flight) reports its live status, not running.
		entries = MergeSessionTasks([]LiveTask{{TaskID: "task_live", Kind: "shell", Status: "completed"}}, nil, nil)
		if entries[0].Status != taskStatusComplete {
			t.Fatalf("live terminal status = %q, want completed", entries[0].Status)
		}

		// Non-terminal non-running statuses are alive in-process: running.
		entries = MergeSessionTasks([]LiveTask{{TaskID: "task_live", Kind: "shell", Status: "pending"}}, nil, nil)
		if entries[0].Status != TaskStatusRunning {
			t.Fatalf("live pending status = %q, want running", entries[0].Status)
		}
	})

	t.Run("historical-only", func(t *testing.T) {
		completed := []TaskCompletedPayload{{
			TaskID:     "task_old",
			Kind:       "shell",
			Outcome:    taskStatusFailed,
			OutputPath: TasksOutputDir + "/task_old.output",
			Summary:    "nightly build",
		}}
		entries := MergeSessionTasks(nil, nil, completed)
		want := TaskListEntry{
			TaskID:      "task_old",
			Kind:        "shell",
			Status:      taskStatusFailed,
			Description: "nightly build",
			OutputPath:  TasksOutputDir + "/task_old.output",
			Summary:     "nightly build",
		}
		if len(entries) != 1 || entries[0] != want {
			t.Fatalf("entries = %+v, want [%+v]", entries, want)
		}
	})

	t.Run("created-lost-to-restart", func(t *testing.T) {
		created := []TaskCreatedRecord{{TaskID: "task_lost", Description: "research sweep"}}
		entries := MergeSessionTasks(nil, created, nil)
		want := TaskListEntry{
			TaskID:      "task_lost",
			Status:      TaskStatusCreated,
			Description: "research sweep",
		}
		if len(entries) != 1 || entries[0] != want {
			t.Fatalf("entries = %+v, want [%+v]", entries, want)
		}
		if entries[0].Status == TaskStatusRunning || entries[0].Kind != "" {
			t.Fatalf("restart-lost task must render created with no kind, got %+v", entries[0])
		}

		// Created-with-completion on a dead process renders the outcome.
		completed := []TaskCompletedPayload{{TaskID: "task_lost", Kind: "delegation", Outcome: taskStatusCanceled, Summary: "research sweep"}}
		entries = MergeSessionTasks(nil, created, completed)
		if entries[0].Status != taskStatusCanceled || entries[0].Kind != "delegation" {
			t.Fatalf("created+completed = %+v, want canceled delegation", entries[0])
		}
	})

	t.Run("terminal-beats-live", func(t *testing.T) {
		live := []LiveTask{{
			TaskID:      "task_done",
			Kind:        "delegation",
			Status:      "running",
			Description: "research sweep",
		}}
		completed := []TaskCompletedPayload{{
			TaskID:     "task_done",
			Kind:       "delegation",
			Outcome:    taskStatusCanceled,
			OutputPath: TasksOutputDir + "/task_done.output",
			Summary:    "research sweep",
		}}
		entries := MergeSessionTasks(live, nil, completed)
		if len(entries) != 1 {
			t.Fatalf("entries = %d, want 1 (live and persisted evidence join on the same task)", len(entries))
		}
		entry := entries[0]
		if entry.Status != taskStatusCanceled {
			t.Fatalf("status = %q, want persisted canceled to beat live running", entry.Status)
		}
		if entry.OutputPath != TasksOutputDir+"/task_done.output" || entry.Summary != "research sweep" {
			t.Fatalf("persisted output/summary must win: %+v", entry)
		}
	})

	t.Run("join dedup determinism and empty", func(t *testing.T) {
		if entries := MergeSessionTasks(nil, nil, nil); entries == nil || len(entries) != 0 {
			t.Fatalf("empty merge = %#v, want an empty non-nil slice", entries)
		}

		live := []LiveTask{
			{TaskID: "task_b", Kind: "shell", Status: "running", Description: "b"},
			{TaskID: "task_a", Kind: "shell", Status: "running", Description: "a"},
		}
		created := []TaskCreatedRecord{{TaskID: "task_c", Description: "c"}}
		completed := []TaskCompletedPayload{
			{TaskID: "task_a", Outcome: taskStatusCanceled},
			{TaskID: "task_a", Outcome: taskStatusComplete}, // redelivery duplicate: first wins
		}
		entries := MergeSessionTasks(live, created, completed)
		if len(entries) != 3 {
			t.Fatalf("entries = %d, want 3", len(entries))
		}
		if entries[0].TaskID != "task_a" || entries[1].TaskID != "task_b" || entries[2].TaskID != "task_c" {
			t.Fatalf("entries not sorted by task id: %+v", entries)
		}
		if entries[0].Status != taskStatusCanceled {
			t.Fatalf("duplicate completion collapsed to the wrong outcome: %+v", entries[0])
		}
	})
}

// TestTaskListEntryWireShape: the merged entry marshals with the pinned JSON
// keys the session tasks handler serves.
func TestTaskListEntryWireShape(t *testing.T) {
	data, err := json.Marshal(TaskListEntry{
		TaskID:      "task_1",
		Kind:        "delegation",
		Status:      TaskStatusRunning,
		Description: "research sweep",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	for _, key := range []string{"task_id", "kind", "status", "description"} {
		if _, ok := keys[key]; !ok {
			t.Fatalf("entry JSON missing pinned key %q: %s", key, data)
		}
	}
	if _, ok := keys["output_path"]; ok {
		t.Fatalf("empty output_path must be omitted: %s", data)
	}
	if _, ok := keys["summary"]; ok {
		t.Fatalf("empty summary must be omitted: %s", data)
	}
}
