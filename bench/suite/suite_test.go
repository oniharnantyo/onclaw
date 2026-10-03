package suite

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fullFixture builds a suite exercising every setup-step variant, every
// check kind, and every field, for round-trip and lint tests.
func fullFixture() TaskSuite {
	cw := 2048
	return TaskSuite{
		SuiteVersion: "2026.10.0",
		Lane:         LaneToolError,
		Description:  "Fixture exercising the whole schema",
		JudgeAllowed: true,
		Tasks: []Task{{
			ID:        "dead-mcp-recovery",
			Title:     "Recover from a dead MCP server",
			Summary:   "The agent must fall back gracefully when the connection fails.",
			ToolsUsed: []string{"grafana.query", "files.write"},
			Setup: []SetupStep{
				{Type: StepAgentConfig, Model: "glm-5.3-flash", ContextWindow: &cw, DisabledTools: []string{"files.delete"}},
				{Type: StepChatTurns, Actor: "amir", Session: "onboarding", Turns: []string{"We onboard vendors on Mondays.", "Our vendor contact is Rina."}},
				{Type: StepHook, Name: "block-grafana", Event: "pre_tool_use", MatcherTool: "grafana.query", Handler: &HookHandler{Type: HandlerHTTP, URL: PlaceholderControlURL + "/block/grafana"}},
				{Type: StepConnection, Recipe: "mcp", Name: "dead-grafana", Config: map[string]string{"url": "http://127.0.0.1:1/mcp"}},
				{Type: StepWait, Seconds: 2},
			},
			Turns: []TaskTurn{
				{Actor: "amir", Text: "Pull the latency dashboard please."},
				{Text: "Now summarize what you found."},
			},
			Verify: []Check{
				{Kind: CheckAnswerContains, AnyOf: [][]string{{"unavailable", "down"}, {"retried"}}, AllOf: [][]string{{"latency"}}},
				{Kind: CheckAnswerNotContains, NoneOf: []string{"made-up numbers"}},
				{Kind: CheckToolCalled, Tool: "grafana.query", ArgsContains: "latency", OutputContains: "p99"},
				{Kind: CheckToolNotCalled, Tool: "files.delete"},
				{Kind: CheckTranscriptFileWrite, Path: "transcript.md", ContentContains: "dashboard"},
				{Kind: CheckAPICEffect, Method: "GET", Path: "/api/v1/workspaces/{{workspace}}/agents/{{agent}}", Status: 200, JSONContains: []string{"ready"}},
				{Kind: CheckJudge, Rubric: "The answer cites the retry and invents no numbers."},
			},
		}},
	}
}

func TestTaskSuiteRoundTrip(t *testing.T) {
	in := fullFixture()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip mismatch:\nwant %+v\ngot  %+v", in, out)
	}
	again, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(data) != string(again) {
		t.Fatalf("re-marshal differs:\nwant %s\ngot  %s", data, again)
	}
}

func TestHookHandlerCommandRoundTrip(t *testing.T) {
	// The command handler variant must round-trip byte-identically through
	// marshal/parse, including the {{control_file}} placeholder and a
	// JSON-escaped newline inside the script body.
	s := fullFixture()
	s.Tasks[0].Setup[2].Handler = &HookHandler{
		Type:    HandlerCommand,
		Command: "cat " + PlaceholderControlFile + " >/dev/null\necho 'blocked by bench control hook' >&2\nexit 2",
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(s, out) {
		t.Fatalf("round trip mismatch:\nwant %+v\ngot  %+v", s, out)
	}
	got := out.Tasks[0].Setup[2].Handler
	if got.Type != HandlerCommand || got.URL != "" {
		t.Fatalf("decoded handler type/url = %q/%q, want %q/\"\"", got.Type, got.URL, HandlerCommand)
	}
	if want := "cat " + PlaceholderControlFile + " >/dev/null\necho 'blocked by bench control hook' >&2\nexit 2"; got.Command != want {
		t.Fatalf("decoded command = %q, want %q", got.Command, want)
	}
	if !strings.Contains(string(data), PlaceholderControlFile) {
		t.Fatal("marshaled suite lost the {{control_file}} placeholder")
	}
}

func TestParseStrictUnknownField(t *testing.T) {
	// Inject an unknown field into a check: strict decoding must reject it.
	broken := []byte(`{"suite_version":"x","lane":"memory","judge_allowed":false,"tasks":[{"id":"a","turns":[{"text":"hi"}],"verify":[{"kind":"tool_called","tool":"files.write","bogus_field":1}]}]}`)
	if _, err := Parse(broken); err == nil {
		t.Fatal("expected error for unknown check field, got nil")
	}
}

func TestParseStrictUnknownCheckKind(t *testing.T) {
	// Unknown kinds decode fine (they are strings) and are caught by Lint,
	// not by Parse — assert that contract explicitly.
	s, err := Parse([]byte(`{"suite_version":"x","lane":"memory","judge_allowed":false,"tasks":[{"id":"a","turns":[{"text":"hi"}],"verify":[{"kind":"made_up_kind"}]}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Tasks[0].Verify[0].Kind != "made_up_kind" {
		t.Fatalf("kind = %q, want made_up_kind", s.Tasks[0].Verify[0].Kind)
	}
}

func TestParseRejectsTrailingData(t *testing.T) {
	if _, err := Parse([]byte(`{"suite_version":"x","lane":"memory","tasks":[]} {"suite_version":"y"}`)); err == nil {
		t.Fatal("expected error for trailing JSON, got nil")
	}
}

func TestLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-error.json")
	data, err := json.Marshal(fullFixture())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(fullFixture(), got) {
		t.Fatalf("loaded suite differs from fixture")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestNormalizeText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Won’t Find Anything", "won't find anything"},
		{"12 February, 2026; Q1", "12 february 2026 q1"},
		{"  spaces\tand\nnewlines ", "spaces and newlines"},
		{"a@b.com. Next", "a@b.com. next"},
	}
	for _, tc := range cases {
		if got := NormalizeText(tc.in); got != tc.want {
			t.Errorf("NormalizeText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCanonicalLanes(t *testing.T) {
	want := []string{"compaction", "tool-error", "instruction-fidelity", "delegation", "memory", "anchor"}
	got := CanonicalLanes()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CanonicalLanes() = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if CanonicalLanes()[0] != "compaction" {
		t.Fatal("CanonicalLanes must return a fresh copy each call")
	}
	for _, lane := range want {
		if !IsCanonicalLane(lane) {
			t.Errorf("IsCanonicalLane(%q) = false, want true", lane)
		}
	}
	if IsCanonicalLane("compsction") || IsCanonicalLane("") {
		t.Fatal("non-canonical lanes must not pass IsCanonicalLane")
	}
}
