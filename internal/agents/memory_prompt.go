package agents

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/oniharnantyo/buddies/internal/memory"
	"github.com/oniharnantyo/buddies/internal/store"
)

// memoryGuidance is the curation guidance composed after the knowledge
// guidance on parent turns (add-agent-memory design D2/D3): where the memory
// files live, what to save and how (the frontmatter schema, the daily-note
// purpose, the supersede-don't-contradict convention), and the directed
// activation of the memory skill — using the jailed write/edit tools the
// turn already has. The block tells the model HOW; it adds no tools, and the
// memory path makes no model call (the change's hard constraint).
const memoryGuidance = `## Memory

You keep long-term memory as plain Markdown under /workspace/memory, and you curate it yourself during your own turns with the write and edit tools. MEMORY.md is the curated index, fact notes are atomic files written to exactly /workspace/memory/<name>.md (the memory directory is one level deep — never memory/memory/), and /workspace/memory/YYYY-MM-DD.md is the day's working note for observations not yet durable enough for a fact note. Save what should outlive the session: durable facts about the user and the project, stated preferences, decisions reached, corrections the user makes. A fact note's frontmatter carries name (equal to the filename without .md), description (one line), and type (one of user, feedback, project, reference), plus optional concepts, relations, and supersedes. To replace an outdated note, write a new note whose supersedes names the old one — never edit the old note into a contradiction. Activate the memory skill ($memory) whenever a request concerns the user's preferences, past decisions, or earlier work.`

// The memory block's budgets and gates (add-agent-memory design D3/D9): the
// injected index copy truncates at 3,200 characters (≈ 800 tokens, Hermes'
// operating point) while the file on disk stays intact; the notes section
// carries at most five notes within 6,000 characters, and only notes whose
// retention score meets the injection threshold qualify at all — silence is
// the default.
const (
	memoryIndexBudget        = 3200
	memoryNotesBudget        = 6000
	memoryMaxNotes           = 5
	memoryInjectionThreshold = 0.3
)

// memorySweepInterval is the consolidation sweep cadence (add-agent-memory
// design D7), single-sourced from the sweep ticker's interval.
const memorySweepInterval = memory.SweepInterval

// The memory block's notices (spec agent-memory "Memory block in every
// turn"). Each is distinctive so a test can pin that it appears only in its
// own case.
const (
	memoryTruncatedNotice   = "(The index is truncated at 3,200 characters; the full MEMORY.md on disk is intact — read /workspace/memory/MEMORY.md for the rest.)"
	memoryEmptyNotice       = "Memory is currently empty — nothing has been saved yet. Record what deserves to outlive this session."
	memoryUnavailableNotice = "Memory is currently unavailable (the memory directory could not be read); this turn proceeds without it."
	memoryStaleNotice       = "Memory maintenance is stale: the background consolidation sweep has not completed recently, so the index above may lag the notes on disk."
)

// memoryNoteView is one note's injected view: the frontmatter identity plus
// the body text, read from disk for the turn. Notes are the source of truth —
// the view is rebuilt from the file every turn, never persisted.
type memoryNoteView struct {
	Name        string
	Description string
	Type        string
	Body        string
}

// memoryBlockInput carries everything one memory block renders from: the
// curated index text, the qualifying notes in retention order, whether the
// memory directory holds no content at all, and whether the sweep heartbeat
// is stale. No timestamps ride in — the same inputs must render a
// byte-identical block so the prompt prefix stays cacheable (design D14).
type memoryBlockInput struct {
	// index is the MEMORY.md text, "" when the index is missing or blank.
	index string
	// notes are the qualifying notes, best first; the builder applies the
	// count and character budgets.
	notes []memoryNoteView
	// empty reports a memory directory with no content at all.
	empty bool
	// stale reports a stale sweep heartbeat while the gateway is Running.
	stale bool
}

// memoryBlock renders one turn's memory block (spec agent-memory "Memory
// block in every turn"): the curation guidance, then the index — truncated
// in the injected copy only — then the qualifying notes, then the
// stale-maintenance notice. The empty notice replaces the index and notes
// sections when the memory directory has no content; the notes section is
// absent entirely when nothing qualifies (silence as default). The output
// carries no timestamps: unchanged inputs render byte-identical output
// (design D14).
func memoryBlock(in memoryBlockInput) string {
	var sb strings.Builder
	sb.WriteString(memoryGuidance)
	if in.empty {
		sb.WriteString("\n\n")
		sb.WriteString(memoryEmptyNotice)
		return sb.String()
	}
	if section, truncated := memoryIndexSection(in.index); section != "" {
		sb.WriteString("\n\n")
		sb.WriteString(section)
		if truncated {
			sb.WriteString("\n\n")
			sb.WriteString(memoryTruncatedNotice)
		}
	}
	if section := memoryNotesSection(in.notes); section != "" {
		sb.WriteString("\n\n")
		sb.WriteString(section)
	}
	if in.stale {
		sb.WriteString("\n\n")
		sb.WriteString(memoryStaleNotice)
	}
	return sb.String()
}

