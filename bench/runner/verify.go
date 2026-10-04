package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw-bench/driver"
	"github.com/oniharnantyo/onclaw-bench/scoreboard"
	"github.com/oniharnantyo/onclaw-bench/suite"
)

// verifyEnv carries everything a check needs: the pooled task transcript
// (concatenated measured answers + every tool call) and the handles the
// stateful checks (api_effect, judge) use.
type verifyEnv struct {
	// answer is every measured turn's answer text, newline-joined; answer
	// probes match over the whole concatenation.
	answer string
	// toolCalls pools every measured turn's tool calls in order.
	toolCalls []driver.ToolCall

	client     *driver.Client
	adminToken string
	workspace  string
	agent      string

	// judge is nil when the judge environment is not configured; judgeErr
	// then carries the actionable message surfaced as task infra-error.
	judge    *judgeClient
	judgeErr error
}

// runChecks evaluates a task's checks in order. All checks are AND-ed by the
// caller; a non-nil error return is task-level infra (a check could not be
// evaluated at all — transport failure, judge environment missing), distinct
// from a check evaluating to passed=false.
func runChecks(ctx context.Context, checks []suite.Check, env *verifyEnv) ([]scoreboard.CheckResult, error) {
	results := make([]scoreboard.CheckResult, 0, len(checks))
	for i := range checks {
		res, err := runCheck(ctx, &checks[i], env)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return results, nil
}

func runCheck(ctx context.Context, c *suite.Check, env *verifyEnv) (scoreboard.CheckResult, error) {
	switch c.Kind {
	case suite.CheckAnswerContains:
		pass, detail := checkAnswerContains(c, env.answer)
		return scoreboard.CheckResult{Kind: c.Kind, Passed: pass, Detail: detail}, nil
	case suite.CheckAnswerNotContains:
		pass, detail := checkAnswerNotContains(c, env.answer)
		return scoreboard.CheckResult{Kind: c.Kind, Passed: pass, Detail: detail}, nil
	case suite.CheckToolCalled:
		pass, detail := checkToolCalled(c, env.toolCalls)
		return scoreboard.CheckResult{Kind: c.Kind, Passed: pass, Detail: detail}, nil
	case suite.CheckToolNotCalled:
		pass, detail := checkToolNotCalled(c, env.toolCalls)
		return scoreboard.CheckResult{Kind: c.Kind, Passed: pass, Detail: detail}, nil
	case suite.CheckTranscriptFileWrite:
		pass, detail := checkTranscriptFileWrite(c, env.toolCalls)
		return scoreboard.CheckResult{Kind: c.Kind, Passed: pass, Detail: detail}, nil
	case suite.CheckAPICEffect:
		return checkAPICEffect(ctx, c, env)
	case suite.CheckJudge:
		return checkJudge(ctx, c, env)
	default:
		// Unreachable: Lint rejects unknown kinds before anything runs.
		return scoreboard.CheckResult{Kind: c.Kind, Passed: false, Detail: fmt.Sprintf("unknown check kind %q", c.Kind)}, nil
	}
}

// checkAnswerContains implements the alias-group semantics: any_of is
// satisfied when at least one of its groups hits; all_of when every group
// hits; a group hits when at least one of its aliases appears (normalized
// substring) in the pooled answer. Both fields set: both must hold.
func checkAnswerContains(c *suite.Check, answer string) (bool, string) {
	normalized := suite.NormalizeText(answer)

	anyOK := len(c.AnyOf) == 0
	for _, group := range c.AnyOf {
		if groupHits(normalized, group) {
			anyOK = true
			break
		}
	}
	allOK := true
	detail := ""
	if anyOK {
		// Report the first all_of group that missed (any_of misses carry
		// their own detail below).
		for gi, group := range c.AllOf {
			if !groupHits(normalized, group) {
				allOK = false
				detail = fmt.Sprintf("no alias of all_of group %d found in the answer", gi)
				break
			}
		}
	} else {
		detail = "no any_of group matched the answer"
	}
	return anyOK && allOK, detail
}

func groupHits(normalized string, group []string) bool {
	for _, alias := range group {
		if strings.Contains(normalized, suite.NormalizeText(alias)) {
			return true
		}
	}
	return false
}

func checkAnswerNotContains(c *suite.Check, answer string) (bool, string) {
	normalized := suite.NormalizeText(answer)
	var leaked []string
	for _, token := range c.NoneOf {
		if strings.Contains(normalized, suite.NormalizeText(token)) {
			leaked = append(leaked, token)
		}
	}
	if len(leaked) > 0 {
		return false, fmt.Sprintf("forbidden token(s) present in the answer: %s", strings.Join(leaked, ", "))
	}
	return true, ""
}

func checkToolCalled(c *suite.Check, calls []driver.ToolCall) (bool, string) {
	candidates := candidateTools(c)
	for _, call := range calls {
		if !anyToolMatches(candidates, call.Name) {
			continue
		}
		if c.ArgsContains != "" && !strings.Contains(suite.NormalizeText(call.ArgsJSON), suite.NormalizeText(c.ArgsContains)) {
			continue
		}
		if c.OutputContains != "" && !strings.Contains(suite.NormalizeText(call.Output), suite.NormalizeText(c.OutputContains)) {
			continue
		}
		return true, ""
	}
	probes := ""
	switch {
	case c.ArgsContains != "" && c.OutputContains != "":
		probes = fmt.Sprintf(" with args containing %q and output containing %q", c.ArgsContains, c.OutputContains)
	case c.ArgsContains != "":
		probes = fmt.Sprintf(" with args containing %q", c.ArgsContains)
	case c.OutputContains != "":
		probes = fmt.Sprintf(" with output containing %q", c.OutputContains)
	}
	return false, fmt.Sprintf("no %s tool call%s in the transcript", strings.Join(candidates, " or "), probes)
}

// candidateTools returns the check's accepted tool names: the single Tool
// when set, otherwise the ToolAny alternative set (lint enforces exactly one
// of the two being set).
func candidateTools(c *suite.Check) []string {
	if c.Tool != "" {
		return []string{c.Tool}
	}
	return c.ToolAny
}

func anyToolMatches(candidates []string, wireName string) bool {
	for _, tool := range candidates {
		if toolNameMatches(tool, wireName) {
			return true
		}
	}
	return false
}

func checkToolNotCalled(c *suite.Check, calls []driver.ToolCall) (bool, string) {
	for _, call := range calls {
		if toolNameMatches(call.Name, c.Tool) {
			return false, fmt.Sprintf("tool %s was called (output: %s)", c.Tool, truncateText(call.Output, 200))
		}
	}
	return true, ""
}

// writeToolNames are the wire names a filesystem-write tool call carries.
// "files.write" is the suite's dotted convention; "write_file" is the
// middleware's registered tool name (eino filesystem middleware,
// ToolNameWriteFile).
var writeToolNames = map[string]bool{"files.write": true, "write_file": true}

// checkTranscriptFileWrite asserts a filesystem-write tool call wrote Path
// (normalized exact or suffix match — the jailed-dir mount prefixes absolute
// paths, so the suite's relative path is the suffix) with ContentContains
// probing the written content when set.
func checkTranscriptFileWrite(c *suite.Check, calls []driver.ToolCall) (bool, string) {
	for _, call := range calls {
		if !writeToolNames[call.Name] {
			continue
		}
		path, content, ok := parseWriteArgs(call.ArgsJSON)
		if !ok {
			continue
		}
		if !writePathMatches(path, c.Path) {
			continue
		}
		if c.ContentContains != "" && !strings.Contains(suite.NormalizeText(content), suite.NormalizeText(c.ContentContains)) {
			continue
		}
		return true, ""
	}
	return false, fmt.Sprintf("no filesystem-write tool call wrote %q", c.Path)
}

// parseWriteArgs decodes the write tool's argument shape. The middleware
// registers {"file_path", "content"}; "path" is tolerated as a fallback so a
// shape drift degrades to a check miss, not a parse error.
func parseWriteArgs(argsJSON string) (path, content string, ok bool) {
	var args struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", "", false
	}
	path = args.FilePath
	if path == "" {
		path = args.Path
	}
	if path == "" {
		return "", "", false
	}
	return path, args.Content, true
}

