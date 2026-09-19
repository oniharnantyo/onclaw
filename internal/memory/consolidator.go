package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/cloudwego/eino/callbacks"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Consolidator tunables, defaulted conservatively (design parameter pins:
// consolidation cadence nightly 02:00; dedupe similarity tuning is a wave-0
// adjustable).
const (
	// defaultConsolidationInterval is the nightly loop's wake cadence. The
	// fire time itself is computed per workspace — the next ~02:00 in the
	// workspace's local timezone — so the interval only bounds how often the
	// loop re-aligns (a workspace created or re-timezoned mid-window is
	// picked up on the next wake).
	defaultConsolidationInterval = 24 * time.Hour
	// consolidationHourLocal is the local hour the nightly pass aligns to.
	consolidationHourLocal = 2
	// consolidationSimilarity is the near-duplicate cluster threshold,
	// mirroring the gate's dedupeSimilarity — the gate refuses ADDs above
	// this similarity, so anything left behind accumulates here.
	consolidationSimilarity = 0.8
	// maxConsolidationNotes bounds one pass's candidate listing (a
	// workspace-sized batch, not a page — the pass clusters in memory).
	maxConsolidationNotes = 2000
	// maxMergeCluster caps one merge decision's prompt; a larger group is
	// split across consecutive nights rather than fed as one giant call.
	maxMergeCluster = 8
	// topicBatchSize bounds one topic-folding side-call.
	topicBatchSize = 50
	// maxTopicRunes bounds a folded topic label.
	maxTopicRunes = 64
	// maxConflictExcerpt bounds a report's note excerpt.
	maxConflictExcerpt = 200
)

// StatsFunc snapshots the ingestion worker's monotonic counters; the
// morning report's extraction-failure count is the delta between the
// pass's snapshot and the workspace's last-seen one. The consolidator takes
// the function rather than the *Worker so the two services stay decoupled —
// the composition root passes memoryWorker.Stats.
type StatsFunc func() IngestStats

// Consolidator is the nightly per-workspace consolidation pass (tasks
// 6.1–6.3, D12): merging near-duplicate notes into canonical ones with
// multi-evidence links, folding notes into browsable topics, and producing
// the morning report — surfaced in the Memory pane and persisted as the
// workspace's last report.
//
// Copy-out only (D6): merges supersede — history stays answerable and raw
// evidence is never touched. Doc-over-notes precedence (D7): the
// consolidator NEVER writes USER.md/WORKSPACE.md — it holds no document
// store at all (a structural guarantee: there is no doc-write dependency
// anywhere on the type), and a fact contradicting a document is only
// surfaced as a report flag. Fail-soft (D10): side-calls ride the shared
// cheap-model seam; any stage failure — model unresolvable, unparseable
// response, store rejection — logs, skips the affected note or cluster, and
// the report is still produced.
//
// The pass reads the workspace-shared note set (the pipeline's
// infrastructure view). Visibility never widens in the pipeline (D4), so
// clusters never cross tiers or owners: per-owner (user/agent) notes keep
// their birth scope and are not folded — collapsing them into another
// owner's view would be the one widening the design forbids.
type Consolidator struct {
	notes      store.MemoryNoteStore
	reports    store.MemoryReportStore
	workspaces store.WorkspaceStore
	stats      StatsFunc
	log        *slog.Logger
	resolver   ModelResolver
	trace      callbacks.Handler

	interval time.Duration
	enabled  bool
	sideCall sideCallConfig

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	// runMu serializes passes: the nightly sweep and the manual
	// consolidate-now share one code path, and Stop waits for the holder.
	runMu sync.Mutex

	marksMu   sync.Mutex
	marks     map[string]time.Time   // workspaceID -> last pass (attempted)
	lastStats map[string]IngestStats // workspaceID -> last-seen counters
}

// ConsolidatorOption configures the consolidator beyond its granular
// dependencies.
type ConsolidatorOption func(*Consolidator)

// WithInterval sets the nightly loop's wake cadence (default 24h). The fire
// time is per-workspace — the next ~02:00 local — and is recomputed on
// every wake.
func WithInterval(d time.Duration) ConsolidatorOption {
	return func(c *Consolidator) {
		if d > 0 {
			c.interval = d
		}
	}
}

