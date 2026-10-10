// Package runner executes bench task suites against a running OnClaw server:
// it loads + lints suites, provisions the fixture through the public API
// (driver), drives every task's scripted setup and measured turns through the
// real chat path, verifies each task's checks, classifies infrastructure
// failures distinctly from task failures, and assembles the scoreboard.
//
// The runner is the only place the bench owns execution policy (design D4 +
// Risks): seeds run sequentially, infra-errors retry within a bounded budget
// with exponential backoff, and task failures are recorded — never retried —
// because the gate, not the runner, decides.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw-bench/driver"
	"github.com/oniharnantyo/onclaw-bench/scoreboard"
	"github.com/oniharnantyo/onclaw-bench/suite"
)

// runStatusCompleted is the /v1 run status a turn must return; anything else
// fails the task unless it is an infra run status (see isInfraRunStatus).
const runStatusCompleted = "completed"

// runStatusFailSignature is the recorded-error fragment of the server-side
// run-failure classification: the endpoint accepted the turn but the run came
// back non-completed — the dead/drained-endpoint signature the circuit
// breaker counts on (after the retry budget exhausts).
const runStatusFailSignature = "returned run status"

// isInfraRunStatus reports whether a /v1 run status marks a run that died at
// the provider rather than a turn the gate owns. "failed" is the crash/quota
// signature (the run errored server-side); "cancelled" is a stop race. Both
// are infrastructure and retry within the per-(task,seed) budget. prompt_blocked
// and any other terminal status are real task outcomes and stay task failures —
// hooks are soft gates, so a blocked turn is the measure's business.
func isInfraRunStatus(status string) bool {
	return status == "failed" || status == "cancelled"
}

// providerDeadStreakLimit is the consecutive server-side run failures that
// trip the circuit breaker. Six single-fail tasks in a row cannot plausibly
// be a live endpoint's capability profile — run-status failures mean the
// agent run errored server-side, not that the answer was wrong.
const providerDeadStreakLimit = 6

// errProviderDead aborts the run (exit 1, no scoreboard written) when the
// breaker trips: writing a scoreboard full of 0% lanes invites gating on
// garbage, which is how the Opencode-zen outage cost a full cycle.
var errProviderDead = errors.New("provider circuit breaker tripped")

// command-hook artifact naming: the temp script per hook step and the
// per-hook control file created only when the script body references
// {{control_file}}.
const (
	hookScriptPrefix   = "onclaw-bench-hook-"
	controlFilePattern = "onclaw-bench-block-*.txt"
	hookScriptPerm     = 0o755
)

// backoffCap bounds one retry sleep no matter how many attempts accumulated.
const backoffCap = 60 * time.Second

// Options parameterize one bench run. See the README protocol for the
// operator-facing meanings.
type Options struct {
	BaseURL       string
	AdminEmail    string
	AdminPassword string
	WorkspaceSlug string
	AgentSlug     string
	// Model pins the fixture agent's model — the scoreboard's model pin.
	// Required except in score-only mode (where the stored agent's model is
	// the pin).
	Model string
	// ContextWindow optionally pins the agent's context window for the whole
	// run; nil leaves the server default. Tasks that override it restore to
	// this pin afterwards. When nil, a task override cannot be cleared
	// through the public API (the agent PATCH cannot unset), so a run whose
	// suites override context_window is aborted fail-fast unless the pin is
	// set (ensureContextWindowPinned).
	ContextWindow *int

	// SuiteSpec is the suite file path or directory (see LoadSuites).
	SuiteSpec string
	// Out is the scoreboard JSON path.
	Out string
	// Seeds is the number of sequential seeds per task (>= 1).
	Seeds int
	// Retries is the per-(task,seed) infra-error retry budget.
	Retries int
	// Backoff is the exponential backoff base (capped at 60s).
	Backoff time.Duration

	// DryRun loads + lints + plans and emits a structural scoreboard; no
	// server contact, no model calls.
	DryRun bool
	// SeedOnly provisions the fixture and exits.
	SeedOnly bool
	// ScoreOnly skips provisioning (the workspace must already exist) and
	// runs the scored tasks directly.
	ScoreOnly bool

	// Resume continues a prior aborted run: (task, seed) results already in
	// the progress file (Out + ".progress.jsonl") are loaded and skipped, new
	// results append, and the assembled scoreboard covers both. Without it a
	// fresh run truncates the progress file. Pairs with the circuit breaker +
	// an external retry loop so provider flaps cost minutes, not cycles.
	Resume bool

	// JudgeModel pins the judge check's model (default glm-5.3-flash);
	// separate from the agent-under-test pin (design D5).
	JudgeModel string

	// Optional provider bootstrap knobs passed through to driver.Provision.
	ProviderAPIKeyEnv string
	ProviderType      string
	ProviderBaseURL   string
	ProviderName      string
}