// writePathMatches compares the call's written path with the check's path on
// normalized text: exact, or the check path is a path suffix of the written
// one (the model writes under the jailed mount, the suite names the file).
func writePathMatches(written, want string) bool {
	w, g := suite.NormalizeText(written), suite.NormalizeText(want)
	return w == g || strings.HasSuffix(w, "/"+g)
}

func checkAPICEffect(ctx context.Context, c *suite.Check, env *verifyEnv) (scoreboard.CheckResult, error) {
	path := strings.NewReplacer(
		suite.PlaceholderWorkspace, env.workspace,
		suite.PlaceholderAgent, env.agent,
	).Replace(c.Path)

	status, body, err := env.client.RawResponse(ctx, c.Method, path, env.adminToken, nil)
	if err != nil {
		return scoreboard.CheckResult{}, fmt.Errorf("api_effect %s %s: %w", c.Method, path, err)
	}
	if status != c.Status {
		return scoreboard.CheckResult{Kind: c.Kind, Passed: false, Detail: fmt.Sprintf("%s %s: got HTTP %d, want %d", c.Method, path, status, c.Status)}, nil
	}
	raw := string(body)
	for _, probe := range c.JSONContains {
		if !strings.Contains(raw, probe) {
			return scoreboard.CheckResult{Kind: c.Kind, Passed: false, Detail: fmt.Sprintf("%s %s: probe %q not found in the response body", c.Method, path, probe)}, nil
		}
	}
	return scoreboard.CheckResult{Kind: c.Kind, Passed: true, Detail: fmt.Sprintf("%s %s -> %d", c.Method, path, status)}, nil
}

