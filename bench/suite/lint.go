package suite

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Issue is one authoring-lint finding. TaskID is empty for suite-level
// issues; Field is a dotted/indexed path into the suite (e.g.
// "tasks[2].verify[0].any_of[1]").
type Issue struct {
	TaskID  string `json:"task_id,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Lint validates a parsed suite and applies the ABC authoring lint (design
// D5 + Risks): schema shape, grader consistency, and the unintervention
// check. It is a pure function over the suite — no I/O — so lane-file tests
// assert len(Lint(suite)) == 0 before a lane ever costs live tokens.
//
// Groups enforced, in output order (suite-level first, then per task in
// file order):
//
//  1. Schema: non-empty unique kebab-case ids; lane in the canonical set;
//     at least one task per suite; every task has at least one turn and one
//     check; tools_used non-empty and dotted-lowercase shaped; per-variant
//     required fields on setup steps and checks.
//  2. Grader consistency: answer_contains groups non-empty; none_of tokens
//     must not appear (normalized substring) inside any any_of/all_of alias
//     group of the same task — a task that can both pass and fail can never
//     gate; judge checks carry a non-empty rubric; a judge check is a
//     failure unless the suite sets judge_allowed: true.
//  3. Unintervention: no expected literal from the checks (any_of/all_of
//     aliases, none_of tokens, args_contains/output_contains/
//     content_contains/json_contains values) with length >= 12 characters
//     may appear in the task's own turns or summary text — a prompt that
//     contains its expected answer is not a measurement. Setup turns are
//     exempt: seeding context is the point of setup.
//  4. Placeholders and paths: hook http handler urls must contain the
//     {{control_url}} placeholder; hook command handler bodies must be
//     non-empty ({{control_file}} optional); api_effect paths must be
//     absolute API paths starting /api/ or /v1/.
func Lint(s TaskSuite) []Issue {
	var issues []Issue

	if strings.TrimSpace(s.SuiteVersion) == "" {
		issues = append(issues, Issue{
			Field:   "suite_version",
			Message: "suite_version is empty; all lane files in one run must carry the same non-empty version",
		})
	}
	if !IsCanonicalLane(s.Lane) {
		issues = append(issues, Issue{
			Field:   "lane",
			Message: fmt.Sprintf("lane %q is not one of the canonical lanes %v", s.Lane, CanonicalLanes()),
		})
	}
	if len(s.Tasks) == 0 {
		issues = append(issues, Issue{
			Field:   "tasks",
			Message: "suite has no tasks; a suite needs at least one task",
		})
	}

	seen := map[string]bool{}
	for ti := range s.Tasks {
		issues = append(issues, lintTask(&s.Tasks[ti], seen, s.JudgeAllowed)...)
	}
	return issues
}

var (
	kebabRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	// dottedToolRe: dotted-lowercase shaped tool id, e.g. "files.write" —
	// one or more lowercase segments joined by single dots, at least one dot.
	dottedToolRe = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)
)

func lintTask(t *Task, seen map[string]bool, judgeAllowed bool) []Issue {
	var issues []Issue
	id := t.ID

	switch {
	case id == "":
		issues = append(issues, Issue{Field: "id", Message: "task id is empty"})
	case !kebabRe.MatchString(id):
		issues = append(issues, Issue{Field: "id", Message: fmt.Sprintf("task id %q is not kebab-case (lowercase letters/digits separated by single hyphens)", id)})
	case seen[id]:
		issues = append(issues, Issue{Field: "id", Message: fmt.Sprintf("duplicate task id %q; ids must be unique within the suite", id)})
	}
	seen[id] = true

	if len(t.ToolsUsed) == 0 {
		issues = append(issues, Issue{TaskID: id, Field: "tools_used", Message: "tools_used is empty; record which tools the task exercises"})
	}
	for i, tool := range t.ToolsUsed {
		if !dottedToolRe.MatchString(tool) {
			issues = append(issues, Issue{TaskID: id, Field: fmt.Sprintf("tools_used[%d]", i), Message: fmt.Sprintf("tool %q is not dotted-lowercase shaped (e.g. \"files.write\")", tool)})
		}
	}

	if len(t.Turns) == 0 {
		issues = append(issues, Issue{TaskID: id, Field: "turns", Message: "task has no turns; the measured interaction needs at least one chat message"})
	}
	for i, turn := range t.Turns {
		if strings.TrimSpace(turn.Text) == "" {
			issues = append(issues, Issue{TaskID: id, Field: fmt.Sprintf("turns[%d].text", i), Message: "turn text is empty"})
		}
	}

	if len(t.Verify) == 0 {
		issues = append(issues, Issue{TaskID: id, Field: "verify", Message: "task has no checks; a task without a verifier cannot gate"})
	}
	for si := range t.Setup {
		issues = append(issues, lintSetupStep(id, &t.Setup[si], fmt.Sprintf("setup[%d]", si))...)
	}
	for ci := range t.Verify {
		issues = append(issues, lintCheck(id, &t.Verify[ci], fmt.Sprintf("verify[%d]", ci), judgeAllowed)...)
	}

	issues = append(issues, lintGraderConsistency(id, t)...)
	issues = append(issues, lintUnintervention(id, t)...)
	return issues
}

func lintSetupStep(taskID string, step *SetupStep, field string) []Issue {
	var issues []Issue
	fail := func(sub, format string, args ...any) {
		issues = append(issues, Issue{TaskID: taskID, Field: field + sub, Message: fmt.Sprintf(format, args...)})
	}

	switch step.Type {
	case StepChatTurns:
		if step.Actor == "" {
			fail(".actor", "chat_turns step requires the fixture actor name")
		}
		if step.Session == "" {
			fail(".session", "chat_turns step requires the logical session name")
		}
		if len(step.Turns) == 0 {
			fail(".turns", "chat_turns step has no turns to seed")
		}
	case StepAgentConfig:
		if step.ContextWindow != nil && *step.ContextWindow <= 0 {
			fail(".context_window", "context_window must be a positive token count when set")
		}
	case StepHook:
		if step.Name == "" {
			fail(".name", "hook step requires a hook name")
		}
		if step.Event == "" {
			fail(".event", "hook step requires the hook event (e.g. \"pre_tool_use\")")
		}
		if step.Handler == nil {
			fail(".handler", "hook step requires a handler")
			break
		}
		switch step.Handler.Type {
		case HandlerHTTP:
			if !strings.Contains(step.Handler.URL, PlaceholderControlURL) {
				fail(".handler.url", "hook http handler url must contain the %s placeholder so the runner serves it from the bench control server", PlaceholderControlURL)
			}
		case HandlerCommand:
			if strings.TrimSpace(step.Handler.Command) == "" {
				fail(".handler.command", "hook command handler requires a non-empty script body (the harness runs it via /bin/sh: exit 2 blocks, exit 0 allows)")
			}
		default:
			fail(".handler.type", "unsupported hook handler type %q; expected %q or %q", step.Handler.Type, HandlerHTTP, HandlerCommand)
		}
	case StepConnection:
		if step.Recipe == "" {
			fail(".recipe", "connection step requires the connection recipe")
		}
		if step.Name == "" {
			fail(".name", "connection step requires the connection name")
		}
	case StepWait:
		if step.Seconds <= 0 {
			fail(".seconds", "wait step requires a positive number of seconds")
		}
	default:
		fail(".type", "unknown setup step type %q; expected one of chat_turns, agent_config, hook, connection, wait", step.Type)
	}
	return issues
}

func lintCheck(taskID string, check *Check, field string, judgeAllowed bool) []Issue {
	var issues []Issue
	fail := func(sub, format string, args ...any) {
		issues = append(issues, Issue{TaskID: taskID, Field: field + sub, Message: fmt.Sprintf(format, args...)})
	}

	switch check.Kind {
	case CheckAnswerContains:
		if len(check.AnyOf) == 0 && len(check.AllOf) == 0 {
			fail("", "answer_contains requires at least one non-empty any_of or all_of alias group")
		}
		issues = append(issues, lintAliasGroups(taskID, check.AnyOf, field+".any_of")...)
		issues = append(issues, lintAliasGroups(taskID, check.AllOf, field+".all_of")...)
	case CheckAnswerNotContains:
		if len(check.NoneOf) == 0 {
			fail(".none_of", "answer_not_contains requires at least one forbidden token")
		}
		for i, token := range check.NoneOf {
			if strings.TrimSpace(token) == "" {
				fail(fmt.Sprintf(".none_of[%d]", i), "forbidden token is empty (an empty token matches every answer)")
			}
		}
	case CheckToolCalled:
		switch {
		case check.Tool != "" && len(check.ToolAny) > 0:
			fail(".tool_any", "tool_called accepts either tool or tool_any, not both")
		case check.Tool == "" && len(check.ToolAny) == 0:
			fail(".tool", "tool_called requires the tool name or a tool_any alternative set")
		}
		for i, tool := range check.ToolAny {
			if strings.TrimSpace(tool) == "" {
				fail(fmt.Sprintf(".tool_any[%d]", i), "tool_any entry is empty")
			}
		}
	case CheckToolNotCalled:
		if check.Tool == "" {
			fail(".tool", "tool_not_called requires the tool name")
		}
	case CheckTranscriptFileWrite:
		if check.Path == "" {
			fail(".path", "transcript_file_write requires the written file path")
		}
	case CheckAPICEffect:
		if check.Method == "" {
			fail(".method", "api_effect requires the HTTP method")
		}
		if check.Path == "" {
			fail(".path", "api_effect requires the API path")
		} else if !strings.HasPrefix(check.Path, "/api/") && !strings.HasPrefix(check.Path, "/v1/") {
			fail(".path", "api_effect path %q must be an absolute API path starting /api/ or /v1/", check.Path)
		}
		if check.Status == 0 {
			fail(".status", "api_effect requires the expected HTTP status")
		}
	case CheckJudge:
		if strings.TrimSpace(check.Rubric) == "" {
			fail(".rubric", "judge check requires a non-empty pinned rubric")
		}
		if !judgeAllowed {
			fail("", "judge check present but the suite does not set judge_allowed: true; a judge check is a lint failure unless the suite opts in (deterministic graders first)")
		}
	default:
		fail(".kind", "unknown check kind %q; expected one of answer_contains, answer_not_contains, tool_called, tool_not_called, transcript_file_write, api_effect, judge", check.Kind)
	}
	return issues
}

func lintAliasGroups(taskID string, groups [][]string, field string) []Issue {
	var issues []Issue
	for gi, group := range groups {
		if len(group) == 0 {
			issues = append(issues, Issue{TaskID: taskID, Field: fmt.Sprintf("%s[%d]", field, gi), Message: "alias group is empty"})
			continue
		}
		for ai, alias := range group {
			if strings.TrimSpace(alias) == "" {
				issues = append(issues, Issue{TaskID: taskID, Field: fmt.Sprintf("%s[%d][%d]", field, gi, ai), Message: "alias is empty (an empty alias matches every answer)"})
			}
		}
	}
	return issues
}

// expectedLiteral is one probe string from the checks that must not leak
// into the task's own prompt material.
type expectedLiteral struct {
	value string
	field string
}

func collectExpectedLiterals(t *Task) []expectedLiteral {
	var literals []expectedLiteral
	add := func(value, field string) {
		if value != "" {
			literals = append(literals, expectedLiteral{value: value, field: field})
		}
	}
	for ci := range t.Verify {
		c := &t.Verify[ci]
		cf := fmt.Sprintf("verify[%d]", ci)
		switch c.Kind {
		case CheckAnswerContains:
			for gi, group := range c.AnyOf {
				for ai, alias := range group {
					add(alias, fmt.Sprintf("%s.any_of[%d][%d]", cf, gi, ai))
				}
			}
			for gi, group := range c.AllOf {
				for ai, alias := range group {
					add(alias, fmt.Sprintf("%s.all_of[%d][%d]", cf, gi, ai))
				}
			}
		case CheckAnswerNotContains:
			for ti, token := range c.NoneOf {
				add(token, fmt.Sprintf("%s.none_of[%d]", cf, ti))
			}
		case CheckToolCalled:
			add(c.ArgsContains, cf+".args_contains")
			add(c.OutputContains, cf+".output_contains")
		case CheckTranscriptFileWrite:
			add(c.ContentContains, cf+".content_contains")
		case CheckAPICEffect:
			for ji, probe := range c.JSONContains {
				add(probe, fmt.Sprintf("%s.json_contains[%d]", cf, ji))
			}
		}
	}
	return literals
}

// minLiteralRunes: expected literals shorter than this are skipped by the
// unintervention check to avoid false positives on common short words.
const minLiteralRunes = 12

func lintUnintervention(taskID string, t *Task) []Issue {
	var issues []Issue
	for _, lit := range collectExpectedLiterals(t) {
		if utf8.RuneCountInString(lit.value) < minLiteralRunes {
			continue
		}
		needle := NormalizeText(lit.value)
		if needle == "" {
			continue
		}
		found := false
		for ti, turn := range t.Turns {
			if strings.Contains(NormalizeText(turn.Text), needle) {
				issues = append(issues, Issue{TaskID: taskID, Field: lit.field, Message: fmt.Sprintf("expected literal %q appears in the task's own turn %d; a prompt that contains its expected answer is not a measurement (unintervention)", lit.value, ti)})
				found = true
				break
			}
		}
		if found {
			continue
		}
		if strings.Contains(NormalizeText(t.Summary), needle) {
			issues = append(issues, Issue{TaskID: taskID, Field: lit.field, Message: fmt.Sprintf("expected literal %q appears in the task summary; a prompt that contains its expected answer is not a measurement (unintervention)", lit.value)})
		}
	}
	return issues
}

type tokenRef struct {
	value string
	field string
}

func lintGraderConsistency(taskID string, t *Task) []Issue {
	var forbidden []tokenRef
	var aliases []tokenRef
	for ci := range t.Verify {
		c := &t.Verify[ci]
		cf := fmt.Sprintf("verify[%d]", ci)
		switch c.Kind {
		case CheckAnswerNotContains:
			for ti, token := range c.NoneOf {
				if strings.TrimSpace(token) != "" {
					forbidden = append(forbidden, tokenRef{token, fmt.Sprintf("%s.none_of[%d]", cf, ti)})
				}
			}
		case CheckAnswerContains:
			for gi, group := range c.AnyOf {
				for ai, alias := range group {
					if strings.TrimSpace(alias) != "" {
						aliases = append(aliases, tokenRef{alias, fmt.Sprintf("%s.any_of[%d][%d]", cf, gi, ai)})
					}
				}
			}
			for gi, group := range c.AllOf {
				for ai, alias := range group {
					if strings.TrimSpace(alias) != "" {
						aliases = append(aliases, tokenRef{alias, fmt.Sprintf("%s.all_of[%d][%d]", cf, gi, ai)})
					}
				}
			}
		}
	}

	var issues []Issue
	for _, f := range forbidden {
		needle := NormalizeText(f.value)
		if needle == "" {
			continue
		}
		for _, a := range aliases {
			if strings.Contains(NormalizeText(a.value), needle) {
				issues = append(issues, Issue{TaskID: taskID, Field: f.field, Message: fmt.Sprintf("forbidden token %q appears inside expected alias %q (%s); the task can both pass and fail and can never gate (grader consistency)", f.value, a.value, a.field)})
			}
		}
	}
	return issues
}
