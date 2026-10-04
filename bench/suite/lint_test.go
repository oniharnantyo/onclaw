package suite

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLintCleanFixture(t *testing.T) {
	// The full fixture exercises every setup-step variant and every check
	// kind; it must lint clean.
	if issues := Lint(fullFixture()); len(issues) != 0 {
		t.Fatalf("expected clean lint, got %d issues: %+v", len(issues), issues)
	}
}

func TestLintEmptySuite(t *testing.T) {
	issues := Lint(TaskSuite{})
	fields := make([]string, 0, len(issues))
	for _, is := range issues {
		fields = append(fields, is.Field)
	}
	want := []string{"suite_version", "lane", "tasks"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("empty suite issues = %v, want %v", fields, want)
	}
}

// TestLintIssues mutates the clean fixture one violation at a time and
// asserts the exact single issue produced (field + task id).
func TestLintIssues(t *testing.T) {
	cw := 0
	tests := []struct {
		name   string
		mutate func(*TaskSuite)
		taskID string // expected Issue.TaskID ("" = suite-level)
		field  string // expected Issue.Field
	}{
		// Schema: suite level.
		{"bad lane", func(s *TaskSuite) { s.Lane = "compsction" }, "", "lane"},
		{"blank suite version", func(s *TaskSuite) { s.SuiteVersion = "  " }, "", "suite_version"},
		{"no tasks", func(s *TaskSuite) { s.Tasks = nil }, "", "tasks"},

		// Schema: task shape.
		{"empty task id", func(s *TaskSuite) { s.Tasks[0].ID = "" }, "", "id"},
		{"non-kebab task id", func(s *TaskSuite) { s.Tasks[0].ID = "Dead MCP" }, "", "id"},
		{"duplicate task ids", func(s *TaskSuite) { s.Tasks = append(s.Tasks, s.Tasks[0]) }, "", "id"},
		{"empty tools_used", func(s *TaskSuite) { s.Tasks[0].ToolsUsed = nil }, "dead-mcp-recovery", "tools_used"},
		{"undotted tool", func(s *TaskSuite) { s.Tasks[0].ToolsUsed[1] = "shell" }, "dead-mcp-recovery", "tools_used[1]"},
		{"no turns", func(s *TaskSuite) { s.Tasks[0].Turns = nil }, "dead-mcp-recovery", "turns"},
		{"empty turn text", func(s *TaskSuite) { s.Tasks[0].Turns[1].Text = "  " }, "dead-mcp-recovery", "turns[1].text"},
		{"no checks", func(s *TaskSuite) { s.Tasks[0].Verify = nil }, "dead-mcp-recovery", "verify"},

		// Schema: setup steps.
		{"chat_turns missing actor", func(s *TaskSuite) { s.Tasks[0].Setup[1].Actor = "" }, "dead-mcp-recovery", "setup[1].actor"},
		{"chat_turns missing session", func(s *TaskSuite) { s.Tasks[0].Setup[1].Session = "" }, "dead-mcp-recovery", "setup[1].session"},
		{"chat_turns no turns", func(s *TaskSuite) { s.Tasks[0].Setup[1].Turns = nil }, "dead-mcp-recovery", "setup[1].turns"},
		{"agent_config zero context window", func(s *TaskSuite) { s.Tasks[0].Setup[0].ContextWindow = &cw }, "dead-mcp-recovery", "setup[0].context_window"},
		{"hook missing handler", func(s *TaskSuite) { s.Tasks[0].Setup[2].Handler = nil }, "dead-mcp-recovery", "setup[2].handler"},
		{"hook unsupported handler type", func(s *TaskSuite) { s.Tasks[0].Setup[2].Handler.Type = "webhook" }, "dead-mcp-recovery", "setup[2].handler.type"},
		{"hook url missing control placeholder", func(s *TaskSuite) { s.Tasks[0].Setup[2].Handler.URL = "http://elsewhere.example/block" }, "dead-mcp-recovery", "setup[2].handler.url"},
		{"connection missing recipe", func(s *TaskSuite) { s.Tasks[0].Setup[3].Recipe = "" }, "dead-mcp-recovery", "setup[3].recipe"},
		{"connection missing name", func(s *TaskSuite) { s.Tasks[0].Setup[3].Name = "" }, "dead-mcp-recovery", "setup[3].name"},
		{"wait zero seconds", func(s *TaskSuite) { s.Tasks[0].Setup[4].Seconds = 0 }, "dead-mcp-recovery", "setup[4].seconds"},
		{"unknown setup type", func(s *TaskSuite) { s.Tasks[0].Setup[4].Type = "restart" }, "dead-mcp-recovery", "setup[4].type"},

		// Schema + grader consistency: checks.
		{"answer_contains no groups", func(s *TaskSuite) { s.Tasks[0].Verify[0].AnyOf = nil; s.Tasks[0].Verify[0].AllOf = nil }, "dead-mcp-recovery", "verify[0]"},
		{"answer_contains empty group", func(s *TaskSuite) { s.Tasks[0].Verify[0].AnyOf = [][]string{{}} }, "dead-mcp-recovery", "verify[0].any_of[0]"},
		{"answer_contains empty alias", func(s *TaskSuite) { s.Tasks[0].Verify[0].AnyOf = [][]string{{"down", "  "}} }, "dead-mcp-recovery", "verify[0].any_of[0][1]"},
		{"answer_not_contains no tokens", func(s *TaskSuite) { s.Tasks[0].Verify[1].NoneOf = nil }, "dead-mcp-recovery", "verify[1].none_of"},
		{"answer_not_contains empty token", func(s *TaskSuite) { s.Tasks[0].Verify[1].NoneOf = []string{""} }, "dead-mcp-recovery", "verify[1].none_of[0]"},
		{"tool_called missing tool", func(s *TaskSuite) { s.Tasks[0].Verify[2].Tool = "" }, "dead-mcp-recovery", "verify[2].tool"},
		{"tool_called both tool and tool_any", func(s *TaskSuite) { s.Tasks[0].Verify[2].ToolAny = []string{"glob", "ls"} }, "dead-mcp-recovery", "verify[2].tool_any"},
		{"tool_called empty tool_any entry", func(s *TaskSuite) {
			s.Tasks[0].Verify[2].Tool = ""
			s.Tasks[0].Verify[2].ToolAny = []string{"glob", " "}
		}, "dead-mcp-recovery", "verify[2].tool_any[1]"},
		{"tool_not_called missing tool", func(s *TaskSuite) { s.Tasks[0].Verify[3].Tool = "" }, "dead-mcp-recovery", "verify[3].tool"},
		{"transcript_file_write missing path", func(s *TaskSuite) { s.Tasks[0].Verify[4].Path = "" }, "dead-mcp-recovery", "verify[4].path"},
		{"api_effect missing method", func(s *TaskSuite) { s.Tasks[0].Verify[5].Method = "" }, "dead-mcp-recovery", "verify[5].method"},
		{"api_effect relative path", func(s *TaskSuite) { s.Tasks[0].Verify[5].Path = "workspaces/{{workspace}}/agents" }, "dead-mcp-recovery", "verify[5].path"},
		{"api_effect missing status", func(s *TaskSuite) { s.Tasks[0].Verify[5].Status = 0 }, "dead-mcp-recovery", "verify[5].status"},
		{"judge missing rubric", func(s *TaskSuite) { s.Tasks[0].Verify[6].Rubric = "" }, "dead-mcp-recovery", "verify[6].rubric"},
		{"judge without judge_allowed", func(s *TaskSuite) { s.JudgeAllowed = false }, "dead-mcp-recovery", "verify[6]"},
		{"unknown check kind", func(s *TaskSuite) { s.Tasks[0].Verify[6].Kind = "vibes" }, "dead-mcp-recovery", "verify[6].kind"},

		// Grader consistency: none_of token inside an expected alias group.
		{"none_of token inside alias", func(s *TaskSuite) {
			s.Tasks[0].Verify[0].AllOf = [][]string{{"the retry happened eventually"}}
			s.Tasks[0].Verify[1].NoneOf = []string{"retry happened"}
		}, "dead-mcp-recovery", "verify[1].none_of[0]"},

		// Unintervention: long literal in the task's own material.
		{"expected literal in turn", func(s *TaskSuite) {
			s.Tasks[0].Verify[0].AnyOf = [][]string{{"latency dashboard"}}
		}, "dead-mcp-recovery", "verify[0].any_of[0][0]"},
		{"expected literal in summary", func(s *TaskSuite) {
			s.Tasks[0].Summary = "The agent must fall back gracefully instead of inventing made-up numbers."
		}, "dead-mcp-recovery", "verify[1].none_of[0]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := fullFixture()
			tc.mutate(&s)
			issues := Lint(s)
			if len(issues) != 1 {
				t.Fatalf("expected exactly 1 issue, got %d: %+v", len(issues), issues)
			}
			if issues[0].TaskID != tc.taskID {
				t.Errorf("issue task_id = %q, want %q", issues[0].TaskID, tc.taskID)
			}
			if issues[0].Field != tc.field {
				t.Errorf("issue field = %q, want %q", issues[0].Field, tc.field)
			}
			if issues[0].Message == "" {
				t.Error("issue message must not be empty")
			}
		})
	}
}

