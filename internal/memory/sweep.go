package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

// WriteGuardWindow is the mid-write guard (design risk note): a memory file
// modified within the last stretch is skipped by this sweep and re-read on
// the next, so a half-written note is never consolidated.
const WriteGuardWindow = 10 * time.Second

// Sweep outcomes recorded in the sweep row.
const (
	OutcomeOK    = "ok"
	OutcomeError = "error"
)

// SweepTarget names one agent's sweep: the store identity plus the workspace
// directory whose memory/ subtree is swept.
type SweepTarget struct {
	AgentID      string
	WorkspaceDir string
}

// SweepResult reports one target's sweep outcome; a nil Err is a successful
// sweep.
type SweepResult struct {
	Target SweepTarget
	Err    error
}

// Sweeper runs the per-agent consolidation sweep (design D7/D8). It holds a
// store and a clock — nothing else. The constructor deliberately takes no
// model, provider, or embedding dependency (the change's hard constraint:
// no part of the memory system makes an additional model call), and nothing
// on the sweep's call path reaches one.
type Sweeper struct {
	store store.Store
	// now is the clock, injectable for tests; the constructor sets it to
	// time.Now in UTC.
	now func() time.Time
}

// NewSweeper returns a sweeper over the store.
func NewSweeper(st store.Store) *Sweeper {
	return &Sweeper{store: st, now: func() time.Time { return time.Now().UTC() }}
}

// mem is the sweeper's derived-memory-state port: everything the sweep
// reads and writes goes through the store's Memory accessor.
func (s *Sweeper) mem() store.MemoryStore {
	return s.store.Memory()
}

// sweepCounts is the heartbeat's counts document (design D13): what one
// sweep saw and did, recorded as opaque JSON in the sweep row.
type sweepCounts struct {
	Candidates int `json:"candidates"`
	Promotions int `json:"promotions"`
	Archivals  int `json:"archivals"`
	Expired    int `json:"expired"`
}

// Sweep runs the sweep for every target in order. One target's failure is
// recorded with its reason and never stops the others (spec agent-memory:
// a sweep failure is isolated).
func (s *Sweeper) Sweep(ctx context.Context, targets []SweepTarget) []SweepResult {
	results := make([]SweepResult, 0, len(targets))
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			results = append(results, SweepResult{Target: t, Err: err})
			break
		}
		results = append(results, SweepResult{Target: t, Err: s.SweepAgent(ctx, t)})
	}
	return results
}

// ingestedFiles is one sweep's view of the memory directory: every readable
// file is read and parsed (reads mutate nothing — the managed index, the
// retention ordering, and the archive checks all need the full picture); the
// watermark only decides which files count as newly ingested for watermark
// advancement.
type ingestedFiles struct {
	// notes are the parsed active fact notes by name (malformed ones are
	// skipped with a logged reason).
	notes map[string]Note
	// noteMtimes are the files' modification times by note name — the
	// creation reference for the 30-day grace.
	noteMtimes map[string]time.Time
	// dailies are the daily notes, oldest first.
	dailies []dailyFile
	// ingested counts files inside [watermark, cutoff); skippedFresh counts
	// files inside the mid-write guard window.
	ingested     int
	skippedFresh int
}

// dailyFile is one daily note read whole: its memory-relative name, path,
// lines, and modification time.
type dailyFile struct {
	name  string
	path  string
	lines []string
	mtime time.Time
}