// Run executes one bench run per opts and returns the assembled scoreboard
// (also written to opts.Out for the scored modes). An error return is an
// operational failure (bad options, lint issues, unreachable server) — task
// failures are data inside the scoreboard, never a Run error.
func Run(ctx context.Context, opts Options) (*scoreboard.Scoreboard, error) {
	if err := validateOptions(&opts); err != nil {
		return nil, err
	}

	// Seed-only provisions and exits; it needs no suites.
	var suites []suite.TaskSuite
	if !opts.SeedOnly {
		var err error
		suites, err = LoadSuites(opts.SuiteSpec)
		if err != nil {
			return nil, err
		}
		// Fail fast before anything is provisioned or planned: a task that
		// overrides context_window without an explicit pin is unrestorable
		// (the agent PATCH cannot unset context_window).
		if err := ensureContextWindowPinned(opts, suites); err != nil {
			return nil, err
		}
	}

	if opts.DryRun {
		return dryRun(opts, suites)
	}

	// Live paths from here: provision (or reuse) the fixture.
	var handles *driver.Handles
	var err error
	switch {
	case opts.ScoreOnly:
		handles, err = provisionScoreOnly(ctx, opts)
		if err != nil {
			return nil, err
		}
	default:
		handles, err = driver.Provision(ctx, driver.Options{
			BaseURL:           opts.BaseURL,
			AdminEmail:        opts.AdminEmail,
			AdminPassword:     opts.AdminPassword,
			WorkspaceSlug:     opts.WorkspaceSlug,
			AgentSlug:         opts.AgentSlug,
			Model:             opts.Model,
			ContextWindow:     opts.ContextWindow,
			ProviderAPIKeyEnv: opts.ProviderAPIKeyEnv,
			ProviderType:      opts.ProviderType,
			ProviderBaseURL:   opts.ProviderBaseURL,
			ProviderName:      opts.ProviderName,
		})
		if err != nil {
			return nil, err
		}
	}
	for _, w := range handles.Warnings {
		fmt.Printf("warning: %s\n", w)
	}

	if opts.SeedOnly {
		fmt.Printf("Seed: workspace=%s agent=%s model=%s reused=%v\n",
			handles.Workspace.Slug, handles.Agent.Slug, handles.Agent.Model, handles.Reused)
		return nil, nil
	}

	e, err := newExecutor(opts, handles, suites[0].SuiteVersion)
	if err != nil {
		return nil, err
	}
	defer e.close()

	results, err := e.executeAll(ctx, suites, opts)
	if err != nil {
		return nil, err
	}
	board, err := e.assemble(opts, results)
	if err != nil {
		return nil, err
	}
	return board, nil
}

func validateOptions(opts *Options) error {
	switch {
	case opts.SeedOnly && opts.ScoreOnly:
		return fmt.Errorf("--seed-only and --score-only are mutually exclusive")
	case opts.SeedOnly && opts.DryRun:
		return fmt.Errorf("--seed-only and --dry-run are mutually exclusive")
	case opts.ScoreOnly && opts.DryRun:
		return fmt.Errorf("--score-only and --dry-run are mutually exclusive")
	}
	if opts.Seeds < 1 && !opts.SeedOnly {
		// Seed-only never runs tasks, so it carries no seed count.
		return fmt.Errorf("seeds must be at least 1")
	}
	if opts.Retries < 0 {
		opts.Retries = 0
	}
	if opts.Backoff <= 0 {
		opts.Backoff = 5 * time.Second
	}
	if !opts.SeedOnly && strings.TrimSpace(opts.SuiteSpec) == "" {
		return fmt.Errorf("suite path is empty: pass --suite <file.json|dir>")
	}
	switch {
	case opts.DryRun:
		if opts.Model == "" {
			return fmt.Errorf("model pin is required in dry-run (the scoreboard records it)")
		}
	case opts.ScoreOnly:
		// The stored agent's model is the pin; no --model needed.
		if opts.BaseURL == "" || opts.AdminEmail == "" || opts.AdminPassword == "" {
			return fmt.Errorf("base-url, email and password are required (score-only logs in and reads the fixture, it does not provision)")
		}
	default:
		if opts.Model == "" {
			return fmt.Errorf("model pin is required (the bench protocol pins the model in every scoreboard)")
		}
	}
	return nil
}

// ensureContextWindowPinned aborts the run when a loaded task overrides
// context_window (an agent_config step) while the run carries no explicit
// --context-window pin: the agent PATCH cannot unset context_window, so the
// after-task restore to the baseline would be inexpressible and every later
// task would silently inherit the override. Options.ContextWindow is a
// pointer — nil is "no pin" (the CLI maps the 0 default and an explicit 0
// alike), non-nil is the baseline pin the runner restores to.
func ensureContextWindowPinned(opts Options, suites []suite.TaskSuite) error {
	if opts.ContextWindow != nil {
		return nil
	}
	var overridden []string
	for _, s := range suites {
		for _, task := range s.Tasks {
			for _, step := range task.Setup {
				if step.Type == suite.StepAgentConfig && step.ContextWindow != nil {
					overridden = append(overridden, task.ID)
					break
				}
			}
		}
	}
	if len(overridden) == 0 {
		return nil
	}
	return fmt.Errorf("tasks %s override context_window but the run carries no --context-window pin: the agent PATCH cannot unset context_window, so an override could never be restored and every later task would inherit it — pass --context-window <tokens> (the baseline pin the runner restores to)", strings.Join(overridden, ", "))
}

