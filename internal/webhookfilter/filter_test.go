package webhookfilter_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/expr-lang/expr/parser"

	"github.com/oniharnantyo/onclaw/internal/webhookfilter"
)

// canonicalPayload mirrors the spec scenario: a GitLab merge request delivery
// whose project carries a namespace path.
const canonicalPayload = `{
	"object_kind": "merge_request",
	"project": {
		"path_with_namespace": "team/api",
		"default_branch": "main",
		"archived": false,
		"stars": 150,
		"changes": [1, 2, 3]
	}
}`

// canonicalExpression is the spec's canonical filter example.
const canonicalExpression = `event == "merge_request" && project.path_with_namespace == "team/api"`

// deliveryEnv builds the eval environment per the cross-worker contract: the
// payload's top-level fields plus the reserved `event` key.
func deliveryEnv(t *testing.T, payloadJSON, event string) map[string]any {
	t.Helper()
	env := map[string]any{}
	if payloadJSON != "" {
		if err := json.Unmarshal([]byte(payloadJSON), &env); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
	}
	env["event"] = event
	return env
}

func TestCompile_Valid(t *testing.T) {
	tests := []struct {
		name       string
		expression string
	}{
		{"canonical", canonicalExpression},
		{"single quoted literal", `event == 'merge_request'`},
		{"negation", `!project.archived`},
		{"keyword not", `not project.archived`},
		{"in operator", `event in ["merge_request", "note"]`},
		{"matches operator", `event matches "^merge.*$"`},
		{"numeric comparison", `project.stars > 100`},
		{"index access", `project.changes[0] >= 1`},
		{"arithmetic", `project.stars + 10 > 100`},
		{"optional chain", `project?.archived == false`},
		{"ternary of bools", `event == "note" ? project.archived : !project.archived`},
		{"let alias", `let p = project; p.path_with_namespace == "team/api"`},
		{"parenthesized", `(event == "merge_request") && (project.path_with_namespace == "team/api")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := webhookfilter.Compile(tt.expression); err != nil {
				t.Fatalf("Compile(%q) = %v, want nil", tt.expression, err)
			}
		})
	}
}

func TestCompile_InvalidSyntaxReturnsParserMessageVerbatim(t *testing.T) {
	bad := `event == && project.x == "y"`
	_, parseErr := parser.Parse(bad)
	if parseErr == nil {
		t.Fatal("probe parse unexpectedly succeeded; use a different malformed expression")
	}
	_, err := webhookfilter.Compile(bad)
	if err == nil {
		t.Fatal("expected a compile error for malformed syntax")
	}
	if want := "invalid filter expression: " + parseErr.Error(); err.Error() != want {
		t.Fatalf("Compile error = %q, want the parser's message verbatim: %q", err.Error(), want)
	}
}

func TestCompile_DeniedCalls(t *testing.T) {
	tests := []struct {
		name       string
		expression string
	}{
		{"builtin", `len(project) > 0`},
		{"method call", `project.lower() == "x"`},
		{"nested builtin in comparison", `count(project) == 1`},
		{"call inside predicate", `filter(project.changes, # > 1)`},
		{"call in let value", `let n = len(project); n > 0`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := webhookfilter.Compile(tt.expression)
			if err == nil {
				t.Fatalf("Compile(%q) succeeded, want sandbox rejection", tt.expression)
			}
			if !strings.Contains(err.Error(), "function calls are not allowed") {
				t.Fatalf("Compile(%q) error = %v, want sandbox rejection message", tt.expression, err)
			}
		})
	}
}

func TestCompile_NonBooleanResultRejected(t *testing.T) {
	tests := []struct {
		name       string
		expression string
	}{
		{"bare member", `project.path_with_namespace`},
		{"bare identifier", `event`},
		{"arithmetic", `project.stars + 1`},
		{"string literal", `"merge_request"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := webhookfilter.Compile(tt.expression)
			if err == nil {
				t.Fatalf("Compile(%q) succeeded, want a non-boolean-result rejection", tt.expression)
			}
			if !strings.Contains(err.Error(), "must evaluate to a boolean") {
				t.Fatalf("Compile(%q) error = %v, want boolean-result message", tt.expression, err)
			}
		})
	}
}

func TestEval(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		payload    string
		event      string
		want       bool
		wantErr    bool
		errText    string
	}{
		{
			name:       "canonical match",
			expression: canonicalExpression,
			payload:    canonicalPayload,
			event:      "merge_request",
			want:       true,
		},
		{
			name:       "event mismatch is false without error",
			expression: canonicalExpression,
			payload:    canonicalPayload,
			event:      "note",
			want:       false,
		},
		{
			name:       "field mismatch is false without error",
			expression: canonicalExpression,
			payload:    `{"project": {"path_with_namespace": "team/web"}}`,
			event:      "merge_request",
			want:       false,
		},
		{
			name:       "single quoted literal matches",
			expression: `event == 'merge_request'`,
			payload:    `{}`,
			event:      "merge_request",
			want:       true,
		},
		{
			name:       "numeric comparison over JSON float",
			expression: `project.stars > 100`,
			payload:    canonicalPayload,
			event:      "merge_request",
			want:       true,
		},
		{
			name:       "negation",
			expression: `!project.archived`,
			payload:    canonicalPayload,
			event:      "merge_request",
			want:       true,
		},
		{
			name:       "let alias",
			expression: `let p = project; p.path_with_namespace == "team/api"`,
			payload:    canonicalPayload,
			event:      "merge_request",
			want:       true,
		},
		{
			name:       "missing top-level field is an eval error",
			expression: canonicalExpression,
			payload:    `{"event_source": "gitlab"}`,
			event:      "merge_request",
			wantErr:    true,
			errText:    `field "project" not found in payload`,
		},
		{
			name:       "missing nested field is an eval error",
			expression: canonicalExpression,
			payload:    `{"project": {"name": "api"}}`,
			event:      "merge_request",
			wantErr:    true,
			errText:    `field "project.path_with_namespace" not found in payload`,
		},
		{
			name:       "nil deref is an eval error",
			expression: `project.owner.name == "alice"`,
			payload:    `{"project": {"owner": null}}`,
			event:      "merge_request",
			wantErr:    true,
		},
		{
			name:       "type mismatch is an eval error",
			expression: `project.stars > "many"`,
			payload:    canonicalPayload,
			event:      "merge_request",
			wantErr:    true,
		},
		{
			name:       "non-boolean runtime result is an eval error",
			expression: `event == "note" ? "a" : "b"`,
			payload:    `{}`,
			event:      "note",
			wantErr:    true,
			errText:    "want bool",
		},
		{
			name:       "optional chain tolerates missing root",
			expression: `nope?.x == "y"`,
			payload:    canonicalPayload,
			event:      "merge_request",
			want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := webhookfilter.Compile(tt.expression)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			got, err := compiled.Eval(context.Background(), deliveryEnv(t, tt.payload, tt.event))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Eval = %v, nil error, want error", got)
				}
				if tt.errText != "" && !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("Eval error = %v, want it to contain %q", err, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Eval = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEval_CancelledContext(t *testing.T) {
	compiled, err := webhookfilter.Compile(`event == "merge_request"`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = compiled.Eval(ctx, deliveryEnv(t, `{}`, "merge_request"))
	if err == nil {
		t.Fatal("Eval with a cancelled context = nil error, want error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Eval error = %v, want it to wrap context.Canceled", err)
	}
}

func TestEval_ZeroCompiledIsAnError(t *testing.T) {
	var compiled webhookfilter.Compiled
	if _, err := compiled.Eval(context.Background(), map[string]any{}); err == nil {
		t.Fatal("Eval on a zero Compiled = nil error, want an error")
	}
}

func TestDryRun(t *testing.T) {
	compiled, err := webhookfilter.Compile(canonicalExpression)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	tests := []struct {
		name          string
		samplePayload string
		event         string
		want          webhookfilter.VerdictOutcome
		wantMessage   string
	}{
		{
			name:          "match",
			samplePayload: canonicalPayload,
			event:         "merge_request",
			want:          webhookfilter.VerdictMatch,
		},
		{
			name:          "ignored on non-match",
			samplePayload: canonicalPayload,
			event:         "note",
			want:          webhookfilter.VerdictIgnored,
		},
		{
			name:          "error on malformed sample JSON",
			samplePayload: `{"project":`,
			event:         "merge_request",
			want:          webhookfilter.VerdictError,
			wantMessage:   "not a JSON object",
		},
		{
			name:          "error on missing field",
			samplePayload: `{"event_source": "gitlab"}`,
			event:         "merge_request",
			want:          webhookfilter.VerdictError,
			wantMessage:   `not found`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verdict := webhookfilter.DryRun(compiled, []byte(tt.samplePayload), tt.event)
			if verdict.Outcome != tt.want {
				t.Fatalf("DryRun outcome = %v (message %q), want %v", verdict.Outcome, verdict.Message, tt.want)
			}
			if tt.wantMessage != "" && !strings.Contains(verdict.Message, tt.wantMessage) {
				t.Fatalf("DryRun message = %q, want it to contain %q", verdict.Message, tt.wantMessage)
			}
			if tt.wantMessage == "" && verdict.Message != "" {
				t.Fatalf("DryRun message = %q, want empty", verdict.Message)
			}
		})
	}
}