// SweepAgent runs one agent's consolidation sweep (spec agent-memory
// "Consolidation sweep"): ingest what changed since the watermark, dedupe
// candidates, compute signals, apply gates, update retention, archive,
// promote, regenerate the managed index section, and record the outcome.
// An agent without a memory directory is inert — the sweep records nothing
// but a heartbeat, and the directory stays turn-created.
func (s *Sweeper) SweepAgent(ctx context.Context, target SweepTarget) error {
	start := s.now()
	layout := NewLayout(target.WorkspaceDir)

	prev, _, err := s.mem().GetSweep(ctx, target.AgentID)
	if err != nil {
		return fmt.Errorf("memory: read sweep state for %s: %w", target.AgentID, err)
	}
	watermark := prev.Watermark

	// An agent without a memory directory is inert: the heartbeat records
	// that the sweep ran, and nothing else happens.
	if _, err := os.Stat(layout.Dir); os.IsNotExist(err) {
		return s.recordSuccess(ctx, target, watermark, sweepCounts{}, start)
	}

	// One clock reading governs the whole sweep: the guard cutoff, the
	// staging stamps, and the next watermark all derive from it.
	now := start
	files, err := ingestMemoryFiles(layout, watermark, now.Add(-WriteGuardWindow))
	if err != nil {
		return s.recordFailure(ctx, target, watermark, prev.Counts, start, err)
	}

	windowTraces, err := s.mem().ListTracesSince(ctx, target.AgentID, watermark)
	if err != nil {
		return s.recordFailure(ctx, target, watermark, prev.Counts, start,
			fmt.Errorf("memory: list traces since watermark: %w", err))
	}
	allTraces, err := s.mem().ListTracesSince(ctx, target.AgentID, time.Time{})
	if err != nil {
		return s.recordFailure(ctx, target, watermark, prev.Counts, start,
			fmt.Errorf("memory: list traces: %w", err))
	}

	counts, err := s.consolidate(ctx, target.AgentID, layout, files, windowTraces, allTraces, prev)
	if err != nil {
		return s.recordFailure(ctx, target, watermark, prev.Counts, start, err)
	}

	// The watermark advances to the guard cutoff exactly when the sweep
	// consumed input — ingested files, files it must re-read next time
	// (mid-write), or traces — so every consumed trace is counted once and
	// every skipped file is re-read. A sweep with no new input leaves the
	// watermark untouched.
	nextWatermark := prev.Watermark
	if files.ingested > 0 || files.skippedFresh > 0 || len(windowTraces) > 0 {
		nextWatermark = now.Add(-WriteGuardWindow)
	}
	return s.recordSuccess(ctx, target, nextWatermark, counts, start)
}

// consolidate is the sweep's middle: dedupe, signals, gates, retention,
// archive, promote, index. It returns the heartbeat counts.
func (s *Sweeper) consolidate(
	ctx context.Context,
	agentID string,
	layout Layout,
	files *ingestedFiles,
	windowTraces, allTraces []store.MemoryTrace,
	prev store.MemorySweep,
) (sweepCounts, error) {
	now := s.now()
	var counts sweepCounts

	// Staging first: candidates that never passed the gates within 30 days
	// of first staging leave staging and stop being candidates (design D8).
	expired, err := s.mem().ExpireStaging(ctx, agentID, now.Add(-StagingExpiryDays*24*time.Hour))
	if err != nil {
		return counts, fmt.Errorf("memory: expire staging: %w", err)
	}
	counts.Expired = expired

	// Promotion candidates: every non-blank line of a daily note hit by a
	// user-origin trace, deduplicated (design D11).
	cands := buildCandidates(files.dailies, windowTraces)
	cands = mergeDuplicateCandidates(cands)
	counts.Candidates = len(cands)

	signals, staged, err := s.stageCandidates(ctx, agentID, cands, now)
	if err != nil {
		return counts, err
	}

	// Retention (design D9).
	states, err := s.mem().ListNoteStates(ctx, agentID)
	if err != nil {
		return counts, fmt.Errorf("memory: list note states: %w", err)
	}
	phi, err := s.updateRetention(ctx, agentID, files, windowTraces, allTraces, states, prev, now)
	if err != nil {
		return counts, err
	}

	// Archive: superseded notes, then notes below the floor — never inside
	// the creation grace, and always a move (design D11).
	archived, err := s.archiveNotes(layout, files, phi, now)
	if err != nil {
		return counts, err
	}
	counts.Archivals = len(archived)

	// Promotion: the gated candidates, at most ten, never one twice
	// (design D8/D10).
	promoted, err := s.promote(ctx, agentID, layout, cands, signals, staged, now)
	if err != nil {
		return counts, err
	}
	counts.Promotions = len(promoted)

	// Index: regenerate the managed section from the active notes minus the
	// superseded and just-archived ones, plus the promoted subsection
	// (design D10/D14).
	if err := s.regenerateIndex(layout, files, phi, archived, promoted); err != nil {
		return counts, err
	}
	return counts, nil
}

