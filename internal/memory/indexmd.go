package memory

import (
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"
)

// The managed-section marker comments the consolidation sweep owns (spec
// agent-memory "Curated index with a managed section"): everything above the
// begin marker is the agent's free section and is never touched; everything
// between the markers is regenerated deterministically each sweep.
const (
	managedBegin = "<!-- memory:managed begin -->"
	managedEnd   = "<!-- memory:managed end -->"

	// promotedHeading heads the promoted subsection inside the managed
	// section.
	promotedHeading = "Promoted from daily notes:"

	// sourcePrefix and promotedPrefix introduce a promoted entry's source
	// reference and promotion date lines.
	sourcePrefix   = "  - Source: "
	promotedPrefix = "  - Promoted: "
)

// IndexNote is one active fact note's line in the managed index: name,
// description, and the retention score that orders the list.
type IndexNote struct {
	Name        string
	Description string
	Retention   float64
}

// PromotedLine is one entry of the promoted subsection: a daily-note line
// appended verbatim with its source reference and its promotion date (design
// D10).
type PromotedLine struct {
	Line       string
	SourceRef  string
	PromotedOn time.Time
}

// ParseIndex splits an index file's raw content into its free section and
// its promoted entries. The free section is everything above the begin
// marker, byte-for-byte — or the whole content when the file carries no
// markers yet. The promoted entries are the parseable "- line / Source /
// Promoted" triples of the managed section; anything unparseable there is
// dropped (the managed section is sweep-owned), while the free section is
// never interpreted.
func ParseIndex(raw string) (free string, promoted []PromotedLine) {
	begin := findMarkerLine(raw, managedBegin)
	if begin < 0 {
		return raw, nil
	}
	free = raw[:begin]

	end := len(raw)
	if marker := strings.Index(raw[begin:], managedEnd); marker >= 0 {
		end = begin + marker
	}
	promoted = parsePromotedLines(strings.Split(raw[begin:end], "\n")[1:])
	return free, promoted
}

// findMarkerLine returns the byte offset where marker starts a line, or −1
// when the content carries no such line.
func findMarkerLine(raw, marker string) int {
	idx := strings.Index(raw, marker)
	for idx > 0 && raw[idx-1] != '\n' {
		next := strings.Index(raw[idx+1:], marker)
		if next < 0 {
			return -1
		}
		idx += 1 + next
	}
	return idx
}

// parsePromotedLines extracts the promoted entries from a managed section's
// body lines. An entry is a "- <line>" bullet immediately followed by a
// source-reference line and a promotion-date line; anything else (the note
// list, stray text) is not an entry and is skipped.
func parsePromotedLines(lines []string) []PromotedLine {
	var out []PromotedLine
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if !strings.HasPrefix(line, "- ") || i+2 >= len(lines) {
			continue
		}
		source, ok := strings.CutPrefix(strings.TrimRight(lines[i+1], "\r"), sourcePrefix)
		if !ok {
			continue
		}
		dateText, ok := strings.CutPrefix(strings.TrimRight(lines[i+2], "\r"), promotedPrefix)
		if !ok {
			continue
		}
		promotedOn, err := time.Parse(dailyFormat, dateText)
		if err != nil {
			slog.Warn("memory: dropping promoted entry with bad date",
				"source", source, "date", dateText, "reason", err)
			continue
		}
		out = append(out, PromotedLine{
			Line:       strings.TrimPrefix(line, "- "),
			SourceRef:  source,
			PromotedOn: promotedOn.UTC(),
		})
		i += 2
	}
	return out
}

// RenderIndex renders the whole index file: the free section untouched, then
// the managed section — one line per active note ordered by retention (best
// first, name ascending on ties) followed by the promoted subsection ordered
// by promotion date then source reference. The output is deterministic: the
// same inputs always render byte-identically (design D14), so an unchanged
// memory state leaves the file — and the prompt prefix it feeds — stable.
func RenderIndex(free string, notes []IndexNote, promoted []PromotedLine) string {
	ordered := make([]IndexNote, len(notes))
	copy(ordered, notes)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Retention != ordered[j].Retention {
			return ordered[i].Retention > ordered[j].Retention
		}
		return ordered[i].Name < ordered[j].Name
	})

	entries := make([]PromotedLine, len(promoted))
	copy(entries, promoted)
	sort.SliceStable(entries, func(i, j int) bool {
		if !entries[i].PromotedOn.Equal(entries[j].PromotedOn) {
			return entries[i].PromotedOn.Before(entries[j].PromotedOn)
		}
		return entries[i].SourceRef < entries[j].SourceRef
	})

	var b strings.Builder
	b.WriteString(ensureFreeSeparator(free))
	b.WriteString(managedBegin)
	b.WriteString("\n")
	for _, n := range ordered {
		b.WriteString("- [")
		b.WriteString(n.Name)
		b.WriteString("](")
		b.WriteString(n.Name)
		b.WriteString(".md) — ")
		b.WriteString(n.Description)
		b.WriteString("\n")
	}
	if len(entries) > 0 {
		b.WriteString("\n")
		b.WriteString(promotedHeading)
		b.WriteString("\n\n")
		for _, p := range entries {
			b.WriteString("- ")
			b.WriteString(p.Line)
			b.WriteString("\n")
			b.WriteString(sourcePrefix)
			b.WriteString(p.SourceRef)
			b.WriteString("\n")
			b.WriteString(promotedPrefix)
			b.WriteString(p.PromotedOn.UTC().Format(dailyFormat))
			b.WriteString("\n")
		}
	}
	b.WriteString(managedEnd)
	b.WriteString("\n")
	return b.String()
}

// ensureFreeSeparator joins the free section to the begin marker: an empty
// free section contributes nothing, an already newline-terminated one passes
// through byte-for-byte, and a file without a trailing newline gains exactly
// the one separator newline the marker needs.
func ensureFreeSeparator(free string) string {
	if free == "" {
		return ""
	}
	if strings.HasSuffix(free, "\n") {
		return free
	}
	return free + "\n"
}

// WriteIndexFile writes the rendered index content only when it differs from
// what is on disk — a no-op sweep never rewrites the file, keeping its mtime
// (and therefore the next sweep's ingest watermark) honest. It reports
// whether the file was written.
func WriteIndexFile(path, content string) (written bool, err error) {
	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == content {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if writeErr := os.WriteFile(path, []byte(content), filePerm); writeErr != nil {
		return false, writeErr
	}
	return true, nil
}