// TestLintHookHandlerVariants pins the per-type hook-handler rules: http
// keeps its {{control_url}} requirement, command requires a non-empty body
// with the {{control_file}} placeholder optional, and any other type is an
// unknown handler.
func TestLintHookHandlerVariants(t *testing.T) {
	tests := []struct {
		name      string
		handler   HookHandler
		wantField string // empty = expect clean
	}{
		{"http with control_url", HookHandler{Type: HandlerHTTP, URL: PlaceholderControlURL + "/block"}, ""},
		{"http without control_url", HookHandler{Type: HandlerHTTP, URL: "http://elsewhere.example/block"}, "setup[2].handler.url"},
		{"command without control_file", HookHandler{Type: HandlerCommand, Command: "echo blocked >&2\nexit 2"}, ""},
		{"command with control_file", HookHandler{Type: HandlerCommand, Command: "cat " + PlaceholderControlFile + " >/dev/null\necho blocked >&2\nexit 2"}, ""},
		{"command empty body", HookHandler{Type: HandlerCommand, Command: ""}, "setup[2].handler.command"},
		{"command blank body", HookHandler{Type: HandlerCommand, Command: "  \n\t "}, "setup[2].handler.command"},
		{"unknown type", HookHandler{Type: "webhook", URL: PlaceholderControlURL + "/block"}, "setup[2].handler.type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := fullFixture()
			s.Tasks[0].Setup[2].Handler = &tc.handler
			issues := Lint(s)
			if tc.wantField == "" {
				if len(issues) != 0 {
					t.Fatalf("expected clean lint, got %+v", issues)
				}
				return
			}
			if len(issues) != 1 || issues[0].Field != tc.wantField {
				t.Fatalf("expected 1 issue at %s, got %+v", tc.wantField, issues)
			}
		})
	}
}

