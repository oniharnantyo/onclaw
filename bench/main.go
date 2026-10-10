// Command onclaw-bench is the bench CLI: it runs the benchmark task suites
// against a running OnClaw server (run) and compares a run's scoreboard
// against a committed baseline with pre-registered gates (compare).
//
// The bench is a nested, decoupled Go module (add-harness-bench design D1):
// stdlib only, no onclaw imports, driving the server exclusively through its
// public HTTP API.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw-bench/runner"
	"github.com/oniharnantyo/onclaw-bench/scoreboard"
)

const usageText = `onclaw-bench — the OnClaw harness benchmark (nested decoupled module)

Usage:

  go run . run <flags>       run suites against a live server, write a scoreboard
  go run . compare <flags>   compare a scoreboard against a baseline's pre-registered gates

Run flags:

  --base-url URL        base URL of the running onclaw server (default http://localhost:8080)
  --email EMAIL         admin identity login (env: ONCLAW_BENCH_ADMIN_EMAIL)
  --password PASS       admin identity password (env: ONCLAW_BENCH_ADMIN_PASSWORD)
  --workspace SLUG      fixture workspace slug (default bench-eval; created when absent, reused when present)
  --agent SLUG          fixture agent slug (default bench-agent; created when absent, reused when present)
  --model MODEL         pinned agent-under-test model (default glm-5.3-flash via Z.AI)
  --context-window N    pin the agent's context window for the run (0 = leave the server default)
  --suite PATH          suite file or directory of suite files (default bench/suites;
                        directory loads skip underscore-prefixed helper files)
  --out PATH            scoreboard JSON output path (default scoreboard.json)
  --seeds N             sequential seeds per task (default 3)
  --retries N           per-(task,seed) infra-error retry budget (default 2)
  --backoff D           exponential backoff base, capped at 60s (default 5s)
  --judge-model MODEL   judge check model, pinned separately (default glm-5.3-flash)
  --dry-run             load + lint + plan only: emit a structural scoreboard, no server contact
  --seed-only           provision the fixture workspace/agent and exit (no tasks)
  --score-only          skip provisioning (workspace must already exist) and run the scored tasks
  --resume              continue an aborted run: skip (task,seed) slots already in <out>.progress.jsonl (pass/fail only — journaled infra slots re-measure)

Provider bootstrap (only used when the fixture workspace has no enabled provider):
  ONCLAW_BENCH_PROVIDER_KEY   API key for the pinned provider (GLM-5.3-flash via Z.AI)

Judge checks (only suites with judge_allowed: true):
  ONCLAW_BENCH_JUDGE_KEY      judge API key (missing key = those tasks record infra-error)
  ONCLAW_BENCH_JUDGE_BASE_URL OpenAI-compatible base URL (default https://api.z.ai/api/paas/v4)

Compare flags:

  --run PATH        the run's scoreboard JSON
  --baseline PATH   the committed baseline JSON (scoreboard + pre-registered gates)

Exit codes: run exits 0 when the scoreboard was written (task failures are
data; the gate decides) and 1 on operational failure (unreachable server,
lint issues, suite version mismatch). compare exits 0 when every gate passes
and 1 otherwise.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		os.Exit(cmdRun(os.Args[2:], os.Stdout, os.Stderr))
	case "compare":
		os.Exit(cmdCompare(os.Args[2:], os.Stdout, os.Stderr))
	case "-h", "--help", "help":
		fmt.Print(usageText)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usageText)
		os.Exit(2)
	}
}

// cmdRun executes the run subcommand. Returns the process exit code: 0 when
// the scoreboard was written (task failures are data), 1 on operational
// failure, 2 on flag misuse.
func cmdRun(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	baseURL := fs.String("base-url", "http://localhost:8080", "base URL of the running onclaw server")
	email := fs.String("email", os.Getenv("ONCLAW_BENCH_ADMIN_EMAIL"), "admin identity login (env ONCLAW_BENCH_ADMIN_EMAIL)")
	password := fs.String("password", os.Getenv("ONCLAW_BENCH_ADMIN_PASSWORD"), "admin identity password (env ONCLAW_BENCH_ADMIN_PASSWORD)")
	workspace := fs.String("workspace", "bench-eval", "fixture workspace slug")
	agent := fs.String("agent", "bench-agent", "fixture agent slug")
	model := fs.String("model", "glm-5.3-flash", "pinned agent-under-test model")
	contextWindow := fs.Int("context-window", 0, "pin the agent's context window for the run (0 = server default)")
	suiteSpec := fs.String("suite", "bench/suites", "suite file or directory of suite files")
	out := fs.String("out", "scoreboard.json", "scoreboard JSON output path")
	seeds := fs.Int("seeds", 3, "sequential seeds per task")
	retries := fs.Int("retries", 2, "per-(task,seed) infra-error retry budget")
	backoff := fs.Duration("backoff", 5*time.Second, "exponential backoff base (cap 60s)")
	judgeModel := fs.String("judge-model", "glm-5.3-flash", "judge check model (pinned separately from the agent)")
	dryRun := fs.Bool("dry-run", false, "load + lint + plan only, emit a structural scoreboard")
	seedOnly := fs.Bool("seed-only", false, "provision the fixture and exit (no tasks)")
	scoreOnly := fs.Bool("score-only", false, "skip provisioning; the workspace must already exist")
	resume := fs.Bool("resume", false, "continue an aborted run from <out>.progress.jsonl (skip completed task/seed slots)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, "run:", err)
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "run: unexpected positional arguments: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}

	// Admin credentials backfill from the environment via the flag defaults
	// above; after parsing, an empty value means neither source had it.
	// Dry-run never contacts the server, so it needs no credentials.
	if (*email == "" || *password == "") && !*dryRun {
		fmt.Fprintln(stderr, "run: --email/--password are required (or set ONCLAW_BENCH_ADMIN_EMAIL and ONCLAW_BENCH_ADMIN_PASSWORD)")
		return 1
	}

	var window *int
	if *contextWindow > 0 {
		w := *contextWindow
		window = &w
	}

	board, err := runner.Run(context.Background(), runner.Options{
		BaseURL:       *baseURL,
		AdminEmail:    *email,
		AdminPassword: *password,
		WorkspaceSlug: *workspace,
		AgentSlug:     *agent,
		Model:         *model,
		ContextWindow: window,
		SuiteSpec:     *suiteSpec,
		Out:           *out,
		Seeds:         *seeds,
		Retries:       *retries,
		Backoff:       *backoff,
		JudgeModel:    *judgeModel,
		DryRun:        *dryRun,
		SeedOnly:      *seedOnly,
		ScoreOnly:     *scoreOnly,
		Resume:        *resume,
	})
	if err != nil {
		fmt.Fprintln(stderr, "run failed:", err)
		return 1
	}
	if board != nil {
		fmt.Fprintf(stdout, "Run %s: suite %s, model %s, seeds %d, infra-errors %d\n",
			board.RunID, board.SuiteVersion, board.ModelPin, board.Seeds, board.InfraErrors)
	}
	return 0
}

// cmdCompare executes the compare subcommand: load the run scoreboard and the
// committed baseline, evaluate the pre-registered gates, print per-lane rows
// plus the overall row. Exit 0 iff every gate passes.
func cmdCompare(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	runPath := fs.String("run", "", "the run's scoreboard JSON")
	baselinePath := fs.String("baseline", "", "the committed baseline JSON")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, "compare:", err)
		return 2
	}
	if *runPath == "" || *baselinePath == "" {
		fmt.Fprintln(stderr, "compare: both --run and --baseline are required")
		return 2
	}

	code, err := runCompare(*runPath, *baselinePath, stdout)
	if err != nil {
		fmt.Fprintln(stderr, "compare:", err)
		return 1
	}
	return code
}

// runCompare is cmdCompare's testable core: it loads both artifacts, runs
// scoreboard.Compare, prints the report, and returns the exit code
// (0 = gates pass, 1 = any gate fails).
func runCompare(runPath, baselinePath string, stdout io.Writer) (int, error) {
	run, err := loadScoreboard(runPath)
	if err != nil {
		return 1, err
	}
	baseline, err := loadBaseline(baselinePath)
	if err != nil {
		return 1, err
	}

	report := scoreboard.Compare(*run, *baseline)

	fmt.Fprintf(stdout, "compare: run %s (suite %s, model %s, sha %s)\n",
		run.RunID, run.SuiteVersion, run.ModelPin, run.OnclawGitSHA)
	fmt.Fprintf(stdout, "         baseline %s (suite %s, model %s, sha %s)\n",
		baselinePath, baseline.SuiteVersion, baseline.ModelPin, baseline.OnclawGitSHA)
	fmt.Fprintf(stdout, "%-24s %12s %12s %10s   %s\n", "lane", "baseline", "run", "delta", "verdict")
	for _, row := range report.Rows {
		fmt.Fprintf(stdout, "%-24s %12.4f %12.4f %9.4f%%   %s\n",
			row.Lane, row.BaselineRate, row.RunRate, row.Delta*100, strings.ToUpper(row.Verdict))
		if row.Verdict == scoreboard.VerdictFail && row.Reason != "" {
			fmt.Fprintf(stdout, "%-24s %s\n", "", row.Reason)
		}
	}
	if !report.Pass {
		fmt.Fprintln(stdout, "GATE: FAIL")
		return 1, nil
	}
	fmt.Fprintln(stdout, "GATE: PASS")
	return 0, nil
}

// loadScoreboard reads a run artifact. The schema version is checked so a
// stale file fails loudly instead of comparing nonsense.
func loadScoreboard(path string) (*scoreboard.Scoreboard, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading run scoreboard: %w", err)
	}
	var board scoreboard.Scoreboard
	if err := json.Unmarshal(raw, &board); err != nil {
		return nil, fmt.Errorf("parsing run scoreboard %s: %w", path, err)
	}
	if board.SchemaVersion != scoreboard.SchemaVersion {
		return nil, fmt.Errorf("run scoreboard %s has schema_version %d, want %d — regenerate the artifact with this bench", path, board.SchemaVersion, scoreboard.SchemaVersion)
	}
	if len(board.Tasks) == 0 {
		return nil, fmt.Errorf("run scoreboard %s holds no task rows", path)
	}
	return &board, nil
}

// loadBaseline reads a committed baseline artifact (gates + previous
// scoreboard).
func loadBaseline(path string) (*scoreboard.Baseline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading baseline: %w", err)
	}
	var baseline scoreboard.Baseline
	if err := json.Unmarshal(raw, &baseline); err != nil {
		return nil, fmt.Errorf("parsing baseline %s: %w", path, err)
	}
	if baseline.SchemaVersion != scoreboard.SchemaVersion {
		return nil, fmt.Errorf("baseline %s has schema_version %d, want %d — re-record the baseline with this bench", path, baseline.SchemaVersion, scoreboard.SchemaVersion)
	}
	return &baseline, nil
}
