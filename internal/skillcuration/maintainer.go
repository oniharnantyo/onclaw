package skillcuration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// The wiki maintainer (add-skill-curation-from-traces 4.2, design D3/D7):
// one bounded side-call per qualified cluster per cycle. It samples
// stratified failing/passing windows from the cluster's membership, asks the
// resolved curation model for page operations against the current wiki, and
// parses the output STRICTLY into ops the Go wiki layer executes — models
// draft text, Go code materializes everything (design Goals). Every pattern
// page cites the evidence runs it was derived from, and ops citing unknown
// runs are rejected before anything is written.
//
// Fail-soft: every failure returns as an error the cycle logs and defers to
// the next tick (D7) — the ingest path, the raw session events, and an
// already-consistent wiki are never touched by a failed stage.

// Sampling and prompt budgets. The curation config carries no per-call
// prompt knob (2.2's contract), so the stratification caps ARE the budget:
// at most maxQualifyingWindows gate-passing windows and maxContrastWindows
// failing windows, each rendered within windowRenderBudget characters.
const (
	maxQualifyingWindows = 3
	maxContrastWindows   = 2
	windowRenderBudget   = 4000
)

// Maintainer runs the wiki-maintenance side-call for qualified clusters.
type Maintainer struct {
	sessions store.SessionEventStore
	clusters store.SkillCandidateStore
	resolver ModelResolver
	wiki     *Wiki
	log      *slog.Logger
	budget   time.Duration
}

// MaintainerOption configures the maintainer; every knob has a safe default.
type MaintainerOption func(*Maintainer)

// WithSideCallBudget overrides the side-call deadline (DefaultSideCallBudget
// by default; the parent context's earlier deadline always wins).
func WithSideCallBudget(d time.Duration) MaintainerOption {
	return func(m *Maintainer) {
		if d > 0 {
			m.budget = d
		}
	}
}