// WithEnabled gates the nightly loop (the test seam); RunNow — the manual
// button's code path — always works.
func WithEnabled(enabled bool) ConsolidatorOption {
	return func(c *Consolidator) {
		c.enabled = enabled
	}
}

// WithSideCall applies the shared cheap-model seam options — the gate's
// WithSideCallModel, WithModelResolver, and WithTraceCallback — to the
// consolidator's side-calls. Unset, provider-backed resolution is used and
// an unresolvable workspace falls back to the deterministic heuristic.
func WithSideCall(opts ...SideCallOption) ConsolidatorOption {
	return func(c *Consolidator) {
		for _, opt := range opts {
			opt(&c.sideCall)
		}
	}
}

// NewConsolidator constructs the consolidator from its granular
// dependencies: the curated notes store, the morning-report store, the
// workspace catalog (the nightly sweep's tenant enumeration and the
// per-workspace timezone), the workspace provider catalog, the instance
// encryption key, and the shared agentic model factory for the cheap-model
// side-calls — plus the ingestion worker's Stats snapshotter.
func NewConsolidator(
	notes store.MemoryNoteStore,
	reports store.MemoryReportStore,
	workspaces store.WorkspaceStore,
	providerStore store.ProviderStore,
	encryptionKey []byte,
	factory ModelFactory,
	stats StatsFunc,
	log *slog.Logger,
	opts ...ConsolidatorOption,
) *Consolidator {
	c := &Consolidator{
		notes:      notes,
		reports:    reports,
		workspaces: workspaces,
		stats:      stats,
		log:        log,
		interval:   defaultConsolidationInterval,
		enabled:    true,
		stopCh:     make(chan struct{}),
		marks:      make(map[string]time.Time),
		lastStats:  make(map[string]IngestStats),
	}
	for _, opt := range opts {
		opt(c)
	}
	c.resolver = newSideCallResolver(providerStore, encryptionKey, factory, c.sideCall)
	return c
}

// Start launches the nightly loop under the process-lifetime context: the
// loop exits when ctx is cancelled or Stop is called, and in-flight passes
// are not interrupted — Stop waits for them (the scheduler/heartbeat
// lifecycle). A disabled consolidator starts nothing; RunNow still works.
func (c *Consolidator) Start(ctx context.Context) {
	if !c.enabled {
		return
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.loop(ctx)
	}()
}

// Stop halts the nightly loop and waits — bounded by the stop grace — for
// any in-flight pass (nightly or manual) to finish. Idempotent.
func (c *Consolidator) Stop() {
	c.stopOnce.Do(func() { close(c.stopCh) })
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		// Wait out a manual RunNow holding the pass lock too.
		c.runMu.Lock()
		c.runMu.Unlock()
		close(done)
	}()
	timer := time.NewTimer(stopGrace)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		c.log.Warn("memory consolidator: stop deadline exceeded with a pass still in flight")
	}
}

// loop wakes on the interval cadence and fires the per-workspace passes whose
// local consolidation slot (the most recent ~02:00) has arrived since the
// workspace's last attempt.
func (c *Consolidator) loop(ctx context.Context) {
	for {
		timer := time.NewTimer(c.nextDelay(time.Now()))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-c.stopCh:
			timer.Stop()
			return
		case <-timer.C:
		}
		c.sweep(ctx)
	}
}

// nextDelay computes how long the loop should sleep: until the earliest
// workspace's next local consolidation slot, bounded by the interval (the
// test seam wakes frequently; production re-aligns daily).
func (c *Consolidator) nextDelay(now time.Time) time.Duration {
	delay := c.interval
	workspaces, err := c.workspaces.ListAll(context.Background())
	if err != nil {
		c.log.Warn("memory consolidator: list workspaces for alignment", "error", err)
		return delay
	}
	for _, ws := range workspaces {
		next := nextConsolidationSlot(workspaceLoc(ws.Timezone), now)
		if d := next.Sub(now); d < delay {
			delay = d
		}
	}
	if delay < 0 {
		return 0
	}
	return delay
}

