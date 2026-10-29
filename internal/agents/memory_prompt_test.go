package agents

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// The memory block builder tests (tasks.md 2.1): the guidance names the
// curation conventions, unchanged inputs render byte-identical output, each
// notice appears only in its own case, and the index and notes budgets hold
// (add-agent-memory design D3/D14).

// TestMemoryGuidance_NamesConventions spot-checks the claims the spec
// depends on (spec agent-memory "Guidance names the conventions"): the note
// types, the frontmatter fields, the daily-note purpose, the supersede
// convention, the jailed write/edit tools, and the memory-skill activation.
func TestMemoryGuidance_NamesConventions(t *testing.T) {
	for _, fragment := range []string{
		"user, feedback, project, reference", // the four note types
		"name",                               // frontmatter: name
		"description",                        // frontmatter: description
		"type",                               // frontmatter: type
		"concepts",                           // frontmatter: concepts
		"relations",                          // frontmatter: relations
		"supersedes",                         // frontmatter: supersedes
		"MEMORY.md",                          // the curated index
		"memory/YYYY-MM-DD.md",               // the daily working note
		"write and edit tools",               // in-turn curation, no new tools
		"never edit the old note into a contradiction", // supersede convention
		"$memory", // directed skill activation
	} {
		require.Containsf(t, memoryGuidance, fragment, "guidance must name %q", fragment)
	}
}

// TestMemoryBlock_DeterministicByteIdentical pins byte stability (design
// D14): the same inputs rebuilt from scratch render byte-identical output,
// and the block carries no per-turn timestamps.
func TestMemoryBlock_DeterministicByteIdentical(t *testing.T) {
	build := func() string {
		return memoryBlock(memoryBlockInput{
			index: "# index\n\n- first-note — the first note\n",
			notes: []memoryNoteView{
				{Name: "first-note", Description: "the first note", Type: "user", Body: "The body."},
			},
			stale: true,
		})
	}
	first := build()
	require.Equal(t, first, build(), "unchanged inputs must render byte-identical output")
	require.NotContains(t, first, now().Format("2006-01-02"), "the block carries no per-turn timestamps")
}

// TestMemoryBlock_IndexBudget covers the index budget (design D3): a short
// index is injected whole, an oversized one truncates in the injected copy
// only — with the truncation notice — and the budget counts characters, not
// bytes. A blank index composes no index section.
func TestMemoryBlock_IndexBudget(t *testing.T) {
	t.Run("short index stays whole", func(t *testing.T) {
		index := "# index\n\n- first-note — the first note\n"
		got := memoryBlock(memoryBlockInput{index: index})
		require.Contains(t, got, index)
		require.NotContains(t, got, memoryTruncatedNotice)
	})
	t.Run("oversized index truncates with notice", func(t *testing.T) {
		// 3,200 injected characters, then a distinctive tail the cut removes.
		index := strings.Repeat("x", memoryIndexBudget-1) + "Y" + strings.Repeat("z", 800)
		got := memoryBlock(memoryBlockInput{index: index})
		require.Contains(t, got, strings.Repeat("x", memoryIndexBudget-1)+"Y")
		require.NotContains(t, got, "zzz", "the tail beyond the budget must not ride")
		require.Contains(t, got, memoryTruncatedNotice)
	})
	t.Run("budget counts characters not bytes", func(t *testing.T) {
		// 2,000 two-byte runes: 4,000 bytes but under the character budget.
		index := strings.Repeat("é", 2000)
		got := memoryBlock(memoryBlockInput{index: index})
		require.Contains(t, got, index)
		require.NotContains(t, got, memoryTruncatedNotice)
	})
	t.Run("blank index composes no section", func(t *testing.T) {
		got := memoryBlock(memoryBlockInput{index: ""})
		require.NotContains(t, got, "### Index")
	})
}