// NewMaintainer constructs the maintainer from its granular dependencies:
// the session-event store the sampled windows load from (the qualifier's
// window seam), the candidate store the cluster memberships read from, the
// model resolver (CurationModelResolver in production; the test seam in
// unit tests), the workspace's wiki, and the logger.
func NewMaintainer(sessions store.SessionEventStore, clusters store.SkillCandidateStore, resolver ModelResolver, wiki *Wiki, log *slog.Logger, opts ...MaintainerOption) *Maintainer {
	m := &Maintainer{
		sessions: sessions,
		clusters: clusters,
		resolver: resolver,
		wiki:     wiki,
		log:      log,
		budget:   DefaultSideCallBudget,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// MaintainRequest scopes one cluster's wiki-maintenance side-call.
type MaintainRequest struct {
	WorkspaceID string
	AgentID     string
	ClusterID   string
}

// MaintainCluster runs one maintenance side-call: sample → prompt → parse →
// apply. A cluster with no membership rows (nothing indexed yet) is a quiet
// no-op; a cluster whose windows are all unread (deleted sessions) is
// quietly skipped — no material, no model call (the memory gate's
// empty-material precedent).
func (m *Maintainer) MaintainCluster(ctx context.Context, req MaintainRequest) error {
	ctx, cancel := context.WithTimeout(ctx, m.budget)
	defer cancel()

	membership, err := m.clusters.ListClusterRunsByCluster(ctx, req.WorkspaceID, req.ClusterID)
	if err != nil {
		return fmt.Errorf("skillcuration: list cluster runs: %w", err)
	}
	if len(membership) == 0 {
		return nil
	}

	start := time.Now()
	model, err := m.resolver(ctx, req.WorkspaceID, req.AgentID)
	if err != nil {
		return fmt.Errorf("skillcuration: resolve maintainer model: %w", err)
	}

	sampled := sampleWindows(membership)
	windows, knownRuns := m.loadWindows(ctx, req.WorkspaceID, sampled)
	if len(windows) == 0 {
		m.log.DebugContext(ctx, "skillcuration: wiki maintenance skipped; no readable windows",
			"workspace_id", req.WorkspaceID, "agent_id", req.AgentID, "cluster_id", req.ClusterID)
		return nil
	}

	pages, err := m.wiki.List()
	if err != nil {
		return fmt.Errorf("skillcuration: list wiki pages: %w", err)
	}
	// The model may legitimately re-cite evidence already on the pages it
	// was shown; anything else it names must come from the sampled windows.
	for _, page := range pages {
		for _, run := range page.EvidenceRuns {
			knownRuns[run] = true
		}
	}

	raw, err := generateSideCallText(ctx, model, maintainerSystemPrompt, maintainerUserPrompt(pages, windows))
	if err != nil {
		return fmt.Errorf("skillcuration: maintainer model call: %w", err)
	}
	ops, err := parseWikiOps(raw)
	if err != nil {
		return fmt.Errorf("skillcuration: parse maintainer ops: %w", err)
	}

	applied, err := m.applyOps(ctx, req, ops, knownRuns)
	elapsed := time.Since(start)
	if err != nil {
		return err
	}
	if len(ops) > 0 {
		m.log.InfoContext(ctx, "skillcuration: wiki maintenance applied",
			"workspace_id", req.WorkspaceID, "agent_id", req.AgentID,
			"cluster_id", req.ClusterID, "ops_proposed", len(ops),
			"ops_applied", applied, "elapsed_ms", elapsed.Milliseconds())
	}
	return nil
}

// sampledRun is one membership row picked for the prompt, tagged with its
// stratum.
type sampledRun struct {
	run       domain.SkillClusterRun
	stratum   string
	citations string
}

// sampleWindows picks the prompt's windows, deterministically: the
// top-scoring qualifying runs (the soft score over the stored tally) and the
// error-heaviest contrast runs (the failing evidence a pattern's failure
// modes come from). Oldest-first, then id, breaks score ties so equal-input
// clusters sample identically every cycle.
func sampleWindows(membership []domain.SkillClusterRun) []sampledRun {
	var qualifying, contrast []domain.SkillClusterRun
	for _, run := range membership {
		if run.Qualifying {
			qualifying = append(qualifying, run)
		} else {
			contrast = append(contrast, run)
		}
	}
	sort.SliceStable(qualifying, func(i, j int) bool {
		si, sj := ScoreRun(qualifying[i], 0), ScoreRun(qualifying[j], 0)
		if si != sj {
			return si > sj
		}
		if !qualifying[i].IndexedAt.Equal(qualifying[j].IndexedAt) {
			return qualifying[i].IndexedAt.Before(qualifying[j].IndexedAt)
		}
		return qualifying[i].ID < qualifying[j].ID
	})
	sort.SliceStable(contrast, func(i, j int) bool {
		if contrast[i].ErrorResults != contrast[j].ErrorResults {
			return contrast[i].ErrorResults > contrast[j].ErrorResults
		}
		if !contrast[i].IndexedAt.Equal(contrast[j].IndexedAt) {
			return contrast[i].IndexedAt.Before(contrast[j].IndexedAt)
		}
		return contrast[i].ID < contrast[j].ID
	})

	sampled := make([]sampledRun, 0, maxQualifyingWindows+maxContrastWindows)
	for i, run := range qualifying {
		if i >= maxQualifyingWindows {
			break
		}
		sampled = append(sampled, sampledRun{
			run:     run,
			stratum: "qualifying",
			citations: fmt.Sprintf("recoveries=%d errors=%d tool_calls=%d distinct_tools=%d",
				run.Recoveries, run.ErrorResults, run.ToolCalls, run.DistinctTools),
		})
	}
	for i, run := range contrast {
		if i >= maxContrastWindows {
			break
		}
		sampled = append(sampled, sampledRun{
			run:     run,
			stratum: "failing",
			citations: fmt.Sprintf("recoveries=%d errors=%d tool_calls=%d distinct_tools=%d status=%s",
				run.Recoveries, run.ErrorResults, run.ToolCalls, run.DistinctTools, run.RunStatus),
		})
	}
	return sampled
}

// loadWindows loads and renders the sampled runs' transcript windows (the
// qualifier's window seam: LoadEvents narrowed to the run's turn). Deleted
// or empty sessions contribute nothing and their ids stay unknown — the
// model can only cite windows it was shown. knownRuns is never nil.
func (m *Maintainer) loadWindows(ctx context.Context, workspaceID string, sampled []sampledRun) ([]renderedWindow, map[string]bool) {
	knownRuns := make(map[string]bool, len(sampled))
	windows := make([]renderedWindow, 0, len(sampled))
	for _, s := range sampled {
		events, err := m.sessions.LoadEvents(ctx, store.LoadSessionEventsParams{
			WorkspaceID: workspaceID,
			SessionID:   s.run.SessionID,
		})
		if err != nil {
			// One unreadable window never sinks the stage (fail-soft); the
			// remaining windows still carry the call.
			m.log.DebugContext(ctx, "skillcuration: window load failed; skipping",
				"session_id", s.run.SessionID, "turn_id", s.run.TurnID, "error", err)
			continue
		}
		window := renderRunWindow(turnEvents(events, s.run.TurnID))
		if window == "" {
			continue
		}
		knownRuns[s.run.SessionID] = true
		windows = append(windows, renderedWindow{
			SessionID: s.run.SessionID,
			Stratum:   s.stratum,
			Tally:     s.citations,
			Body:      window,
		})
	}
	return windows, knownRuns
}

// renderedWindow is one sampled run window as the prompt shows it.
type renderedWindow struct {
	SessionID string
	Stratum   string
	Tally     string
	Body      string
}

// renderRunWindow renders one run's transcript window to the compact
// line-per-event form the maintainer prompt shows: user and assistant text,
// tool calls with outcome and latency. The raw log is read-only evidence —
// unparseable rows contribute nothing (the qualifier's tallyRun posture).
func renderRunWindow(events []domain.SessionEvent) string {
	var sb strings.Builder
	for _, row := range events {
		var se adk.SessionEvent[*schema.AgenticMessage]
		if err := runSerializer.Unmarshal(row.Payload, &se); err != nil {
			continue
		}
		_ = adk.NormalizeSessionEventKind(&se)
		switch se.Kind {
		case adk.SessionEventMessage:
			if se.Message == nil {
				continue
			}
			if text := agenticRenderableText(se.Message); text != "" {
				fmt.Fprintf(&sb, "%s: %s\n", strings.ToLower(strings.TrimSpace(string(se.Message.Role))), truncateRunes(text, 800))
			}
		case adk.SessionEventSpanToolCallStart:
			if se.Span != nil && se.Span.Tool != nil {
				fmt.Fprintf(&sb, "-> tool %s started\n", se.Span.Tool.Name)
			}
		case adk.SessionEventSpanToolCallEnd:
			if se.Span == nil || se.Span.Tool == nil {
				continue
			}
			if se.Span.Status == "error" || se.Span.Err != "" {
				fmt.Fprintf(&sb, "<- tool %s ERROR: %s\n", se.Span.Tool.Name, truncateRunes(se.Span.Err, 300))
				continue
			}
			fmt.Fprintf(&sb, "<- tool %s ok\n", se.Span.Tool.Name)
		}
	}
	return truncateRunes(strings.TrimRight(sb.String(), "\n"), windowRenderBudget)
}

// truncateRunes cuts s to at most max runes, appending an elision marker
// when truncated (the memory truncateRunes precedent).
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + " …[truncated]"
}

// wikiOp is one page operation — the strict JSON the maintainer side-call
// emits. All fields are required except successor_slug (supersede) and
// slugs (merge), per the op kind.
type wikiOp struct {
	Op            string   `json:"op"`
	Slug          string   `json:"slug"`
	SuccessorSlug string   `json:"successor_slug"`
	Slugs         []string `json:"slugs"`
	Title         string   `json:"title"`
	CitedRuns     []string `json:"cited_runs"`
	Body          string   `json:"body"`
}

// parseWikiOps extracts the ops array from the model response: code fences
// and surrounding prose are tolerated, anything unparseable is an error the
// stage fails soft on (the memory parseGateOps precedent).
func parseWikiOps(raw string) ([]wikiOp, error) {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		return nil, errors.New("no ops array in response")
	}
	var ops []wikiOp
	if err := json.Unmarshal([]byte(raw[start:end+1]), &ops); err != nil {
		return nil, fmt.Errorf("decode ops: %w", err)
	}
	return ops, nil
}