// buildCandidates collects the promotion candidates: every non-blank line of
// a daily note that received at least one user-origin trace hit, each line
// carrying the file's evidence and its wiki-link count. The candidates come
// out in a stable order (oldest daily note first, then line number) so
// dedupe and the promotion cap are deterministic.
func buildCandidates(dailies []dailyFile, windowTraces []store.MemoryTrace) []LineCandidate {
	ordered := make([]dailyFile, len(dailies))
	copy(ordered, dailies)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].name < ordered[j].name })

	var cands []LineCandidate
	for _, d := range ordered {
		ev := EvidenceFromTraces(windowTraces, d.name)
		if len(ev.Scores) == 0 {
			continue
		}
		for i, line := range d.lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			cands = append(cands, LineCandidate{
				Line:      line,
				SourceRef: sourceRef(d.name, i+1),
				Evidence:  ev,
				Concepts:  len(ExtractWikiLinks(line)),
			})
		}
	}
	return cands
}

// stageCandidates merges every candidate's evidence into its staging row
// (creating the row on first sight) and returns the candidates' cumulative
// signals aligned with the input, plus the staging rows by source reference.
func (s *Sweeper) stageCandidates(ctx context.Context, agentID string, cands []LineCandidate, now time.Time) ([]Signals, map[string]store.MemoryStaging, error) {
	signals := make([]Signals, len(cands))
	if len(cands) == 0 {
		return signals, map[string]store.MemoryStaging{}, nil
	}
	rows, err := s.mem().ListStaging(ctx, agentID)
	if err != nil {
		return nil, nil, fmt.Errorf("memory: list staging: %w", err)
	}
	byRef := make(map[string]store.MemoryStaging, len(rows))
	for _, r := range rows {
		byRef[r.SourceRef] = r
	}
	for i, c := range cands {
		row, existed := byRef[c.SourceRef]
		cur, err := decodeStagedSignals(row.Signals)
		if err != nil {
			// An unreadable signals document starts fresh; the row's
			// timestamps are what matter for expiry.
			slog.Warn("memory: resetting unreadable staging signals", "source", c.SourceRef, "reason", err)
			cur = stagedSignals{}
		}
		cur = appendStagedSignals(cur, c.Evidence)
		signals[i] = ComputeSignals(cur.evidence(), c.Concepts, now)

		row.AgentID = agentID
		row.SourceRef = c.SourceRef
		row.Signals, err = cur.encode()
		if err != nil {
			return nil, nil, err
		}
		if !existed || row.FirstStaged.IsZero() {
			row.FirstStaged = now
		}
		if err := s.mem().UpsertStaging(ctx, row); err != nil {
			return nil, nil, fmt.Errorf("memory: upsert staging %s: %w", c.SourceRef, err)
		}
		byRef[c.SourceRef] = row
	}
	return signals, byRef, nil
}

