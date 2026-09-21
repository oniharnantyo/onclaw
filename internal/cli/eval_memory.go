package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/oniharnantyo/onclaw/internal/memory/eval"
	"github.com/urfave/cli/v3"
)

// evalMemoryCmd runs the wave-0 memory evaluation harness
// (integrate-agent-zero-memory, design D15 / tasks 1.1–1.2).
type evalMemoryCmd struct{}

// NewEvalMemoryCmd creates a new evalMemoryCmd instance.
func NewEvalMemoryCmd() *evalMemoryCmd {
	return &evalMemoryCmd{}
}

// Command returns the *cli.Command definition for "eval-memory".
func (e *evalMemoryCmd) Command() *cli.Command {
	return &cli.Command{
		Name:        "eval-memory",
		Usage:       "Run the wave-0 LongMemEval-protocol memory scoreboard (live server + live model keys required)",
		Description: evalMemoryHelp,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "base-url",
				Usage: "Base URL of the running onclaw server to evaluate",
				Value: "http://localhost:8080",
			},
			&cli.StringFlag{
				Name:     "email",
				Usage:    "Login email of the admin identity (superadmin, or the workspace owner when reusing a workspace)",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "password",
				Usage:    "Login password of the admin identity",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "workspace",
				Usage: "Fixture workspace slug (created when absent, reused when present)",
				Value: "memory-eval",
			},
			&cli.StringFlag{
				Name:  "agent",
				Usage: "Fixture agent slug to drive (created when absent, reused when present)",
				Value: "memory-eval-agent",
			},
			&cli.StringFlag{
				Name:  "model",
				Usage: "Provider model to configure the fixture agent with (e.g. gpt-4o-mini); on reuse the agent is repointed at this model",
			},
			&cli.StringFlag{
				Name:  "out",
				Usage: "Path for the scoreboard JSON report",
				Value: "./eval-scoreboard.json",
			},
			&cli.BoolFlag{
				Name:  "seed-only",
				Usage: "Provision the fixture workspace and sessions, then exit without running scored questions",
			},
			&cli.BoolFlag{
				Name:  "score-only",
				Usage: "Skip seeding; the fixture workspace must already hold the scripted corpus",
			},
			&cli.DurationFlag{
				Name:  "ingest-wait",
				Usage: "Bounded wait for background memory ingestion after seeding (the pipeline is async)",
				Value: 180 * time.Second,
			},
		},
		Action: e.Run,
	}
}

// Run executes the harness: seed (unless --score-only), drive the fixture
// questions through the real /v1 chat path, score, and write the scoreboard.
func (e *evalMemoryCmd) Run(ctx context.Context, cmd *cli.Command) error {
	board, err := eval.Run(ctx, eval.Options{
		BaseURL:       cmd.String("base-url"),
		Email:         cmd.String("email"),
		Password:      cmd.String("password"),
		WorkspaceSlug: cmd.String("workspace"),
		AgentSlug:     cmd.String("agent"),
		Model:         cmd.String("model"),
		Out:           cmd.String("out"),
		SeedOnly:      cmd.Bool("seed-only"),
		ScoreOnly:     cmd.Bool("score-only"),
		IngestWait:    cmd.Duration("ingest-wait"),
	})
	if err != nil {
		return err
	}

	if board.Seed != nil {
		fmt.Printf("Seed: workspace=%s agent=%s sessions=%d turns=%d reused=%v notes_api=%v\n",
			board.Seed.WorkspaceSlug, board.Seed.AgentSlug, board.Seed.Sessions, board.Seed.Turns, board.Seed.Reused, board.Seed.NotesAPILive)
	}
	if board.ScopeAudit != nil {
		fmt.Printf("Scope audit: passed=%v (third-party sees %d private rows, owner sees %d)\n",
			board.ScopeAudit.Passed, board.ScopeAudit.BudiSees, board.ScopeAudit.SariSees)
	}
	fmt.Printf("Scoreboard: recall=%.1f%% citation_valid=%.1f%% scope_safe=%.1f%% abstention=%.1f%% overall=%.1f%%\n",
		board.Summary.Recall, board.Summary.CitationValid, board.Summary.ScopeSafe, board.Summary.Abstention, board.Summary.Overall)
	fmt.Printf("Wrote %s (run %s, model %s)\n", cmd.String("out"), board.RunID, board.Model)
	return nil
}