// checkJudge runs the pinned-rubric LLM judge. A missing judge environment is
// task infra (the check could not run), not a check failure — design D5
// keeps the default lanes judge-free, so only suites that set
// judge_allowed: true ever reach this path.
func checkJudge(ctx context.Context, c *suite.Check, env *verifyEnv) (scoreboard.CheckResult, error) {
	if env.judge == nil {
		return scoreboard.CheckResult{}, env.judgeErr
	}
	verdict, reason, err := env.judge.grade(ctx, c.Rubric, env.answer, env.toolCalls)
	if err != nil {
		return scoreboard.CheckResult{}, fmt.Errorf("judge: %w", err)
	}
	detail := reason
	if detail == "" {
		detail = fmt.Sprintf("judge verdict %v", verdict)
	}
	return scoreboard.CheckResult{Kind: c.Kind, Passed: verdict, Detail: detail}, nil
}

// ---------------------------------------------------------------------------
// Tool-name equivalence
// ---------------------------------------------------------------------------

// wireToolAliases maps the suite's dotted tool convention to the wire names
// the harness's tool cards carry, so a check written against either name
// matches. Matching is symmetric: the map is consulted from both sides, and
// an exact name match always wins.
var wireToolAliases = map[string][]string{
	"files.write":   {"write_file"},
	"files.read":    {"read_file"},
	"shell":         {"execute"},
	"subagents":     {"agent"},
	"web.search":    {"web_search", "web.fetch"},
	"web.fetch":     {"web_fetch"},
	"memory.search": {"memory"},
}

func toolNameMatches(checkTool, wireName string) bool {
	checkTool, wireName = strings.TrimSpace(strings.ToLower(checkTool)), strings.TrimSpace(strings.ToLower(wireName))
	if checkTool == wireName {
		return true
	}
	for _, alt := range wireToolAliases[checkTool] {
		if alt == wireName {
			return true
		}
	}
	for _, alt := range wireToolAliases[wireName] {
		if alt == checkTool {
			return true
		}
	}
	return false
}

func truncateText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// ---------------------------------------------------------------------------
// Judge client (OpenAI-compatible /chat/completions)
// ---------------------------------------------------------------------------

// Judge env vars. The judge model is pinned separately from the
// agent-under-test (design D5) so judge drift cannot masquerade as harness
// improvement.
const (
	EnvJudgeKey     = "ONCLAW_BENCH_JUDGE_KEY"
	EnvJudgeBaseURL = "ONCLAW_BENCH_JUDGE_BASE_URL"

	DefaultJudgeBaseURL = "https://api.z.ai/api/paas/v4"
	DefaultJudgeModel   = "glm-5.3-flash"
)