// provisionScoreOnly is score-only mode's provisioning: login + read the
// workspace and fixture agent + exchange the actors' chat keys — no create,
// no provider bootstrap (the workspace must already exist and hold a working
// provider; the tasks themselves are the only live traffic).
func provisionScoreOnly(ctx context.Context, opts Options) (*driver.Handles, error) {
	client := driver.NewClient(opts.BaseURL, 0)
	token, userID, err := client.Login(ctx, opts.AdminEmail, opts.AdminPassword)
	if err != nil {
		return nil, fmt.Errorf("score-only: admin login: %w", err)
	}
	ws, err := client.GetWorkspace(ctx, token, opts.WorkspaceSlug)
	if err != nil {
		if driver.IsNotFound(err) {
			return nil, fmt.Errorf("score-only: workspace %s does not exist — provision it first with a seeded (non --score-only) run", opts.WorkspaceSlug)
		}
		return nil, fmt.Errorf("score-only: looking up workspace %s: %w", opts.WorkspaceSlug, err)
	}
	agent, err := client.GetAgent(ctx, token, ws.Slug, opts.AgentSlug)
	if err != nil {
		if driver.IsNotFound(err) {
			return nil, fmt.Errorf("score-only: agent %s does not exist in workspace %s — provision it first with a seeded (non --score-only) run", opts.AgentSlug, ws.Slug)
		}
		return nil, fmt.Errorf("score-only: looking up agent %s: %w", opts.AgentSlug, err)
	}

	handles := &driver.Handles{
		AdminToken:  token,
		AdminUserID: userID,
		Actors:      map[string]*driver.Actor{},
		Workspace:   ws,
		Agent:       agent,
		Reused:      true,
		Warnings:    []string{},
	}
	for _, spec := range []struct{ email, password string }{
		{driver.CaseyEmail, driver.CaseyPassword},
		{driver.RileyEmail, driver.RileyPassword},
	} {
		actorToken, _, err := client.Login(ctx, spec.email, spec.password)
		if err != nil {
			return nil, fmt.Errorf("score-only: fixture actor %s login: %w", spec.email, err)
		}
		key, err := client.ExchangeChatKey(ctx, actorToken, ws.Slug)
		if err != nil {
			return nil, fmt.Errorf("score-only: exchanging chat key for %s: %w", spec.email, err)
		}
		handles.Actors[spec.email] = &driver.Actor{Email: spec.email, Token: actorToken, ChatKey: key}
	}
	if c := handles.Actors[driver.CaseyEmail]; c != nil {
		handles.ChatKey = c.ChatKey
	}
	return handles, nil
}

// ---------------------------------------------------------------------------
// Executor
// ---------------------------------------------------------------------------

type executor struct {
	opts    Options
	client  *driver.Client
	handles *driver.Handles
	control *controlServer

	// judge is nil when the judge environment is not configured; judgeErr
	// then carries the actionable message surfaced by judge-check tasks.
	judge    *judgeClient
	judgeErr error

	suiteVersion string
	runTS        string // bare timestamp woven into session ids
	started      time.Time
}

func newExecutor(opts Options, handles *driver.Handles, suiteVersion string) (*executor, error) {
	// The judge client is built up front but its missing-environment error is
	// deferred to the check that needs it: judge-free suites run without any
	// judge environment (design D5).
	judge, judgeErr := newJudgeClient(opts.JudgeModel)

	// The control server serves every run's hook steps ({{control_url}}); it
	// binds an ephemeral loopback port so concurrent bench runs never collide.
	control, err := startControlServer()
	if err != nil {
		return nil, err
	}

	return &executor{
		opts:         opts,
		client:       driver.NewClient(opts.BaseURL, 0),
		handles:      handles,
		control:      control,
		judge:        judge,
		judgeErr:     judgeErr,
		suiteVersion: suiteVersion,
		runTS:        time.Now().UTC().Format("20060102-150405"),
		started:      time.Now().UTC(),
	}, nil
}

func (e *executor) warn(msg string) {
	fmt.Printf("warning: %s\n", msg)
}

func (e *executor) close() {
	if e.control != nil {
		_ = e.control.Close()
	}
}

// sessionID is the measured turns' logical session for one (task, seed,
// attempt): fresh per execution so a retried attempt never stacks on a
// half-executed transcript.
func (e *executor) sessionID(taskID string, seed, attempt int) string {
	id := fmt.Sprintf("bench-%s-%s-s%d", e.runTS, taskID, seed)
	if attempt > 0 {
		id = fmt.Sprintf("%s-r%d", id, attempt)
	}
	return id
}

// setupSessionID scopes a setup chat_turns step's named logical session to
// the same (task, seed, attempt) scope as the measured session.
func (e *executor) setupSessionID(name, taskID string, seed, attempt int) string {
	id := fmt.Sprintf("%s-%s-%s-s%d", name, e.runTS, taskID, seed)
	if attempt > 0 {
		id = fmt.Sprintf("%s-r%d", id, attempt)
	}
	return id
}