// sweep fires one pass per due workspace. A workspace is due when its most
// recent local consolidation slot is newer than its last attempt — a pass
// is never replayed inside the same slot, and a workspace first observed
// mid-day waits for its next night.
func (c *Consolidator) sweep(ctx context.Context) {
	workspaces, err := c.workspaces.ListAll(ctx)
	if err != nil {
		c.log.Warn("memory consolidator: nightly sweep skipped", "error", err)
		return
	}
	now := time.Now()
	for i := range workspaces {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
		}
		if !c.due(&workspaces[i], now) {
			continue
		}
		if _, err := c.RunNow(ctx, workspaces[i].ID); err != nil {
			// One workspace's failure never blocks the others' mornings.
			c.log.Warn("memory consolidator: nightly pass failed",
				"workspace_id", workspaces[i].ID, "error", err)
		}
	}
}

// due reports whether the workspace's local consolidation slot has crossed
// since its last attempt, recording the first-seen mark for workspaces the
// loop has not observed yet.
func (c *Consolidator) due(ws *domain.Workspace, now time.Time) bool {
	c.marksMu.Lock()
	defer c.marksMu.Unlock()
	mark, seen := c.marks[ws.ID]
	if !seen {
		c.marks[ws.ID] = now
		return false
	}
	return lastConsolidationSlot(workspaceLoc(ws.Timezone), now).After(mark)
}

// RunNow runs one consolidation pass for the workspace and returns its
// morning report. It backs both the nightly ticker and the Memory pane's
// consolidate-now button — one code path (task 6.2).
func (c *Consolidator) RunNow(ctx context.Context, workspaceID string) (MorningReport, error) {
	c.runMu.Lock()
	defer c.runMu.Unlock()

	// The pass runs only for a real workspace row — ByID is the existence
	// check behind the tenant-scope invariant (no pass without a workspace).
	if _, err := c.workspaces.ByID(ctx, workspaceID); err != nil {
		return MorningReport{}, fmt.Errorf("memory consolidator: load workspace: %w", err)
	}

	now := time.Now().UTC()
	report := MorningReport{
		GeneratedAt: now,
		Conflicts:   []ConflictFlag{},
		Merges:      []MergeRecord{},
	}

	// The candidate set is the workspace-shared current state; clustering,
	// the conflict section, and the topic fold all read from this one
	// listing so a pass is a consistent snapshot of the workspace.
	notes, err := c.notes.ListNotesForUI(ctx, workspaceID, "", "", store.MemoryNoteFilters{Limit: maxConsolidationNotes})
	if err != nil {
		return MorningReport{}, fmt.Errorf("memory consolidator: list notes: %w", err)
	}

	model := c.resolveModel(ctx, workspaceID)
	for _, cluster := range clusterNotes(notes, consolidationSimilarity) {
		merge, ok := c.mergeCluster(ctx, workspaceID, cluster, model)
		if ok {
			report.Merges = append(report.Merges, merge)
		}
	}
	if model != nil {
		c.foldTopics(ctx, workspaceID, notes, model)
	}
	report.Conflicts = conflictFlags(notes)
	report.ExtractionFailures = c.failureDelta(workspaceID)

	if raw, err := json.Marshal(report); err != nil {
		c.log.Warn("memory consolidator: marshal report", "workspace_id", workspaceID, "error", err)
	} else if err := c.reports.Save(ctx, workspaceID, raw, now); err != nil {
		// The pass itself succeeded — fail soft: the caller still gets the
		// report, the next pass rewrites the persisted one (D10).
		c.log.Warn("memory consolidator: save report", "workspace_id", workspaceID, "error", err)
	}

	c.mark(workspaceID, now)
	return report, nil
}

// resolveModel resolves the workspace's cheap-model side-call, degrading to
// the deterministic heuristic (nil model) when unresolvable — an
// unconfigured provider is a normal state for consolidation, never a
// failure of the morning.
func (c *Consolidator) resolveModel(ctx context.Context, workspaceID string) Model {
	m, err := c.resolver(ctx, workspaceID, "")
	if err != nil {
		c.log.Warn("memory consolidator: model unresolvable; using the deterministic fallback",
			"workspace_id", workspaceID, "error", err)
		return nil
	}
	return m
}

