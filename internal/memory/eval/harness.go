package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Options configures one harness invocation (mirrors the eval-memory CLI
// flags one-to-one).
type Options struct {
	BaseURL       string // running onclaw server
	Email         string // admin identity: workspace create-or-reuse + member provisioning
	Password      string
	WorkspaceSlug string
	AgentSlug     string
	Model         string // optional model override for the fixture agent
	Out           string // scoreboard JSON path
	SeedOnly      bool   // seed the corpus and exit (no scored turns)
	ScoreOnly     bool   // skip seeding (workspace must already hold the corpus)
	IngestWait    time.Duration
	RunID         string        // defaults to a timestamp
	Timeout       time.Duration // whole-command budget; 0 = no wrapper deadline
	PollInterval  time.Duration // notes-poll cadence for the ingestion wait
}

// Run executes the harness end to end: seed (unless ScoreOnly), drive the
// questions, score, and write the scoreboard JSON to Options.Out.
func Run(ctx context.Context, opts Options) (*Scoreboard, error) {
	if opts.BaseURL == "" {
		return nil, fmt.Errorf("eval-memory: --base-url is required")
	}
	if opts.Email == "" || opts.Password == "" {
		return nil, fmt.Errorf("eval-memory: --email and --password are required")
	}
	if opts.WorkspaceSlug == "" {
		opts.WorkspaceSlug = "memory-eval"
	}
	if opts.AgentSlug == "" {
		opts.AgentSlug = "memory-eval-agent"
	}
	if opts.Out == "" {
		opts.Out = "./eval-scoreboard.json"
	}
	if err := Fixture.ValidateFixture(); err != nil {
		return nil, err
	}

	runID := opts.RunID
	if runID == "" {
		runID = "eval-" + time.Now().Format("20060102-150405")
	}

	client := NewAPIClient(opts.BaseURL, opts.Timeout)
	board := &Scoreboard{
		RunID:     runID,
		StartedAt: time.Now().UTC(),
		Workspace: opts.WorkspaceSlug,
	}

	seed := &Seeder{client: client, AdminEmail: opts.Email, AdminPassword: opts.Password, PollInterval: opts.PollInterval}
	var seedInfo *SeedInfo
	if !opts.ScoreOnly {
		result, err := seed.Seed(ctx, SeedOptions{
			WorkspaceSlug: opts.WorkspaceSlug,
			AgentSlug:     opts.AgentSlug,
			Model:         opts.Model,
			IngestWait:    opts.IngestWait,
			RunID:         runID,
		})
		if err != nil {
			return nil, fmt.Errorf("seeding: %w", err)
		}
		board.Model = agentModelLabel(result.Agent)
		board.ScopeAudit = &result.ScopeAudit
		board.Seed = &SeedInfo{
			WorkspaceSlug: result.Workspace.Slug,
			AgentSlug:     result.Agent.Slug,
			Sessions:      result.Sessions,
			Turns:         result.Turns,
			NotesAPILive:  result.NotesAPILive,
			Reused:        result.Reused,
		}
		board.PerQuestion = append(board.PerQuestion, seedNotesToNotes(result)...)
		seedInfo = board.Seed
		if opts.SeedOnly {
			board.Totals, board.Summary = BuildScoreboard(nil)
			if err := writeScoreboard(opts.Out, board); err != nil {
				return nil, err
			}
			return board, nil
		}
	}

	runner := &Runner{client: client}
	results, err := runner.Run(ctx, RunOptions{
		WorkspaceSlug: opts.WorkspaceSlug,
		AgentSlug:     opts.AgentSlug,
		RunID:         runID,
	})
	if err != nil {
		return nil, fmt.Errorf("running questions: %w", err)
	}

	scores := make([]QuestionScore, 0, len(results))
	for _, tr := range results {
		if tr.Err != nil {
			scores = append(scores, erroredScore(tr))
			continue
		}
		scores = append(scores, ScoreQuestion(tr.Question, tr.Answer, tr.Evidence))
	}

	board.PerQuestion = append(scores, board.PerQuestion...)
	board.Totals, board.Summary = BuildScoreboard(scores)
	if board.Seed == nil {
		board.Seed = seedInfo
	}

	if err := writeScoreboard(opts.Out, board); err != nil {
		return nil, err
	}
	return board, nil
}

// erroredScore records a question whose turn failed outright: every arm fails
// so the scoreboard never silently hides a broken run.
func erroredScore(tr TurnResult) QuestionScore {
	f := false
	return QuestionScore{
		ID:               tr.Question.ID,
		Type:             tr.Question.Type,
		Recall:           &f,
		CitationValid:    &f,
		ScopeSafe:        &f,
		AbstainedCorrect: &f,
		Status:           "error",
		Notes:            []string{tr.Err.Error()},
	}
}

// seedNotesToNotes carries the seeder's operational warnings into the
// scoreboard as pseudo-rows on a seed-only run (visible without a decode).
func seedNotesToNotes(result *SeedResult) []QuestionScore {
	if len(result.Notes) == 0 {
		return nil
	}
	notes := make([]string, 0, len(result.Notes))
	notes = append(notes, result.Notes...)
	return []QuestionScore{{
		ID:    "_seed",
		Type:  "meta",
		Notes: notes,
	}}
}

func agentModelLabel(a *Agent) string {
	if a == nil {
		return ""
	}
	return a.Slug + "@" + a.Model
}

func writeScoreboard(path string, board *Scoreboard) error {
	buf, err := json.MarshalIndent(board, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return fmt.Errorf("writing scoreboard %s: %w", path, err)
	}
	return nil
}