// updateRetention advances every active note's survival potential (design
// D9) and upserts the changed note states. It returns each note's new Φ by
// name — first-sight notes start at their importance, known notes gain
// I·Γ·Ψ and lose λ per day since the previous sweep.
func (s *Sweeper) updateRetention(
	ctx context.Context,
	agentID string,
	files *ingestedFiles,
	windowTraces, allTraces []store.MemoryTrace,
	states []store.MemoryNoteState,
	prev store.MemorySweep,
	now time.Time,
) (map[string]float64, error) {
	stateByName := make(map[string]store.MemoryNoteState, len(states))
	for _, st := range states {
		stateByName[st.Name] = st
	}

	// Note strengths for the Ψ dampening: stored retention for known notes,
	// zero for first-sight ones (their importance enters after this pass).
	// Ties break by name so exactly one of a duplicate pair is the dampened
	// one.
	type strength struct {
		phi  float64
		text string
	}
	strengths := make(map[string]strength, len(files.notes))
	names := make([]string, 0, len(files.notes))
	for name, note := range files.notes {
		names = append(names, name)
		text := note.Description + "\n" + note.Body
		phi := 0.0
		if st, ok := stateByName[name]; ok {
			phi = st.Retention
		}
		strengths[name] = strength{phi: phi, text: text}
	}
	sort.Strings(names)

	// Window recall statistics per note and the window's total, for the
	// importance's relevance and repetition terms.
	windowRecalls := make(map[string]int, len(names))
	windowRelevance := make(map[string]float64, len(names))
	totalWindowRecalls := 0
	for _, name := range names {
		ev := EvidenceFromTraces(windowTraces, name+".md")
		windowRecalls[name] = len(ev.Scores)
		windowRelevance[name] = mean(ev.Scores)
		totalWindowRecalls += len(ev.Scores)
	}

	phiOut := make(map[string]float64, len(names))
	for _, name := range names {
		// Novelty: 1 − the strongest token-set similarity to any other
		// active note; a lone note is fully novel.
		novelty := 1.0
		for other, st := range strengths {
			if other == name {
				continue
			}
			if sim := tokenSetJaccard(strengths[name].text, st.text); 1-sim < novelty {
				novelty = 1 - sim
			}
		}

		// Ψ: 1 − the strongest similarity to a strictly stronger note
		// (retention descending, name ascending on ties).
		psi := 1.0
		for other, st := range strengths {
			if other == name || !strongerThan(st.phi, other, strengths[name].phi, name) {
				continue
			}
			if sim := tokenSetJaccard(strengths[name].text, st.text); 1-sim < psi {
				psi = 1 - sim
			}
		}

		repetition := 0.0
		if totalWindowRecalls > 0 {
			repetition = float64(windowRecalls[name]) / float64(totalWindowRecalls)
		}
		importance := Importance(novelty, windowRelevance[name], repetition)

		// The cumulative counters are recomputed from the full trace
		// history — exact and self-healing whatever the window held.
		fullEv := EvidenceFromTraces(allTraces, name+".md")
		var lastRecall *time.Time
		if last := latest(fullEv.Ats); !last.IsZero() {
			t := last
			lastRecall = &t
		}

		prevState, known := stateByName[name]
		var newPhi float64
		if !known {
			newPhi = InitialRetention(importance)
		} else {
			dtDays := 0.0
			switch {
			case prev.LastFinish != nil:
				dtDays = now.Sub(*prev.LastFinish).Hours() / 24
			case prev.LastStart != nil:
				dtDays = now.Sub(*prev.LastStart).Hours() / 24
			}
			newPhi = UpdateRetention(prevState.Retention, importance, windowRecalls[name], psi, dtDays)
		}
		phiOut[name] = newPhi

		next := store.MemoryNoteState{
			AgentID:         agentID,
			Name:            name,
			Retention:       newPhi,
			RecallCount:     len(fullEv.Scores),
			DistinctQueries: len(distinctStrings(fullEv.Queries)),
			LastRecall:      lastRecall,
			FirstSeen:       now,
		}
		if known {
			next.FirstSeen = prevState.FirstSeen
		}
		if known && noteStateEqual(prevState, next) {
			continue
		}
		if err := s.mem().UpsertNoteState(ctx, next); err != nil {
			return nil, fmt.Errorf("memory: upsert note state %s: %w", name, err)
		}
		stateByName[name] = next
	}
	return phiOut, nil
}

// strongerThan orders notes for the Ψ dampening: strictly higher retention
// wins, and an equal retention is broken by name ascending so exactly one of
// a near-duplicate pair damps the other.
func strongerThan(phiA float64, nameA string, phiB float64, nameB string) bool {
	if phiA != phiB {
		return phiA > phiB
	}
	return nameA < nameB
}

// noteStateEqual reports whether two note states carry identical derived
// content — the write-on-change gate that keeps a no-op sweep from touching
// rows.
func noteStateEqual(a, b store.MemoryNoteState) bool {
	if a.Retention != b.Retention ||
		a.RecallCount != b.RecallCount ||
		a.DistinctQueries != b.DistinctQueries ||
		!a.FirstSeen.Equal(b.FirstSeen) {
		return false
	}
	switch {
	case a.LastRecall == nil && b.LastRecall == nil:
		return true
	case a.LastRecall == nil || b.LastRecall == nil:
		return false
	default:
		return a.LastRecall.Equal(*b.LastRecall)
	}
}