// mergeCluster folds one near-duplicate cluster into its canonical note:
// the model picks the survivor (heuristic fallback), every folded duplicate
// is superseded INTO it (D6 — pointers, never deletes), and all members'
// source events land as the canonical's multi-evidence links (D12). A
// rejected duplicate or evidence link logs and skips; the merge record
// carries only what actually folded.
func (c *Consolidator) mergeCluster(ctx context.Context, workspaceID string, cluster []domain.MemoryNote, m Model) (MergeRecord, bool) {
	canonical := c.pickCanonical(ctx, cluster, m)
	merge := MergeRecord{CanonicalID: canonical.ID, MergedIDs: []string{}}

	var sourceEvents []string
	for _, note := range cluster {
		sourceEvents = append(sourceEvents, note.SourceEventID)
		if note.ID == canonical.ID {
			continue
		}
		if err := c.notes.SupersedeInto(ctx, workspaceID, note.ID, canonical.ID); err != nil {
			c.log.Warn("memory consolidator: fold rejected",
				"workspace_id", workspaceID, "note_id", note.ID, "canonical_id", canonical.ID, "error", err)
			continue
		}
		merge.MergedIDs = append(merge.MergedIDs, note.ID)
	}
	if len(merge.MergedIDs) == 0 {
		return MergeRecord{}, false
	}

	// Multi-evidence on the survivor: every member's birth evidence,
	// idempotent per (note, source event) — re-runs never duplicate links.
	if err := c.notes.AddNoteEvidence(ctx, workspaceID, canonical.ID, sourceEvents); err != nil {
		c.log.Warn("memory consolidator: evidence links failed",
			"workspace_id", workspaceID, "note_id", canonical.ID, "error", err)
	}
	return merge, true
}

// pickCanonical chooses the cluster's survivor: the model's choice — one
// cheap side-call per cluster — when it names a listed note, else the
// deterministic heuristic (also the path for a failed or unparseable call):
// pinned first, then most important, then the oldest observation, then the
// id (stable across runs and backends).
func (c *Consolidator) pickCanonical(ctx context.Context, cluster []domain.MemoryNote, m Model) domain.MemoryNote {
	if m != nil {
		raw, err := generateText(sideCallContext(ctx, c.trace, "memory.consolidator_merge"), m, mergeSystemPrompt, mergeUserPrompt(cluster))
		if err != nil {
			c.log.Warn("memory consolidator: merge decision skipped", "error", err)
		} else if id := parseCanonicalID(raw, cluster); id != "" {
			for _, note := range cluster {
				if note.ID == id {
					return note
				}
			}
		}
	}
	best := cluster[0]
	for _, note := range cluster[1:] {
		switch {
		case note.Pinned != best.Pinned:
			if note.Pinned {
				best = note
			}
		case note.Importance != best.Importance:
			if note.Importance > best.Importance {
				best = note
			}
		case note.LearnedAt.Before(best.LearnedAt):
			best = note
		case note.ID < best.ID:
			best = note
		}
	}
	return best
}

// foldTopics labels the untopic'd notes in batches (D12's topic fold),
// failing soft per batch — a bad batch leaves its notes for the next night.
func (c *Consolidator) foldTopics(ctx context.Context, workspaceID string, notes []domain.MemoryNote, m Model) {
	var batch []domain.MemoryNote
	flush := func() {
		if len(batch) == 0 {
			return
		}
		c.applyTopicBatch(ctx, workspaceID, batch, m)
		batch = batch[:0]
	}
	for _, note := range notes {
		if note.Topic != nil {
			continue
		}
		batch = append(batch, note)
		if len(batch) >= topicBatchSize {
			flush()
		}
	}
	flush()
}