// evalMemoryHelp is the harness README, surfaced through --help: what the
// harness measures, its live-run requirements, and how to record the
// pre-change baseline (tasks 1.1–1.3).
const evalMemoryHelp = `Wave-0 memory evaluation harness (integrate-agent-zero-memory, design D15).

WHAT IT MEASURES
  Seeds a fixture workspace (members Budi and Sari, one fixture agent) with
  scripted multi-session chat history, then drives the LongMemEval-protocol
  questions through the REAL chat path (POST /v1/responses against a live
  model) and scores each answer. The hardened fixture carries 30 sessions /
  75 turns and 28 questions (10 multihop, 12 recall, 1 update, 2 temporal,
  2 abstention, 1 scope):

    recall       known facts surface again (single-session)
    update       superseded facts answer with the NEW fact, not the stale one
    temporal     dates recorded in conversation are recalled
    multihop     two facts chain across sessions (A -> B)
    abstention   unrecorded topics are met with "nothing recorded", no
                 fabricated specifics
    scope        the OTHER member's private facts never leak (and the notes
                 API is probed store-level from both identities when present)

  Every answer is additionally checked for citation validity: a memory-backed
  claim must trace to evidence the run actually opened (memory.search tool
  cards in the /v1 transcript, or the notes REST API as a fallback). The
  scoreboard is written as JSON to --out.

RETRIEVAL BEHAVIOR (post-wave-3)
  - With an embedding model configured on the fixture workspace's memory
    settings (the post-wave-3 configuration), the vector channel and the
    raw-evidence index are live: notes, gists, and raw turns embed at
    ingestion (the raw turn BEFORE extraction), and free-text retrieval
    fuses the lexical and vector channels with Reciprocal Rank Fusion
    (k=60).
  - Fusion is an enhancement, never a dependency: with no embedding model
    configured the vector channel is absent and lexical fallback is the
    whole read path.
  - Dimension-exclusion semantics: only rows embedded at the CURRENT
    configured dimension participate in the vector channel. Configure the
    model BEFORE seeding — rows written at an older dimension stay
    lexical-retrievable but are never vector-returned and are never
    re-embedded.
  - Entity/associative questions route through depth-1 graph traversal
    (exact normalized label first, then prefix/trigram seeds, one hop to
    the linked rows) — zero model calls on the read path.
  - Raw turns surface as cited raw-evidence hits only when they lead the
    vector ranking.

LIVE-RUN REQUIREMENTS
  - A running onclaw server (--base-url) with its PostgreSQL database
    migrated (the pgvector extension is part of the migration chain).
  - LIVE MODEL KEYS: the workspace must have an enabled provider whose keys
    actually work. Agent creation runs real prompt generation, and every
    fixture turn and question is a real model run — nothing is mocked.
  - --email/--password must be superadmin credentials (to provision the
    fixture users Budi and Sari) or the owner credentials of an existing
    fixture workspace. Fixture users log in with the harness's fixed
    passwords, so re-runs against the same workspace are deterministic.
  - For a post-wave-3 scoreboard, pin the embedding model on the fixture
    workspace (Settings -> Memory -> Configuration) before seeding so the
    whole corpus embeds at one dimension.
  - The run takes minutes: seeding posts 30 sessions / 75 turns plus 28
    questions, each a live model call, followed by a bounded wait for the
    async ingestion pipeline (--ingest-wait).

RECORDING THE BASELINE (task 1.3)
  1. Build the harness from HEAD: go build -o /tmp/onclaw-eval .
  2. Start the PRE-CHANGE server (kept docs only — before the memory pipeline
     lands) with live model keys, e.g. from the change's base revision.
  3. Run: onclaw eval-memory --email ... --password ... \
         --out eval-scoreboard.baseline.json
     Expect recall/multihop/update numbers near zero on a pre-change server:
     the scripted facts exist only in session history, which the kept docs
     never see. Abstention and scope should already hold (there is nothing to
     leak).
  4. Apply the change, restart the server, and re-run with
     --out eval-scoreboard.post.json (use a FRESH --workspace slug so the
     corpus re-seeds cleanly).
  5. Record both scoreboards in the change folder; the hardened fixture is
     the standing gate — its pre-registered decision rule and the recorded
     runs (the wave-3 D11 override and the pre-wave-3 baseline) live as
     hardened-run.md in the change folders.

MODES
  default          seed (create-or-reuse workspace/agent, post all scripted
                   sessions, wait for ingestion) + run + score
  --seed-only      provision the corpus and exit
  --score-only     skip seeding; drive and score the questions only`