const judgeSystemPrompt = `You are a strict grader for an agent benchmark. You are given a rubric and the agent's task transcript. Judge ONLY by the rubric — do not reward partial credit the rubric does not ask for. Reply with exactly PASS or FAIL on the first line, then a one-line reason on the second line. No other text.`

// judgeClient posts grading requests to an OpenAI-compatible
// /chat/completions endpoint.
type judgeClient struct {
	baseURL string
	model   string
	key     string
	http    *http.Client
}

// newJudgeClient reads the judge environment. A missing key is not an error
// here — judge-free suites must still run — so it returns the client (or
// nil) plus the actionable error surfaced only when a judge check executes.
func newJudgeClient(model string) (*judgeClient, error) {
	key := strings.TrimSpace(os.Getenv(EnvJudgeKey))
	if key == "" {
		return nil, fmt.Errorf("judge check requires the judge API key: export %s=<key for the judge provider> (and optionally %s; default %s), or pin the suite deterministic — the judge model is pinned separately from the agent-under-test", EnvJudgeKey, EnvJudgeBaseURL, DefaultJudgeBaseURL)
	}
	if model == "" {
		model = DefaultJudgeModel
	}
	baseURL := strings.TrimSpace(os.Getenv(EnvJudgeBaseURL))
	if baseURL == "" {
		baseURL = DefaultJudgeBaseURL
	}
	return &judgeClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		key:     key,
		http:    &http.Client{Timeout: 2 * time.Minute},
	}, nil
}

// grade sends rubric + transcript and parses the verdict: the first line
// must say PASS or FAIL; the rest is the reason.
func (j *judgeClient) grade(ctx context.Context, rubric, answer string, calls []driver.ToolCall) (passed bool, reason string, err error) {
	body := map[string]any{
		"model": j.model,
		"messages": []map[string]string{
			{"role": "system", "content": judgeSystemPrompt},
			{"role": "user", "content": buildJudgePrompt(rubric, answer, calls)},
		},
		"temperature": 0,
		"stream":      false,
	}

	status, raw, err := postJSON(ctx, j.http, j.baseURL+"/chat/completions", j.key, body)
	if err != nil {
		return false, "", err
	}
	if status < 200 || status > 299 {
		return false, "", fmt.Errorf("judge endpoint returned HTTP %d: %s", status, truncateText(string(raw), 200))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return false, "", fmt.Errorf("decoding judge response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return false, "", fmt.Errorf("judge response carried no choices")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)

	firstLine := content
	if idx := strings.IndexAny(content, "\r\n"); idx >= 0 {
		firstLine = content[:idx]
	}
	switch {
	case strings.Contains(strings.ToUpper(firstLine), "PASS"):
		return true, content, nil
	case strings.Contains(strings.ToUpper(firstLine), "FAIL"):
		return false, content, nil
	default:
		return false, fmt.Sprintf("unparseable judge verdict: %s", truncateText(content, 300)), nil
	}
}

// buildJudgePrompt assembles the user message: the pinned rubric plus the
// task transcript (answers + tool-call cards), size-capped.
func buildJudgePrompt(rubric, answer string, calls []driver.ToolCall) string {
	var b strings.Builder
	b.WriteString("RUBRIC:\n")
	b.WriteString(strings.TrimSpace(rubric))
	b.WriteString("\n\nTRANSCRIPT:\n")
	b.WriteString("ANSWER:\n")
	b.WriteString(answer)
	b.WriteString("\nTOOL CALLS:\n")
	if len(calls) == 0 {
		b.WriteString("(none)\n")
	}
	for _, call := range calls {
		fmt.Fprintf(&b, "- %s(%s) -> %s\n", call.Name, truncateText(call.ArgsJSON, 500), truncateText(call.Output, 500))
	}
	out := b.String()
	const maxPrompt = 48_000
	if len(out) > maxPrompt {
		return out[:maxPrompt] + "\n…(transcript truncated)"
	}
	return out
}

// postJSON is the judge client's tiny HTTP helper (the driver's client is
// bound to the onclaw server, not to the judge provider).
func postJSON(ctx context.Context, client *http.Client, url, key string, body any) (int, []byte, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(buf)))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}