// actorChatKey resolves a fixture actor name ("casey"/"riley", case
// insensitive; empty = casey) to that actor's chat key.
func (e *executor) actorChatKey(name string) (string, error) {
	email, err := actorEmail(name)
	if err != nil {
		return "", err
	}
	actor, ok := e.handles.Actors[email]
	if !ok || actor.ChatKey == "" {
		return "", fmt.Errorf("fixture actor %s is not provisioned", email)
	}
	return actor.ChatKey, nil
}

func actorEmail(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "casey":
		return driver.CaseyEmail, nil
	case "riley":
		return driver.RileyEmail, nil
	default:
		return "", fmt.Errorf("unknown fixture actor %q (expected \"casey\" or \"riley\")", name)
	}
}

// progressPath is the per-run result journal: one final TaskResult JSON per
// line, appended as each (task, seed) completes so an aborted run can resume.
func progressPath(out string) string { return out + ".progress.jsonl" }

// resultKey identifies one (task, seed) slot across resumes.
func resultKey(r scoreboard.TaskResult) string {
	return fmt.Sprintf("%s|%d", r.TaskID, r.Seed)
}

// loadProgress reads a prior run's journal, returning the recorded results
// (in recorded order) and the done-slot set. A missing file is an empty
// journal; a malformed line aborts — a corrupt journal must not silently
// shrink the run.
func loadProgress(path string) ([]scoreboard.TaskResult, map[string]bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, map[string]bool{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var results []scoreboard.TaskResult
	done := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r scoreboard.TaskResult
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, nil, fmt.Errorf("progress journal %s: %w", path, err)
		}
		results = append(results, r)
		done[resultKey(r)] = true
	}
	return results, done, nil
}

// appendProgress journals one result line.
func appendProgress(path string, r scoreboard.TaskResult) error {
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// truncateProgress drops the last n journal entries (the dead-streak results
// the breaker discards so outage windows never reach a scoreboard).
func truncateProgress(path string, n int) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if n > len(lines) {
		n = len(lines)
	}
	kept := strings.Join(lines[:len(lines)-n], "\n")
	if kept != "" {
		kept += "\n"
	}
	return os.WriteFile(path, []byte(kept), 0o644)
}

// executeAll drives every task × seed sequentially in suite load order, with
// the per-(task,seed) infra retry budget and bounded exponential backoff. A
// resumed run skips the (task, seed) slots already journaled with a final
// result (pass/fail) and re-measures journaled infra slots; new results
// append as they land. The provider circuit breaker aborts the run once
// providerDeadStreakLimit consecutive tasks exhaust their retry budgets on
// the server-side run-failure signature (the endpoint accepted the turn but
// the agent run died) — a dead or quota-drained endpoint otherwise burns the
// rest of the cycle producing all-fail lanes, which is how two cycles were
// lost to outages before the breaker existed.
func (e *executor) executeAll(ctx context.Context, suites []suite.TaskSuite, opts Options) ([]scoreboard.TaskResult, error) {
	reRunInfra := 0
	prior, done, err := func() ([]scoreboard.TaskResult, map[string]bool, error) {
		path := progressPath(opts.Out)
		if opts.Resume {
			p, _, err := loadProgress(path)
			if err != nil {
				return nil, nil, err
			}
			// Journaled infra results are never final: a slot that exhausted
			// its retry budget inside an outage window re-measures on resume
			// instead of surviving to the scoreboard as an excluded row.
			live := p[:0]
			for _, r := range p {
				if r.Status == scoreboard.StatusInfraError {
					continue
				}
				live = append(live, r)
			}
			reRunInfra = len(p) - len(live)
			done := make(map[string]bool, len(live))
			for _, r := range live {
				done[resultKey(r)] = true
			}
			return live, done, nil
		}
		// Fresh run: the journal from any prior attempt is void.
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, nil, err
		}
		return nil, map[string]bool{}, nil
	}()
	if err != nil {
		return nil, err
	}
	results := prior
	skipped := 0
	total := len(prior)
	for _, s := range suites {
		total += len(s.Tasks) * opts.Seeds
	}
	deadStreak := 0
	for si := range suites {
		s := &suites[si]
		for ti := range s.Tasks {
			task := &s.Tasks[ti]
			for seed := 0; seed < opts.Seeds; seed++ {
				if done[resultKey(scoreboard.TaskResult{TaskID: task.ID, Seed: seed})] {
					skipped++
					continue
				}
				result := e.executeWithRetries(ctx, s, task, seed)
				results = append(results, result)
				if err := appendProgress(progressPath(opts.Out), result); err != nil {
					return results, fmt.Errorf("journal progress: %w", err)
				}
				fmt.Printf("task %-28s seed %d/%d -> %s\n", task.ID, seed+1, opts.Seeds, result.Status)
				if ctx.Err() != nil {
					return results, nil
				}
				if result.Status == scoreboard.StatusInfraError && strings.Contains(result.Error, runStatusFailSignature) {
					deadStreak++
					if deadStreak >= providerDeadStreakLimit {
						// Discard the streak from the journal: those results
						// died to the outage, not the model — re-running them
						// on resume measures the task, not the flap. Without
						// this, every flap poisons the journal with up to
						// providerDeadStreakLimit all-fail lanes (which is how
						// the first supervised baseline recorded 0% lanes
						// across three dead windows).
						if err := truncateProgress(progressPath(opts.Out), deadStreak); err != nil {
							return results, fmt.Errorf("truncate dead-streak journal: %w", err)
						}
						results = results[:len(results)-deadStreak]
						fmt.Printf("\nCIRCUIT BREAKER: %d consecutive tasks exhausted their retry budgets on server-side run failures — the model endpoint looks dead or drained. Streak discarded from the journal; no scoreboard written. Re-run with --resume to continue this run.\n", deadStreak)
						return results, errProviderDead
					}
				} else {
					deadStreak = 0
				}
			}
		}
	}
	if skipped > 0 || reRunInfra > 0 {
		fmt.Printf("resume: %d journaled results loaded, %d tasks skipped, %d journaled infra slots re-measuring\n", len(prior), skipped, reRunInfra)
	}
	return results, nil
}

