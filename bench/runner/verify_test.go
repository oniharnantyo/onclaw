package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw-bench/driver"
	"github.com/oniharnantyo/onclaw-bench/suite"
)

func call(name, argsJSON, output string) driver.ToolCall {
	return driver.ToolCall{Name: name, CallID: "call-" + name, ArgsJSON: argsJSON, Output: output}
}

// TestCheckAnswerContains covers the alias-group semantics: any_of is an
// OR over its groups, all_of an AND over its groups, and a group hits when
// at least one alias appears (normalized substring).
func TestCheckAnswerContains(t *testing.T) {
	answer := "The vault code is Alpha Bravo, saved to the file."
	tests := []struct {
		name  string
		check suite.Check
		want  bool
	}{
		{"any_of first alias", suite.Check{Kind: suite.CheckAnswerContains, AnyOf: [][]string{{"vault code"}}}, true},
		{"any_of second alias", suite.Check{Kind: suite.CheckAnswerContains, AnyOf: [][]string{{"vault code", "alpha bravo"}}}, true},
		{"any_of miss", suite.Check{Kind: suite.CheckAnswerContains, AnyOf: [][]string{{"gamma"}}}, false},
		{"any_of second group", suite.Check{Kind: suite.CheckAnswerContains, AnyOf: [][]string{{"gamma"}, {"alpha bravo"}}}, true},
		{"all_of both groups", suite.Check{Kind: suite.CheckAnswerContains, AllOf: [][]string{{"vault code"}, {"alpha bravo"}}}, true},
		{"all_of one group missing", suite.Check{Kind: suite.CheckAnswerContains, AllOf: [][]string{{"vault code"}, {"gamma"}}}, false},
		{"any_of + all_of both hold", suite.Check{Kind: suite.CheckAnswerContains, AnyOf: [][]string{{"vault code"}}, AllOf: [][]string{{"saved"}}}, true},
		{"any_of + all_of one fails", suite.Check{Kind: suite.CheckAnswerContains, AnyOf: [][]string{{"vault code"}}, AllOf: [][]string{{"gamma"}}}, false},
		{"normalization folds commas+case", suite.Check{Kind: suite.CheckAnswerContains, AnyOf: [][]string{{"alpha, bravo"}}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := checkAnswerContains(&tc.check, answer)
			if got != tc.want {
				t.Fatalf("passed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckAnswerNotContains(t *testing.T) {
	c := suite.Check{Kind: suite.CheckAnswerNotContains, NoneOf: []string{"cannot", "error"}}
	if pass, _ := checkAnswerNotContains(&c, "All done, saved."); !pass {
		t.Fatal("clean answer should pass")
	}
	if pass, _ := checkAnswerNotContains(&c, "I cannot do that."); pass {
		t.Fatal("leaky answer should fail")
	}
}

// TestCheckToolCalled pins the pooled-call matching: name equivalence
// (suite convention vs wire name) plus the optional args/output probes.
func TestCheckToolCalled(t *testing.T) {
	calls := []driver.ToolCall{
		call("write_file", `{"file_path":"/workspace/notes.md","content":"alpha"}`, "wrote /workspace/notes.md"),
	}
	tests := []struct {
		name  string
		check suite.Check
		want  bool
	}{
		{"suite convention name matches wire name", suite.Check{Kind: suite.CheckToolCalled, Tool: "files.write"}, true},
		{"wire name exact", suite.Check{Kind: suite.CheckToolCalled, Tool: "write_file"}, true},
		{"unknown tool", suite.Check{Kind: suite.CheckToolCalled, Tool: "grafana.query"}, false},
		{"args probe hit", suite.Check{Kind: suite.CheckToolCalled, Tool: "files.write", ArgsContains: "notes.md"}, true},
		{"args probe miss", suite.Check{Kind: suite.CheckToolCalled, Tool: "files.write", ArgsContains: "other.md"}, false},
		{"output probe hit", suite.Check{Kind: suite.CheckToolCalled, Tool: "files.write", OutputContains: "wrote"}, true},
		{"output probe miss", suite.Check{Kind: suite.CheckToolCalled, Tool: "files.write", OutputContains: "failed"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := checkToolCalled(&tc.check, calls)
			if got != tc.want {
				t.Fatalf("passed = %v, want %v", got, tc.want)
			}
		})
	}

	notCalled := suite.Check{Kind: suite.CheckToolNotCalled, Tool: "files.write"}
	if pass, _ := checkToolNotCalled(&notCalled, calls); pass {
		t.Fatal("tool_not_called should fail when the tool was called")
	}
	if pass, _ := checkToolNotCalled(&suite.Check{Kind: suite.CheckToolNotCalled, Tool: "grafana.query"}, calls); !pass {
		t.Fatal("tool_not_called should pass when the tool was not called")
	}
}

// TestCheckTranscriptFileWrite pins the write-tool path matching: the jailed
// mount prefixes absolute paths, so the suite's relative path is the suffix;
// ContentContains probes the args' content field.
func TestCheckTranscriptFileWrite(t *testing.T) {
	calls := []driver.ToolCall{
		call("write_file", `{"file_path":"/workspace/notes.md","content":"alpha bravo"}`, "ok"),
	}
	tests := []struct {
		name  string
		check suite.Check
		want  bool
	}{
		{"relative path suffix", suite.Check{Kind: suite.CheckTranscriptFileWrite, Path: "notes.md"}, true},
		{"exact path", suite.Check{Kind: suite.CheckTranscriptFileWrite, Path: "/workspace/notes.md"}, true},
		{"wrong file", suite.Check{Kind: suite.CheckTranscriptFileWrite, Path: "other.md"}, false},
		{"content probe hit", suite.Check{Kind: suite.CheckTranscriptFileWrite, Path: "notes.md", ContentContains: "alpha bravo"}, true},
		{"content probe miss", suite.Check{Kind: suite.CheckTranscriptFileWrite, Path: "notes.md", ContentContains: "gamma"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := checkTranscriptFileWrite(&tc.check, calls)
			if got != tc.want {
				t.Fatalf("passed = %v, want %v", got, tc.want)
			}
		})
	}

	// The suite's dotted convention name matches too.
	if pass, _ := checkTranscriptFileWrite(&suite.Check{Kind: suite.CheckTranscriptFileWrite, Path: "notes.md"}, []driver.ToolCall{call("files.write", `{"file_path":"notes.md"}`, "ok")}); !pass {
		t.Fatal("files.write-named call should count as a filesystem write")
	}
}

// TestToolNameMatches pins the alias table both ways plus exact matches.
func TestToolNameMatches(t *testing.T) {
	tests := []struct {
		checkTool, wireName string
		want                bool
	}{
		{"files.write", "write_file", true},
		{"write_file", "files.write", true},
		{"shell", "execute", true},
		{"subagents", "agent", true},
		{"memory.search", "memory", true},
		{"write_file", "write_file", true},
		{"web.search", "web.fetch", true},
		{"files.write", "execute", false},
		{"grafana.query", "grafana.query", true},
	}
	for _, tc := range tests {
		if got := toolNameMatches(tc.checkTool, tc.wireName); got != tc.want {
			t.Errorf("toolNameMatches(%q, %q) = %v, want %v", tc.checkTool, tc.wireName, got, tc.want)
		}
	}
}

// TestCheckAPICEffect pins the stateful api_effect check: {{workspace}} /
// {{agent}} substitution, status assertion, and raw-body probes.
func TestCheckAPICEffect(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"agent":{"slug":"bench-agent"}}`))
	}))
	defer srv.Close()

	client := driver.NewClient(srv.URL, 0)
	env := &verifyEnv{client: client, adminToken: "tok-admin", workspace: "bench-eval", agent: "bench-agent"}

	check := suite.Check{Kind: suite.CheckAPICEffect, Method: http.MethodGet, Path: "/api/v1/workspaces/{{workspace}}/agents/{{agent}}", Status: 200, JSONContains: []string{"bench-agent"}}
	res, err := runCheck(context.Background(), &check, env)
	if err != nil {
		t.Fatalf("runCheck: %v", err)
	}
	if !res.Passed {
		t.Fatalf("api_effect should pass: %+v", res)
	}
	if gotPath != "/api/v1/workspaces/bench-eval/agents/bench-agent" {
		t.Fatalf("path = %q, placeholders not substituted", gotPath)
	}
	if gotAuth != "Bearer tok-admin" {
		t.Fatalf("auth = %q, want the admin bearer token", gotAuth)
	}

	// Wrong expected status fails the check (it is data, not an error).
	check.Status = 404
	res, err = runCheck(context.Background(), &check, env)
	if err != nil || res.Passed {
		t.Fatalf("wrong status should fail the check: passed=%v err=%v", res.Passed, err)
	}

	// Missing probe fails the check.
	check.Status = 200
	check.JSONContains = []string{"nope"}
	res, err = runCheck(context.Background(), &check, env)
	if err != nil || res.Passed {
		t.Fatalf("missing probe should fail the check: passed=%v err=%v", res.Passed, err)
	}
}

// TestJudgeCheck pins the judge flow: the configured environment grades, a
// missing key is task infra with an actionable message, and the verdict
// parser reads PASS/FAIL from the first line.
func TestJudgeCheck(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decoding judge request: %v", err)
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"PASS\nthe answer satisfies every rubric point"}}]}`))
	}))
	defer srv.Close()

	t.Setenv(EnvJudgeKey, "test-key")
	t.Setenv(EnvJudgeBaseURL, srv.URL)

	judge, err := newJudgeClient("glm-judge-test")
	if err != nil {
		t.Fatalf("newJudgeClient: %v", err)
	}
	env := &verifyEnv{answer: "the full answer text", judge: judge}
	check := suite.Check{Kind: suite.CheckJudge, Rubric: "the reply must restate the codename"}
	res, err := runCheck(context.Background(), &check, env)
	if err != nil {
		t.Fatalf("runCheck(judge): %v", err)
	}
	if !res.Passed {
		t.Fatalf("judge check should pass: %+v", res)
	}
	if got, _ := gotBody["model"].(string); got != "glm-judge-test" {
		t.Fatalf("judge request model = %v, want the pinned judge model", gotBody["model"])
	}
	if user := userContent(t, gotBody); !strings.Contains(user, "codename") || !strings.Contains(user, "the full answer text") {
		t.Fatalf("judge user prompt should carry rubric + transcript, got %q", user)
	}

	// Unparseable verdicts fail the check with the raw content in the detail.
	unparseable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"looks fine to me"}}]}`))
	}))
	defer unparseable.Close()
	t.Setenv(EnvJudgeBaseURL, unparseable.URL)
	judge2, err := newJudgeClient("")
	if err != nil {
		t.Fatalf("newJudgeClient: %v", err)
	}
	res, err = runCheck(context.Background(), &check, &verifyEnv{answer: "answer", judge: judge2})
	if err != nil {
		t.Fatalf("runCheck(unparseable judge): %v", err)
	}
	if res.Passed {
		t.Fatal("unparseable judge verdict should fail the check")
	}
	if !strings.Contains(res.Detail, "unparseable") {
		t.Fatalf("detail should flag the unparseable verdict, got %q", res.Detail)
	}

	// Missing key environment: infra, with the env var named.
	t.Setenv(EnvJudgeKey, "")
	res, err = runCheck(context.Background(), &check, &verifyEnv{judgeErr: mustJudgeErr(t)})
	if err == nil {
		t.Fatal("missing judge env should be task infra (error return)")
	}
	if res.Passed {
		t.Fatal("infra return should not carry a passed check")
	}
	if !strings.Contains(err.Error(), EnvJudgeKey) {
		t.Fatalf("infra error should name %s, got %v", EnvJudgeKey, err)
	}
}

func mustJudgeErr(t *testing.T) error {
	t.Helper()
	_, err := newJudgeClient("")
	if err == nil {
		t.Fatal("newJudgeClient without a key should return the actionable error")
	}
	return err
}

func userContent(t *testing.T, body map[string]any) string {
	t.Helper()
	choices, _ := body["messages"].([]any)
	for _, m := range choices {
		msg, _ := m.(map[string]any)
		if msg["role"] == "user" {
			s, _ := msg["content"].(string)
			return s
		}
	}
	t.Fatal("judge request carried no user message")
	return ""
}