// TestMemoryBlock_NotesSectionBudget covers the notes section (design D3):
// absent when nothing qualifies, at most five notes, and within 6,000
// characters — an oversized section drops whole notes rather than
// half-including one.
func TestMemoryBlock_NotesSectionBudget(t *testing.T) {
	t.Run("no notes composes nothing", func(t *testing.T) {
		got := memoryBlock(memoryBlockInput{})
		require.NotContains(t, got, "### Recalled notes")
	})
	t.Run("six notes keeps five", func(t *testing.T) {
		notes := make([]memoryNoteView, 0, 6)
		for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
			notes = append(notes, memoryNoteView{Name: name, Type: "user", Body: "body " + name})
		}
		got := memoryBlock(memoryBlockInput{notes: notes})
		require.Contains(t, got, "**a**")
		require.Contains(t, got, "**e**")
		require.NotContains(t, got, "**f**", "at most five notes ride")
	})
	t.Run("oversized notes stay within the budget", func(t *testing.T) {
		notes := []memoryNoteView{
			{Name: "n1", Type: "user", Body: strings.Repeat("a", 4000)},
			{Name: "n2", Type: "user", Body: strings.Repeat("b", 4000)},
		}
		got := memoryBlock(memoryBlockInput{notes: notes})
		require.Contains(t, got, "**n1**")
		require.NotContains(t, got, "**n2**", "a note that no longer fits is dropped whole")
		section := got[strings.Index(got, "### Recalled notes"):]
		require.LessOrEqual(t, utf8.RuneCountInString(section), memoryNotesBudget)
	})
}

// TestMemoryBlock_NoticesExclusive pins that each notice appears only in its
// own case (tasks.md 2.1): a plain block carries none, and the empty, stale,
// truncation, and unavailable cases carry exactly their own.
func TestMemoryBlock_NoticesExclusive(t *testing.T) {
	plain := memoryBlock(memoryBlockInput{
		index: "short index",
		notes: []memoryNoteView{{Name: "n", Type: "user", Body: "body"}},
	})
	oversized := memoryBlock(memoryBlockInput{index: strings.Repeat("x", memoryIndexBudget+1)})
	empty := memoryBlock(memoryBlockInput{empty: true})
	stale := memoryBlock(memoryBlockInput{index: "short index", stale: true})
	unavailable := memoryUnavailableBlock()

	// The plain block is silent: no notice at all.
	for _, notice := range []string{
		memoryTruncatedNotice, memoryEmptyNotice, memoryUnavailableNotice, memoryStaleNotice,
	} {
		require.NotContains(t, plain, notice)
	}

	cases := []struct {
		name    string
		got     string
		want    string
		notWant []string
	}{
		{"truncation notice only when truncating", oversized, memoryTruncatedNotice,
			[]string{memoryEmptyNotice, memoryUnavailableNotice, memoryStaleNotice}},
		{"empty notice only when empty", empty, memoryEmptyNotice,
			[]string{memoryTruncatedNotice, memoryUnavailableNotice, memoryStaleNotice}},
		{"stale notice only when stale", stale, memoryStaleNotice,
			[]string{memoryTruncatedNotice, memoryEmptyNotice, memoryUnavailableNotice}},
		{"unavailable notice only on the unavailable block", unavailable, memoryUnavailableNotice,
			[]string{memoryTruncatedNotice, memoryEmptyNotice, memoryStaleNotice}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Contains(t, tc.got, tc.want)
			for _, notice := range tc.notWant {
				require.NotContains(t, tc.got, notice)
			}
		})
	}

	// The empty case composes no index or notes section (spec agent-memory
	// "Empty memory still carries guidance").
	require.Contains(t, empty, "## Memory")
	require.NotContains(t, empty, "### Index")
	require.NotContains(t, empty, "### Recalled notes")
	// The unavailable block keeps the guidance — the model still learns HOW.
	require.Contains(t, unavailable, "## Memory")
}

// TestMemorySweepStale covers the staleness check (add-agent-memory design
// D13): no completed sweep within twice the interval is stale while the
// gateway is Running; a gateway that is not Running is never stale.
func TestMemorySweepStale(t *testing.T) {
	at := now()
	finish := func(before time.Duration) *time.Time {
		t := at.Add(-before)
		return &t
	}
	cases := []struct {
		name    string
		sw      store.MemorySweep
		found   bool
		running bool
		want    bool
	}{
		{"not running is never stale", store.MemorySweep{}, false, false, false},
		{"not running with an old finish", store.MemorySweep{LastFinish: finish(48 * time.Hour)}, true, false, false},
		{"running and never swept", store.MemorySweep{}, false, true, true},
		{"running and never finished", store.MemorySweep{LastFinish: nil}, true, true, true},
		{"running with a fresh finish", store.MemorySweep{LastFinish: finish(30 * time.Minute)}, true, true, false},
		{"running at exactly two intervals", store.MemorySweep{LastFinish: finish(2 * memorySweepInterval)}, true, true, false},
		{"running past two intervals", store.MemorySweep{LastFinish: finish(2*memorySweepInterval + time.Second)}, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, memorySweepStale(tc.sw, tc.found, tc.running, at))
		})
	}
}