func (e *executor) executeWithRetries(ctx context.Context, s *suite.TaskSuite, task *suite.Task, seed int) scoreboard.TaskResult {
	for attempt := 0; ; attempt++ {
		result := e.executeOnce(ctx, s, task, seed, attempt)
		if result.Status != scoreboard.StatusInfraError || attempt >= e.opts.Retries {
			return result
		}
		delay := backoffDelay(e.opts.Backoff, attempt)
		fmt.Printf("  infra error on %s seed %d attempt %d, retrying in %s: %s\n", task.ID, seed, attempt+1, delay, result.Error)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return result
		}
	}
}

// backoffDelay returns min(base * 2^attempt, cap) — bounded exponential
// backoff for infra retries and 429s.
func backoffDelay(base time.Duration, attempt int) time.Duration {
	d := base
	for i := 0; i < attempt && d < backoffCap; i++ {
		d *= 2
	}
	if d > backoffCap {
		d = backoffCap
	}
	return d
}

// executeOnce runs one (task, seed, attempt) execution end to end: setup
// steps in order, measured turns, checks, and the pinned-baseline restore.
func (e *executor) executeOnce(ctx context.Context, s *suite.TaskSuite, task *suite.Task, seed, attempt int) scoreboard.TaskResult {
	result := scoreboard.TaskResult{
		TaskID: task.ID,
		Lane:   s.Lane,
		Seed:   seed,
		Checks: []scoreboard.CheckResult{},
	}
	start := time.Now()

	var (
		collected      []driver.Turn
		collectedCalls []driver.ToolCall
		restoreModel   bool
		restoreWindow  bool
		restoreTools   bool
	)
	failWith := func(status string, errMsg string) scoreboard.TaskResult {
		result.Status = status
		result.Error = errMsg
		result.Answer = joinAnswers(collected)
		result.ToolCalls = len(collectedCalls)
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	// Teardowns run best-effort in LIFO order, then the pinned agent baseline
	// is restored — tasks never leak config into each other.
	var cleanups []func()
	defer func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
		e.restoreAgentBaseline(restore{model: restoreModel, window: restoreWindow, tools: restoreTools})
		result.DurationMS = time.Since(start).Milliseconds()
	}()

	// ---- setup steps, in order ----
	for i := range task.Setup {
		step := &task.Setup[i]
		switch step.Type {
		case suite.StepChatTurns:
			key, err := e.actorChatKey(step.Actor)
			if err != nil {
				return failWith(scoreboard.StatusInfraError, err.Error())
			}
			sid := e.setupSessionID(step.Session, task.ID, seed, attempt)
			for _, text := range step.Turns {
				resp, err := e.client.ChatTurn(ctx, key, e.handles.Agent.Slug, text, sid)
				if err != nil {
					return failWith(scoreboard.StatusInfraError, fmt.Sprintf("setup chat turn: %s", err))
				}
				if t := driver.ExtractTurn(resp); t.Status != runStatusCompleted {
					// The setup turn seeds fixture state — a dead run here is
					// infrastructure regardless of which lane it serves.
					status := scoreboard.StatusFail
					if isInfraRunStatus(t.Status) {
						status = scoreboard.StatusInfraError
					}
					return failWith(status, fmt.Sprintf("setup chat turn returned run status %q (want %q)", t.Status, runStatusCompleted))
				}
			}

		case suite.StepAgentConfig:
			err := e.client.PatchAgent(ctx, e.handles.AdminToken, e.handles.Workspace.Slug, e.handles.Agent.ID, step.Model, step.ContextWindow, step.DisabledTools)
			if err != nil {
				return failWith(scoreboard.StatusInfraError, fmt.Sprintf("agent_config setup: %s", err))
			}
			if step.Model != "" {
				restoreModel = true
			}
			if step.ContextWindow != nil {
				restoreWindow = true
			}
			if step.DisabledTools != nil {
				restoreTools = true
			}

		case suite.StepHook:
			hookID, removeFiles, err := e.registerHook(ctx, task, seed, i, step)
			if err != nil {
				return failWith(scoreboard.StatusInfraError, fmt.Sprintf("hook setup: %s", err))
			}
			if removeFiles != nil {
				cleanups = append(cleanups, removeFiles)
			}
			cleanups = append(cleanups, func() {
				cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_ = e.client.DeleteWorkspaceHook(cctx, e.handles.AdminToken, e.handles.Workspace.Slug, hookID)
			})

		case suite.StepConnection:
			connID, err := e.registerConnection(ctx, step)
			if err != nil {
				return failWith(scoreboard.StatusInfraError, fmt.Sprintf("connection setup: %s", err))
			}
			cleanups = append(cleanups, func() {
				cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_ = e.client.DeleteConnection(cctx, e.handles.AdminToken, e.handles.Workspace.Slug, connID)
			})

		case suite.StepWait:
			select {
			case <-time.After(time.Duration(step.Seconds) * time.Second):
			case <-ctx.Done():
				return failWith(scoreboard.StatusInfraError, ctx.Err().Error())
			}

		default:
			return failWith(scoreboard.StatusInfraError, fmt.Sprintf("unknown setup step type %q", step.Type))
		}
	}

	// ---- measured turns ----
	sid := e.sessionID(task.ID, seed, attempt)
	for i, turnSpec := range task.Turns {
		key, err := e.actorChatKey(turnSpec.Actor)
		if err != nil {
			return failWith(scoreboard.StatusInfraError, err.Error())
		}
		resp, err := e.client.ChatTurn(ctx, key, e.handles.Agent.Slug, turnSpec.Text, sid)
		if err != nil {
			return failWith(scoreboard.StatusInfraError, fmt.Sprintf("turn %d: %s", i, err))
		}
		turn := driver.ExtractTurn(resp)
		collected = append(collected, turn)
		collectedCalls = append(collectedCalls, turn.ToolCalls...)
		if turn.Status != runStatusCompleted {
			// failed/cancelled: the run died at the provider — infrastructure,
			// retried within the budget (the v4 baseline lost 9 slots to
			// mid-cycle flaps scored as task fails). prompt_blocked and other
			// terminal statuses are gate outcomes the measure owns: recorded
			// task failures, never retried.
			status := scoreboard.StatusFail
			if isInfraRunStatus(turn.Status) {
				status = scoreboard.StatusInfraError
			}
			return failWith(status, fmt.Sprintf("turn %d returned run status %q (want %q)", i, turn.Status, runStatusCompleted))
		}
	}

	// ---- verification ----
	env := &verifyEnv{
		answer:     joinAnswers(collected),
		toolCalls:  collectedCalls,
		client:     e.client,
		adminToken: e.handles.AdminToken,
		workspace:  e.handles.Workspace.Slug,
		agent:      e.handles.Agent.Slug,
		judge:      e.judge,
		judgeErr:   e.judgeErr,
	}
	checks, err := runChecks(ctx, task.Verify, env)
	if err != nil {
		return failWith(scoreboard.StatusInfraError, err.Error())
	}

	result.Checks = checks
	result.Answer = env.answer
	result.ToolCalls = len(collectedCalls)
	passed := true
	for _, c := range checks {
		if !c.Passed {
			passed = false
			break
		}
	}
	if passed {
		result.Status = scoreboard.StatusPass
	} else {
		result.Status = scoreboard.StatusFail
	}
	result.DurationMS = time.Since(start).Milliseconds()
	return result
}