// TestToolErrorLaneCommandHandlers loads the tool-error lane file and asserts
// the lane's block mechanism contract after the command-handler switch: it
// still parses and lints clean, every hook step carries a non-empty command
// handler, and no http handler remains (the http variant is dead on a harness
// whose hook fetcher rejects private targets).
func TestToolErrorLaneCommandHandlers(t *testing.T) {
	s, err := Load(filepath.Join("..", "suites", "tool_error.json"))
	if err != nil {
		t.Fatalf("load tool_error.json: %v", err)
	}
	if issues := Lint(s); len(issues) != 0 {
		t.Fatalf("expected clean lint, got %+v", issues)
	}
	found := 0
	for _, task := range s.Tasks {
		for i, step := range task.Setup {
			if step.Type != StepHook || step.Handler == nil {
				continue
			}
			found++
			if step.Handler.Type != HandlerCommand {
				t.Errorf("task %s setup[%d] hook %q uses handler type %q, want %q", task.ID, i, step.Name, step.Handler.Type, HandlerCommand)
			}
			if strings.TrimSpace(step.Handler.Command) == "" {
				t.Errorf("task %s setup[%d] hook %q has an empty command body", task.ID, i, step.Name)
			}
		}
	}
	if found == 0 {
		t.Fatal("expected hook steps in tool_error.json, found none")
	}
}

func TestLintUninterventionIgnoresSetupTurnsAndShortLiterals(t *testing.T) {
	// Setup turns are context seeding by design: an expected literal living
	// in a setup turn is legitimate. Short literals (< 12 runes) are skipped
	// even when they appear in the task's own turns.
	s := fullFixture()
	s.Tasks[0].Verify[0].AnyOf = [][]string{{"grafana source of truth"}} // 22 runes, only in the setup turn below
	s.Tasks[0].Verify[0].AllOf = [][]string{{"latency"}}                 // 7 runes, appears in turn 0
	s.Tasks[0].Setup[1].Turns = append(s.Tasks[0].Setup[1].Turns, "Our grafana source of truth is the latency board.")
	if issues := Lint(s); len(issues) != 0 {
		t.Fatalf("expected clean lint, got %+v", issues)
	}
}

func TestLintUninterventionNormalizedMatch(t *testing.T) {
	// Matching uses NormalizeText semantics: comma folding and curly
	// apostrophe folding must not hide a leak.
	s := fullFixture()
	s.Tasks[0].Turns[1].Text = "Nothing here for example, contractions aren’t the point."
	s.Tasks[0].Verify[1].NoneOf = []string{"for example, contractions aren't the point"}
	issues := Lint(s)
	if len(issues) != 1 {
		t.Fatalf("expected exactly 1 unintervention issue, got %d: %+v", len(issues), issues)
	}
	if issues[0].Field != "verify[1].none_of[0]" {
		t.Fatalf("field = %q, want verify[1].none_of[0]", issues[0].Field)
	}
}