// applyOps validates EVERY op first, then applies them in order — one
// malformed op fails the whole stage with the wiki untouched (strict
// parsing, 4.1). Validation checks the op's shape and that every cited run
// is known evidence (a sampled window this call, or a citation already on a
// page the model was shown); ops citing unknown runs are rejected here,
// before anything is written (spec: pages always cite their evidence).
func (m *Maintainer) applyOps(ctx context.Context, req MaintainRequest, ops []wikiOp, knownRuns map[string]bool) (int, error) {
	pages := make([]Page, 0, len(ops))
	for i, op := range ops {
		page, err := validateWikiOp(op, knownRuns, m.wiki)
		if err != nil {
			return 0, fmt.Errorf("skillcuration: maintainer op %d (%s) rejected: %w", i, strings.TrimSpace(op.Op), err)
		}
		pages = append(pages, page)
	}

	applied := 0
	for i, op := range ops {
		var err error
		switch strings.ToLower(strings.TrimSpace(op.Op)) {
		case "create":
			err = m.wiki.Create(pages[i])
		case "update":
			err = m.wiki.Update(pages[i])
		case "supersede":
			err = m.wiki.Supersede(strings.TrimSpace(op.Slug), pages[i])
		case "merge":
			err = m.wiki.Merge(op.Slugs, pages[i])
		default:
			err = fmt.Errorf("unknown op %q", op.Op)
		}
		if err != nil {
			// Validated ops only fail here on real I/O errors — fail the
			// stage (fail-soft at the caller) rather than half-apply the
			// batch silently.
			return applied, fmt.Errorf("skillcuration: apply maintainer op %d (%s): %w", i, op.Op, err)
		}
		applied++
	}
	return applied, nil
}