// memoryUnavailableBlock degrades the block on a layout or read failure
// (spec agent-memory "Read failure does not fail the turn"): guidance plus
// the unavailable notice, never an error — the turn always proceeds.
func memoryUnavailableBlock() string {
	return memoryGuidance + "\n\n" + memoryUnavailableNotice
}

// memoryIndexSection renders the index section: the heading plus the
// injected index copy, truncated to memoryIndexBudget characters when the
// file is longer (the file on disk stays intact). A blank index composes no
// section. The second result reports whether truncation happened, so the
// caller can append the truncation notice.
func memoryIndexSection(index string) (section string, truncated bool) {
	if strings.TrimSpace(index) == "" {
		return "", false
	}
	if truncated = utf8.RuneCountInString(index) > memoryIndexBudget; truncated {
		index = string([]rune(index)[:memoryIndexBudget])
	}
	return "### Index\n\n" + index, truncated
}

// memoryNotesSection renders the qualifying notes (design D3): at most
// memoryMaxNotes whole notes within memoryNotesBudget characters, in the
// given (retention) order. A note that no longer fits is dropped whole, not
// half-included; when no note fits — or none were given — the section is
// composed as nothing.
func memoryNotesSection(notes []memoryNoteView) string {
	if len(notes) == 0 {
		return ""
	}
	var sb strings.Builder
	used := 0
	count := 0
	for _, note := range notes {
		if count == memoryMaxNotes {
			break
		}
		entry := renderMemoryNote(note)
		if count > 0 {
			entry = "\n\n" + entry
		}
		if used+utf8.RuneCountInString(entry) > memoryNotesBudget {
			break
		}
		sb.WriteString(entry)
		used += utf8.RuneCountInString(entry)
		count++
	}
	if count == 0 {
		return ""
	}
	return "### Recalled notes\n\n" + sb.String()
}

// renderMemoryNote renders one note's injected form: the identity line
// (name, type, description) over the body. No timestamps — the body is the
// note's own text.
func renderMemoryNote(note memoryNoteView) string {
	var sb strings.Builder
	sb.WriteString("**" + note.Name + "**")
	if note.Type != "" {
		sb.WriteString(" (" + note.Type + ")")
	}
	if note.Description != "" {
		sb.WriteString(" — " + note.Description)
	}
	sb.WriteString("\n\n")
	sb.WriteString(note.Body)
	return sb.String()
}

// gatewayRunningRead holds the composition root's gateway Running read
// (add-agent-memory task 6.7): installed once at startup, read on every
// turn's staleness check. Nil — tests, hosts that never wire it — reads as
// not-Running, which is never stale.
var gatewayRunningRead atomic.Pointer[func() bool]

// SetMemoryGatewayRunning installs the gateway Running read the memory
// block's staleness check consumes (add-agent-memory task 6.7). The
// composition root calls it once beside the sweep ticker's gateway
// attachment; passing nil reverts to the not-Running default.
func SetMemoryGatewayRunning(running func() bool) {
	if running == nil {
		gatewayRunningRead.Store(nil)
		return
	}
	gatewayRunningRead.Store(&running)
}

// gatewayRunning reports the live gateway state: Running only when the
// installed read says so.
func gatewayRunning() bool {
	if read := gatewayRunningRead.Load(); read != nil {
		return (*read)()
	}
	return false
}

// memorySweepStale reports a stale maintenance heartbeat: the memory
// ticker's staleness rule (add-agent-memory design D13, memory.SweepStale)
// — with the gateway Running, no completed sweep within twice the interval
// is stale; a gateway that is not Running is never stale, because sweeps
// are skipped while Paused by design and cold is not dead.
func memorySweepStale(sw store.MemorySweep, found, gatewayRunning bool, at time.Time) bool {
	return memory.SweepStale(sw, found, gatewayRunning, at)
}