func (c *Consolidator) applyTopicBatch(ctx context.Context, workspaceID string, batch []domain.MemoryNote, m Model) {
	raw, err := generateText(sideCallContext(ctx, c.trace, "memory.consolidator_topics"), m, topicSystemPrompt, topicUserPrompt(batch))
	if err != nil {
		c.log.Warn("memory consolidator: topic batch skipped", "workspace_id", workspaceID, "error", err)
		return
	}
	byID := make(map[string]struct{}, len(batch))
	for _, note := range batch {
		byID[note.ID] = struct{}{}
	}
	for _, assignment := range parseTopicAssignments(raw) {
		topic := strings.TrimSpace(assignment.Topic)
		if assignment.ID == "" || topic == "" {
			continue
		}
		if _, ok := byID[assignment.ID]; !ok {
			continue // never label a note the batch did not contain
		}
		if err := c.notes.SetNoteTopic(ctx, workspaceID, assignment.ID, truncateRunes(topic, maxTopicRunes)); err != nil {
			c.log.Warn("memory consolidator: topic rejected",
				"workspace_id", workspaceID, "note_id", assignment.ID, "error", err)
		}
	}
}

// conflictFlags extracts the doc-over-notes review section (D7): every
// note the gate flagged against the kept documents, with its excerpt. The
// documents themselves are never touched — the flag is the review surface.
func conflictFlags(notes []domain.MemoryNote) []ConflictFlag {
	flags := []ConflictFlag{}
	for _, note := range notes {
		if note.ConflictFlag == nil {
			continue
		}
		flags = append(flags, ConflictFlag{
			NoteID:    note.ID,
			Document:  *note.ConflictFlag,
			Excerpt:   truncateRunes(note.Content, maxConflictExcerpt),
			FlaggedAt: note.LearnedAt,
		})
	}
	return flags
}

// failureDelta reports this period's extraction failures: the delta between
// the pass's global counter snapshot and the workspace's last-seen one
// (counters are process-global; per-workspace snapshots attribute the delta
// to the pass that observes it). A counter reset is clamped to zero.
func (c *Consolidator) failureDelta(workspaceID string) int {
	// stats is an optional capability (nil = no worker wired — the fallback
	// assembly); an unwired counter reads as all-zero, never a panic.
	if c.stats == nil {
		return 0
	}
	current := c.stats()
	c.marksMu.Lock()
	defer c.marksMu.Unlock()
	previous := c.lastStats[workspaceID]
	c.lastStats[workspaceID] = current
	if current.Failed < previous.Failed {
		return 0
	}
	return int(current.Failed - previous.Failed)
}

// mark records the pass's attempt time so the nightly loop never re-fires
// the same slot.
func (c *Consolidator) mark(workspaceID string, at time.Time) {
	c.marksMu.Lock()
	defer c.marksMu.Unlock()
	c.marks[workspaceID] = at
}

// workspaceLoc resolves the workspace timezone, falling back to UTC when
// unset or unknown (a bad tz string must never stall the morning).
func workspaceLoc(tz string) *time.Location {
	if strings.TrimSpace(tz) == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	return time.UTC
}

// lastConsolidationSlot returns the most recent 02:00 local at or before
// now — the slot a pass is answering.
func lastConsolidationSlot(loc *time.Location, now time.Time) time.Time {
	local := now.In(loc)
	t := time.Date(local.Year(), local.Month(), local.Day(), consolidationHourLocal, 0, 0, 0, loc)
	if t.After(now) {
		t = t.AddDate(0, 0, -1)
	}
	return t
}

// nextConsolidationSlot returns the next strictly-future 02:00 local — the
// loop's alignment target.
func nextConsolidationSlot(loc *time.Location, now time.Time) time.Time {
	local := now.In(loc)
	t := time.Date(local.Year(), local.Month(), local.Day(), consolidationHourLocal, 0, 0, 0, loc)
	if !t.After(now) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

// clusterNotes groups the candidate notes into near-duplicate clusters by
// token overlap (the lexical stand-in for the pg_trgm similarity the store
// pairs use), greedily and deterministically: notes are visited in id order
// and join the first cluster they clear the threshold against. Singleton
// clusters are dropped — nothing to merge. A cluster never crosses notes'
// visibility/owner boundary (the candidate set is single-tier by read).
func clusterNotes(notes []domain.MemoryNote, threshold float64) [][]domain.MemoryNote {
	ordered := make([]domain.MemoryNote, len(notes))
	copy(ordered, notes)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })

	var clusters [][]domain.MemoryNote
	for _, note := range ordered {
		joined := false
		for ci := range clusters {
			if len(clusters[ci]) >= maxMergeCluster {
				continue
			}
			if noteSimilarity(note.Content, clusters[ci][0].Content) > threshold {
				clusters[ci] = append(clusters[ci], note)
				joined = true
				break
			}
		}
		if !joined {
			clusters = append(clusters, []domain.MemoryNote{note})
		}
	}

	merged := [][]domain.MemoryNote{}
	for _, cluster := range clusters {
		if len(cluster) >= 2 {
			merged = append(merged, cluster)
		}
	}
	// Deterministic cluster order: by the canonical-first member's id.
	sort.Slice(merged, func(i, j int) bool { return merged[i][0].ID < merged[j][0].ID })
	return merged
}