// validateWikiOp checks one op's shape and targets against the wiki,
// returning the page the op writes. Unknown op kinds, empty titles/bodies,
// invalid slugs, empty or unknown evidence citations, and dangling targets
// all reject the op.
func validateWikiOp(op wikiOp, knownRuns map[string]bool, w *Wiki) (Page, error) {
	kind := strings.ToLower(strings.TrimSpace(op.Op))
	switch kind {
	case "create", "update", "supersede", "merge":
	default:
		return Page{}, fmt.Errorf("unknown op kind %q", op.Op)
	}

	page := Page{
		Title:        strings.TrimSpace(op.Title),
		EvidenceRuns: op.CitedRuns,
		Body:         strings.TrimSpace(op.Body),
		Status:       PageActive,
	}
	if page.Title == "" {
		return Page{}, errors.New("title is empty")
	}
	if page.Body == "" {
		return Page{}, errors.New("body is empty")
	}
	if len(page.EvidenceRuns) == 0 {
		return Page{}, errors.New("cites no evidence runs")
	}
	for _, run := range page.EvidenceRuns {
		if !knownRuns[strings.TrimSpace(run)] {
			return Page{}, fmt.Errorf("cites unknown evidence run %q", run)
		}
	}

	switch kind {
	case "create", "update":
		page.Slug = strings.TrimSpace(op.Slug)
		if err := ValidatePageSlug(page.Slug); err != nil {
			return Page{}, fmt.Errorf("slug: %w", err)
		}
	case "supersede":
		page.Slug = strings.TrimSpace(op.SuccessorSlug)
		if err := ValidatePageSlug(page.Slug); err != nil {
			return Page{}, fmt.Errorf("successor slug: %w", err)
		}
		oldSlug := strings.TrimSpace(op.Slug)
		if err := ValidatePageSlug(oldSlug); err != nil {
			return Page{}, fmt.Errorf("superseded slug: %w", err)
		}
		if oldSlug == page.Slug {
			return Page{}, errors.New("successor slug equals the superseded slug")
		}
		current, err := w.Page(oldSlug)
		if err != nil {
			return Page{}, fmt.Errorf("superseded page: %w", err)
		}
		if current.Status == PageSuperseded {
			return Page{}, fmt.Errorf("page %s is already superseded", oldSlug)
		}
		if _, err := w.Page(page.Slug); err == nil {
			return Page{}, fmt.Errorf("successor page %s already exists", page.Slug)
		} else if !errors.Is(err, ErrPageNotFound) {
			return Page{}, err
		}
	case "merge":
		if len(op.Slugs) < 2 {
			return Page{}, errors.New("merge needs at least two source pages")
		}
		for _, src := range op.Slugs {
			if err := ValidatePageSlug(strings.TrimSpace(src)); err != nil {
				return Page{}, fmt.Errorf("merge source slug %q: %w", src, err)
			}
			current, err := w.Page(strings.TrimSpace(src))
			if err != nil {
				return Page{}, fmt.Errorf("merge source %s: %w", src, err)
			}
			if current.Status == PageSuperseded {
				return Page{}, fmt.Errorf("merge source %s is already superseded", src)
			}
		}
		page.Slug = strings.TrimSpace(op.Slug)
		if err := ValidatePageSlug(page.Slug); err != nil {
			return Page{}, fmt.Errorf("merge target slug: %w", err)
		}
	}
	return page, nil
}