// memoryBlock composes one turn's memory block from the agent's workspace
// memory (add-agent-memory tasks 2.2/2.3): ensure the layout, read the index
// and the qualifying notes, read the sweep heartbeat from the store, and hand
// all of it to the builder. Fail-soft end to end (spec agent-memory "Read
// failure does not fail the turn"): every failure degrades to the unavailable
// notice, and an agent without a workspace composes no block at all.
func (m *manager) memoryBlock(ctx context.Context, agent store.Agent) string {
	if m.workspaces == nil {
		return ""
	}
	workspaceDir, err := m.workspaces.Dir(agent.Slug)
	if err != nil {
		slog.Warn("compose memory block", "agent", agent.Slug, "error", err)
		return memoryUnavailableBlock()
	}
	layout := memory.NewLayout(workspaceDir)
	// The directory is created on demand the first time a turn runs for the
	// agent (spec agent-memory "Memory directory created on demand").
	if err := memory.EnsureLayout(layout); err != nil {
		slog.Warn("compose memory block", "agent", agent.Slug, "error", err)
		return memoryUnavailableBlock()
	}
	index, empty, err := readMemoryIndex(layout)
	if err != nil {
		slog.Warn("compose memory block", "agent", agent.Slug, "error", err)
		return memoryUnavailableBlock()
	}
	notes, err := m.qualifyingMemoryNotes(ctx, agent.ID, layout)
	if err != nil {
		slog.Warn("compose memory block", "agent", agent.Slug, "error", err)
		return memoryUnavailableBlock()
	}
	sw, found, err := m.store.Memory().GetSweep(ctx, agent.ID)
	if err != nil {
		slog.Warn("compose memory block", "agent", agent.Slug, "error", err)
		return memoryUnavailableBlock()
	}
	// Staleness is evaluated against the live gateway the sweep ticker
	// follows (add-agent-memory design D13): while the gateway is not
	// Running, sweeps are skipped by design and cold is not dead.
	stale := memorySweepStale(sw, found, gatewayRunning(), now())
	return memoryBlock(memoryBlockInput{index: index, notes: notes, empty: empty, stale: stale})
}

// readMemoryIndex reads the curated index and decides emptiness: the index
// text ("" when the file is missing or blank) and whether the memory
// directory holds no content at all — no index, no notes, no daily notes.
// Subdirectories (the archive) do not count as content.
func readMemoryIndex(layout memory.Layout) (index string, empty bool, err error) {
	entries, err := os.ReadDir(layout.Dir)
	if err != nil {
		return "", false, err
	}
	raw, err := os.ReadFile(layout.IndexPath())
	switch {
	case err == nil:
		index = string(raw)
	case os.IsNotExist(err):
		index = ""
	default:
		return "", false, err
	}
	if strings.TrimSpace(index) != "" {
		return index, false, nil
	}
	empty = true
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == memory.IndexFileName {
			continue
		}
		if entry.Type().IsRegular() {
			empty = false
			break
		}
	}
	return "", empty, nil
}

// qualifyingMemoryNotes reads the notes eligible for injection (design
// D3/D9): retention state at or above the injection threshold, best first,
// capped at memoryMaxNotes, each note read from disk and parsed — the files
// are the source of truth, so a missing file or a malformed note is skipped
// (ParseNote logs the reason) even when its retention row survives.
func (m *manager) qualifyingMemoryNotes(ctx context.Context, agentID string, layout memory.Layout) ([]memoryNoteView, error) {
	states, err := m.store.Memory().ListNoteStates(ctx, agentID)
	if err != nil {
		return nil, err
	}
	// Retention descending, name ascending on ties: the injected order must
	// be deterministic for the block to be byte-stable (design D14).
	sort.Slice(states, func(i, j int) bool {
		if states[i].Retention != states[j].Retention {
			return states[i].Retention > states[j].Retention
		}
		return states[i].Name < states[j].Name
	})
	views := make([]memoryNoteView, 0, memoryMaxNotes)
	for _, st := range states {
		if len(views) == memoryMaxNotes {
			break
		}
		// Sorted descending: the first below-threshold state ends the walk.
		if st.Retention < memoryInjectionThreshold {
			break
		}
		raw, err := os.ReadFile(layout.NotePath(st.Name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		note, ok := memory.ParseNote(layout.NotePath(st.Name), raw)
		if !ok {
			continue
		}
		views = append(views, memoryNoteView{
			Name:        note.Name,
			Description: note.Description,
			Type:        note.Type,
			Body:        note.Body,
		})
	}
	return views, nil
}