// archiveNotes moves superseded and below-floor notes into the archive —
// never inside the creation grace (which a note file's modification time
// defines, so a moved-back note is protected anew) and never a delete. It
// returns the names that moved (and must therefore leave this sweep's
// index). Superseded notes leave the index on the sweep that observes the
// superseding note regardless of the grace; only the file move waits it out.
func (s *Sweeper) archiveNotes(layout Layout, files *ingestedFiles, phi map[string]float64, now time.Time) (map[string]bool, error) {
	superseded := make(map[string]string)
	for _, note := range files.notes {
		for _, old := range note.Supersedes {
			superseded[old] = note.Name
		}
	}

	names := make([]string, 0, len(files.noteMtimes))
	for name := range files.noteMtimes {
		names = append(names, name)
	}
	sort.Strings(names)

	moved := make(map[string]bool)
	for _, name := range names {
		var reason string
		if by, ok := superseded[name]; ok {
			reason = "superseded by " + by
		} else if p, ok := phi[name]; ok && BelowFloor(p) {
			reason = "retention below floor"
		} else {
			continue
		}
		if InGrace(now, files.noteMtimes[name]) {
			continue
		}
		if err := ArchiveNote(layout, name); err != nil {
			archiveSkips(name, err.Error())
			continue
		}
		slog.Info("memory: archived note", "note", name, "reason", reason)
		moved[name] = true
	}
	return moved, nil
}

// promote gates the staged candidates, appends the passing ones to the
// index's promoted subsection, and flags their staging rows — never the same
// source reference twice (its staging flag or an existing index entry both
// stop it), at most ten per sweep (design D8/D10).
func (s *Sweeper) promote(
	ctx context.Context,
	agentID string,
	layout Layout,
	cands []LineCandidate,
	signals []Signals,
	staged map[string]store.MemoryStaging,
	now time.Time,
) ([]PromotedLine, error) {
	if len(cands) == 0 {
		return nil, nil
	}
	already, err := s.promotedRefs(ctx, agentID, layout)
	if err != nil {
		return nil, err
	}
	chosen := selectPromotions(cands, signals, already)
	if len(chosen) == 0 {
		return nil, nil
	}

	newEntries := make([]PromotedLine, 0, len(chosen))
	for _, c := range chosen {
		row := staged[c.SourceRef]
		row.AgentID = agentID
		row.SourceRef = c.SourceRef
		row.Promoted = true
		if row.FirstStaged.IsZero() {
			row.FirstStaged = now
		}
		if err := s.mem().UpsertStaging(ctx, row); err != nil {
			return nil, fmt.Errorf("memory: flag staging %s promoted: %w", c.SourceRef, err)
		}
		staged[c.SourceRef] = row
		newEntries = append(newEntries, promotedLine(c, now))
	}
	return newEntries, nil
}