// registerHook creates the workspace hook from a suite hook step.
//
// HTTP handlers substitute {{control_url}} with the bench control server's
// base URL. Command handlers are materialized locally: the script body (with
// any {{control_file}} placeholder swapped for a freshly created empty
// control file) is written to a per-execution temp script and registered as
// /bin/sh exec-form config — the harness's command hook handler executes the
// program on the server host, which works because the bench and the server
// share the filesystem (the documented local protocol). The returned cleanup
// (command handlers only) removes the materialized script and control file;
// it must run after the hook deletion teardown so the hook never points at a
// missing script.
func (e *executor) registerHook(ctx context.Context, task *suite.Task, seed, stepIdx int, step *suite.SetupStep) (hookID string, removeFiles func(), err error) {
	if step.Handler == nil {
		return "", nil, fmt.Errorf("hook step %q carries no handler", step.Name)
	}
	switch step.Handler.Type {
	case suite.HandlerHTTP:
		url := strings.ReplaceAll(step.Handler.URL, suite.PlaceholderControlURL, e.control.BaseURL())
		config, err := json.Marshal(map[string]string{"url": url})
		if err != nil {
			return "", nil, err
		}
		id, err := e.client.CreateWorkspaceHook(ctx, e.handles.AdminToken, e.handles.Workspace.Slug, driver.HookPayload{
			Name:        step.Name,
			Event:       step.Event,
			Matcher:     step.MatcherTool,
			HandlerType: suite.HandlerHTTP,
			Config:      config,
		})
		return id, nil, err

	case suite.HandlerCommand:
		files, err := e.materializeCommandHook(step.Handler.Command, task.ID, seed, stepIdx)
		if err != nil {
			return "", nil, err
		}
		config, err := json.Marshal(map[string]any{"command": "/bin/sh", "args": []string{files.scriptPath}})
		if err != nil {
			e.removeHookFiles(files)
			return "", nil, err
		}
		id, err := e.client.CreateWorkspaceHook(ctx, e.handles.AdminToken, e.handles.Workspace.Slug, driver.HookPayload{
			Name:        step.Name,
			Event:       step.Event,
			Matcher:     step.MatcherTool,
			HandlerType: suite.HandlerCommand,
			Config:      config,
		})
		if err != nil {
			e.removeHookFiles(files)
			return "", nil, err
		}
		return id, func() { e.removeHookFiles(files) }, nil

	default:
		return "", nil, fmt.Errorf("hook step %q carries unsupported handler type %q", step.Name, step.Handler.Type)
	}
}

