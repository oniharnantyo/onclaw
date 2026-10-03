package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw-bench/scoreboard"
)

// writeTempFile writes content into dir/name and returns the path.
func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// writeArtifact marshals v as indented JSON into a temp file and returns its
// path.
func writeArtifact(t *testing.T, v any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.json")
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshaling artifact: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing artifact: %v", err)
	}
	return path
}

func runBoard(rates ...string) scoreboard.Scoreboard {
	board := scoreboard.Scoreboard{
		SchemaVersion: scoreboard.SchemaVersion,
		RunID:         "bench-test",
		SuiteVersion:  "1",
		ModelPin:      "glm-test",
		Tasks:         []scoreboard.TaskResult{},
	}
	pass := true
	for _, r := range rates {
		status := scoreboard.StatusFail
		if pass {
			status = scoreboard.StatusPass
		}
		pass = !pass
		board.Tasks = append(board.Tasks, scoreboard.TaskResult{TaskID: "t-" + r, Lane: r, Seed: 0, Status: status})
	}
	return board
}

// TestCompareExitCodes pins tasks.md 2.3: compare prints the per-lane delta
// report and exits 0 on gate PASS / 1 on FAIL.
func TestCompareExitCodes(t *testing.T) {
	baseTasks := []scoreboard.TaskResult{
		{TaskID: "m1", Lane: "memory", Seed: 0, Status: scoreboard.StatusPass},
		{TaskID: "m2", Lane: "memory", Seed: 0, Status: scoreboard.StatusFail},
	}
	makeRun := func(memoryPasses int) scoreboard.Scoreboard {
		statuses := []string{scoreboard.StatusFail, scoreboard.StatusFail}
		for i := 0; i < memoryPasses; i++ {
			statuses[i] = scoreboard.StatusPass
		}
		return scoreboard.Scoreboard{
			SchemaVersion: scoreboard.SchemaVersion,
			RunID:         "bench-run",
			SuiteVersion:  "1",
			ModelPin:      "glm-test",
			Tasks: []scoreboard.TaskResult{
				{TaskID: "m1", Lane: "memory", Seed: 0, Status: statuses[0]},
				{TaskID: "m2", Lane: "memory", Seed: 0, Status: statuses[1]},
			},
		}
	}
	baseline := scoreboard.Baseline{
		SchemaVersion: scoreboard.SchemaVersion,
		SuiteVersion:  "1",
		ModelPin:      "glm-test",
		Gates: map[string]scoreboard.Gate{
			"memory": {MaxRegression: 0.1},
		},
		Scoreboard: scoreboard.Scoreboard{SchemaVersion: scoreboard.SchemaVersion, Tasks: baseTasks},
	}
	baselinePath := writeArtifact(t, baseline)

	t.Run("flat rates pass the gate", func(t *testing.T) {
		runPath := writeArtifact(t, makeRun(1)) // 0.5 vs baseline 0.5
		var out bytes.Buffer
		code, err := runCompare(runPath, baselinePath, &out)
		if err != nil {
			t.Fatalf("runCompare: %v", err)
		}
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, out.String())
		}
		if !strings.Contains(out.String(), "GATE: PASS") || !strings.Contains(out.String(), "memory") || !strings.Contains(out.String(), "overall") {
			t.Fatalf("report should print per-lane rows plus the overall row and the verdict:\n%s", out.String())
		}
	})

	t.Run("regression beyond the gate fails", func(t *testing.T) {
		runPath := writeArtifact(t, makeRun(0)) // 0.0 vs baseline 0.5, gate floor 0.4
		var out bytes.Buffer
		code, err := runCompare(runPath, baselinePath, &out)
		if err != nil {
			t.Fatalf("runCompare: %v", err)
		}
		if code != 1 {
			t.Fatalf("exit = %d, want 1\n%s", code, out.String())
		}
		if !strings.Contains(out.String(), "GATE: FAIL") {
			t.Fatalf("report should end in GATE: FAIL:\n%s", out.String())
		}
	})

	t.Run("min improvement gate is honored", func(t *testing.T) {
		strict := baseline
		strict.Gates = map[string]scoreboard.Gate{"memory": {MinImprovement: 0.2}}
		strictPath := writeArtifact(t, strict)
		runPath := writeArtifact(t, makeRun(1)) // 0.5 is not +0.2 over 0.5
		var out bytes.Buffer
		code, err := runCompare(runPath, strictPath, &out)
		if err != nil {
			t.Fatalf("runCompare: %v", err)
		}
		if code != 1 {
			t.Fatalf("exit = %d, want 1\n%s", code, out.String())
		}
	})
}

