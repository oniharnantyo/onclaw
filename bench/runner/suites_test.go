package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw-bench/suite"
)

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

const validSuiteA = `{
  "suite_version": "7",
  "lane": "memory",
  "description": "suite a",
  "tasks": [{
    "id": "task-a",
    "title": "A",
    "summary": "Seeded recall probe.",
    "tools_used": ["memory.search"],
    "turns": [{"text": "What was the project codename?"}],
    "verify": [{"kind": "answer_contains", "any_of": [["Nightingale"]]}]
  }]
}`

const validSuiteB = `{
  "suite_version": "7",
  "lane": "compaction",
  "description": "suite b",
  "tasks": [{
    "id": "task-b",
    "title": "B",
    "summary": "Long-thread probe.",
    "tools_used": ["files.write"],
    "turns": [{"text": "Summarize the thread into a file."}],
    "verify": [{"kind": "tool_called", "tool": "files.write"}]
  }]
}`

// TestLoadSuitesDirectorySkipsHelpers pins the underscore convention: a
// directory load takes every *.json except underscore-prefixed helper files,
// in sorted name order; an explicit file path always loads.
func TestLoadSuitesDirectorySkipsHelpers(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "b-compaction.json", validSuiteB)
	writeTempFile(t, dir, "_helpers.json", validSuiteA)
	writeTempFile(t, dir, "notes.txt", "not a suite")

	suites, err := LoadSuites(dir)
	if err != nil {
		t.Fatalf("LoadSuites: %v", err)
	}
	if len(suites) != 1 || suites[0].Lane != suite.LaneCompaction {
		t.Fatalf("suites = %+v, want exactly the non-underscore compaction suite", suites)
	}

	// Explicit underscore file loads.
	suites, err = LoadSuites(filepath.Join(dir, "_helpers.json"))
	if err != nil {
		t.Fatalf("LoadSuites(explicit helper): %v", err)
	}
	if len(suites) != 1 || suites[0].Tasks[0].ID != "task-a" {
		t.Fatalf("explicit load returned %+v, want the helper suite", suites)
	}
}

// TestLoadSuitesVersionMismatch pins the one-suite-version-per-run rule.
func TestLoadSuitesVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	other := strings.Replace(validSuiteB, `"suite_version": "7"`, `"suite_version": "8"`, 1)
	writeTempFile(t, dir, "a.json", validSuiteA)
	writeTempFile(t, dir, "b.json", other)

	if _, err := LoadSuites(dir); err == nil || !strings.Contains(err.Error(), "suite version mismatch") {
		t.Fatalf("LoadSuites error = %v, want a suite version mismatch", err)
	}
}

// TestLoadSuitesLintAborts pins that any lint issue aborts the load with the
// issue list printed, before anything runs.
func TestLoadSuitesLintAborts(t *testing.T) {
	dir := t.TempDir()
	// Judge check without judge_allowed is a lint failure.
	broken := strings.Replace(validSuiteA,
		`"verify": [{"kind": "answer_contains", "any_of": [["Nightingale"]]}]`,
		`"verify": [{"kind": "judge", "rubric": "grade it"}]`, 1)
	writeTempFile(t, dir, "a.json", broken)

	_, err := LoadSuites(dir)
	if err == nil {
		t.Fatalf("LoadSuites succeeded, want a lint failure")
	}
	if !strings.Contains(err.Error(), "suite lint failed") || !strings.Contains(err.Error(), "judge") {
		t.Fatalf("lint error %q should name the failure mode", err)
	}
}

// TestLoadSuitesDuplicateTaskID pins cross-suite task-id uniqueness (session
// ids and scoreboard rows are keyed by task id).
func TestLoadSuitesDuplicateTaskID(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "a.json", validSuiteA)
	dup := strings.Replace(validSuiteB, `"id": "task-b"`, `"id": "task-a"`, 1)
	writeTempFile(t, dir, "b.json", dup)

	if _, err := LoadSuites(dir); err == nil || !strings.Contains(err.Error(), "duplicate task id") {
		t.Fatalf("LoadSuites error = %v, want a duplicate task id error", err)
	}
}

// TestLoadSuitesMissing covers the explicit operator errors.
func TestLoadSuitesMissing(t *testing.T) {
	if _, err := LoadSuites(""); err == nil {
		t.Fatal("LoadSuites(\"\") should fail")
	}
	if _, err := LoadSuites(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("LoadSuites(missing file) should fail")
	}
	empty := t.TempDir()
	writeTempFile(t, empty, "_only-helper.json", validSuiteA)
	if _, err := LoadSuites(empty); err == nil || !strings.Contains(err.Error(), "no loadable") {
		t.Fatalf("helper-only directory should report no loadable suites, got %v", err)
	}
}