// hookFiles are the local artifacts one command-hook step materializes on
// the shared bench/server filesystem.
type hookFiles struct {
	scriptPath  string
	controlPath string // empty when the script body has no {{control_file}}
}

// materializeCommandHook writes the command handler's script body to a
// per-execution temp script (chmod 0755), creating the control file first
// when the body references {{control_file}} and substituting its path in.
// The control file is created empty — the decision logic lives in the script
// body (e.g. exit 2 + stderr to block); the bench only guarantees a stable,
// per-hook path for it.
func (e *executor) materializeCommandHook(body, taskID string, seed, stepIdx int) (hookFiles, error) {
	files := hookFiles{}
	script := body
	if strings.Contains(script, suite.PlaceholderControlFile) {
		f, err := os.CreateTemp("", controlFilePattern)
		if err != nil {
			return files, fmt.Errorf("creating hook control file: %w", err)
		}
		files.controlPath = f.Name()
		_ = f.Close() // the harness's command handler only needs the path to exist
		script = strings.ReplaceAll(script, suite.PlaceholderControlFile, files.controlPath)
	}
	files.scriptPath = filepath.Join(os.TempDir(), fmt.Sprintf("%s%s-%s-s%d-%d.sh", hookScriptPrefix, e.runTS, taskID, seed, stepIdx))
	if err := os.WriteFile(files.scriptPath, []byte(script), hookScriptPerm); err != nil {
		e.removeHookFiles(files)
		return files, fmt.Errorf("writing hook script %s: %w", files.scriptPath, err)
	}
	if err := os.Chmod(files.scriptPath, hookScriptPerm); err != nil {
		e.removeHookFiles(files)
		return files, fmt.Errorf("chmod hook script %s: %w", files.scriptPath, err)
	}
	return files, nil
}

// removeHookFiles deletes a command hook's materialized artifacts. Best
// effort: teardown must never fail a task over a temp file.
func (e *executor) removeHookFiles(f hookFiles) {
	if f.scriptPath != "" {
		_ = os.Remove(f.scriptPath)
	}
	if f.controlPath != "" {
		_ = os.Remove(f.controlPath)
	}
}

// registerConnection creates the workspace connection from the step's recipe
// + config map (token / access_level / origin keys; absent keys keep the
// server's defaults).
func (e *executor) registerConnection(ctx context.Context, step *suite.SetupStep) (string, error) {
	conn, err := e.client.CreateConnection(ctx, e.handles.AdminToken, e.handles.Workspace.Slug, driver.ConnectionPayload{
		RecipeID:    step.Recipe,
		AccessLevel: step.Config["access_level"],
		Token:       step.Config["token"],
		Origin:      step.Config["origin"],
	})
	if err != nil {
		return "", err
	}
	return conn.ID, nil
}

// restore carries which agent config dimensions a task's agent_config steps
// touched and therefore need restoring to the pinned baseline.
type restore struct {
	model, window, tools bool
}