// noteSimilarity is the consolidator's in-memory token-overlap stand-in for
// pg_trgm's similarity: the Jaccard overlap of lowercased word tokens,
// strictly-greater-than semantics matching the store's threshold reads.
func noteSimilarity(a, b string) float64 {
	tokens := func(s string) map[string]struct{} {
		set := make(map[string]struct{})
		for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			set[w] = struct{}{}
		}
		return set
	}
	as, bs := tokens(a), tokens(b)
	if len(as) == 0 || len(bs) == 0 {
		return 0
	}
	shared := 0
	for w := range as {
		if _, ok := bs[w]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(as)+len(bs)-shared)
}

// mergeDecision is the merge side-call's strict JSON answer.
type mergeDecision struct {
	CanonicalID string `json:"canonical_id"`
}

// topicAssignment is one topic-fold answer row.
type topicAssignment struct {
	ID    string `json:"id"`
	Topic string `json:"topic"`
}

// parseCanonicalID extracts the model's canonical pick, tolerating code
// fences and prose; empty when absent, invented, or ambiguous.
func parseCanonicalID(raw string, cluster []domain.MemoryNote) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return ""
	}
	var decision mergeDecision
	if err := json.Unmarshal([]byte(raw[start:end+1]), &decision); err != nil {
		return ""
	}
	id := strings.TrimSpace(decision.CanonicalID)
	for _, note := range cluster {
		if note.ID == id {
			return id
		}
	}
	return ""
}

// parseTopicAssignments extracts the topic-fold answer rows, tolerating
// code fences and prose; unparseable answers fold nothing this batch.
func parseTopicAssignments(raw string) []topicAssignment {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		return nil
	}
	var assignments []topicAssignment
	if err := json.Unmarshal([]byte(raw[start:end+1]), &assignments); err != nil {
		return nil
	}
	return assignments
}

const mergeSystemPrompt = `You are the memory consolidator for an AI agent workspace. Overnight, near-duplicate facts were grouped into one cluster; decide which single note should survive as the canonical one.

Emit ONLY a JSON object — no prose:
{"canonical_id":"<the id of the note whose content best represents the fact>"}

Rules:
- Choose exactly one id from the listed notes.
- Prefer the most complete, current, and self-contained phrasing.
- Never invent ids. If the listed notes are genuinely different facts, emit {"canonical_id":""} and nothing will merge.`

func mergeUserPrompt(cluster []domain.MemoryNote) string {
	var sb strings.Builder
	sb.WriteString("## Near-duplicate cluster (pick the canonical note)\n")
	for _, note := range cluster {
		fmt.Fprintf(&sb, "- [%s] %s\n", note.ID, truncateRunes(note.Content, 300))
	}
	return sb.String()
}

const topicSystemPrompt = `You are the memory consolidator for an AI agent workspace. Assign each fact a short browsable topic label.

Emit ONLY a JSON array — no prose, one object per note you can label:
[{"id":"<note id>","topic":"<one-to-three words>"}]

Rules:
- Topics are broad browsing categories (e.g. "deployments", "billing", "oncall"), lowercase, no punctuation.
- Skip notes you cannot label confidently.
- Never invent ids.`

func topicUserPrompt(batch []domain.MemoryNote) string {
	var sb strings.Builder
	sb.WriteString("## Unlabeled facts (assign a topic)\n")
	for _, note := range batch {
		fmt.Fprintf(&sb, "- [%s] %s\n", note.ID, truncateRunes(note.Content, 300))
	}
	return sb.String()
}