// TestCompareRejectsBadArtifacts pins the loud failures: missing files,
// unparseable JSON, and stale schema versions never compare as PASS.
func TestCompareRejectsBadArtifacts(t *testing.T) {
	dir := t.TempDir()
	absent := filepath.Join(dir, "absent.json")

	var out bytes.Buffer
	if _, err := runCompare(absent, absent, &out); err == nil {
		t.Fatal("missing artifacts should error")
	}

	stale := map[string]any{"schema_version": 99}
	stalePath := filepath.Join(dir, "stale.json")
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(stalePath, data, 0o644); err != nil {
		t.Fatalf("writing stale artifact: %v", err)
	}
	if _, err := runCompare(stalePath, stalePath, &out); err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("stale schema should error naming schema_version, got %v", err)
	}
}

// TestCmdRunFlagHandling pins the CLI surface: missing credentials exit 1,
// flag misuse exits 2, and the env fallbacks backfill the admin credentials.
func TestCmdRunFlagHandling(t *testing.T) {
	t.Run("missing credentials exit 1", func(t *testing.T) {
		t.Setenv("ONCLAW_BENCH_ADMIN_EMAIL", "")
		t.Setenv("ONCLAW_BENCH_ADMIN_PASSWORD", "")
		dir := t.TempDir()
		suitePath := writeTempFile(t, dir, "s.json", validRunSuite)
		var out, stderr bytes.Buffer
		code := cmdRun([]string{"--base-url", "http://127.0.0.1:1", "--suite", suitePath, "--out", filepath.Join(dir, "o.json"), "--seeds", "1"}, &out, &stderr)
		if code != 1 {
			t.Fatalf("exit = %d, want 1\n%s", code, stderr.String())
		}
	})

	t.Run("positional arguments exit 2", func(t *testing.T) {
		var out, stderr bytes.Buffer
		code := cmdRun([]string{"stray"}, &out, &stderr)
		if code != 2 {
			t.Fatalf("exit = %d, want 2", code)
		}
	})

	t.Run("env fallback backfills credentials", func(t *testing.T) {
		t.Setenv("ONCLAW_BENCH_ADMIN_EMAIL", "admin@env")
		t.Setenv("ONCLAW_BENCH_ADMIN_PASSWORD", "pw")
		dir := t.TempDir()
		suitePath := writeTempFile(t, dir, "s.json", validRunSuite)
		var out, stderr bytes.Buffer
		// No --email/--password flags: the env fallback covers it. The server
		// is unreachable, so the run must fail operationally (1) — not with a
		// credentials complaint — proving the backfill happened.
		code := cmdRun([]string{"--base-url", "http://127.0.0.1:1", "--suite", suitePath, "--out", filepath.Join(dir, "o.json"), "--seeds", "1", "--backoff", "1ms"}, &out, &stderr)
		if code != 1 {
			t.Fatalf("exit = %d, want 1 (operational)\n%s", code, stderr.String())
		}
		if strings.Contains(stderr.String(), "--email") {
			t.Fatalf("env fallback should have satisfied the credential requirement:\n%s", stderr.String())
		}
	})
}

const validRunSuite = `{
  "suite_version": "1",
  "lane": "memory",
  "description": "cli fixture",
  "tasks": [{
    "id": "cli-probe",
    "title": "CLI",
    "summary": "Live fixture probe.",
    "tools_used": ["memory.search"],
    "turns": [{"text": "Say something."}],
    "verify": [{"kind": "answer_contains", "any_of": [["done"]]}]
  }]
}`