// promotedRefs collects every source reference that must not promote again:
// the staging rows flagged promoted and the entries already in the index's
// promoted subsection — the index is the on-disk truth a rebuilt store must
// respect, so both gates hold.
func (s *Sweeper) promotedRefs(ctx context.Context, agentID string, layout Layout) (map[string]bool, error) {
	refs := make(map[string]bool)
	rows, err := s.mem().ListStaging(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("memory: list staging: %w", err)
	}
	for _, r := range rows {
		if r.Promoted {
			refs[r.SourceRef] = true
		}
	}
	raw, err := os.ReadFile(layout.IndexPath())
	if err == nil {
		_, entries := ParseIndex(string(raw))
		for _, e := range entries {
			refs[e.SourceRef] = true
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("memory: read %s: %w", layout.IndexPath(), err)
	}
	return refs, nil
}

// regenerateIndex rebuilds the managed section from the active notes — minus
// the superseded ones, which leave the index on the sweep that observes
// their successor, and minus the ones this sweep just archived — ordered by
// retention, plus the promoted subsection; it writes the file only when its
// content changed (design D14).
func (s *Sweeper) regenerateIndex(layout Layout, files *ingestedFiles, phi map[string]float64, archived map[string]bool, promoted []PromotedLine) error {
	superseded := make(map[string]bool)
	for _, note := range files.notes {
		for _, old := range note.Supersedes {
			superseded[old] = true
		}
	}

	notes := make([]IndexNote, 0, len(files.notes))
	for name, note := range files.notes {
		if superseded[name] || archived[name] {
			continue
		}
		notes = append(notes, IndexNote{Name: name, Description: note.Description, Retention: phi[name]})
	}

	raw, err := os.ReadFile(layout.IndexPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("memory: read %s: %w", layout.IndexPath(), err)
	}
	free, existing := ParseIndex(string(raw))
	content := RenderIndex(free, notes, append(existing, promoted...))

	if _, err := WriteIndexFile(layout.IndexPath(), content); err != nil {
		return fmt.Errorf("memory: write %s: %w", layout.IndexPath(), err)
	}
	return nil
}

// recordSuccess writes the heartbeat: the watermark, start and finish, the
// outcome, and the counts (design D13).
func (s *Sweeper) recordSuccess(ctx context.Context, target SweepTarget, watermark time.Time, counts sweepCounts, start time.Time) error {
	finish := s.now()
	encoded, err := json.Marshal(counts)
	if err != nil {
		return fmt.Errorf("memory: encode sweep counts: %w", err)
	}
	if err := s.mem().PutSweep(ctx, store.MemorySweep{
		AgentID:    target.AgentID,
		Watermark:  watermark,
		LastStart:  &start,
		LastFinish: &finish,
		Outcome:    OutcomeOK,
		Counts:     string(encoded),
	}); err != nil {
		return fmt.Errorf("memory: record sweep for %s: %w", target.AgentID, err)
	}
	return nil
}

// recordFailure records a failed sweep with its reason and the untouched
// watermark, and returns the cause. A failure changes no file and no derived
// row beyond the heartbeat.
func (s *Sweeper) recordFailure(ctx context.Context, target SweepTarget, watermark time.Time, prevCounts string, start time.Time, cause error) error {
	finish := s.now()
	outcome := OutcomeError + ": " + truncateReason(cause.Error())
	if err := s.mem().PutSweep(ctx, store.MemorySweep{
		AgentID:    target.AgentID,
		Watermark:  watermark,
		LastStart:  &start,
		LastFinish: &finish,
		Outcome:    outcome,
		Counts:     prevCounts,
	}); err != nil {
		return fmt.Errorf("%w (recording the failure also failed: %v)", cause, err)
	}
	return cause
}

// truncateReason caps a failure reason so the outcome token stays short.
func truncateReason(reason string) string {
	const maxReasonLen = 200
	if len(reason) <= maxReasonLen {
		return reason
	}
	return reason[:maxReasonLen]
}

// ingestMemoryFiles reads the memory directory: every readable .md file is
// parsed (the index file and the archive subtree are not notes), daily notes
// by name pattern, everything else as a fact note. Files inside the mid-write
// guard window are skipped for this sweep; the watermark only classifies what
// counts as newly ingested. A missing memory directory ingests nothing.
func ingestMemoryFiles(layout Layout, watermark, cutoff time.Time) (*ingestedFiles, error) {
	res := &ingestedFiles{
		notes:      make(map[string]Note),
		noteMtimes: make(map[string]time.Time),
	}
	entries, err := os.ReadDir(layout.Dir)
	if os.IsNotExist(err) {
		return res, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory: read %s: %w", layout.Dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == IndexFileName {
			continue
		}
		info, err := e.Info()
		if err != nil {
			slog.Warn("memory: skipping unreadable entry", "path", filepath.Join(layout.Dir, e.Name()), "reason", err)
			continue
		}
		mtime := info.ModTime()
		if !mtime.Before(cutoff) {
			res.skippedFresh++
			continue
		}
		if !mtime.Before(watermark) {
			res.ingested++
		}
		path := filepath.Join(layout.Dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("memory: skipping unreadable file", "path", path, "reason", err)
			continue
		}
		if isDailyNoteFile(e.Name()) {
			res.dailies = append(res.dailies, dailyFile{
				name:  e.Name(),
				path:  path,
				lines: strings.Split(string(raw), "\n"),
				mtime: mtime,
			})
			continue
		}
		if note, ok := ParseNote(path, raw); ok {
			res.notes[note.Name] = note
			res.noteMtimes[note.Name] = mtime
		}
	}
	sort.Slice(res.dailies, func(i, j int) bool { return res.dailies[i].name < res.dailies[j].name })
	return res, nil
}

// isDailyNoteFile reports whether a file name is a daily note: YYYY-MM-DD.md
// (the full file name, unlike index.go's isDailyName which takes the bare
// stem).
func isDailyNoteFile(name string) bool {
	if !strings.HasSuffix(name, ".md") {
		return false
	}
	_, err := time.Parse(dailyFormat, strings.TrimSuffix(name, ".md"))
	return err == nil
}
