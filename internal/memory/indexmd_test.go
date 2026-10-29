package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// indexFixtures returns a stable note list and promoted list for the
// golden-file tests.
func indexFixtures() ([]IndexNote, []PromotedLine) {
	notes := []IndexNote{
		{Name: "prefers-dark-mode", Description: "The user prefers dark mode in every tool", Retention: 0.42},
		{Name: "api-base-url", Description: "The staging API base URL", Retention: 0.17},
		{Name: "deploy-checklist", Description: "Steps to deploy the service", Retention: 0.17},
	}
	promoted := []PromotedLine{
		{
			Line:       "User's time zone is Asia/Jakarta (UTC+7)",
			SourceRef:  "memory/2026-10-26.md#L4",
			PromotedOn: time.Date(2026, 10, 26, 9, 30, 0, 0, time.UTC),
		},
		{
			Line:       "Release trains leave every second Tuesday",
			SourceRef:  "memory/2026-10-27.md#L2",
			PromotedOn: time.Date(2026, 10, 27, 9, 30, 0, 0, time.UTC),
		},
	}
	return notes, promoted
}

// TestRenderIndex_DeterministicGolden is the byte-stability golden file
// (design D14): the same inputs always render byte-for-byte the same file,
// notes ordered by retention (name ascending on ties) and promoted entries
// by date then source reference regardless of input order.
func TestRenderIndex_DeterministicGolden(t *testing.T) {
	notes, promoted := indexFixtures()
	free := "# Memory\n\nThe agent may write anything here.  \nIncluding   odd spacing.\n\n"

	first := RenderIndex(free, notes, promoted)

	// Shuffle the orders: the rendered output must not care.
	shuffledNotes := []IndexNote{notes[2], notes[0], notes[1]}
	shuffledPromoted := []PromotedLine{promoted[1], promoted[0]}
	second := RenderIndex(free, shuffledNotes, shuffledPromoted)

	require.Equal(t, first, second)

	want := free + `<!-- memory:managed begin -->
- [prefers-dark-mode](prefers-dark-mode.md) — The user prefers dark mode in every tool
- [api-base-url](api-base-url.md) — The staging API base URL
- [deploy-checklist](deploy-checklist.md) — Steps to deploy the service

Promoted from daily notes:

- User's time zone is Asia/Jakarta (UTC+7)
  - Source: memory/2026-10-26.md#L4
  - Promoted: 2026-10-26
- Release trains leave every second Tuesday
  - Source: memory/2026-10-27.md#L2
  - Promoted: 2026-10-27
<!-- memory:managed end -->
`
	require.Equal(t, want, first)
}

// TestRenderIndex_SameDateTieBreak covers the promoted ordering on equal
// dates: the source reference breaks the tie, ascending.
func TestRenderIndex_SameDateTieBreak(t *testing.T) {
	day := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	promoted := []PromotedLine{
		{Line: "second", SourceRef: "memory/2026-10-27.md#L9", PromotedOn: day},
		{Line: "first", SourceRef: "memory/2026-10-27.md#L2", PromotedOn: day},
	}

	rendered := RenderIndex("", nil, promoted)

	require.Contains(t, rendered, "- first\n  - Source: memory/2026-10-27.md#L2")
	require.Contains(t, rendered, "- second\n  - Source: memory/2026-10-27.md#L9")
	require.Less(t, strings.Index(rendered, "- first\n"), strings.Index(rendered, "- second\n"))
}

// TestRenderIndex_Empty covers the empty index: bare markers, nothing else —
// still byte-stable.
func TestRenderIndex_Empty(t *testing.T) {
	require.Equal(t, "<!-- memory:managed begin -->\n<!-- memory:managed end -->\n",
		RenderIndex("", nil, nil))
	require.Equal(t, RenderIndex("", nil, nil), RenderIndex("", nil, nil))
}

// TestParseIndex_RoundTrip covers the parse of what render wrote: the free
// section comes back byte-for-byte and the promoted entries round-trip.
func TestParseIndex_RoundTrip(t *testing.T) {
	free := "# Memory\n\nAgent's own notes.\n"
	notes, promoted := indexFixtures()
	rendered := RenderIndex(free, notes, promoted)

	gotFree, gotPromoted := ParseIndex(rendered)

	require.Equal(t, free, gotFree)
	require.Len(t, gotPromoted, 2)
	require.Equal(t, promoted[0].Line, gotPromoted[0].Line)
	require.Equal(t, promoted[0].SourceRef, gotPromoted[0].SourceRef)
	require.Equal(t, promoted[0].PromotedOn.Format(dailyFormat), gotPromoted[0].PromotedOn.Format(dailyFormat))
}

// TestParseIndex_NoMarkers covers a pre-existing index the sweep has never
// managed: the whole content is the free section and nothing is promoted.
func TestParseIndex_NoMarkers(t *testing.T) {
	raw := "# Handwritten\n\nno markers here\n"

	free, promoted := ParseIndex(raw)

	require.Equal(t, raw, free)
	require.Empty(t, promoted)
}

// TestParseIndex_DropsBrokenEntries covers robustness: a promoted entry with
// an unparseable date is dropped, the good one survives.
func TestParseIndex_DropsBrokenEntries(t *testing.T) {
	raw := "<!-- memory:managed begin -->\n" +
		"- [a](a.md) — a note line, not an entry\n" +
		"- good line\n  - Source: memory/2026-10-26.md#L1\n  - Promoted: 2026-10-26\n" +
		"- broken line\n  - Source: memory/2026-10-26.md#L2\n  - Promoted: not-a-date\n" +
		"<!-- memory:managed end -->\n"

	free, promoted := ParseIndex(raw)

	require.Equal(t, "", free)
	require.Len(t, promoted, 1)
	require.Equal(t, "good line", promoted[0].Line)
}

// TestWriteIndexFile_NoOpIsNotWritten is the sweep's golden-file contract: a
// second write of identical content does not touch the file at all — same
// bytes and same modification time.
func TestWriteIndexFile_NoOpIsNotWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "MEMORY.md")
	notes, promoted := indexFixtures()
	content := RenderIndex("# Memory\n", notes, promoted)

	written, err := WriteIndexFile(path, content)
	require.NoError(t, err)
	require.True(t, written)

	info, err := os.Stat(path)
	require.NoError(t, err)

	written, err = WriteIndexFile(path, content)
	require.NoError(t, err)
	require.False(t, written, "identical content must not rewrite the file")

	again, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, info.ModTime(), again.ModTime(), "the no-op write must not touch the file")

	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, string(onDisk))
}

// TestWriteIndexFile_ChangedContentRewrites covers the write-on-change
// direction: new content replaces the file's.
func TestWriteIndexFile_ChangedContentRewrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "MEMORY.md")

	_, err := WriteIndexFile(path, "first\n")
	require.NoError(t, err)

	written, err := WriteIndexFile(path, "second\n")
	require.NoError(t, err)
	require.True(t, written)

	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "second\n", string(onDisk))
}