// maintainerSystemPrompt is the strict JSON contract for the maintenance
// side-call.
const maintainerSystemPrompt = `You are the skill-wiki maintainer for an AI agent workspace. You are shown the workspace's pattern wiki and sampled transcript windows from one procedure family (a task the agent repeatedly performs). Decide how the pattern wiki should change so it captures the procedure's failure modes and the strategies that actually work.

Emit ONLY a JSON array — no prose — with one object per page operation:
[{"op":"create","slug":"kebab-case-name","title":"Page title","cited_runs":["session-id"],"body":"markdown body"}]

Rules:
- op is one of: create (a new pattern page; slug must not exist), update (rewrite an existing active page), supersede (retire an outdated page: slug is the old page, successor_slug is a NEW page that replaces it), merge (collapse duplicate pages: slugs lists the sources, slug is the canonical page — either one of the sources or a new name).
- cited_runs lists the evidence run session IDs the page is derived from. Every page cites at least one run, and you may only cite session IDs shown in the sampled windows or already printed on an existing page.
- body is concise markdown: the failure modes observed, the working strategy, and any workaround. Keep pages small and specific.
- Emit [] when the wiki already covers the evidence and nothing should change.`

// maintainerUserPrompt renders the maintenance call's user turn: the wiki
// index (active pages with full content, superseded pages as pointers), then
// the sampled windows with their run ids and tallies.
func maintainerUserPrompt(pages []Page, windows []renderedWindow) string {
	var sb strings.Builder
	sb.WriteString("## Pattern wiki\n")
	if len(pages) == 0 {
		sb.WriteString("(empty — no pattern pages yet)\n")
	}
	for _, page := range pages {
		fmt.Fprintf(&sb, "\n### %s — %s [%s]\n", page.Slug, page.Title, page.Status)
		if page.Status == PageSuperseded {
			fmt.Fprintf(&sb, "Superseded by %s; do not update, supersede only if again outdated.\n", page.SupersededBy)
			continue
		}
		fmt.Fprintf(&sb, "Evidence: %s\n\n%s\n", strings.Join(page.EvidenceRuns, ", "), truncateRunes(page.Body, windowRenderBudget))
	}

	sb.WriteString("\n## Sampled run windows\n")
	sb.WriteString("Each window is one run of the same procedure family; cite runs by session id.\n")
	for _, w := range windows {
		fmt.Fprintf(&sb, "\n### Run %s (%s; %s)\n\n%s\n", w.SessionID, w.Stratum, w.Tally, w.Body)
	}
	return sb.String()
}