// restoreAgentBaseline patches the fixture agent back to the pinned baseline
// after a task that applied agent_config steps. Best-effort with a fresh
// timeout: a restore failure is a warning, not a task failure (the next
// Provision repairs divergence anyway).
func (e *executor) restoreAgentBaseline(r restore) {
	if !r.model && !r.window && !r.tools {
		return
	}
	agent := e.handles.Agent
	model := ""
	if r.model {
		model = agent.Model
	}
	var window *int
	if r.window {
		window = agent.ContextWindow
		if window == nil {
			fmt.Println("warning: agent baseline context window is unset; the public API cannot clear a task's context_window override — pin --context-window on the run for full task isolation")
		}
	}
	var tools []string
	if r.tools {
		tools = agent.DisabledTools
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := e.client.PatchAgent(ctx, e.handles.AdminToken, e.handles.Workspace.Slug, agent.ID, model, window, tools); err != nil {
		fmt.Printf("warning: restoring agent baseline after task: %s\n", err)
	}
}

func joinAnswers(turns []driver.Turn) string {
	parts := make([]string, 0, len(turns))
	for _, t := range turns {
		if strings.TrimSpace(t.AnswerText) != "" {
			parts = append(parts, t.AnswerText)
		}
	}
	return strings.Join(parts, "\n")
}

// ---------------------------------------------------------------------------
// Dry run
// ---------------------------------------------------------------------------

// dryRun loads + lints + plans and emits a structurally valid scoreboard
// where every row is a recorded fail ("dry-run (no model call)") with empty
// checks — a structural artifact, not a scored run. Suite version, git sha,
// and the model pin are still recorded.
func dryRun(opts Options, suites []suite.TaskSuite) (*scoreboard.Scoreboard, error) {
	fmt.Println("=== DRY RUN — structural artifact, not a scored run ===")

	board := &scoreboard.Scoreboard{
		SchemaVersion: scoreboard.SchemaVersion,
		RunID:         newRunID(),
		SuiteVersion:  suites[0].SuiteVersion,
		OnclawGitSHA:  gitSHA(),
		ModelPin:      opts.Model,
		BaseURL:       opts.BaseURL,
		StartedAt:     time.Now().UTC(),
		Seeds:         opts.Seeds,
		Tasks:         []scoreboard.TaskResult{},
	}
	for si := range suites {
		s := &suites[si]
		for ti := range s.Tasks {
			for seed := 0; seed < opts.Seeds; seed++ {
				board.Tasks = append(board.Tasks, scoreboard.TaskResult{
					TaskID: s.Tasks[ti].ID,
					Lane:   s.Lane,
					Seed:   seed,
					Status: scoreboard.StatusFail,
					Checks: []scoreboard.CheckResult{},
					Error:  "dry-run (no model call)",
				})
			}
		}
	}
	board.Lanes, board.Overall = scoreboard.BuildScoreboard(board.Tasks)
	board.FinishedAt = time.Now().UTC()
	printSummary(board)
	if err := writeScoreboard(opts.Out, board); err != nil {
		return nil, err
	}
	fmt.Printf("Wrote %s (run %s, dry-run)\n", opts.Out, board.RunID)
	return board, nil
}

// ---------------------------------------------------------------------------
// Assembly + output
// ---------------------------------------------------------------------------

func (e *executor) assemble(opts Options, results []scoreboard.TaskResult) (*scoreboard.Scoreboard, error) {
	board := &scoreboard.Scoreboard{
		SchemaVersion: scoreboard.SchemaVersion,
		RunID:         newRunID(),
		SuiteVersion:  e.suiteVersion,
		OnclawGitSHA:  gitSHA(),
		ModelPin:      e.handles.Agent.Model,
		BaseURL:       opts.BaseURL,
		StartedAt:     e.started,
		FinishedAt:    time.Now().UTC(),
		Seeds:         opts.Seeds,
		Tasks:         results,
	}
	board.Lanes, board.Overall = scoreboard.BuildScoreboard(board.Tasks)
	for _, r := range results {
		if r.Status == scoreboard.StatusInfraError {
			board.InfraErrors++
		}
	}
	printSummary(board)
	if err := writeScoreboard(opts.Out, board); err != nil {
		return nil, err
	}
	fmt.Printf("Wrote %s (run %s, model %s)\n", opts.Out, board.RunID, board.ModelPin)
	return board, nil
}

func newRunID() string {
	return "bench-" + time.Now().UTC().Format("20060102-150405")
}

// gitSHA shells out to `git rev-parse --short HEAD` in the process working
// directory (the bench lives inside the onclaw repo). Any failure degrades to
// "unknown" — a missing sha is recorded, never a run failure.
func gitSHA() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return "unknown"
	}
	return sha
}

// writeScoreboard writes the JSON artifact: indented encoding with a
// trailing newline, overwriting any previous file.
func writeScoreboard(path string, board *scoreboard.Scoreboard) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(board); err != nil {
		return fmt.Errorf("encoding scoreboard: %w", err)
	}
	buf.WriteString("\n")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing scoreboard %s: %w", path, err)
	}
	return nil
}

// printSummary prints the per-lane summary table (lane, pass/fail/infra,
// pass_rate), overall last — the eval-memory CLI's convention.
func printSummary(board *scoreboard.Scoreboard) {
	pass := map[string]int{}
	fail := map[string]int{}
	infra := map[string]int{}
	for _, t := range board.Tasks {
		switch t.Status {
		case scoreboard.StatusPass:
			pass[t.Lane]++
		case scoreboard.StatusFail:
			fail[t.Lane]++
		case scoreboard.StatusInfraError:
			infra[t.Lane]++
		}
	}
	fmt.Printf("%-24s %6s %6s %6s %10s\n", "lane", "pass", "fail", "infra", "pass_rate")
	for _, lane := range board.Lanes {
		fmt.Printf("%-24s %6d %6d %6d %9.1f%%\n", lane.Lane, pass[lane.Lane], fail[lane.Lane], infra[lane.Lane], lane.PassRate*100)
	}
	fmt.Printf("%-24s %6d %6d %6d %9.1f%%\n", "overall", board.Overall.Passed, board.Overall.TasksRun-board.Overall.Passed, board.InfraErrors, board.Overall.PassRate*100)
}
